package controllers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	corev1 "k8s.io/api/core/v1"

	kipsv1alpha1 "faux.ninja/kips-operator/api/v1alpha1"
)

// ServiceBridgeReconciler reconciles a ServiceBridge object
type ServiceBridgeReconciler struct {
	client.Client
	Log logr.Logger
}

// +kubebuilder:rbac:groups=kips.faux.ninja,resources=servicebridges,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kips.faux.ninja,resources=servicebridges/status,verbs=get;update;patch
// +kubebuilder:rbac:resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:resources=events,verbs=get;list;watch;create

func (r *ServiceBridgeReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("servicebridge", req.NamespacedName)

	serviceBridge := kipsv1alpha1.ServiceBridge{}

	if err := r.Get(ctx, req.NamespacedName, &serviceBridge); err != nil {
		log.Error(err, "unable to fetch ServiceBridge")
		return ctrl.Result{}, ignoreNotFound(err)
	}

	// TODO - add a finalizer to revert the selectors on the Service: https://book.kubebuilder.io/reference/using-finalizers.html?highlight=delete#using-finalizers

	serviceName := serviceBridge.Spec.TargetServiceName
	serviceNamespacedName := types.NamespacedName{
		Namespace: req.Namespace,
		Name:      serviceName,
	}

	service := corev1.Service{}

	if err := r.Get(ctx, serviceNamespacedName, &service); err != nil {
		log.Error(err, "unable to fetch service")
		return ctrl.Result{}, ignoreNotFound(err)
	}

	serviceBridge.Status.Temp = fmt.Sprintf("working... %s: %v", serviceBridge.Spec.TargetServiceName, service.Spec.Selector)
	if err := r.Status().Update(ctx, &serviceBridge); err != nil {
		log.Error(err, "unable to update ServiceBridge status")
		return ctrl.Result{}, err
	}

	serviceOriginalSelectors := service.ObjectMeta.Annotations["faux.ninja/kips-original-selectors"]
	if serviceOriginalSelectors != "" {
		// another ServiceBridge is applied
		serviceBridge.Status.Message = "TargetService already has a ServiceBridge applied"
		if err := r.Status().Update(ctx, &serviceBridge); err != nil {
			log.Error(err, "unable to update ServiceBridge status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// store selectors as JSON on the Service
	originalSelectorsJSON, err := json.Marshal(service.Spec.Selector)
	if err != nil {
		log.Error(err, "Failed to convert selectors to JSON")
		return ctrl.Result{}, err
	}
	service.ObjectMeta.Annotations["faux.ninja/kips-original-selectors"] = string(originalSelectorsJSON)
	if err := r.Update(ctx, &service); err != nil {
		log.Error(err, "unable to update Service metadata")
		return ctrl.Result{}, err
	}

	// TODO - modify the Service to add the original selectors and apply new ones (Add a check that the original selectors aren't already set, which would indicate another ServiceBridge is already applied - fail in this case)
	// TODO - create a config map for the azbridge config
	// TODO - create a deployment for azbridge

	return ctrl.Result{}, nil
}

func ignoreNotFound(err error) error {
	if apierrs.IsNotFound(err) {
		return nil
	}
	return err
}

func (r *ServiceBridgeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kipsv1alpha1.ServiceBridge{}).
		Complete(r)
}
