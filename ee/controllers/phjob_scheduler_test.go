package controllers

import (
	phcache "primehub-controller/pkg/cache"
	"primehub-controller/pkg/quota"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	primehubv1alpha1 "primehub-controller/ee/api/v1alpha1"
	"primehub-controller/pkg/graphql"
)

func getConvertedResourceQuota(cpu float32, gpu float32, memory string) *quota.ResourceQuota {
	resourceQuota, _ := quota.ConvertToResourceQuota(cpu, gpu, memory)
	return resourceQuota
}

func createGroupInfo(name string, projectCpu float32, projectGpu float32, projectMemory string, userCpu float32, userGpu float32, userMemory string) *graphql.DtoGroup {
	return &graphql.DtoGroup{
		Name:               name,
		ProjectQuotaCpu:    projectCpu,
		ProjectQuotaGpu:    projectGpu,
		ProjectQuotaMemory: projectMemory,
		QuotaCpu:           userCpu,
		QuotaGpu:           userGpu,
		QuotaMemory:        userMemory,
	}
}

func createInstanceTypeInfo(name string, limitCpu float32, limitGpu int, limitMemory string) *graphql.DtoInstanceType {
	return &graphql.DtoInstanceType{
		Name: name,
		Spec: graphql.DtoInstanceTypeSpec{
			LimitsCpu:    limitCpu,
			LimitsMemory: limitMemory,
			LimitsGpu:    limitGpu,
		},
	}
}

func createPhJob(user string, group string, instanceType string) *primehubv1alpha1.PhJob {
	return &primehubv1alpha1.PhJob{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         "test",
			Name:              "test",
			CreationTimestamp: v1.Now(),
		},
		Spec: primehubv1alpha1.PhJobSpec{
			DisplayName:  "test",
			UserId:       user,
			UserName:     user,
			GroupId:      group,
			GroupName:    group,
			InstanceType: instanceType,
			Image:        "testImage",
			Command:      "echo 'test'",
		},
	}
}

func TestValidate(t *testing.T) {
	phJobScheduler := PHJobScheduler{}

	groupInfo := createGroupInfo("groupA", 3, -1, "", 5, -1, "")
	requestedQuota, _ := quota.ConvertToResourceQuota(4, 1, "1Gi")
	valid, _ := phJobScheduler.validate(requestedQuota, groupInfo)
	if valid != false {
		t.Error("Wrong Validate of Group Quota Test 1")
	}
	requestedQuota, _ = quota.ConvertToResourceQuota(3, 1, "1Gi")
	valid, _ = phJobScheduler.validate(requestedQuota, groupInfo)
	if valid != true {
		t.Error("Wrong Validate of Group Quota Test 2")
	}

	groupInfo = createGroupInfo("groupA", 5, -1, "", 3, -1, "")
	requestedQuota, _ = quota.ConvertToResourceQuota(4, 1, "1Gi")
	valid, _ = phJobScheduler.validate(requestedQuota, groupInfo)
	if valid != false {
		t.Error("Wrong Validate of User Quota Test 1")
	}
	requestedQuota, _ = quota.ConvertToResourceQuota(3, 1, "1Gi")
	valid, _ = phJobScheduler.validate(requestedQuota, groupInfo)
	if valid != true {
		t.Error("Wrong Validate of User Quota Test 2")
	}

	groupInfo = createGroupInfo("groupA", -1, -1, "0.5Gi", 3, -1, "")
	requestedQuota, _ = quota.ConvertToResourceQuota(1, 1, "1Gi")
	valid, _ = phJobScheduler.validate(requestedQuota, groupInfo)
	if valid != false {
		t.Error("Wrong Validate of Memory Quota Test 1")
	}
	groupInfo = createGroupInfo("groupA", -1, -1, "1Gi", 3, -1, "")
	requestedQuota, _ = quota.ConvertToResourceQuota(1, 1, "1Gi")
	valid, _ = phJobScheduler.validate(requestedQuota, groupInfo)
	if valid != true {
		t.Error("Wrong Validate of Memory Quota Test 2")
	}
}

