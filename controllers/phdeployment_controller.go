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
	"fmt"
	"strings"
	"time"

	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	primehubv1alpha1 "primehub-controller/api/v1alpha1"
	"primehub-controller/pkg/escapism"
	"primehub-controller/pkg/graphql"
	seldonv1 "primehub-controller/seldon/apis/v1"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
)

// PhDeploymentReconciler reconciles a PhDeployment object
type PhDeploymentReconciler struct {
	client.Client
	Log           logr.Logger
	Scheme        *runtime.Scheme
	GraphqlClient *graphql.GraphqlClient
	Ingress       PhIngress
}

type FailedPodStatus struct {
	pod               string
	conditions        []corev1.PodCondition
	containerStatuses []corev1.ContainerStatus
	isImageError      bool
	isTerminated      bool
	isUnschedulable   bool
}

// +kubebuilder:rbac:groups=primehub.io,resources=phdeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=primehub.io,resources=phdeployments/status,verbs=get;update;patch

func (r *PhDeploymentReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("phdeployment", req.NamespacedName)
	seldonDeploymentKey := client.ObjectKey{
		Namespace: req.Namespace,
		Name:      req.Name,
	}
	ingressKey := client.ObjectKey{
		Namespace: req.Namespace,
		Name:      "deploy-" + req.Name,
	}

	phDeployment := &primehubv1alpha1.PhDeployment{}
	if err := r.Get(ctx, req.NamespacedName, phDeployment); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("PhDeployment deleted")
		} else {
			log.Error(err, "Unable to fetch PhShceduleJob")
		}
		return ctrl.Result{}, nil
	}

	log.Info("Start Reconcile PhDeployment")
	startTime := time.Now()
	defer func() {
		log.Info("Finished Reconciling phDeployment ", "phDeployment", phDeployment.Name, "ReconcileTime", time.Since(startTime))
	}()

	oldStatus := phDeployment.Status.DeepCopy()
	phDeployment = phDeployment.DeepCopy()

	if phDeployment.Status.History == nil {
		phDeployment.Status.History = make([]primehubv1alpha1.PhDeploymentHistory, 0)
	}

	// update history
	hasChanged := r.updateHistory(ctx, phDeployment)

	// phDeployment has been stoped
	if phDeployment.Spec.Stop == true {
		// delete seldonDeployment
		if err := r.deleteSeldonDeployment(ctx, seldonDeploymentKey); err != nil {
			log.Error(err, "failed to delete seldonDeployment and stop phDeployment")
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
		}

		// delete ingress
		// if err := r.deleteIngress(ctx, ingressKey); err != nil {
		// 	log.Error(err, "failed to delete ingress and stop phDeployment")
		// 	return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
		// }

		// update stauts
		phDeployment.Status.Phase = primehubv1alpha1.DeploymentStopped
		phDeployment.Status.Messsage = "deployment has been stopped"
		phDeployment.Status.Replicas = phDeployment.Spec.Predictors[0].Replicas
		phDeployment.Status.AvailableReplicas = 0
		phDeployment.Status.Endpoint = "" // TODO: should be hard coded

		// update history
		r.updateHistory(ctx, phDeployment)

		if !apiequality.Semantic.DeepEqual(oldStatus, phDeployment.Status) {
			if err := r.updatePhDeploymentStatus(ctx, phDeployment); err != nil {
				return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
			}
		}

		return ctrl.Result{}, nil
	}

	// reconcile seldonDeployment
	if err := r.reconcileSeldonDeployment(ctx, phDeployment, seldonDeploymentKey, hasChanged); err != nil {
		log.Error(err, "reconcile Seldon Deployment error.")
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
	}

	if err := r.reconcileIngress(ctx, phDeployment, ingressKey, seldonDeploymentKey); err != nil {
		log.Error(err, "reconcile Ingress error.")
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
	}

	// if the status has changed, update the phDeployment status
	if !apiequality.Semantic.DeepEqual(oldStatus, phDeployment.Status) {
		if err := r.updatePhDeploymentStatus(ctx, phDeployment); err != nil {
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
		}
	}

	return ctrl.Result{RequeueAfter: 3 * time.Minute}, nil
}

func (r *PhDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&primehubv1alpha1.PhDeployment{}).
		Owns(&seldonv1.SeldonDeployment{}).
		Owns(&v1beta1.Ingress{}).
		Complete(r)
}

