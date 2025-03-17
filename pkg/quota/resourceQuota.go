package quota

import (
	"context"
	"primehub-controller/pkg/escapism"
	"primehub-controller/pkg/graphql"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ResourceQuota struct {
	Cpu    *resource.Quantity
	Gpu    *resource.Quantity
	Memory *resource.Quantity
}

func (r *ResourceQuota) Sub(y ResourceQuota) {
	if r.Cpu != nil && y.Cpu != nil {
		r.Cpu.Sub(*y.Cpu)
	}
	if r.Gpu != nil && y.Gpu != nil {
		r.Gpu.Sub(*y.Gpu)
	}
	if r.Memory != nil && y.Memory != nil {
		r.Memory.Sub(*y.Memory)
	}
}

func (r *ResourceQuota) Add(y ResourceQuota) {
	if r.Cpu != nil && y.Cpu != nil {
		r.Cpu.Add(*y.Cpu)
	}
	if r.Gpu != nil && y.Gpu != nil {
		r.Gpu.Add(*y.Gpu)
	}
	if r.Memory != nil && y.Memory != nil {
		r.Memory.Add(*y.Memory)
	}
}

func NewResourceQuota() *ResourceQuota {
	return &ResourceQuota{
		Cpu:    &resource.Quantity{},
		Gpu:    &resource.Quantity{},
		Memory: &resource.Quantity{},
	}
}

func InstanceTypeRequestedQuota(instanceType *graphql.DtoInstanceType) (*ResourceQuota, error) {
	return ConvertToResourceQuota(instanceType.Spec.LimitsCpu,
		float32(instanceType.Spec.LimitsGpu),
		instanceType.Spec.LimitsMemory)
}

func GroupResourceQuota(group *graphql.DtoGroup) (groupQuota *ResourceQuota, userQuota *ResourceQuota, err error) {
	groupQuota, err = ConvertToResourceQuota(group.ProjectQuotaCpu, group.ProjectQuotaGpu, group.ProjectQuotaMemory)
	if err != nil {
		return nil, nil, err
	}

	userQuota, err = ConvertToResourceQuota(group.QuotaCpu, group.QuotaGpu, group.QuotaMemory)
	if err != nil {
		return nil, nil, err
	}

	return groupQuota, userQuota, nil
}

func CurrentResourceUsage(cgo client.Client, namespace string, matchingLabels client.MatchingLabels) (*ResourceQuota, error) {
	pods := corev1.PodList{}
	err := cgo.List(context.Background(), &pods, client.InNamespace(namespace), matchingLabels)
	if err != nil {
		return nil, err
	}

	resourceUsage := *NewResourceQuota()
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodPending || pod.Status.Phase == corev1.PodRunning {
			for _, container := range pod.Spec.Containers {
				resourceUsage.Cpu.Add(*container.Resources.Limits.Cpu())
				if value, ok := getGpuFromLimits(container.Resources.Limits); ok {
					resourceUsage.Gpu.Add(value)
				}
				resourceUsage.Memory.Add(*container.Resources.Limits.Memory())
			}
		}
	}
	return &resourceUsage, nil
}

func CurrentGroupResourceUsage(cgo client.Client, namespace string, groupName string) (*ResourceQuota, error) {
	return CurrentResourceUsage(cgo, namespace, map[string]string{
		"primehub.io/group": groupName,
	})
}

func CurrentUserResourceUsage(cgo client.Client, namespace string, groupName string, userName string) (*ResourceQuota, error) {
	return CurrentResourceUsage(cgo, namespace, map[string]string{
		"primehub.io/group": groupName,
		"primehub.io/user":  userName,
	})
}

