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

	corev1 "k8s.io/api/core/v1"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kipsv1alpha1 "faux.ninja/kips-operator/api/v1alpha1"

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
		return ctrl.Result{}, ignoreNotFound(err)
	}

	serviceName := serviceBridge.Spec.TargetServiceName
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

	deletionTime := serviceBridge.ObjectMeta.DeletionTimestamp
	if !deletionTime.IsZero() {
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

	// TODO - create a deployment for azbridge

	return ctrl.Result{}, nil
}

func (r *ServiceBridgeReconciler) ensureFinalizerPresent(ctx context.Context, log logr.Logger, serviceBridge *kipsv1alpha1.ServiceBridge) error {
	// The object is not being deleted, so if it does not have our finalizer,
	// then lets add the finalizer and update the object. This is equivalent
	// registering our finalizer.
	if !containsString(serviceBridge.ObjectMeta.Finalizers, finalizerName) {
		log.V(1).Info("Adding ServiceBridge finalizer")
		serviceBridge.ObjectMeta.Finalizers = append(serviceBridge.ObjectMeta.Finalizers, finalizerName)
		if err := r.Update(ctx, serviceBridge); err != nil {
			return err
		}
	}
	return nil
}
func (r *ServiceBridgeReconciler) tearDownKipsAndRemoveFinalizer(ctx context.Context, log logr.Logger, serviceBridge *kipsv1alpha1.ServiceBridge) error {
	if containsString(serviceBridge.ObjectMeta.Finalizers, finalizerName) {

		// our finalizer is present, so lets handle any external dependency
		if err := r.revertServiceSelectors(ctx, log, serviceBridge); err != nil {
			// if fail to delete the external dependency here, return with error
			// so that it can be retried
			return err
		}

		// TODO - delete the config map
		configMap := r.getConfigMap(*serviceBridge)
		if err := r.Delete(ctx, configMap); err != nil {
			r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeNormal, "ConfigMapDeleteFailed", fmt.Sprintf("Failed to delete ConfigMap: %s", err))
			return err
		}
		r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeNormal, "ConfigMapDeleted", "Deleted ConfigMap")

		// TODO - delete the deployment

		// remove our finalizer from the list and update it.
		log.V(1).Info("Remove ServiceBridge finalizer")
		serviceBridge.ObjectMeta.Finalizers = removeString(serviceBridge.ObjectMeta.Finalizers, finalizerName)
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

	newStatusTemp := fmt.Sprintf("working... %s: %v", serviceBridge.Spec.TargetServiceName, service.Spec.Selector)
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

func (r *ServiceBridgeReconciler) ensureConfigMap(ctx context.Context, log logr.Logger, serviceBridge *kipsv1alpha1.ServiceBridge, service *corev1.Service) (*ctrl.Result, error) {

	desiredConfigMap := r.getConfigMap(*serviceBridge)
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
				return &ctrl.Result{}, nil
			}
			r.eventBroadcaster.Event(serviceBridge, corev1.EventTypeNormal, "ConfigMapUpdated", "Updated ConfigMap")
		}
	}

	return nil, nil
}

func (r *ServiceBridgeReconciler) revertServiceSelectors(ctx context.Context, log logr.Logger, serviceBridge *kipsv1alpha1.ServiceBridge) error {

	serviceName := serviceBridge.Spec.TargetServiceName
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

func (r *ServiceBridgeReconciler) getConfigMap(serviceBridge kipsv1alpha1.ServiceBridge) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: serviceBridge.Namespace,
			Name:      serviceBridge.Name,
		},
		Data: map[string]string{
			"config.yaml": `LocalForward:
	- RelayName: api
		BindAddress: 0.0.0.0
		BindPort: 80
		PortName: http`,
		},
	}
}

func ignoreNotFound(err error) error {
	if apierrs.IsNotFound(err) {
		return nil
	}
	return err
}
func ignoreConflict(err error) error {
	if apierrs.IsConflict(err) {
		return nil
	}
	return err
}

func (r *ServiceBridgeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.eventBroadcaster = mgr.GetEventRecorderFor("kips-operator")
	return ctrl.NewControllerManagedBy(mgr).
		For(&kipsv1alpha1.ServiceBridge{}).
		Complete(r)
}

// Helper functions to check and remove string from a slice of strings.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}
