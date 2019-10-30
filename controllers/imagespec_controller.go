/*

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
	"os"
	"strings"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	primehubv1alpha1 "primehub-controller/api/v1alpha1"
)

// ImageSpecReconciler reconciles a ImageSpec object
type ImageSpecReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=primehub.io,resources=imagespecs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=primehub.io,resources=imagespecs/status,verbs=get;update;patch

func (r *ImageSpecReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("imagespec", req.NamespacedName)

	var imageSpec primehubv1alpha1.ImageSpec
	if err := r.Get(ctx, req.NamespacedName, &imageSpec); err != nil {
		log.Error(err, "unable to fetch ImageSpec")
		return ctrl.Result{}, nil
	}

	revision := computeHash(imageSpec)
	repoPrefix := os.Getenv("BUILD_IMAGE_REPO_PREFIX")

	log.Info("Computed hash:", "hash", revision)

	imageSpecJob := primehubv1alpha1.ImageSpecJob{}
	err := r.Get(ctx, client.ObjectKey{Namespace: imageSpec.Namespace, Name: imageSpec.ObjectMeta.Name + "-" + revision}, &imageSpecJob)
	if apierrors.IsNotFound(err) {
		log.Info("could not find existing ImageSpecJob for ImageSpec, creating one...")

		imageSpecJob = *buildImageSpecJob(imageSpec, revision, repoPrefix)
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

	log.Info("updating ImageSpec resource status")
	imageSpecClone := imageSpec.DeepCopy()

	imageSpecClone.Status.JobName = imageSpecJob.Name
	imageSpecClone.Status.Phase = string(imageSpecJob.Status.Phase)
	if imageSpecClone.Status.Phase == "Succeeded" {
		imageSpecClone.Status.Image = fmt.Sprintf("%s/%s:%s", repoPrefix, imageSpec.ObjectMeta.Name, revision)
	}

	if r.Status().Update(ctx, imageSpecClone); err != nil {
		log.Error(err, "failed to update ImageSpec status")
		return ctrl.Result{}, err
	}

	log.Info("resource status synced")

	return ctrl.Result{}, nil
}

func (r *ImageSpecReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(&primehubv1alpha1.ImageSpecJob{}, ".metadata.controller", func(rawObj runtime.Object) []string {
		// grab the imageSpecJob object, extract the owner...
		job := rawObj.(*primehubv1alpha1.ImageSpecJob)
		owner := metav1.GetControllerOf(job)
		if owner == nil {
			return nil
		}
		// ...make sure it's a ImageSpec...
		if owner.APIVersion != primehubv1alpha1.GroupVersion.String() || owner.Kind != "ImageSpec" {
			return nil
		}

		// ...and if so, return it
		return []string{owner.Name}
	}); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&primehubv1alpha1.ImageSpec{}).
		Owns(&primehubv1alpha1.ImageSpecJob{}).
		Complete(r)
}

func buildImageSpecJob(imageSpec primehubv1alpha1.ImageSpec, hash string, repoPrefix string) *primehubv1alpha1.ImageSpecJob {
	imageSpecJob := primehubv1alpha1.ImageSpecJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      imageSpec.ObjectMeta.Name + "-" + hash,
			Namespace: imageSpec.Namespace,
			Annotations: map[string]string{
				"imagespecs.primehub.io/hash": hash,
			},
			Labels: map[string]string{
				"imagespecs.primehub.io/name": imageSpec.ObjectMeta.Name,
			},
		},
		Spec: primehubv1alpha1.ImageSpecJobSpec{
			BaseImage:   imageSpec.Spec.BaseImage,
			PullSecret:  imageSpec.Spec.PullSecret,
			Packages:    imageSpec.Spec.Packages,
			TargetImage: fmt.Sprintf("%s:%s", imageSpec.ObjectMeta.Name, hash),
			PushSecret:  os.Getenv("BUILD_IMAGE_SECRET_NAME"),
			RepoPrefix:  repoPrefix,
		},
	}

	return &imageSpecJob
}

func computeHash(imageSpec primehubv1alpha1.ImageSpec) string {
	var s []string
	s = append(s, imageSpec.Spec.BaseImage)
	s = append(s, fmt.Sprintf("apt:[%s]", strings.Join(imageSpec.Spec.Packages.Apt, ",")))
	s = append(s, fmt.Sprintf("pip:[%s]", strings.Join(imageSpec.Spec.Packages.Pip, ",")))
	s = append(s, fmt.Sprintf("conda:[%s]", strings.Join(imageSpec.Spec.Packages.Conda, ",")))

	h := sha1.New()
	h.Write([]byte(strings.Join(s, ";")))

	return hex.EncodeToString(h.Sum(nil))[0:8]
}
