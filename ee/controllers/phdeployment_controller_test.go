package controllers

import (
	"context"
	primehubv1alpha1 "primehub-controller/ee/api/v1alpha1"
	"primehub-controller/pkg/graphql"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	log "github.com/go-logr/logr/testing"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestConfigurationError(t *testing.T) {
	// Setup
	logger := log.NullLogger{}
	scheme := runtime.NewScheme()
	_ = primehubv1alpha1.AddToScheme(scheme)
	_ = v1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewFakeClientWithScheme(scheme)

	type fields struct {
		Client                client.Client
		Log                   logr.Logger
		Scheme                *runtime.Scheme
		GraphqlClient         graphql.AbstractGraphqlClient
		Ingress               PhIngress
		PrimehubUrl           string
		EngineImage           string
		EngineImagePullPolicy corev1.PullPolicy
	}

	type podRuntime struct {
		isImageError         bool
		isTerminatedError    bool
		isUnschedulableError bool
	}

	tests := map[string]struct {
		fields fields

		configurationError       bool
		configurationErrorReason string

		podRuntimeError podRuntime

		quotaNotEnough bool

		allPodsReady bool

		expectedPhase   primehubv1alpha1.PhDeploymentPhase
		expectedMessage string
	}{
		"Group Not Found": {
			fields{Log: logger, Client: fakeClient, Scheme: scheme},
			true,
			"Group Not Found",
			podRuntime{
				isImageError:         false,
				isTerminatedError:    false,
				isUnschedulableError: false,
			},
			false,
			false,
			primehubv1alpha1.DeploymentFailed,
			"Group Not Found",
		},
		"Model deployment is disabled": {
			fields{Log: logger, Client: fakeClient, Scheme: scheme},
			true,
			"The model deployment is not enabled for the selected group",
			podRuntime{
				isImageError:         false,
				isTerminatedError:    false,
				isUnschedulableError: false,
			},
			false,
			false,
			primehubv1alpha1.DeploymentFailed,
			"The model deployment is not enabled for the selected group",
		},
		"InstanceType not found": {
			fields{Log: logger, Client: fakeClient, Scheme: scheme},
			true,
			"InstanceType not found",
			podRuntime{
				isImageError:         false,
				isTerminatedError:    false,
				isUnschedulableError: false,
			},
			false,
			false,
			primehubv1alpha1.DeploymentFailed,
			"InstanceType not found",
		},
	}

	for name, tt := range tests {
		r := &PhDeploymentReconciler{
			Client:                tt.fields.Client,
			Log:                   tt.fields.Log,
			Scheme:                tt.fields.Scheme,
			GraphqlClient:         tt.fields.GraphqlClient,
			Ingress:               tt.fields.Ingress,
			PrimehubUrl:           tt.fields.PrimehubUrl,
			EngineImage:           tt.fields.EngineImage,
			EngineImagePullPolicy: tt.fields.EngineImagePullPolicy,
		}

		phDeployment := &primehubv1alpha1.PhDeployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "TestGroupNotFound",
				Namespace: "Unit-Test",
			},
			Spec: primehubv1alpha1.PhDeploymentSpec{
				Predictors: []primehubv1alpha1.PhDeploymentPredictor{
					{
						Name:     "predictor1",
						Replicas: 1,
					},
				},
			},
			Status: primehubv1alpha1.PhDeploymentStatus{},
		}

		failedPodList := make([]FailedPodStatus, 0)
		failedPodList = NewFailedPodList(tt.podRuntimeError.isImageError, tt.podRuntimeError.isTerminatedError, tt.podRuntimeError.isUnschedulableError)

		replicas := int32(1)
		availableReplicas := int32(0)
		updatedReplicas := int32(0)

		if tt.allPodsReady {
			availableReplicas = int32(1)
			updatedReplicas = int32(1)
		}

		deployment := &v1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "TestGroupNotFound",
				Namespace: "Unit-Test",
			},
			Spec: v1.DeploymentSpec{
				Replicas: &replicas,
			},
			Status: v1.DeploymentStatus{
				AvailableReplicas: availableReplicas,
				UpdatedReplicas:   updatedReplicas,
				Replicas:          int32(1),
			},
		}

		r.updateStatus(phDeployment, deployment, tt.configurationError, tt.configurationErrorReason, failedPodList)
		if phDeployment.Status.Phase != tt.expectedPhase {
			t.Errorf("%s: unexpected phase after update status. Expected %v, saw %v", name, tt.expectedPhase, phDeployment.Status.Phase)
		}
		if phDeployment.Status.Messsage != tt.expectedMessage {
			t.Errorf("%s: unexpected message after update status. Expected %v, saw %v", name, tt.expectedMessage, phDeployment.Status.Messsage)
		}
	}
}

