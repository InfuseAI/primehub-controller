/*
Copyright 2022.

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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	primehubv1alpha1 "primehub-controller/ee/api/v1alpha1"
	"primehub-controller/pkg/airgap"
	"primehub-controller/pkg/escapism"
	"primehub-controller/pkg/graphql"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-logr/logr"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkv1 "k8s.io/api/networking/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PhDeploymentReconciler reconciles a PhDeployment object
type PhDeploymentReconciler struct {
	client.Client
	Log                                     logr.Logger
	Scheme                                  *runtime.Scheme
	GraphqlClient                           graphql.AbstractGraphqlClient
	Ingress                                 PhIngress
	PrimehubUrl                             string
	EngineImage                             string
	EngineImagePullPolicy                   corev1.PullPolicy
	ModelStorageInitializerImage            string
	ModelStorageInitializerPullPolicy       corev1.PullPolicy
	MlflowModelStorageInitializerImage      string
	MlflowModelStorageInitializerPullPolicy corev1.PullPolicy
	PhfsEnabled                             bool
	PhfsPVC                                 string
	ImagePrefix                             string
}

type FailedPodStatus struct {
	pod                     string
	conditions              []corev1.PodCondition
	containerStatuses       []corev1.ContainerStatus
	isImageError            bool
	isTerminated            bool
	isUnschedulable         bool
	isModelStorageInitError bool
}

const (
	lastAppliedAnnotation       = "phdeployment.primehub.io/last-applied-configuration"
	ModelStorageInitializerName = "model-storage-initializer"
)

//+kubebuilder:rbac:groups=primehub.io,resources=phdeployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=primehub.io,resources=phdeployments/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=primehub.io,resources=phdeployments/finalizers,verbs=update
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=update;list;watch;create

func (r *PhDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Log.WithValues("phdeployment", req.NamespacedName)

	phDeployment := &primehubv1alpha1.PhDeployment{}
	if err := r.Get(ctx, req.NamespacedName, phDeployment); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("PhDeployment deleted")
		} else {
			logger.Error(err, "Unable to fetch PhDeployment")
		}
		return ctrl.Result{}, nil
	}

	logger.Info("Start Reconcile PhDeployment")
	startTime := time.Now()
	defer func() {
		logger.Info("Finished Reconciling phDeployment ", "phDeployment", phDeployment.Name, "ReconcileTime", time.Since(startTime))
	}()
	phDeployment = phDeployment.DeepCopy()

	if phDeployment.Status.History == nil {
		phDeployment.Status.History = make([]primehubv1alpha1.PhDeploymentHistory, 0)
	}
	if len(phDeployment.Spec.Env) > 0 {
		sort.Slice(phDeployment.Spec.Env, func(i, j int) bool {
			return phDeployment.Spec.Env[i].Name < phDeployment.Spec.Env[j].Name
		})
	}

	// update history
	r.updateHistory(ctx, phDeployment)

	var enableModelDeployment bool = false
	enableModelDeployment, err := r.checkModelDeploymentByGroup(ctx, phDeployment)
	if err != nil {
		logger.Error(err, "check Model Deployment By Group failed")
		if err.Error() == "can not find group in response" {
			r.releaseResources(ctx, phDeployment)
			r.updateStatus(phDeployment, nil, true, "Group Not Found", nil)
		} else {
			r.updateStatus(phDeployment, nil, true, err.Error(), nil)
		}
	} else if enableModelDeployment == false {
		// release the resources
		r.releaseResources(ctx, phDeployment)

		// update the status to failed
		r.updateStatus(phDeployment, nil, true, "The model deployment is not enabled for the selected group", nil)
	} else {
		// reconcile secret
		if err := r.reconcileSecret(ctx, phDeployment); err != nil {
			logger.Error(err, "reconcile Secret error.")
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
		}

		// reconcile deployment
		if err := r.reconcileDeployment(ctx, phDeployment); err != nil {
			logger.Error(err, "reconcile Deployment error.")
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
		}

		// reconcile service
		if err := r.reconcileService(ctx, phDeployment); err != nil {
			logger.Error(err, "reconcile Service error.")
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
		}

		// reconcile ingress
		if err := r.reconcileIngress(ctx, phDeployment); err != nil {
			logger.Error(err, "reconcile Ingress error.")
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
		}
	}

	// get the phdeployment again to minimize the possibility of updating a stale object
	// because the sync loop interval might be too small
	// for operator to get the stale object from the cache
	// this is a common issue in kubebuilder
	// ref: https://github.com/banzaicloud/bank-vaults/issues/364
	phDeploymentToBeUpdated := &primehubv1alpha1.PhDeployment{}
	if err := r.Get(ctx, req.NamespacedName, phDeploymentToBeUpdated); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("PhDeployment deleted")
		} else {
			logger.Error(err, "Unable to fetch PhDeployment")
		}
		return ctrl.Result{}, nil
	}

	// if the status has changed, update the phDeployment status
	if !reflect.DeepEqual(phDeploymentToBeUpdated.Status, phDeployment.Status) {
		phDeploymentToBeUpdated.Status = phDeployment.Status
		if err := r.updatePhDeploymentStatus(ctx, phDeploymentToBeUpdated); err != nil {
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
		}
	}

	if phDeployment.Status.Phase == primehubv1alpha1.DeploymentDeploying {
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	} else {
		return ctrl.Result{RequeueAfter: 2 * time.Minute}, nil
	}
}

func (r *PhDeploymentReconciler) releaseResources(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment) {
	r.deleteDeployment(ctx, getDeploymentKey(phDeployment))
	r.deleteService(ctx, getServiceKey(phDeployment))
	r.deleteIngress(ctx, getIngressKey(phDeployment))
	r.deleteSecret(ctx, getSecretKey(phDeployment))
}

func (r *PhDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&primehubv1alpha1.PhDeployment{}).
		Owns(&v1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkv1.Ingress{}).
		Owns(&corev1.Secret{}).
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

func (r *PhDeploymentReconciler) reconcileSecret(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment) error {
	var err error
	logger := r.Log.WithValues("phDeployment", phDeployment.Name)
	secretKey := getSecretKey(phDeployment)

	if !isPrivateAccess(phDeployment) {
		return nil
	}

	phDeploymentSecret, err := r.getSecret(ctx, secretKey)
	if err != nil && !apierrors.IsNotFound(err) {
		logger.Info("return since k8s GET secret error", "secret", secretKey, "err", err)
		return err
	}

	if phDeploymentSecret == nil {
		logger.Info("Create secret: " + secretKey.Namespace + "/" + secretKey.Name)
		err = r.createSecret(ctx, phDeployment, secretKey)
		if err != nil {
			return err
		}
		logger.Info("Secret " + secretKey.Namespace + "/" + secretKey.Name + " created")
	} else {
		logger.Info("Update secret: " + secretKey.Namespace + "/" + secretKey.Name)
		err = r.updateSecret(ctx, phDeployment, phDeploymentSecret)
		if err != nil {
			return err
		}
		logger.Info("Secret " + secretKey.Namespace + "/" + secretKey.Name + " updated")
	}

	return nil
}

func (r *PhDeploymentReconciler) reconcileDeployment(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment) error {
	// 1. check deployment exists, if no create one
	// 2. update the deployment if spec has been changed
	// 3. update the phDeployment status based on the deployment status

	logger := r.Log.WithValues("phDeployment", phDeployment.Name)
	deploymentKey := getDeploymentKey(phDeployment)

	deployment, err := r.getDeployment(ctx, deploymentKey)
	if err != nil && !apierrors.IsNotFound(err) {
		logger.Error(err, "return since k8s GET deployment error ")
		return err
	}

	if deployment == nil {
		logger.Info("deployment doesn't exist, create one...")
		return r.createDeployment(ctx, phDeployment)
	}

	logger.Info("deployment exist, check the status of current deployment and update phDeployment")
	return r.updateDeployment(ctx, phDeployment, deploymentKey, deployment)
}

func (r *PhDeploymentReconciler) checkModelDeploymentByGroup(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment) (bool, error) {
	logger := r.Log.WithValues("phDeployment", phDeployment.Name)
	enabledDeployment, err := r.GraphqlClient.FetchGroupEnableModelDeployment(phDeployment.Spec.GroupId)
	if err != nil {
		// configuration failed since fetching group from graphql failed
		logger.Info("failed to query group by id: " + phDeployment.Spec.GroupId)
		return false, err
	} else if enabledDeployment == false {
		// configuration failed since modelDeployment is disabled for the group
		logger.Info("Group doesn't enable model deployment flag", "group", phDeployment.Spec.GroupName)

		return false, nil
	}
	return true, nil
}

func (r *PhDeploymentReconciler) createDeployment(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment) error {
	logger := r.Log.WithValues("phDeployment", phDeployment.Name)

	deployment, err := r.buildDeployment(ctx, phDeployment)
	if err != nil {
		// building modelContainer or engineContainer failed
		// configurationError with configurationErrorReason
		logger.Error(err, "build deployment failed")
		r.updateStatus(phDeployment, nil, true, err.Error(), nil)
		return nil
	}

	err = r.Client.Create(ctx, deployment)
	if err != nil {
		logger.Error(err, "return since k8s CREATE deployment error")
		return err
	}

	logger.Info("deployment created", "deployment", deployment.Name)
	return nil
}

func compareEnvVar(a []corev1.EnvVar, b []corev1.EnvVar) bool {
	if len(a) != len(b) {
		return false
	}

	for i, obj := range a {
		if obj.Name != b[i].Name {
			return false
		}
		if obj.Value != b[i].Value {
			return false
		}
	}

	return true
}

func needUpdateDeployment(phDeployment *primehubv1alpha1.PhDeployment, lastAppliedSpec *primehubv1alpha1.PhDeploymentSpec) bool {
	needUpdateDeployment := false

	if lastAppliedSpec == nil {
		return true
	}

	lastAppliedPredictor := lastAppliedSpec.Predictors[0]
	lastAppliedImage := lastAppliedPredictor.ModelImage
	lastAppliedPullSecret := lastAppliedPredictor.ImagePullSecret
	lastAppliedInstanceType := lastAppliedPredictor.InstanceType
	lastAppliedModelURI := lastAppliedPredictor.ModelURI
	lastAppliedEnv := lastAppliedSpec.Env

	if phDeployment.Spec.Predictors[0].ModelImage != lastAppliedImage ||
		phDeployment.Spec.Predictors[0].ImagePullSecret != lastAppliedPullSecret ||
		phDeployment.Spec.Predictors[0].InstanceType != lastAppliedInstanceType ||
		phDeployment.Spec.Predictors[0].ModelURI != lastAppliedModelURI ||
		compareEnvVar(phDeployment.Spec.Env, lastAppliedEnv) == false {

		needUpdateDeployment = true
	}

	return needUpdateDeployment
}

func needScaleDeployment(phDeployment *primehubv1alpha1.PhDeployment, lastAppliedSpec *primehubv1alpha1.PhDeploymentSpec) bool {
	return phDeployment.Spec.Stop != lastAppliedSpec.Stop ||
		phDeployment.Spec.Predictors[0].Replicas != lastAppliedSpec.Predictors[0].Replicas
}

func (r *PhDeploymentReconciler) updateDeployment(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment, deploymentKey client.ObjectKey, currentDeployment *v1.Deployment) error {
	logger := r.Log.WithValues("phDeployment", phDeployment.Name)

	var err error

	lastAppliedSpec, err := GetLastApplied(currentDeployment)

	// Fully update
	if needUpdateDeployment(phDeployment, lastAppliedSpec) {
		// Update: if image, pull secret, instance type, or environment variables change
		logger.Info("phDeployment has been updated, update the deployment to reflect the update.")

		deploymentUpdated, err := r.buildDeployment(ctx, phDeployment)
		if err != nil {
			// building modelContainer or engineContainer failed
			// configurationError with configurationErrorReason
			logger.Error(err, "build deployment failed")
			r.updateStatus(phDeployment, nil, true, err.Error(), nil)
			return nil
		}
		currentDeployment.Spec = deploymentUpdated.Spec

	} else if needScaleDeployment(phDeployment, lastAppliedSpec) {
		//Scale: if stop, replicas changes
		logger.Info("phDeployment has been scaled, scale the deployment to reflect the update.")

		replicas := int32(phDeployment.Spec.Predictors[0].Replicas)
		if phDeployment.Spec.Stop {
			replicas = 0
		}
		currentDeployment.Spec.Replicas = &replicas
	}

	// set the last applied to the current spec in the annotation
	appliedSpec := phDeployment.Spec
	if !reflect.DeepEqual(&appliedSpec, lastAppliedSpec) {
		SetLastApplied(currentDeployment, &appliedSpec)

		// update the deployment when spec has changed
		// because when user update the spec during the deployment is stopped
		// we still need to update the annotation
		// if there is no change among spec.stop, annotation, image, instanceType, secret, replicas
		// currentDeployment will be the same and k8s won't update it.
		err = r.Client.Update(ctx, currentDeployment)
		if err != nil {
			logger.Error(err, "return since k8s UPDATE deployment error")
			return err
		}
		logger.Info("deployment updated", "deployment", currentDeployment.Name)
	}

	// if update/create deployment successfully
	// we get the deployment from cluster again.
	currentDeployment, err = r.getDeployment(ctx, deploymentKey)
	if err != nil {
		logger.Error(err, "return since k8s GET deployment error")
		return err
	}

	// get failed pods if there is any
	failedPods := r.listFailedPods(ctx, phDeployment, currentDeployment)
	return r.updateStatus(phDeployment, currentDeployment, false, "", failedPods)
}

func getSecretKey(p *primehubv1alpha1.PhDeployment) client.ObjectKey {
	return client.ObjectKey{
		Namespace: p.Namespace,
		Name:      "deploy-" + p.Name,
	}
}

func getDeploymentKey(p *primehubv1alpha1.PhDeployment) client.ObjectKey {
	return client.ObjectKey{
		Namespace: p.Namespace,
		Name:      "deploy-" + p.Name,
	}
}

func getServiceKey(p *primehubv1alpha1.PhDeployment) client.ObjectKey {
	return client.ObjectKey{
		Namespace: p.Namespace,
		Name:      "deploy-" + p.Name,
	}
}

func getIngressKey(p *primehubv1alpha1.PhDeployment) client.ObjectKey {
	return client.ObjectKey{
		Namespace: p.Namespace,
		Name:      "deploy-" + p.Name,
	}
}

func isPrivateAccess(phDeployment *primehubv1alpha1.PhDeployment) bool {
	return phDeployment.Spec.Endpoint.AccessType == primehubv1alpha1.DeploymentPrivateEndpoint
}

func (r *PhDeploymentReconciler) updateStatus(phDeployment *primehubv1alpha1.PhDeployment,
	deployment *v1.Deployment,
	configurationError bool,
	configurationErrorReason string,
	failedPods []FailedPodStatus) error {

	if phDeployment.Spec.Stop {
		if deployment != nil && (deployment.Status.AvailableReplicas != 0 || deployment.Status.UpdatedReplicas != 0) {
			phDeployment.Status.Phase = primehubv1alpha1.DeploymentStopping
			phDeployment.Status.Message = "deployment is stopping"
			phDeployment.Status.Replicas = phDeployment.Spec.Predictors[0].Replicas
			phDeployment.Status.AvailableReplicas = 0
		} else {
			phDeployment.Status.Phase = primehubv1alpha1.DeploymentStopped
			phDeployment.Status.Message = "deployment has stopped"
			phDeployment.Status.Replicas = phDeployment.Spec.Predictors[0].Replicas
			phDeployment.Status.AvailableReplicas = 0
		}
		return nil
	}

	// configurationError happens while following events happen
	// 1. group, instanceTyep not found
	// 2. fetch group modelDeployment flag from graphql failed
	// 3. group modelDeployment is disabled
	// the status should be failed.
	if configurationError {
		phDeployment.Status.Phase = primehubv1alpha1.DeploymentFailed
		phDeployment.Status.Message = configurationErrorReason
		phDeployment.Status.Replicas = phDeployment.Spec.Predictors[0].Replicas

		if deployment == nil {
			phDeployment.Status.AvailableReplicas = 0
		} else {
			phDeployment.Status.AvailableReplicas = int(deployment.Status.AvailableReplicas)
		}
		return nil
	}

	// if deployment is still nil
	if deployment == nil {
		phDeployment.Status.Phase = primehubv1alpha1.DeploymentDeploying
		phDeployment.Status.Message = "Deployment is being deployed and not available now"
		phDeployment.Status.Replicas = phDeployment.Spec.Predictors[0].Replicas
		phDeployment.Status.AvailableReplicas = 0

		return nil
	}

	// phDdeployment is deploying if AvailableReplicas != Replicas or UpdatedReplicas != Replicas
	if deployment.Status.AvailableReplicas != *deployment.Spec.Replicas ||
		deployment.Status.UpdatedReplicas != *deployment.Spec.Replicas {

		phDeployment.Status.Phase = primehubv1alpha1.DeploymentDeploying
		phDeployment.Status.Replicas = phDeployment.Spec.Predictors[0].Replicas
		phDeployment.Status.AvailableReplicas = int(deployment.Status.AvailableReplicas)

		// if following events happen, show correct message

		// 1. create replicaset failed,
		// - because request exceeds quota and is denied by admission webhook
		for _, c := range deployment.Status.Conditions {
			if c.Type == v1.DeploymentReplicaFailure && c.Status == corev1.ConditionTrue && c.Reason == "FailedCreate" {
				if strings.Contains(c.Message, "denied the request: ") {
					phDeployment.Status.Message = strings.Split(c.Message, "denied the request: ")[1]
					return nil
				}
			}
		}

		// 2. pod runtime error
		// - wrong image settings (image configurations)
		// - unschedulable pods (cluster resources not enough)
		// - application terminated
		if failedPods != nil {
			for _, p := range failedPods {
				if p.isModelStorageInitError && len(p.containerStatuses) > 0 {
					for _, status := range p.containerStatuses {
						if status.Name == "FailedMount" {
							phDeployment.Status.Message = "Failed because cannot mount NFS server." + r.explain(failedPods)
							return nil
						}
					}
					phDeployment.Status.Message = "Failed because cannot successfully copy the model from the model URI." + r.explain(failedPods)
					return nil
				}
				if p.isImageError {
					phDeployment.Status.Message = "Failed because of wrong image settings." + r.explain(failedPods)
					return nil
				}
				if p.isTerminated {
					phDeployment.Status.Message = "Failed because of pod is terminated" + r.explain(failedPods)
					return nil
				}
				if p.isUnschedulable {
					// even pod is unschedulable, deployment is still deploying, wait for scale down
					phDeployment.Status.Message = "Certain pods unschedulable." + r.explain(failedPods)
					return nil
				}
			}
		}

		phDeployment.Status.Message = "Deployment is being deployed and not available now"

		return nil
	} else {
		phDeployment.Status.Phase = primehubv1alpha1.DeploymentDeployed
		phDeployment.Status.Message = "Deployment is deployed and available now"
		phDeployment.Status.Replicas = phDeployment.Spec.Predictors[0].Replicas
		phDeployment.Status.AvailableReplicas = int(deployment.Status.AvailableReplicas)

		return nil
	}

}

func (r *PhDeploymentReconciler) explain(failedPods []FailedPodStatus) string {
	b := &strings.Builder{}
	for _, p := range failedPods {
		fmt.Fprintf(b, "\npod[%s] failed", p.pod)
		for _, v := range p.conditions {
			fmt.Fprintf(b, "\n  reason: %s, message: %s", v.Reason, v.Message)
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

func (r *PhDeploymentReconciler) listFailedPods(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment, deployment *v1.Deployment) []FailedPodStatus {
	failedPods := make([]FailedPodStatus, 0)

	pods := &corev1.PodList{}
	err := r.Client.List(ctx, pods, client.InNamespace(phDeployment.Namespace), client.MatchingLabels(deployment.Spec.Selector.MatchLabels))

	if err != nil {
		return failedPods
	}

	pods = pods.DeepCopy()
	for _, pod := range pods.Items {
		if pod.DeletionTimestamp != nil {
			continue
		}
		result := FailedPodStatus{
			pod:                     pod.Name,
			conditions:              make([]corev1.PodCondition, 0),
			containerStatuses:       make([]corev1.ContainerStatus, 0),
			isImageError:            false,
			isTerminated:            false,
			isUnschedulable:         false,
			isModelStorageInitError: false,
		}

		for _, c := range pod.Status.Conditions {

			if c.Status == corev1.ConditionFalse && c.Reason != "ContainersNotReady" {
				result.conditions = append(result.conditions, c)
			}

			if c.Reason == "Unschedulable" {
				result.isUnschedulable = true
			}
		}
		for _, c := range pod.Status.InitContainerStatuses {
			var s corev1.ContainerState
			if c.Ready == true { // container is ready, we don't need to capture the error
				s = c.State
			} else {
				if c.LastTerminationState == (corev1.ContainerState{}) {
					s = c.State
				} else {
					s = c.LastTerminationState
				}
			}

			if s.Terminated != nil && s.Terminated.ExitCode != 0 {
				// terminated and exit code is not 0
				if c.Name == ModelStorageInitializerName {
					result.isModelStorageInitError = true
				}
				result.isTerminated = true
				result.containerStatuses = append(result.containerStatuses, c)
			}
			if s.Waiting != nil && (s.Waiting.Reason == "ImagePullBackOff" || s.Waiting.Reason == "ErrImagePull") {
				result.isImageError = true
				result.containerStatuses = append(result.containerStatuses, c)
			}
			if s.Waiting != nil && s.Waiting.Reason == "PodInitializing" {
				events := &corev1.EventList{}
				err := r.Client.List(ctx, events, client.InNamespace(phDeployment.Namespace))
				if err == nil {
					for _, event := range events.Items {
						if event.Reason == "FailedMount" && event.InvolvedObject.Kind == "Pod" && event.InvolvedObject.UID == pod.UID {
							result.isModelStorageInitError = true
							result.containerStatuses = append(result.containerStatuses, corev1.ContainerStatus{
								Name: "FailedMount",
								State: corev1.ContainerState{
									Waiting: &corev1.ContainerStateWaiting{
										Reason:  event.Reason,
										Message: event.Message,
									},
									Running:    nil,
									Terminated: nil,
								},
								LastTerminationState: corev1.ContainerState{},
								Ready:                false,
								RestartCount:         0,
								Image:                "",
								ImageID:              "",
								ContainerID:          "",
							})
						}
					}
				}
			}
		}
		for _, c := range pod.Status.ContainerStatuses {
			var s corev1.ContainerState
			if c.Ready == true { // container is ready, we don't need to capture the error
				s = c.State
			} else {
				if c.LastTerminationState == (corev1.ContainerState{}) {
					s = c.State
				} else {
					s = c.LastTerminationState
				}
			}

			if s.Terminated != nil && s.Terminated.ExitCode != 0 {
				// terminated and exit code is not 0
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

func (r *PhDeploymentReconciler) reconcileService(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment) error {
	log := r.Log.WithValues("phDeployment", phDeployment.Name)
	serviceKey := getServiceKey(phDeployment)

	service, err := r.getService(ctx, serviceKey)
	if err != nil && !apierrors.IsNotFound(err) {
		log.Info("return since k8s GET service error ", "service", serviceKey, "err", err)
		return err
	}

	if service == nil { // service is not found, create one
		log.Info("service doesn't exist, create one...")

		// create service
		service = r.buildService(ctx, phDeployment)

		err := r.Client.Create(ctx, service)
		if err != nil {
			log.Info("return since k8s CREATE service error ", "service", service.Name, "err", err)
		} else { // create service successfully
			log.Info("service created", "service", service.Name)
		}
	}

	return nil
}

func (r *PhDeploymentReconciler) reconcileIngress(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment) error {
	logger := r.Log.WithValues("phDeployment", phDeployment.Name)
	ingressKey := getIngressKey(phDeployment)
	secretKey := getSecretKey(phDeployment)

	phDeploymentIngress, err := r.getIngress(ctx, ingressKey)
	if err != nil && !apierrors.IsNotFound(err) {
		logger.Info("return since k8s GET Ingress error ", "ingress", ingressKey, "err", err)
		return err
	}

	if phDeploymentIngress == nil { // phDeploymentIngress is not found, create one
		logger.Info("Ingress doesn't exist, create one...")
		// Create Ingress
		phDeploymentIngress, err = r.createIngress(ctx, phDeployment, ingressKey, secretKey)
		if err != nil { // create seldonDeployment successfully
			logger.Info("return since k8s CREATE phDeploymentIngress error ", "phDeploymentIngress", phDeploymentIngress.Name, "err", err)
			return err
		}
		logger.Info("phDeploymentIngress created", "phDeploymentIngress", phDeploymentIngress.Name)
	} else {
		logger.Info("Ingress already exist, update it...")
		err = r.updateIngress(ctx, phDeployment, phDeploymentIngress, secretKey)
		if err != nil {
			return err
		}
		logger.Info("Ingress updated", "ingress", phDeploymentIngress.Name)
	}
	// Sync the phDeployment.Status.Endpoint
	phDeployment.Status.Endpoint = r.PrimehubUrl + "/deployment/" + phDeployment.Name + "/api/v1.0/predictions"

	return nil
}

// [Deployment] build deployment of the phDeployment
func (r *PhDeploymentReconciler) buildDeployment(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment) (*v1.Deployment, error) {

	// build model container
	modelContainer, err := r.buildModelContainer(phDeployment)
	if err != nil {
		return nil, err
	}

	predictor := r.buildSeldonPredictor(ctx, phDeployment, modelContainer)

	// build seldon engine container
	engineContainer, err := r.buildEngineContainer(phDeployment, predictor)
	if err != nil {
		return nil, err
	}

	r.adjustResourcesToFitConstraint(engineContainer, modelContainer)

	replicas := int32(phDeployment.Spec.Predictors[0].Replicas)
	if phDeployment.Spec.Stop {
		replicas = 0
	}
	defaultMode := corev1.DownwardAPIVolumeSourceDefaultMode

	imagepullsecrets := []corev1.LocalObjectReference{}
	if phDeployment.Spec.Predictors[0].ImagePullSecret != "" {
		imagepullsecrets = append(imagepullsecrets, corev1.LocalObjectReference{Name: phDeployment.Spec.Predictors[0].ImagePullSecret})
	}

	// get the instancetype from graphql
	predictorInstanceType := phDeployment.Spec.Predictors[0].InstanceType
	predictorImage := phDeployment.Spec.Predictors[0].ModelImage
	var result *graphql.DtoResult
	if result, err = r.GraphqlClient.FetchByUserId(phDeployment.Spec.UserId); err != nil {
		return nil, err
	}
	var spawner *graphql.Spawner
	if spawner, err = graphql.NewSpawnerForModelDeployment(result.Data, phDeployment.Spec.GroupName, predictorInstanceType, predictorImage); err != nil {
		return nil, err
	}
	nodeSelector := make(map[string]string)
	for k, v := range spawner.NodeSelector {
		nodeSelector[k] = v
	}
	tolerations := make([]corev1.Toleration, 0)
	tolerations = append(tolerations, spawner.Tolerations...)

	initContainers := make([]corev1.Container, 0)
	volumes := []corev1.Volume{
		{
			Name: "podinfo",
			VolumeSource: corev1.VolumeSource{
				DownwardAPI: &corev1.DownwardAPIVolumeSource{
					Items: []corev1.DownwardAPIVolumeFile{
						{
							Path:     "annotations",
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.annotations", APIVersion: "v1"},
						},
					},
					DefaultMode: &defaultMode,
				},
			},
		},
	}

	if len(phDeployment.Spec.Predictors[0].ModelURI) > 0 {
		modelURI := phDeployment.Spec.Predictors[0].ModelURI
		initContainersVolumeMount := []corev1.VolumeMount{
			corev1.VolumeMount{
				Name:      "model-storage",
				MountPath: "/mnt/models",
			},
		}

		if len(modelURI) > 4 && strings.HasPrefix(modelURI, "phfs") {
			if !r.PhfsEnabled || len(r.PhfsPVC) <= 0 {
				return nil, errors.New("Phfs is not enabled")
			}
			groupName := strings.ToLower(strings.ReplaceAll(phDeployment.Spec.GroupName, "_", "-"))
			modelURI = strings.ReplaceAll(modelURI, "phfs://", "file:///phfs")
			initContainersVolumeMount = append(initContainersVolumeMount, corev1.VolumeMount{
				MountPath: "/phfs",
				Name:      "phfs",
				SubPath:   "groups/" + groupName,
				ReadOnly:  true,
			})
			volumes = append(volumes, corev1.Volume{
				Name: "phfs",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: r.PhfsPVC,
					},
				},
			})
		}

		if strings.HasPrefix(modelURI, "nfs://") {
			initContainersVolumeMount = append(initContainersVolumeMount, corev1.VolumeMount{
				MountPath: "/nfs",
				Name:      "nfs",
				ReadOnly:  true,
			})

			server, path, err := ExtractNFSConfig(modelURI)
			modelURI = "file:///nfs" + path

			if err != nil {
				return nil, err
			}
			volumes = append(volumes, corev1.Volume{
				Name: "nfs",
				VolumeSource: corev1.VolumeSource{
					NFS: &corev1.NFSVolumeSource{
						Path:     "/",
						ReadOnly: true,
						Server:   server,
					},
				},
			})
		}

		// build the model-storage-init container
		if len(modelURI) > 8 && strings.HasPrefix(modelURI, "models:/") {
			result, err := r.GraphqlClient.FetchByUserId(phDeployment.Spec.UserId)

			if err == nil {
				// mount group volume for copying model data
				groupName := strings.ToLower(strings.ReplaceAll(phDeployment.Spec.GroupName, "_", "-"))
				name := "project-" + groupName
				mountPath := "/project/" + groupName
				volumes = append(volumes, corev1.Volume{
					Name: name,
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: name,
						},
					},
				})

				initContainersVolumeMount = append(initContainersVolumeMount, corev1.VolumeMount{
					MountPath: mountPath,
					Name:      name,
				})

				initContainers = append(initContainers, corev1.Container{
					Name:                     ModelStorageInitializerName,
					Image:                    r.MlflowModelStorageInitializerImage,
					ImagePullPolicy:          r.MlflowModelStorageInitializerPullPolicy,
					Args:                     []string{modelURI},
					VolumeMounts:             initContainersVolumeMount,
					TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
					Env:                      graphql.BuildMlflowEnvironmentVariables(phDeployment.Spec.GroupName, result),
				})
			} else {
				return nil, err
			}
		} else {
			initContainers = append(initContainers, corev1.Container{
				Name:                     ModelStorageInitializerName,
				Image:                    r.ModelStorageInitializerImage,
				ImagePullPolicy:          r.ModelStorageInitializerPullPolicy,
				Args:                     []string{modelURI, "/mnt/models"},
				VolumeMounts:             initContainersVolumeMount,
				TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
				Env: []corev1.EnvVar{
					// Workaround to bypass credential discovery in GCE
					// https://app.clubhouse.io/infuseai/story/14009/cannot-download-files-of-model-uri-in-gke
					{
						Name:  "GCE_METADATA_IP",
						Value: "127.0.0.1",
					},
					{
						Name:  "GCE_METADATA_HOST",
						Value: "127.0.0.1",
					},
				},
			})
		}

		volumes = append(volumes, corev1.Volume{
			Name: "model-storage",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}

	deployment := &v1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deploy-" + phDeployment.Name,
			Namespace: phDeployment.Namespace,
		},
		Spec: v1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":                      "primehub-deployment",
					"primehub.io/phdeployment": phDeployment.Name,
					"primehub.io/group":        escapism.EscapeToPrimehubLabel(phDeployment.Spec.GroupName),
				},
			},
			Strategy: v1.DeploymentStrategy{
				Type: v1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &v1.RollingUpdateDeployment{
					MaxSurge: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 0,
					},
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":                      "primehub-deployment",
						"primehub.io/phdeployment": phDeployment.Name,
						"primehub.io/group":        escapism.EscapeToPrimehubLabel(phDeployment.Spec.GroupName),
					},
					Annotations: map[string]string{
						"prometheus.io/path":   "prometheus",
						"prometheus.io/port":   "8000",
						"prometheus.io/scrape": "true",
						"primehub.io/usage":    r.buildUsageAnnotation(phDeployment),
					},
				},
				Spec: corev1.PodSpec{
					NodeSelector:     nodeSelector,
					Tolerations:      tolerations,
					RestartPolicy:    corev1.RestartPolicyAlways,
					DNSPolicy:        corev1.DNSClusterFirst,
					SchedulerName:    "default-scheduler",
					ImagePullSecrets: imagepullsecrets,
					InitContainers:   initContainers,
					Containers: []corev1.Container{
						*modelContainer,
						*engineContainer,
					},

					Volumes: volumes,
				},
			},
		},
	}

	spec := phDeployment.Spec
	SetLastApplied(deployment, &spec)

	if err := ctrl.SetControllerReference(phDeployment, deployment, r.Scheme); err != nil {
		r.Log.WithValues("phDeployment", phDeployment.Name).Error(err, "failed to set deployment's controller reference to phDeployment")
		return nil, err
	}

	airgap.ApplyAirGapImagePrefix(&deployment.Spec.Template.Spec, r.ImagePrefix)
	return deployment, nil
}

func ExtractNFSConfig(uri string) (string, string, error) {
	if !strings.HasPrefix(uri, "nfs://") {
		return "", "", errors.New("invalid nfs uri " + uri)
	}
	compile, err := regexp.Compile(`nfs://([^/]+)(/.+)?`)
	if err != nil {
		return "", "", err
	}
	groups := compile.FindAllStringSubmatch(uri, -1)
	if len(groups[0]) == 3 {
		path := groups[0][2]
		if path == "" {
			path = "/"
		}
		return groups[0][1], path, nil
	}
	return "", "", errors.New("invalid nfs uri " + uri)
}

func (r *PhDeploymentReconciler) buildUsageAnnotation(phDeployment *primehubv1alpha1.PhDeployment) string {
	usageAnnotations, _ := json.Marshal(map[string]string{
		"component":      "model_deploy",
		"component_name": phDeployment.Name,
		"instance_type":  phDeployment.Spec.Predictors[0].InstanceType,
		"group":          phDeployment.Spec.GroupName})
	return string(usageAnnotations)
}

func (r *PhDeploymentReconciler) adjustResourcesToFitConstraint(engineContainer *corev1.Container, modelContainer *corev1.Container) {

	// adjust containers' resources to fit our resources constraint
	//
	// model container resources share to engine container, according to the following rules:
	// max(100m, instance-cpu * 10%)
	// max(250M, instance-memory * 10%)

	engineCpu := int64(math.Max(float64(modelContainer.Resources.Limits.Cpu().MilliValue())*0.1, 300))
	engineMemory := int64(math.Max(float64(modelContainer.Resources.Limits.Memory().Value())*0.1, 250*1024*1024))
	modelCpu := modelContainer.Resources.Limits.Cpu().MilliValue() - engineCpu
	modelMemory := modelContainer.Resources.Limits.Memory().Value() - engineMemory

	engineResources := corev1.ResourceList{
		corev1.ResourceName(corev1.ResourceCPU):    *resource.NewMilliQuantity(engineCpu, resource.DecimalSI),
		corev1.ResourceName(corev1.ResourceMemory): *resource.NewQuantity(engineMemory, resource.DecimalSI),
	}

	modelResources := corev1.ResourceList{
		corev1.ResourceName(corev1.ResourceCPU):    *resource.NewMilliQuantity(modelCpu, resource.DecimalSI),
		corev1.ResourceName(corev1.ResourceMemory): *resource.NewQuantity(modelMemory, resource.DecimalSI),
	}

	if name, value, ok := getGpuFromLimits(modelContainer.Resources.Limits); ok {
		modelResources[corev1.ResourceName(name)] = value
	}

	engineContainer.Resources = corev1.ResourceRequirements{
		Limits:   engineResources,
		Requests: engineResources,
	}
	modelContainer.Resources = corev1.ResourceRequirements{
		Limits:   modelResources,
		Requests: modelResources,
	}
}

// build predictor for seldon engine
func (r *PhDeploymentReconciler) buildSeldonPredictor(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment, modelContainer *corev1.Container) PredictorSpec {
	var predictor PredictorSpec

	modelType := MODEL
	predictor = PredictorSpec{
		Name: "deploy",
		ComponentSpecs: []*SeldonPodSpec{
			{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						*modelContainer,
					},
				},
			},
		},
		Graph: &PredictiveUnit{
			Name: "model",
			Type: &modelType,
			Endpoint: &Endpoint{
				ServiceHost: "localhost",
				ServicePort: int32(9000),
				Type:        REST,
			},
		},
		Replicas: int32(phDeployment.Spec.Predictors[0].Replicas),
		Labels: map[string]string{
			"app":                      "primehub-deployment",
			"primehub.io/phdeployment": phDeployment.Name,
			"primehub.io/group":        escapism.EscapeToPrimehubLabel(phDeployment.Spec.GroupName),
		},
	}

	return predictor
}

// Create the engine container
func (r *PhDeploymentReconciler) buildEngineContainer(phDeployment *primehubv1alpha1.PhDeployment, predictor PredictorSpec) (*corev1.Container, error) {

	// set traffic to zero to ensure this doesn't cause a diff in the resulting  deployment created
	predictor.Traffic = int32(0)
	predictorB64, err := getEngineVarJson(predictor)
	if err != nil {
		return nil, err
	}

	// engine resources
	cpuQuantity, _ := resource.ParseQuantity("0.1")
	engineResources := &corev1.ResourceRequirements{
		Requests: map[corev1.ResourceName]resource.Quantity{
			corev1.ResourceCPU: cpuQuantity,
		},
	}

	user := int64(8888)
	engineContainer := &corev1.Container{
		Name:            "seldon-container-engine",
		Image:           r.EngineImage,
		ImagePullPolicy: r.EngineImagePullPolicy,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "podinfo",
				MountPath: "/etc/podinfo",
			},
		},
		Args: []string{
			"--sdep",
			phDeployment.Name,
			"--namespace",
			phDeployment.Namespace,
			"--predictor",
			"deploy",
			"--http_port",
			"8000",
			"--grpc_port",
			"5001",
			"--transport",
			"rest",
			"--protocol",
			"seldon",
			"--prometheus_path",
			"/prometheus",
		},
		Env: []corev1.EnvVar{
			{Name: "ENGINE_PREDICTOR", Value: predictorB64},
			{Name: "DEPLOYMENT_NAME", Value: "deploy-" + phDeployment.Name},
			{Name: "DEPLOYMENT_NAMESPACE", Value: phDeployment.Namespace},
			{Name: "ENGINE_SERVER_PORT", Value: "8000"},
			{Name: "ENGINE_SERVER_GRPC_PORT", Value: "5001"},
			{Name: "SELDON_LOG_MESSAGES_EXTERNALLY", Value: "false"},
		},
		Ports: []corev1.ContainerPort{
			{ContainerPort: int32(8000), Name: "rest", Protocol: corev1.ProtocolTCP},
			{ContainerPort: int32(5001), Name: "grpc", Protocol: corev1.ProtocolTCP},
			// export custom metrics port
			// https://docs.seldon.io/projects/seldon-core/en/latest/analytics/analytics.html#metrics-endpoints
			{ContainerPort: int32(6000), Name: "metrics", Protocol: corev1.ProtocolTCP},
			//{ContainerPort: int32(8082), Name: "admin", Protocol: corev1.ProtocolTCP},
			//{ContainerPort: int32(9090), Name: "jmx", Protocol: corev1.ProtocolTCP},
		},
		ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Port: intstr.FromString("rest"), Path: "/ready", Scheme: corev1.URISchemeHTTP}},
			InitialDelaySeconds: 30,
			PeriodSeconds:       10,
			FailureThreshold:    90,
			SuccessThreshold:    1,
			TimeoutSeconds:      60},
		LivenessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Port: intstr.FromString("rest"), Path: "/live", Scheme: corev1.URISchemeHTTP}},
			InitialDelaySeconds: 30,
			PeriodSeconds:       10,
			FailureThreshold:    90,
			SuccessThreshold:    1,
			TimeoutSeconds:      60},
		Lifecycle: &corev1.Lifecycle{
			PreStop: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{Command: []string{"/bin/sh", "-c", "curl 127.0.0.1:8000" + "/pause; /bin/sleep 10"}},
			},
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser: &user,
		},
		Resources: *engineResources,
	}

	return engineContainer, nil
}

func (r PhDeploymentReconciler) buildModelContainer(phDeployment *primehubv1alpha1.PhDeployment) (*corev1.Container, error) {
	var err error
	// currently we only have one predictor, need to change when need to support multiple predictors
	predictorInstanceType := phDeployment.Spec.Predictors[0].InstanceType
	predictorImage := phDeployment.Spec.Predictors[0].ModelImage
	env := phDeployment.Spec.Env

	// get the instancetype from graphql
	var result *graphql.DtoResult
	if result, err = r.GraphqlClient.FetchByUserId(phDeployment.Spec.UserId); err != nil {
		return nil, err
	}
	var spawner *graphql.Spawner
	podSpec := corev1.PodSpec{}
	if spawner, err = graphql.NewSpawnerForModelDeployment(result.Data, phDeployment.Spec.GroupName, predictorInstanceType, predictorImage); err != nil {
		return nil, err
	}

	spawner.BuildPodSpec(&podSpec)

	envs := []corev1.EnvVar{
		{Name: "PREDICTIVE_UNIT_SERVICE_PORT", Value: "9000"},
		{Name: "PREDICTIVE_UNIT_ID", Value: "model"},
		{Name: "PREDICTIVE_UNIT_IMAGE", Value: predictorImage},
		{Name: "PREDICTOR_ID", Value: "deploy"},
		{Name: "SELDON_DEPLOYMENT_ID", Value: "deploy-" + phDeployment.Name},
	}
	volumeMounts := []corev1.VolumeMount{
		corev1.VolumeMount{
			Name:      "podinfo",
			MountPath: "/etc/podinfo",
		},
	}

	if len(phDeployment.Spec.Predictors[0].ModelURI) > 0 {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "model-storage",
			MountPath: "/mnt/models",
		})
		envs = append(envs, corev1.EnvVar{
			Name:  "PREDICTIVE_UNIT_PARAMETERS",
			Value: "[{\"name\":\"model_uri\",\"value\":\"/mnt/models\",\"type\":\"STRING\"}]",
		})
		if len(phDeployment.Spec.Predictors[0].ModelURI) > 4 && strings.HasPrefix(phDeployment.Spec.Predictors[0].ModelURI, "phfs") {
			groupName := strings.ToLower(strings.ReplaceAll(phDeployment.Spec.GroupName, "_", "-"))
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				MountPath: "/phfs",
				Name:      "phfs",
				SubPath:   "groups/" + groupName,
				ReadOnly:  true,
			})
		}
		if strings.HasPrefix(phDeployment.Spec.Predictors[0].ModelURI, "nfs://") {
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				MountPath: "/nfs",
				Name:      "nfs",
				ReadOnly:  true,
			})
		}
	}

	// build mode container
	modelContainer := &corev1.Container{
		Name:  "model",
		Image: strings.TrimSpace(phDeployment.Spec.Predictors[0].ModelImage),
		ReadinessProbe: &corev1.Probe{
			ProbeHandler:        corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromString("http")}},
			InitialDelaySeconds: 30,
			PeriodSeconds:       10,
			FailureThreshold:    90,
			SuccessThreshold:    1,
			TimeoutSeconds:      60,
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler:        corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromString("http")}},
			InitialDelaySeconds: 30,
			PeriodSeconds:       10,
			FailureThreshold:    90,
			SuccessThreshold:    1,
			TimeoutSeconds:      60,
		},
		Lifecycle: &corev1.Lifecycle{
			PreStop: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"/bin/sh", "-c", "/bin/sleep 10"},
				},
			},
		},
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		Env:                      envs,
		VolumeMounts:             volumeMounts,
		Ports: []corev1.ContainerPort{
			{ContainerPort: 9000, Name: "http", Protocol: corev1.ProtocolTCP},
		},
		Resources: podSpec.Containers[0].Resources,
	}
	if len(env) > 0 {
		modelContainer.Env = append(modelContainer.Env, env...)
	}
	return modelContainer, nil
}

// [Service] build service of the phDeployment
func (r *PhDeploymentReconciler) buildService(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment) *corev1.Service {

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deploy-" + phDeployment.Name,
			Namespace: phDeployment.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "http", Port: int32(8000), Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8000)},
				{Name: "grpc", Port: int32(5001), Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(5001)},
			},
			Selector: map[string]string{
				"app":                      "primehub-deployment",
				"primehub.io/phdeployment": phDeployment.Name,
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}

	if err := ctrl.SetControllerReference(phDeployment, service, r.Scheme); err != nil {
		r.Log.WithValues("phDeployment", phDeployment.Name).Error(err, "failed to set service's controller reference to phDeployment")
		return nil
	}
	return service
}

func (r *PhDeploymentReconciler) createSecret(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment, secretKey client.ObjectKey) error {
	cipherText := ""
	for _, c := range phDeployment.Spec.Endpoint.Clients {
		cipherText = fmt.Sprintf("%s%s:%s\n", cipherText, c.Name, c.Token)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretKey.Name,
			Namespace: secretKey.Namespace,
		},
		Data: map[string][]byte{
			"auth": []byte(cipherText),
		},
		Type: corev1.SecretTypeOpaque,
	}

	if err := ctrl.SetControllerReference(phDeployment, secret, r.Scheme); err != nil {
		r.Log.WithValues("phDeployment", phDeployment.Name).Error(err, "failed to set secret's controller reference to phDeployment")
		return err
	}

	err := r.Client.Create(ctx, secret)
	if err != nil {
		return err
	}

	return nil
}

func (r *PhDeploymentReconciler) updateSecret(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment, oldSecret *corev1.Secret) error {
	cipherText := ""
	for _, c := range phDeployment.Spec.Endpoint.Clients {
		cipherText = fmt.Sprintf("%s%s:%s\n", cipherText, c.Name, c.Token)
	}

	secret := oldSecret.DeepCopy()
	secret.Data["auth"] = []byte(cipherText)

	err := r.Client.Update(ctx, secret)
	if err != nil {
		return err
	}

	return nil
}

// get secret of the phDeployment
func (r *PhDeploymentReconciler) getSecret(ctx context.Context, secretKey client.ObjectKey) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	if err := r.Client.Get(ctx, secretKey, secret); err != nil {
		return nil, err
	}
	return secret, nil
}

// delete secret of the phDeployment
func (r *PhDeploymentReconciler) deleteSecret(ctx context.Context, secretKey client.ObjectKey) error {
	secret := &corev1.Secret{}
	if err := r.Client.Get(ctx, secretKey, secret); err != nil {
		if apierrors.IsNotFound(err) { // secret not found
			return nil
		}
		return err
	}

	gracePeriodSeconds := int64(0)
	deleteOptions := client.DeleteOptions{GracePeriodSeconds: &gracePeriodSeconds}

	if err := r.Client.Delete(ctx, secret, &deleteOptions); err != nil {
		return err
	}

	return nil
}

// get deployment of the phDeployment
func (r *PhDeploymentReconciler) getDeployment(ctx context.Context, deploymentKey client.ObjectKey) (*v1.Deployment, error) {
	deployment := &v1.Deployment{}
	if err := r.Client.Get(ctx, deploymentKey, deployment); err != nil {
		return nil, err
	}
	return deployment, nil
}

// delete deployment of the phDeployment
func (r *PhDeploymentReconciler) deleteDeployment(ctx context.Context, deploymentKey client.ObjectKey) error {
	deployment := &v1.Deployment{}

	if err := r.Client.Get(ctx, deploymentKey, deployment); err != nil {
		if apierrors.IsNotFound(err) { // deployment not found
			return nil
		}
		return err
	}

	gracePeriodSeconds := int64(0)
	deleteOptions := client.DeleteOptions{GracePeriodSeconds: &gracePeriodSeconds}

	if err := r.Client.Delete(ctx, deployment, &deleteOptions); err != nil {
		return err
	}

	return nil
}

// scale down deployment to 0 replica
func (r *PhDeploymentReconciler) scaleDownDeployment(ctx context.Context, deployment *v1.Deployment) error {
	log := r.Log.WithValues("deployment", deployment.Name)
	if *deployment.Spec.Replicas == int32(0) {
		// if replicas has already changed to 0, then return
		return nil
	}

	replicas := int32(0)
	deployment.Spec.Replicas = &replicas

	err := r.Client.Update(ctx, deployment)
	if err != nil {
		log.Error(err, "return since k8s UPDATE deployment err ")
		return err
	}
	return nil
}

// [Service] get service of the phDeployment
func (r *PhDeploymentReconciler) getService(ctx context.Context, serviceKey client.ObjectKey) (*corev1.Service, error) {
	service := &corev1.Service{}
	if err := r.Client.Get(ctx, serviceKey, service); err != nil {
		return nil, err
	}
	return service, nil
}

// [Service] delete service of the phDeployment
func (r *PhDeploymentReconciler) deleteService(ctx context.Context, serviceKey client.ObjectKey) error {
	service := &v1.Deployment{}

	if err := r.Client.Get(ctx, serviceKey, service); err != nil {
		if apierrors.IsNotFound(err) { // deployment not found
			return nil
		}
		return err
	}

	gracePeriodSeconds := int64(0)
	deleteOptions := client.DeleteOptions{GracePeriodSeconds: &gracePeriodSeconds}

	if err := r.Client.Delete(ctx, service, &deleteOptions); err != nil {
		return err
	}

	return nil
}

func (r *PhDeploymentReconciler) getIngress(ctx context.Context, ingressKey client.ObjectKey) (*networkv1.Ingress, error) {
	ingress := &networkv1.Ingress{}

	if err := r.Client.Get(ctx, ingressKey, ingress); err != nil {
		return nil, err
	}

	return ingress, nil
}

// build ingress of the phDeployment
func (r *PhDeploymentReconciler) createIngress(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment, ingressKey client.ObjectKey, secretKey client.ObjectKey) (*networkv1.Ingress, error) {

	annotations := r.Ingress.Annotations
	hosts := r.Ingress.Hosts
	ingressTLS := r.Ingress.TLS
	className := r.Ingress.ClassName
	pathTypeImplementationSpecific := networkv1.PathTypeImplementationSpecific

	annotations["nginx.ingress.kubernetes.io/rewrite-target"] = "/$1"
	if isPrivateAccess(phDeployment) {
		annotations["nginx.ingress.kubernetes.io/auth-type"] = "basic"
		annotations["nginx.ingress.kubernetes.io/auth-secret"] = secretKey.Name
		annotations["nginx.ingress.kubernetes.io/auth-secret-type"] = "auth-file"
		annotations["nginx.ingress.kubernetes.io/auth-realm"] = "Authentication Required - "
		annotations["nginx.ingress.kubernetes.io/configuration-snippet"] = `proxy_set_header X-Forwarded-User $remote_user;`
		annotations["nginx.ingress.kubernetes.io/proxy-body-size"] = "10m"
	}

	ingress := &networkv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        ingressKey.Name,
			Namespace:   ingressKey.Namespace,
			Annotations: annotations, // from config
		},
		Spec: networkv1.IngressSpec{
			IngressClassName: &className,
			TLS:              ingressTLS, // from config
			Rules: []networkv1.IngressRule{
				{
					Host: hosts[0], // from config
					IngressRuleValue: networkv1.IngressRuleValue{
						HTTP: &networkv1.HTTPIngressRuleValue{
							Paths: []networkv1.HTTPIngressPath{
								{
									Path: "/deployment/" + phDeployment.Name + "/(.+)",
									Backend: networkv1.IngressBackend{
										Service: &networkv1.IngressServiceBackend{
											Name: ingressKey.Name,
											Port: networkv1.ServiceBackendPort{
												Number: 8000,
											},
										},
									},
									PathType: &pathTypeImplementationSpecific,
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

	err := r.Client.Create(ctx, ingress)
	return ingress, err
}

func (r *PhDeploymentReconciler) updateIngress(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment, oldIngress *networkv1.Ingress, secretKey client.ObjectKey) error {
	shouldUpdate := false
	ingress := oldIngress.DeepCopy()

	if !isPrivateAccess(phDeployment) && ingress.Annotations["nginx.ingress.kubernetes.io/auth-type"] == "basic" {
		// Update ingress from private to public
		delete(ingress.Annotations, "nginx.ingress.kubernetes.io/auth-type")
		delete(ingress.Annotations, "nginx.ingress.kubernetes.io/auth-secret")
		delete(ingress.Annotations, "nginx.ingress.kubernetes.io/auth-secret-type")
		delete(ingress.Annotations, "nginx.ingress.kubernetes.io/auth-realm")
		delete(ingress.Annotations, "nginx.ingress.kubernetes.io/configuration-snippet")
		shouldUpdate = true
	} else if isPrivateAccess(phDeployment) && ingress.Annotations["nginx.ingress.kubernetes.io/auth-type"] != "basic" {
		// Update ingress from public to private
		ingress.Annotations["nginx.ingress.kubernetes.io/auth-type"] = "basic"
		ingress.Annotations["nginx.ingress.kubernetes.io/auth-secret"] = secretKey.Name
		ingress.Annotations["nginx.ingress.kubernetes.io/auth-secret-type"] = "auth-file"
		ingress.Annotations["nginx.ingress.kubernetes.io/auth-realm"] = "Authentication Required"
		ingress.Annotations["nginx.ingress.kubernetes.io/configuration-snippet"] = `proxy_set_header X-Forwarded-User $remote_user;`
		shouldUpdate = true
	}

	if shouldUpdate {
		return r.Client.Update(ctx, ingress)
	}

	return nil
}

// delete the seldonDeployment of the phDeployment
func (r *PhDeploymentReconciler) deleteIngress(ctx context.Context, ingressKey client.ObjectKey) error {
	ingress := &networkv1.Ingress{}
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
func (r *PhDeploymentReconciler) updateHistory(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment) {

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
	} else {
		latestHistory := phDeployment.Status.History[0]
		if !apiequality.Semantic.DeepEqual(phDeployment.Spec, latestHistory.Spec) { // current spec is not the same as latest history

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

	return
}

// Translte the PredictorSpec p in to base64 encoded JSON to feed to engine in env var.
func getEngineVarJson(predictor PredictorSpec) (string, error) {

	// engine doesn't need to know about metadata or explainer
	// leaving these out means they're not part of diffs on main predictor deployments
	for _, compSpec := range predictor.ComponentSpecs {
		compSpec.Metadata.CreationTimestamp = metav1.Time{}
	}
	predictor.Explainer = nil

	str, err := json.Marshal(predictor)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(str), nil
}

func SetLastApplied(obj *v1.Deployment, lastAppliedSpec *primehubv1alpha1.PhDeploymentSpec) error {
	lastAppliedJSON, err := json.Marshal(lastAppliedSpec)
	if err != nil {
		return fmt.Errorf("can't marshal last applied config: %v", err)
	}

	if obj.ObjectMeta.Annotations == nil {
		obj.ObjectMeta.Annotations = make(map[string]string, 1)
	}
	obj.ObjectMeta.Annotations[lastAppliedAnnotation] = string(lastAppliedJSON)
	return nil
}

func GetLastApplied(obj metav1.Object) (*primehubv1alpha1.PhDeploymentSpec, error) {
	lastAppliedJSON := obj.GetAnnotations()[lastAppliedAnnotation]
	if lastAppliedJSON == "" {
		return nil, nil
	}
	lastApplied := &primehubv1alpha1.PhDeploymentSpec{}
	err := json.Unmarshal([]byte(lastAppliedJSON), lastApplied)
	if err != nil {
		return nil, fmt.Errorf("can't unmarshal %q annotation: %v", lastAppliedAnnotation, err)
	}
	return lastApplied, nil
}

//// SetupWithManager sets up the controller with the Manager.
//func (r *PhDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
//	return ctrl.NewControllerManagedBy(mgr).
//		For(&primehubv1alpha1.PhDeployment{}).
//		Complete(r)
//}

func getGpuFromLimits(limits corev1.ResourceList) (corev1.ResourceName, resource.Quantity, bool) {
	if _, ok := limits["nvidia.com/gpu"]; ok {
		return "nvidia.com/gpu", limits["nvidia.com/gpu"], ok
	}
	if _, ok := limits["amd.com/gpu"]; ok {
		return "amd.com/gpu", limits["amd.com/gpu"], ok
	}
	for key, _ := range limits {
		if strings.HasPrefix(string(key), "gpu.intel.com/") {
			return key, limits[key], true
		}
	}
	return "", resource.Quantity{}, false
}