// updatePhDeploymentStatus update the status of the phDeployment in the cluster.
func (r *PhDeploymentReconciler) updatePhDeploymentStatus(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment) error {
	log := r.Log.WithValues("phDeployment", phDeployment.Name)
	updateTime := time.Now()
	defer func() {
		log.Info("Finished updating PhDeployment ", "UpdateTime", time.Since(updateTime))
	}()
	if err := r.Status().Update(ctx, phDeployment); err != nil {
		log.Error(err, "failed to update PhDeployment status")
		return err
	}
	return nil
}

func (r *PhDeploymentReconciler) reconcileSeldonDeployment(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment, seldonDeploymentKey client.ObjectKey, hasChanged bool) error {
	// 1. check seldonDeployment exists, if no create one
	// 2. update the seldonDeployment if spec has been changed
	// 3. update the phDeployment status based on the sseldonDeployment status
	// 4. currently, the phDeployment failed if seldonDeployment failed or it is not available for over 5 mins
	log := r.Log.WithValues("phDeployment", phDeployment.Name)

	seldonDeploymentAvailableTimeout := false
	reconcilationFailed := false
	reconcilationFailedReason := ""

	resourceInvalid := false
	resourceInvalidReason := ""

	seldonDeployment, err := r.getSeldonDeployment(ctx, seldonDeploymentKey)
	if err != nil && !apierrors.IsNotFound(err) {
		log.Error(err, "return since GET seldonDeployment error ")
		return err
	}

	// resource constraint verify
	resourceInvalid, err = r.validate(phDeployment)
	if err != nil {
		log.Error(err, "failed to validate phDeployment")
		resourceInvalidReason = err.Error()
	}
	if resourceInvalid == true && resourceInvalidReason == "" {
		resourceInvalidReason = "because your instance type resource limit multiply replicas number is larger than your group quota."
	}

	if resourceInvalid == false { // if resource is valid then create/update seldonDeployment
		if seldonDeployment == nil { // seldonDeployment is not found
			log.Info("SeldonDeployment doesn't exist, create one...")

			seldonDeployment, err := r.buildSeldonDeployment(ctx, phDeployment)

			if err == nil {
				err = r.Client.Create(ctx, seldonDeployment)
			}

			if err == nil { // create seldonDeployment successfully
				log.Info("SeldonDeployment created", "SeldonDeployment", seldonDeployment.Name)
				return nil
			} else { // error occurs when creating or building seldonDeployment
				log.Error(err, "CREATE seldonDeployment error")
				reconcilationFailed = true
				reconcilationFailedReason = err.Error()
			}
		} else { // seldonDeployment exist
			log.Info("SeldonDeployment exist, check the status of current seldonDeployment and update phDeployment")

			if hasChanged {
				log.Info("phDeployment has been updated, update the seldonDeployemt to reflect the update.")
				// build the new seldonDeployment
				seldonDeploymentUpdated, err := r.buildSeldonDeployment(ctx, phDeployment)
				seldonDeployment.Spec = seldonDeploymentUpdated.Spec
				if err == nil {
					err = r.Client.Update(ctx, seldonDeployment)
				}

				if err == nil { // create seldonDeployment successfully
					log.Info("SeldonDeployment updated", "SeldonDeployment", seldonDeployment.Name)
				} else {
					log.Error(err, "Failed to update seldonDeployment")
					reconcilationFailed = true
					reconcilationFailedReason = err.Error()
				}
			} else if r.unAvailableTimeout(phDeployment, seldonDeployment) {
				// check if seldonDeployment is unAvailable for over 5 min
				// we would like to keep the SeldonDeployment object rather than delete it
				// because we need the status messages in its managed pods
				log.Info("SeldonDeployment is not available for over 5 min. Change the phDeployment to failed state.")
				seldonDeploymentAvailableTimeout = true
			}
		}
	}

	if resourceInvalid == false && reconcilationFailed == false { // if resource is valid and  update/create seldonDeployment successfully
		// if the situation is creation, then seldonDeployment comes from buildSeldonDeployment
		// and thus the status will be nil, so we get the seldonDeployment from cluster again.
		seldonDeployment, err = r.getSeldonDeployment(ctx, seldonDeploymentKey)
		if err != nil {
			log.Error(err, "Failed to get created seldonDeployment or it doesn't exist after reconciling seldonDeployment")
			return err
		}
	}
	return r.updateStatus(ctx, phDeployment, seldonDeployment, seldonDeploymentAvailableTimeout, reconcilationFailed, reconcilationFailedReason, resourceInvalid, resourceInvalidReason)
}