func TestRuntimeError(t *testing.T) {
	// Setup
	logger := log.NullLogger{}
	scheme := runtime.NewScheme()
	_ = primehubv1alpha1.AddToScheme(scheme)
	_ = v1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewFakeClientWithScheme(scheme)

	type fields struct {
		Client                client.Client
		Log                   logr.Logger
		Scheme                *runtime.Scheme
		GraphqlClient         graphql.AbstractGraphqlClient
		Ingress               PhIngress
		PrimehubUrl           string
		EngineImage           string
		EngineImagePullPolicy corev1.PullPolicy
	}

	type podRuntime struct {
		isImageError         bool
		isTerminatedError    bool
		isUnschedulableError bool
	}

	tests := map[string]struct {
		fields fields

		configurationError       bool
		configurationErrorReason string

		podRuntimeError podRuntime

		quotaNotEnough bool

		allPodsReady bool

		expectedPhase   primehubv1alpha1.PhDeploymentPhase
		expectedMessage string
	}{
		"Pod Image Error": {
			fields{Log: logger, Client: fakeClient, Scheme: scheme},
			false,
			"",
			podRuntime{
				isImageError:         true,
				isTerminatedError:    false,
				isUnschedulableError: false,
			},
			false,
			false, // allPodsReady false
			primehubv1alpha1.DeploymentDeploying,
			"wrong image settings",
		},
		"Pod Terminated Error": {
			fields{Log: logger, Client: fakeClient, Scheme: scheme},
			false,
			"",
			podRuntime{
				isImageError:         false,
				isTerminatedError:    true,
				isUnschedulableError: false,
			},
			false,
			false, // allPodsReady false
			primehubv1alpha1.DeploymentDeploying,
			"pod is terminated",
		},
		"Pod Unschedulable Error": {
			fields{Log: logger, Client: fakeClient, Scheme: scheme},
			false,
			"",
			podRuntime{
				isImageError:         false,
				isTerminatedError:    false,
				isUnschedulableError: true,
			},
			false,
			false, // allPodsReady false
			primehubv1alpha1.DeploymentDeploying,
			"Certain pods unschedulable",
		},
	}

	for name, tt := range tests {
		r := &PhDeploymentReconciler{
			Client:                tt.fields.Client,
			Log:                   tt.fields.Log,
			Scheme:                tt.fields.Scheme,
			GraphqlClient:         tt.fields.GraphqlClient,
			Ingress:               tt.fields.Ingress,
			PrimehubUrl:           tt.fields.PrimehubUrl,
			EngineImage:           tt.fields.EngineImage,
			EngineImagePullPolicy: tt.fields.EngineImagePullPolicy,
		}

		phDeployment := &primehubv1alpha1.PhDeployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "TestPodRunTimeError",
				Namespace: "Unit-Test",
			},
			Spec: primehubv1alpha1.PhDeploymentSpec{
				Predictors: []primehubv1alpha1.PhDeploymentPredictor{
					{
						Name:     "predictor1",
						Replicas: 1,
					},
				},
			},
			Status: primehubv1alpha1.PhDeploymentStatus{},
		}

		failedPodList := make([]FailedPodStatus, 0)
		failedPodList = NewFailedPodList(tt.podRuntimeError.isImageError, tt.podRuntimeError.isTerminatedError, tt.podRuntimeError.isUnschedulableError)
		replicas := int32(1)
		availableReplicas := int32(0)
		updatedReplicas := int32(0)

		if tt.allPodsReady {
			availableReplicas = int32(1)
			updatedReplicas = int32(1)
		}
		deployment := &v1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "TestGroupNotFound",
				Namespace: "Unit-Test",
			},
			Spec: v1.DeploymentSpec{
				Replicas: &replicas,
			},
			Status: v1.DeploymentStatus{
				AvailableReplicas: availableReplicas,
				UpdatedReplicas:   updatedReplicas,
				Replicas:          int32(1),
			},
		}

		r.updateStatus(phDeployment, deployment, tt.configurationError, tt.configurationErrorReason, failedPodList)
		if phDeployment.Status.Phase != tt.expectedPhase {
			t.Errorf("%s: unexpected phase after update status. Expected %v, saw %v", name, tt.expectedPhase, phDeployment.Status.Phase)
		}
		if !strings.Contains(phDeployment.Status.Messsage, tt.expectedMessage) {
			t.Errorf("%s: unexpected message after update status. Expected %v, saw %v", name, tt.expectedMessage, phDeployment.Status.Messsage)
		}
	}
}

