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
	Log logr.Logger
}

func buildJob(phJob primehubv1alpha1.PhJob) *batchv1.Job {
	job := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:            phJob.ObjectMeta.Name,
			Namespace:       phJob.Namespace,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(&phJob, primehubv1alpha1.GroupVersion.WithKind("PhJob"))},
			Annotations:     phJob.ObjectMeta.Annotations,
			Labels:          phJob.ObjectMeta.Labels,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    phJob.ObjectMeta.Name,
							Image:   "busybox",
							Command: []string{"sleep", "60"},
						},
					},
					RestartPolicy: "Never",
				},
			},
		},
	}

	return &job
}

// +kubebuilder:rbac:groups=primehub.io,resources=phjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=primehub.io,resources=phjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=jobs,verbs=get;list;watch;create;update;delete

func (r *PhJobReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("phjob", req.NamespacedName)

	var phJob primehubv1alpha1.PhJob
	if err := r.Get(ctx, req.NamespacedName, &phJob); err != nil {
		log.Error(err, "unable to fetch PhJob")
		return ctrl.Result{}, nil
	}

	if phJob.Spec.JobType == "Job" {
		job := batchv1.Job{}
		err := r.Client.Get(ctx, client.ObjectKey{Namespace: phJob.Namespace, Name: phJob.ObjectMeta.Name}, &job)
		if apierrors.IsNotFound(err) {
			log.Info("could not find existing Job for PhJob, creating one...")

			job = *buildJob(phJob)
			if err := r.Client.Create(ctx, &job); err != nil {
				log.Error(err, "failed to create Job resource")
				return ctrl.Result{}, err
			}

			log.Info("created Job resource for PhJob")
			return ctrl.Result{}, nil
		}
	} else {
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func (r *PhJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&primehubv1alpha1.PhJob{}).
		Complete(r)
}
