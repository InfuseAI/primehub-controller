package controllers

import (
	"context"
	"errors"
	primehubv1alpha1 "primehub-controller/ee/api/v1alpha1"
	phcache "primehub-controller/pkg/cache"
	"primehub-controller/pkg/escapism"
	"primehub-controller/pkg/graphql"
	"primehub-controller/pkg/quota"
	"sort"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
)

var (
	groupAggregationKey = "primehub.io/group"
	userAggregationKey  = "primehub.io/user"
)

type PhjobCompareFunc func(*primehubv1alpha1.PhJob, *primehubv1alpha1.PhJob) bool

func compareByCreationTimestamp(job1 *primehubv1alpha1.PhJob, job2 *primehubv1alpha1.PhJob) bool {
	return job1.CreationTimestamp.Before(&job2.CreationTimestamp)
}

type PHJobScheduler struct {
	client.Client
	Log           logr.Logger
	GraphqlClient graphql.AbstractGraphqlClient
	PrimeHubCache *phcache.PrimeHubCache
}

func (r *PHJobScheduler) getCurrentUsage(namespace string, aggregationKeys []string, aggregationValues []string) (*quota.ResourceQuota, error) {
	if len(aggregationKeys) != len(aggregationValues) {
		return nil, errors.New("length of key and value must be the same")
	}

	ctx := context.Background()

	pods := corev1.PodList{}
	labels := make(map[string]string)
	for idx, aggregationKey := range aggregationKeys {
		labels[aggregationKey] = escapism.EscapeToPrimehubLabel(aggregationValues[idx])
	}
	err := r.Client.List(ctx, &pods, client.InNamespace(namespace), client.MatchingLabels(labels))
	if err != nil {
		return nil, err
	}

	resourceUsage := *quota.NewResourceQuota()
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodPending || pod.Status.Phase == corev1.PodRunning {
			for _, container := range pod.Spec.Containers {
				resourceUsage.Cpu.Add(*container.Resources.Limits.Cpu())
				if _, ok := container.Resources.Limits["nvidia.com/gpu"]; ok {
					resourceUsage.Gpu.Add(container.Resources.Limits["nvidia.com/gpu"])
				}
				resourceUsage.Memory.Add(*container.Resources.Limits.Memory())
			}
		}
	}
	return &resourceUsage, nil
}

func (r *PHJobScheduler) getGroupRemainingQuota(phJob *primehubv1alpha1.PhJob) (*quota.ResourceQuota, error) {
	groupInfo, err := r.PrimeHubCache.FetchGroup(phJob.Spec.GroupId)
	if err != nil {
		return nil, err
	}
	return quota.CalculateGroupRemainingResourceQuota(r.Client, phJob.Namespace, groupInfo)
}

