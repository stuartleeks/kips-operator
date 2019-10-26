package controllers

import (
	"context"
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

	// TODO store selectors as JSON on the *Service* rather than go map output on *ServiceBridge*
	serviceBridge.ObjectMeta.Annotations["OriginalSelectors"] = fmt.Sprintf("%v", service.Spec.Selector)
	if err := r.Update(ctx, &serviceBridge); err != nil {
		log.Error(err, "unable to update ServiceBridge metadata")
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
