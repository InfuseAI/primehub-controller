package controllers

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	primehubv1alpha1 "primehub-controller/api/v1alpha1"
	"primehub-controller/pkg/graphql"
)

func TestScheduleByStrictOrder(t *testing.T) {
	phJobScheduler := PHJobScheduler{}
	phJobs := []*primehubv1alpha1.PhJob{}

	phJobs = append(phJobs, &primehubv1alpha1.PhJob{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
			Name:      "test",
		},
		Spec: primehubv1alpha1.PhJobSpec{
			DisplayName:  "test",
			UserId:       "testUserA",
			UserName:     "testUserA",
			GroupId:      "testGroup",
			GroupName:    "testGroup",
			InstanceType: "testInstanceType",
			Image:        "testImage",
			Command:      "echo 'test'",
		},
	})

	phJobs = append(phJobs, &primehubv1alpha1.PhJob{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
			Name:      "test",
		},
		Spec: primehubv1alpha1.PhJobSpec{
			DisplayName:  "test",
			UserId:       "testUserB",
			UserName:     "testUserB",
			GroupId:      "testGroup",
			GroupName:    "testGroup",
			InstanceType: "testInstanceType",
			Image:        "testImage",
			Command:      "echo 'test'",
		},
	})

	phJobs = append(phJobs, &primehubv1alpha1.PhJob{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
			Name:      "test",
		},
		Spec: primehubv1alpha1.PhJobSpec{
			DisplayName:  "test",
			UserId:       "testUserC",
			UserName:     "testUserC",
			GroupId:      "testGroup",
			GroupName:    "testGroup",
			InstanceType: "testInstanceType",
			Image:        "testImage",
			Command:      "echo 'test'",
		},
	})

	usersRemainingQuota := make(map[string]ResourceQuota)
	userQuota := NewResourceQuota()
	userQuota.cpu = resource.NewQuantity(8, resource.DecimalSI)
	userQuota.gpu = nil
	userQuota.memory = nil
	usersRemainingQuota["testUserA"] = *userQuota
	usersRemainingQuota["testUserB"] = *userQuota
	usersRemainingQuota["testUserC"] = *userQuota

	groupRemainingQuota := NewResourceQuota()
	groupRemainingQuota.cpu = resource.NewQuantity(8, resource.DecimalSI)
	groupRemainingQuota.gpu = nil
	groupRemainingQuota.memory = nil

	instanceTypeInfo := graphql.DtoInstanceType{
		Name: "testInstanceType",
		Spec: graphql.DtoInstanceTypeSpec{
			LimitsCpu:    4,
			LimitsMemory: "1Gi",
			LimitsGpu:    0,
		},
	}

	InstanceTypeCache.Set("instanceType:testInstanceType", &instanceTypeInfo, time.Hour*1)

	phJobScheduler.scheduleByStrictOrder(&phJobs, &usersRemainingQuota, groupRemainingQuota)

	if phJobs[0].Status.Phase != primehubv1alpha1.JobReady || phJobs[1].Status.Phase != primehubv1alpha1.JobReady || phJobs[2].Status.Phase == primehubv1alpha1.JobReady {
		t.Error("Wrong Scheduling")
	}
}
