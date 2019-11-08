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
	"bytes"
	"context"
	"text/template"

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

		if imageSpecJob.Spec.PullSecret != "" {
			pullSecret := corev1.Secret{}
			if err := r.Client.Get(ctx, client.ObjectKey{Namespace: imageSpecJob.Namespace, Name: imageSpecJob.Spec.PullSecret}, &pullSecret); err != nil {
				log.Error(err, "failed to get pull secret: "+imageSpecJob.Spec.PullSecret)
				return ctrl.Result{}, err
			}

		}

		dockerfile := generateDockerfile(imageSpecJob)
		pod = *buildPod(imageSpecJob, dockerfile)
		if err := ctrl.SetControllerReference(&imageSpecJob, &pod, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
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

	if err := r.Status().Update(ctx, imageSpecJobClone); err != nil {
		log.Error(err, "failed to update ImageSpecJob status")
		return ctrl.Result{}, err
	}

	log.Info("resource status synced")

	return ctrl.Result{}, nil
}

func (r *ImageSpecJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&primehubv1alpha1.ImageSpecJob{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}

func generateDockerfile(imageSpecJob primehubv1alpha1.ImageSpecJob) string {
	dockerfileTmpl := `FROM {{ .BaseImage }}

USER root
{{ if .Packages.Apt }}
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      {{- range $index, $element := .Packages.Apt }}
      {{ $element }} \
      {{- end }}
      && \
    apt-get purge && apt-get clean
{{ end }}

{{ if .Packages.Pip }}
RUN pip install --no-cache-dir
      {{- range $index, $element := .Packages.Pip }}
      {{- printf " %s" $element }}
      {{- end }}
{{ end }}

{{ if .Packages.Conda }}
RUN conda install --quiet --yes \
      {{- range $index, $element := .Packages.Conda }}
      {{ $element }} \
      {{- end }}
      && \
    conda clean --all -f -y
{{ end }}`

	var res bytes.Buffer
	tmpl, _ := template.New("dockerfile").Parse(dockerfileTmpl)
	_ = tmpl.Execute(&res, imageSpecJob.Spec)

	return res.String()
}

func buildPod(imageSpecJob primehubv1alpha1.ImageSpecJob, dockerfile string) *corev1.Pod {
	containerName := "build-and-push"
	containerImage := "quay.io/buildah/stable:v1.11.3"
	privileged := true

	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        imageSpecJob.ObjectMeta.Name,
			Namespace:   imageSpecJob.Namespace,
			Annotations: imageSpecJob.ObjectMeta.Annotations,
			Labels:      imageSpecJob.ObjectMeta.Labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    containerName,
					Image:   containerImage,
					Command: []string{"/bin/sh", "/scripts/build-and-push.sh"},
					Env: []corev1.EnvVar{
						{
							Name:  "DOCKERFILE",
							Value: dockerfile,
						},
						{
							Name:  "BASE_IMAGE",
							Value: imageSpecJob.Spec.BaseImage,
						},
						{
							Name:  "TARGET_IMAGE",
							Value: imageSpecJob.Spec.RepoPrefix + "/" + imageSpecJob.Spec.TargetImage,
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "varlibcontainers",
							MountPath: "/var/lib/containers",
						},
						{
							Name:      "scripts",
							MountPath: "/scripts",
							ReadOnly:  true,
						},
						{
							Name:      "push-secret",
							MountPath: "/push-secret",
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
					Name: "varlibcontainers",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: "scripts",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "primehub-controller-custom-image-scripts",
							},
						},
					},
				},
				{
					Name: "push-secret",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: imageSpecJob.Spec.PushSecret,
						},
					},
				},
			},
		},
	}

	if imageSpecJob.Spec.PullSecret != "" {
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name: "pull-secret",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: imageSpecJob.Spec.PullSecret,
				},
			},
		})
		pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
			Name:      "pull-secret",
			MountPath: "/pull-secret",
			ReadOnly:  true,
		})
	}

	return &pod
}