func ConvertToResourceQuota(cpu float32, gpu float32, memory string) (*ResourceQuota, error) {
	resourceQuota := *NewResourceQuota()
	if cpu < 0 {
		resourceQuota.Cpu = nil
	} else {
		resourceQuota.Cpu.SetMilli(int64(cpu*1000 + 0.5))
	}

	if gpu < 0 {
		resourceQuota.Gpu = nil
	} else {
		resourceQuota.Gpu.Set(int64(gpu + 0.5))
	}

	if memory == "" {
		resourceQuota.Memory = nil
	} else {
		memoryLimit, err := resource.ParseQuantity(memory)
		if err != nil {
			return nil, err
		}
		resourceQuota.Memory = &memoryLimit
	}

	return &resourceQuota, nil
}

func ValidateResource(requestedQuota *ResourceQuota, resourceQuota *ResourceQuota) bool {
	if (resourceQuota.Cpu != nil && requestedQuota.Cpu != nil && resourceQuota.Cpu.Cmp(*requestedQuota.Cpu) == -1) ||
		(resourceQuota.Gpu != nil && requestedQuota.Gpu != nil && resourceQuota.Gpu.Cmp(*requestedQuota.Gpu) == -1) ||
		(resourceQuota.Memory != nil && requestedQuota.Memory != nil && resourceQuota.Memory.Cmp(*requestedQuota.Memory) == -1) {
		return false
	}
	return true
}

func CalculateGroupRemainingResourceQuota(cgo client.Client, namespace string, groupInfo *graphql.DtoGroup) (*ResourceQuota, error) {
	if groupInfo == nil {
		return nil, apierrors.NewBadRequest("groupInfo not provided")
	}
	groupName := escapism.EscapeToPrimehubLabel(groupInfo.Name)
	groupUsage, err := CurrentGroupResourceUsage(cgo, namespace, groupName)
	if err != nil {
		return nil, err
	}
	groupRemainingQuota, _, err := GroupResourceQuota(groupInfo)
	if err != nil {
		return nil, err
	}

	if groupRemainingQuota.Cpu != nil {
		groupRemainingQuota.Cpu.Sub(*groupUsage.Cpu)
	}
	if groupRemainingQuota.Gpu != nil {
		groupRemainingQuota.Gpu.Sub(*groupUsage.Gpu)
	}
	if groupRemainingQuota.Memory != nil {
		groupRemainingQuota.Memory.Sub(*groupUsage.Memory)
	}
	return groupRemainingQuota, nil
}

func CalculateUserRemainingResourceQuota(cgo client.Client, namespace string, groupInfo *graphql.DtoGroup, userName string) (*ResourceQuota, error) {
	if groupInfo == nil {
		return nil, apierrors.NewBadRequest("groupInfo not provided")
	}
	groupName := escapism.EscapeToPrimehubLabel(groupInfo.Name)
	if strings.Index(userName, "escaped-") != 0 {
		userName = escapism.EscapeToPrimehubLabel(userName)
	}
	userUsage, err := CurrentUserResourceUsage(cgo, namespace, groupName, userName)
	if err != nil {
		return nil, err
	}
	_, userRemainingQuota, err := GroupResourceQuota(groupInfo)
	if err != nil {
		return nil, err
	}

	if userRemainingQuota.Cpu != nil {
		userRemainingQuota.Cpu.Sub(*userUsage.Cpu)
	}
	if userRemainingQuota.Gpu != nil {
		userRemainingQuota.Gpu.Sub(*userUsage.Gpu)
	}
	if userRemainingQuota.Memory != nil {
		userRemainingQuota.Memory.Sub(*userUsage.Memory)
	}
	return userRemainingQuota, nil
}

func getGpuFromLimits(limits corev1.ResourceList) (resource.Quantity, bool) {
	if _, ok := limits["nvidia.com/gpu"]; ok {
		return limits["nvidia.com/gpu"], ok
	}
	if _, ok := limits["amd.com/gpu"]; ok {
		return limits["amd.com/gpu"], ok
	}
	for key, _ := range limits {
		if strings.HasPrefix(string(key), "gpu.intel.com/") {
			return limits[key], true
		}
	}
	return resource.Quantity{}, false
}
