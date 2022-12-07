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
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"primehub-controller/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
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

//+kubebuilder:rbac:groups=primehub.io,resources=imagespecs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=primehub.io,resources=imagespecs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=primehub.io,resources=imagespecs/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ImageSpec object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.2/pkg/reconcile
func (r *ImageSpecReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("imagespec", req.NamespacedName)

	if err := checkPushSecret(r, ctx, req, log); err != nil {
		return ctrl.Result{}, err
	}

	var imageSpec v1alpha1.ImageSpec
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

	hash := computeHash(imageSpec.Spec.BaseImage, imageSpec.Spec.Packages)

	log.Info("Computed hash:", "hash", hash)

	imageSpecJob := v1alpha1.ImageSpecJob{}
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
		image := imageSpecJob.TargetImageURL()
		imageSpecClone.Status.Image = image
	}

	if err := r.Status().Update(ctx, imageSpecClone); err != nil {
		log.Error(err, "failed to update ImageSpec status")
		return ctrl.Result{}, err
	}

	log.Info("resource status synced")

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ImageSpecReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ImageSpec{}).
		Complete(r)
}

func buildImageSpecJob(imageSpec v1alpha1.ImageSpec, hash string) *v1alpha1.ImageSpecJob {
	pushSecretName := viper.GetString("customImage.pushSecretName")
	var repoPrefix string
	var targetImage string

	if repo := viper.GetString("customImage.pushRepo"); len(repo) > 0 {
		targetImage = repo + ":" + imageSpec.ObjectMeta.Name + "-" + hash
	} else {
		targetImage = imageSpec.ObjectMeta.Name + ":" + hash
		repoPrefix = viper.GetString("customImage.pushRepoPrefix")
	}
	imageSpecJob := v1alpha1.ImageSpecJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:        imageSpec.ObjectMeta.Name + "-" + hash,
			Namespace:   imageSpec.Namespace,
			Annotations: map[string]string{"imagespecs.primehub.io/hash": hash},
			Labels: map[string]string{
				"imagespecs.primehub.io/name": imageSpec.ObjectMeta.Name,
				"app":                         "primehub-custom-image",
			},
		},
		Spec: v1alpha1.ImageSpecJobSpec{
			BaseImage:   imageSpec.Spec.BaseImage,
			PullSecret:  imageSpec.Spec.PullSecret,
			Packages:    imageSpec.Spec.Packages,
			TargetImage: targetImage,
			PushSecret:  pushSecretName,
			RepoPrefix:  repoPrefix,
			UpdateTime:  imageSpec.Spec.UpdateTime,
		},
	}

	return &imageSpecJob
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

func computeHash(baseImage string, packages v1alpha1.ImageSpecSpecPackages) string {
	var s []string
	s = append(s, baseImage)
	if len(packages.Apt) > 0 {
		s = append(s, fmt.Sprintf("apt:[%s]", strings.Join(packages.Apt, ",")))
	}
	if len(packages.Pip) > 0 {
		s = append(s, fmt.Sprintf("pip:[%s]", strings.Join(packages.Pip, ",")))
	}
	if len(packages.Conda) > 0 {
		s = append(s, fmt.Sprintf("conda:[%s]", strings.Join(packages.Conda, ",")))
	}

	h := sha1.New()
	h.Write([]byte(strings.Join(s, ";")))

	return hex.EncodeToString(h.Sum(nil))[0:8]
}
