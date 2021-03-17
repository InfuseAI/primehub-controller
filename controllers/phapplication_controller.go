package controllers

import (
	"context"
	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	phcache "primehub-controller/pkg/cache"
	"primehub-controller/pkg/graphql"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"primehub-controller/api/v1alpha1"
)

// PhApplicationReconciler reconciles a PhApplication object
type PhApplicationReconciler struct {
	client.Client
	Log           logr.Logger
	Scheme        *runtime.Scheme
	PrimeHubCache *phcache.PrimeHubCache
}

func (r *PhApplicationReconciler) getPhApplicationObject(namespace string, name string, obj runtime.Object) (bool, error) {
	exist := true
	err := r.Get(context.Background(), client.ObjectKey{Namespace: namespace, Name: name}, obj)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return false, err
		}
		exist = false
	}
	return exist, nil
}

func (r *PhApplicationReconciler) generateDeploymentSpec(phApplication *v1alpha1.PhApplication, deployment *appv1.Deployment) error {
	var replicas int32
	var err error

	if phApplication.Spec.Stop == true {
		replicas = 0
	} else {
		replicas = 1
	}
	podSpec := phApplication.Spec.PodTemplate.Spec.DeepCopy()
	labels := map[string]string{
		"app":                       phApplication.App(),
		"primehub.io/phapplication": phApplication.AppName(),
		"primehub.io/group":         phApplication.GroupName(),
	}

	// Fetch InstanceType data from graphql
	instanceTypeInfo, err := r.PrimeHubCache.FetchInstanceType(phApplication.Spec.InstanceType)
	if err != nil {
		return err
	}

	// Fetch Group data from graphql
	groupInfo, err := r.PrimeHubCache.FetchGroupByName(phApplication.Spec.GroupName)
	if err != nil {
		return err
	}

	deployment.Name = phApplication.AppName()
	deployment.Namespace = phApplication.ObjectMeta.Namespace
	deployment.ObjectMeta.Labels = labels
	deployment.Spec.Replicas = &replicas
	deployment.Spec.Selector = &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"primehub.io/phapplication": phApplication.AppName(),
		},
	}
	deployment.Spec.Template.ObjectMeta.Labels = labels
	deployment.Spec.Template.Spec = *podSpec
	spawner, err := graphql.NewSpawnerForPhApplication(phApplication.AppID(), *groupInfo, *instanceTypeInfo, *podSpec)
	if err != nil {
		return err
	}

	spawner.PatchPodSpec(&deployment.Spec.Template.Spec)

	return nil
}

func (r *PhApplicationReconciler) createDeployment(phApplication *v1alpha1.PhApplication, deployment *appv1.Deployment) error {
	if phApplication == nil {
		return apierrors.NewBadRequest("phApplication not provided")
	}
	if deployment == nil {
		return apierrors.NewBadRequest("deployment not provided")
	}

	if err := r.generateDeploymentSpec(phApplication, deployment); err != nil {
		return err
	}

	if err := ctrl.SetControllerReference(phApplication, deployment, r.Scheme); err != nil {
		return err
	}
	if err := r.Client.Create(context.Background(), deployment); err != nil {
		return err
	}

	image := deployment.Spec.Template.Spec.Containers[0].Image
	group := phApplication.GroupName()
	instanceType := phApplication.Spec.InstanceType
	r.Log.Info(
		"Create Deployment",
		"Name", deployment.Name, "Replica", deployment.Spec.Replicas,
		"Image", image, "Group", group, "InstanceType", instanceType)
	return nil
}

func (r *PhApplicationReconciler) updateDeployment(phApplication *v1alpha1.PhApplication, deployment *appv1.Deployment) error {
	if phApplication == nil {
		return apierrors.NewBadRequest("phApplication not provided")
	}
	if deployment == nil {
		return apierrors.NewBadRequest("deployment not provided")
	}

	deploymentClone := deployment.DeepCopy()
	if err := r.generateDeploymentSpec(phApplication, deploymentClone); err != nil {
		return err
	}

	if err := r.Client.Update(context.Background(), deploymentClone); err != nil {
		return err
	}

	image := deployment.Spec.Template.Spec.Containers[0].Image
	group := phApplication.GroupName()
	instanceType := phApplication.Spec.InstanceType
	r.Log.Info("Update Deployment",
		"Name", deploymentClone.Name, "Replica", deploymentClone.Spec.Replicas,
		"Image", image, "Group", group, "InstanceType", instanceType)

	return nil
}

func (r *PhApplicationReconciler) reconcileDeployment(phApplication *v1alpha1.PhApplication) error {
	// Check if deployment exist
	namespace := phApplication.ObjectMeta.Namespace
	deployment := &appv1.Deployment{}
	deploymentExist, err := r.getPhApplicationObject(namespace, phApplication.AppName(), deployment)
	if err != nil {
		return err
	}

	if deploymentExist {
		// Update deployment data
		err = r.updateDeployment(phApplication, deployment)
	} else {
		// Create deployment
		err = r.createDeployment(phApplication, deployment)
	}
	return err
}

