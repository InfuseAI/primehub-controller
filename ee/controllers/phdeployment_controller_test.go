package controllers

import (
	"context"
	"fmt"
	primehubv1alpha1 "primehub-controller/ee/api/v1alpha1"
	"primehub-controller/pkg/graphql"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	log "github.com/go-logr/logr"
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
	logger := log.Discard()
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
		if phDeployment.Status.Message != tt.expectedMessage {
			t.Errorf("%s: unexpected message after update status. Expected %v, saw %v", name, tt.expectedMessage, phDeployment.Status.Message)
		}
	}
}

func TestRuntimeError(t *testing.T) {
	// Setup
	logger := log.Discard()
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
		if !strings.Contains(phDeployment.Status.Message, tt.expectedMessage) {
			t.Errorf("%s: unexpected message after update status. Expected %v, saw %v", name, tt.expectedMessage, phDeployment.Status.Message)
		}
	}
}

func TestQuotaNotEnough(t *testing.T) {
	// Setup
	logger := log.Discard()
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
		if !strings.Contains(phDeployment.Status.Message, tt.expectedMessage) {
			t.Errorf("%s: unexpected message after update status. Expected %v, saw %v", name, tt.expectedMessage, phDeployment.Status.Message)
		}
	}
}

func TestPhDeploymentReconciler_reconcileSecret(t *testing.T) {
	// Setup
	ctx := context.TODO()
	logger := log.Discard()
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

func TestExtractNFSConfig(t *testing.T) {
	tests := []struct {
		modelUri string
		server   string
		path     string
		wantErr  bool
	}{
		{modelUri: "nfs://1.2.3.4/model/to/path", server: "1.2.3.4", path: "/model/to/path", wantErr: false},
		{modelUri: "nfs://5.5.6.6", server: "5.5.6.6", path: "/", wantErr: false},
		{modelUri: "nfs://5.5.6.8/", server: "5.5.6.8", path: "/", wantErr: false},
		{modelUri: "nfs:7.8.6.6", server: "7.8.6.6", path: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.modelUri, func(t *testing.T) {
			server, path, err := ExtractNFSConfig(tt.modelUri)
			if tt.wantErr && err == nil {
				t.Errorf("should get error from modelUri: %s", tt.modelUri)
			}

			if !tt.wantErr && (tt.server != server || tt.path != path) {
				t.Errorf("failed to extract server from %s, => server:%s, path:%s", tt.modelUri, server, path)
			}
		})
	}
}

func TestPhDeployment_BuildModelContainer(t *testing.T) {
	// Setup
	ctx := context.TODO()
	logger := log.Discard()
	scheme := runtime.NewScheme()
	_ = primehubv1alpha1.AddToScheme(scheme)
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

	type args struct {
		ctx          context.Context
		phDeployment *primehubv1alpha1.PhDeployment
	}

	graphqlClient := &FakeGraphQLForModelContainer{}
	tests := []struct {
		name             string
		fields           fields
		args             args
		initImage        string
		injectMlflowEnvs bool
	}{
		{"ModelUri from phfs:// should mount PHFS", fields{Log: logger, Client: fakeClient, Scheme: scheme, GraphqlClient: graphqlClient}, args{ctx, phdeploymentWithModelUri("phfs:///path/to/my-model")}, "model-init-image", false},
		{"ModelUri from nfs:// should mount NFS", fields{Log: logger, Client: fakeClient, Scheme: scheme, GraphqlClient: graphqlClient}, args{ctx, phdeploymentWithModelUri("nfs://1.2.3.4/path/to/my-model")}, "model-init-image", false},
		{"ModelUri from models:/ should use mlflow-storage-initializer and all mlflow envs", fields{Log: logger, Client: fakeClient, Scheme: scheme, GraphqlClient: graphqlClient}, args{ctx, phdeploymentWithModelUri("models:/foo-bar-bar/5566")}, "mlflow-model-init-image", true},
		{"ModelUri from gs should use model-storage-initializer", fields{Log: logger, Client: fakeClient, Scheme: scheme, GraphqlClient: graphqlClient}, args{ctx, phdeploymentWithModelUri("gs://my-bucket/path/to/my-model")}, "model-init-image", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &PhDeploymentReconciler{
				Client:                             tt.fields.Client,
				Log:                                tt.fields.Log,
				Scheme:                             tt.fields.Scheme,
				GraphqlClient:                      tt.fields.GraphqlClient,
				Ingress:                            tt.fields.Ingress,
				PrimehubUrl:                        tt.fields.PrimehubUrl,
				EngineImage:                        tt.fields.EngineImage,
				EngineImagePullPolicy:              tt.fields.EngineImagePullPolicy,
				PhfsEnabled:                        true,
				PhfsPVC:                            "primehub-store",
				ModelStorageInitializerImage:       "model-init-image",
				MlflowModelStorageInitializerImage: "mlflow-model-init-image",
			}

			deployment, err := r.buildDeployment(tt.args.ctx, tt.args.phDeployment)
			if err != nil {
				t.Errorf("buildDeployment() error = %v", err)
			}

			initContainer := deployment.Spec.Template.Spec.InitContainers[0]
			if initContainer.Image != tt.initImage {
				t.Errorf("model-init-image should be %v, but %v", tt.initImage, initContainer.Image)
			}

			modelStorageMount := initContainer.VolumeMounts[0]
			if modelStorageMount.Name != "model-storage" || modelStorageMount.MountPath != "/mnt/models" {
				t.Errorf("should have the first volume that named model-storage with path /mnt/models, but [%v]", modelStorageMount)
			}

			verifyNFSVolume(t, tt.args.phDeployment.Spec.Predictors[0].ModelURI, initContainer, deployment)

			result, _ := tt.fields.GraphqlClient.FetchByUserId("any-id")
			if tt.injectMlflowEnvs == true && !foundEachEnvsInMlflowSettings(initContainer.Env, result) {
				t.Errorf("should have each envs in from mlflow. envs[%v], mlflow[%v]", initContainer.Env, result.Data.User.Groups[0].Mlflow)
			}
		})
	}
}

