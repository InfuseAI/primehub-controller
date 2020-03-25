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
	"strings"
	"time"

	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	primehubv1alpha1 "primehub-controller/api/v1alpha1"
	"primehub-controller/pkg/graphql"
	seldonv1 "primehub-controller/seldon/apis/v1"

	corev1 "k8s.io/api/core/v1"
)

// PhDeploymentReconciler reconciles a PhDeployment object
type PhDeploymentReconciler struct {
	client.Client
	Log           logr.Logger
	GraphqlClient *graphql.GraphqlClient
}

func (r *PhDeploymentReconciler) buildSeldonDeployment(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment, log logr.Logger) (*seldonv1.SeldonDeployment, error) {
	seldonDeploymentName := phDeployment.Name
	var err error

	// TODO:
	// Currently we only have one predictor, need to change when need to support multiple predictors
	predictorInstanceType := phDeployment.Spec.Predictors[0].InstanceType
	predictorImage := phDeployment.Spec.Predictors[0].ModelImage

	// Get the instancetype, image from graphql
	var result *graphql.DtoResult
	if result, err = r.GraphqlClient.FetchByUserId(phDeployment.Spec.UserId); err != nil {
		return nil, err
	}
	log.Info("info:", "phDeployment.Spec.Predictors[0].InstanceType", predictorInstanceType)

	var spawner *graphql.Spawner
	options := graphql.SpawnerDataOptions{}
	podSpec := corev1.PodSpec{}
	if spawner, err = graphql.NewSpawnerByData(result.Data, phDeployment.Spec.GroupName, predictorInstanceType, predictorImage, options); err != nil {
		return nil, err
	}

	spawner.BuildPodSpec(&podSpec)
	// BuildPodSpec assign the container with name "main" in spawn.go
	podSpec.Containers[0].Name = "model"

	// Labels: map[string]string{
	// 	"phjob.primehub.io/scheduledBy": phSchedule.Name,
	// 	"primehub.io/group":             escapism.EscapeToPrimehubLabel(phSchedule.Spec.JobTemplate.Spec.GroupName),
	// 	"primehub.io/user":              escapism.EscapeToPrimehubLabel(phSchedule.Spec.JobTemplate.Spec.UserName),
	// },
	// TODO group-id and user-id need to escapism
	annotations := map[string]string{
		"primehub.io/group": phDeployment.Spec.GroupId,
		"primehub.io/user":  phDeployment.Spec.UserId,
	}

	ownerReference := metav1.NewControllerRef(phDeployment, phDeployment.GroupVersionKind())
	seldonDeployment := &seldonv1.SeldonDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            seldonDeploymentName,
			Namespace:       phDeployment.Namespace,
			Annotations:     annotations,
			OwnerReferences: []metav1.OwnerReference{*ownerReference},
		},
		Spec: seldonv1.SeldonDeploymentSpec{
			Name:        phDeployment.Name,
			Predictors:  nil,
			OauthKey:    "",
			OauthSecret: "",
			Annotations: phDeployment.ObjectMeta.Annotations,
			Protocol:    "",
			Transport:   "",
		},
	}

	// TODO add resource to PodSpec
	// instanceInfo, err := r.getInstanceTypeInfo(phJob.Spec.InstanceType)
	// resources := ConvertToResourceQuota(instanceInfo.Spec.LimitsCpu, (float32)(instanceInfo.Spec.LimitsGpu), instanceInfo.Spec.LimitsMemory)

	componentSpecs := make([]*seldonv1.SeldonPodSpec, 0)

	seldonPodSpec1 := &seldonv1.SeldonPodSpec{
		Metadata: metav1.ObjectMeta{
			Annotations: annotations,
			Name:        seldonDeploymentName,
		},
		// Spec: corev1.PodSpec{
		// 	Containers: []corev1.Container{
		// 		{
		// 			Name:  "model",
		// 			Image: predictorImage,
		// 		},
		// 	},
		// },
		Spec: podSpec,
	}
	componentSpecs = append(componentSpecs, seldonPodSpec1)

	modelType := seldonv1.MODEL

	graph := &seldonv1.PredictiveUnit{
		Name: "model",
		Type: &modelType,
		Endpoint: &seldonv1.Endpoint{
			Type: seldonv1.REST,
		},
	}

	predictors := make([]seldonv1.PredictorSpec, 0)
	predictor1 := seldonv1.PredictorSpec{
		Name:           "predictor1",
		ComponentSpecs: componentSpecs,
		Graph:          graph,
		Replicas:       int32(1),
		Annotations: map[string]string{
			"predictor_version": "v1",
		},
	}
	predictors = append(predictors, predictor1)
	seldonDeployment.Spec.Predictors = predictors
	seldonDeployment.Spec.Annotations = map[string]string{
		"a": "b",
	}
	return seldonDeployment, nil
}