func (r *PhDeploymentReconciler) updateStatus(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment, seldonDeployment *seldonv1.SeldonDeployment, seldonDeploymentAvailableTimeout bool, reconcilationFailed bool, reconcilationFailedReason string, resourceInvalid bool, resourceInvalidReason string) error {

	if resourceInvalid {
		phDeployment.Status.Phase = primehubv1alpha1.DeploymentFailed
		phDeployment.Status.Messsage = "Deployment's resource is not valid, " + resourceInvalidReason
		phDeployment.Status.Replicas = phDeployment.Spec.Predictors[0].Replicas
		phDeployment.Status.AvailableReplicas = 0

		return nil
	}

	failedPods := r.listFailedPods(ctx, phDeployment)

	// fast-fail cases:
	// 1. wrong image settings (image configurations)
	// 2. unschedulable pods (cluster resources not enough)
	// 3. application terminated
	for _, p := range failedPods {
		if p.isImageError || p.isTerminated {
			phDeployment.Status.Phase = primehubv1alpha1.DeploymentFailed
			phDeployment.Status.Messsage = "Failed because of wrong image settings." + r.explain(failedPods)
			phDeployment.Status.Replicas = phDeployment.Spec.Predictors[0].Replicas
			phDeployment.Status.AvailableReplicas = 0
			phDeployment.Status.Endpoint = ""
			return nil
		}

		if p.isUnschedulable {
			phDeployment.Status.Phase = primehubv1alpha1.DeploymentFailed
			phDeployment.Status.Messsage = "Failed because of certain pods unschedulable." + r.explain(failedPods)
			phDeployment.Status.Replicas = phDeployment.Spec.Predictors[0].Replicas
			phDeployment.Status.AvailableReplicas = 0
			phDeployment.Status.Endpoint = ""
			return nil
		}
	}

	if seldonDeploymentAvailableTimeout {
		phDeployment.Status.Phase = primehubv1alpha1.DeploymentFailed
		phDeployment.Status.Messsage = "Failed because the deployment is not available for over 5 min" + r.explain(failedPods)
		phDeployment.Status.Replicas = phDeployment.Spec.Predictors[0].Replicas
		phDeployment.Status.AvailableReplicas = 0

		return nil
	}

	if reconcilationFailed { // update / create failed need to reconcile in 1 min
		phDeployment.Status.Phase = primehubv1alpha1.DeploymentFailed
		phDeployment.Status.Messsage = reconcilationFailedReason + r.explain(failedPods)
		phDeployment.Status.Replicas = phDeployment.Spec.Predictors[0].Replicas
		phDeployment.Status.AvailableReplicas = 0

		return fmt.Errorf("reconcile seldonDeployment failed")
	}

	if seldonDeployment.Status.State == seldonv1.StatusStateFailed {
		phDeployment.Status.Phase = primehubv1alpha1.DeploymentFailed
		phDeployment.Status.Messsage = "Failed because the seldon deployment on k8s is failed " + r.explain(failedPods)
		phDeployment.Status.Replicas = phDeployment.Spec.Predictors[0].Replicas
		phDeployment.Status.AvailableReplicas = 0

		return nil
	}

	if seldonDeployment.Status.State == seldonv1.StatusStateAvailable {

		phDeployment.Status.Phase = primehubv1alpha1.DeploymentDeployed
		phDeployment.Status.Messsage = "Deployment is deployed and available now"
		phDeployment.Status.Replicas = phDeployment.Spec.Predictors[0].Replicas
		// TODO: there is only one deployment currently, need to change in the future when enable A/B test
		phDeployment.Status.AvailableReplicas = int(0)
		for _, v := range seldonDeployment.Status.DeploymentStatus {
			phDeployment.Status.AvailableReplicas = int(v.AvailableReplicas)
		}

		return nil
	}

	if seldonDeployment.Status.State == seldonv1.StatusStateCreating {
		phDeployment.Status.Phase = primehubv1alpha1.DeploymentDeploying
		phDeployment.Status.Messsage = "Deployment is being deployed and not available now"
		phDeployment.Status.Replicas = phDeployment.Spec.Predictors[0].Replicas
		// TODO: there is only one deployment currently, need to change in the future when enable A/B test
		phDeployment.Status.AvailableReplicas = int(0)
		for _, v := range seldonDeployment.Status.DeploymentStatus {
			phDeployment.Status.AvailableReplicas = int(v.AvailableReplicas)
		}

		return nil
	}

	return fmt.Errorf("seldonDeployment is in Unknown state.")
}

