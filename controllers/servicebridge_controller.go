package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kipsv1alpha1 "faux.ninja/kips-operator/api/v1alpha1"
	utils "faux.ninja/kips-operator/utils"

	"k8s.io/client-go/tools/record"
)

// ReferencedServices holds the services referenced by a service bridge
type ReferencedServices struct {
	TargetService      *corev1.Service
	AdditionalServices map[string]*corev1.Service // AdditionalServices keyed on service name
}

// ServiceBridgeReconciler reconciles a ServiceBridge object
type ServiceBridgeReconciler struct {
	client.Client
	Log              logr.Logger
	counter          int
	eventBroadcaster record.EventRecorder
}

const (
	finalizerName                      string = "servicebridge.finalizers.kips.faux.ninja"
	annotationServiceOriginalSelectors string = "faux.ninja/kips-original-selectors"
	annotationServiceServiceBridge     string = "faux.ninja/kips-servicebridge"
)

// +kubebuilder:rbac:groups=kips.faux.ninja,resources=servicebridges,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kips.faux.ninja,resources=servicebridges/status,verbs=get;update;patch
// +kubebuilder:rbac:resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:resources=events,verbs=get;list;watch;create

// Reconcile loop
func (r *ServiceBridgeReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	r.counter++ // do we need atomic increment?
	log := r.Log.WithValues("servicebridge", req.NamespacedName).WithValues("counter", fmt.Sprintf("%d", r.counter))

	log.V(1).Info("starting reconcile")

	serviceBridge := &kipsv1alpha1.ServiceBridge{}
	if err := r.Get(ctx, req.NamespacedName, serviceBridge); err != nil {
		log.Error(err, "unable to fetch ServiceBridge - it may have been deleted") // TODO - look at whether we can prevent entering the reconcile loop on deletion when item is deleted
		return ctrl.Result{}, utils.IgnoreNotFound(err)
	}

	if r.isBeingDeleted(serviceBridge) {
		// The object is being deleted
		if err := r.tearDownKipsAndRemoveFinalizer(ctx, log, serviceBridge); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// add a finalizer to revert the selectors on the Service: https://book.kubebuilder.io/reference/using-finalizers.html?highlight=delete#using-finalizers
	if err := r.ensureFinalizerPresent(ctx, log, serviceBridge); err != nil {
		return ctrl.Result{}, err
	}

	referencedServices, err := r.getReferencedServices(ctx, serviceBridge)
	if err != nil {
		log.Error(err, "unable to fetch service")
		r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeWarning, "ServiceResolverError", err.Error())
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil // TODO - backoff, max attempts, ...?
	}

	// Create a config map for the azbridge config
	result, err := r.ensureConfigMap(ctx, log, serviceBridge, referencedServices)
	if result != nil {
		return *result, err
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	// create a deployment for azbridge
	result, err = r.ensureDeployment(ctx, log, serviceBridge)
	if result != nil {
		return *result, err
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	// remap the service selectors to the azbridge deployment
	// TODO - wait for the deployment to be ready
	result, err = r.updateServiceSelectors(ctx, log, serviceBridge, referencedServices.TargetService)
	if result != nil {
		return *result, err
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ServiceBridgeReconciler) getReferencedServices(ctx context.Context, serviceBridge *kipsv1alpha1.ServiceBridge) (*ReferencedServices, error) {

	referencedServices := &ReferencedServices{
		AdditionalServices: map[string]*corev1.Service{},
	}

	service, err := r.getService(ctx, serviceBridge.Spec.TargetService.Name, serviceBridge.ObjectMeta.Namespace)
	if err != nil {
		return nil, err
	}
	referencedServices.TargetService = service

	for _, additionalService := range serviceBridge.Spec.AdditionalServices {
		service, err := r.getService(ctx, additionalService.Name, serviceBridge.ObjectMeta.Namespace)
		if err != nil {
			return nil, err
		}
		referencedServices.AdditionalServices[service.Name] = service
	}

	return referencedServices, nil
}

func (r *ServiceBridgeReconciler) getService(ctx context.Context, name string, namespace string) (*corev1.Service, error) {
	serviceNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}

	service := &corev1.Service{}
	if err := r.Get(ctx, serviceNamespacedName, service); err != nil {
		return nil, fmt.Errorf("Failed to load service ('%s', namespace '%s'): %s", name, namespace, err)
	}
	return service, nil
}

func (r *ServiceBridgeReconciler) ensureFinalizerPresent(ctx context.Context, log logr.Logger, serviceBridge *kipsv1alpha1.ServiceBridge) error {
	// The object is not being deleted, so if it does not have our finalizer,
	// then lets add the finalizer and update the object. This is equivalent
	// registering our finalizer.
	if !utils.ContainsString(serviceBridge.ObjectMeta.Finalizers, finalizerName) {
		log.V(1).Info("Adding ServiceBridge finalizer")
		serviceBridge.ObjectMeta.Finalizers = append(serviceBridge.ObjectMeta.Finalizers, finalizerName)
		if err := r.Update(ctx, serviceBridge); err != nil {
			return err
		}
	}
	return nil
}
func (r *ServiceBridgeReconciler) tearDownKipsAndRemoveFinalizer(ctx context.Context, log logr.Logger, serviceBridge *kipsv1alpha1.ServiceBridge) error {
	if utils.ContainsString(serviceBridge.ObjectMeta.Finalizers, finalizerName) {

		// our finalizer is present, so lets handle any external dependencies

		// delete the config map
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: serviceBridge.Namespace,
				Name:      serviceBridge.Name,
			},
		}

		if err := r.Delete(ctx, configMap); err != nil {
			if !apierrs.IsNotFound(err) { // ignore not found - we wanted to delete it anyway!
				r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeNormal, "ConfigMapDeleteFailed", fmt.Sprintf("Failed to delete ConfigMap: %s", err))
				return err
			}
		} else {
			r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeNormal, "ConfigMapDeleted", "Deleted ConfigMap")
		}

		// delete the deployment
		deployment := r.getDeployment(*serviceBridge)
		if err := r.Delete(ctx, deployment); err != nil {
			if !apierrs.IsNotFound(err) { // ignore not found - we wanted to delete it anyway!
				r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeNormal, "DeploymentDeleteFailed", fmt.Sprintf("Failed to delete Deployment: %s", err))
				return err
			}
		} else {
			r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeNormal, "DeploymentDeleted", "Deleted Deployment")
		}

		// revert service info once other resources are cleaned up
		if err := r.revertServiceSelectors(ctx, log, serviceBridge); err != nil {
			// if fail to delete the external dependency here, return with error
			// so that it can be retried
			return err
		}

		// remove our finalizer from the list and update it.
		log.V(1).Info("Remove ServiceBridge finalizer")
		serviceBridge.ObjectMeta.Finalizers = utils.RemoveString(serviceBridge.ObjectMeta.Finalizers, finalizerName)
		if err := r.Update(context.Background(), serviceBridge); err != nil {
			return err
		}
	}
	return nil
}

