package graphql

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type SpawnerDataOptions struct {
	WorkingDir     string
	WorkingDirSize resource.Quantity
}

// Represent the pod spawner.
// This class is supposed to generate the pod for phjob, jupypter, and all primehub-components pods.
// In these components, they share the same logic
// 1. group volume if any
// 2. dataset volumes
// 3. derived environemnt for env dataset
// 4. resource constraints from instancetype
//
// Spawner concept is from jupyterhub spawner.
// Reference: https://jupyterhub.readthedocs.io/en/stable/api/spawner.html#spawner
type Spawner struct {
	volumes         []corev1.Volume      // pod: volumes
	volumeMounts    []corev1.VolumeMount // main container: volume mounts
	env             []corev1.EnvVar
	workingDir      string   // main container: working directory
	symlinks        []string // main container: symbolic links commands
	image           string   // main container: image
	imagePullSecret string   // main container: imagePullSecret
	command         []string // main container: command

	NodeSelector map[string]string
	Tolerations  []corev1.Toleration
	Affinity     corev1.Affinity

	// main container: resources requests and limits for
	requestsCpu    resource.Quantity
	limitsCpu      resource.Quantity
	requestsMemory resource.Quantity
	limitsMemory   resource.Quantity
	requestsGpu    resource.Quantity
	limitsGpu      resource.Quantity
	containerName  string // main container: name
}

type ContainerResource struct {
	RequestsCpu    resource.Quantity
	LimitsCpu      resource.Quantity
	RequestsMemory resource.Quantity
	LimitsMemory   resource.Quantity
	RequestsGpu    resource.Quantity
	LimitsGpu      resource.Quantity
}

// Set spanwer by graphql response data
func NewSpawnerByData(data DtoData, groupName string, instanceTypeName string, imageName string, options SpawnerDataOptions) (*Spawner, error) {
	var group DtoGroup
	var groupGlobal DtoGroup
	var image DtoImage
	var instanceType DtoInstanceType

	var err error
	spawner := &Spawner{}

	workingDir := options.WorkingDir
	if workingDir == "" {
		workingDir = "/home/jovyan"
	}

	workingDirSize := options.WorkingDirSize

	if groupName == "" {
		groupName = "everyone"
	}

	// Find the group
	if group, groupGlobal, err = findGroup(data.User.Groups, groupName); err != nil {
		return nil, err
	}

	// Group volume
	for _, group := range data.User.Groups {
		spawner.applyVolumeForGroup(groupName, group)
	}
	if viper.GetString("jobSubmission.phfsEnabled") == "true" {
		spawner.applyVolumeForPhfs(groupName)
	}

	// Instance type
	if instanceType, err = findInstanceType(group.InstanceTypes, groupGlobal.InstanceTypes, instanceTypeName); err != nil {
		return nil, err
	}
	isGpu := spawner.applyResourceForInstanceType(instanceType)

	// Image
	if image, err = findImage(group.Images, groupGlobal.Images, imageName); err != nil {
		return nil, err
	}
	spawner.applyImageForImageSpec(image.Spec, isGpu)

	// Working Dir
	spawner.applyVolumeForWorkingDir(workingDir, workingDirSize)

	// Dataset
	spawner.applyDatasets(data.User.Groups, groupName)

	// User and Group env variables
	spawner.applyUserAndGroupEnv(data.User.Username, groupName)

	spawner.containerName = "main"

	return spawner, nil
}

func NewSpawnerForModelDeployment(data DtoData, groupName string, instanceTypeName string, imageUrl string, options SpawnerDataOptions) (*Spawner, error) {
	var group DtoGroup
	var groupGlobal DtoGroup
	var instanceType DtoInstanceType

	var err error
	spawner := &Spawner{}

	if groupName == "" {
		groupName = "everyone"
	}

	// Find the group
	if group, groupGlobal, err = findGroup(data.User.Groups, groupName); err != nil {
		return nil, err
	}

	// Instance type
	if instanceType, err = findInstanceType(group.InstanceTypes, groupGlobal.InstanceTypes, instanceTypeName); err != nil {
		return nil, err
	}

	spawner.applyResourceForInstanceType(instanceType)
	spawner.image = imageUrl

	spawner.containerName = "model"

	return spawner, nil
}

