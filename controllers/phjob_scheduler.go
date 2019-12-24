package controllers

import (
	"context"
	"sort"
	"time"

	primehubv1alpha1 "primehub-controller/api/v1alpha1"
	"primehub-controller/pkg/escapism"
	"primehub-controller/pkg/graphql"

	"github.com/go-logr/logr"
	"github.com/karlseguin/ccache"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
)

var (
	GroupCache        = ccache.New(ccache.Configure().MaxSize(1000).ItemsToPrune(100))
	InstanceTypeCache = ccache.New(ccache.Configure().MaxSize(1000).ItemsToPrune(100))

	group_aggregation_key = "primehub.io/group"
	user_aggregation_key  = "primehub.io/user"
	cacheExpiredTime      = time.Minute * 10
)

// nil means it doesn't limit the quota
type ResourceQuota struct {
	cpu    *resource.Quantity
	gpu    *resource.Quantity
	memory *resource.Quantity
}

func NewResourceQuota() *ResourceQuota {
	return &ResourceQuota{
		cpu:    &resource.Quantity{},
		gpu:    &resource.Quantity{},
		memory: &resource.Quantity{},
	}
}

func ConvertToResourceQuota(cpu float32, gpu float32, memory string) (*ResourceQuota, error) {
	var resourceQuota ResourceQuota = *NewResourceQuota()
	if cpu < 0 {
		resourceQuota.cpu = nil
	} else {
		resourceQuota.cpu.SetMilli(int64(cpu*1000 + 0.5))
	}

	if gpu < 0 {
		resourceQuota.gpu = nil
	} else {
		resourceQuota.gpu.Set(int64(gpu + 0.5))
	}

	if memory == "" {
		resourceQuota.memory = nil
	} else {
		memoryLimit, err := resource.ParseQuantity(memory)
		if err != nil {
			return nil, err
		}
		resourceQuota.memory = &memoryLimit
	}

	return &resourceQuota, nil
}

type PhjobCompareFunc func(*primehubv1alpha1.PhJob, *primehubv1alpha1.PhJob) bool

func compareByCreationTimestamp(job1 *primehubv1alpha1.PhJob, job2 *primehubv1alpha1.PhJob) bool {
	return job1.CreationTimestamp.Before(&job2.CreationTimestamp)
}

type PHJobScheduler struct {
	client.Client
	Log           logr.Logger
	GraphqlClient *graphql.GraphqlClient
}

func (r *PHJobScheduler) getGroupInfo(groupId string) (*graphql.DtoGroup, error) {
	cacheKey := "group:" + groupId
	cacheItem := GroupCache.Get(cacheKey)
	if cacheItem == nil || cacheItem.Expired() {
		groupInfo, err := r.GraphqlClient.FetchGroupInfo(groupId)
		if err != nil {
			return nil, err
		}
		GroupCache.Set(cacheKey, groupInfo, cacheExpiredTime)
	}
	return GroupCache.Get(cacheKey).Value().(*graphql.DtoGroup), nil
}

func (r *PHJobScheduler) getInstanceTypeInfo(instanceTypeId string) (*graphql.DtoInstanceType, error) {
	cacheKey := "instanceType:" + instanceTypeId
	cacheItem := InstanceTypeCache.Get(cacheKey)
	if cacheItem == nil || cacheItem.Expired() {
		instanceTypeInfo, err := r.GraphqlClient.FetchInstanceTypeInfo(instanceTypeId)
		if err != nil {
			return nil, err
		}
		InstanceTypeCache.Set(cacheKey, instanceTypeInfo, cacheExpiredTime)
	}
	return InstanceTypeCache.Get(cacheKey).Value().(*graphql.DtoInstanceType), nil
}