func (r *ServiceBridgeReconciler) updateServiceSelectors(ctx context.Context, log logr.Logger, serviceBridge *kipsv1alpha1.ServiceBridge, service *corev1.Service) (*ctrl.Result, error) {

	serviceAppliedBridge := service.ObjectMeta.Annotations[annotationServiceServiceBridge]
	if serviceAppliedBridge != "" {
		if serviceAppliedBridge != serviceBridge.Name {
			r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeWarning, "ServiceAlreadyAttached", fmt.Sprintf("Service '%s' already attached to ServiceBridge '%s'", service.Name, serviceAppliedBridge))
			return &ctrl.Result{}, nil
		}
		return nil, nil // have already applied the service selector changes
	}

	newStatusTemp := fmt.Sprintf("working... %s: %v", serviceBridge.Spec.TargetService.Name, service.Spec.Selector)
	if newStatusTemp != serviceBridge.Status.Temp {
		log.V(1).Info("Update ServiceBridge status (Temp)")
		serviceBridge.Status.Temp = newStatusTemp
		if err := r.Status().Update(ctx, serviceBridge); err != nil {
			log.Error(err, "unable to update ServiceBridge status")
			return &ctrl.Result{}, err
		}
	}

	// store selectors as JSON on the Service
	originalSelectorsJSON, err := json.Marshal(service.Spec.Selector)
	if err != nil {
		log.Error(err, "Failed to convert selectors to JSON")
		return &ctrl.Result{}, err
	}
	service.ObjectMeta.Annotations[annotationServiceOriginalSelectors] = string(originalSelectorsJSON)
	service.ObjectMeta.Annotations[annotationServiceServiceBridge] = serviceBridge.Name // Check this when reverting in finalizer
	service.Spec.Selector = map[string]string{
		"service-bridge": serviceBridge.Name,
	}
	log.V(1).Info("Update Service metadata", "Service.ObjectMeta.ResourceVersion", service.ObjectMeta.ResourceVersion)
	if err := r.Update(ctx, service); err != nil {
		if apierrs.IsConflict(err) {
			// most likely cause is we got a stale Service from cache and the updates haven't propagated yet
			log.V(1).Info("Got conflict attempting to update Service metadata - requeuing", "Service.ObjectMeta.ResourceVersion", service.ObjectMeta.ResourceVersion)
			return &ctrl.Result{Requeue: true}, nil // TODO - add metric for number of conflicts? TODO - specify delay for requeue? TODO - limit number of retries??
		}
		log.Error(err, "unable to update Service metadata")
		return &ctrl.Result{}, err
	}
	log.V(1).Info("Updated Service metadata", "Service.ObjectMeta.ResourceVersion", service.ObjectMeta.ResourceVersion)
	r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeNormal, "ServiceMetadataUpdated", fmt.Sprintf("Service metadata updated ('%s')", service.Name))

	return nil, nil
}