func verifyNFSVolume(t *testing.T, modelURI string, initContainer corev1.Container, deployment *v1.Deployment) {
	if !strings.HasPrefix(modelURI, "nfs://") {
		return
	}
	nfsMount := initContainer.VolumeMounts[1]
	if nfsMount.Name != "nfs" && nfsMount.MountPath != "/nfs" {
		t.Errorf("should have nfs volumeMount at /nfs")
	}

	nfsVolume := corev1.Volume{}
	for idx := range deployment.Spec.Template.Spec.Volumes {
		v := deployment.Spec.Template.Spec.Volumes[idx]
		if v.Name == "nfs" {
			nfsVolume = v
		}
	}

	if nfsVolume.Name != "nfs" || nfsVolume.NFS == nil {
		t.Errorf("NFS Volume not found")
	}

	server, path, _ := ExtractNFSConfig(modelURI)
	if server != nfsVolume.NFS.Server || nfsVolume.NFS.Path != "/" || nfsVolume.NFS.ReadOnly != true {
		t.Errorf("invalid NFS volume %v", nfsVolume.NFS)
	}

	expectedPath := "file:///nfs" + path
	if initContainer.Args[0] != expectedPath {
		t.Errorf("invalid modelPath %s (expected: %s)", initContainer.Args[0], expectedPath)
	}
}

func foundEachEnvsInMlflowSettings(envVars []corev1.EnvVar, result *graphql.DtoResult) bool {
	stat := make(map[string]int)
	variables := graphql.BuildMlflowEnvironmentVariables("tester", result)
	variables = append(variables, envVars...)

	for _, v := range variables {
		k := fmt.Sprintf("%v:%v", v.Name, v.Value)
		if val, ok := stat[k]; ok {
			stat[k] = val + 1
		} else {
			stat[k] = 1
		}
	}

	for _, e := range stat {
		if e != 2 {
			return false
		}
	}
	return true
}