func (r *PHJobScheduler) getCurrentUsage(namespace string, aggregation_key string, aggregation_value string) (*ResourceQuota, error) {
	ctx := context.Background()

	pods := corev1.PodList{}
	escaped_aggregation_value := escapism.EscapeToPrimehubLabel(aggregation_value)
	err := r.Client.List(ctx, &pods, client.InNamespace(namespace), client.MatchingLabels(map[string]string{aggregation_key: escaped_aggregation_value}))
	if err != nil {
		return nil, err
	}

	var resourceUsage ResourceQuota = *NewResourceQuota()
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodPending || pod.Status.Phase == corev1.PodRunning {
			for _, container := range pod.Spec.Containers {
				resourceUsage.cpu.Add(*container.Resources.Limits.Cpu())
				if _, ok := container.Resources.Limits["nvidia.com/gpu"]; ok {
					resourceUsage.gpu.Add(container.Resources.Limits["nvidia.com/gpu"])
				}
				resourceUsage.memory.Add(*container.Resources.Limits.Memory())
			}
		}
	}
	return &resourceUsage, nil
}

func (r *PHJobScheduler) getGroupRemainingQuota(phJob *primehubv1alpha1.PhJob) (*ResourceQuota, error) {
	groupInfo, err := r.getGroupInfo(phJob.Spec.GroupId)
	if err != nil {
		return nil, err
	}
	groupUsage, err := r.getCurrentUsage(phJob.Namespace, group_aggregation_key, phJob.Spec.GroupName)
	if err != nil {
		return nil, err
	}
	groupRemainingQuota, err := ConvertToResourceQuota(groupInfo.ProjectQuotaCpu, groupInfo.ProjectQuotaGpu, groupInfo.ProjectQuotaMemory)
	if err != nil {
		return nil, err
	}
	if groupRemainingQuota.cpu != nil {
		groupRemainingQuota.cpu.Sub(*groupUsage.cpu)
	}
	if groupRemainingQuota.gpu != nil {
		groupRemainingQuota.gpu.Sub(*groupUsage.gpu)
	}
	if groupRemainingQuota.memory != nil {
		groupRemainingQuota.memory.Sub(*groupUsage.memory)
	}
	return groupRemainingQuota, nil
}

func (r *PHJobScheduler) getUserRemainingQuota(phJob *primehubv1alpha1.PhJob) (*ResourceQuota, error) {
	groupInfo, err := r.getGroupInfo(phJob.Spec.GroupId)
	if err != nil {
		return nil, err
	}
	userUsage, err := r.getCurrentUsage(phJob.Namespace, user_aggregation_key, phJob.Spec.UserName)
	if err != nil {
		return nil, err
	}
	userRemainingQuota, err := ConvertToResourceQuota(groupInfo.QuotaCpu, groupInfo.QuotaGpu, groupInfo.QuotaMemory)
	if err != nil {
		return nil, err
	}
	if userRemainingQuota.cpu != nil {
		userRemainingQuota.cpu.Sub(*userUsage.cpu)
	}
	if userRemainingQuota.gpu != nil {
		userRemainingQuota.gpu.Sub(*userUsage.gpu)
	}
	if userRemainingQuota.memory != nil {
		userRemainingQuota.memory.Sub(*userUsage.memory)
	}
	return userRemainingQuota, nil
}

func (r *PHJobScheduler) validateResource(requestedQuota *ResourceQuota, resourceQuota *ResourceQuota) (bool, error) {
	if (resourceQuota.cpu != nil && requestedQuota.cpu != nil && resourceQuota.cpu.Cmp(*requestedQuota.cpu) == -1) ||
		(resourceQuota.gpu != nil && requestedQuota.gpu != nil && resourceQuota.gpu.Cmp(*requestedQuota.gpu) == -1) ||
		(resourceQuota.memory != nil && requestedQuota.memory != nil && resourceQuota.memory.Cmp(*requestedQuota.memory) == -1) {
		return false, nil
	}

	return true, nil
}

func (r *PHJobScheduler) validateUserAndGroupQuota(requestedQuota *ResourceQuota, userQuota *ResourceQuota, groupQuota *ResourceQuota) (bool, bool, error) {
	userValid, err := r.validateResource(requestedQuota, userQuota)
	if err != nil {
		return false, false, err
	}

	groupValid, err := r.validateResource(requestedQuota, groupQuota)
	if err != nil {
		return false, false, err
	}

	return userValid, groupValid, nil
}

