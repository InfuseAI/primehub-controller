package controllers

import (
	"context"
	"sort"
	"time"

	primehubv1alpha1 "primehub-controller/ee/api/v1alpha1"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PHJobCleaner struct {
	client.Client
	Log           logr.Logger
	JobTTLSeconds int64
	JobLimit      int32
}

func (r *PHJobCleaner) list() (*primehubv1alpha1.PhJobList, error) {
	ctx := context.Background()

	var phJobList primehubv1alpha1.PhJobList
	if err := r.Client.List(ctx, &phJobList); err != nil {
		return &primehubv1alpha1.PhJobList{}, err
	}

	// choose jobs in final states
	// TODO: can improve performance by fieldSelector or labelSelector
	i := 0
	for _, phJob := range phJobList.Items {
		if inFinalPhase(phJob.Status.Phase) {
			phJobList.Items[i] = phJob
			i++
		}
	}

	phJobList.Items = phJobList.Items[:i]
	return &phJobList, nil
}

func (r *PHJobCleaner) sort(phJobs *[]primehubv1alpha1.PhJob, cmpFunc PhjobCompareFunc) {
	phJobsValues := *phJobs
	sort.Slice(phJobsValues, func(i int, j int) bool {
		return cmpFunc(&phJobsValues[i], &phJobsValues[j])
	})
}

func (r *PHJobCleaner) deletePhJob(ctx context.Context, phJob *primehubv1alpha1.PhJob) error {
	gracePeriodSeconds := int64(0)
	deleteOptions := client.DeleteOptions{GracePeriodSeconds: &gracePeriodSeconds}
	if err := r.Client.Delete(ctx, phJob, &deleteOptions); err != nil {
		return err
	}
	return nil
}

func (r *PHJobCleaner) Clean() {
	r.Log.V(1).Info("start cleaning phjobs")

	ctx := context.Background()

	phJobList, err := r.list()
	if err != nil {
		r.Log.Error(err, "cannot get phjob list")
		return
	}
	r.Log.V(1).Info("list completed", "len of phjobs", len(phJobList.Items))

	if r.JobTTLSeconds != int64(0) {
		i := 0
		for _, phJob := range phJobList.Items {
			currentTime := time.Now()
			ttlDuration := time.Second * time.Duration(r.JobTTLSeconds)
			if phJob.Status.FinishTime == nil {
				r.Log.Info("phjob in final phase (Succeeded, Failed, Unknown) without finish time, it is incorrect!!!")
				r.Log.Info("will ignore it and do nothing.")
				continue
			}

			if currentTime.After(phJob.Status.FinishTime.Add(ttlDuration)) {
				if err := r.deletePhJob(ctx, &phJob); err != nil {
					r.Log.Error(err, "cannot get delete the phjob")
					continue
				}
				r.Log.Info("delete phjob due to JobTTLSeconds", "phjob name", phJob.Name)
			} else {
				phJobList.Items[i] = phJob
				i++
			}
		}
		phJobList.Items = phJobList.Items[:i]
	}

	phJobs := phJobList.Items
	if r.JobLimit != int32(0) && int32(len(phJobs)) > r.JobLimit {
		r.sort(&phJobs, compareByCreationTimestamp)
		countOfDelete := int(int32(len(phJobs)) - r.JobLimit)
		for i := 0; i < countOfDelete; i++ {
			phJob := phJobs[i]
			if err := r.deletePhJob(ctx, &phJob); err != nil {
				r.Log.Error(err, "cannot get delete the phjob")
				continue
			}
			r.Log.Info("delete phjob due to JobLimit", "phjob name", phJob.Name)
		}
	}

	r.Log.V(1).Info("end of cleaning")
}
