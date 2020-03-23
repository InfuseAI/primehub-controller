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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	primehubv1alpha1 "primehub-controller/api/v1alpha1"
	seldonv1 "primehub-controller/seldon/apis/v1"

	corev1 "k8s.io/api/core/v1"
)

// PhDeploymentReconciler reconciles a PhDeployment object
type PhDeploymentReconciler struct {
	client.Client
	Log logr.Logger
}

func (r *PhDeploymentReconciler) buildSeldonDeployment(phDeployment primehubv1alpha1.PhDeployment) *seldonv1.SeldonDeployment {
	// log := r.Log.WithValues("phDeployment", phDeployment.Name)

	hash, _ := generateRandomString(6)
	seldonDeploymentName := phDeployment.Name + "-" + hash

	seldonDeployment := &seldonv1.SeldonDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        seldonDeploymentName,
			Namespace:   phDeployment.Namespace,
			Annotations: phDeployment.ObjectMeta.Annotations,
			// Labels: map[string]string{
			// 	"phjob.primehub.io/scheduledBy": phSchedule.Name,
			// 	"primehub.io/group":             escapism.EscapeToPrimehubLabel(phSchedule.Spec.JobTemplate.Spec.GroupName),
			// 	"primehub.io/user":              escapism.EscapeToPrimehubLabel(phSchedule.Spec.JobTemplate.Spec.UserName),
			// },
		},
	}

	// instanceInfo, err := r.getInstanceTypeInfo(phJob.Spec.InstanceType)
	// resources := ConvertToResourceQuota(instanceInfo.Spec.LimitsCpu, (float32)(instanceInfo.Spec.LimitsGpu), instanceInfo.Spec.LimitsMemory)

	componentSpecs := make([]*seldonv1.SeldonPodSpec, 0)

	seldonPodSpec1 := &seldonv1.SeldonPodSpec{
		Metadata: metav1.ObjectMeta{
			Annotations: map[string]string{
				"primehub.io/group":      phDeployment.Spec.GroupId,
				"primehub.io/deployment": seldonDeploymentName,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "model",
					Image: phDeployment.predictors[0].modelImage,
				},
			},
		},
	}
	componentSpecs = append(componentSpecs, seldonPodSpec1)

	graph := &seldonv1.PredictiveUnit{
		Name: "model",
		Type: seldonv1.MODEL,
		Endpoint: seldonv1.Endpoint{
			Type: REST,
		},
	}

	predictors := make([]seldonv1.PredictorSpec, 0)
	predictor1 := &seldonv1.PredictorSpec{
		Name:           "predictor1",
		ComponentSpecs: componentSpecs,
		Graph:          graph,
		Replicas:       int32(1),
		Annotation: map[string]string{
			"predictor_version", "v1",
		},
	}
	predictors = append(predictors, predictor1)

	seldonDeployment.Spec.Predictors = predictors

	return seldonDeployment

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
			log.Info("phDeployment deleted")
			return ctrl.Result{}, nil
		} else {
			log.Error(err, "unable to fetch PhShceduleJob")
		}
	}

	log.Info("Start Reconcile PhDeployment")
	startTime := time.Now()
	defer func() {
		log.Info("Finished Reconciling phDeployment ", "phDeployment", phDeployment, "ReconcileTime", time.Since(startTime))
	}()

	phDeployment = phDeployment.DeepCopy()

	if phDeployment.Spec.Stop == true {

		// delete seldondeployment and ingress

		phDeployment.Status.Phase = primehubv1alpha1.DeploymentStopped
		if err := r.updatePhDeploymentStatus(ctx, phDeployment); err != nil {
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
		}
	}

	// create seldon deployment
	seldonDeployment, err = r.buildSeldonDeployment(phDeployment)
	if err == nil {
		err = r.Client.Create(seldonDeployment)
	}

	if err == nil {
		log.Info("seldonDeployment created", "seldonDeployment", seldonDeployment)
	} else {
		log.Error(err, "creating seldonDeployment failed")
	}

	// create ingress

	// update status based on seldon deployment

	return ctrl.Result{}, nil
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
