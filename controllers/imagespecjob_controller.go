package controllers

import (
	"bytes"
	"context"
	"fmt"
	"primehub-controller/api/v1alpha1"
	"primehub-controller/pkg/random"
	"regexp"
	"text/template"

	"github.com/go-logr/logr"
	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ImageSpecJobReconciler reconciles a ImageSpecJob object
type ImageSpecJobReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	EphemeralStorage resource.Quantity
}

const (
	CustomImageJobStatusSucceeded = "Succeeded"
	CustomImageJobStatusCancelled = "Cancelled"
	CustomImageJobStatusFailed    = "Failed"
)

// +kubebuilder:rbac:groups=primehub.io,resources=imagespecjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=primehub.io,resources=imagespecjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;delete

func (r *ImageSpecJobReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("imagespecjob", req.NamespacedName)

	var imageSpecJob v1alpha1.ImageSpecJob
	if err := r.Get(ctx, req.NamespacedName, &imageSpecJob); err != nil {
		if ignoreNotFound(err) != nil {
			log.Error(err, "unable to fetch ImageSpecJob")
		}
		return ctrl.Result{}, ignoreNotFound(err)
	}

	imageSpecJobClone := imageSpecJob.DeepCopy()

	if imageSpecJobClone.Status.Phase == CustomImageJobStatusSucceeded {
		return ctrl.Result{}, nil
	}

	var podName string
	if imageSpecJobClone.Status.PodName != "" {
		podName = imageSpecJobClone.Status.PodName
	} else {
		hash, err := random.GenerateRandomString(4)
		if err != nil {
			log.Error(err, "unable to generate random string for pod name")
			return ctrl.Result{}, err
		}
		podName = "image-" + imageSpecJobClone.ObjectMeta.Name + "-" + hash
		imageSpecJobClone.Status.PodName = podName
		if err := r.Status().Update(ctx, imageSpecJobClone); err != nil {
			log.Error(err, "failed to update ImageSpecJob status")
			return ctrl.Result{}, err
		}
	}

	pod := corev1.Pod{}
	err := r.Client.Get(ctx, client.ObjectKey{Namespace: imageSpecJob.Namespace, Name: podName}, &pod)
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
		pod = *r.buildPod(imageSpecJob, podName, dockerfile)
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

	imageSpecJobClone.Status.Phase = string(pod.Status.Phase)
	if pod.Status.StartTime != nil && !pod.Status.StartTime.Equal(imageSpecJobClone.Status.StartTime) {
		imageSpecJobClone.Status.StartTime = pod.Status.StartTime
		imageSpecJobClone.Status.FinishTime = nil
	}
	podStatuses := pod.Status.ContainerStatuses
	if len(podStatuses) > 0 && podStatuses[0].State.Terminated != nil && !podStatuses[0].State.Terminated.FinishedAt.IsZero() {
		containerFinishedAt := podStatuses[0].State.Terminated.FinishedAt
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
		For(&v1alpha1.ImageSpecJob{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}

func filterIllegalPackages(packages v1alpha1.ImageSpecSpecPackages) v1alpha1.ImageSpecSpecPackages {
	re := regexp.MustCompile(`^[a-zA-Z0-9-_:=\.]+$`)
	legalPackages := v1alpha1.ImageSpecSpecPackages{}
	for _, s := range packages.Apt {
		if re.MatchString(s) {
			legalPackages.Apt = append(legalPackages.Apt, s)
		}
	}
	for _, s := range packages.Conda {
		if re.MatchString(s) {
			legalPackages.Conda = append(legalPackages.Conda, s)
		}
	}
	for _, s := range packages.Pip {
		if re.MatchString(s) {
			legalPackages.Pip = append(legalPackages.Pip, s)
		}
	}
	return legalPackages
}

func generateDockerfile(imageSpecJob v1alpha1.ImageSpecJob) string {
	spec := imageSpecJob.Spec.DeepCopy()
	spec.Packages = filterIllegalPackages(spec.Packages)
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

{{ if .Packages.Conda }}
RUN conda install --quiet --yes \
      {{- range $index, $element := .Packages.Conda }}
      {{ $element }} \
      {{- end }}
      && \
    conda clean --all -f -y
{{ end }}

{{ if .Packages.Pip }}
RUN pip install --no-cache-dir
      {{- range $index, $element := .Packages.Pip }}
      {{- printf " %s" $element }}
      {{- end }}
{{ end }}`

	var res bytes.Buffer
	tmpl, _ := template.New("dockerfile").Parse(dockerfileTmpl)
	_ = tmpl.Execute(&res, spec)

	return res.String()
}

func (r *ImageSpecJobReconciler) buildPod(imageSpecJob v1alpha1.ImageSpecJob, podName string, dockerfile string) *corev1.Pod {
	containerName := "build-and-push"
	containerImage := fmt.Sprintf("%s:%s", viper.GetString("customImage.buildJob.image.repository"), viper.GetString("customImage.buildJob.image.tag"))
	privileged := true

	podResources := corev1.ResourceRequirements{}
	if len(viper.GetStringMap("customimage.buildjob.resources.requests")) > 0 {
		requests := corev1.ResourceList{}
		requests[corev1.ResourceCPU], _ = resource.ParseQuantity(viper.GetString("customImage.buildJob.resources.requests.cpu"))
		requests[corev1.ResourceMemory], _ = resource.ParseQuantity(viper.GetString("customImage.buildJob.resources.requests.memory"))
		podResources.Requests = requests
	}
	if len(viper.GetStringMap("customimage.buildjob.resources.limits")) > 0 {
		limits := corev1.ResourceList{}
		limits[corev1.ResourceCPU], _ = resource.ParseQuantity(viper.GetString("customImage.buildJob.resources.limits.cpu"))
		limits[corev1.ResourceMemory], _ = resource.ParseQuantity(viper.GetString("customImage.buildJob.resources.limits.memory"))
		podResources.Limits = limits
	}

	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        podName,
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
							Value: imageSpecJob.TargetImageURL(),
						},
						{
							Name:  "SKIP_TLS_VERIFY",
							Value: viper.GetString("customImage.insecureSkipVerify"),
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
						EmptyDir: &corev1.EmptyDirVolumeSource{
							SizeLimit: &r.EphemeralStorage,
						},
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

	pod.Spec.Containers[0].Resources = podResources

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