func (r *PhDeploymentReconciler) explain(failedPods []FailedPodStatus) string {
	b := &strings.Builder{}
	for _, p := range failedPods {
		fmt.Fprintf(b, "\npod[%s] failed", p.pod)
		for _, v := range p.conditions {
			fmt.Fprintf(b, "\n  reason: %s, message %s", v.Reason, v.Message)
		}

		if len(p.containerStatuses) > 0 {
			for _, v := range p.containerStatuses {
				var s corev1.ContainerState
				if v.LastTerminationState == (corev1.ContainerState{}) {
					s = v.State
				} else {
					s = v.LastTerminationState
				}

				if s.Waiting != nil {
					fmt.Fprintf(b, "\n  container state: %s, reason: %s, message %s", "Waiting", s.Waiting.Reason, s.Waiting.Message)
				}
				if s.Terminated != nil {
					fmt.Fprintf(b, "\n  container state: %s, reason: %s, message %s, exit-code: %d", "Terminated", s.Terminated.Reason, s.Terminated.Message, s.Terminated.ExitCode)
				}
			}
		}
	}
	return b.String()
}

func (r *PhDeploymentReconciler) listFailedPods(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment) []FailedPodStatus {
	failedPods := make([]FailedPodStatus, 0)

	pods := &corev1.PodList{}
	err := r.Client.List(ctx, pods, client.InNamespace(phDeployment.Namespace), client.MatchingLabels{"primehub.io/phdeployment": phDeployment.Name})

	if err != nil {
		return failedPods
	}

	pods = pods.DeepCopy()
	for _, pod := range pods.Items {
		result := FailedPodStatus{
			pod:               pod.Name,
			conditions:        make([]corev1.PodCondition, 0),
			containerStatuses: make([]corev1.ContainerStatus, 0),
			isImageError:      false,
			isTerminated:      false,
			isUnschedulable:   false,
		}
		for _, c := range pod.Status.Conditions {
			if c.Status == corev1.ConditionFalse && c.Reason != "ContainersNotReady" {
				result.conditions = append(result.conditions, c)
			}

			if c.Reason == "Unschedulable" {
				result.isUnschedulable = true
			}
		}
		for _, c := range pod.Status.ContainerStatuses {
			var s corev1.ContainerState
			if c.LastTerminationState == (corev1.ContainerState{}) {
				s = c.State
			} else {
				s = c.LastTerminationState
			}

			if s.Terminated != nil {
				result.isTerminated = true
				result.containerStatuses = append(result.containerStatuses, c)
			}
			if s.Waiting != nil && (s.Waiting.Reason == "ImagePullBackOff" || s.Waiting.Reason == "ErrImagePull") {
				result.isImageError = true
				result.containerStatuses = append(result.containerStatuses, c)
			}
		}

		if len(result.conditions) > 0 || len(result.containerStatuses) > 0 {
			failedPods = append(failedPods, result)
		}
	}
	return failedPods
}

func (r *PhDeploymentReconciler) reconcileIngress(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment, ingressKey client.ObjectKey, seldonDeploymentKey client.ObjectKey) error {
	log := r.Log.WithValues("phDeployment", phDeployment.Name)

	phDeploymentIngress, err := r.getIngress(ctx, ingressKey)
	if err != nil && !apierrors.IsNotFound(err) {
		log.Info("return since GET Ingress error ", "ingress", ingressKey, "err", err)
		return err
	}

	if phDeploymentIngress == nil { // phDeploymentIngress is not found, create one
		log.Info("Ingress doesn't exist, create one...")

		// serviceName: pop-deploy2-spec1-predictor1
		// <metadata.name>-<spec.name>-<spec.predictor.name>
		// TODO: Change spec name
		serviceName := phDeployment.Name + "-" + phDeployment.Name + "-deploy"

		// Create Ingress
		phDeploymentIngress, err = r.buildIngress(ctx, phDeployment, serviceName)
		if err == nil {
			err = r.Client.Create(ctx, phDeploymentIngress)
		}

		if err == nil { // create seldonDeployment successfully
			log.Info("phDeploymentIngress created", "phDeploymentIngress", phDeploymentIngress.Name)
		} else {
			log.Info("return since CREATE phDeploymentIngress error ", "phDeploymentIngress", phDeploymentIngress.Name, "err", err)
			return err
		}

		// Sync the phDeployment.Status.Endpoint
		phDeployment.Status.Endpoint = "https://" + r.Ingress.Hosts[0] + "/deployment/" + phDeployment.Name + "/api/v0.1/predictions"
	}

	return nil
}

