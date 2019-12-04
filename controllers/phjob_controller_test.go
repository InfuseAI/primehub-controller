package controllers

import (
	"errors"
	primehubv1alpha1 "primehub-controller/api/v1alpha1"
	"testing"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

func TestHandleCreatePodFailedWithAdmission(t *testing.T) {
	phJob := &primehubv1alpha1.PhJob{}
	pod := &corev1.Pod{}
	phJob.Status.Phase = primehubv1alpha1.JobReady
	phJob.Status.Requeued = new(int32)
	*phJob.Status.Requeued = int32(0)

	r := PhJobReconciler{
		Log: ctrl.Log.WithName("controllers").WithName("PhJob"),
	}

	err := errors.New("Internal error occurred: admission webhook \"resources-validation-webhook.primehub.io\" denied the request: Group phusers exceeded cpu quota: 1, requesting 4.0")
	r.handleCreatePodFailed(phJob, pod, err)

	if phJob.Status.Phase != primehubv1alpha1.JobPending {
		t.Error("should be pending")
	}
	if *phJob.Status.Requeued != 1 {
		t.Error("Requeued num should be 1")
	}
}

func TestHandleCreatePodFailedWithoutAdmission(t *testing.T) {
	phJob := &primehubv1alpha1.PhJob{}
	pod := &corev1.Pod{}
	phJob.Status.Phase = primehubv1alpha1.JobReady
	phJob.Status.Requeued = new(int32)
	*phJob.Status.Requeued = int32(0)

	r := PhJobReconciler{
		Log: ctrl.Log.WithName("controllers").WithName("PhJob"),
	}

	err := errors.New("Something wrong")
	r.handleCreatePodFailed(phJob, pod, err)

	if phJob.Status.Phase != primehubv1alpha1.JobFailed {
		t.Error("should be failed")
	}
	if *phJob.Status.Requeued != 0 {
		t.Error("Requeued num should be 0")
	}
}