func (r *PhApplicationReconciler) createService(phApplication *v1alpha1.PhApplication, service *corev1.Service) error {
	if phApplication == nil {
		return apierrors.NewBadRequest("phApplication not provided")
	}
	if service == nil {
		return apierrors.NewBadRequest("service not provided")
	}

	service.Name = phApplication.AppName()
	service.Namespace = phApplication.ObjectMeta.Namespace
	service.ObjectMeta.Labels = map[string]string{
		"app":                       phApplication.App(),
		"primehub.io/phapplication": phApplication.AppName(),
		"primehub.io/group":         phApplication.GroupName(),
	}
	service.Spec.Type = corev1.ServiceTypeClusterIP
	service.Spec.Selector = map[string]string{
		"primehub.io/phapplication": phApplication.AppName(),
	}
	service.Spec.Ports = phApplication.Spec.SvcTemplate.Spec.Ports

	if err := ctrl.SetControllerReference(phApplication, service, r.Scheme); err != nil {
		return err
	}
	if err := r.Client.Create(context.Background(), service); err != nil {
		return err
	}

	r.Log.Info("Create Service", "Name", service.Name)
	return nil
}

func (r *PhApplicationReconciler) updateService(phApplication *v1alpha1.PhApplication, service *corev1.Service) error {
	if phApplication == nil {
		return apierrors.NewBadRequest("phApplication not provided")
	}
	if service == nil {
		return apierrors.NewBadRequest("service not provided")
	}

	serviceClone := service.DeepCopy()
	serviceClone.ObjectMeta.Labels["primehub.io/group"] = phApplication.GroupName()
	serviceClone.Spec.Ports = phApplication.Spec.SvcTemplate.Spec.Ports

	if err := r.Client.Update(context.Background(), serviceClone); err != nil {
		return err
	}
	r.Log.Info("Updated Service", "Name", serviceClone.Name)
	return nil
}

func (r *PhApplicationReconciler) reconcileService(phApplication *v1alpha1.PhApplication) error {
	namespace := phApplication.ObjectMeta.Namespace
	service := &corev1.Service{}
	serviceExist, err := r.getPhApplicationObject(namespace, phApplication.AppName(), service)
	if err != nil {
		return err
	}

	if serviceExist {
		// Update Service
		err = r.updateService(phApplication, service)
	} else {
		// Create Service
		err = r.createService(phApplication, service)
	}
	return err
}

const (
	GroupNetworkPolicy string = "group"
	ProxyNetwrokPolicy string = "proxy"
)

func (r *PhApplicationReconciler) createNetworkPolicy(phApplication *v1alpha1.PhApplication, networkPolicy *networkv1.NetworkPolicy, npType string) error {
	if phApplication == nil {
		return apierrors.NewBadRequest("phApplication not provided")
	}
	if networkPolicy == nil {
		return apierrors.NewBadRequest("networkPolicy not provided")
	}

	var ingress []networkv1.NetworkPolicyIngressRule
	switch npType {
	case GroupNetworkPolicy:
		ingress = phApplication.GroupNetworkPolicyIngressRule()
	case ProxyNetwrokPolicy:
		ingress = phApplication.ProxyNetworkPolicyIngressRule()
	}

	networkPolicy.Name = phApplication.AppName() + "-" + npType
	networkPolicy.Namespace = phApplication.ObjectMeta.Namespace
	networkPolicy.ObjectMeta.Labels = map[string]string{
		"app":                       phApplication.App(),
		"primehub.io/phapplication": phApplication.AppName(),
		"primehub.io/group":         phApplication.GroupName(),
	}
	networkPolicy.Spec = networkv1.NetworkPolicySpec{
		PodSelector: metav1.LabelSelector{
			MatchLabels: map[string]string{
				"primehub.io/phapplication": phApplication.AppName(),
			},
		},
		Ingress: ingress,
		Egress:  nil,
		PolicyTypes: []networkv1.PolicyType{
			networkv1.PolicyTypeIngress,
		},
	}

	if err := ctrl.SetControllerReference(phApplication, networkPolicy, r.Scheme); err != nil {
		return err
	}
	if err := r.Client.Create(context.Background(), networkPolicy); err != nil {
		return err
	}
	r.Log.Info("Created NetworkPolicy", "Name", networkPolicy.Name)
	return nil
}

func (r *PhApplicationReconciler) updateNetworkPolicy(phApplication *v1alpha1.PhApplication, networkPolicy *networkv1.NetworkPolicy, npType string) error {
	if phApplication == nil {
		return apierrors.NewBadRequest("phApplication not provided")
	}
	if networkPolicy == nil {
		return apierrors.NewBadRequest("networkPolicy not provided")
	}

	networkPolicyClone := networkPolicy.DeepCopy()
	networkPolicyClone.ObjectMeta.Labels["primehub.io/group"] = phApplication.GroupName()
	switch npType {
	case GroupNetworkPolicy:
		networkPolicyClone.Spec.Ingress = phApplication.GroupNetworkPolicyIngressRule()
	case ProxyNetwrokPolicy:
		networkPolicyClone.Spec.Ingress = phApplication.ProxyNetworkPolicyIngressRule()
	}
	if err := r.Client.Update(context.Background(), networkPolicyClone); err != nil {
		return err
	}
	r.Log.Info("Updated NetworkPolicy", "Name", networkPolicyClone.Name)
	return nil
}