// ApplyNodeSelectorForOperator allow operator to set the nodeSelector to the pods it spawns.
func (spawner *Spawner) ApplyNodeSelectorForOperator(nodeSelector map[string]string) {
	if spawner.NodeSelector == nil {
		spawner.NodeSelector = make(map[string]string)
	}
	for k, v := range nodeSelector {
		spawner.NodeSelector[k] = v
	}
}

// ApplyTolerationsForOperator allow operator to set the tolerations to the pods it spawns.
func (spawner *Spawner) ApplyTolerationsForOperator(tolerations []corev1.Toleration) *Spawner {
	spawner.Tolerations = append(spawner.Tolerations, tolerations...)
	return spawner
}

// ApplyAffinityForOperator allow operator to set the Affinity to the pods it spawns.
func (spawner *Spawner) ApplyAffinityForOperator(affinity corev1.Affinity) *Spawner {
	spawner.Affinity = affinity
	return spawner
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

// add volume and volume mount if necessary.
//
// project volume is mounted while
// 1. shared volume is enabled
// 2. group is the launch group or
//    group setting `isLaunchGroupOnly` is disabled
//
// mount path: /projects/<group name>
//
// (if homeSymlink is enabled)
// homeSymlink: ./<group name> -> /projects/<group name>
//
func (spawner *Spawner) applyVolumeForGroup(launchGroup string, group DtoGroup) {
	if !group.EnabledSharedVolume {
		return
	}

	isLaunchGroupOnly := true
	if group.LaunchGroupOnly != nil {
		isLaunchGroupOnly = *group.LaunchGroupOnly
	}

	if group.Name != launchGroup && isLaunchGroupOnly {
		return
	}

	groupName := strings.ToLower(strings.ReplaceAll(group.Name, "_", "-"))
	name := "project-" + groupName
	mountPath := "/project/" + groupName
	homeSymlink := true
	if group.HomeSymlink != nil {
		homeSymlink = *group.HomeSymlink
	}

	volume := corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: name,
			},
		},
	}

	volumeMount := corev1.VolumeMount{
		MountPath: mountPath,
		Name:      name,
	}

	spawner.volumes = append(spawner.volumes, volume)
	spawner.volumeMounts = append(spawner.volumeMounts, volumeMount)
	if homeSymlink {
		spawner.symlinks = append(spawner.symlinks, fmt.Sprintf("ln -s %s .", mountPath))
	}
}

func (spawner *Spawner) applyVolumeForPhfs(groupName string) {
	groupName = strings.ToLower(strings.ReplaceAll(groupName, "_", "-"))

	pvcName := viper.GetString("jobSubmission.phfsPVC")
	if len(pvcName) > 0 {
		volume := corev1.Volume{
			Name: "phfs",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: pvcName,
				},
			},
		}

		volumeMount := corev1.VolumeMount{
			MountPath: "/phfs",
			Name:      "phfs",
			SubPath:   "groups/" + groupName,
		}

		spawner.volumes = append(spawner.volumes, volume)
		spawner.volumeMounts = append(spawner.volumeMounts, volumeMount)
		spawner.symlinks = append(spawner.symlinks, fmt.Sprintf("ln -s %s .", "/phfs"))
	}
}

func (spawner *Spawner) applyDatasets(groups []DtoGroup, launchGroupName string) {

	launchGroupDataset := make(map[string]DtoDataset)
	globalDataset := make(map[string]DtoDataset)
	allDataset := make(map[string]DtoDataset)

	for _, group := range groups {
		for _, dataset := range group.Datasets {
			if group.Name == launchGroupName { // collect lauch group dataset
				launchGroupDataset[dataset.Name] = dataset
			}
			if group.Name == "everyone" { // collect global dataset
				globalDataset[dataset.Name] = dataset
			}

			if _, ok := allDataset[dataset.Name]; !ok { // all dataset
				allDataset[dataset.Name] = dataset
			}
		}
	}

	for datasetName, dataset := range allDataset {
		flag := false
		dataset.Writable = false // decided by dataset in launch group or global
		if lgDataset, ok := launchGroupDataset[datasetName]; ok {
			dataset.Writable = lgDataset.Writable
			flag = true
		}
		if gDataset, ok := globalDataset[datasetName]; ok {
			dataset.Writable = dataset.Writable || gDataset.Writable
			flag = true
		}
		if flag == true {
			spawner.applyDataset(dataset)
		}
	}

	spawner.applyVolumeForDatasetDir()
}

