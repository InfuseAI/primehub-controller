package graphql

import (
	"encoding/json"
	"io/ioutil"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	serial "k8s.io/apimachinery/pkg/runtime/serializer/json"
)

// Test basic behavior of group, image, instancetype.
func TestSpawnByData(t *testing.T) {
	jsonResult := `{"data":{"system":{"defaultUserVolumeCapacity":20},"user":{"id":"a4ca7d07-c69f-45e3-b99b-89f8643162b4","username":"phadmin","isAdmin":true,"volumeCapacity":null,"groups":[{"name":"everyone","displayName":"Global","enabledSharedVolume":false,"sharedVolumeCapacity":null,"homeSymlink":null,"launchGroupOnly":null,"quotaCpu":0,"quotaGpu":0,"quotaMemory":null,"userVolumeCapacity":null,"projectQuotaCpu":0,"projectQuotaGpu":0,"projectQuotaMemory":null,"instanceTypes":[{"name":"cpu-test","displayName":"cpu-test","description":"cpu-test","spec":{"description":"cpu-test","displayName":"cpu-test","limits.cpu":1,"limits.memory":"1G","limits.nvidia.com/gpu":0},"global":true},{"name":"cpu-test2","displayName":"cpu-test2","description":"cpu-test2","spec":{"description":"cpu-test2","displayName":"cpu-test2","limits.cpu":1,"limits.memory":"1G","limits.nvidia.com/gpu":0},"global":true}],"images":[],"datasets":[{"name":"test","displayName":"test","description":"test","spec":{"description":"test","displayName":"test","enableUploadServer":"true","type":"pv","url":"","variables":{},"volumeName":"test"},"global":true,"writable":false,"mountRoot":"/datasets","homeSymlink":false,"launchGroupOnly":true}]},{"name":"phusers","displayName":"auto generated by bootstrap","enabledSharedVolume":false,"sharedVolumeCapacity":null,"homeSymlink":null,"launchGroupOnly":null,"quotaCpu":null,"quotaGpu":0,"quotaMemory":null,"userVolumeCapacity":null,"projectQuotaCpu":null,"projectQuotaGpu":0,"projectQuotaMemory":null,"instanceTypes":[{"name":"cpu-only","displayName":"cpu-only","description":"auto generated by bootstrap","spec":{"description":"auto generated by bootstrap","displayName":"cpu-only","limits.cpu":1,"limits.memory":"1G","limits.nvidia.com/gpu":0,"requests.cpu":1,"requests.memory":"1G"},"global":false}],"images":[{"name":"base-notebook","displayName":"base-notebook","description":"auto generated by bootstrap","spec":{"description":"auto generated by bootstrap","displayName":"base-notebook","type":"both","url":"jupyter/base-notebook","urlForGpu":"jupyter/base-notebook"},"global":false}],"datasets":[]}]}}}`

	tests := []struct {
		name         string
		group        string
		instanceType string
		image        string
		success      bool
	}{
		{name: "normal", group: "phusers", instanceType: "cpu-only", image: "base-notebook", success: true},
		{name: "it-from-global", group: "phusers", instanceType: "cpu-test", image: "base-notebook", success: true},
		{name: "group-not-found", group: "phusers!", instanceType: "cpu-test", image: "base-notebook", success: false},
		{name: "it-not-found", group: "phusers", instanceType: "cpu-test!", image: "base-notebook", success: false},
		{name: "image-not-found", group: "phusers", instanceType: "cpu-test", image: "base-notebook!", success: false},
	}

	var result DtoResult
	json.Unmarshal([]byte(jsonResult), &result)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var pod corev1.Pod
			var spawner *Spawner
			var err error

			options := SpawnerDataOptions{}
			spawner, err = NewSpawnerByData(result.Data, test.group, test.instanceType, test.image, options)
			if test.success {
				assert.Nil(t, err, "NewSpawnerByData")

				spawner.WithCommand([]string{"echo", "helloworld"}).BuildPodSpec(&pod.Spec)
				serializer := serial.NewSerializerWithOptions(serial.DefaultMetaFactory, nil, nil, serial.SerializerOptions{})
				serializer.Encode(&pod, ioutil.Discard)

				assert.NotEmpty(t, pod.Spec.Containers[0].Image, "image not empty")
				assert.NotEmpty(t, pod.Spec.Containers[0].Resources.Limits, "reserouce limit not empty")
			} else {
				assert.NotNil(t, err, "NewSpawnerByData should fail")
			}
		})
	}
}

