package controllers

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"strings"

	"github.com/go-logr/logr"
	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	primehubv1alpha1 "primehub-controller/ee/api/v1alpha1"
)

const (
	CustomImageJobStatusSucceeded = "Succeeded"
)

// ImageSpecReconciler reconciles a ImageSpec object
type ImageSpecReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

func ignoreNotFound(err error) error {
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

// +kubebuilder:rbac:groups=primehub.io,resources=imagespecs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=primehub.io,resources=imagespecs/status,verbs=get;update;patch

func (r *ImageSpecReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("imagespec", req.NamespacedName)

	if err := checkPushSecret(r, ctx, req, log); err != nil {
		return ctrl.Result{}, err
	}

	var imageSpec primehubv1alpha1.ImageSpec
	if err := r.Get(ctx, req.NamespacedName, &imageSpec); err != nil {
		if ignoreNotFound(err) != nil {
			log.Error(err, "unable to fetch ImageSpec")
		}
		return ctrl.Result{}, ignoreNotFound(err)
	}

	imageSpecClone := imageSpec.DeepCopy()
	if imageSpecClone.Spec.UpdateTime.IsZero() {
		log.Info("updateTime is not set, auto fill the current local time")
		now := metav1.Now()
		imageSpecClone.Spec.UpdateTime = &now
		if err := r.Update(ctx, imageSpecClone); err != nil {
			log.Error(err, "failed to update ImageSpec updateTime")
			return ctrl.Result{}, err
		}
	}

	t := imageSpecClone.Spec.UpdateTime.Rfc3339Copy()
	if !imageSpecClone.Spec.UpdateTime.Equal(&t) {
		imageSpecClone.Spec.UpdateTime = &t
		if err := r.Update(ctx, imageSpecClone); err != nil {
			log.Error(err, "failed to update ImageSpec updateTime")
			return ctrl.Result{}, err
		}
	}

	hash := computeHash(imageSpec)

	log.Info("Computed hash:", "hash", hash)

	imageSpecJob := primehubv1alpha1.ImageSpecJob{}
	name := imageSpec.ObjectMeta.Name + "-" + hash
	err := r.Get(ctx, client.ObjectKey{Namespace: imageSpec.Namespace, Name: name}, &imageSpecJob)
	if apierrors.IsNotFound(err) {
		log.Info("could not find existing ImageSpecJob for ImageSpec, creating one...")

		imageSpecJob = *buildImageSpecJob(imageSpec, hash)
		if err := ctrl.SetControllerReference(&imageSpec, &imageSpecJob, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}

		if err := r.Client.Create(ctx, &imageSpecJob); err != nil {
			log.Error(err, "failed to create ImageSpecJob resource")
			return ctrl.Result{}, err
		}

		log.Info("created ImageSpecJob resource for ImageSpec")
		return ctrl.Result{}, nil
	}

	if err != nil {
		log.Error(err, "failed to get ImageSpecJob for ImageSpec resource")
		return ctrl.Result{}, err
	}

	if !imageSpecJob.Spec.UpdateTime.Equal(imageSpecClone.Spec.UpdateTime) {
		if err := r.Delete(ctx, &imageSpecJob, client.PropagationPolicy(metav1.DeletePropagationBackground)); ignoreNotFound(err) != nil {
			log.Error(err, "unable to delete outdated ImageSpecJob", "imageSpecJob", imageSpecJob)
		} else {
			log.Info("deleted old outdated ImageSpecJob", "imageSpecJob", imageSpecJob)
		}
		return ctrl.Result{}, nil
	}

	log.Info("updating ImageSpec resource status")

	imageSpecClone.Status.JobName = imageSpecJob.Name
	imageSpecClone.Status.Phase = string(imageSpecJob.Status.Phase)
	if imageSpecClone.Status.Phase == CustomImageJobStatusSucceeded {
		image := imageSpecJob.Spec.RepoPrefix + "/" + imageSpecJob.Spec.TargetImage
		imageSpecClone.Status.Image = image
	}

	if err := r.Status().Update(ctx, imageSpecClone); err != nil {
		log.Error(err, "failed to update ImageSpec status")
		return ctrl.Result{}, err
	}

	log.Info("resource status synced")

	return ctrl.Result{}, nil
}

func checkPushSecret(r *ImageSpecReconciler, ctx context.Context, req ctrl.Request, log logr.Logger) error {
	var secret corev1.Secret
	pushSecretName := viper.GetString("customImage.pushSecretName")
	if err := r.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: pushSecretName}, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			log.Error(err, "image builder is not configured. push secret '"+pushSecretName+"' not found")
		}
		return err
	}
	return nil
}

func (r *ImageSpecReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&primehubv1alpha1.ImageSpec{}).
		Owns(&primehubv1alpha1.ImageSpecJob{}).
		Complete(r)
}

func buildImageSpecJob(imageSpec primehubv1alpha1.ImageSpec, hash string) *primehubv1alpha1.ImageSpecJob {
	pushSecretName := viper.GetString("customImage.pushSecretName")
	repoPrefix := viper.GetString("customImage.pushRepoPrefix")
	imageSpecJob := primehubv1alpha1.ImageSpecJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:        imageSpec.ObjectMeta.Name + "-" + hash,
			Namespace:   imageSpec.Namespace,
			Annotations: map[string]string{"imagespecs.primehub.io/hash": hash},
			Labels: map[string]string{
				"imagespecs.primehub.io/name": imageSpec.ObjectMeta.Name,
				"app":                         "primehub-custom-image",
			},
		},
		Spec: primehubv1alpha1.ImageSpecJobSpec{
			BaseImage:   imageSpec.Spec.BaseImage,
			PullSecret:  imageSpec.Spec.PullSecret,
			Packages:    imageSpec.Spec.Packages,
			TargetImage: imageSpec.ObjectMeta.Name + ":" + hash,
			PushSecret:  pushSecretName,
			RepoPrefix:  repoPrefix,
			UpdateTime:  imageSpec.Spec.UpdateTime,
		},
	}

	return &imageSpecJob
}

func computeHash(imageSpec primehubv1alpha1.ImageSpec) string {
	var s []string
	s = append(s, imageSpec.Spec.BaseImage)
	if len(imageSpec.Spec.Packages.Apt) > 0 {
		s = append(s, fmt.Sprintf("apt:[%s]", strings.Join(imageSpec.Spec.Packages.Apt, ",")))
	}
	if len(imageSpec.Spec.Packages.Pip) > 0 {
		s = append(s, fmt.Sprintf("pip:[%s]", strings.Join(imageSpec.Spec.Packages.Pip, ",")))
	}
	if len(imageSpec.Spec.Packages.Conda) > 0 {
		s = append(s, fmt.Sprintf("conda:[%s]", strings.Join(imageSpec.Spec.Packages.Conda, ",")))
	}

	h := sha1.New()
	h.Write([]byte(strings.Join(s, ";")))

	return hex.EncodeToString(h.Sum(nil))[0:8]
}