func TestGroup(t *testing.T) {
	phJobScheduler := PHJobScheduler{}
	phJobList := primehubv1alpha1.PhJobList{}
	phJobs := []primehubv1alpha1.PhJob{}

	phJobs = append(phJobs, *createPhJob("userA", "groupA", "typeA"))
	phJobs = append(phJobs, *createPhJob("userB", "groupA", "typeA"))
	phJobs = append(phJobs, *createPhJob("userC", "groupB", "typeA"))
	phJobs = append(phJobs, *createPhJob("userD", "groupA", "typeA"))
	phJobs = append(phJobs, *createPhJob("userE", "groupB", "typeA"))

	phJobList.Items = phJobs

	phJobsByGroupRef, _ := phJobScheduler.group(&phJobList)
	phJobsByGroup := *phJobsByGroupRef
	if len(phJobsByGroup) != 2 {
		t.Error("Wrong Len of Group")
	}
	if len(phJobsByGroup["groupA"]) != 3 {
		t.Error("Wrong Len of groupA")
	}
	if len(phJobsByGroup["groupB"]) != 2 {
		t.Error("Wrong Len of groupB")
	}
}

func TestCreationTimestampSort(t *testing.T) {
	phJobScheduler := PHJobScheduler{}
	phJobs := []*primehubv1alpha1.PhJob{}

	phJobA := createPhJob("userA", "groupA", "typeA")
	phJobB := createPhJob("userB", "groupA", "typeA")
	phJobC := createPhJob("userC", "groupA", "typeA")

	phJobs = append(phJobs, phJobB)
	phJobs = append(phJobs, phJobC)
	phJobs = append(phJobs, phJobA)

	phJobScheduler.sort(&phJobs, compareByCreationTimestamp)

	if phJobs[0].Spec.UserName != "userA" || phJobs[1].Spec.UserName != "userB" || phJobs[2].Spec.UserName != "userC" {
		t.Error("Wrong Creation Timestamp Sort")
	}
}

func TestScheduleByStrictOrder1(t *testing.T) {
	phJobScheduler := PHJobScheduler{}
	phJobScheduler.PrimeHubCache = phcache.NewPrimeHubCache(nil)
	phJobs := []*primehubv1alpha1.PhJob{}

	phJobs = append(phJobs, createPhJob("userA", "groupA", "typeA"))
	phJobs = append(phJobs, createPhJob("userB", "groupA", "typeA"))
	phJobs = append(phJobs, createPhJob("userC", "groupA", "typeA"))

	usersRemainingQuota := make(map[string]quota.ResourceQuota)
	usersRemainingQuota["userA"] = *getConvertedResourceQuota(8, -1, "")
	usersRemainingQuota["userB"] = *getConvertedResourceQuota(8, -1, "")
	usersRemainingQuota["userC"] = *getConvertedResourceQuota(8, -1, "")

	groupRemainingQuota := getConvertedResourceQuota(8, -1, "")

	instanceTypeInfo := createInstanceTypeInfo("typeA", 4, 0, "")
	phJobScheduler.PrimeHubCache.InstanceType.Set("instanceType:typeA", instanceTypeInfo, time.Hour*1)

	phJobScheduler.scheduleByStrictOrder(&phJobs, &usersRemainingQuota, groupRemainingQuota)

	if phJobs[0].Status.Phase != primehubv1alpha1.JobPreparing || phJobs[1].Status.Phase != primehubv1alpha1.JobPreparing || phJobs[2].Status.Phase == primehubv1alpha1.JobPreparing {
		t.Error("Wrong Scheduling")
	}
}

