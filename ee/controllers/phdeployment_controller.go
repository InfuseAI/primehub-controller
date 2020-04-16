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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/go-logr/logr"
	"github.com/prometheus/common/log"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	primehubv1alpha1 "primehub-controller/ee/api/v1alpha1"
	"primehub-controller/pkg/escapism"
	"primehub-controller/pkg/graphql"

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

	deploymentKey := client.ObjectKey{
		Namespace: req.Namespace,
		Name:      req.Name,
	}

	serviceKey := client.ObjectKey{
		Namespace: req.Namespace,
		Name:      "deploy-" + req.Name,
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

		deployment, err := r.getDeployment(ctx, deploymentKey)
		if err != nil && !apierrors.IsNotFound(err) {
			log.Error(err, "return since GET deployment error ")
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
		}

		// scale down deployment
		if err := r.scaleDownDeployment(ctx, deployment); err != nil {
			log.Error(err, "failed to delete deployment and stop phDeployment")
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
		}

		if deployment.Status.AvailableReplicas != 0 || deployment.Status.UpdatedReplicas != 0 {
			phDeployment.Status.Phase = primehubv1alpha1.DeploymentStopping
			phDeployment.Status.Messsage = "deployment is stopping"
			phDeployment.Status.Replicas = phDeployment.Spec.Predictors[0].Replicas
			phDeployment.Status.AvailableReplicas = 0
		} else {
			phDeployment.Status.Phase = primehubv1alpha1.DeploymentStopped
			phDeployment.Status.Messsage = "deployment has stopped"
			phDeployment.Status.Replicas = phDeployment.Spec.Predictors[0].Replicas
			phDeployment.Status.AvailableReplicas = 0
		}

		if !apiequality.Semantic.DeepEqual(oldStatus, phDeployment.Status) {
			if err := r.updatePhDeploymentStatus(ctx, phDeployment); err != nil {
				return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
			}
		}

		return ctrl.Result{}, nil
	}

	// reconcile deployment
	if err := r.reconcileDeployment(ctx, phDeployment, deploymentKey, hasChanged); err != nil {
		log.Error(err, "reconcile Seldon Deployment error.")
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
	}

	// reconcile service
	if err := r.reconcileService(ctx, phDeployment, serviceKey); err != nil {
		log.Error(err, "reconcile Seldon Deployment error.")
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
	}

	// reconcile ingress
	if err := r.reconcileIngress(ctx, phDeployment, ingressKey); err != nil {
		log.Error(err, "reconcile Ingress error.")
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
	}

	// if the status has changed, update the phDeployment status
	if !apiequality.Semantic.DeepEqual(oldStatus, phDeployment.Status) {
		if err := r.updatePhDeploymentStatus(ctx, phDeployment); err != nil {
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
		}
	}

	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *PhDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&primehubv1alpha1.PhDeployment{}).
		Owns(&v1.Deployment{}).
		Owns(&corev1.Service{}).
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

func (r *PhDeploymentReconciler) reconcileDeployment(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment, deploymentKey client.ObjectKey, hasChanged bool) error {
	// 1. check deployment exists, if no create one
	// 2. update the deployment if spec has been changed
	// 3. update the phDeployment status based on the deployment status
	// 4. currently, the phDeployment failed if deployment failed or it is not available for over 5 mins
	log := r.Log.WithValues("phDeployment", phDeployment.Name)

	deploymentAvailableTimeout := false
	reconcilationFailed := false
	reconcilationFailedReason := ""

	deployment, err := r.getDeployment(ctx, deploymentKey)
	if err != nil && !apierrors.IsNotFound(err) {
		log.Error(err, "return since GET deployment error ")
		return err
	}

	if deployment == nil { // deployment is not found
		log.Info("deployment doesn't exist, create one...")
		deployment, err := r.buildDeployment(ctx, phDeployment)

		if err == nil {
			err = r.Client.Create(ctx, deployment)
		}

		if err == nil { // create deployment successfully
			log.Info("deployment created", "deployment", deployment.Name)
			return nil
		} else { // error occurs when creating or building deployment
			log.Error(err, "CREATE deployment error")

			// create failed, return directly
			reconcilationFailedReason = err.Error()

			phDeployment.Status.Phase = primehubv1alpha1.DeploymentFailed
			phDeployment.Status.Messsage = reconcilationFailedReason
			phDeployment.Status.Replicas = phDeployment.Spec.Predictors[0].Replicas
			phDeployment.Status.AvailableReplicas = 0

			return nil
		}
	} else { // deployment exist
		log.Info("deployment exist, check the status of current deployment and update phDeployment")

		if hasChanged {
			log.Info("phDeployment has been updated, update the deployment to reflect the update.")

			// If model image changed, build new deployment.
			// If only replica number changed, update replica numver
			if phDeployment.Status.History[1].Spec.Predictors[0].ModelImage != phDeployment.Spec.Predictors[0].ModelImage {
				deploymentUpdated, err := r.buildDeployment(ctx, phDeployment)
				deployment.Spec = deploymentUpdated.Spec
				if err == nil {
					err = r.Client.Update(ctx, deployment)
				}
			} else if phDeployment.Status.History[1].Spec.Predictors[0].Replicas != phDeployment.Spec.Predictors[0].Replicas {
				replicas := int32(phDeployment.Spec.Predictors[0].Replicas)
				deployment.Spec.Replicas = &replicas
				if err == nil {
					err = r.Client.Update(ctx, deployment)
				}
			}

			if err == nil { // create deployment successfully
				log.Info("deployment updated", "deployment", deployment.Name)
			} else {
				log.Error(err, "Failed to update deployment")
				reconcilationFailed = true
				reconcilationFailedReason = err.Error()
			}
		}

		// check if deployment is unAvailable which means there is no available pod for over 5 min
		if r.unAvailableTimeout(phDeployment, deployment) {
			// we would like to keep the deployment object rather than delete it
			// because we need the status messages in its managed pods
			log.Info("deployment is not available for over 5 min. Change the phDeployment to failed state.")
			deploymentAvailableTimeout = true
		}
	}

	if reconcilationFailed == false { // if update/create deployment successfully
		// if the situation is creation, then deployment comes from buildDeployment
		// and thus the status will be nil, so we get the deployment from cluster again.
		deployment, err = r.getDeployment(ctx, deploymentKey)
		if err != nil {
			log.Error(err, "Failed to get created deployment or it doesn't exist after reconciling deployment")
			return err
		}
	}

	return r.updateStatus(ctx, phDeployment, deployment, deploymentAvailableTimeout, reconcilationFailed, reconcilationFailedReason)
}