func (r *PHJobScheduler) list() (*primehubv1alpha1.PhJobList, error) {
	ctx := context.Background()

	var phJobList primehubv1alpha1.PhJobList
	if err := r.Client.List(ctx, &phJobList); err != nil {
		return &primehubv1alpha1.PhJobList{}, err
	}

	// choose pending jobs
	// TODO: can improve performance by fieldSelector or labelSelector
	i := 0
	for _, phJob := range phJobList.Items {
		if phJob.Status.Phase == primehubv1alpha1.JobPending {
			phJobList.Items[i] = phJob
			i++
		}
	}

	phJobList.Items = phJobList.Items[:i]
	return &phJobList, nil
}

func (r *PHJobScheduler) validate(requestedQuota *ResourceQuota, groupInfo *graphql.DtoGroup) (bool, error) {
	groupQuota, err := ConvertToResourceQuota(groupInfo.ProjectQuotaCpu, groupInfo.ProjectQuotaGpu, groupInfo.ProjectQuotaMemory)
	if err != nil {
		return false, err
	}
	userQuota, err := ConvertToResourceQuota(groupInfo.QuotaCpu, groupInfo.QuotaGpu, groupInfo.QuotaMemory)
	if err != nil {
		return false, err
	}
	userValid, groupValid, err := r.validateUserAndGroupQuota(requestedQuota, userQuota, groupQuota)
	if err != nil {
		return false, err
	}

	if userValid == false || groupValid == false {
		return false, nil
	}

	return true, nil
}

func (r *PHJobScheduler) filter(phJob *primehubv1alpha1.PhJob) (bool, error) {
	return false, nil
}

func (r *PHJobScheduler) group(phJobList *primehubv1alpha1.PhJobList) (*map[string][]*primehubv1alpha1.PhJob, error) {
	phJobsGroupMapping := make(map[string][]*primehubv1alpha1.PhJob)

	phJobItems := phJobList.Items
	for _, phJob := range phJobItems {
		if _, ok := phJobsGroupMapping[phJob.Spec.GroupId]; !ok {
			phJobsGroupMapping[phJob.Spec.GroupId] = []*primehubv1alpha1.PhJob{}
		}

		phJobsGroupMapping[phJob.Spec.GroupId] = append(phJobsGroupMapping[phJob.Spec.GroupId], phJob.DeepCopy())
	}

	return &phJobsGroupMapping, nil
}

func (r *PHJobScheduler) sort(phJobs *[]*primehubv1alpha1.PhJob, cmpFunc PhjobCompareFunc) {
	phJobsValues := *phJobs
	sort.Slice(phJobsValues, func(i int, j int) bool {
		return cmpFunc(phJobsValues[i], phJobsValues[j])
	})
}

func (r *PHJobScheduler) scheduleByStrictOrder(phJobsRef *[]*primehubv1alpha1.PhJob, usersRemainingQuotaRef *map[string]ResourceQuota, groupRemainingQuota *ResourceQuota) error {
	phJobs := *phJobsRef
	usersRemainingQuota := *usersRemainingQuotaRef

	for _, phJob := range phJobs {
		instanceInfo, err := r.getInstanceTypeInfo(phJob.Spec.InstanceType)
		if err != nil {
			return err
		}
		instanceRequestedQuota, err := ConvertToResourceQuota(instanceInfo.Spec.LimitsCpu, (float32)(instanceInfo.Spec.LimitsGpu), instanceInfo.Spec.LimitsMemory)
		if err != nil {
			return err
		}

		_, foundUser := usersRemainingQuota[phJob.Spec.UserName]
		if !foundUser {
			userRemainingQuota, err := r.getUserRemainingQuota(phJob)
			if err != nil {
				return err
			}
			usersRemainingQuota[phJob.Spec.UserName] = *userRemainingQuota
		}
		userRemainingQuota := usersRemainingQuota[phJob.Spec.UserName]

		userValid, groupValid, err := r.validateUserAndGroupQuota(instanceRequestedQuota, &userRemainingQuota, groupRemainingQuota)
		if userValid == false {
			continue
		}
		if groupValid == false {
			break
		}

		phJob.Status.Phase = primehubv1alpha1.JobReady

		if groupRemainingQuota.cpu != nil {
			groupRemainingQuota.cpu.Sub(*instanceRequestedQuota.cpu)
		}
		if groupRemainingQuota.gpu != nil {
			groupRemainingQuota.gpu.Sub(*instanceRequestedQuota.gpu)
		}
		if groupRemainingQuota.memory != nil {
			groupRemainingQuota.memory.Sub(*instanceRequestedQuota.memory)
		}
		if usersRemainingQuota[phJob.Spec.UserName].cpu != nil {
			usersRemainingQuota[phJob.Spec.UserName].cpu.Sub(*instanceRequestedQuota.cpu)
		}
		if usersRemainingQuota[phJob.Spec.UserName].gpu != nil {
			usersRemainingQuota[phJob.Spec.UserName].gpu.Sub(*instanceRequestedQuota.gpu)
		}
		if usersRemainingQuota[phJob.Spec.UserName].memory != nil {
			usersRemainingQuota[phJob.Spec.UserName].memory.Sub(*instanceRequestedQuota.memory)
		}
	}
	return nil
}

