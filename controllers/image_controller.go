package controllers

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	primehubv1alpha1 "primehub-controller/api/v1alpha1"
)

// ImageReconciler reconciles a Image object
type ImageReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=primehub.io,resources=images,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=primehub.io,resources=images/status,verbs=get;update;patch

func (r *ImageReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	_ = r.Log.WithValues("image", req.NamespacedName)

	// your logic here

	return ctrl.Result{}, nil
}

func (r *ImageReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&primehubv1alpha1.Image{}).
		Complete(r)
}