func (r *PhDeploymentReconciler) updateStatus(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment, deployment *v1.Deployment, deploymentAvailableTimeout bool, reconcilationFailed bool, reconcilationFailedReason string) error {

	// update deployment failed
	if reconcilationFailed {
		phDeployment.Status.Phase = primehubv1alpha1.DeploymentFailed
		phDeployment.Status.Messsage = reconcilationFailedReason
		phDeployment.Status.Replicas = int(deployment.Status.Replicas)
		phDeployment.Status.AvailableReplicas = int(deployment.Status.AvailableReplicas)

		return nil
	}

	failedPods := r.listFailedPods(ctx, phDeployment, deployment)

	// fast-fail cases:
	// 1. wrong image settings (image configurations)
	// 2. unschedulable pods (cluster resources not enough)
	// 3. application terminated
	for _, p := range failedPods {
		if p.isImageError || p.isTerminated {
			phDeployment.Status.Phase = primehubv1alpha1.DeploymentFailed
			phDeployment.Status.Messsage = "Failed because of wrong image settings." + r.explain(failedPods)
			phDeployment.Status.Replicas = int(deployment.Status.Replicas)
			phDeployment.Status.AvailableReplicas = int(deployment.Status.AvailableReplicas)
			return nil
		}
	}

	for _, p := range failedPods {
		if p.isUnschedulable {
			// even pod is unschedulable, deployment is still deploying, wait for scale down or timeout
			phDeployment.Status.Phase = primehubv1alpha1.DeploymentDeploying
			phDeployment.Status.Messsage = "Certain pods unschedulable." + r.explain(failedPods)
			phDeployment.Status.Replicas = int(deployment.Status.Replicas)
			phDeployment.Status.AvailableReplicas = int(deployment.Status.AvailableReplicas)
			return nil
		}
	}

	if deploymentAvailableTimeout {
		phDeployment.Status.Phase = primehubv1alpha1.DeploymentFailed
		phDeployment.Status.Messsage = "Failed because the deployment is not available for over 5 min" + r.explain(failedPods)
		phDeployment.Status.Replicas = int(deployment.Status.Replicas)
		phDeployment.Status.AvailableReplicas = 0

		return nil
	}

	ready := true
	// deployment is ready only when AvailableReplicas = Replicas and UpdatedReplicas = Replicas
	if deployment.Status.AvailableReplicas != *deployment.Spec.Replicas || deployment.Status.UpdatedReplicas != *deployment.Spec.Replicas {
		ready = false
	}

	if ready == true {
		phDeployment.Status.Phase = primehubv1alpha1.DeploymentDeployed
		phDeployment.Status.Messsage = "phDeployment is deployed and available now"
		phDeployment.Status.Replicas = int(deployment.Status.Replicas)
		phDeployment.Status.AvailableReplicas = int(deployment.Status.AvailableReplicas)

		return nil
	}

	if ready == false {
		phDeployment.Status.Phase = primehubv1alpha1.DeploymentDeploying
		phDeployment.Status.Messsage = "phDeployment is being deployed and not available now"
		phDeployment.Status.Replicas = int(deployment.Status.Replicas)
		phDeployment.Status.AvailableReplicas = int(deployment.Status.AvailableReplicas)

		return nil
	}

	return nil
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

func (r *PhDeploymentReconciler) listFailedPods(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment, deployment *v1.Deployment) []FailedPodStatus {
	failedPods := make([]FailedPodStatus, 0)

	pods := &corev1.PodList{}
	err := r.Client.List(ctx, pods, client.InNamespace(phDeployment.Namespace), client.MatchingLabels(deployment.Spec.Selector.MatchLabels))

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

func (r *PhDeploymentReconciler) reconcileService(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment, serviceKey client.ObjectKey) error {
	log := r.Log.WithValues("phDeployment", phDeployment.Name)

	service, err := r.getService(ctx, serviceKey)
	if err != nil && !apierrors.IsNotFound(err) {
		log.Info("return since GET service error ", "service", serviceKey, "err", err)
		return err
	}

	if service == nil { // service is not found, create one
		log.Info("service doesn't exist, create one...")

		// create service
		service = r.buildService(ctx, phDeployment)

		err := r.Client.Create(ctx, service)
		if err != nil {
			log.Info("return since CREATE service error ", "service", service.Name, "err", err)
		} else { // create service successfully
			log.Info("service created", "service", service.Name)
		}
	}

	return nil
}

func (r *PhDeploymentReconciler) reconcileIngress(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment, ingressKey client.ObjectKey) error {
	log := r.Log.WithValues("phDeployment", phDeployment.Name)

	phDeploymentIngress, err := r.getIngress(ctx, ingressKey)
	if err != nil && !apierrors.IsNotFound(err) {
		log.Info("return since GET Ingress error ", "ingress", ingressKey, "err", err)
		return err
	}

	if phDeploymentIngress == nil { // phDeploymentIngress is not found, create one
		log.Info("Ingress doesn't exist, create one...")

		serviceName := "deploy-" + phDeployment.Name

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
	}
	// Sync the phDeployment.Status.Endpoint
	phDeployment.Status.Endpoint = "https://" + r.Ingress.Hosts[0] + "/deployment/" + phDeployment.Name + "/api/v1.0/predictions"

	return nil
}

// [Deployment] build deployment of the phDeployment
func (r *PhDeploymentReconciler) buildDeployment(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment) (*v1.Deployment, error) {

	// build model container
	modelContainer, err := r.buildModelContainer(phDeployment)
	if err != nil {
		return nil, err
	}

	predictor, err := r.buildSeldonPredictor(ctx, phDeployment, modelContainer)

	// build seldon engine container
	engineContainer, err := r.buildEngineContainer(phDeployment, predictor)
	if err != nil {
		return nil, err
	}

	replicas := int32(phDeployment.Spec.Predictors[0].Replicas)
	defaultMode := corev1.DownwardAPIVolumeSourceDefaultMode

	deployment := &v1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      phDeployment.Name,
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
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyAlways,
					DNSPolicy:     corev1.DNSClusterFirst,
					SchedulerName: "default-scheduler",
					Containers: []corev1.Container{
						*modelContainer,
						*engineContainer,
					},
					Volumes: []corev1.Volume{
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
					},
				},
			},
		},
	}

	if err := ctrl.SetControllerReference(phDeployment, deployment, r.Scheme); err != nil {
		r.Log.WithValues("phDeployment", phDeployment.Name).Error(err, "failed to set deployment's controller reference to phDeployment")
		return nil, err
	}

	return deployment, nil
}