func (r *ServiceBridgeReconciler) revertServiceSelectors(ctx context.Context, log logr.Logger, serviceBridge *kipsv1alpha1.ServiceBridge) error {

	serviceName := serviceBridge.Spec.TargetService.Name
	serviceNamespacedName := types.NamespacedName{
		Namespace: serviceBridge.ObjectMeta.Namespace,
		Name:      serviceName,
	}

	service := &corev1.Service{}

	if err := r.Get(ctx, serviceNamespacedName, service); err != nil {
		log.Error(err, "unable to fetch service")
		return err
	}

	serviceOriginalSelectors := service.ObjectMeta.Annotations[annotationServiceOriginalSelectors]
	serviceAppliedBridge := service.ObjectMeta.Annotations[annotationServiceServiceBridge]

	if serviceAppliedBridge == "" && serviceOriginalSelectors == "" {
		return nil // we didn't apply the selectors
	}
	if serviceAppliedBridge != serviceBridge.Name {
		// TODO - need to think about this. Should we just skip this step if the selectors don't match? Are we deleting a servicebridge that failed because a service already had another bridge attached?
		r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeWarning, "ServiceMetadataMismatch", fmt.Sprintf("Metadata for service '%s' doesn't match current ServiceBridge (expected '%s', got '%s')", service.Name, serviceBridge.Name, serviceAppliedBridge))
		return fmt.Errorf("Service does not match the current ServiceBridge name")
	}

	originalSelectors := map[string]string{}
	if err := json.Unmarshal([]byte(serviceOriginalSelectors), &originalSelectors); err != nil {
		return err
	}

	service.Spec.Selector = originalSelectors
	delete(service.ObjectMeta.Annotations, annotationServiceOriginalSelectors)
	delete(service.ObjectMeta.Annotations, annotationServiceServiceBridge)
	log.V(1).Info("Remove Service metadata", "Service.ObjectMeta.ResourceVersion", service.ObjectMeta.ResourceVersion)
	if err := r.Update(ctx, service); err != nil {
		log.Error(err, "unable to update Service metadata")
		return err
	}
	r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeNormal, "ServiceMetadataReverted", fmt.Sprintf("Service metadata updated ('%s')", service.Name))

	return nil
}

func (r *ServiceBridgeReconciler) ensureConfigMap(ctx context.Context, log logr.Logger, serviceBridge *kipsv1alpha1.ServiceBridge, referencedServices *ReferencedServices) (*ctrl.Result, error) {

	// TODO - set owner reference?

	desiredConfigMap, clientAzbridgeConfig, err := r.getAzBridgeConfig(*serviceBridge, referencedServices)
	if err != nil {
		r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeWarning, "ConfigMapCreateFailed", fmt.Sprintf("Failed to create desired config map: %s", err))
		log.Error(err, "Failed to create desired configMap")
		return &ctrl.Result{}, nil // don't pass the error as we don't want to requeue (TODO - revisit and requeue with back-off)
	}
	configMap := &corev1.ConfigMap{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: desiredConfigMap.Namespace, Name: desiredConfigMap.Name}, configMap); err != nil {
		if !apierrs.IsNotFound(err) {
			return &ctrl.Result{}, err
		}
		if err := r.Create(ctx, desiredConfigMap); err != nil {
			if apierrs.IsAlreadyExists(err) {
				// requeue and reprocess as cache is stale
				log.V(1).Info("Creating config map failed - already exists. Requeuing")
				return &ctrl.Result{Requeue: true}, nil
			}
			r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeWarning, "ConfigMapCreateFailed", fmt.Sprintf("Failed to create config map: %s", err))
			log.Error(err, "Failed to create configMap")
			return &ctrl.Result{}, nil
		}
		r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeNormal, "ConfigMapCreated", "Created ConfigMap")
	} else {
		if !reflect.DeepEqual(configMap.Data, desiredConfigMap.Data) {
			configMap.Data = desiredConfigMap.Data
			if err := r.Update(ctx, configMap); err != nil {
				r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeWarning, "ConfigMapUpdateFailed", fmt.Sprintf("Failed to update config map: %s", err))
				log.Error(err, "Failed to update configMap")
				return &ctrl.Result{}, err
			}
			r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeNormal, "ConfigMapUpdated", "Updated ConfigMap")
		}
	}

	if serviceBridge.Status.ClientAzbridgeConfig != clientAzbridgeConfig {
		serviceBridge.Status.ClientAzbridgeConfig = clientAzbridgeConfig
		if err := r.Status().Update(ctx, serviceBridge); err != nil {
			log.Error(err, "Failed to update ServiceBridge.Status.ClientAzbridgeConfig")
			return &ctrl.Result{}, err
		}
		log.V(1).Info("Updated ServiceBridge.Status.ClientAzbridgeConfig")

	}

	return nil, nil
}

