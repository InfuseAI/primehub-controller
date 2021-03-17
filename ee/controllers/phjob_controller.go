package controllers

import (
	"context"
	"encoding/json"
	phcache "primehub-controller/pkg/cache"
	"primehub-controller/pkg/graphql"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"

	"primehub-controller/pkg/escapism"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	primehubv1alpha1 "primehub-controller/ee/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	// DefaultJobPreparingTimeout is the phJob preparing state timeout value
	DefaultJobPreparingTimeout = time.Duration(180) * time.Second
)

// PhJobReconciler reconciles a PhJob object
type PhJobReconciler struct {
	client.Client
	Log                            logr.Logger
	Scheme                         *runtime.Scheme
	GraphqlClient                  graphql.AbstractGraphqlClient
	WorkingDirSize                 resource.Quantity
	DefaultActiveDeadlineSeconds   int64
	DefaultTTLSecondsAfterFinished int32
	NodeSelector                   map[string]string
	Tolerations                    []corev1.Toleration
	Affinity                       corev1.Affinity
	PhfsEnabled                    bool
	PhfsPVC                        string
	ArtifactEnabled                bool
	ArtifactLimitSizeMb            int32
	ArtifactLimitFiles             int32
	GrantSudo                      bool
	ArtifactRetentionSeconds       int32
	MonitoringAgentImageRepository string
	MonitoringAgentImageTag        string
	MonitoringAgentImagePullPolicy corev1.PullPolicy
	PrimeHubCache                  *phcache.PrimeHubCache
}

// +kubebuilder:rbac:groups=primehub.io,resources=phjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=primehub.io,resources=phjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=jobs,verbs=get;list;watch;create;update;delete

func (r *PhJobReconciler) buildAnnotationsWithUsageMetadata(phJob *primehubv1alpha1.PhJob) map[string]string {
	annotations := make(map[string]string)
	for k, v := range phJob.ObjectMeta.Annotations {
		annotations[k] = v
	}

	usageMetadata, _ := json.Marshal(map[string]string{
		"component":      "job",
		"component_name": phJob.Name,
		"instance_type":  phJob.Spec.InstanceType,
		"group":          phJob.Spec.GroupName,
		"user":           phJob.Spec.UserName,
	})
	annotations["primehub.io/usage"] = string(usageMetadata)
	return annotations
}