func (r *PHJobScheduler) getUserRemainingQuotaInGroup(phJob *primehubv1alpha1.PhJob) (*quota.ResourceQuota, error) {
	groupInfo, err := r.PrimeHubCache.FetchGroup(phJob.Spec.GroupId)
	if err != nil {
		return nil, err
	}
	return quota.CalculateUserRemainingResourceQuota(r.Client, phJob.Namespace, groupInfo, phJob.Spec.UserName)
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

func (r *PHJobScheduler) validate(requestedQuota *quota.ResourceQuota, groupInfo *graphql.DtoGroup) (bool, error) {
	groupQuota, err := quota.ConvertToResourceQuota(groupInfo.ProjectQuotaCpu, groupInfo.ProjectQuotaGpu, groupInfo.ProjectQuotaMemory)
	if err != nil {
		return false, err
	}
	userQuota, err := quota.ConvertToResourceQuota(groupInfo.QuotaCpu, groupInfo.QuotaGpu, groupInfo.QuotaMemory)
	if err != nil {
		return false, err
	}
	userValid := quota.ValidateResource(requestedQuota, userQuota)
	groupValid := quota.ValidateResource(requestedQuota, groupQuota)

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

func (r *PHJobScheduler) scheduleByStrictOrder(phJobsRef *[]*primehubv1alpha1.PhJob, usersRemainingQuotaRef *map[string]quota.ResourceQuota, groupRemainingQuota *quota.ResourceQuota) error {
	phJobs := *phJobsRef
	usersRemainingQuota := *usersRemainingQuotaRef

	for _, phJob := range phJobs {
		instanceInfo, err := r.PrimeHubCache.FetchInstanceType(phJob.Spec.InstanceType)
		if err != nil {
			return err
		}
		instanceRequestedQuota, err := quota.InstanceTypeRequestedQuota(instanceInfo)
		if err != nil {
			return err
		}

		_, foundUser := usersRemainingQuota[phJob.Spec.UserName]
		if !foundUser {
			userRemainingQuota, err := r.getUserRemainingQuotaInGroup(phJob)
			if err != nil {
				return err
			}
			usersRemainingQuota[phJob.Spec.UserName] = *userRemainingQuota
		}
		userRemainingQuota := usersRemainingQuota[phJob.Spec.UserName]

		userValid := quota.ValidateResource(instanceRequestedQuota, &userRemainingQuota)
		groupValid := quota.ValidateResource(instanceRequestedQuota, groupRemainingQuota)
		if userValid == false {
			continue
		}
		if groupValid == false {
			break
		}

		phJob.Status.Phase = primehubv1alpha1.JobPreparing

		if groupRemainingQuota.Cpu != nil {
			groupRemainingQuota.Cpu.Sub(*instanceRequestedQuota.Cpu)
		}
		if groupRemainingQuota.Gpu != nil {
			groupRemainingQuota.Gpu.Sub(*instanceRequestedQuota.Gpu)
		}
		if groupRemainingQuota.Memory != nil {
			groupRemainingQuota.Memory.Sub(*instanceRequestedQuota.Memory)
		}
		if usersRemainingQuota[phJob.Spec.UserName].Cpu != nil {
			usersRemainingQuota[phJob.Spec.UserName].Cpu.Sub(*instanceRequestedQuota.Cpu)
		}
		if usersRemainingQuota[phJob.Spec.UserName].Gpu != nil {
			usersRemainingQuota[phJob.Spec.UserName].Gpu.Sub(*instanceRequestedQuota.Gpu)
		}
		if usersRemainingQuota[phJob.Spec.UserName].Memory != nil {
			usersRemainingQuota[phJob.Spec.UserName].Memory.Sub(*instanceRequestedQuota.Memory)
		}
	}
	return nil
}

func (r *PHJobScheduler) Schedule() {
	r.Log.V(1).Info("start scheduling")

	ctx := context.Background()

	phJobList, err := r.list()
	if err != nil {
		r.Log.Error(err, "cannot get phjob list")
		return
	}
	r.Log.V(1).Info("list completed", "len of phjobs", len(phJobList.Items))

	i := 0
	for _, phJob := range phJobList.Items {
		groupInfo, err := r.PrimeHubCache.FetchGroup(phJob.Spec.GroupId)
		if err != nil {
			r.Log.Error(err, "cannot get group info")

			phJobCopy := phJob.DeepCopy()
			phJobCopy.Status.Phase = primehubv1alpha1.JobFailed
			phJobCopy.Status.Reason = primehubv1alpha1.JobReasonPodCreationFailed
			phJobCopy.Status.Message = "cannot get group info"
			now := metav1.Now()
			phJobCopy.Status.FinishTime = &now
			err := r.Status().Update(ctx, phJobCopy)
			if err != nil {
				r.Log.Error(err, "update phjob status failed")
			}
			continue
		}
		instanceInfo, err := r.PrimeHubCache.FetchInstanceType(phJob.Spec.InstanceType)
		if err != nil {
			r.Log.Error(err, "cannot get instance type info")
			continue
		}
		instanceRequestedQuota, err := quota.InstanceTypeRequestedQuota(instanceInfo)
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
			phJobCopy.Status.Reason = primehubv1alpha1.JobReasonOverQuota
			phJobCopy.Status.Message = "Your instance type resource limit is bigger than your user or group quota."
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
	r.Log.V(1).Info("group completed", "len of groups", len(*phJobsGroupMapping))

	for groupId, phJobs := range *phJobsGroupMapping {
		r.Log.V(1).Info("start processing group", "group id", groupId)

		r.sort(&phJobs, compareByCreationTimestamp)

		groupRemainingQuota, err := r.getGroupRemainingQuota(phJobs[0])
		if err != nil {
			r.Log.Error(err, "cannot get group remaining quota")
			continue
		}
		usersRemainingQuota := make(map[string]quota.ResourceQuota)
		err = r.scheduleByStrictOrder(&phJobs, &usersRemainingQuota, groupRemainingQuota)
		if err != nil {
			r.Log.Error(err, "schedule by strict order failed")
		}

		for _, phJob := range phJobs {
			if phJob.Status.Phase == primehubv1alpha1.JobPreparing {
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

	r.Log.V(1).Info("end of scheduling")
}
