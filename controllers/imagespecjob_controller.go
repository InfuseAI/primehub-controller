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

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	primehubv1alpha1 "primehub-controller/api/v1alpha1"
)

// ImageSpecJobReconciler reconciles a ImageSpecJob object
type ImageSpecJobReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=primehub.io,resources=imagespecjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=primehub.io,resources=imagespecjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;delete

func (r *ImageSpecJobReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("imagespecjob", req.NamespacedName)

	var imageSpecJob primehubv1alpha1.ImageSpecJob
	if err := r.Get(ctx, req.NamespacedName, &imageSpecJob); err != nil {
		log.Error(err, "unable to fetch ImageSpecJob")
		return ctrl.Result{}, nil
	}

	pod := corev1.Pod{}
	err := r.Client.Get(ctx, client.ObjectKey{Namespace: imageSpecJob.Namespace, Name: imageSpecJob.ObjectMeta.Name}, &pod)
	if apierrors.IsNotFound(err) {
		log.Info("could not find existing Pod for ImageSpecJob, creating one...")

		dockerfile := generateDockerfile(imageSpecJob)
		pod = *buildPod(imageSpecJob, dockerfile)
		if err := r.Client.Create(ctx, &pod); err != nil {
			log.Error(err, "failed to create Pod resource")
			return ctrl.Result{}, err
		}

		log.Info("created Pod resource for ImageSpecJob")
		return ctrl.Result{}, nil
	}

	if err != nil {
		log.Error(err, "failed to get Pod for ImageSpecJob resource")
		return ctrl.Result{}, err
	}

	log.Info("updating ImageSpecJob resource status")

	imageSpecJobClone := imageSpecJob.DeepCopy()
	imageSpecJobClone.Status.PodName = pod.Name
	imageSpecJobClone.Status.Phase = string(pod.Status.Phase)
	if pod.Status.StartTime != nil {
		imageSpecJobClone.Status.StartTime = pod.Status.StartTime
	}
	if len(pod.Status.ContainerStatuses) > 0 && pod.Status.ContainerStatuses[0].State.Terminated != nil {
		containerFinishedAt := pod.Status.ContainerStatuses[0].State.Terminated.FinishedAt
		imageSpecJobClone.Status.FinishTime = &containerFinishedAt
	}

	if r.Status().Update(ctx, imageSpecJobClone); err != nil {
		log.Error(err, "failed to update ImageSpecJob status")
		return ctrl.Result{}, err
	}

	log.Info("resource status synced")

	return ctrl.Result{}, nil
}

func (r *ImageSpecJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(&corev1.Pod{}, ".metadata.controller", func(rawObj runtime.Object) []string {
		// grab the pod object, extract the owner...
		pod := rawObj.(*corev1.Pod)
		owner := metav1.GetControllerOf(pod)
		if owner == nil {
			return nil
		}
		// ...make sure it's a ImageSpecJob...
		if owner.APIVersion != primehubv1alpha1.GroupVersion.String() || owner.Kind != "ImageSpecJob" {
			return nil
		}

		// ...and if so, return it
		return []string{owner.Name}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&primehubv1alpha1.ImageSpecJob{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}

func generateDockerfile(imageSpecJob primehubv1alpha1.ImageSpecJob) string {
	dockerfile := "FROM ubuntu:bionic\nRUN echo hello"
	return dockerfile
}

func buildPod(imageSpecJob primehubv1alpha1.ImageSpecJob, dockerfile string) *corev1.Pod {
	privileged := true
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            imageSpecJob.ObjectMeta.Name,
			Namespace:       imageSpecJob.Namespace,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(&imageSpecJob, primehubv1alpha1.GroupVersion.WithKind("ImageSpecJob"))},
			Annotations:     imageSpecJob.ObjectMeta.Annotations,
			Labels:          imageSpecJob.ObjectMeta.Labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "build-and-push",
					Image:   "quay.io/buildah/stable:v1.11.3",
					Command: []string{"/bin/sh", "/scripts/build-and-push.sh"},
					Env: []corev1.EnvVar{
						{
							Name:  "DOCKERFILE",
							Value: dockerfile,
						},
						{
							Name:  "IMAGE_NAME",
							Value: imageSpecJob.ObjectMeta.Labels["imagespecs.primehub.io/name"],
						},
						{
							Name:  "IMAGE_TAG",
							Value: imageSpecJob.ObjectMeta.Annotations["imagespecs.primehub.io/hash"],
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "scripts",
							MountPath: "/scripts",
							ReadOnly:  true,
						},
						{
							Name:      "registry",
							MountPath: "/var/run/secrets/registry",
							ReadOnly:  true,
						},
					},
					SecurityContext: &corev1.SecurityContext{
						Privileged: &privileged,
					},
				},
			},
			RestartPolicy: "Never",
			Volumes: []corev1.Volume{
				{
					Name: "scripts",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "script-build-and-push",
							},
						},
					},
				},
				{
					Name: "registry",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: imageSpecJob.Spec.PushSecret,
						},
					},
				},
			},
		},
	}

	return &pod
}
