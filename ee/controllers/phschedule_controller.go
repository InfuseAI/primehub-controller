package controllers

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	cron "github.com/robfig/cron/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	primehubv1alpha1 "primehub-controller/ee/api/v1alpha1"
	"primehub-controller/pkg/escapism"
	"primehub-controller/pkg/graphql"
	"primehub-controller/pkg/random"

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
	GraphqlClient     graphql.AbstractGraphqlClient
}

func (r *PhScheduleReconciler) buildPhJob(phSchedule *primehubv1alpha1.PhSchedule) (*primehubv1alpha1.PhJob, error) {
	log := r.Log.WithValues("phschedule", phSchedule.Name)

	t := time.Now().UTC() // generate job name with timestamp based on UTC

	hash, err := random.GenerateRandomString(6)
	phJobName := "job-" + t.Format("200601021504") + "-" + hash

	phJob := &primehubv1alpha1.PhJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:        phJobName,
			Namespace:   phSchedule.Namespace,
			Annotations: phSchedule.ObjectMeta.Annotations,
			Labels: map[string]string{
				"phjob.primehub.io/scheduledBy": phSchedule.Name,
				"primehub.io/group":             escapism.EscapeToPrimehubLabel(phSchedule.Spec.JobTemplate.Spec.GroupName),
				"primehub.io/user":              escapism.EscapeToPrimehubLabel(phSchedule.Spec.JobTemplate.Spec.UserName),
			},
		},
	}
	// (JUST WORKAROUND): check user/group/instancetype/image is correct by using graphql functions
	phJobSpec := phSchedule.Spec.JobTemplate.Spec
	var result *graphql.DtoResult
	if result, err = r.GraphqlClient.FetchByUserId(phJobSpec.UserId); err != nil {
		return nil, err
	}
	options := graphql.SpawnerOptions{PhfsEnabled: false}
	log.Info("info:", "phJobSpec.GroupName", phJobSpec.GroupName, "phJobSpec.InstanceType", phJobSpec.InstanceType, "phJobSpec.Image", phJobSpec.Image)
	if _, err = graphql.NewSpawnerForJob(result.Data, phJobSpec.GroupName, phJobSpec.InstanceType, phJobSpec.Image, options); err != nil {
		return nil, err
	}

	phJob.Spec = phJobSpec
	return phJob, nil

}

// +kubebuilder:rbac:groups=primehub.io,resources=phschedules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=primehub.io,resources=phschedules/status,verbs=get;update;patch

