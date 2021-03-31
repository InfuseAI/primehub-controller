package controllers

import (
	"context"
	log "github.com/go-logr/logr/testing"
	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"primehub-controller/api/v1alpha1"
	phcache "primehub-controller/pkg/cache"
	"primehub-controller/pkg/graphql"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"testing"
)

func TestPhApplicationReconciler_Reconcile(t *testing.T) {
	logger := log.NullLogger{}
	scheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(scheme)
	_ = appv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = networkv1.AddToScheme(scheme)
	name := "phapplication-unittest"
	namespace := "hub"
	instanceType := "cpu-1"
	group := "group"
	httpPort := int32(5000)

	phApplication := &v1alpha1.PhApplication{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: v1alpha1.PhApplicationSpec{
			DisplayName:  "Unit Test",
			GroupName:    group,
			InstanceType: instanceType,
			Scope:        "public",
			Stop:         false,
			PodTemplate: v1alpha1.PhApplicationPodTemplate{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "mlflow",
							Image:   "larribas/mlflow:1.9.1",
							Command: []string{"mlflow"},
							Args: []string{
								"server",
								"--host",
								"0.0.0.0",
								"--default-artifact-root",
								"$(DEFAULT_ARTIFACT_ROOT)",
								"--backend-store-uri",
								"$(BACKEND_STORE_URI)",
								"--static-prefix",
								"$(PRIMEHUB_APP_BASE_URL)",
							},
							Env: []corev1.EnvVar{
								{Name: "Foo", Value: "bar"},
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 5000,
									Protocol:      corev1.ProtocolTCP,
								},
							},
						},
					},
				},
			},
			SvcTemplate: v1alpha1.PhApplicationSvcTemplate{
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Port:       5000,
							Protocol:   corev1.ProtocolTCP,
							TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 5000},
						},
					},
				},
			},
			HTTPPort: &httpPort,
		},
	}
	fakeClient := fake.NewFakeClientWithScheme(scheme, []runtime.Object{phApplication}...)
	primehubCache := phcache.NewPrimeHubCache(nil)
	r := &PhApplicationReconciler{fakeClient, logger, scheme, primehubCache}
	req := controllerruntime.Request{}

	primehubCache.InstanceType.Set("instanceType:"+instanceType, &graphql.DtoInstanceType{
		Name:        "cpu-1",
		Description: "cpu-1",
		DisplayName: "cpu-1",
		Global:      true,
		Spec: graphql.DtoInstanceTypeSpec{
			LimitsCpu:      1,
			RequestsCpu:    1,
			RequestsMemory: "100G",
			LimitsMemory:   "100G",
			RequestsGpu:    0,
			LimitsGpu:      0,
			NodeSelector:   nil,
			Tolerations:    nil,
		},
	}, primehubCache.ExpiredTime)
	primehubCache.Group.Set("group:"+group, &graphql.DtoGroup{
		Name:                            "puser",
		DisplayName:                     "primehub user",
		EnabledSharedVolume:             true,
		SharedVolumeCapacity:            "1",
		HomeSymlink:                     nil,
		LaunchGroupOnly:                 nil,
		QuotaCpu:                        0,
		QuotaGpu:                        0,
		QuotaMemory:                     "",
		UserVolumeCapacity:              "",
		ProjectQuotaCpu:                 0,
		ProjectQuotaGpu:                 0,
		ProjectQuotaMemory:              "",
		EnabledDeployment:               false,
		JobDefaultActiveDeadlineSeconds: nil,
		InstanceTypes:                   nil,
		Images:                          nil,
		Datasets:                        nil,
	}, primehubCache.ExpiredTime)
	primehubCache.Datasets.Set("globalDatasets", []*graphql.DtoDataset{}, primehubCache.ExpiredTime)

	t.Run("Create PhApplication", func(t *testing.T) {
		var err error
		_, err = r.Reconcile(req)
		if err != nil {
			t.Errorf("PhApplication should create without error")
		}

		err = r.Get(context.Background(), client.ObjectKey{Name: name, Namespace: namespace}, phApplication)
		if err != nil || phApplication.Status.Phase == v1alpha1.ApplicationError {
			t.Errorf("PhApplication should create without status error")
		}

		createdDeployment := &appv1.Deployment{}
		err = r.Get(context.Background(), client.ObjectKey{Name: phApplication.AppName(), Namespace: phApplication.Namespace}, createdDeployment)
		if err != nil {
			t.Errorf("PhApplicationReconciler should create a deployment without error")
		}

		createdService := &corev1.Service{}
		err = r.Get(context.Background(), client.ObjectKey{Name: phApplication.AppName(), Namespace: phApplication.Namespace}, createdService)
		if err != nil {
			t.Errorf("PhApplicationReconciler should create a service without error")
		}

		createdNetworkPolicy := &networkv1.NetworkPolicy{}
		err = r.Get(context.Background(), client.ObjectKey{Name: phApplication.AppName() + "-" + GroupNetworkPolicy, Namespace: phApplication.Namespace}, createdNetworkPolicy)
		if err != nil {
			t.Errorf("PhApplicationReconciler should create a group network policy without error")
		}

		err = r.Get(context.Background(), client.ObjectKey{Name: phApplication.AppName() + "-" + ProxyNetwrokPolicy, Namespace: phApplication.Namespace}, createdNetworkPolicy)
		if err != nil {
			t.Errorf("PhApplicationReconciler should create a proxy network policy without error")
		}
	})
}
