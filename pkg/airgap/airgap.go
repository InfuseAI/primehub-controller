package airgap

import (
	corev1 "k8s.io/api/core/v1"
	"strings"
)

func ApplyAirGapImagePrefix(podSpec *corev1.PodSpec, imagePrefix string) *corev1.PodSpec {
	initContainers := podSpec.InitContainers
	for i := range initContainers {
		PatchContainerImage(&initContainers[i], imagePrefix)
	}

	containers := podSpec.Containers
	for i := range containers {
		PatchContainerImage(&containers[i], imagePrefix)
	}

	return podSpec
}

func PatchContainerImage(container *corev1.Container, imagePrefix string) {
	if !strings.Contains(container.Image, imagePrefix) {
		container.Image = imagePrefix + container.Image
	}
}
