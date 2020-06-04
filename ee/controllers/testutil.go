package controllers

import (
	corev1 "k8s.io/api/core/v1"
)

func NewFailedPodList(isImageError bool, isTerminated bool, isUnschedulable bool) []FailedPodStatus {
	failedPods := make([]FailedPodStatus, 0)

	if isImageError {
		result := FailedPodStatus{
			pod:               "ImageErrorPod",
			conditions:        make([]corev1.PodCondition, 0),
			containerStatuses: make([]corev1.ContainerStatus, 0),
			isImageError:      true,
			isTerminated:      false,
			isUnschedulable:   false,
		}
		failedPods = append(failedPods, result)
	}

	if isTerminated {
		result := FailedPodStatus{
			pod:               "PodTerminatedError",
			conditions:        make([]corev1.PodCondition, 0),
			containerStatuses: make([]corev1.ContainerStatus, 0),
			isImageError:      false,
			isTerminated:      true,
			isUnschedulable:   false,
		}
		failedPods = append(failedPods, result)
	}

	if isUnschedulable {
		result := FailedPodStatus{
			pod:               "PodUnschedulable",
			conditions:        make([]corev1.PodCondition, 0),
			containerStatuses: make([]corev1.ContainerStatus, 0),
			isImageError:      false,
			isTerminated:      false,
			isUnschedulable:   true,
		}
		failedPods = append(failedPods, result)
	}
	return failedPods
}
