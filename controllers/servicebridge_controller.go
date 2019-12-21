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

	serviceName := serviceBridge.Spec.TargetService.Name
	serviceNamespacedName := types.NamespacedName{
		Namespace: serviceBridge.ObjectMeta.Namespace,
		Name:      serviceName,
	}
	service := &corev1.Service{}
	if err := r.Get(ctx, serviceNamespacedName, service); err != nil {
		log.Error(err, "unable to fetch service")
		r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeWarning, "ServiceNotFound", fmt.Sprintf("Unable to retrieve service '%s'", serviceName))
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil // TODO - backoff, max attempts, ...?
	}

	if r.isBeingDeleted(serviceBridge) {
		// The object is being deleted
		if err := r.tearDownKipsAndRemoveFinalizer(ctx, log, serviceBridge, service); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// add a finalizer to revert the selectors on the Service: https://book.kubebuilder.io/reference/using-finalizers.html?highlight=delete#using-finalizers
	if err := r.ensureFinalizerPresent(ctx, log, serviceBridge); err != nil {
		return ctrl.Result{}, err
	}

	result, err := r.updateServiceSelectors(ctx, log, serviceBridge, service)
	if result != nil {
		return *result, err
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	// Create a config map for the azbridge config
	result, err = r.ensureConfigMap(ctx, log, serviceBridge, service)
	if result != nil {
		return *result, err
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	// create a deployment for azbridge
	result, err = r.ensureDeployment(ctx, log, serviceBridge, service)
	if result != nil {
		return *result, err
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
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
func (r *ServiceBridgeReconciler) tearDownKipsAndRemoveFinalizer(ctx context.Context, log logr.Logger, serviceBridge *kipsv1alpha1.ServiceBridge, service *corev1.Service) error {
	if utils.ContainsString(serviceBridge.ObjectMeta.Finalizers, finalizerName) {

		// our finalizer is present, so lets handle any external dependency

		// delete the config map
		configMap, _, _ := r.getConfigMap(*serviceBridge, *service)
		if err := r.Delete(ctx, configMap); err != nil {
			r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeNormal, "ConfigMapDeleteFailed", fmt.Sprintf("Failed to delete ConfigMap: %s", err))
			return err
		}
		r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeNormal, "ConfigMapDeleted", "Deleted ConfigMap")

		// delete the deployment
		deployment := r.getDeployment(*serviceBridge, *service)
		if err := r.Delete(ctx, deployment); err != nil {
			r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeNormal, "DeploymentDeleteFailed", fmt.Sprintf("Failed to delete Deployment: %s", err))
			return err
		}
		r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeNormal, "DeploymentDeleted", "Deleted Deployment")

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
			return &ctrl.Result{}, fmt.Errorf("Service does not match the current ServiceBridge name")
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

	if serviceAppliedBridge != serviceBridge.Name {
		r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeWarning, "ServiceMetadataMismatch", fmt.Sprintf("Metadata for service '%s' doesn't match current ServiceBridge ('%s')", service.Name, serviceBridge.Name))
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

func (r *ServiceBridgeReconciler) ensureConfigMap(ctx context.Context, log logr.Logger, serviceBridge *kipsv1alpha1.ServiceBridge, service *corev1.Service) (*ctrl.Result, error) {

	// TODO - set owner reference?

	desiredConfigMap, clientAzbridgeConfig, err := r.getConfigMap(*serviceBridge, *service)
	if err != nil {
		return &ctrl.Result{}, err
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

func (r *ServiceBridgeReconciler) ensureDeployment(ctx context.Context, log logr.Logger, serviceBridge *kipsv1alpha1.ServiceBridge, service *corev1.Service) (*ctrl.Result, error) {

	// TODO - set owner reference?

	desiredDeployment := r.getDeployment(*serviceBridge, *service)
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

func (r *ServiceBridgeReconciler) getConfigMap(serviceBridge kipsv1alpha1.ServiceBridge, service corev1.Service) (*corev1.ConfigMap, string, error) {
	clusterConfigData := "LocalForward:"
	localConfigData := "RemoteForward:"

	for _, targetPort := range serviceBridge.Spec.TargetService.TargetServicePorts {

		var servicePort *corev1.ServicePort
		for _, p := range service.Spec.Ports {
			if p.Name == targetPort.Name {
				servicePort = &p
				break
			}
		}
		if servicePort == nil {
			// TODO Raise error on service bridge
			return nil, "", fmt.Errorf("Unable to find port '%s' on Service", targetPort.Name)
		}

		port := servicePort.TargetPort.IntValue() // TODO - need to handle StringValue being set, and looking this up on the ports in the pod
		portString := fmt.Sprintf("%d", port)
		// TODO - test multiple ports bound to single relay name
		// TODO - allow relay name(s) to be set in ServiceBridge spec
		clusterConfigData += "\n" +
			"  - RelayName: " + service.Name + "\n" +
			"    BindAddress: 0.0.0.0\n" +
			"    BindPort: " + portString + "\n" +
			"    PortName: port" + portString + "\n"

		// TODO - allow HostPort(s) to be set in ServiceBridge spec
		localConfigData += "\n" +
			"  - RelayName: " + service.Name + "\n" +
			"    HostPort: " + fmt.Sprintf("%d", targetPort.RemotePort) + "\n" +
			"    Host: localhost\n" +
			"    PortName: port" + portString + "\n"
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: serviceBridge.Namespace,
			Name:      serviceBridge.Name,
		},
		Data: map[string]string{
			"config.yaml": clusterConfigData,
		},
	}, localConfigData, nil
}

func (r *ServiceBridgeReconciler) getDeployment(serviceBridge kipsv1alpha1.ServiceBridge, service corev1.Service) *appsv1.Deployment {
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
							Image:           "slk8stest2.azurecr.io/azbridge:latest", // TODO - make this configurable
							ImagePullPolicy: corev1.PullAlways,
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("128Mi"),
									corev1.ResourceCPU:    resource.MustParse("500m"),
								},
							},
							Command: []string{"sh"},
							Args:    []string{"-C", "/azbridge-script/start-azbridge.sh"},
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
								{
									Name:      "azbridge-script",
									MountPath: "/azbridge-script",
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
						{
							Name: "azbridge-script",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: "azbridge-script"},
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

func (r *ServiceBridgeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.eventBroadcaster = mgr.GetEventRecorderFor("kips-operator")
	return ctrl.NewControllerManagedBy(mgr).
		For(&kipsv1alpha1.ServiceBridge{}).
		Complete(r)
}
