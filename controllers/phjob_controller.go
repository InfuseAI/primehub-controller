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
	"primehub-controller/pkg/graphql"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	primehubv1alpha1 "primehub-controller/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PhJobReconciler reconciles a PhJob object
type PhJobReconciler struct {
	client.Client
	Log           logr.Logger
	Scheme        *runtime.Scheme
	GraphqlClient *graphql.GraphqlClient
}

func (r *PhJobReconciler) buildPod(phJob *primehubv1alpha1.PhJob) (*corev1.Pod, error) {
	var err error

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "phjob-" + phJob.ObjectMeta.Name,
			Namespace:   phJob.Namespace,
			Annotations: phJob.ObjectMeta.Annotations,
			Labels:      phJob.ObjectMeta.Labels,
		},
	}

	// Fetch data from graphql
	podSpec := corev1.PodSpec{}
	var result *graphql.DtoResult
	if result, err = r.GraphqlClient.FetchByUserId(phJob.Spec.UserId); err != nil {
		return nil, err
	}

	// Build the podTemplate according to data from graphql and phjob group, instanceType, image settings
	var spawner *graphql.Spawner
	if spawner, err = graphql.NewSpawnerByData(result.Data, phJob.Spec.Group, phJob.Spec.InstanceType, phJob.Spec.Image); err != nil {
		return nil, err
	}
	spawner.WithCommand([]string{"sh", "-c", phJob.Spec.Command}).BuildPodSpec(&podSpec)
	podSpec.RestartPolicy = corev1.RestartPolicyNever
	pod.Spec = podSpec
	pod.Labels = map[string]string{
		"app": "primehub-job",
	}

	// Owner reference
	if err := ctrl.SetControllerReference(phJob, pod, r.Scheme); err != nil {
		r.Log.WithValues("phjob", phJob.Namespace).Error(err, "failed to set job's controller reference to phjob")
		return nil, err
	}

	return pod, nil
}

// +kubebuilder:rbac:groups=primehub.io,resources=phjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=primehub.io,resources=phjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=jobs,verbs=get;list;watch;create;update;delete

func (r *PhJobReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("phjob", req.NamespacedName)
	podkey := client.ObjectKey{
		Namespace: req.Namespace,
		Name:      "phjob-" + req.Name,
	}
	var err error

	log.Info("start Reconcile")

	phJob := &primehubv1alpha1.PhJob{}
	if err := r.Get(ctx, req.NamespacedName, phJob); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("PhJob deleted")
			return ctrl.Result{}, nil
		} else {
			log.Error(err, "unable to fetch PhJob")
			return ctrl.Result{}, nil
		}
	}

	pod := &corev1.Pod{}
	err = r.Client.Get(ctx, podkey, pod)
	if err != nil {

		if !apierrors.IsNotFound(err) {
			log.Error(err, "failed to get pod")
			return ctrl.Result{}, nil
		}

		if phJob.Status.Phase == "" || phJob.Status.Phase == primehubv1alpha1.JobPending {
			// create the pod
			phJob = phJob.DeepCopy()
			log.Info("could not find existing pod for PhJob, creating one...")
			pod, err = r.buildPod(phJob)
			if err == nil {
				err = r.Client.Create(ctx, pod)
			}

			// Update the phjob status
			if err == nil {
				// If not yet create pod and status phase is not ready, status phase is pending
				phJob.Status.Phase = primehubv1alpha1.JobReady
				phJob.Status.PodName = podkey.Name
				log.Info("create pod", "pod", pod)
			} else {
				phJob.Status.Phase = primehubv1alpha1.JobFailed
				phJob.Status.Reason = err.Error()
				log.Error(err, "failed to create pod")
			}

			if err = r.Status().Update(ctx, phJob); err != nil {
				log.Error(err, "failed to update PhJob status")
				return ctrl.Result{}, err
			}
		} else {
			log.Info("Pod not found but not necessary to create")
		}
	} else {
		log.Info("found pod resource for PhJob")

		phJob = phJob.DeepCopy()
		phJob.Status.StartTime = getStartTime(pod)

		if phJob.Spec.Cancel == true && phJob.Status.Phase != primehubv1alpha1.JobCancelled {
			gracePeriodSeconds := int64(0)
			deleteOptions := client.DeleteOptions{GracePeriodSeconds: &gracePeriodSeconds}
			if err := r.Client.Delete(ctx, pod, &deleteOptions); err != nil {
				log.Error(err, "failed to delete pod and cancel phjob")
				return ctrl.Result{}, err
			}

			phJob.Status.Phase = primehubv1alpha1.JobCancelled
			now := metav1.Now()
			phJob.Status.FinishTime = &now
		} else if phJob.Spec.Cancel != true {
			phJob.Status.Phase = convertJobPhase(pod)
			phJob.Status.FinishTime = getFinishTime(pod)

			if pod.Status.Phase == corev1.PodFailed && len(pod.Status.ContainerStatuses) > 0 {
				phJob.Status.Reason = pod.Status.ContainerStatuses[0].State.Terminated.Reason
				phJob.Status.Message = pod.Status.ContainerStatuses[0].State.Terminated.Message
			}
		}

		if err = r.Status().Update(ctx, phJob); err != nil {
			log.Error(err, "failed to update PhJob status")
			return ctrl.Result{}, err
		}
		log.Info("Update status from phjob")
	}

	return ctrl.Result{}, nil
}

func (r *PhJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&primehubv1alpha1.PhJob{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}

func convertJobPhase(pod *corev1.Pod) primehubv1alpha1.PhJobPhase {
	switch pod.Status.Phase {
	case corev1.PodPending:
		return primehubv1alpha1.JobReady
	case corev1.PodRunning:
		return primehubv1alpha1.JobRunning
	case corev1.PodSucceeded:
		return primehubv1alpha1.JobSucceeded
	case corev1.PodFailed:
		return primehubv1alpha1.JobFailed
	case corev1.PodUnknown:
		return primehubv1alpha1.JobUnknown
	default:
		return primehubv1alpha1.JobPending
	}
}

func getStartTime(pod *corev1.Pod) *metav1.Time {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == "main" {
			if cs.State.Running != nil {
				return cs.State.Running.StartedAt.DeepCopy()
			} else if cs.State.Terminated != nil {
				return cs.State.Terminated.StartedAt.DeepCopy()
			}
		}
	}

	return nil
}

func getFinishTime(pod *corev1.Pod) *metav1.Time {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == "main" {
			if cs.State.Terminated != nil {
				return cs.State.Terminated.FinishedAt.DeepCopy()
			}
		}
	}

	return nil
}