func TestScheduleByStrictOrder2(t *testing.T) {
	phJobScheduler := PHJobScheduler{}
	phJobScheduler.PrimeHubCache = phcache.NewPrimeHubCache(nil)
	phJobs := []*primehubv1alpha1.PhJob{}

	phJobs = append(phJobs, createPhJob("userA", "groupA", "typeA"))
	phJobs = append(phJobs, createPhJob("userB", "groupA", "typeB"))
	phJobs = append(phJobs, createPhJob("userC", "groupA", "typeC"))

	usersRemainingQuota := make(map[string]quota.ResourceQuota)
	usersRemainingQuota["userA"] = *getConvertedResourceQuota(8, -1, "")
	usersRemainingQuota["userB"] = *getConvertedResourceQuota(8, -1, "")
	usersRemainingQuota["userC"] = *getConvertedResourceQuota(8, -1, "")

	groupRemainingQuota := getConvertedResourceQuota(8, -1, "")

	typeAInfo := createInstanceTypeInfo("typeA", 4, 0, "")
	phJobScheduler.PrimeHubCache.InstanceType.Set("instanceType:typeA", typeAInfo, time.Hour*1)
	typeBInfo := createInstanceTypeInfo("typeB", 6, 0, "")
	phJobScheduler.PrimeHubCache.InstanceType.Set("instanceType:typeB", typeBInfo, time.Hour*1)
	typeCInfo := createInstanceTypeInfo("typeB", 2, 0, "")
	phJobScheduler.PrimeHubCache.InstanceType.Set("instanceType:typeC", typeCInfo, time.Hour*1)

	phJobScheduler.scheduleByStrictOrder(&phJobs, &usersRemainingQuota, groupRemainingQuota)

	if phJobs[0].Status.Phase != primehubv1alpha1.JobPreparing || phJobs[1].Status.Phase == primehubv1alpha1.JobPreparing || phJobs[2].Status.Phase == primehubv1alpha1.JobPreparing {
		t.Error("Wrong Scheduling")
	}
}

func TestScheduleByStrictOrder3(t *testing.T) {
	phJobScheduler := PHJobScheduler{}
	phJobScheduler.PrimeHubCache = phcache.NewPrimeHubCache(nil)
	phJobs := []*primehubv1alpha1.PhJob{}

	phJobs = append(phJobs, createPhJob("userA", "groupA", "typeA"))
	phJobs = append(phJobs, createPhJob("userB", "groupA", "typeB"))
	phJobs = append(phJobs, createPhJob("userC", "groupA", "typeC"))

	usersRemainingQuota := make(map[string]quota.ResourceQuota)
	usersRemainingQuota["userA"] = *getConvertedResourceQuota(4, -1, "")
	usersRemainingQuota["userB"] = *getConvertedResourceQuota(4, -1, "")
	usersRemainingQuota["userC"] = *getConvertedResourceQuota(4, -1, "")

	groupRemainingQuota := getConvertedResourceQuota(8, -1, "")

	typeAInfo := createInstanceTypeInfo("typeA", 4, 0, "")
	phJobScheduler.PrimeHubCache.InstanceType.Set("instanceType:typeA", typeAInfo, time.Hour*1)
	typeBInfo := createInstanceTypeInfo("typeB", 6, 0, "")
	phJobScheduler.PrimeHubCache.InstanceType.Set("instanceType:typeB", typeBInfo, time.Hour*1)
	typeCInfo := createInstanceTypeInfo("typeB", 2, 0, "")
	phJobScheduler.PrimeHubCache.InstanceType.Set("instanceType:typeC", typeCInfo, time.Hour*1)

	phJobScheduler.scheduleByStrictOrder(&phJobs, &usersRemainingQuota, groupRemainingQuota)

	if phJobs[0].Status.Phase != primehubv1alpha1.JobPreparing || phJobs[1].Status.Phase == primehubv1alpha1.JobPreparing || phJobs[2].Status.Phase != primehubv1alpha1.JobPreparing {
		t.Error("Wrong Scheduling")
	}
}