func (r *PHJobScheduler) Schedule() {
	r.Log.Info("start scheduling")

	ctx := context.Background()

	phJobList, err := r.list()
	if err != nil {
		r.Log.Error(err, "cannot get phjob list")
		return
	}
	r.Log.Info("list completed", "len of phjobs", len(phJobList.Items))

	i := 0
	for _, phJob := range phJobList.Items {
		groupInfo, err := r.getGroupInfo(phJob.Spec.GroupId)
		if err != nil {
			r.Log.Error(err, "cannot get group info")
			continue
		}
		instanceInfo, err := r.getInstanceTypeInfo(phJob.Spec.InstanceType)
		if err != nil {
			r.Log.Error(err, "cannot get instance type info")
			continue
		}
		instanceRequestedQuota, err := ConvertToResourceQuota(instanceInfo.Spec.LimitsCpu, (float32)(instanceInfo.Spec.LimitsGpu), instanceInfo.Spec.LimitsMemory)
		if err != nil {
			r.Log.Error(err, "cannot get convert instance type resource quota")
			continue
		}

		valid, err := r.validate(instanceRequestedQuota, groupInfo)
		if err != nil {
			r.Log.Error(err, "failed to validate phjob")
			continue
		}
		if valid == false {
			phJobCopy := phJob.DeepCopy()
			phJobCopy.Status.Phase = primehubv1alpha1.JobFailed
			phJobCopy.Status.Reason = "Your instance type resource limit is bigger than your user or group quota."
			now := metav1.Now()
			phJobCopy.Status.FinishTime = &now
			err := r.Status().Update(ctx, phJobCopy)
			if err != nil {
				r.Log.Error(err, "update phjob status failed")
				continue
			}
		}

		filtered, err := r.filter(&phJob)
		if err != nil {
			r.Log.Error(err, "failed to filter phjob")
			continue
		}

		if valid == true && filtered == false {
			phJobList.Items[i] = phJob
			i++
		}
	}
	phJobList.Items = phJobList.Items[:i]

	phJobsGroupMapping, err := r.group(phJobList)
	if err != nil {
		r.Log.Error(err, "cannot group phjob list")
		return
	}
	r.Log.Info("group completed", "len of groups", len(*phJobsGroupMapping))

	for groupId, phJobs := range *phJobsGroupMapping {
		r.Log.Info("start processing group", "group id", groupId)

		r.sort(&phJobs, compareByCreationTimestamp)

		groupRemainingQuota, err := r.getGroupRemainingQuota(phJobs[0])
		if err != nil {
			r.Log.Error(err, "cannot get group remaining quota")
			continue
		}
		usersRemainingQuota := make(map[string]ResourceQuota)
		err = r.scheduleByStrictOrder(&phJobs, &usersRemainingQuota, groupRemainingQuota)
		if err != nil {
			r.Log.Error(err, "schedule by strict order failed")
		}

		for _, phJob := range phJobs {
			if phJob.Status.Phase == primehubv1alpha1.JobReady {
				r.Log.Info("scheduled", "phjob", phJob.Name)
				phJobCopy := phJob.DeepCopy()
				err := r.Status().Update(ctx, phJobCopy)
				if err != nil {
					r.Log.Error(err, "update phjob status failed")
					continue
				}
			}
		}
	}

	r.Log.Info("end of scheduling")
}
