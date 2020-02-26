/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	cron "github.com/robfig/cron/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	primehubv1alpha1 "primehub-controller/api/v1alpha1"
	"primehub-controller/pkg/graphql"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type PhScheduleCron struct {
	c          *cron.Cron
	entryID    cron.EntryID
	recurType  primehubv1alpha1.RecurrenceType
	recurrence string
	// (TODO) add timezone
}

// PhScheduleReconciler reconciles a PhSchedule object
type PhScheduleReconciler struct {
	client.Client
	Log               logr.Logger
	PhScheduleCronMap map[string]*PhScheduleCron
	GraphqlClient     *graphql.GraphqlClient
}

func (r *PhScheduleReconciler) buildPhJob(phSchedule *primehubv1alpha1.PhSchedule) (*primehubv1alpha1.PhJob, error) {
	log := r.Log.WithValues("phschedule", phSchedule.Name)
	t := time.Now()
	hash, err := generateRandomString(4)
	phJobName := "job-" + t.Format("200601021504") + "-" + hash

	phJob := &primehubv1alpha1.PhJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:        phJobName,
			Namespace:   phSchedule.Namespace,
			Annotations: phSchedule.ObjectMeta.Annotations,
			Labels: map[string]string{
				"phjob.primehub.io/scheduledBy": phSchedule.Name,
			},
		},
	}
	// (JUST WORKAROUND): check user/group/instancetype/image is correct by using graphql functions
	phJobSpec := phSchedule.Spec.JobTemplate.Spec
	var result *graphql.DtoResult
	if result, err = r.GraphqlClient.FetchByUserId(phJobSpec.UserId); err != nil {
		return nil, err
	}
	options := graphql.SpawnerDataOptions{}
	log.Info("info:", "phJobSpec.GroupName", phJobSpec.GroupName, "phJobSpec.InstanceType", phJobSpec.InstanceType, "phJobSpec.Image", phJobSpec.Image)
	if _, err = graphql.NewSpawnerByData(result.Data, phJobSpec.GroupName, phJobSpec.InstanceType, phJobSpec.Image, options); err != nil {
		return nil, err
	}

	phJob.Spec = phJobSpec
	return phJob, nil

}

// +kubebuilder:rbac:groups=primehub.io,resources=phschedules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=primehub.io,resources=phschedules/status,verbs=get;update;patch

