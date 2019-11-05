package graphql

import (
	"fmt"
	"k8s.io/apimachinery/pkg/api/resource"
)

import (
	"errors"
	corev1 "k8s.io/api/core/v1"
)

type Spawner struct {
	volumes         []corev1.Volume
	volumeMounts    []corev1.VolumeMount
	image           string
	imagePullSecret string
	command         []string
	requestsCpu     resource.Quantity
	limitsCpu       resource.Quantity
	requestsMemory  resource.Quantity
	limitsMemory    resource.Quantity
	requestsGpu     resource.Quantity
	limitsGpu       resource.Quantity
}

func NewSpawnerByData(data DtoData, groupName string, instanceTypeName string, imageName string) (*Spawner, error) {
	var group DtoGroup
	var image DtoImage
	var instanceType DtoInstanceType
	var err error
	spawner := &Spawner{}

	//isGlobal := false

	fmt.Println("user: " + data.User.Username)

	if groupName == "" {
		groupName = "everyone"
		//isGlobal = true
	}

	// Find the group
	if group, err = findGroup(data.User.Groups, groupName); err != nil {
		return nil, err
	}

	// Group volume
	if group.EnabledSharedVolume {
		volume, volumeMount := volumeForGroup(groupName)
		spawner.volumes = append(spawner.volumes, volume)
		spawner.volumeMounts = append(spawner.volumeMounts, volumeMount)
	}

	// Instance type
	if instanceType, err = findInstanceType(group.InstanceTypes, instanceTypeName); err != nil {
		return nil, err
	}
	isGpu := spawner.resourceForInstanceType(instanceType)

	// Image
	if image, err = findImage(group.Images, imageName); err != nil {
		return nil, err
	}
	spawner.image, spawner.imagePullSecret = imageForImageSpec(image.Spec, isGpu)

	// Dataset

	return spawner, nil
}

func (spawner *Spawner) WithCommand(command []string) *Spawner {
	spawner.command = command
	return spawner
}

func findGroup(groups []DtoGroup, groupName string) (DtoGroup, error) {
	for _, group := range groups {
		if group.Name == groupName {
			return group, nil
		}
	}

	return DtoGroup{}, errors.New("Group not found: " + groupName)
}

func findImage(images []DtoImage, imageName string) (DtoImage, error) {
	for _, image := range images {
		if image.Name == imageName {
			return image, nil
		}
	}

	return DtoImage{}, errors.New("Image not found: " + imageName)
}

func findInstanceType(instanceTypes []DtoInstanceType, instanceTypeName string) (DtoInstanceType, error) {
	for _, instanceType := range instanceTypes {
		if instanceType.Name == instanceTypeName {
			return instanceType, nil
		}
	}

	return DtoInstanceType{}, errors.New("InstanceType not found: " + instanceTypeName)
}

func volumeForGroup(project string) (corev1.Volume, corev1.VolumeMount) {
	name := "project-" + project
	path := "/project/" + project

	volume := corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: name,
			},
		},
	}

	volumeMount := corev1.VolumeMount{
		MountPath: path,
		Name:      name,
	}

	return volume, volumeMount
}

func (spawner *Spawner) resourceForInstanceType(instanceType DtoInstanceType) bool {
	isGpu := instanceType.Spec.RequestsCpu > 0

	spawner.requestsCpu.SetMilli(int64(instanceType.Spec.RequestsCpu * 1000))
	spawner.limitsCpu.SetMilli(int64(instanceType.Spec.LimitsCpu * 1000))

	spawner.requestsGpu.Set(int64(instanceType.Spec.RequestsGpu))
	spawner.limitsGpu.Set(int64(instanceType.Spec.LimitsGpu))

	spawner.requestsMemory = resource.MustParse(instanceType.Spec.RequestsMemory)
	spawner.limitsMemory = resource.MustParse(instanceType.Spec.LimitsMemory)

	return isGpu
}

func imageForImageSpec(spec DtoImageSpec, isGpu bool) (string, string) {
	return spec.Url, ""
}

func (spawner *Spawner) BuildPodSpec(podSpec *corev1.PodSpec) {
	container := corev1.Container{}

	// container
	container.Name = "main"
	container.Image = spawner.image
	container.Command = spawner.command
	container.VolumeMounts = append(container.VolumeMounts, spawner.volumeMounts...)
	container.Resources.Requests = map[corev1.ResourceName]resource.Quantity{}
	container.Resources.Limits = map[corev1.ResourceName]resource.Quantity{}

	if !spawner.requestsCpu.IsZero() {
		container.Resources.Requests["cpu"] = spawner.requestsCpu
	}

	if !spawner.requestsMemory.IsZero() {
		container.Resources.Requests["memory"] = spawner.requestsMemory
	}

	if !spawner.requestsGpu.IsZero() {
		container.Resources.Requests["nvidia.com/gpu"] = spawner.requestsGpu
	}

	if !spawner.limitsCpu.IsZero() {
		container.Resources.Requests["cpu"] = spawner.limitsCpu
	}

	if !spawner.limitsMemory.IsZero() {
		container.Resources.Requests["memory"] = spawner.limitsMemory
	}

	if !spawner.limitsGpu.IsZero() {
		container.Resources.Requests["nvidia.com/gpu"] = spawner.limitsGpu
	}

	// pod
	podSpec.Volumes = append(podSpec.Volumes, spawner.volumes...)
	podSpec.Containers = append(podSpec.Containers, container)
	if spawner.imagePullSecret != "" {
		podSpec.ImagePullSecrets = append(podSpec.ImagePullSecrets, corev1.LocalObjectReference{
			Name: spawner.imagePullSecret,
		})
	}

}
