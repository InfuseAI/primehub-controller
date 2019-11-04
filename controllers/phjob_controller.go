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

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PhJobReconciler reconciles a PhJob object
type PhJobReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
	GraphqlClient *graphql.GraphqlClient
}

func (r *PhJobReconciler) buildJob(phJob *primehubv1alpha1.PhJob) (*batchv1.Job, error) {
	var err error

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        phJob.ObjectMeta.Name,
			Namespace:   phJob.Namespace,
			Annotations: phJob.ObjectMeta.Annotations,
			Labels:      phJob.ObjectMeta.Labels,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{},
		},
	}

	var result *graphql.DtoResult
	if result, err = r.GraphqlClient.FetchByUser(phJob.Spec.UserId); err != nil {
		return nil, err
	}
	podSpec := corev1.PodSpec{}
	spawner := graphql.Spawner{}
	if err = spawner.WithData(result.Data, phJob.Spec.Group, phJob.Spec.InstanceType, phJob.Spec.Image); err != nil {
		return nil, err
	}
	spawner.BuildPodSpec(&podSpec)
	podSpec.RestartPolicy = corev1.RestartPolicyNever
	job.Spec.Template.Spec = podSpec


	if err := ctrl.SetControllerReference(phJob, job, r.Scheme); err != nil {
		r.Log.WithValues("phjob", phJob.Namespace).Error(err, "failed to set job's controller reference to phjob")
		return nil, err
	}

	return job, nil
}

// +kubebuilder:rbac:groups=primehub.io,resources=phjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=primehub.io,resources=phjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=jobs,verbs=get;list;watch;create;update;delete

func (r *PhJobReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("phjob", req.NamespacedName)

	log.Info("start Reconcile")

	phJob := &primehubv1alpha1.PhJob{}
	if err := r.Get(ctx, req.NamespacedName, phJob); err != nil {
		log.Error(err, "unable to fetch PhJob")
		return ctrl.Result{}, nil
	}

	if phJob.Spec.JobType == "Job" {
		job := &batchv1.Job{}
		err := r.Client.Get(ctx, client.ObjectKey{Namespace: phJob.Namespace, Name: phJob.ObjectMeta.Name}, job)
		if apierrors.IsNotFound(err) {
			// If not yet create job and status phase is not ready, status phase is pending
			if phJob.Status.Phase != primehubv1alpha1.JobReady && phJob.Status.Phase != primehubv1alpha1.JobPending {
				phJob.Status.Phase = primehubv1alpha1.JobPending
				if err := r.Status().Update(ctx, phJob); err != nil {
					log.Error(err, "failed to update PhJob status")
					return ctrl.Result{}, err
				}
				log.Info("updated PhJob status")
			}

			// If status phase is ready
			log.Info("could not find existing Job for PhJob, creating one...")

			job, err := r.buildJob(phJob)
			if err != nil {
				return ctrl.Result{}, err
			}
			if err := r.Client.Create(ctx, job); err != nil {
				log.Error(err, "failed to create Job resource")
				return ctrl.Result{}, err
			}

			log.Info("created Job resource for PhJob")

			return ctrl.Result{}, nil
		} else {
			log.Info("found Job resource for PhJob")
			_, finishedType := isJobFinished(job)
			switch finishedType {
			case "": // ongoing
				phJob.Status.Phase = primehubv1alpha1.JobRunning
			case batchv1.JobFailed:
				phJob.Status.Phase = primehubv1alpha1.JobFailed
			case batchv1.JobComplete:
				phJob.Status.Phase = primehubv1alpha1.JobSucceeded
			}

			if err := r.Status().Update(ctx, phJob); err != nil {
				log.Error(err, "failed to update PhJob status")
				return ctrl.Result{}, err
			}
			log.Info("updated PhJob status")
		}
	} else {
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func (r *PhJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&primehubv1alpha1.PhJob{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}

func isJobFinished(job *batchv1.Job) (bool, batchv1.JobConditionType) {
	for _, c := range job.Status.Conditions {
		if (c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) && c.Status == corev1.ConditionTrue {
			return true, c.Type
		}
	}

	return false, ""
}