func (r *PhJobReconciler) buildPod(phJob *primehubv1alpha1.PhJob) (*corev1.Pod, error) {
	var err error
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        phJob.ObjectMeta.Name,
			Namespace:   phJob.Namespace,
			Annotations: r.buildAnnotationsWithUsageMetadata(phJob),
			Labels:      phJob.ObjectMeta.Labels,
		},
	}

	// Fetch data from graphql
	podSpec := corev1.PodSpec{}
	var result *graphql.DtoResult
	if result, err = r.GraphqlClient.FetchByUserId(phJob.Spec.UserId); err != nil {
		return nil, err
	}

	// Build the podTemplate according to data from graphql and phjob group, instanceType, image settings
	var spawner *graphql.Spawner
	options := graphql.SpawnerForJobOptions{
		WorkingDirSize:           r.WorkingDirSize,
		PhfsEnabled:              r.PhfsEnabled,
		PhfsPVC:                  r.PhfsPVC,
		ArtifactEnabled:          r.ArtifactEnabled,
		ArtifactLimitSizeMb:      r.ArtifactLimitSizeMb,
		ArtifactLimitFiles:       r.ArtifactLimitFiles,
		ArtifactRetentionSeconds: r.ArtifactRetentionSeconds,
		GrantSudo:                r.GrantSudo,
	}
	if spawner, err = graphql.NewSpawnerForJob(result.Data, phJob.Spec.GroupName, phJob.Spec.InstanceType, phJob.Spec.Image, options); err != nil {
		return nil, err
	}

	operatorNodeSelector := r.NodeSelector
	spawner.ApplyNodeSelectorForOperator(operatorNodeSelector)

	operatorTolerations := r.Tolerations
	spawner.ApplyTolerationsForOperator(operatorTolerations)

	operatorAffinity := r.Affinity
	spawner.ApplyAffinityForOperator(operatorAffinity)

	spawner.WithCommand([]string{"/scripts/run-job.sh", "sleep 1\n" + phJob.Spec.Command})

	spawner.BuildPodSpec(&podSpec)
	r.attachMonitoringAgent(phJob, &podSpec)

	podSpec.RestartPolicy = corev1.RestartPolicyNever
	pod.Spec = podSpec
	pod.Labels = map[string]string{
		"app":               "primehub-job",
		"primehub.io/group": escapism.EscapeToPrimehubLabel(phJob.Spec.GroupName),
		"primehub.io/user":  escapism.EscapeToPrimehubLabel(phJob.Spec.UserName),
	}
	location, err := r.PrimeHubCache.FetchTimeZone()
	if err == nil {
		for idx, _ := range pod.Spec.Containers {
			pod.Spec.Containers[idx].Env = append(pod.Spec.Containers[idx].Env, corev1.EnvVar{Name: "TZ", Value: location})
		}
	} else {
		r.Log.Error(err, "cannot get location")
	}

	// add envs for the primehub-job python package (submit a function as a job)
	for idx, _ := range pod.Spec.Containers {
		envsToAppend := []corev1.EnvVar{
			corev1.EnvVar{Name: "GROUP_ID", Value: phJob.Spec.GroupId},
			corev1.EnvVar{Name: "GROUP_NAME", Value: phJob.Spec.GroupName},
			corev1.EnvVar{Name: "JUPYTERHUB_USER", Value: phJob.Spec.UserName},
			corev1.EnvVar{Name: "INSTANCE_TYPE", Value: phJob.Spec.InstanceType},
			corev1.EnvVar{Name: "IMAGE_NAME", Value: phJob.Spec.Image},
		}
		pod.Spec.Containers[idx].Env = append(pod.Spec.Containers[idx].Env, envsToAppend...)
	}

	// mount shared memory volume
	pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
		Name:      "dshm",
		MountPath: "/dev/shm",
	})

	pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
		Name: "dshm",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium: corev1.StorageMediumMemory,
			},
		},
	})

	pod.Spec.InitContainers = append(pod.Spec.InitContainers, corev1.Container{
		Name:            "admission-is-not-found",
		Image:           "admission-is-not-found",
		ImagePullPolicy: "Never",
		Command:         []string{"false"},
	})

	// Owner reference
	if err := ctrl.SetControllerReference(phJob, pod, r.Scheme); err != nil {
		r.Log.WithValues("phjob", phJob.Namespace).Error(err, "failed to set job's controller reference to phjob")
		return nil, err
	}

	return pod, nil
}

func (r *PhJobReconciler) attachMonitoringAgent(phJob *primehubv1alpha1.PhJob, podSpec *corev1.PodSpec) {
	log := r.Log.WithValues("phjob", phJob.Namespace)

	if !r.ArtifactEnabled {
		return
	}

	const monitoringAgentImageRepository = "infuseai/primehub-monitoring-agent"
	const monitoringAgentImageTag = "latest"
	const sharedVolumeName = "monitoring-utils"
	const sharedVolumeMountPath = "/monitoring-utils"

	if r.MonitoringAgentImageRepository == "" {
		r.MonitoringAgentImageRepository = monitoringAgentImageRepository
		log.Info("monitoringAgent.image.repository is not set, use default value: " + r.MonitoringAgentImageRepository)
	}

	if r.MonitoringAgentImageTag == "" {
		r.MonitoringAgentImageTag = monitoringAgentImageTag
		log.Info("monitoringAgent.image.tag is not set, use default value: " + r.MonitoringAgentImageTag)
	}

	if r.MonitoringAgentImagePullPolicy == "" {
		r.MonitoringAgentImagePullPolicy = corev1.PullIfNotPresent
		log.Info("monitoringAgent.image.pullPolicy is not set, use default value: " + string(r.MonitoringAgentImagePullPolicy))
	}

	podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
		Name: sharedVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})

	podSpec.Containers[0].VolumeMounts = append(podSpec.Containers[0].VolumeMounts, corev1.VolumeMount{
		Name:      sharedVolumeName,
		MountPath: sharedVolumeMountPath,
	})

	podSpec.InitContainers = append(podSpec.InitContainers, corev1.Container{
		Name:    "copy-monitoring-utils",
		Command: []string{"cp", "/primehub-monitoring-agent", sharedVolumeMountPath},
		VolumeMounts: []corev1.VolumeMount{{
			Name:      sharedVolumeName,
			MountPath: sharedVolumeMountPath,
		}},
		Image:           r.MonitoringAgentImageRepository + ":" + r.MonitoringAgentImageTag,
		ImagePullPolicy: r.MonitoringAgentImagePullPolicy,
	})
}