func TestQuotaNotEnough(t *testing.T) {
	// Setup
	logger := log.NullLogger{}
	scheme := runtime.NewScheme()
	_ = primehubv1alpha1.AddToScheme(scheme)
	_ = v1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewFakeClientWithScheme(scheme)

	type fields struct {
		Client                client.Client
		Log                   logr.Logger
		Scheme                *runtime.Scheme
		GraphqlClient         graphql.AbstractGraphqlClient
		Ingress               PhIngress
		PrimehubUrl           string
		EngineImage           string
		EngineImagePullPolicy corev1.PullPolicy
	}

	type podRuntime struct {
		isImageError         bool
		isTerminatedError    bool
		isUnschedulableError bool
	}

	tests := map[string]struct {
		fields fields

		configurationError       bool
		configurationErrorReason string

		podRuntimeError podRuntime

		quotaNotEnough bool

		allPodsReady bool
		message      string

		expectedPhase   primehubv1alpha1.PhDeploymentPhase
		expectedMessage string
	}{
		"Quota not enough": {
			fields{Log: logger, Client: fakeClient, Scheme: scheme},
			false,
			"",
			podRuntime{
				isImageError:         false,
				isTerminatedError:    false,
				isUnschedulableError: false,
			},
			true,  // quota not enough
			false, // allPodsReady
			`admission webhook "resources-validation-webhook.primehub.io" denied the request: Group phusers exceeded cpu quota: 1, requesting 2.0`,
			primehubv1alpha1.DeploymentDeploying,
			"Group phusers exceeded cpu quota",
		},
	}

	for name, tt := range tests {
		r := &PhDeploymentReconciler{
			Client:                tt.fields.Client,
			Log:                   tt.fields.Log,
			Scheme:                tt.fields.Scheme,
			GraphqlClient:         tt.fields.GraphqlClient,
			Ingress:               tt.fields.Ingress,
			PrimehubUrl:           tt.fields.PrimehubUrl,
			EngineImage:           tt.fields.EngineImage,
			EngineImagePullPolicy: tt.fields.EngineImagePullPolicy,
		}

		phDeployment := &primehubv1alpha1.PhDeployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "TestGroupNotFound",
				Namespace: "Unit-Test",
			},
			Spec: primehubv1alpha1.PhDeploymentSpec{
				Predictors: []primehubv1alpha1.PhDeploymentPredictor{
					{
						Name:     "predictor1",
						Replicas: 1,
					},
				},
			},
			Status: primehubv1alpha1.PhDeploymentStatus{},
		}

		failedPodList := make([]FailedPodStatus, 0)
		failedPodList = NewFailedPodList(tt.podRuntimeError.isImageError, tt.podRuntimeError.isTerminatedError, tt.podRuntimeError.isUnschedulableError)
		replicas := int32(1)
		availableReplicas := int32(0)
		updatedReplicas := int32(0)

		if tt.allPodsReady {
			availableReplicas = int32(1)
			updatedReplicas = int32(1)
		}
		// quota not enough deployment
		deployment := &v1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "TestQuotaNotEnough",
				Namespace: "Unit-Test",
			},
			Spec: v1.DeploymentSpec{
				Replicas: &replicas,
			},
			Status: v1.DeploymentStatus{
				AvailableReplicas: availableReplicas,
				UpdatedReplicas:   updatedReplicas,
				Replicas:          int32(1),
				Conditions: []v1.DeploymentCondition{
					{
						Type:    v1.DeploymentReplicaFailure,
						Status:  corev1.ConditionTrue,
						Reason:  "FailedCreate",
						Message: tt.message,
					},
				},
			},
		}

		r.updateStatus(phDeployment, deployment, tt.configurationError, tt.configurationErrorReason, failedPodList)
		if phDeployment.Status.Phase != tt.expectedPhase {
			t.Errorf("%s: unexpected phase after update status. Expected %v, saw %v", name, tt.expectedPhase, phDeployment.Status.Phase)
		}
		if !strings.Contains(phDeployment.Status.Messsage, tt.expectedMessage) {
			t.Errorf("%s: unexpected message after update status. Expected %v, saw %v", name, tt.expectedMessage, phDeployment.Status.Messsage)
		}
	}
}

