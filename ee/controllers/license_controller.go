package controllers

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"primehub-controller/ee/pkg/license"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	primehubv1alpha1 "primehub-controller/ee/api/v1alpha1"
)

// LicenseReconciler reconciles a License object
type LicenseReconciler struct {
	client.Client
	Log               logr.Logger
	Scheme            *runtime.Scheme
	ResourceName      string
	ResourceNamespace string
}

func (r *LicenseReconciler) generateStatus(content map[string]string) (status primehubv1alpha1.LicenseStatus) {
	status.LicensedTo = content["licensed_to"]
	status.StartedAt = content["started_at"]
	status.ExpiredAt = content["expired_at"]
	status.MaxGroup, _ = strconv.Atoi(content["max_group"])
	status.MaxNode, _ = strconv.Atoi(content["max_node"])
	return status
}

func (r *LicenseReconciler) createDefaultLicense() (lic primehubv1alpha1.License, err error) {
	signedLicense := license.DEFAULT_SIGNED_LICENSE
	content, err := license.Decode(signedLicense)
	if err != nil {
		panic("invalid default license")
	}

	defaultLic := primehubv1alpha1.License{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.ResourceName,
			Namespace: r.ResourceNamespace,
		},
		Spec: primehubv1alpha1.LicenseSpec{
			SignedLicense: signedLicense,
		},
	}
	status := r.generateStatus(content)
	// TODO: need to check 'started_at' and 'expired_at'
	status.Expired = "check expired time"
	status.Reason = "invalid since we can't valid your licensed key, using default now"
	defaultLic.Status = status

	return defaultLic, nil
}

// +kubebuilder:rbac:groups=primehub.io,resources=licenses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=primehub.io,resources=licenses/status,verbs=get;update;patch

func (r *LicenseReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("license", req.NamespacedName)

	// Skip if it's not in the same namespace
	if req.Namespace != r.ResourceNamespace {
		return ctrl.Result{}, nil
	}

	lic := &primehubv1alpha1.License{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: r.ResourceNamespace, Name: r.ResourceName}, lic); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("License not found")
			// TODO: create a default license
		} else {
			log.Error(err, "unable to fetch License")
		}
		return ctrl.Result{}, nil
	}

	if ok, err := license.Verify(lic.Spec.SignedLicense); ok == false && err != nil {
		log.Info(err.Error())
		log.Info("Use default license")
		defaultLic, _ := r.createDefaultLicense()
		lic := lic.DeepCopy()
		lic.Spec = defaultLic.Spec
		lic.Status = defaultLic.Status
		if err := r.Update(ctx, lic); err != nil {
			log.Error(err, "failed to update License")
			return ctrl.Result{}, err
		}
	} else {
		// Decode message and fill in status field
		content, _ := license.Decode(lic.Spec.SignedLicense)
		lic := lic.DeepCopy()
		// TODO: need to merge status instead of replacement
		lic.Status = r.generateStatus(content)
		log.Info("lic", "status", lic.Status)
		if err := r.Status().Update(ctx, lic); err != nil {
			log.Error(err, "failed to update License status")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *LicenseReconciler) EnsureLicense(mgr ctrl.Manager) (err error) {
	ctx := context.Background()
	log := r.Log.WithValues("license", "ensure license")

	// ref: https://github.com/kubernetes/test-infra/pull/15489/files
	// Wait for cachesync then ensure license installed
	mgrSyncCtx, mgrSyncCtxCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer mgrSyncCtxCancel()

	if synced := mgr.GetCache().WaitForCacheSync(mgrSyncCtx.Done()); !synced {
		return errors.New("Timed out waiting for cachesync")
	}

	lic := &primehubv1alpha1.License{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: r.ResourceNamespace, Name: r.ResourceName}, lic); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("License not found, use default")
			defaultLic, _ := r.createDefaultLicense()
			if err := r.Create(ctx, &defaultLic); err != nil {
				return errors.New("failed to create default License")
			}
		}
	}

	return
}

func (r *LicenseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&primehubv1alpha1.License{}).
		Owns(&corev1.Secret{}).
		Complete(r)
}
