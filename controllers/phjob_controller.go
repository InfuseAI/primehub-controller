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
	"primehub-controller/pkg/graphql"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"

	"primehub-controller/pkg/escapism"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	primehubv1alpha1 "primehub-controller/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	// DefaultJobReadyTimeout is the phJob ready state timeout value
	DefaultJobReadyTimeout = time.Duration(180) * time.Second
)

// PhJobReconciler reconciles a PhJob object
type PhJobReconciler struct {
	client.Client
	Log                            logr.Logger
	Scheme                         *runtime.Scheme
	GraphqlClient                  *graphql.GraphqlClient
	WorkingDirSize                 resource.Quantity
	DefaultActiveDeadlineSeconds   int64
	DefaultTTLSecondsAfterFinished int32
}

func (r *PhJobReconciler) buildPod(phJob *primehubv1alpha1.PhJob) (*corev1.Pod, error) {
	var err error

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "phjob-" + phJob.ObjectMeta.Name,
			Namespace:   phJob.Namespace,
			Annotations: phJob.ObjectMeta.Annotations,
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
	options := graphql.SpawnerDataOptions{
		WorkingDirSize: r.WorkingDirSize,
	}
	if spawner, err = graphql.NewSpawnerByData(result.Data, phJob.Spec.GroupName, phJob.Spec.InstanceType, phJob.Spec.Image, options); err != nil {
		return nil, err
	}
	spawner.WithCommand([]string{"sh", "-c", phJob.Spec.Command})
	spawner.BuildPodSpec(&podSpec)

	podSpec.RestartPolicy = corev1.RestartPolicyNever
	pod.Spec = podSpec
	pod.Labels = map[string]string{
		"app":               "primehub-job",
		"primehub.io/group": escapism.Escape(phJob.Spec.GroupName),
		"primehub.io/user":  escapism.Escape(phJob.Spec.UserName),
	}
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

// +kubebuilder:rbac:groups=primehub.io,resources=phjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=primehub.io,resources=phjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=jobs,verbs=get;list;watch;create;update;delete

func (r *PhJobReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("phjob", req.NamespacedName)
	podkey := client.ObjectKey{
		Namespace: req.Namespace,
		Name:      "phjob-" + req.Name,
	}

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

	oldStatus := phJob.Status.DeepCopy()
	phJob = phJob.DeepCopy()

	// if user didn't set ActiveDeadlineSeconds, use default value which is 1 day (86400)
	if phJob.Spec.ActiveDeadlineSeconds == nil {
		phJob.Spec.ActiveDeadlineSeconds = &r.DefaultActiveDeadlineSeconds
	}

	// if user didn't set TTLSecondsAfterFinished, use default value which is 7 day (604800)
	if phJob.Spec.TTLSecondsAfterFinished == nil {
		phJob.Spec.TTLSecondsAfterFinished = &r.DefaultTTLSecondsAfterFinished
	}

	errorCheckAfter := 1 * time.Minute
	nextCheck := 1 * time.Minute
	phJobExceedsRequeueLimit := false
	phJobExceedsLimit := false
	var failureMessage string

	if phJob.Status.Phase == "" { // New Job, move it into pending state.
		phJob.Status.Phase = primehubv1alpha1.JobPending
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
		if phJob.Status.FinishTime == nil {
			now := metav1.Now()
			phJob.Status.FinishTime = &now
		}

		if !apiequality.Semantic.DeepEqual(*oldStatus, phJob.Status) {
			if err := r.updatePhJobStatus(ctx, phJob); err != nil {
				return ctrl.Result{RequeueAfter: errorCheckAfter}, err
			}
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

		if *phJob.Spec.TTLSecondsAfterFinished != int32(0) { // has ttl set
			ttlDuration := time.Second * time.Duration(*phJob.Spec.TTLSecondsAfterFinished)
			next := phJob.Status.FinishTime.Add(ttlDuration).Sub(time.Now())
			if next > 0 {
				log.Info("phJob has finished, reconcile it after ttl.", "next", next)
				return ctrl.Result{RequeueAfter: next}, nil
			}
		}

		return ctrl.Result{RequeueAfter: nextCheck}, nil
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
		failureMessage = "phJob has failed because it was requeued more than specified times"
	} else if r.pastActiveDeadline(phJob) {
		phJobExceedsLimit = true
		failureMessage = "phJob has failed because it was active longer than specified deadline"
	}

	if phJobExceedsLimit { // phJob exceeds requeue limit or active deadline.
		if err := r.deletePod(ctx, podkey); err != nil {
			log.Error(err, "failed to delete pod when phJob exceeds requeue limit")
			return ctrl.Result{}, err
		}
		if phJob.Status.FinishTime == nil {
			now := metav1.Now()
			phJob.Status.FinishTime = &now
		}
		phJob.Status.Phase = primehubv1alpha1.JobFailed
		phJob.Status.Reason = failureMessage
		if !apiequality.Semantic.DeepEqual(*oldStatus, phJob.Status) {
			if err := r.updatePhJobStatus(ctx, phJob); err != nil {
				return ctrl.Result{RequeueAfter: errorCheckAfter}, err
			}
		}

		return ctrl.Result{RequeueAfter: nextCheck}, nil
	}

	if phJob.Status.Phase != primehubv1alpha1.JobPending { // only Job in Ready, Running will reconcile the pod.
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
	phJobReadyTimeout := false
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
			admissionReject = r.handleCreatePodFailed(phJob, pod, err)
			if admissionReject == false {
				createPodFailed = true
				createPodFailedReason = err.Error()
			}
		}
	} else { // pod exist, check the status of current pod and update the phJob
		log.Info("pod exist, check the status of current pod and update the phJob")
		if pod.Status.Phase == corev1.PodPending { // if pod is in pending phase check timeout
			if r.readyStateTimeout(phJob, pod) {
				log.Info("phJob is in ready state longer then deadline. Going to requeue the phJob.")
				if err := r.deletePod(ctx, podkey); err != nil {
					log.Error(err, "failed to delete pod after ready state timeout")
					return err
				}
				phJobReadyTimeout = true
			}
		}

	}

	return r.updateStatus(ctx, phJob, pod, phJobReadyTimeout, admissionReject, createPodFailed, createPodFailedReason)

}

// updateStatus updates the status of the phjob based on the pod.
func (r *PhJobReconciler) updateStatus(
	ctx context.Context,
	phJob *primehubv1alpha1.PhJob,
	pod *corev1.Pod,
	phJobReadyTimeout, admissionReject, createPodFailed bool,
	createPodFailedReason string) error {

	log := r.Log.WithValues("phjob", phJob.Namespace)
	if phJobReadyTimeout || admissionReject { // phjob ready state timeout or admission reject, requeue the phjob.
		*phJob.Status.Requeued += int32(1)
		phJob.Status.Phase = primehubv1alpha1.JobPending
		if err := r.updatePhJobStatus(ctx, phJob); err != nil {
			return err
		}
		return nil
	}

	if createPodFailed {
		phJob.Status.Phase = primehubv1alpha1.JobFailed
		phJob.Status.Reason = createPodFailedReason
		now := metav1.Now()
		phJob.Status.FinishTime = &now
		if err := r.updatePhJobStatus(ctx, phJob); err != nil {
			return err
		}
		return nil
	}

	// set StartTime.
	phJob.Status.StartTime = getStartTime(pod)
	// log.Info("updateStatus", "pod status", pod.Status)

	if pod.Status.Phase == corev1.PodSucceeded {
		phJob.Status.Phase = primehubv1alpha1.JobSucceeded
		phJob.Status.FinishTime = getFinishTime(pod)
	}
	if pod.Status.Phase == corev1.PodFailed {
		phJob.Status.Phase = primehubv1alpha1.JobFailed
		phJob.Status.FinishTime = getFinishTime(pod)
		if len(pod.Status.ContainerStatuses) > 0 {
			phJob.Status.Reason = pod.Status.ContainerStatuses[0].State.Terminated.Reason
			phJob.Status.Message = pod.Status.ContainerStatuses[0].State.Terminated.Message
		}
	}
	if pod.Status.Phase == corev1.PodUnknown {
		phJob.Status.Phase = primehubv1alpha1.JobUnknown
	}
	if pod.Status.Phase == corev1.PodRunning {
		phJob.Status.Phase = primehubv1alpha1.JobRunning
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
func (r *PhJobReconciler) readyStateTimeout(phJob *primehubv1alpha1.PhJob, pod *corev1.Pod) bool {
	now := metav1.Now()
	start := pod.ObjectMeta.CreationTimestamp.Time
	duration := now.Time.Sub(start)
	return duration >= DefaultJobReadyTimeout
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

func inFinalPhase(phase primehubv1alpha1.PhJobPhase) bool { // TODO: change the name
	switch phase {
	case primehubv1alpha1.JobSucceeded, primehubv1alpha1.JobFailed, primehubv1alpha1.JobUnknown, primehubv1alpha1.JobCancelled:
		return true
	default:
		return false
	}
}

func getStartTime(pod *corev1.Pod) *metav1.Time {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == "main" {
			if cs.State.Running != nil {
				return cs.State.Running.StartedAt.DeepCopy()
			} else if cs.State.Terminated != nil {
				return cs.State.Terminated.StartedAt.DeepCopy()
			}
		}
	}

	return nil
}

func getFinishTime(pod *corev1.Pod) *metav1.Time {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == "main" {
			if cs.State.Terminated != nil {
				return cs.State.Terminated.FinishedAt.DeepCopy()
			}
		}
	}

	return nil
}
