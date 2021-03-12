package controllers

import (
	"context"
	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"k8s.io/apimachinery/pkg/runtime"
	"primehub-controller/pkg/escapism"
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

func (r *PhApplicationReconciler) getPhApplicationObject(ctx context.Context, namespace string, name string, obj runtime.Object) (bool, error) {
	exist := true
	err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, obj)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return false, err
		}
		exist = false
	}
	return exist, nil
}

func (r *PhApplicationReconciler) createDeployment(ctx context.Context, phApplication *v1alpha1.PhApplication) error {
	return nil
}

func (r *PhApplicationReconciler) updateDeployment(ctx context.Context, phApplication *v1alpha1.PhApplication) error {
	return nil
}

func (r *PhApplicationReconciler) reconcileDeployment(ctx context.Context, phApplication *v1alpha1.PhApplication) error {
	// Check if deployment exist
	namespace := phApplication.ObjectMeta.Namespace
	appID := phApplication.ObjectMeta.Name
	deployment := &appv1.Deployment{}
	deploymentExist, err := r.getPhApplicationObject(ctx, namespace, "app-"+appID, deployment)
	if err != nil {
		return err
	}

	if deploymentExist {
		// Update deployment data
		return r.updateDeployment(ctx, phApplication)
	} else {
		// Create deployment
		return r.createDeployment(ctx, phApplication)
	}
}

func (r *PhApplicationReconciler) createService(ctx context.Context, phApplication *v1alpha1.PhApplication) error {
	return nil
}

func (r *PhApplicationReconciler) updateService(ctx context.Context, phApplication *v1alpha1.PhApplication) error {
	return nil
}

func (r *PhApplicationReconciler) reconcileService(ctx context.Context, phApplication *v1alpha1.PhApplication) error {
	namespace := phApplication.ObjectMeta.Namespace
	appID := phApplication.ObjectMeta.Name
	service := &corev1.Service{}
	serviceExist, err := r.getPhApplicationObject(ctx, namespace, "app-"+appID, service)
	if err != nil {
		return err
	}

	if serviceExist {
		// Update Service
		return r.updateService(ctx, phApplication)
	} else {
		// Create Service
		return r.createService(ctx, phApplication)
	}
}

const (
	GroupNetworkPolicy string = "group"
	ProxyNetwrokPolicy string = "proxy"
)

func (r *PhApplicationReconciler) createNetworkPolicy(ctx context.Context, phApplication *v1alpha1.PhApplication, networkPolicy *networkv1.NetworkPolicy, npType string) error {
	if phApplication == nil {
		return apierrors.NewBadRequest("phApplication not provided")
	}
	if networkPolicy == nil {
		return apierrors.NewBadRequest("networkPolicy not provided")
	}

	appID := "app-" + phApplication.Name
	name := appID + "-" + npType
	groupName := escapism.EscapeToPrimehubLabel(phApplication.Spec.GroupName)

	var ingress []networkv1.NetworkPolicyIngressRule
	switch npType {
	case GroupNetworkPolicy:
		ingress = phApplication.GroupNetworkPolicyIngressRule()
	case ProxyNetwrokPolicy:
		ingress = phApplication.ProxyNetworkPolicyIngressRule()
	}

	networkPolicy.Name = name
	networkPolicy.Namespace = phApplication.ObjectMeta.Namespace
	networkPolicy.ObjectMeta.Labels = map[string]string{
		"app":                       "primehub-app",
		"primehub.io/phapplication": appID,
		"primehub.io/group":         groupName,
	}
	networkPolicy.Spec = networkv1.NetworkPolicySpec{
		PodSelector: metav1.LabelSelector{
			MatchLabels: map[string]string{
				"primehub.io/phapplication": appID,
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
	if err := r.Client.Create(ctx, networkPolicy); err != nil {
		return err
	}
	r.Log.Info("Created NetworkPolicy", "Name", networkPolicy.Name)
	return nil
}

func (r *PhApplicationReconciler) updateNetworkPolicy(ctx context.Context, phApplication *v1alpha1.PhApplication, networkPolicy *networkv1.NetworkPolicy, npType string) error {
	switch npType {
	case GroupNetworkPolicy:
		networkPolicy.Spec.Ingress = phApplication.GroupNetworkPolicyIngressRule()
	case ProxyNetwrokPolicy:
		networkPolicy.Spec.Ingress = phApplication.ProxyNetworkPolicyIngressRule()
	}
	if err := r.Client.Update(ctx, networkPolicy); err != nil {
		return err
	}
	r.Log.Info("Updated NetworkPolicy", "Name", networkPolicy.Name)
	return nil
}

func (r *PhApplicationReconciler) reconcileNetworkPolicy(ctx context.Context, phApplication *v1alpha1.PhApplication) error {
	var name string
	namespace := phApplication.ObjectMeta.Namespace
	appID := phApplication.ObjectMeta.Name
	groupNetworkPolicy := &networkv1.NetworkPolicy{}
	proxyNetworkPolicy := &networkv1.NetworkPolicy{}

	name = "app-" + appID + "-" + GroupNetworkPolicy
	groupNetworkPolicyExist, err := r.getPhApplicationObject(ctx, namespace, name, groupNetworkPolicy)
	if err != nil {
		return err
	}

	name = "app-" + appID + "-" + ProxyNetwrokPolicy
	proxyNetworkPolicyExist, err := r.getPhApplicationObject(ctx, namespace, name, proxyNetworkPolicy)
	if err != nil {
		return err
	}

	if groupNetworkPolicyExist {
		// Update NetworkPolicy
		err = r.updateNetworkPolicy(ctx, phApplication, groupNetworkPolicy, GroupNetworkPolicy)
	} else {
		// Create NetworkPolicy
		err = r.createNetworkPolicy(ctx, phApplication, groupNetworkPolicy, GroupNetworkPolicy)
	}

	if proxyNetworkPolicyExist {
		// Update NetworkPolicy
		err = r.updateNetworkPolicy(ctx, phApplication, proxyNetworkPolicy, ProxyNetwrokPolicy)
	} else {
		// Create NetworkPolicy
		err = r.createNetworkPolicy(ctx, phApplication, proxyNetworkPolicy, ProxyNetwrokPolicy)
	}

	return err
}

func (r *PhApplicationReconciler) updatePhApplicationStatus(ctx context.Context, phApplication *v1alpha1.PhApplication) {
	return
}

// +kubebuilder:rbac:groups=primehub.io,resources=phapplications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=primehub.io,resources=phapplications/status,verbs=get;update;patch

func (r *PhApplicationReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	var err error
	var phApplication v1alpha1.PhApplication
	ctx := context.Background()
	log := r.Log.WithValues("phapplication", req.NamespacedName)

	// Fetch phApplication object
	if err = r.Get(ctx, req.NamespacedName, &phApplication); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "Unable to fetch phApplication")
		return ctrl.Result{}, err
	}

	// Reconcile Deployment
	if err = r.reconcileDeployment(ctx, &phApplication); err != nil {
		log.Error(err, "Reconcile Deployment failed")
	}

	// Reconcile Service
	if err = r.reconcileService(ctx, &phApplication); err != nil {
		log.Error(err, "Reconcile Service failed")
	}

	// Reconcile Network-policy
	if err = r.reconcileNetworkPolicy(ctx, &phApplication); err != nil {
		log.Error(err, "Reconcile NetworkPolicy failed")
	}

	// Update status
	r.updatePhApplicationStatus(ctx, &phApplication)

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