// apply dataset setting to volumes and environment variables
//
// For all volumes
//
// mount point: 		/mnt/dataset-<name>
// dataset symlink:  	/datasets/<name> -> /mnt/dataset-<name> (pv)
// 									     -> /mnt/dataset-<name>/<name> (git)
// home symlink: 		./<name> -> /datasets/<name>
// all dataset symlink: ./datasets -> /datasets
//
// For envirnonment variable
//
// <key>:				<DATASET_NAME>_<KEY>=<value>
//
func (spawner *Spawner) applyDataset(dataset DtoDataset) {
	homeSymlink := true
	isVolume := false

	if dataset.HomeSymlink != nil {
		homeSymlink = *dataset.HomeSymlink
	}
	logicName := "dataset-" + dataset.Name
	mountPath := filepath.Join("/mnt/", logicName)
	datasetRoot := dataset.MountRoot
	if datasetRoot == "" {
		datasetRoot = "/datasets/"
	}
	datasetPath := filepath.Join(datasetRoot, dataset.Name)

	switch dataset.Spec.Type {
	case "pv":
		spawner.applyVolumeForPvDataset(dataset, logicName, mountPath, datasetPath)
		isVolume = true
	case "git":
		spawner.applyVolumeForGitDataset(dataset, logicName, mountPath, datasetPath)
		isVolume = true
	case "hostPath":
		spawner.applyVolumeForHostPathDataset(dataset, logicName, mountPath, datasetPath)
		isVolume = true
	case "nfs":
		spawner.applyVolumeForNfsDataset(dataset, logicName, mountPath, datasetPath)
		isVolume = true
	case "env":
		spawner.applyVolumeForEnvDataset(dataset)
	default:
		// invalid type
	}

	if isVolume && homeSymlink {
		spawner.symlinks = append(spawner.symlinks, fmt.Sprintf("ln -s %s .", datasetPath))
	}
}

func (spawner *Spawner) applyVolumeForPvDataset(
	dataset DtoDataset,
	logicName string,
	mountPath string,
	datasetPath string) {
	pvcName := "dataset-" + dataset.Spec.VolumeName
	writable := dataset.Writable

	var volume corev1.Volume

	// Deprecated
	matchedHostpath, err := regexp.MatchString(`^hostpath:`, dataset.Spec.VolumeName)
	if err == nil && matchedHostpath {
		re := regexp.MustCompile(`^hostpath:`)
		path := re.ReplaceAllString(dataset.Spec.VolumeName, "")
		volume = corev1.Volume{
			Name: logicName,
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: path,
				},
			},
		}
	} else {
		volume = corev1.Volume{
			Name: logicName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: pvcName,
				},
			},
		}
	}

	volumeMount := corev1.VolumeMount{
		MountPath: mountPath,
		Name:      logicName,
		ReadOnly:  !writable,
	}

	spawner.volumes = append(spawner.volumes, volume)
	spawner.volumeMounts = append(spawner.volumeMounts, volumeMount)
	spawner.symlinks = append(spawner.symlinks, fmt.Sprintf("ln -sf %s %s", mountPath, datasetPath))
}

func (spawner *Spawner) applyVolumeForGitDataset(
	dataset DtoDataset,
	logicName string,
	mountPath string,
	datasetPath string) {

	gitSyncHostRoot := dataset.Spec.GitSyncHostRoot
	if gitSyncHostRoot == "" {
		gitSyncHostRoot = "/home/dataset/"
	}

	volume := corev1.Volume{
		Name: logicName,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: filepath.Join(gitSyncHostRoot, dataset.Name),
			},
		},
	}

	volumeMount := corev1.VolumeMount{
		MountPath: mountPath,
		Name:      logicName,
		ReadOnly:  true,
	}

	spawner.volumes = append(spawner.volumes, volume)
	spawner.volumeMounts = append(spawner.volumeMounts, volumeMount)
	spawner.symlinks = append(spawner.symlinks, fmt.Sprintf("ln -sf %s %s", filepath.Join(mountPath, dataset.Name), datasetPath))
}