func (r *PhScheduleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("phschedule", req.Name)

	phSchedule := &primehubv1alpha1.PhSchedule{}
	if err := r.Get(ctx, req.NamespacedName, phSchedule); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("PhSchedule deleted")

			// if PhSchedule is deleted, we should also make sure they have no cron in controller
			phScheduleCron, ok := r.PhScheduleCronMap[req.Name]
			if ok {
				phScheduleCron.c.Stop()
				phScheduleCron.c.Remove(phScheduleCron.entryID)
				delete(r.PhScheduleCronMap, req.Name)
			}

			return ctrl.Result{}, nil
		} else {
			log.Error(err, "unable to fetch PhShceduleJob")
		}
	}

	log.Info("start Reconcile")
	startTime := time.Now()
	defer func() {
		log.Info("Finished Reconciling phSchedule ", "phSchedule", phSchedule, "ReconcileTime", time.Since(startTime))
	}()

	phSchedule = phSchedule.DeepCopy()
	recurType := phSchedule.Spec.Recurrence.Type
	var recurrence string
	if recurType == "custom" { // if the phSchedule has type custom, check the format
		recurrence = phSchedule.Spec.Recurrence.Cron
		specParser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		_, err := specParser.Parse(recurrence)
		if err != nil {
			log.Error(err, "phSchedule recurrence format invalid.")
			phSchedule.Status.Invalid = true
			phSchedule.Status.Message = "phSchedule recurrence format invalid"
			phSchedule.Status.NextRunTime = nil

			if err := r.updatePhScheduleStatus(ctx, phSchedule); err != nil {
				return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
			}
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
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
		phSchedule.Status.Invalid = false
		phSchedule.Status.NextRunTime = nil
		phSchedule.Status.Message = "This phSchedule is inactive"
		if err := r.updatePhScheduleStatus(ctx, phSchedule); err != nil {
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
		}
		log.Info("the phSchedule is inactive, so delete cron and return")
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	// fetch timezone from system to sync the timezone

	location, err := r.GraphqlClient.FetchTimeZone()
	if err != nil {
		log.Error(err, "cannot fetch timezone through graphql from timezone")
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	log.Info("current timezone location is: ", "timezone", location)

	phScheduleCron, ok := r.PhScheduleCronMap[phSchedule.Name]
	var nextRun metav1.Time
	var entryID cron.EntryID

	_, err = r.buildPhJob(phSchedule)
	if err != nil {
		phSchedule.Status.Invalid = true
		phSchedule.Status.Message = err.Error()
		phSchedule.Status.NextRunTime = nil
		if err := r.updatePhScheduleStatus(ctx, phSchedule); err != nil {
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
		}
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	createPhJob := func() {
		log.Info("phSchedule is triggered, create phJob", "phSchedule", phSchedule.Name)

		prevPhJobIsRunning := false

		phJobs := primehubv1alpha1.PhJobList{}
		labels := make(map[string]string)
		labels["phjob.primehub.io/scheduledBy"] = phSchedule.Name

		err := r.Client.List(ctx, &phJobs, client.MatchingLabels(labels))
		if err != nil {
			log.Error(err, "phSchedule list previous phjobs failed.")
			return
		}

		for _, phJob := range phJobs.Items {
			if phJob.Status.Phase == primehubv1alpha1.JobPending || phJob.Status.Phase == primehubv1alpha1.JobPreparing || phJob.Status.Phase == primehubv1alpha1.JobRunning {
				prevPhJobIsRunning = true
				log.Info("prev phjob is still running, will not spawn next phjob")
				break
			}
		}

		// if prev phjob doesn't exist or is not running we can spawn the next job
		if prevPhJobIsRunning == false {
			// Maybe the spec isn't changed, but change happens in primehub.
			// In this case, the reconcile will not be triggered
			// and the cron job will still be spawned.
			// So we build the phjob again in the cron function.
			// If there is anything changed in primehub, this will catch the error.
			phJob, err := r.buildPhJob(phSchedule)
			if err != nil {
				log.Error(err, "phSchedule is triggered, but failed when building phJob", "phSchedule", phSchedule.Name)
				phSchedule.Status.Invalid = true
				phSchedule.Status.Message = err.Error()
				phSchedule.Status.NextRunTime = nil
				if err := r.updatePhScheduleStatus(ctx, phSchedule); err != nil {
				}
				return
			}

			err = r.Client.Create(ctx, phJob)

			if err != nil { // error occurs when creating phjob
				log.Error(err, "phSchedule failed to create phJob", "phJob")
			}

			log.Info("phSchedule successfully create phJob", "phJob", phJob.ObjectMeta.Name)

		}

		nextRun = metav1.NewTime(r.PhScheduleCronMap[phSchedule.Name].c.Entry(phScheduleCron.entryID).Next)
		phSchedule.Status.NextRunTime = &nextRun
		if err := r.updatePhScheduleStatus(ctx, phSchedule); err != nil {
		}
	}

	if !ok { // phSchedule has no cron in controller yet, create one
		log.Info("phSchedule has no cron, create one")
		phScheduleCron = &PhScheduleCron{}

		c := cron.New()
		phScheduleCron.c = c

	} else { // phSchedule already has cron in controller, overwite the spec
		log.Info("phSchedule has cron, overwrite the old cron function to make it consistent with the spec")

		// clear old cron job entry, and create a new one
		phScheduleCron.c.Stop()
		phScheduleCron.c.Remove(phScheduleCron.entryID)
	}

	entryID, err = phScheduleCron.c.AddFunc("CRON_TZ="+location+" "+recurrence, createPhJob) // use system timezone
	if err != nil {
		log.Error(err, "controller cannot create cron job")
		phSchedule.Status.Invalid = true
		phSchedule.Status.Message = "controller cannot create cron job. " + err.Error()
		phSchedule.Status.NextRunTime = nil
		if err := r.updatePhScheduleStatus(ctx, phSchedule); err != nil {
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
		}
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	phScheduleCron.entryID = entryID
	phScheduleCron.recurType = recurType
	phScheduleCron.recurrence = recurrence

	phScheduleCron.c.Start()
	r.PhScheduleCronMap[phSchedule.Name] = phScheduleCron

	nextRun = metav1.NewTime(r.PhScheduleCronMap[phSchedule.Name].c.Entry(phScheduleCron.entryID).Next)
	log.Info("reconcile phSchedule", "phSchedule", phSchedule.Name, "recurrence", recurrence, "next", nextRun.String())

	phSchedule.Status.NextRunTime = &nextRun
	phSchedule.Status.Invalid = false
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
