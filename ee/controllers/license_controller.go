package controllers

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	primehubv1alpha1 "primehub-controller/ee/api/v1alpha1"
)

// LicenseReconciler reconciles a License object
type LicenseReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=primehub.io,resources=licenses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=primehub.io,resources=licenses/status,verbs=get;update;patch

func (r *LicenseReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("license", req.NamespacedName)

	// your logic here
	license := &primehubv1alpha1.License{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: "hub", Name: "primehub-license"}, license); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("License not found")
			return ctrl.Result{}, nil
		} else {
			log.Error(err, "unable to fetch License")
			return ctrl.Result{}, nil
		}
	}

	log.Info("SIGNED_LIC: " + license.Spec.SignedLicense)

	return ctrl.Result{}, nil
}

func (r *LicenseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&primehubv1alpha1.License{}).
		Complete(r)
}