func (spawner *Spawner) applyVolumeForHostPathDataset(
	dataset DtoDataset,
	logicName string,
	mountPath string,
	datasetPath string) {
	writable := dataset.Writable

	if len(dataset.Spec.HostPath.Path) > 0 {
		volume := corev1.Volume{
			Name: logicName,
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: dataset.Spec.HostPath.Path,
				},
			},
		}
		volumeMount := corev1.VolumeMount{
			MountPath: mountPath,
			Name:      logicName,
			ReadOnly:  !writable,
		}

		spawner.volumes = append(spawner.volumes, volume)
		spawner.volumeMounts = append(spawner.volumeMounts, volumeMount)
		spawner.symlinks = append(spawner.symlinks, fmt.Sprintf("ln -sf %s %s", mountPath, datasetPath))
	}
}

func (spawner *Spawner) applyVolumeForNfsDataset(
	dataset DtoDataset,
	logicName string,
	mountPath string,
	datasetPath string) {
	writable := dataset.Writable

	if len(dataset.Spec.Nfs.Server) > 0 && len(dataset.Spec.Nfs.Path) > 0 {
		volume := corev1.Volume{
			Name: logicName,
			VolumeSource: corev1.VolumeSource{
				NFS: &corev1.NFSVolumeSource{
					Path:   dataset.Spec.Nfs.Path,
					Server: dataset.Spec.Nfs.Server,
				},
			},
		}
		volumeMount := corev1.VolumeMount{
			MountPath: mountPath,
			Name:      logicName,
			ReadOnly:  !writable,
		}

		spawner.volumes = append(spawner.volumes, volume)
		spawner.volumeMounts = append(spawner.volumeMounts, volumeMount)
		spawner.symlinks = append(spawner.symlinks, fmt.Sprintf("ln -sf %s %s", mountPath, datasetPath))
	}
}

func (spawner *Spawner) applyVolumeForEnvDataset(dataset DtoDataset) {
	for key, value := range dataset.Spec.Variables {
		envKey, valid := transformEnvKey(dataset.Name, key)
		if !valid {
			continue
		} else {
			spawner.env = append(spawner.env, corev1.EnvVar{
				Name:  envKey,
				Value: value,
			})
		}
	}
}

func transformEnvKey(prefix string, key string) (string, bool) {
	re := regexp.MustCompile("^[-._a-zA-Z][-._a-zA-Z0-9]*$")

	envKey := strings.ToUpper(prefix + "_" + key)
	if !re.MatchString(envKey) {
		return "", false
	}

	return envKey, true
}

func (spawner *Spawner) applyVolumeForDatasetDir() {
	sizeLimit, _ := resource.ParseQuantity("1M")

	name := "datasets"
	path := "/datasets"

	volume := corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &sizeLimit},
		},
	}

	volumeMount := corev1.VolumeMount{
		MountPath: path,
		Name:      name,
	}

	spawner.volumes = append(spawner.volumes, volume)
	spawner.volumeMounts = append(spawner.volumeMounts, volumeMount)
	spawner.symlinks = append(spawner.symlinks, "ln -sf /datasets .")

}

func (spawner *Spawner) applyVolumeForWorkingDir(workingDirPath string, workingDirSize resource.Quantity) {
	name := "workingdir"
	path := workingDirPath

	volume := corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &workingDirSize},
		},
	}

	volumeMount := corev1.VolumeMount{
		MountPath: path,
		Name:      name,
	}

	spawner.volumes = append(spawner.volumes, volume)
	spawner.volumeMounts = append(spawner.volumeMounts, volumeMount)
	spawner.workingDir = path
}