// build seldonDeployment of the phDeployment
func (r *PhDeploymentReconciler) buildSeldonDeployment(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment) (*seldonv1.SeldonDeployment, error) {
	seldonDeploymentName := phDeployment.Name
	seldonDeploymentNamespace := phDeployment.Namespace

	seldonDeployment := &seldonv1.SeldonDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      seldonDeploymentName,
			Namespace: seldonDeploymentNamespace,
			Labels: map[string]string{
				"app": "primehub-deployment",
			},
		},
		Spec: seldonv1.SeldonDeploymentSpec{
			Name:        phDeployment.Name,
			Predictors:  nil,
			OauthKey:    "",
			OauthSecret: "",
			Protocol:    "",
			Transport:   "",
		},
	}

	var err error

	// Currently we only have one predictor, need to change when need to support multiple predictors
	predictorInstanceType := phDeployment.Spec.Predictors[0].InstanceType
	predictorImage := phDeployment.Spec.Predictors[0].ModelImage

	// Get the instancetype, image from graphql
	var result *graphql.DtoResult
	if result, err = r.GraphqlClient.FetchByUserId(phDeployment.Spec.UserId); err != nil {
		return nil, err
	}
	var spawner *graphql.Spawner
	options := graphql.SpawnerDataOptions{}
	podSpec := corev1.PodSpec{}
	if spawner, err = graphql.NewSpawnerForModelDeployment(result.Data, phDeployment.Spec.GroupName, predictorInstanceType, predictorImage, options); err != nil {
		return nil, err
	}

	spawner.BuildPodSpec(&podSpec)
	podSpec.Containers[0].Name = "model"

	imageSecrets := []corev1.LocalObjectReference{}
	if phDeployment.Spec.Predictors[0].ImagePullSecret != "" {
		imageSecrets = append(
			imageSecrets,
			corev1.LocalObjectReference{
				Name: phDeployment.Spec.Predictors[0].ImagePullSecret,
			},
		)
	}
	podSpec.ImagePullSecrets = imageSecrets

	seldonPodSpec1 := &seldonv1.SeldonPodSpec{
		// we have to remove components name so the image can be updated
		// the deployment of the seldonDeployment name will be spec.predict.hash()
		// Metadata: metav1.ObjectMeta{
		// 	// components name will be used in deployment, use "seldon-"+seldonDeploymentName
		// 	Name: "seldon-" + seldonDeploymentName,
		// },
		Spec: podSpec,
	}
	componentSpecs := make([]*seldonv1.SeldonPodSpec, 0)
	componentSpecs = append(componentSpecs, seldonPodSpec1)

	modelType := seldonv1.MODEL
	graph := &seldonv1.PredictiveUnit{
		Name: "model",
		Type: &modelType,
		Endpoint: &seldonv1.Endpoint{
			Type: seldonv1.REST,
		},
	}

	predictor1 := seldonv1.PredictorSpec{
		Name:           "deploy",
		ComponentSpecs: componentSpecs,
		Graph:          graph,
		Replicas:       int32(phDeployment.Spec.Predictors[0].Replicas),
		Labels: map[string]string{
			"primehub.io/group":        escapism.EscapeToPrimehubLabel(phDeployment.Spec.GroupName),
			"primehub.io/phdeployment": phDeployment.Name,
			"app":                      "primehub-deployment",
		},
	}
	predictors := make([]seldonv1.PredictorSpec, 0)
	predictors = append(predictors, predictor1)

	seldonDeployment.Spec.Predictors = predictors

	// Owner reference
	if err := ctrl.SetControllerReference(phDeployment, seldonDeployment, r.Scheme); err != nil {
		r.Log.WithValues("phDeployment", phDeployment.Name).Error(err, "failed to set seldonDeployment's controller reference to phDeployment")
		return nil, err
	}

	return seldonDeployment, nil
}

