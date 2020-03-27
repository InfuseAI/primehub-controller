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
		Name:      req.Name,
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
	r.updateHistory(ctx, phDeployment)

	// phDeployment has been stoped
	if phDeployment.Spec.Stop == true {
		// delete seldonDeployment
		if err := r.deleteSeldonDeployment(ctx, seldonDeploymentKey); err != nil {
			log.Error(err, "failed to delete seldonDeployment and stop phDeployment")
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
		}

		// delete ingress
		if err := r.deleteIngress(ctx, ingressKey); err != nil {
			log.Error(err, "failed to delete ingress and stop phDeployment")
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
		}

		// update stauts
		phDeployment.Status.Phase = primehubv1alpha1.DeploymentStopped
		phDeployment.Status.Messsage = "deployment has been stoped"
		phDeployment.Status.Replicas = phDeployment.Spec.Predictors[0].Replicas
		phDeployment.Status.AvailableReplicas = 0
		phDeployment.Status.Endpoint = ""

		// update history
		r.updateHistory(ctx, phDeployment)

		if !apiequality.Semantic.DeepEqual(*oldStatus, phDeployment.Status) {
			if err := r.updatePhDeploymentStatus(ctx, phDeployment); err != nil {
				return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
			}
		}

		return ctrl.Result{}, nil
	}

	// reconcile seldonDeployment
	if err := r.reconcileSeldonDeployment(ctx, phDeployment, seldonDeploymentKey); err != nil {
		log.Error(err, "reconcile Seldon Deployment error.")
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
	}

	// reconcile ingress only when phDeployment is deployed, sync from seldonDeployment
	if phDeployment.Status.Phase == primehubv1alpha1.DeploymentDeployed {
		if err := r.reconcileIngress(ctx, phDeployment, ingressKey, seldonDeploymentKey); err != nil {
			log.Error(err, "reconcile Ingress error.")
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
		}
	} else {
		// delete ingress if phDeployment is not deployed and available
		if err := r.deleteIngress(ctx, ingressKey); err != nil {
			log.Error(err, "failed to delete ingress and stop phDeployment")
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
		}
	}

	// if the status has changed, update the phDeployment status
	if !apiequality.Semantic.DeepEqual(*oldStatus, phDeployment.Status) {
		if err := r.updatePhDeploymentStatus(ctx, phDeployment); err != nil {
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
		}
	}

	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
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

func (r *PhDeploymentReconciler) reconcileSeldonDeployment(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment, seldonDeploymentKey client.ObjectKey) error {
	// 1. check seldonDeployment exists, if no create one
	// 2. update the seldonDeployment if spec has been changed
	// 3. update the phDeployment status based on the sseldonDeployment status
	// 4. currently, the phDeployment failed if seldonDeployment failed or it is not available for over 5 mins
	log := r.Log.WithValues("phDeployment", phDeployment.Name)

	seldonDeploymentAvailableTimeout := false
	reconcilationFailed := false
	reconcilationFailedReason := ""

	seldonDeployment, err := r.getSeldonDeployment(ctx, seldonDeploymentKey)
	if err != nil && !apierrors.IsNotFound(err) {
		log.Error(err, "return since GET seldonDeployment error ")
		return err
	}

	if seldonDeployment == nil { // seldonDeployment is not found
		log.Info("SeldonDeployment doesn't exist, create one...")

		seldonDeployment, err := r.buildSeldonDeployment(ctx, phDeployment)

		if err == nil {
			err = r.Client.Create(ctx, seldonDeployment)
		}

		if err == nil { // create seldonDeployment successfully
			log.Info("SeldonDeployment created", "SeldonDeployment", seldonDeployment.Name)
		} else { // error occurs when creating or building seldonDeployment
			log.Error(err, "CREATE seldonDeployment error")
			reconcilationFailed = true
			reconcilationFailedReason = err.Error()
		}
	} else { // seldonDeployment exist
		log.Info("SeldonDeployment exist, check the status of current seldonDeployment and update phDeployment")

		// update the seldonDeployment if spec has been changed
		if len(phDeployment.Status.History) > 1 {
			// we prepend the spec first then do the reconcilation, so we should compare to the second one
			latestHistory := phDeployment.Status.History[1]
			if !apiequality.Semantic.DeepEqual(phDeployment.Spec, latestHistory.Spec) {

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

			}
		}

		// check if seldonDeployment is unAvailable for over 5 min
		if r.unAvailableTimeout(phDeployment, seldonDeployment) {
			log.Info("SeldonDeployment is not available for over 5 min. Change the phDeployment to failed state.")
			if err := r.deleteSeldonDeployment(ctx, seldonDeploymentKey); err != nil {
				log.Error(err, "failed to delete seldonDeployment after preparing state timeout")
				return err
			}
			seldonDeploymentAvailableTimeout = true
		}
	}

	if reconcilationFailed == false { // if update/create seldonDeployment successfully
		// if the situation is creation, then seldonDeployment comes from buildSeldonDeployment
		// and thus the status will be nil, so we get the seldonDeployment from cluster again.
		seldonDeployment, err = r.getSeldonDeployment(ctx, seldonDeploymentKey)
		if err != nil {
			log.Error(err, "Failed to get created seldonDeployment or it doesn't exist after reconciling seldonDeployment")
			return err
		}
	}

	return r.updateStatus(ctx, phDeployment, seldonDeployment, seldonDeploymentAvailableTimeout, reconcilationFailed, reconcilationFailedReason)
}

