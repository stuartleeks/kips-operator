package controllers

import (
	"context"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apierrs "k8s.io/apimachinery/pkg/api/errors"

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

	// var serviceBridge kipsv1alpha1.ServiceBridge
	serviceBridge := kipsv1alpha1.ServiceBridge{}

	if err := r.Get(ctx, req.NamespacedName, &serviceBridge); err != nil {
		err2 := ignoreNotFound(err)
		if err2 == nil {
			log.Info("ServiceBridge not found")
			return ctrl.Result{}, nil
		}
		log.Error(err2, "unable to fetch ServiceBridge")
		return ctrl.Result{}, ignoreNotFound(err2)
	}

	serviceBridge.Status.Temp = "working..." + serviceBridge.Spec.TargetServiceName

	if err := r.Status().Update(ctx, &serviceBridge); err != nil {
		log.Error(err, "unable to update ServiceBridge status")
		return ctrl.Result{}, err
	}

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