func (r *PhApplicationReconciler) reconcileNetworkPolicy(phApplication *v1alpha1.PhApplication) error {
	var name string
	namespace := phApplication.ObjectMeta.Namespace
	groupNetworkPolicy := &networkv1.NetworkPolicy{}
	proxyNetworkPolicy := &networkv1.NetworkPolicy{}

	name = phApplication.AppName() + "-" + GroupNetworkPolicy
	groupNetworkPolicyExist, err := r.getPhApplicationObject(namespace, name, groupNetworkPolicy)
	if err != nil {
		return err
	}

	name = phApplication.AppName() + "-" + ProxyNetwrokPolicy
	proxyNetworkPolicyExist, err := r.getPhApplicationObject(namespace, name, proxyNetworkPolicy)
	if err != nil {
		return err
	}

	if groupNetworkPolicyExist {
		// Update NetworkPolicy
		err = r.updateNetworkPolicy(phApplication, groupNetworkPolicy, GroupNetworkPolicy)
	} else {
		// Create NetworkPolicy
		err = r.createNetworkPolicy(phApplication, groupNetworkPolicy, GroupNetworkPolicy)
	}

	if proxyNetworkPolicyExist {
		// Update NetworkPolicy
		err = r.updateNetworkPolicy(phApplication, proxyNetworkPolicy, ProxyNetwrokPolicy)
	} else {
		// Create NetworkPolicy
		err = r.createNetworkPolicy(phApplication, proxyNetworkPolicy, ProxyNetwrokPolicy)
	}

	return err
}

func (r *PhApplicationReconciler) updatePhApplicationStatus(phApplication *v1alpha1.PhApplication) error {
	var message string
	phase := v1alpha1.ApplicationError
	namespace := phApplication.ObjectMeta.Namespace
	deployment := &appv1.Deployment{}
	err := r.Get(context.Background(), client.ObjectKey{Namespace: namespace, Name: phApplication.AppName()}, deployment)
	if err != nil {
		phase = v1alpha1.ApplicationError
		message = err.Error()
	} else if phApplication.Spec.Stop {
		// Deployment Stop
		if deployment.Status.Replicas == 0 {
			phase = v1alpha1.ApplicationStopped
			message = "Deployment had stopped"
		} else {
			phase = v1alpha1.ApplicationStopping
			message = "Deployment is stopping"
		}
	} else {
		// Deployment Start
		if deployment.Status.ReadyReplicas == 0 {
			phase = v1alpha1.ApplicationStarting
			message = "Deployment is starting"
		} else if deployment.Status.ReadyReplicas == deployment.Status.Replicas {
			phase = v1alpha1.ApplicationReady
			message = "Deployment is ready"
		} else {
			phase = v1alpha1.ApplicationUpdating
			message = "Deployment is updating"
		}
	}
	phApplicationClone := phApplication.DeepCopy()
	phApplicationClone.Status.Phase = phase
	phApplicationClone.Status.Message = message
	phApplicationClone.Status.ServiceName = phApplication.AppName()
	if err := r.Status().Update(context.Background(), phApplicationClone); err != nil {

		return err
	}
	r.Log.Info("Updated Status",
		"Phase", phase,
		"Replicas", deployment.Status.Replicas,
		"ReadyReplicas", deployment.Status.ReadyReplicas)
	return nil
}

// +kubebuilder:rbac:groups=primehub.io,resources=phapplications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=primehub.io,resources=phapplications/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=service,verbs=get;list;watch;create;update;delete;patch
// +kubebuilder:rbac:groups="",resources=deployment,verbs=get;list;watch;create;update;delete;patch
// +kubebuilder:rbac:groups="",resources=networkpolicy,verbs=get;list;watch;create;update;delete;patch

func (r *PhApplicationReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	var err error
	var phApplication v1alpha1.PhApplication
	log := r.Log.WithValues("phapplication", req.NamespacedName)

	// Fetch phApplication object
	if err = r.Get(context.Background(), req.NamespacedName, &phApplication); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Deleted phApplication")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Unable to fetch phApplication")
		return ctrl.Result{}, err
	}

	// Reconcile Deployment
	if err = r.reconcileDeployment(&phApplication); err != nil {
		log.Info("Reconcile Deployment failed", "error", err)
	}

	// Reconcile Service
	if err = r.reconcileService(&phApplication); err != nil {
		log.Info("Reconcile Service failed", "error", err)
	}

	// Reconcile Network-policy
	if err = r.reconcileNetworkPolicy(&phApplication); err != nil {
		log.Info("Reconcile NetworkPolicy failed", "error", err)
	}

	// Update status
	if err = r.updatePhApplicationStatus(&phApplication); err != nil {
		log.Info("Update status failed", "error", err)
	}

	return ctrl.Result{}, nil
}

func (r *PhApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.PhApplication{}).
		Owns(&appv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkv1.NetworkPolicy{}).
		Complete(r)
}
