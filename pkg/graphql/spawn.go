package graphql

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type SpawnerOptions struct {
	WorkingDir               string
	WorkingDirSize           resource.Quantity
	PhfsEnabled              bool
	PhfsPVC                  string
	ArtifactEnabled          bool
	ArtifactLimitSizeMb      int32
	ArtifactLimitFiles       int32
	ArtifactRetentionSeconds int32
	GrantSudo                bool
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
	prependEnv      []corev1.EnvVar
	workingDir      string   // main container: working directory
	symlinks        []string // main container: symbolic links commands
	image           string   // main container: image
	imagePullSecret string   // main container: imagePullSecret
	command         []string // main container: command
	args            []string // main container: args

	PodSecurityContext corev1.PodSecurityContext
	NodeSelector       map[string]string
	Tolerations        []corev1.Toleration
	Affinity           corev1.Affinity

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
func NewSpawnerForJob(data DtoData, groupName string, instanceTypeName string, imageName string, options SpawnerOptions) (*Spawner, error) {
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
		spawner.applyVolumeForGroup(groupName, group, workingDir)
	}
	if options.PhfsEnabled {
		spawner.applyVolumeForPhfs(groupName, options.PhfsPVC, workingDir)
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
	spawner.applyEmptyDirVolume("workingdir", workingDir, workingDirSize)
	spawner.workingDir = workingDir

	// Dataset
	spawner.applyDatasets(data.User.Groups, groupName, workingDir)

	// User and Group env variables
	spawner.applyUserAndGroupEnv(data.User.Username, groupName)

	// Mount `scripts`
	spawner.applyScripts("primehub-controller-job-scripts")

	// Apply artifacts relative
	if options.ArtifactEnabled && options.PhfsEnabled {
		spawner.applyJobArtifact(options.ArtifactLimitSizeMb, options.ArtifactLimitFiles, options.ArtifactRetentionSeconds)
	}

	// Apply grant sudo
	spawner.applyGrantSudo(options.GrantSudo)

	spawner.containerName = "main"

	return spawner, nil
}

func NewSpawnerForModelDeployment(data DtoData, groupName string, instanceTypeName string, imageUrl string) (*Spawner, error) {
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

func NewSpawnerForPhApplication(appID string, group DtoGroup, instanceType DtoInstanceType, globalDatasets []DtoDataset, spec corev1.PodSpec, options SpawnerOptions) (*Spawner, error) {
	//var err error
	spawner := &Spawner{}
	var primeHubAppRoot string

	// Apply Group volume
	if group.EnabledSharedVolume {
		primeHubAppRoot = "/project/" + strings.ToLower(strings.ReplaceAll(group.Name, "_", "-")) + "/phapplications/" + appID
		spawner.applyVolumeForGroup(group.Name, group, "")
	} else {
		primeHubAppRoot = "/phapplications/" + appID
		spawner.applyEmptyDirVolume("primehubAppRoot", primeHubAppRoot, resource.MustParse("5Gi"))
	}

	// Apply PHFS volume
	if options.PhfsEnabled == true {
		spawner.applyVolumeForPhfs(group.Name, options.PhfsPVC, primeHubAppRoot)
	}

	// Apply Dataset
	datasets := spawner.mergeDataset(group.Datasets, globalDatasets...)
	for _, d := range datasets {
		spawner.applyDataset(d)
	}
	spawner.applyVolumeForDatasetDir(primeHubAppRoot)

	// Apply Resource
	spawner.applyResourceForInstanceType(instanceType)

	// Apply Node Selector
	spawner.ApplyNodeSelectorForOperator(instanceType.Spec.NodeSelector)

	// Apply Toleration
	spawner.ApplyTolerationsForOperator(instanceType.Spec.Tolerations)

	// Apply Container
	container := spec.Containers[0] // According the spec, we only keep the first container
	spawner.containerName = container.Name
	spawner.image = container.Image
	spawner.command = append([]string{"/scripts/run-app"}, container.Command...)
	spawner.args = container.Args

	// Mount `scripts`
	spawner.applyScripts("primehub-controller-phapplication-scripts")

	// Prepend Env
	spawner.prependEnv = append(spawner.env, []corev1.EnvVar{
		{
			Name:  "PRIMEHUB_APP_ID",
			Value: appID,
		},
		{
			Name:  "PRIMEHUB_APP_ROOT",
			Value: primeHubAppRoot,
		},
		{
			Name:  "PRIMEHUB_APP_BASE_URL",
			Value: "/console/apps/" + appID,
		}}...)

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
func (spawner *Spawner) applyVolumeForGroup(launchGroup string, group DtoGroup, workingDir string) {
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
	if homeSymlink && workingDir != "" {
		spawner.symlinks = append(spawner.symlinks, fmt.Sprintf("ln -s %s %s", mountPath, workingDir))
	}
}

func (spawner *Spawner) applyVolumeForPhfs(groupName string, pvcName string, workingDir string) {
	groupName = strings.ToLower(strings.ReplaceAll(groupName, "_", "-"))

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
		spawner.symlinks = append(spawner.symlinks, fmt.Sprintf("ln -s %s %s", "/phfs", workingDir))
	}
}

func (spawner *Spawner) applyDatasets(groups []DtoGroup, launchGroupName string, workingDir string) {

	launchGroupDataset := make(map[string]DtoDataset)
	globalDataset := make(map[string]DtoDataset)
	allDataset := make(map[string]DtoDataset)

	for _, group := range groups {
		for _, dataset := range group.Datasets {
			if group.Name == launchGroupName { // collect launch group dataset
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

	spawner.applyVolumeForDatasetDir(workingDir)
}

func (spawner *Spawner) mergeDataset(slice []DtoDataset, elems ...DtoDataset) []DtoDataset {
	var mergedDatasets []DtoDataset
	datasets := map[string]DtoDataset{}
	tempSlice := append(slice, elems...)

	for _, s := range tempSlice {
		if _, ok := datasets[s.Name]; ok {
			if s.Writable == true {
				datasets[s.Name] = s
			}
		} else {
			datasets[s.Name] = s
		}
	}

	for _, v := range datasets {
		mergedDatasets = append(mergedDatasets, v)
	}

	// Sort the datasets list
	sort.SliceStable(mergedDatasets, func(i, j int) bool {
		return mergedDatasets[i].Name > mergedDatasets[j].Name
	})
	return mergedDatasets
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
			// For backward compatibility in env
			spawner.env = append(spawner.env, corev1.EnvVar{
				Name:  envKey,
				Value: value,
			})
			// Actually, hyphen is not supported in bash
			spawner.env = append(spawner.env, corev1.EnvVar{
				Name:  strings.ReplaceAll(envKey, "-", "_"),
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

func (spawner *Spawner) applyVolumeForDatasetDir(workingDir string) {
	sizeLimit, _ := resource.ParseQuantity("1M")

	name := "datasets"
	mountPath := "/datasets"
	symlinkPath := path.Join(workingDir, name)

	spawner.applyEmptyDirVolume(name, mountPath, sizeLimit)
	spawner.symlinks = append(spawner.symlinks, fmt.Sprintf("test -d %s || ln -sf %s %s", symlinkPath, mountPath, workingDir))
}

func (spawner *Spawner) applyEmptyDirVolume(name string, path string, size resource.Quantity) {
	volume := corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &size},
		},
	}

	volumeMount := corev1.VolumeMount{
		MountPath: path,
		Name:      name,
	}

	spawner.volumes = append(spawner.volumes, volume)
	spawner.volumeMounts = append(spawner.volumeMounts, volumeMount)
}

func (spawner *Spawner) applyResourceForInstanceType(instanceType DtoInstanceType) bool {
	isGpu := instanceType.Spec.LimitsGpu > 0

	if instanceType.Spec.RequestsCpu > 0 {
		spawner.requestsCpu.SetMilli(int64(instanceType.Spec.RequestsCpu * 1000))
		spawner.requestsCpu.Format = resource.DecimalSI
	}

	if instanceType.Spec.LimitsCpu > 0 {
		spawner.limitsCpu.SetMilli(int64(instanceType.Spec.LimitsCpu * 1000))
		spawner.limitsCpu.Format = resource.DecimalSI
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
			spawner.requestsMemory.Format = resource.DecimalSI
		}
	}

	if instanceType.Spec.LimitsMemory != "" {
		quantity, e := resource.ParseQuantity(ConvertMemoryUnit(instanceType.Spec.LimitsMemory))
		if e == nil {
			spawner.limitsMemory = quantity
			spawner.limitsMemory.Format = resource.DecimalSI
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

func (spawner *Spawner) applyScripts(name string) {
	volumeName := "scripts"
	defaultMode := int32(0777)
	volume := corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: name,
				},
				DefaultMode: &defaultMode,
			},
		},
	}

	volumeMount := corev1.VolumeMount{
		MountPath: "/scripts",
		Name:      volumeName,
	}

	spawner.volumes = append(spawner.volumes, volume)
	spawner.volumeMounts = append(spawner.volumeMounts, volumeMount)
}

func (spawner *Spawner) applyJobArtifact(limitSizeMb int32, limitFiles int32, retentionSeconds int32) {
	// Environments
	name := corev1.EnvVar{
		Name: "PHJOB_NAME",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{
				FieldPath: "metadata.name",
			},
		},
	}
	artifact := corev1.EnvVar{
		Name:  "PHJOB_ARTIFACT_ENABLED",
		Value: strconv.FormatBool(true),
	}
	spawner.env = append(spawner.env, name, artifact)

	if limitSizeMb > 0 {
		spawner.env = append(spawner.env, corev1.EnvVar{
			Name:  "PHJOB_ARTIFACT_LIMIT_SIZE_MB",
			Value: fmt.Sprint(limitSizeMb),
		})
	}

	if limitFiles > 0 {
		spawner.env = append(spawner.env, corev1.EnvVar{
			Name:  "PHJOB_ARTIFACT_LIMIT_FILES",
			Value: fmt.Sprint(limitFiles),
		})
	}

	spawner.env = append(spawner.env, corev1.EnvVar{
		Name:  "PHJOB_ARTIFACT_RETENTION_SECONDS",
		Value: fmt.Sprint(retentionSeconds),
	})
}

func (spawner *Spawner) applyGrantSudo(grantSudo bool) {
	securityContext := corev1.PodSecurityContext{}
	if grantSudo {
		runAsUser := int64(0)
		securityContext.RunAsUser = &runAsUser
	}
	spawner.env = append(spawner.env, corev1.EnvVar{
		Name:  "GRANT_SUDO",
		Value: strconv.FormatBool(grantSudo),
	})
	spawner.PodSecurityContext = securityContext
}

func (spawner *Spawner) PatchPodSpec(podSpec *corev1.PodSpec) {
	var container corev1.Container
	if len(podSpec.Containers) == 0 {
		spawner.BuildPodSpec(podSpec)
		return
	}

	container = podSpec.Containers[0]
	container.TerminationMessagePolicy = corev1.TerminationMessageFallbackToLogsOnError
	container.ImagePullPolicy = corev1.PullIfNotPresent
	container.VolumeMounts = append(container.VolumeMounts, spawner.volumeMounts...)

	if spawner.containerName != "" {
		container.Name = spawner.containerName
	}
	if spawner.image != "" {
		container.Image = spawner.image
	}
	if len(spawner.command) > 0 {
		container.Command = spawner.command
	}
	if spawner.workingDir != "" {
		container.WorkingDir = spawner.workingDir
	}
	if len(spawner.env) > 0 {
		container.Env = append(container.Env, spawner.env...)
	}
	if len(spawner.prependEnv) > 0 {
		container.Env = append(spawner.prependEnv, container.Env...)
	}

	// Resource Limit
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

	// Pod
	podSpec.Containers = []corev1.Container{container}
	podSpec.Volumes = append(podSpec.Volumes, spawner.volumes...)
	if spawner.imagePullSecret != "" {
		podSpec.ImagePullSecrets = append(podSpec.ImagePullSecrets, corev1.LocalObjectReference{
			Name: spawner.imagePullSecret,
		})
	}
	if podSpec.SecurityContext == nil {
		podSpec.SecurityContext = &spawner.PodSecurityContext
	}
	if podSpec.Affinity == nil {
		podSpec.Affinity = &spawner.Affinity
	}
	if podSpec.NodeSelector == nil {
		podSpec.NodeSelector = make(map[string]string)
	}
	for k, v := range spawner.NodeSelector {
		podSpec.NodeSelector[k] = v
	}
	podSpec.Tolerations = append(podSpec.Tolerations, spawner.Tolerations...)
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
	podSpec.SecurityContext = &spawner.PodSecurityContext
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