func (r *PhJobReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("phjob", req.NamespacedName)
	podkey := client.ObjectKey{
		Namespace: req.Namespace,
		Name:      req.Name,
	}

	errorCheckAfter := 1 * time.Minute
	nextCheck := 1 * time.Minute

	log.Info("start Reconcile")
	startTime := time.Now()
	defer func() {
		log.Info("Finished Reconciling phJob ", "ReconcileTime", time.Since(startTime))
	}()

	// finding phjob
	phJob := &primehubv1alpha1.PhJob{}
	if err := r.Get(ctx, req.NamespacedName, phJob); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("PhJob deleted")
			return ctrl.Result{}, nil
		} else {
			log.Error(err, "unable to fetch PhJob")
			return ctrl.Result{}, nil
		}
	}

	// if user didn't set ActiveDeadlineSeconds, use default value which is 1 day (86400)
	if phJob.Spec.ActiveDeadlineSeconds == nil {
		groupInfo, err := r.PrimeHubCache.FetchGroup(phJob.Spec.GroupId)
		if err != nil {
			log.Error(err, "Failed to get group info")
		}
		if err == nil && groupInfo.JobDefaultActiveDeadlineSeconds != nil {
			phJob.Spec.ActiveDeadlineSeconds = groupInfo.JobDefaultActiveDeadlineSeconds
		} else {
			phJob.Spec.ActiveDeadlineSeconds = &r.DefaultActiveDeadlineSeconds
		}
		err = r.Client.Update(ctx, phJob)
		if err != nil {
			log.Error(err, "Failed to update phJob")
			return ctrl.Result{RequeueAfter: errorCheckAfter}, err
		}
	}

	// if user didn't set TTLSecondsAfterFinished, use default value which is 7 day (604800)
	if phJob.Spec.TTLSecondsAfterFinished == nil {
		phJob.Spec.TTLSecondsAfterFinished = &r.DefaultTTLSecondsAfterFinished
		err := r.Client.Update(ctx, phJob)
		if err != nil {
			log.Error(err, "Failed to update phJob")
			return ctrl.Result{RequeueAfter: errorCheckAfter}, err
		}
	}

	oldStatus := phJob.Status.DeepCopy()
	phJob = phJob.DeepCopy()

	phJobExceedsRequeueLimit := false
	phJobExceedsLimit := false
	var failureReason primehubv1alpha1.PhJobReason
	var failureMessage string

	if phJob.Status.Phase == "" { // New Job, move it into pending state.
		phJob.Status.Phase = primehubv1alpha1.JobPending
		phJob.Status.Reason = primehubv1alpha1.JobReasonOverQuota
		phJob.Status.Message = "Insufficient group/user quota"
	}
	if phJob.Status.Requeued == nil {
		phJob.Status.Requeued = new(int32)
		*phJob.Status.Requeued = int32(0)
	}

	if phJob.Spec.Cancel == true {
		if err := r.deletePod(ctx, podkey); err != nil {
			log.Error(err, "failed to delete pod and cancel phjob")
			return ctrl.Result{}, err
		}

		phJob.Status.Phase = primehubv1alpha1.JobCancelled
		phJob.Status.Reason = primehubv1alpha1.JobReasonCancelled
		phJob.Status.Message = "Cancelled by user"
		if phJob.Status.FinishTime == nil {
			now := metav1.Now()
			phJob.Status.FinishTime = &now
		}

		if !apiequality.Semantic.DeepEqual(*oldStatus, phJob.Status) {
			if err := r.updatePhJobStatus(ctx, phJob); err != nil {
				return ctrl.Result{RequeueAfter: errorCheckAfter}, err
			}
			r.GraphqlClient.NotifyPhJobEvent(phJob.Name, string(phJob.Status.Phase))
		}
		return ctrl.Result{}, nil
	}

	if inFinalPhase(phJob.Status.Phase) { // phJob is in Succeeded, Failed, Unknown status.
		if err := r.handleTTL(ctx, phJob, podkey); err != nil {
			return ctrl.Result{RequeueAfter: errorCheckAfter}, err
		}

		if !apiequality.Semantic.DeepEqual(*oldStatus, phJob.Status) {
			if err := r.updatePhJobStatus(ctx, phJob); err != nil {
				return ctrl.Result{RequeueAfter: errorCheckAfter}, err
			}
		}

		if *phJob.Spec.TTLSecondsAfterFinished != int32(0) && phJob.Status.FinishTime != nil { // has ttl set
			ttlDuration := time.Second * time.Duration(*phJob.Spec.TTLSecondsAfterFinished)
			next := phJob.Status.FinishTime.Add(ttlDuration).Sub(time.Now())
			if next > 0 {
				log.Info("phJob has finished, reconcile it after ttl.", "next", next)
				return ctrl.Result{RequeueAfter: next}, nil
			}
		}
		return ctrl.Result{}, nil
	}

	if phJob.Spec.RequeueLimit != nil {
		// Requeued exceeds limit
		if *phJob.Status.Requeued > *phJob.Spec.RequeueLimit {
			log.Info("phJob has failed because it was requeued more than specified times")
			phJobExceedsRequeueLimit = true
		}
	}

	if phJobExceedsRequeueLimit {
		phJobExceedsLimit = true
		failureReason = primehubv1alpha1.JobReasonOverRequeueLimit
		failureMessage = "phJob has failed because it was requeued more than specified times"
	} else if r.pastActiveDeadline(phJob) {
		phJobExceedsLimit = true
		failureReason = primehubv1alpha1.JobReasonOverActivateDeadline
		failureMessage = "phJob has failed because it was active longer than specified deadline"
	}

	if phJobExceedsLimit { // phJob exceeds requeue limit or active deadline.
		if err := r.deletePod(ctx, podkey); err != nil {
			log.Error(err, "failed to delete pod")
			return ctrl.Result{}, err
		}
		if phJob.Status.FinishTime == nil {
			now := metav1.Now()
			phJob.Status.FinishTime = &now
		}
		phJob.Status.Phase = primehubv1alpha1.JobFailed
		phJob.Status.Reason = failureReason
		phJob.Status.Message = "Job failed due to System Error: " + failureMessage
		if !apiequality.Semantic.DeepEqual(*oldStatus, phJob.Status) {
			if err := r.updatePhJobStatus(ctx, phJob); err != nil {
				return ctrl.Result{RequeueAfter: errorCheckAfter}, err
			}
			r.GraphqlClient.NotifyPhJobEvent(phJob.Name, string(phJob.Status.Phase))
		}

		return ctrl.Result{RequeueAfter: nextCheck}, nil
	}

	if phJob.Status.Phase != primehubv1alpha1.JobPending { // only Job in Preparing, Running will reconcile the pod.
		log.Info("reconcile Pod start")
		if err := r.reconcilePod(ctx, phJob, podkey); err != nil {
			log.Error(err, "reconcilePod error.")
			return ctrl.Result{RequeueAfter: nextCheck}, err
		}
	}

	//no need to update the phjob if the status hasn't changed since last time.
	if !apiequality.Semantic.DeepEqual(*oldStatus, phJob.Status) {
		if err := r.updatePhJobStatus(ctx, phJob); err != nil {
			return ctrl.Result{RequeueAfter: errorCheckAfter}, err
		}
	}

	if inFinalPhase(phJob.Status.Phase) && *phJob.Spec.TTLSecondsAfterFinished != int32(0) { // Job in Succeeded, Failed, Unknown status.
		ttlDuration := time.Second * time.Duration(*phJob.Spec.TTLSecondsAfterFinished)
		log.Info("phJob has finished, reconcile it after ttl.", "ttlDuration", ttlDuration)
		return ctrl.Result{RequeueAfter: ttlDuration}, nil
	} else if phJob.Status.StartTime != nil && *phJob.Spec.ActiveDeadlineSeconds != int64(0) { // Job in Running
		activeDeadlineSeconds := time.Second * time.Duration(*phJob.Spec.ActiveDeadlineSeconds)
		next := phJob.Status.StartTime.Add(activeDeadlineSeconds).Sub(time.Now())
		log.Info("phJob is still running, reconcile it after for active deadline", "next", next)
		return ctrl.Result{RequeueAfter: next}, nil
	}

	return ctrl.Result{RequeueAfter: nextCheck}, nil
}