func TestPhDeploymentReconciler_reconcileSecret(t *testing.T) {
	// Setup
	ctx := context.TODO()
	logger := log.NullLogger{}
	scheme := runtime.NewScheme()
	_ = primehubv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewFakeClientWithScheme(scheme)

	phdeploymentPrivate := &primehubv1alpha1.PhDeployment{}
	phdeploymentPrivate.Namespace = "Unit-Test"
	phdeploymentPrivate.Name = "TestReconcileSecret"
	phdeploymentPrivate.Spec.Endpoint.AccessType = primehubv1alpha1.DeploymentPrivateEndpoint
	phdeploymentPrivate.Spec.Endpoint.Clients = []primehubv1alpha1.PhDeploymentEndpointClient{
		{Name: "test-user-1", Token: "unit-test-htpasswd"},
	}

	phdeploymentPublic := &primehubv1alpha1.PhDeployment{}
	phdeploymentPublic.Namespace = "Unit-Test"
	phdeploymentPublic.Name = "TestReconcileSecret"
	phdeploymentPublic.Spec.Endpoint.AccessType = primehubv1alpha1.DeploymentPublicEndpoint

	type fields struct {
		Client                client.Client
		Log                   logr.Logger
		Scheme                *runtime.Scheme
		GraphqlClient         graphql.AbstractGraphqlClient
		Ingress               PhIngress
		PrimehubUrl           string
		EngineImage           string
		EngineImagePullPolicy corev1.PullPolicy
	}
	type args struct {
		ctx          context.Context
		phDeployment *primehubv1alpha1.PhDeployment
	}
	tests := []struct {
		name        string
		fields      fields
		args        args
		wantErr     bool
		wantIngress bool
	}{
		{"Create secret when accessType is private", fields{Log: logger, Client: fakeClient, Scheme: scheme}, args{ctx, phdeploymentPrivate}, false, true},
		{"Don't create secret when accessType is public", fields{Log: logger, Client: fakeClient, Scheme: scheme}, args{ctx, phdeploymentPublic}, false, false},
		{"Update secret when accessType is private", fields{Log: logger, Client: fakeClient, Scheme: scheme}, args{ctx, phdeploymentPrivate}, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &PhDeploymentReconciler{
				Client:                tt.fields.Client,
				Log:                   tt.fields.Log,
				Scheme:                tt.fields.Scheme,
				GraphqlClient:         tt.fields.GraphqlClient,
				Ingress:               tt.fields.Ingress,
				PrimehubUrl:           tt.fields.PrimehubUrl,
				EngineImage:           tt.fields.EngineImage,
				EngineImagePullPolicy: tt.fields.EngineImagePullPolicy,
			}
			if err := r.reconcileSecret(tt.args.ctx, tt.args.phDeployment); (err != nil) != tt.wantErr {
				t.Errorf("reconcileSecret() error = %v, wantErr %v", err, tt.wantErr)
			}
			secret := &corev1.Secret{}
			err := r.Client.Get(ctx, getSecretKey(tt.args.phDeployment), secret)
			if err != nil {
				if tt.wantIngress != apierrors.IsNotFound(err) {

				}
			}

			if tt.wantIngress == false && err != nil && secret.Name != "" {
				t.Errorf("reconcileSecret() should not create ingress. error = %v, wantIngress %v", err, tt.wantIngress)
			} else if tt.wantIngress == true && apierrors.IsNotFound(err) == true {
				t.Errorf("reconcileSecret() should create ingress. error = %v, wantIngress %v", err, tt.wantIngress)
			}
		})
	}
}
