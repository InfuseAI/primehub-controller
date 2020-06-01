package controllers

import (
	"context"
	"github.com/go-logr/logr"
	log "github.com/go-logr/logr/testing"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	primehubv1alpha1 "primehub-controller/ee/api/v1alpha1"
	"primehub-controller/pkg/graphql"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"testing"
)

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
		GraphqlClient         *graphql.GraphqlClient
		Ingress               PhIngress
		PrimehubUrl           string
		EngineImage           string
		EngineImagePullPolicy v1.PullPolicy
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
		{"Don't secret when accessType is public", fields{Log: logger, Client: fakeClient, Scheme: scheme}, args{ctx, phdeploymentPublic}, false, false},
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