func (r *PhDeploymentReconciler) updateStatus(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment, seldonDeployment *seldonv1.SeldonDeployment, seldonDeploymentAvailableTimeout bool, reconcilationFailed bool, reconcilationFailedReason string) error {

	// log.Info("=== in updateStatus ===", "seldonDeployment.Status", seldonDeployment.Status)
	//log := r.Log.WithValues("phDeployment", phDeployment.Name)

	if seldonDeploymentAvailableTimeout {
		phDeployment.Status.Phase = primehubv1alpha1.DeploymentFailed
		phDeployment.Status.Messsage = "phDeployment has failed because the deployment is not available for over 5 min"
		phDeployment.Status.Replicas = phDeployment.Spec.Predictors[0].Replicas
		phDeployment.Status.AvailableReplicas = 0
		phDeployment.Status.Endpoint = ""

		return nil
	}

	if reconcilationFailed { // update / create failed need to reconcile in 1 min
		phDeployment.Status.Phase = primehubv1alpha1.DeploymentFailed
		phDeployment.Status.Messsage = reconcilationFailedReason
		phDeployment.Status.Replicas = phDeployment.Spec.Predictors[0].Replicas
		phDeployment.Status.AvailableReplicas = 0
		phDeployment.Status.Endpoint = ""

		return fmt.Errorf("reconcile seldonDeployment failed")
	}

	if seldonDeployment.Status.State == seldonv1.StatusStateFailed {
		phDeployment.Status.Phase = primehubv1alpha1.DeploymentFailed
		phDeployment.Status.Messsage = "phDeployment has failed because the seldon deployment on k8s is failed "
		phDeployment.Status.Replicas = phDeployment.Spec.Predictors[0].Replicas
		phDeployment.Status.AvailableReplicas = 0
		phDeployment.Status.Endpoint = ""

		return nil
	}

	if seldonDeployment.Status.State == seldonv1.StatusStateAvailable {

		phDeployment.Status.Phase = primehubv1alpha1.DeploymentDeployed
		phDeployment.Status.Messsage = "phDeployment is deployed and available now"
		phDeployment.Status.Replicas = phDeployment.Spec.Predictors[0].Replicas
		phDeployment.Status.AvailableReplicas = int(seldonDeployment.Status.DeploymentStatus[phDeployment.Name].AvailableReplicas)

		// assign endpoint when reconcile ingress and sync from ingress
		// phDeployment.Status.Endpoint = ""

		return nil
	}

	if seldonDeployment.Status.State == seldonv1.StatusStateCreating {
		phDeployment.Status.Phase = primehubv1alpha1.DeploymentDeploying
		phDeployment.Status.Messsage = "phDeployment is being deployed and not available now"
		phDeployment.Status.Replicas = phDeployment.Spec.Predictors[0].Replicas
		phDeployment.Status.AvailableReplicas = int(seldonDeployment.Status.DeploymentStatus[phDeployment.Name].AvailableReplicas)
		phDeployment.Status.Endpoint = ""

		return nil
	}

	return fmt.Errorf("seldonDeployment is in Unknown state.")
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

		// get seldonDeployment
		seldonDeployment, err := r.getSeldonDeployment(ctx, seldonDeploymentKey)
		if err != nil && !apierrors.IsNotFound(err) {
			log.Info("return since GET seldonDeployment error ", "seldonDeployment", seldonDeploymentKey, "err", err)
			return err
		}

		if seldonDeployment == nil {
			log.Info("return since GET seldonDeployment error, seldonDeployment doesn't exists")
			return fmt.Errorf("seldonDeployment doesn't exists when creating ingress")
		}

		serviceName := ""
		// httpEndpoint := ""
		for k, v := range seldonDeployment.Status.ServiceStatus {
			if strings.Contains(v.HttpEndpoint, ":8000") {
				serviceName = k
				// httpEndpoint = v.HttpEndpoint
				break
			}
		}

		servingHost := "unknown-domain"
		primehubIngress := v1beta1.Ingress{}
		if err := r.Client.Get(ctx, client.ObjectKey{Namespace: "hub", Name: "primehub-graphql"}, &primehubIngress); err == nil {
			servingHost = primehubIngress.Spec.Rules[0].Host
		}

		// Create Ingress
		phDeploymentIngress, err = r.buildIngress(ctx, phDeployment, serviceName, servingHost)
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
		phDeployment.Status.Endpoint = "https://" + servingHost + "/deployment/" + phDeployment.Name + "/api/v0.1/predictions"
	}

	return nil
}

