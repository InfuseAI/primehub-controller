package controllers

import (
	"context"
	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"k8s.io/apimachinery/pkg/runtime"
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
	Log    logr.Logger
	Scheme *runtime.Scheme
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

func (r *PhApplicationReconciler) createDeployment(phApplication *v1alpha1.PhApplication) error {
	return nil
}

func (r *PhApplicationReconciler) updateDeployment(phApplication *v1alpha1.PhApplication) error {
	return nil
}

func (r *PhApplicationReconciler) reconcileDeployment(phApplication *v1alpha1.PhApplication) error {
	// Check if deployment exist
	namespace := phApplication.ObjectMeta.Namespace
	appID := phApplication.ObjectMeta.Name
	deployment := &appv1.Deployment{}
	deploymentExist, err := r.getPhApplicationObject(namespace, "app-"+appID, deployment)
	if err != nil {
		return err
	}

	if deploymentExist {
		// Update deployment data
		err = r.updateDeployment(phApplication)
	} else {
		// Create deployment
		err = r.createDeployment(phApplication)
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

	service.Name = phApplication.AppID()
	service.Namespace = phApplication.ObjectMeta.Namespace
	service.ObjectMeta.Labels = map[string]string{
		"app":                       phApplication.App(),
		"primehub.io/phapplication": phApplication.AppID(),
		"primehub.io/group":         phApplication.GroupName(),
	}
	service.Spec.Type = corev1.ServiceTypeClusterIP
	service.Spec.Selector = map[string]string{
		"primehub.io/phapplication": phApplication.AppID(),
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

	service.ObjectMeta.Labels["primehub.io/group"] = phApplication.GroupName()
	service.Spec.Ports = phApplication.Spec.SvcTemplate.Spec.Ports

	if err := r.Client.Update(context.Background(), service); err != nil {
		return err
	}

	r.Log.Info("Updated Service", "Name", service.Name)
	return nil
}

func (r *PhApplicationReconciler) reconcileService(phApplication *v1alpha1.PhApplication) error {
	namespace := phApplication.ObjectMeta.Namespace
	appID := phApplication.ObjectMeta.Name
	service := &corev1.Service{}
	serviceExist, err := r.getPhApplicationObject(namespace, "app-"+appID, service)
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

	networkPolicy.Name = phApplication.AppID() + "-" + npType
	networkPolicy.Namespace = phApplication.ObjectMeta.Namespace
	networkPolicy.ObjectMeta.Labels = map[string]string{
		"app":                       phApplication.App(),
		"primehub.io/phapplication": phApplication.AppID(),
		"primehub.io/group":         phApplication.GroupName(),
	}
	networkPolicy.Spec = networkv1.NetworkPolicySpec{
		PodSelector: metav1.LabelSelector{
			MatchLabels: map[string]string{
				"primehub.io/phapplication": phApplication.AppID(),
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

	networkPolicy.ObjectMeta.Labels["primehub.io/group"] = phApplication.GroupName()
	switch npType {
	case GroupNetworkPolicy:
		networkPolicy.Spec.Ingress = phApplication.GroupNetworkPolicyIngressRule()
	case ProxyNetwrokPolicy:
		networkPolicy.Spec.Ingress = phApplication.ProxyNetworkPolicyIngressRule()
	}
	if err := r.Client.Update(context.Background(), networkPolicy); err != nil {
		return err
	}
	r.Log.Info("Updated NetworkPolicy", "Name", networkPolicy.Name)
	return nil
}

func (r *PhApplicationReconciler) reconcileNetworkPolicy(phApplication *v1alpha1.PhApplication) error {
	var name string
	namespace := phApplication.ObjectMeta.Namespace
	appID := phApplication.ObjectMeta.Name
	groupNetworkPolicy := &networkv1.NetworkPolicy{}
	proxyNetworkPolicy := &networkv1.NetworkPolicy{}

	name = "app-" + appID + "-" + GroupNetworkPolicy
	groupNetworkPolicyExist, err := r.getPhApplicationObject(namespace, name, groupNetworkPolicy)
	if err != nil {
		return err
	}

	name = "app-" + appID + "-" + ProxyNetwrokPolicy
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

func (r *PhApplicationReconciler) updatePhApplicationStatus(phApplication *v1alpha1.PhApplication) {
	return
}

// +kubebuilder:rbac:groups=primehub.io,resources=phapplications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=primehub.io,resources=phapplications/status,verbs=get;update;patch

func (r *PhApplicationReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	var err error
	var phApplication v1alpha1.PhApplication
	log := r.Log.WithValues("phapplication", req.NamespacedName)

	// Fetch phApplication object
	if err = r.Get(context.Background(), req.NamespacedName, &phApplication); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "Unable to fetch phApplication")
		return ctrl.Result{}, err
	}

	// Reconcile Deployment
	if err = r.reconcileDeployment(&phApplication); err != nil {
		log.Error(err, "Reconcile Deployment failed")
	}

	// Reconcile Service
	if err = r.reconcileService(&phApplication); err != nil {
		log.Error(err, "Reconcile Service failed")
	}

	// Reconcile Network-policy
	if err = r.reconcileNetworkPolicy(&phApplication); err != nil {
		log.Error(err, "Reconcile NetworkPolicy failed")
	}

	// Update status
	r.updatePhApplicationStatus(&phApplication)

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