// func (r *PhDeploymentReconciler) getInstanceTypeInfo(instanceTypeId string) (*graphql.DtoInstanceType, error) {
// 	cacheKey := "instanceType:" + instanceTypeId
// 	cacheItem := InstanceTypeCache.Get(cacheKey)
// 	if cacheItem == nil || cacheItem.Expired() {
// 		instanceTypeInfo, err := r.GraphqlClient.FetchInstanceTypeInfo(instanceTypeId)
// 		if err != nil {
// 			return nil, err
// 		}
// 		InstanceTypeCache.Set(cacheKey, instanceTypeInfo, cacheExpiredTime)
// 	}
// 	return InstanceTypeCache.Get(cacheKey).Value().(*graphql.DtoInstanceType), nil
// }

// +kubebuilder:rbac:groups=primehub.io,resources=phdeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=primehub.io,resources=phdeployments/status,verbs=get;update;patch

func (r *PhDeploymentReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("phdeployment", req.NamespacedName)

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
		log.Info("Finished Reconciling phDeployment ", "phDeployment", phDeployment, "ReconcileTime", time.Since(startTime))
	}()

	phDeployment = phDeployment.DeepCopy()
	if phDeployment.Spec.Stop == true {
		// delete SeldonDeployment and Ingress
		seldonDeployment := seldonv1.SeldonDeployment{}
		if err := r.Client.Get(ctx, client.ObjectKey{Namespace: phDeployment.Namespace, Name: phDeployment.Name}, &seldonDeployment); err == nil {
			if err := r.Client.Delete(ctx, &seldonDeployment); err != nil {
				log.Error(err, "Unable to delete seldonDeployment")
			} else {
				log.Info("Delete SeldonDeployment", "phDeployment", phDeployment.Namespace+"/"+phDeployment.Name)
			}
		}

		ingress := v1beta1.Ingress{}
		if err := r.Client.Get(ctx, client.ObjectKey{Namespace: phDeployment.Namespace, Name: phDeployment.Name}, &ingress); err == nil {
			if err := r.Client.Delete(ctx, &ingress); err != nil {
				log.Error(err, "Unable to delete ingress")
			} else {
				log.Info("Delete Ingress", "phDeployment", phDeployment.Namespace+"/"+phDeployment.Name)
			}
		}

		phDeployment.Status.Phase = primehubv1alpha1.DeploymentStopped
		if err := r.updatePhDeploymentStatus(ctx, phDeployment); err != nil {
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
		}

		return ctrl.Result{}, nil
	}

	// create seldon deployment
	seldonDeployment, err := r.buildSeldonDeployment(ctx, phDeployment, log)
	cache := &seldonv1.SeldonDeployment{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: phDeployment.Namespace, Name: phDeployment.Name}, cache); err != nil {
		if apierrors.IsNotFound(err) {
			if err := r.Client.Create(ctx, seldonDeployment); err != nil {
				log.Error(err, "Failed to create SeldonDeployment")
				return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
			} else {
				log.Info("SeldonDeployment created")
			}
		}
	} else {
		cache = cache.DeepCopy()
		cache.Spec = seldonDeployment.Spec
		if err := r.Update(ctx, cache); err != nil {
			log.Error(err, "Failed to update SeldonDeployment")
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
		} else {
			log.Info("SeldonDeployment updated")
		}
	}

	if cache.Status.ServiceStatus == nil {
		// Service Status might not be available for now
		return ctrl.Result{RequeueAfter: 15 * time.Second}, err
	}

	serviceName := ""
	httpEndpoint := ""
	for k, v := range cache.Status.ServiceStatus {
		if strings.Contains(v.HttpEndpoint, ":8000") {
			serviceName = k
			httpEndpoint = v.HttpEndpoint
			break
		}
	}

	servingHost := "unknown-domain"
	primehubIngress := v1beta1.Ingress{}
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: "hub", Name: "primehub-graphql"}, &primehubIngress); err == nil {
		servingHost = primehubIngress.Spec.Rules[0].Host
	}

	// Create Ingress
	ingress := r.createIngress(ctx, phDeployment, serviceName, servingHost, log)
	cachedIngress := &v1beta1.Ingress{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: phDeployment.Namespace, Name: phDeployment.Name}, cachedIngress); err != nil {
		if apierrors.IsNotFound(err) {
			if err := r.Client.Create(ctx, ingress); err != nil {
				log.Error(err, "Failed to create Ingress")
				return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
			} else {
				log.Info("Ingress created")
			}
		}
	}

	// update status based on seldon deployment
	deploymentStatus := cache.Status.DeploymentStatus[phDeployment.Name]
	phDeployment.Status.AvailableReplicas = int(deploymentStatus.AvailableReplicas)
	phDeployment.Status.Replicas = int(deploymentStatus.Replicas)
	phDeployment.Status.Endpoint = httpEndpoint

	// Mapping seldon deployment status to our phase
	// TODO seldon's failed might not be showing, but it could go into trouble.
	// We need to find another way to check something wrong
	stateMapping := map[seldonv1.StatusState]primehubv1alpha1.PhDeploymentPhase{
		seldonv1.StatusStateAvailable: primehubv1alpha1.DeploymentDeployed,
		seldonv1.StatusStateCreating:  primehubv1alpha1.DeploymentDeploying,
		seldonv1.StatusStateFailed:    primehubv1alpha1.DeploymentFailed,
	}
	phDeployment.Status.Phase = stateMapping[cache.Status.State]
	// TODO update history content
	phDeployment.Status.History = make([]primehubv1alpha1.PhDeploymentHistory, 0)
	if err := r.updatePhDeploymentStatus(ctx, phDeployment); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *PhDeploymentReconciler) createIngress(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment, serviceName string, servingHost string, log logr.Logger) *v1beta1.Ingress {
	// create ingress
	backend := v1beta1.IngressBackend{
		ServiceName: serviceName,
		ServicePort: intstr.IntOrString{
			Type:   intstr.Int,
			IntVal: 8000,
		},
	}
	rules := []v1beta1.IngressRule{
		{
			Host: servingHost,
			IngressRuleValue: v1beta1.IngressRuleValue{
				HTTP: &v1beta1.HTTPIngressRuleValue{
					Paths: []v1beta1.HTTPIngressPath{
						{
							Path:    "/deployment/" + phDeployment.Name + "/(.+)",
							Backend: backend,
						},
					},
				},
			},
		},
	}

	ownerReference := metav1.NewControllerRef(phDeployment, phDeployment.GroupVersionKind())
	ingress := &v1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      phDeployment.Name,
			Namespace: phDeployment.Namespace,
			Annotations: map[string]string{
				"kubernetes.io/ingress.class": "nginx",
				"kubernetes.io/tls-acme":      "true",
				// TODO was it possible to rewrite with standard ingress feature ?
				"nginx.ingress.kubernetes.io/rewrite-target": "/$1",
			},
			OwnerReferences: []metav1.OwnerReference{*ownerReference},
		},
		Spec: v1beta1.IngressSpec{
			TLS: []v1beta1.IngressTLS{{
				Hosts: []string{servingHost},
			},
			},
			Rules: rules,
		},
	}

	return ingress
}

func (r *PhDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&primehubv1alpha1.PhDeployment{}).
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