func TestGroupVolume(t *testing.T) {
	boolFalse := false

	// Test cases for pvc volumes.
	testsPvc := []struct {
		name string
		// input
		launchGroup string
		group       DtoGroup
		// output
		volumeName string
		pvcName    string
		mountPath  string
	}{
		{
			name:        "is launch group",
			launchGroup: "group1",
			group: DtoGroup{
				Name:                "group1",
				EnabledSharedVolume: true,
			},
			volumeName: "project-group1",
			pvcName:    "project-group1",
			mountPath:  "/project/group1",
		},
		{
			name:        "is launch group but no share",
			launchGroup: "group1",
			group: DtoGroup{
				Name:                "group1",
				EnabledSharedVolume: false,
			},
			volumeName: "",
		},
		{
			name:        "not launch only group",
			launchGroup: "group1",
			group: DtoGroup{
				Name:                "group2",
				EnabledSharedVolume: true,
			},
			volumeName: "",
		},
		{
			name:        "is launch group",
			launchGroup: "group1",
			group: DtoGroup{
				Name:                "group2",
				EnabledSharedVolume: true,
				LaunchGroupOnly:     &boolFalse,
			},
			volumeName: "project-group2",
			pvcName:    "project-group2",
			mountPath:  "/project/group2",
		},
	}

	for _, test := range testsPvc {
		t.Run(test.name, func(t *testing.T) {
			spawner := Spawner{}
			spawner.applyVolumeForGroup(test.launchGroup, test.group)

			volume, volumeMount := findVolume(&spawner, test.volumeName)

			if test.volumeName == "" {
				assert.Nil(t, volume)
				return
			}

			if !assert.NotNil(t, volume) {
				return
			}

			if !assert.NotNil(t, volumeMount) {
				return
			}

			assert.Equal(t, test.pvcName, volume.PersistentVolumeClaim.ClaimName)
			assert.Equal(t, test.mountPath, volumeMount.MountPath)
			assert.False(t, volumeMount.ReadOnly)

		})
	}
}

func TestDatasetMountedandPermission(t *testing.T) {
	newDataset := func(name string, global bool, writable bool) DtoDataset {
		return DtoDataset{
			Name:     name,
			Global:   global,
			Writable: writable,
			Spec: DtoDatasetSpec{
				EnableUploadServer: false,
				Type:               "pv",
				VolumeName:         name,
			},
		}
	}

	checkDatasetPermission := func(spawner *Spawner, dataset string, writable bool) bool {

		for _, volumeMount := range spawner.volumeMounts {
			if volumeMount.Name == "dataset-"+dataset && volumeMount.ReadOnly == !writable {
				return true
			}
		}
		return false
	}

	checkDatasetMounted := func(spawner *Spawner, dataset string) bool {

		for _, volume := range spawner.volumes {
			if volume.Name == "dataset-"+dataset {
				return true
			}
		}
		return false
	}

	// newDataset(name string, global bool, writable bool)
	groups := []DtoGroup{
		{
			Name: "group1",
			Datasets: []DtoDataset{
				newDataset("d1", false, true),
				newDataset("d2", false, true),
				newDataset("d3", true, true),
				newDataset("d4", true, true),
			},
		},
		{
			Name: "group2",
			Datasets: []DtoDataset{
				newDataset("d1", false, false),
				newDataset("d2", false, false),
			},
		},
		{
			Name:     "group3",
			Datasets: []DtoDataset{},
		},
		{
			Name: "everyone",
			Datasets: []DtoDataset{
				newDataset("d3", true, false),
				newDataset("d4", true, false),
				newDataset("d5", true, true), // global and writable, special case
			},
		},
	}

	t.Run("group1", func(t *testing.T) {
		spawner := Spawner{}
		spawner.applyDatasets(groups, "group1")

		assert.True(t, checkDatasetPermission(&spawner, "d1", true))
		assert.True(t, checkDatasetPermission(&spawner, "d2", true))
		assert.True(t, checkDatasetPermission(&spawner, "d3", true))
		assert.True(t, checkDatasetPermission(&spawner, "d4", true))
		assert.True(t, checkDatasetPermission(&spawner, "d5", true))
	})

	t.Run("group2", func(t *testing.T) {
		spawner := Spawner{}
		spawner.applyDatasets(groups, "group2")

		assert.True(t, checkDatasetPermission(&spawner, "d1", false))
		assert.True(t, checkDatasetPermission(&spawner, "d2", false))
		assert.True(t, checkDatasetPermission(&spawner, "d3", false))
		assert.True(t, checkDatasetPermission(&spawner, "d4", false))
		assert.True(t, checkDatasetPermission(&spawner, "d5", true))
	})

	t.Run("group3", func(t *testing.T) {
		spawner := Spawner{}
		spawner.applyDatasets(groups, "group3")

		//should not be mounted, global: false
		assert.True(t, !checkDatasetMounted(&spawner, "d1"))
		//should not be mounted, global: false
		assert.True(t, !checkDatasetMounted(&spawner, "d2"))
		//mounted since global true, but readOnly
		assert.True(t, checkDatasetPermission(&spawner, "d3", false))
		//mounted since global true, but readOnly
		assert.True(t, checkDatasetPermission(&spawner, "d4", false))
		//global and writable
		assert.True(t, checkDatasetPermission(&spawner, "d5", true))
	})

	t.Run("everyone", func(t *testing.T) {
		spawner := Spawner{}
		spawner.applyDatasets(groups, "everyone")

		//should not be mounted, global: fals
		assert.True(t, !checkDatasetMounted(&spawner, "d1"))
		//should not be mounted, global: false, lauchgrouponly: true
		assert.True(t, !checkDatasetMounted(&spawner, "d2"))
		//mounted since global true, but readOnly
		assert.True(t, checkDatasetPermission(&spawner, "d3", false))
		//mounted since global true, but readOnly
		assert.True(t, checkDatasetPermission(&spawner, "d4", false))
		//global and writable
		assert.True(t, checkDatasetPermission(&spawner, "d5", true))
	})
}