func phdeploymentWithModelUri(modelUri string) *primehubv1alpha1.PhDeployment {
	p := &primehubv1alpha1.PhDeployment{}
	p.Namespace = "Unit-Test"
	p.Name = "TestReconcileSecret"
	p.Spec.Predictors = []primehubv1alpha1.PhDeploymentPredictor{
		primehubv1alpha1.PhDeploymentPredictor{
			Name:            "model",
			Replicas:        1,
			ModelImage:      "infuseai/model-image",
			InstanceType:    "cpu-1",
			ModelURI:        modelUri,
			ImagePullSecret: "",
			Metadata:        nil,
		},
	}
	p.Spec.GroupName = "tester"
	p.Spec.UserId = "tester"
	return p
}

type FakeGraphQLForModelContainer struct{}

func (FakeGraphQLForModelContainer) FetchByUserId(s string) (*graphql.DtoResult, error) {
	return &graphql.DtoResult{
		Data: graphql.DtoData{
			System: graphql.DtoSystem{},
			User: graphql.DtoUser{
				Groups: []graphql.DtoGroup{
					{
						Name:                            "tester",
						DisplayName:                     "",
						EnabledSharedVolume:             false,
						SharedVolumeCapacity:            "",
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
						InstanceTypes: []graphql.DtoInstanceType{
							graphql.DtoInstanceType{
								Name:        "cpu-1",
								Description: "",
								DisplayName: "",
								Global:      false,
								Spec: graphql.DtoInstanceTypeSpec{
									LimitsCpu:      1,
									RequestsCpu:    1,
									RequestsMemory: "1G",
									LimitsMemory:   "1G",
									RequestsGpu:    0,
									LimitsGpu:      0,
									NodeSelector:   nil,
									Tolerations:    nil,
								},
							},
						},
						Images:   nil,
						Datasets: nil,
						Mlflow: &graphql.DtoMlflow{
							TrackingUri: "http://example.primehub.io/foo-bar-bar",
							UiUrl:       "http://example.primehub.io/foo-bar-bar",
							TrackingEnvs: []corev1.EnvVar{
								corev1.EnvVar{
									Name:  "MLFLOW_TRACKING_USERNAME",
									Value: "foo",
								},
								corev1.EnvVar{
									Name:  "MLFLOW_TRACKING_PASSWORD",
									Value: "bar",
								},
							},
							ArtifactEnvs: []corev1.EnvVar{
								corev1.EnvVar{
									Name:  "AWS_ACCESS_KEY_ID",
									Value: "aws-key-id",
								}, corev1.EnvVar{
									Name:  "AWS_SECRET_ACCESS_KEY",
									Value: "aws-secret-key",
								}, corev1.EnvVar{
									Name:  "EXTRA",
									Value: "more",
								},
							},
						},
					},
				},
			},
		},
	}, nil
}

func (FakeGraphQLForModelContainer) QueryServer(m map[string]interface{}) ([]byte, error) {
	panic("implement me")
}

func (FakeGraphQLForModelContainer) FetchGroupEnableModelDeployment(s string) (bool, error) {
	panic("implement me")
}

func (FakeGraphQLForModelContainer) FetchGroupInfo(s string) (*graphql.DtoGroup, error) {
	panic("implement me")
}

func (FakeGraphQLForModelContainer) FetchGroupInfoByName(s string) (*graphql.DtoGroup, error) {
	panic("implement me")
}

func (FakeGraphQLForModelContainer) FetchGlobalDatasets() ([]graphql.DtoDataset, error) {
	panic("implement me")
}

func (FakeGraphQLForModelContainer) FetchInstanceTypeInfo(s string) (*graphql.DtoInstanceType, error) {
	panic("implement me")
}

func (FakeGraphQLForModelContainer) FetchTimeZone() (string, error) {
	panic("implement me")
}

func (FakeGraphQLForModelContainer) NotifyPhJobEvent(id string, eventType string) (float64, error) {
	panic("implement me")
}