func (r *ServiceBridgeReconciler) ensureDeployment(ctx context.Context, log logr.Logger, serviceBridge *kipsv1alpha1.ServiceBridge) (*ctrl.Result, error) {

	// TODO - set owner reference?

	desiredDeployment := r.getDeployment(*serviceBridge)
	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: desiredDeployment.Namespace, Name: desiredDeployment.Name}, deployment); err != nil {
		if !apierrs.IsNotFound(err) {
			return &ctrl.Result{}, err
		}
		if err := r.Create(ctx, desiredDeployment); err != nil {
			if apierrs.IsAlreadyExists(err) {
				// requeue and reprocess as cache is stale
				log.V(1).Info("Creating deployment - already exists. Requeuing")
				return &ctrl.Result{Requeue: true}, nil
			}
			r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeWarning, "DeploymentCreateFailed", fmt.Sprintf("Failed to create deployment: %s", err))
			log.Error(err, "Failed to create deployment")
			return &ctrl.Result{}, nil
		}
		r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeNormal, "DeploymentCreated", "Created Deployment")
	} else {
		if !reflect.DeepEqual(deployment.Spec, desiredDeployment.Spec) { // TODO - check if this needs refining
			deployment.Spec = desiredDeployment.Spec
			if err := r.Update(ctx, deployment); err != nil {
				r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeWarning, "DeploymentUpdateFailed", fmt.Sprintf("Failed to update deployment: %s", err))
				log.Error(err, "Failed to update configMap")
				return &ctrl.Result{}, err
			}
			r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeNormal, "DeploymentUpdated", "Updated Deployment")
		}
	}

	return nil, nil
}

