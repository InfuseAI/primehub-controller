package graphql

import (
	"errors"

	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/api/resource"

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
	workingDirSize  resource.Quantity
}

func NewSpawnerByData(data DtoData, groupName string, instanceTypeName string, imageName string) (*Spawner, error) {
	var group DtoGroup
	var groupGlobal DtoGroup
	var image DtoImage
	var instanceType DtoInstanceType
	var err error
	spawner := &Spawner{}

	if groupName == "" {
		groupName = "everyone"
		//isGlobal = true
	}

	// Find the group
	if group, groupGlobal, err = findGroup(data.User.Groups, groupName); err != nil {
		return nil, err
	}

	// Group volume
	if group.EnabledSharedVolume {
		volume, volumeMount := volumeForGroup(groupName)
		spawner.volumes = append(spawner.volumes, volume)
		spawner.volumeMounts = append(spawner.volumeMounts, volumeMount)
	}

	// Instance type
	if instanceType, err = findInstanceType(group.InstanceTypes, groupGlobal.InstanceTypes, instanceTypeName); err != nil {
		return nil, err
	}
	isGpu := spawner.resourceForInstanceType(instanceType)

	// Image
	if image, err = findImage(group.Images, groupGlobal.Images, imageName); err != nil {
		return nil, err
	}
	spawner.image, spawner.imagePullSecret = imageForImageSpec(image.Spec, isGpu)

	// Dataset

	// Others
	spawner.workingDirSize = resource.MustParse(viper.GetString("jobSubmission.workingDirSize"))

	return spawner, nil
}

func (spawner *Spawner) WithCommand(command []string) *Spawner {
	spawner.command = command
	return spawner
}

func findGroup(groups []DtoGroup, groupName string) (DtoGroup, DtoGroup, error) {
	var groupTarget DtoGroup
	var groupGlobal DtoGroup
	found := false

	for _, group := range groups {
		if group.Name == groupName {
			groupTarget = group
			found = true
		}
		if group.Name == "everyone" {
			groupGlobal = group
		}
	}

	if !found {
		return DtoGroup{}, DtoGroup{}, errors.New("Group not found: " + groupName)
	} else {
		return groupTarget, groupGlobal, nil
	}
}

func findImage(images []DtoImage, imagesGlobal []DtoImage, imageName string) (DtoImage, error) {
	for _, image := range images {
		if image.Name == imageName {
			return image, nil
		}
	}

	for _, image := range imagesGlobal {
		if image.Name == imageName {
			return image, nil
		}
	}

	return DtoImage{}, errors.New("Image not found: " + imageName)
}

func findInstanceType(instanceTypes []DtoInstanceType, instanceTypesGlobal []DtoInstanceType, instanceTypeName string) (DtoInstanceType, error) {
	for _, instanceType := range instanceTypes {
		if instanceType.Name == instanceTypeName {
			return instanceType, nil
		}
	}

	for _, instanceType := range instanceTypesGlobal {
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

	if instanceType.Spec.RequestsCpu > 0 {
		spawner.requestsCpu.SetMilli(int64(instanceType.Spec.RequestsCpu * 1000))
	}

	if instanceType.Spec.LimitsCpu > 0 {
		spawner.limitsCpu.SetMilli(int64(instanceType.Spec.LimitsCpu * 1000))
	}

	if instanceType.Spec.RequestsGpu > 0 {
		spawner.requestsGpu.Set(int64(instanceType.Spec.RequestsGpu))
	}

	if instanceType.Spec.LimitsGpu > 0 {
		spawner.limitsGpu.Set(int64(instanceType.Spec.LimitsGpu))
	}

	if instanceType.Spec.RequestsMemory != "" {
		spawner.requestsMemory = resource.MustParse(instanceType.Spec.RequestsMemory)
	}

	if instanceType.Spec.LimitsMemory != "" {
		spawner.limitsMemory = resource.MustParse(instanceType.Spec.LimitsMemory)
	}

	return isGpu
}

func imageForImageSpec(spec DtoImageSpec, isGpu bool) (string, string) {
	return spec.Url, ""
}

func mountEmptyDir(podSpec *corev1.PodSpec, containers []*corev1.Container, name string, path string, emptyDirLimit resource.Quantity) {
	podSpec.Volumes = append(podSpec.Volumes,
		corev1.Volume{
			Name: name,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &emptyDirLimit},
			},
		})

	for _, container := range containers {
		container.VolumeMounts = append(container.VolumeMounts,
			corev1.VolumeMount{
				Name:      name,
				MountPath: path,
			})
	}
}

func (spawner *Spawner) BuildPodSpec(podSpec *corev1.PodSpec) {
	container := corev1.Container{}

	// container
	container.Name = "main"
	container.Image = spawner.image
	container.Command = spawner.command
	container.WorkingDir = "/workingdir"
	container.VolumeMounts = append(container.VolumeMounts, spawner.volumeMounts...)
	container.Resources.Requests = map[corev1.ResourceName]resource.Quantity{}
	container.Resources.Limits = map[corev1.ResourceName]resource.Quantity{}
	container.TerminationMessagePolicy = corev1.TerminationMessageFallbackToLogsOnError

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
		container.Resources.Limits["cpu"] = spawner.limitsCpu
	}

	if !spawner.limitsMemory.IsZero() {
		container.Resources.Limits["memory"] = spawner.limitsMemory
	}

	if !spawner.limitsGpu.IsZero() {
		container.Resources.Limits["nvidia.com/gpu"] = spawner.limitsGpu
	}

	// pod
	podSpec.Volumes = append(podSpec.Volumes, spawner.volumes...)
	mountEmptyDir(podSpec, []*corev1.Container{&container}, "workingdir", "/workingdir", spawner.workingDirSize)
	podSpec.Containers = append(podSpec.Containers, container)
	if spawner.imagePullSecret != "" {
		podSpec.ImagePullSecrets = append(podSpec.ImagePullSecrets, corev1.LocalObjectReference{
			Name: spawner.imagePullSecret,
		})
	}

}