// build seldonDeployment of the phDeployment
func (r *PhDeploymentReconciler) buildSeldonDeployment(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment) (*seldonv1.SeldonDeployment, error) {
	seldonDeploymentName := phDeployment.Name
	seldonDeploymentNamespace := phDeployment.Namespace

	seldonDeployment := &seldonv1.SeldonDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        seldonDeploymentName,
			Namespace:   seldonDeploymentNamespace,
			Annotations: phDeployment.ObjectMeta.Annotations,
			Labels: map[string]string{
				"app":               "primehub-deployment",
				"primehub.io/group": escapism.EscapeToPrimehubLabel(phDeployment.Spec.GroupName),
				//"primehub.io/user":  escapism.EscapeToPrimehubLabel(phDeployment.Spec.UserName),
			},
		},
		Spec: seldonv1.SeldonDeploymentSpec{
			Name:        seldonDeploymentName,
			Predictors:  nil,
			OauthKey:    "",
			OauthSecret: "",
			Annotations: phDeployment.ObjectMeta.Annotations,
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

	seldonPodSpec1 := &seldonv1.SeldonPodSpec{
		Metadata: metav1.ObjectMeta{
			Annotations: phDeployment.ObjectMeta.Annotations,
			Labels: map[string]string{
				"app": "primehub-deployment-predictor",
			},
			Name:              seldonDeploymentName,
			CreationTimestamp: metav1.Now(),
		},
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
		Name:           "predictor1",
		ComponentSpecs: componentSpecs,
		Graph:          graph,
		Replicas:       int32(phDeployment.Spec.Predictors[0].Replicas),
		Annotations: map[string]string{
			"predictor_version": "v1",
		},
		Labels: map[string]string{
			"app":               "primehub-deployment-pod",
			"primehub.io/group": escapism.EscapeToPrimehubLabel(phDeployment.Spec.GroupName),
			//"primehub.io/user":  escapism.EscapeToPrimehubLabel(phDeployment.Spec.UserName),
		},
	}
	predictors := make([]seldonv1.PredictorSpec, 0)
	predictors = append(predictors, predictor1)

	seldonDeployment.Spec.Predictors = predictors
	// if seldonDeployment.Spec.Annotations is nil, seldon will fail
	seldonDeployment.Spec.Annotations = map[string]string{
		"a": "b",
	}

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
func (r *PhDeploymentReconciler) buildIngress(ctx context.Context, phDeployment *primehubv1alpha1.PhDeployment, serviceName string, servingHost string) (*v1beta1.Ingress, error) {
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
							Path: "/deployment/" + phDeployment.Name + "/(.+)",
							//Path:    "/deployment/" + phDeployment.Name + "/",
							Backend: backend,
						},
					},
				},
			},
		},
	}

	ingress := &v1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      phDeployment.Name,
			Namespace: phDeployment.Namespace,
			Annotations: map[string]string{
				"kubernetes.io/ingress.class": "nginx",
				"kubernetes.io/tls-acme":      "true",
				// TODO was it possible to rewrite with standard ingress feature ?
				"nginx.ingress.kubernetes.io/rewrite-target": "/$1",
				// "nginx.ingress.kubernetes.io/rewrite-target": "/",
			},
		},
		Spec: v1beta1.IngressSpec{
			TLS: []v1beta1.IngressTLS{
				{
					Hosts:      []string{servingHost},
					SecretName: "dns01-tls",
				},
			},
			Rules: rules,
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