func (r *PhDeploymentReconciler) getSeldonDeployment(ctx context.Context, seldonDeploymentKey client.ObjectKey) (*seldonv1.SeldonDeployment, error) {
	seldonDeployment := &seldonv1.SeldonDeployment{}
	if err := r.Client.Get(ctx, seldonDeploymentKey, seldonDeployment); err != nil {
		return nil, err
	}
	return seldonDeployment, nil
}

// delete the seldonDeployment of the phDeployment
func (r *PhDeploymentReconciler) deleteSeldonDeployment(ctx context.Context, seldonDeploymentKey client.ObjectKey) error {
	seldonDeployment := &seldonv1.SeldonDeployment{}
	if err := r.Client.Get(ctx, seldonDeploymentKey, seldonDeployment); err != nil {
		if apierrors.IsNotFound(err) { // seldonDeployment not found
			return nil
		}
		return err
	}

	gracePeriodSeconds := int64(0)
	deleteOptions := client.DeleteOptions{GracePeriodSeconds: &gracePeriodSeconds}

	if err := r.Client.Delete(ctx, seldonDeployment, &deleteOptions); err != nil {
		return err
	}

	return nil
}

// check whether the seldonDeployment is not available for over 5 min
func (r *PhDeploymentReconciler) unAvailableTimeout(phDeployment *primehubv1alpha1.PhDeployment, seldonDeployment *seldonv1.SeldonDeployment) bool {

	timeout := false
	var start metav1.Time

	// if we change the spec, seldonDeployemt will turn into createing again
	// we can't use seldonDeployment creation time
	// because it might have been there for a long time so we use latestHistory time
	if len(phDeployment.Status.History) == 0 {
		start = metav1.NewTime(seldonDeployment.ObjectMeta.CreationTimestamp.Time)
	} else {
		latestHistory := phDeployment.Status.History[0]
		start = latestHistory.Time
	}

	if seldonDeployment.Status.State != seldonv1.StatusStateAvailable {
		now := metav1.Now()
		duration := now.Time.Sub(start.Time)
		if duration >= time.Duration(180)*time.Second { // change to 5 min
			timeout = true
		}
	}

	return timeout
}

func (r *PhDeploymentReconciler) getIngress(ctx context.Context, ingressKey client.ObjectKey) (*v1beta1.Ingress, error) {
	ingress := &v1beta1.Ingress{}

	if err := r.Client.Get(ctx, ingressKey, ingress); err != nil {
		return nil, err
	}

	return ingress, nil
}