func (r *PhJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&primehubv1alpha1.PhJob{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}

func (r *PhJobReconciler) reconcilePod(ctx context.Context, phJob *primehubv1alpha1.PhJob, podkey client.ObjectKey) error {
	log := r.Log.WithValues("phjob", phJob.Namespace)
	phJobPreparingTimeout := false
	admissionReject := false
	createPodFailed := false
	createPodFailedReason := ""
	pod, err := r.getPodForJob(ctx, podkey)
	if err != nil && !apierrors.IsNotFound(err) {
		log.Info("return since get pod err ", "podkey", podkey, "err", err)
		return err
	}

	if pod == nil { // pod is not found
		log.Info("could not find existing pod for phJob, creating one...")
		pod, err = r.buildPod(phJob)
		if err == nil {
			err = r.Client.Create(ctx, pod)
		}

		if err == nil { // create pod successfully
			phJob.Status.PodName = podkey.Name
			log.Info("created pod", "pod", pod)
		} else { // error occurs when creating pod
			if !apierrors.IsAlreadyExists(err) {
				admissionReject = r.handleCreatePodFailed(phJob, pod, err)
				if admissionReject == false {
					createPodFailed = true
					createPodFailedReason = err.Error()
				}
			} else {
				log.Error(err, "ignore the pod has been here")
			}
		}
	} else { // pod exist, check the status of current pod and update the phJob
		log.Info("pod exist, check the status of current pod and update the phJob")
		if pod.Status.Phase == corev1.PodPending { // if pod is in pending phase check timeout
			if r.preparingStateTimeout(phJob, pod) {
				log.Info("phJob is in preparing state longer then deadline. Going to requeue the phJob.")
				if err := r.deletePod(ctx, podkey); err != nil {
					log.Error(err, "failed to delete pod after preparing state timeout")
					return err
				}
				phJobPreparingTimeout = true
			}
		}

	}

	return r.updateStatus(ctx, phJob, pod, phJobPreparingTimeout, admissionReject, createPodFailed, createPodFailedReason)

}

// updateStatus updates the status of the phjob based on the pod.
func (r *PhJobReconciler) updateStatus(
	ctx context.Context,
	phJob *primehubv1alpha1.PhJob,
	pod *corev1.Pod,
	phJobPreparingTimeout, admissionReject, createPodFailed bool,
	createPodFailedReason string) error {

	log := r.Log.WithValues("phjob", phJob.Namespace)
	if phJobPreparingTimeout || admissionReject { // phjob preparing state timeout or admission reject, requeue the phjob.
		*phJob.Status.Requeued += int32(1)
		phJob.Status.Phase = primehubv1alpha1.JobPending
		if admissionReject {
			phJob.Status.Reason = primehubv1alpha1.JobReasonOverQuota
			phJob.Status.Message = "Back to pending due to: insufficient group/user quota"
		} else if !strings.Contains(phJob.Status.Message, "Back to pending due to:") {
			phJob.Status.Message = "Back to pending due to: " + phJob.Status.Message
		}
		if err := r.updatePhJobStatus(ctx, phJob); err != nil {
			return err
		}
		return nil
	}

	if createPodFailed {
		phJob.Status.Phase = primehubv1alpha1.JobFailed
		phJob.Status.Reason = primehubv1alpha1.JobReasonPodCreationFailed
		phJob.Status.Message = createPodFailedReason
		now := metav1.Now()
		phJob.Status.FinishTime = &now
		if err := r.updatePhJobStatus(ctx, phJob); err != nil {
			return err
		}
		r.GraphqlClient.NotifyPhJobEvent(phJob.Name, string(phJob.Status.Phase))

		return nil
	}

	if pod.Status.Phase == corev1.PodSucceeded {
		phJob.Status.Phase = primehubv1alpha1.JobSucceeded
		if phJob.Status.FinishTime == nil {
			now := metav1.Now()
			phJob.Status.FinishTime = &now
		}
		phJob.Status.Reason = primehubv1alpha1.JobReasonPodSucceeded
		phJob.Status.Message = "Job completed"
		r.GraphqlClient.NotifyPhJobEvent(phJob.Name, string(phJob.Status.Phase))
	}

	if pod.Status.Phase == corev1.PodFailed {
		phJob.Status.Phase = primehubv1alpha1.JobFailed
		if phJob.Status.FinishTime == nil {
			now := metav1.Now()
			phJob.Status.FinishTime = &now
		}
		if len(pod.Status.ContainerStatuses) > 0 {
			phJob.Status.Reason = primehubv1alpha1.JobReasonPodFailed
			phJob.Status.Message = "Job failed due to " + pod.Status.ContainerStatuses[0].State.Terminated.Reason + ": " + pod.Status.ContainerStatuses[0].State.Terminated.Message
		}
		r.GraphqlClient.NotifyPhJobEvent(phJob.Name, string(phJob.Status.Phase))
	}

	if pod.Status.Phase == corev1.PodUnknown {
		phJob.Status.Phase = primehubv1alpha1.JobUnknown
		if phJob.Status.FinishTime == nil {
			now := metav1.Now()
			phJob.Status.FinishTime = &now
		}
		if len(pod.Status.Conditions) > 0 {
			phJob.Status.Reason = primehubv1alpha1.JobReasonPodUnknown
			phJob.Status.Message = "[" + pod.Status.Conditions[0].Reason + "] " + pod.Status.Conditions[0].Message
		}
		r.GraphqlClient.NotifyPhJobEvent(phJob.Name, string(phJob.Status.Phase))
	}

	if pod.Status.Phase == corev1.PodPending {
		if len(pod.Status.Conditions) > 0 {
			phJob.Status.Reason = primehubv1alpha1.JobReasonPodPending
			phJob.Status.Message = pod.Status.Conditions[0].Message
		}
	}

	if pod.Status.Phase == corev1.PodRunning {
		phJob.Status.Phase = primehubv1alpha1.JobRunning
		// set StartTime.
		if phJob.Status.StartTime == nil {
			now := metav1.Now()
			phJob.Status.StartTime = &now
		}

		// when node lost, pod will keep in running phase,
		// so we should show the correct message
		if pod.Status.Reason == "NodeLost" {
			phJob.Status.Reason = primehubv1alpha1.JobReasonPodRunning
			phJob.Status.Message = pod.Status.Message
		} else {
			phJob.Status.Reason = primehubv1alpha1.JobReasonPodRunning
			phJob.Status.Message = "Job is currently running"
		}
	}

	log.Info("phJob phase: ", "phase", phJob.Status.Phase)
	if err := r.updatePhJobStatus(ctx, phJob); err != nil {
		return err
	}
	return nil
}

// updatePhJobStatus update the status of the phjob in the cluster.
func (r *PhJobReconciler) updatePhJobStatus(ctx context.Context, phJob *primehubv1alpha1.PhJob) error {
	log := r.Log.WithValues("phjob", phJob.Namespace)
	updateTime := time.Now()
	defer func() {
		log.Info("Finished updating PHJob ", "UpdateTime", time.Since(updateTime))
	}()
	if err := r.Status().Update(ctx, phJob); err != nil {
		log.Error(err, "failed to update PhJob status")
		return err
	}
	return nil
}

// handleTTL delete the resources of the terminated phjob after the ttl.
func (r *PhJobReconciler) handleTTL(ctx context.Context, phJob *primehubv1alpha1.PhJob, podkey client.ObjectKey) error {
	log := r.Log.WithValues("phjob", phJob.Namespace)
	if *phJob.Spec.TTLSecondsAfterFinished == int32(0) { // set 0 to disable the cleanup
		return nil
	}

	currentTime := time.Now()
	ttlDuration := time.Second * time.Duration(*phJob.Spec.TTLSecondsAfterFinished)

	if phJob.Status.StartTime == nil && phJob.Status.FinishTime == nil {
		log.Info("phjob in final phase (Succeeded, Failed, Unknown) without finish time and start time, it is incorrect!!!")
		log.Info("will ignore it and do nothing.")
		return nil
	}

	if phJob.Status.FinishTime == nil {
		log.Info("phjob in final phase (Succeeded, Failed, Unknown) without finish time, using start time as finish time")
		phJob.Status.FinishTime = phJob.Status.StartTime
	}

	if currentTime.After(phJob.Status.FinishTime.Add(ttlDuration)) {
		// log.Info("cleanup phJob pod for it has been terminated for a given ttl.")
		err := r.deletePod(ctx, podkey)
		if err != nil {
			if apierrors.IsNotFound(err) { // pod doesn't exist
				log.Info("cleanup phJob pod failed since pod not found")
				return nil
			}
			log.Error(err, "cleanup phJob pod error, will reconcile it after 1 min.")
			return err
		}
		// log.Info("cleanup phJob pod succeeded.")
		return nil
	}

	return nil
}

// check whether the pod is in pending state longer than deadline
func (r *PhJobReconciler) preparingStateTimeout(phJob *primehubv1alpha1.PhJob, pod *corev1.Pod) bool {
	now := metav1.Now()
	start := pod.ObjectMeta.CreationTimestamp.Time
	duration := now.Time.Sub(start)
	return duration >= DefaultJobPreparingTimeout
}

// check whether the job has ActiveDeadlineSeconds field set and if it is exceeded.
func (r *PhJobReconciler) pastActiveDeadline(phJob *primehubv1alpha1.PhJob) bool {

	// Set the ActiveDeadlineSeconds to 0 to disable the function
	if *phJob.Spec.ActiveDeadlineSeconds == int64(0) || phJob.Status.StartTime == nil {
		return false
	}

	now := metav1.Now()
	start := phJob.Status.StartTime.Time
	duration := now.Time.Sub(start)
	allowedDuration := time.Duration(*phJob.Spec.ActiveDeadlineSeconds) * time.Second
	return duration >= allowedDuration
}

func (r *PhJobReconciler) handleCreatePodFailed(phJob *primehubv1alpha1.PhJob, pod *corev1.Pod, err error) bool {
	log := r.Log.WithValues("phjob", phJob.Namespace)
	errMessage := err.Error()
	if strings.Contains(errMessage, "admission webhook") && strings.Contains(errMessage, "resources-validation-webhook") { // if it's resource validation admission error, requeue
		log.Info("admission denied", "pod", pod)
		log.Info("admission denied messages", "messages", errMessage)
		return true
	} else {
		log.Error(err, "failed to create pod")
		return false
	}
}

func (r *PhJobReconciler) getPodForJob(ctx context.Context, podkey client.ObjectKey) (*corev1.Pod, error) {
	pod := &corev1.Pod{}
	if err := r.Client.Get(ctx, podkey, pod); err != nil {
		return nil, err
	}
	return pod, nil
}

func (r *PhJobReconciler) deletePod(ctx context.Context, podkey client.ObjectKey) error { // delete the pod with given podkey
	pod := &corev1.Pod{}
	if err := r.Client.Get(ctx, podkey, pod); err != nil {
		if apierrors.IsNotFound(err) { // pod doesn't exist
			return nil
		}
		return err
	}

	gracePeriodSeconds := int64(0)
	deleteOptions := client.DeleteOptions{GracePeriodSeconds: &gracePeriodSeconds}
	if err := r.Client.Delete(ctx, pod, &deleteOptions); err != nil {
		return err
	}
	return nil
}

func inFinalPhase(phase primehubv1alpha1.PhJobPhase) bool {
	switch phase {
	case primehubv1alpha1.JobSucceeded, primehubv1alpha1.JobFailed, primehubv1alpha1.JobUnknown, primehubv1alpha1.JobCancelled:
		return true
	default:
		return false
	}
}