func (r *PhScheduleReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("phschedule", req.Name)

	log.Info("start Reconcile")
	startTime := time.Now()
	defer func() {
		log.Info("Finished Reconciling phJob ", "ReconcileTime", time.Since(startTime))
	}()

	phSchedule := &primehubv1alpha1.PhSchedule{}
	if err := r.Get(ctx, req.NamespacedName, phSchedule); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("PhJob deleted")
			return ctrl.Result{}, nil
		} else {
			log.Error(err, "unable to fetch PhShceduleJob")
		}
	}

	phSchedule = phSchedule.DeepCopy()
	recurType := phSchedule.Spec.Recurrence.Type
	var recurrence string
	if recurType == "custom" { // if the phSchedule has type custom, check the format
		recurrence = phSchedule.Spec.Recurrence.Cron
		specParser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		_, err := specParser.Parse(recurrence)
		if err != nil {
			log.Error(err, "phSchedule recurrence format invalid.")
			phSchedule.Status.Valid = false
			phSchedule.Status.Message = "recurrence format invalid"
			phSchedule.Status.NextRunTime = nil
			phSchedule.Status.Active = true

			if err := r.updatePhScheduleStatus(ctx, phSchedule); err != nil {
				return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
			}
			return ctrl.Result{RequeueAfter: 10 * time.Minute}, nil
		}
	} else {
		recurrence = getDefinedRecurrence(recurType)
	}

	if recurType == "inactive" {
		phScheduleCron, ok := r.PhScheduleCronMap[phSchedule.Name]
		if ok { // if the schedule job has cron already, stop and delete it
			phScheduleCron.c.Stop()
			phScheduleCron.c.Remove(phScheduleCron.entryID)
			delete(r.PhScheduleCronMap, phSchedule.Name)
		}
		phSchedule.Status.Valid = true
		phSchedule.Status.Active = false
		phSchedule.Status.NextRunTime = nil
		phSchedule.Status.Message = "This phSchedule is inactive"
		if err := r.updatePhScheduleStatus(ctx, phSchedule); err != nil {
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
		}
		log.Info("the phSchedule is inactive, so delete cron and return")
		return ctrl.Result{RequeueAfter: 10 * time.Minute}, nil
	}

	phScheduleCron, ok := r.PhScheduleCronMap[phSchedule.Name]
	var nextRun metav1.Time
	var entryID cron.EntryID

	_, err := r.buildPhJob(phSchedule)
	if err != nil {
		phSchedule.Status.Valid = false
		phSchedule.Status.Message = err.Error()
		phSchedule.Status.NextRunTime = nil
		phSchedule.Status.Active = true
		if err := r.updatePhScheduleStatus(ctx, phSchedule); err != nil {
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
		}
		return ctrl.Result{RequeueAfter: 10 * time.Minute}, nil
	}

	createPhJob := func() {
		log.Info("phSchedule is triggered, create phJob", "phSchedule", phSchedule.Name)

		phJob, _ := r.buildPhJob(phSchedule)
		err = r.Client.Create(ctx, phJob)

		if err == nil { // create phJob successfully
			log.Info("cron successfully create phJob", "phJob", phJob.ObjectMeta.Name)
		} else { // error occurs when creating pod
			log.Error(err, "cron failed to create phJob", "phJob")
		}
	}

	if !ok { // phSchedule has no cron in controller yet, create one
		log.Info("phSchedule has no cron, create one")
		phScheduleCron = &PhScheduleCron{}

		c := cron.New()
		entryID, _ = c.AddFunc(recurrence, createPhJob)

		phScheduleCron.c = c
		phScheduleCron.entryID = entryID
		phScheduleCron.recurType = recurType
		phScheduleCron.recurrence = recurrence
		phScheduleCron.c.Start()

		r.PhScheduleCronMap[phSchedule.Name] = phScheduleCron

	} else { // phSchedule already has cron in controller, check if the spec changed

		if recurType != phScheduleCron.recurType || recurrence != phScheduleCron.recurrence {
			// clear old cron job entry, and create a new one
			phScheduleCron.c.Stop()
			phScheduleCron.c.Remove(phScheduleCron.entryID)

			entryID, _ = phScheduleCron.c.AddFunc(recurrence, createPhJob)

			phScheduleCron.entryID = entryID
			phScheduleCron.recurType = recurType
			phScheduleCron.recurrence = recurrence
			phScheduleCron.c.Start()
		}

	}

	nextRun = metav1.NewTime(r.PhScheduleCronMap[phSchedule.Name].c.Entry(phScheduleCron.entryID).Next)
	log.Info("reconcile phSchedule", "phSchedule", phSchedule.Name, "recurrence", recurrence, "next", nextRun.String())

	phSchedule.Status.NextRunTime = &nextRun
	phSchedule.Status.Valid = true
	phSchedule.Status.Active = true
	if err := r.updatePhScheduleStatus(ctx, phSchedule); err != nil {
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
	}

	return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
}

func (r *PhScheduleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&primehubv1alpha1.PhSchedule{}).
		Complete(r)
}

// updatePhJobStatus update the status of the phjob in the cluster.
func (r *PhScheduleReconciler) updatePhScheduleStatus(ctx context.Context, phSchedule *primehubv1alpha1.PhSchedule) error {
	log := r.Log.WithValues("phschedule", phSchedule.Name)
	updateTime := time.Now()
	defer func() {
		log.Info("Finished updating PhSchedule ", "UpdateTime", time.Since(updateTime))
	}()
	if err := r.Status().Update(ctx, phSchedule); err != nil {
		log.Error(err, "failed to update PhSchedule status")
		return err
	}
	return nil
}

func getDefinedRecurrence(cronType primehubv1alpha1.RecurrenceType) string {
	switch cronType {
	case primehubv1alpha1.RecurrenceTypeDaily:
		return "0 4 * * *"
	case primehubv1alpha1.RecurrenceTypeWeekly:
		return "0 4 * * 0"
	case primehubv1alpha1.RecurrenceTypeMonthly:
		return "0 4 1 * *"
	}
	return ""
}