// build ingress of the phDeployment
func (r *PhDeploymentReconciler) buildIngress(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment, serviceName string) (*v1beta1.Ingress, error) {
	//var ingressAnnotations map[string]string
	//var hosts []string
	//var ingressTLS []v1beta1.IngressTLS

	annotations := r.Ingress.Annotations
	hosts := r.Ingress.Hosts
	ingressTLS := r.Ingress.TLS

	annotations["nginx.ingress.kubernetes.io/rewrite-target"] = "/$1"

	ingress := &v1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "deploy-" + phDeployment.Name,
			Namespace:   phDeployment.Namespace,
			Annotations: annotations, // from config
		},
		Spec: v1beta1.IngressSpec{
			TLS: ingressTLS, // from config
			Rules: []v1beta1.IngressRule{
				{
					Host: hosts[0], // from config
					IngressRuleValue: v1beta1.IngressRuleValue{
						HTTP: &v1beta1.HTTPIngressRuleValue{
							Paths: []v1beta1.HTTPIngressPath{
								{
									Path: "/deployment/" + phDeployment.Name + "/(.+)",
									Backend: v1beta1.IngressBackend{
										ServiceName: serviceName,
										ServicePort: intstr.IntOrString{
											Type:   intstr.Int,
											IntVal: 8000,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Owner reference
	if err := ctrl.SetControllerReference(phDeployment, ingress, r.Scheme); err != nil {
		r.Log.WithValues("phDeployment", phDeployment.Name).Error(err, "failed to set ingress's controller reference to phDeployment")
		return nil, err
	}

	return ingress, nil
}

// delete the seldonDeployment of the phDeployment
func (r *PhDeploymentReconciler) deleteIngress(ctx context.Context, ingressKey client.ObjectKey) error {
	ingress := &v1beta1.Ingress{}
	if err := r.Client.Get(ctx, ingressKey, ingress); err != nil {
		if apierrors.IsNotFound(err) { // seldonDeployment not found
			return nil
		}
		return err
	}

	gracePeriodSeconds := int64(0)
	deleteOptions := client.DeleteOptions{GracePeriodSeconds: &gracePeriodSeconds}

	if err := r.Client.Delete(ctx, ingress, &deleteOptions); err != nil {
		return err
	}

	return nil
}

// update the history of the status
func (r *PhDeploymentReconciler) updateHistory(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment) bool {

	hasChanged := false

	// Append the history
	// if the spec of phDeployment has changed
	// or this is the version one
	if len(phDeployment.Status.History) == 0 {
		now := metav1.Now()
		history := primehubv1alpha1.PhDeploymentHistory{
			Time: now,
			Spec: phDeployment.Spec,
		}
		phDeployment.Status.History = append(phDeployment.Status.History, history)
		hasChanged = false
	} else {
		latestHistory := phDeployment.Status.History[0]
		if !apiequality.Semantic.DeepEqual(phDeployment.Spec, latestHistory.Spec) { // current spec is not the same as latest history
			hasChanged = true

			now := metav1.Now()
			history := primehubv1alpha1.PhDeploymentHistory{
				Time: now,
				Spec: phDeployment.Spec,
			}
			// append to head
			phDeployment.Status.History = append([]primehubv1alpha1.PhDeploymentHistory{history}, phDeployment.Status.History...)
		}
	}

	if len(phDeployment.Status.History) > 32 {
		phDeployment.Status.History = phDeployment.Status.History[:len(phDeployment.Status.History)-1]
	}

	return hasChanged
}

func (r *PhDeploymentReconciler) validate(phDeployment *primehubv1alpha1.PhDeployment) (bool, error) {
	log := r.Log.WithValues("phDeployment", phDeployment.Name)

	groupInfo, err := r.getGroupInfo(phDeployment.Spec.GroupId)
	if err != nil {
		log.Error(err, "cannot get group info")
		return false, err
	}

	instanceInfo, err := r.getInstanceTypeInfo(phDeployment.Spec.Predictors[0].InstanceType)
	if err != nil {
		log.Error(err, "cannot get instance type info")
		return false, err
	}

	replicas := phDeployment.Spec.Predictors[0].Replicas

	instanceRequestedQuota, err := ConvertToResourceQuotaWithReplicas(instanceInfo.Spec.LimitsCpu, (float32)(instanceInfo.Spec.LimitsGpu), instanceInfo.Spec.LimitsMemory, replicas)
	if err != nil {
		log.Error(err, "cannot get convert instance type resource quota")
		return false, err
	}

	groupQuota, err := ConvertToResourceQuota(groupInfo.ProjectQuotaCpu, groupInfo.ProjectQuotaGpu, groupInfo.ProjectQuotaMemory)
	if err != nil {
		log.Error(err, "cannot get convert instance type resource quota")
		return false, err
	}
	groupInvalid := r.validateResource(instanceRequestedQuota, groupQuota)

	if groupInvalid == true {
		return true, nil
	}

	return false, nil
}

func ConvertToResourceQuotaWithReplicas(cpu float32, gpu float32, memory string, replicas int) (*ResourceQuota, error) {
	var resourceQuota ResourceQuota = *NewResourceQuota()

	cpu = cpu * float32(replicas)
	gpu = gpu * float32(replicas)

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

		memoryLimitTmp := memoryLimit
		for i := 0; i < replicas; i++ { // multiply replicas
			memoryLimit.Add(memoryLimitTmp)
		}

		resourceQuota.memory = &memoryLimit
	}

	return &resourceQuota, nil
}

func (r *PhDeploymentReconciler) getGroupInfo(groupId string) (*graphql.DtoGroup, error) {
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

func (r *PhDeploymentReconciler) getInstanceTypeInfo(instanceTypeId string) (*graphql.DtoInstanceType, error) {
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

func (r *PhDeploymentReconciler) validateResource(requestedQuota *ResourceQuota, resourceQuota *ResourceQuota) bool {
	if (resourceQuota.cpu != nil && requestedQuota.cpu != nil && resourceQuota.cpu.Cmp(*requestedQuota.cpu) == -1) ||
		(resourceQuota.gpu != nil && requestedQuota.gpu != nil && resourceQuota.gpu.Cmp(*requestedQuota.gpu) == -1) ||
		(resourceQuota.memory != nil && requestedQuota.memory != nil && resourceQuota.memory.Cmp(*requestedQuota.memory) == -1) {
		return true
	}

	return false
}