// getAzBridgeConfig returns the ConfigMap for the service bridge, the azbridge config for the remote machine, and an error
func (r *ServiceBridgeReconciler) getAzBridgeConfig(serviceBridge kipsv1alpha1.ServiceBridge, referencedServices *ReferencedServices) (*corev1.ConfigMap, string, error) {

	// Config docs: https://github.com/Azure/azure-relay-bridge/blob/master/CONFIG.md#configuration-file
	// LocalForward is the config for the listener side
	// RemoteForward is the config for the side that forwards traffic on from the listener

	// TODO - look at relay naming. Can we use a relay for more than one port? Include the service bridge name in it?

	// TargetService - this is a service redirected from the cluster to the remote client
	// Want LocalForward for in-cluster deployment, RemoteForward for remote client
	clusterConfigData := "LocalForward:"
	remoteConfigData := "RemoteForward:"
	service := referencedServices.TargetService
	for _, targetPort := range serviceBridge.Spec.TargetService.Ports {
		servicePort := r.getServicePortByName(service.Spec.Ports, targetPort.Name)
		if servicePort == nil {
			// TODO Raise error on service bridge
			return nil, "", fmt.Errorf("Unable to find port '%s' on Target Service '%s'", targetPort.Name, service.Name)
		}

		port := servicePort.TargetPort.IntValue() // Use TargetPort here as we want to bind to port that traffic is sent to // TODO - need to handle StringValue being set, and looking this up on the ports in the pod
		portString := fmt.Sprintf("%d", port)
		// TODO - test multiple ports bound to single relay name
		// TODO - allow relay name(s) to be set in ServiceBridge spec
		clusterConfigData += "\n" +
			"  - RelayName: " + service.Name + "\n" +
			"    BindAddress: 0.0.0.0\n" +
			"    BindPort: " + portString + "\n" +
			"    PortName: port" + portString + "\n"

		// TODO - allow HostPort(s) to be set in ServiceBridge spec
		// TODO - allow Host to be set in spec
		remoteConfigData += "\n" +
			"  - RelayName: " + service.Name + "\n" +
			"    HostPort: " + fmt.Sprintf("%d", targetPort.RemotePort) + "\n" +
			"    Host: localhost\n" +
			"    PortName: port" + portString + "\n"
	}
	if len(serviceBridge.Spec.AdditionalServices) > 0 {
		// TargetService - this is a service redirected from the remote client to the cluster
		// Want RemoteForward for in-cluster deployment, LocalForward for remote client
		clusterConfigData += "RemoteForward:"
		remoteConfigData += "LocalForward:"

		for _, additionalService := range serviceBridge.Spec.AdditionalServices {
			service := referencedServices.AdditionalServices[additionalService.Name]
			for _, targetPort := range additionalService.Ports {
				servicePort := r.getServicePortByName(service.Spec.Ports, targetPort.Name)
				if servicePort == nil {
					// TODO Raise error on service bridge
					return nil, "", fmt.Errorf("Unable to find port '%s' on Additional Service '%s'", targetPort.Name, service.Name)
				}

				port := servicePort.Port // use Port here as we want to direct to the service port
				portString := fmt.Sprintf("%d", port)

				// TODO - allow HostPort(s) to be set in ServiceBridge spec
				// TODO - allow Host to be set in spec
				clusterConfigData += "\n" +
					"  - RelayName: " + service.Name + "\n" +
					"    HostPort: " + portString + "\n" +
					"    Host: " + service.Name + "\n" +
					"    PortName: port" + portString + "\n"

				// TODO - test multiple ports bound to single relay name
				// TODO - allow relay name(s) to be set in ServiceBridge spec
				remoteConfigData += "\n" +
					"  - RelayName: " + service.Name + "\n" +
					"    BindAddress: localhost\n" + // TODO - do we need to be able to customise this?
					"    BindPort: " + fmt.Sprintf("%d", targetPort.RemotePort) + "\n" +
					"    HostName: " + service.Name + "\n" +
					"    PortName: port" + portString + "\n"
			}
		}
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: serviceBridge.Namespace,
			Name:      serviceBridge.Name,
		},
		Data: map[string]string{
			"config.yaml": clusterConfigData,
		},
	}, remoteConfigData, nil
}

func (r *ServiceBridgeReconciler) getServicePortByName(ports []corev1.ServicePort, portName string) *corev1.ServicePort {
	for _, p := range ports {
		if p.Name == portName {
			return &p
		}
	}
	return nil
}

func (r *ServiceBridgeReconciler) getDeployment(serviceBridge kipsv1alpha1.ServiceBridge) *appsv1.Deployment {
	kipsName := serviceBridge.Name + "-kips"
	replicaCount := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: serviceBridge.Namespace,
			Name:      kipsName,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicaCount,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"kipsDeployment": kipsName},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: serviceBridge.Namespace,
					Name:      kipsName,
					Labels: map[string]string{
						"kipsDeployment": kipsName,
						"service-bridge": serviceBridge.Name},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "kips",
							Image:           "stuartleeks/kips:latest",
							ImagePullPolicy: corev1.PullAlways,
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("128Mi"),
									corev1.ResourceCPU:    resource.MustParse("500m"),
								},
							},
							Env: []corev1.EnvVar{
								{
									Name: "AZURE_BRIDGE_CONNECTIONSTRING",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "azbridge-connection-string"},
											Key:                  "connectionString",
										},
									},
								},
							},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 80,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "azbridge-config",
									MountPath: "/azbridge-config",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "azbridge-config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: serviceBridge.Name},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (r *ServiceBridgeReconciler) isBeingDeleted(serviceBridge *kipsv1alpha1.ServiceBridge) bool {
	deletionTime := serviceBridge.ObjectMeta.DeletionTimestamp
	return !deletionTime.IsZero()
}

// SetupWithManager sets up the controller
func (r *ServiceBridgeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.eventBroadcaster = mgr.GetEventRecorderFor("kips-operator")
	return ctrl.NewControllerManagedBy(mgr).
		For(&kipsv1alpha1.ServiceBridge{}).
		Complete(r)
}
