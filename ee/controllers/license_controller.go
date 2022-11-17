/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"errors"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"os"
	"primehub-controller/ee/pkg/license"
	"reflect"
	"strconv"
	"time"

	"github.com/fatih/structtag"
	"k8s.io/apimachinery/pkg/runtime"
	primehubv1alpha1 "primehub-controller/ee/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// LicenseReconciler reconciles a License object
type LicenseReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	resourceName      string
	resourceNamespace string
	RequeueAfter      time.Duration
	platformType      string
}

func (r *LicenseReconciler) buildSecret(status *primehubv1alpha1.LicenseStatus) *corev1.Secret {
	s := reflect.ValueOf(*status)
	t := s.Type()
	data := map[string][]byte{}

	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		untypedValue := s.Field(i).Interface()

		tag, err := structtag.Parse(string(sf.Tag))
		if err != nil {
			panic("unable to parse LicenseStatus tag")
		}
		jsonTag, err := tag.Get("json")
		if err != nil {
			panic("unable to get LicenseStatus json tag")
		}

		var value string
		switch v := untypedValue.(type) {
		case *int:
			value = strconv.Itoa(*v)
		case string:
			value = v
		default:
			panic("unsupported LicenseStatus field type")
		}

		key := jsonTag.Name
		data[key] = []byte(value)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      license.SECRET_NAME,
			Namespace: r.resourceNamespace,
		},
		Data: data,
	}
	return secret
}

func (r *LicenseReconciler) updateSecret(ctx context.Context, lic *primehubv1alpha1.License) (err error) {
	desiredSecret := r.buildSecret(&lic.Status)
	if err = ctrl.SetControllerReference(lic, desiredSecret, r.Scheme); err != nil {
		return
	}

	secret := &corev1.Secret{}
	if err = r.Get(ctx, client.ObjectKey{Namespace: r.resourceNamespace, Name: license.SECRET_NAME}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			err = r.Create(ctx, desiredSecret)
		}
	} else {
		err = r.Update(ctx, desiredSecret)
	}

	return
}

func (r *LicenseReconciler) generateStatus(content map[string]string) (status primehubv1alpha1.LicenseStatus) {
	status.LicensedTo = content["licensed_to"]
	status.StartedAt = content["started_at"]
	status.ExpiredAt = content["expired_at"]
	status.MaxUser = getIntField(content, "max_user", -1)
	status.MaxGroup = getIntField(content, "max_group", -1)
	status.MaxNode = getIntField(content, "max_node", -1)
	status.MaxModelDeploy = getIntField(content, "max_model_deploy", 0)
	if value, ok := content["platform_type"]; ok {
		status.PlatformType = value
	}
	return
}

func (r *LicenseReconciler) createDefaultLicense() (lic primehubv1alpha1.License, err error) {
	signedLicense := license.DEFAULT_SIGNED_LICENSE
	content, err := license.Decode(signedLicense)
	if err != nil {
		panic("invalid default license")
	}

	defaultLic := primehubv1alpha1.License{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.resourceName,
			Namespace: r.resourceNamespace,
		},
		Spec: primehubv1alpha1.LicenseSpec{
			SignedLicense: signedLicense,
		},
	}
	status := r.generateStatus(content)
	status.Expired = license.STATUS_INVALID
	status.Reason = "Invalid license or wrong target platform. Use the default license instead."
	defaultLic.Status = status

	return defaultLic, nil
}

//+kubebuilder:rbac:groups=primehub.io,resources=licenses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=primehub.io,resources=licenses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=primehub.io,resources=licenses/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update

func (r *LicenseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	log := r.Log.WithValues("license", req.NamespacedName)

	// Skip if it's not in the same namespace
	if req.Namespace != r.resourceNamespace {
		return ctrl.Result{}, nil
	}

	result = ctrl.Result{RequeueAfter: r.RequeueAfter}

	lic := &primehubv1alpha1.License{}
	if err = r.Get(ctx, client.ObjectKey{Namespace: r.resourceNamespace, Name: r.resourceName}, lic); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("License not found, use default")
			defaultLic, _ := r.createDefaultLicense()

			if err = r.Create(ctx, &defaultLic); err != nil {
				log.Error(err, "failed to create default License")
				return
			}
		} else {
			log.Error(err, "unable to fetch License")
			return
		}
	}

	lic = lic.DeepCopy()
	verifiedLicense := license.NewLicense(lic.Spec.SignedLicense, r.platformType)
	if verifiedLicense.Status == license.STATUS_INVALID {
		log.Info(verifiedLicense.Err.Error())
		defaultLic, _ := r.createDefaultLicense()
		lic.Status = defaultLic.Status
	} else {
		status := r.generateStatus(verifiedLicense.Decoded)
		status.Expired = verifiedLicense.Status
		lic.Status = status
	}
	if err = r.Status().Update(ctx, lic); err != nil {
		log.Error(err, "failed to update License status")
		return
	}

	if err = r.updateSecret(ctx, lic); err != nil {
		log.Error(err, "failed to update Authoritative Secret")
		return
	}

	log.Info("End of reconciling")
	return
}

func (r *LicenseReconciler) EnsureLicense(mgr ctrl.Manager) (err error) {
	ctx := context.Background()
	log := r.Log.WithValues("license", "ensure license")

	// ref: https://github.com/kubernetes/test-infra/pull/15489/files
	// Wait for cachesync then ensure license installed
	mgrSyncCtx, mgrSyncCtxCancel := context.WithTimeout(context.Background(), 10*60*time.Second)
	defer mgrSyncCtxCancel()

	if synced := cache.WaitForCacheSync(mgrSyncCtx.Done()); !synced {
		return errors.New("timed out waiting for cachesync")
	}

	lic := &primehubv1alpha1.License{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: r.resourceNamespace, Name: r.resourceName}, lic); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("License not found, use default")
			defaultLic, _ := r.createDefaultLicense()
			if err := r.Create(ctx, &defaultLic); err != nil {
				return errors.New("failed to create default License")
			}
		} else {
			log.Error(err, "Get errors when EnsureLicense")
		}
	}

	return
}

func (r *LicenseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// ref: https://kubernetes.io/docs/tasks/inject-data-application/environment-variable-expose-pod-information/
	// Use the same namespace of PrimeHub Controller
	namespace := os.Getenv("POD_NAMESPACE")
	if namespace == "" {
		namespace = license.DEFAULT_RESOURCE_NAMESPACE
	}
	r.resourceName = license.RESOURCE_NAME
	r.resourceNamespace = namespace
	r.RequeueAfter = license.CHECK_EXPIRY_INTERVAL
	switch platformType := os.Getenv("PLATFORM_TYPE"); platformType {
	case license.PLATFORM_TYPE_ENTERPRISE, "ee":
		r.platformType = license.PLATFORM_TYPE_ENTERPRISE
	case license.PLATFORM_TYPE_DEPLOY:
		r.platformType = license.PLATFORM_TYPE_DEPLOY
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&primehubv1alpha1.License{}).
		Owns(&corev1.Secret{}).
		Complete(r)
}

func getIntField(content map[string]string, key string, defaultValue int) *int {
	value := new(int)
	if v, ok := content[key]; ok {
		*value, _ = strconv.Atoi(v)
	} else {
		*value = defaultValue
	}
	return value
}