func TestDatasetPV(t *testing.T) {
	newDataset := func(name string, volumeName string, writable bool) DtoDataset {
		return DtoDataset{
			Name:     name,
			Writable: writable,
			Spec: DtoDatasetSpec{
				Type:       "pv",
				VolumeName: volumeName,
			},
		}
	}

	tests := []struct {
		name     string
		dataset  DtoDataset
		pvcName  string
		readOnly bool
		symlink  string
	}{
		{
			"ds1",
			newDataset("ds1", "ds1", false),
			"dataset-ds1",
			true,
			"ln -sf /mnt/dataset-ds1 /datasets/ds1",
		},
		{
			"ds2",
			newDataset("ds2", "ds2-xxx", true),
			"dataset-ds2-xxx",
			false,
			"ln -sf /mnt/dataset-ds2 /datasets/ds2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spawner := &Spawner{}
			spawner.applyVolumeForPvDataset(test.dataset, "dataset-"+test.name, "/mnt/dataset-"+test.name, "/datasets/"+test.name)

			volume, volumeMount := findVolume(spawner, "dataset-"+test.name)
			if volume == nil || volumeMount == nil {
				assert.Fail(t, "volume not found")
				return
			}

			if volume.PersistentVolumeClaim == nil {
				assert.Fail(t, "not pvc")
				return
			}

			assert.Equal(t, test.pvcName, volume.PersistentVolumeClaim.ClaimName)
			assert.True(t, test.readOnly == volumeMount.ReadOnly)
			assert.True(t, checkSymlink(spawner, test.symlink))
		})
	}
}

func TestDatasetGit(t *testing.T) {
	newDataset := func(name string, gitSyncHostRoot string) DtoDataset {
		return DtoDataset{
			Name: name,
			Spec: DtoDatasetSpec{
				Type:            "git",
				Url:             "git@github.com:kubernetes/kubernetes.git",
				GitSyncHostRoot: gitSyncHostRoot,
			},
		}
	}

	tests := []struct {
		name     string
		dataset  DtoDataset
		hostPath string
		symlink  string
	}{
		{
			"ds1",
			newDataset("ds1", ""),
			"/home/dataset/ds1",
			"ln -sf /mnt/dataset-ds1/ds1 /datasets/ds1",
		},
		{
			"ds2",
			newDataset("ds2", "/foo/bar"),
			"/foo/bar/ds2",
			"ln -sf /mnt/dataset-ds2/ds2 /datasets/ds2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spawner := &Spawner{}
			spawner.applyVolumeForGitDataset(test.dataset, "dataset-"+test.name, "/mnt/dataset-"+test.name, "/datasets/"+test.name)

			volume, volumeMount := findVolume(spawner, "dataset-"+test.name)
			if volume == nil || volumeMount == nil {
				assert.Fail(t, "volume not found")
				return
			}

			if volume.HostPath == nil {
				assert.Fail(t, "not hostpath")
				return
			}

			assert.Equal(t, test.hostPath, volume.HostPath.Path)
			assert.True(t, volumeMount.ReadOnly == true)
			assert.True(t, checkSymlink(spawner, test.symlink))
		})
	}
}

func TestDatasetEnv(t *testing.T) {

	dataset := DtoDataset{
		Name: "ds",
		Spec: DtoDatasetSpec{
			Type: "env",
			Variables: map[string]string{
				"foo": "bar",
			},
		},
	}

	spawner := &Spawner{}
	spawner.applyVolumeForEnvDataset(dataset)

	tests := []struct {
		key         string
		expectedKey string
		valid       bool
	}{
		{"foo", "TEST-ENV_FOO", true},
		{"foo-bar", "TEST-ENV_FOO-BAR", true},
		{"!", "", false},
	}

	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			actualKey, valid := transformEnvKey("test-ENV", test.key)

			assert.Equal(t, test.valid, valid)
			if valid {
				assert.Equal(t, test.expectedKey, actualKey)
			}
		})
	}
}

func findVolume(spawner *Spawner, name string) (*corev1.Volume, *corev1.VolumeMount) {
	var volume *corev1.Volume = nil
	var volumeMount *corev1.VolumeMount = nil

	for _, _volume := range spawner.volumes {
		if _volume.Name == name {
			volume = &_volume
			break
		}
	}

	for _, _volumeMount := range spawner.volumeMounts {
		if _volumeMount.Name == name {
			volumeMount = &_volumeMount
			break
		}
	}

	return volume, volumeMount
}

func checkSymlink(spawner *Spawner, symlink string) bool {
	for _, symlink2 := range spawner.symlinks {
		if symlink == symlink2 {
			return true
		}
	}
	return false
}