// build predictor for seldon engine
func (r *PhDeploymentReconciler) buildSeldonPredictor(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment, modelContainer *corev1.Container) (PredictorSpec, error) {

	var err error
	var predictor PredictorSpec

	// Currently we only have one predictor, need to change when need to support multiple predictors
	predictorInstanceType := phDeployment.Spec.Predictors[0].InstanceType
	predictorImage := phDeployment.Spec.Predictors[0].ModelImage

	// Get the instancetype, image from graphql
	var result *graphql.DtoResult
	if result, err = r.GraphqlClient.FetchByUserId(phDeployment.Spec.UserId); err != nil {
		return predictor, err
	}
	var spawner *graphql.Spawner
	options := graphql.SpawnerDataOptions{}
	podSpec := corev1.PodSpec{}
	if spawner, err = graphql.NewSpawnerForModelDeployment(result.Data, phDeployment.Spec.GroupName, predictorInstanceType, predictorImage, options); err != nil {
		return predictor, err
	}

	spawner.BuildPodSpec(&podSpec)
	podSpec.Containers[0].Name = "model"

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

	return predictor, nil
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
		Image:           "seldonio/engine:0.4.0",
		ImagePullPolicy: corev1.PullPolicy("IfNotPresent"),
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "podinfo",
				MountPath: "/etc/podinfo",
			},
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
			{ContainerPort: int32(8082), Name: "admin", Protocol: corev1.ProtocolTCP},
			{ContainerPort: int32(9090), Name: "jmx", Protocol: corev1.ProtocolTCP},
		},
		ReadinessProbe: &corev1.Probe{Handler: corev1.Handler{HTTPGet: &corev1.HTTPGetAction{Port: intstr.FromString("admin"), Path: "/ready", Scheme: corev1.URISchemeHTTP}},
			InitialDelaySeconds: 20,
			PeriodSeconds:       5,
			FailureThreshold:    3,
			SuccessThreshold:    1,
			TimeoutSeconds:      60},
		LivenessProbe: &corev1.Probe{Handler: corev1.Handler{HTTPGet: &corev1.HTTPGetAction{Port: intstr.FromString("admin"), Path: "/live", Scheme: corev1.URISchemeHTTP}},
			InitialDelaySeconds: 20,
			PeriodSeconds:       5,
			FailureThreshold:    3,
			SuccessThreshold:    1,
			TimeoutSeconds:      60},
		Lifecycle: &corev1.Lifecycle{
			PreStop: &corev1.Handler{
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

	// get the instancetype from graphql
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

	// build mode container
	modelContainer := &corev1.Container{
		Name:            "model",
		Image:           phDeployment.Spec.Predictors[0].ModelImage,
		ImagePullPolicy: corev1.PullPolicy("IfNotPresent"),
		ReadinessProbe: &corev1.Probe{
			Handler:             corev1.Handler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromString("http")}},
			InitialDelaySeconds: 20,
			PeriodSeconds:       5,
			FailureThreshold:    3,
			SuccessThreshold:    1,
			TimeoutSeconds:      60,
		},
		LivenessProbe: &corev1.Probe{
			Handler:             corev1.Handler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromString("http")}},
			InitialDelaySeconds: 20,
			PeriodSeconds:       5,
			FailureThreshold:    3,
			SuccessThreshold:    1,
			TimeoutSeconds:      60,
		},
		Lifecycle: &corev1.Lifecycle{
			PreStop: &corev1.Handler{
				Exec: &corev1.ExecAction{
					Command: []string{"/bin/sh", "-c", "/bin/sleep 10"},
				},
			},
		},
		Env: []corev1.EnvVar{
			{Name: "PREDICTIVE_UNIT_SERVICE_PORT", Value: "9000"},
			{Name: "PREDICTIVE_UNIT_ID", Value: "model"},
			{Name: "PREDICTOR_ID", Value: "deploy"},
			{Name: "SELDON_DEPLOYMENT_ID", Value: "deploy-" + phDeployment.Name},
		},
		VolumeMounts: []corev1.VolumeMount{
			corev1.VolumeMount{
				Name:      "podinfo",
				MountPath: "/etc/podinfo",
			},
		},
		Ports: []corev1.ContainerPort{
			{ContainerPort: 9000, Name: "http", Protocol: corev1.ProtocolTCP},
		},
		Resources: podSpec.Containers[0].Resources,
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

	if *deployment.Spec.Replicas == int32(0) {
		// if replicas has already changed to 0, then return
		return nil
	}

	replicas := int32(0)
	deployment.Spec.Replicas = &replicas

	err := r.Client.Update(ctx, deployment)
	if err != nil {
		log.Error(err, "return since UPDATE deployment err ")
		return err
	}
	return nil
}

// [Service] get service of the phDeployment
func (r *PhDeploymentReconciler) getService(ctx context.Context, serviceKey client.ObjectKey) (*corev1.Service, error) {
	log.Info("**getService", "serviceKey", serviceKey)
	service := &corev1.Service{}
	if err := r.Client.Get(ctx, serviceKey, service); err != nil {
		log.Info("**getService error", "err", err)
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

// check whether the deployment is not available which it has no any available pod for over 5 min
func (r *PhDeploymentReconciler) unAvailableTimeout(phDeployment *primehubv1alpha1.PhDeployment, deployment *v1.Deployment) bool {

	timeout := false
	var start metav1.Time

	if len(phDeployment.Status.History) == 0 {
		start = metav1.NewTime(deployment.CreationTimestamp.Time)
	} else {
		latestHistory := phDeployment.Status.History[0]
		start = latestHistory.Time
	}

	// if phDeployment in deploying phase for over 5 min
	if phDeployment.Status.Phase == primehubv1alpha1.DeploymentDeploying {
		now := metav1.Now()
		duration := now.Time.Sub(start.Time)
		if duration >= time.Duration(60)*time.Second { // change to 5 min
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
		log.Info(history)
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