func (spawner *Spawner) applyResourceForInstanceType(instanceType DtoInstanceType) bool {
	isGpu := instanceType.Spec.LimitsGpu > 0

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
		quantity, e := resource.ParseQuantity(ConvertMemoryUnit(instanceType.Spec.RequestsMemory))
		if e == nil {
			spawner.requestsMemory = quantity
		}
	}

	if instanceType.Spec.LimitsMemory != "" {
		quantity, e := resource.ParseQuantity(ConvertMemoryUnit(instanceType.Spec.LimitsMemory))
		if e == nil {
			spawner.limitsMemory = quantity
		}
	}

	if spawner.NodeSelector == nil {
		spawner.NodeSelector = make(map[string]string)
	}
	for k, v := range instanceType.Spec.NodeSelector {
		spawner.NodeSelector[k] = v
	}
	spawner.Tolerations = append(spawner.Tolerations, instanceType.Spec.Tolerations...)

	return isGpu
}

func (spawner *Spawner) applyImageForImageSpec(spec DtoImageSpec, isGpu bool) {
	if isGpu {
		spawner.image = spec.UrlForGpu
	} else {
		spawner.image = spec.Url
	}

	spawner.imagePullSecret = spec.PullSecret
}

func (spawner *Spawner) applyUserAndGroupEnv(userName string, groupName string) {
	user := corev1.EnvVar{
		Name:  "PRIMEHUB_USER",
		Value: userName,
	}

	group := corev1.EnvVar{
		Name:  "PRIMEHUB_GROUP",
		Value: groupName,
	}

	spawner.env = append(spawner.env, user, group)
}

func (spawner *Spawner) BuildPodSpec(podSpec *corev1.PodSpec) {
	// container
	container := corev1.Container{}
	container.Name = spawner.containerName
	container.Image = spawner.image
	container.Command = spawner.command
	container.WorkingDir = spawner.workingDir
	container.VolumeMounts = append(container.VolumeMounts, spawner.volumeMounts...)
	container.Env = append(container.Env, spawner.env...)
	container.Resources.Requests = map[corev1.ResourceName]resource.Quantity{}
	container.Resources.Limits = map[corev1.ResourceName]resource.Quantity{}
	container.TerminationMessagePolicy = corev1.TerminationMessageFallbackToLogsOnError
	container.ImagePullPolicy = corev1.PullIfNotPresent

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

	if spawner.symlinks != nil {
		container.Lifecycle = &corev1.Lifecycle{
			PostStart: &corev1.Handler{
				Exec: &corev1.ExecAction{
					Command: []string{
						"sh",
						"-c",
						strings.Join(spawner.symlinks, ";"),
					},
				},
			},
		}
	}

	// pod
	podSpec.Volumes = append(podSpec.Volumes, spawner.volumes...)
	podSpec.Containers = append(podSpec.Containers, container)

	if spawner.imagePullSecret != "" {
		podSpec.ImagePullSecrets = append(podSpec.ImagePullSecrets, corev1.LocalObjectReference{
			Name: spawner.imagePullSecret,
		})
	}

	if podSpec.NodeSelector == nil {
		podSpec.NodeSelector = make(map[string]string)
	}
	for k, v := range spawner.NodeSelector {
		podSpec.NodeSelector[k] = v
	}
	podSpec.Tolerations = append(podSpec.Tolerations, spawner.Tolerations...)
	podSpec.Affinity = &spawner.Affinity
}

func ConvertMemoryUnit(value string) string {
	if value == "" {
		return ""
	}

	if matched, err := regexp.MatchString("i$", value); err == nil && matched {
		return value
	}

	if matched, err := regexp.MatchString("[GMK]$", value); err == nil && matched {
		return value + "i"
	}

	return value
}

func (spawner *Spawner) GetContainerResource() ContainerResource {
	return ContainerResource{
		RequestsCpu:    spawner.requestsCpu,
		LimitsCpu:      spawner.limitsCpu,
		RequestsMemory: spawner.requestsMemory,
		LimitsMemory:   spawner.limitsMemory,
		RequestsGpu:    spawner.requestsGpu,
		LimitsGpu:      spawner.limitsGpu,
	}
}
