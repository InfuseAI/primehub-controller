package controllers

import (
	"context"
	log "github.com/go-logr/logr/testing"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"primehub-controller/api/v1alpha1"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"testing"
	"time"
)

func Test_createImageSpecJob(t *testing.T) {
	logger := log.NullLogger{}
	scheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(scheme)
	_ = v1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewFakeClientWithScheme(scheme)
	imageWithCustomBuild := &v1alpha1.Image{
		ObjectMeta: metav1.ObjectMeta{Name: "image-with-custom-build"},
		Spec: v1alpha1.ImageCrdSpec{
			Url:         "",
			UrlForGpu:   "",
			Type:        "cpu",
			DisplayName: "Image with custom build",
			Description: "Image with custom build",
			PullSecret:  "",
			GroupName:   "",
			ImageSpec: v1alpha1.ImageCrdSpecImageSpec{
				BaseImage:  "unit-test/base-image",
				PullSecret: "",
				Packages:   v1alpha1.ImageSpecSpecPackages{},
				UpdateTime: &metav1.Time{},
				Cancel:     false,
			},
		},
	}
	imageWithoutCustomBuild := &v1alpha1.Image{
		ObjectMeta: metav1.ObjectMeta{Name: "image-without-custom-build"},
		Spec: v1alpha1.ImageCrdSpec{
			Url:         "unit-test/without-custom-build",
			UrlForGpu:   "",
			Type:        "cpu",
			DisplayName: "Image without custom build",
			Description: "Image without custom build",
			PullSecret:  "",
			GroupName:   "",
			ImageSpec:   v1alpha1.ImageCrdSpecImageSpec{},
		},
	}
	type args struct {
		r     *ImageReconciler
		ctx   context.Context
		image *v1alpha1.Image
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "Create Image CRD without custom build",
			args: args{
				r:     &ImageReconciler{Client: fakeClient, Log: logger, Scheme: scheme},
				ctx:   context.Background(),
				image: imageWithoutCustomBuild,
			},
			wantErr: true,
		},
		{
			name: "Create Image CRD with custom build",
			args: args{
				r:     &ImageReconciler{Client: fakeClient, Log: logger, Scheme: scheme},
				ctx:   context.Background(),
				image: imageWithCustomBuild,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := createImageSpecJob(tt.args.r, tt.args.ctx, tt.args.image); (err != nil) != tt.wantErr {
				t.Errorf("createImageSpecJob() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_isImageCustomBuild(t *testing.T) {
	imageWithCustomBuild := &v1alpha1.Image{
		ObjectMeta: metav1.ObjectMeta{Name: "image-with-custom-build"},
		Spec: v1alpha1.ImageCrdSpec{
			Url:         "",
			UrlForGpu:   "",
			Type:        "cpu",
			DisplayName: "Image with custom build",
			Description: "Image with custom build",
			PullSecret:  "",
			GroupName:   "",
			ImageSpec: v1alpha1.ImageCrdSpecImageSpec{
				BaseImage:  "unit-test/base-image",
				PullSecret: "",
				Packages:   v1alpha1.ImageSpecSpecPackages{},
				UpdateTime: &metav1.Time{},
				Cancel:     false,
			},
		},
	}
	imageWithoutCustomBuild := &v1alpha1.Image{
		ObjectMeta: metav1.ObjectMeta{Name: "image-without-custom-build"},
		Spec: v1alpha1.ImageCrdSpec{
			Url:         "unit-test/without-custom-build",
			UrlForGpu:   "",
			Type:        "cpu",
			DisplayName: "Image without custom build",
			Description: "Image without custom build",
			PullSecret:  "",
			GroupName:   "",
			ImageSpec:   v1alpha1.ImageCrdSpecImageSpec{},
		},
	}
	type args struct {
		image *v1alpha1.Image
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{"Image CRD with custom build", args{imageWithCustomBuild}, true},
		{"Image CRD without custom build", args{imageWithoutCustomBuild}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isImageCustomBuild(tt.args.image); got != tt.want {
				t.Errorf("isImageCustomBuild() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_makeImageControllerAction(t *testing.T) {
	ctx := context.Background()
	logger := log.NullLogger{}
	scheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(scheme)
	_ = v1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	var name string
	var namespace string
	var imageCreateTime time.Time

	// For Create test case
	name = "create-custom-image"
	namespace = "hub"
	imageCreateTime = time.Date(2020, time.December, 31, 23, 59, 59, 0, time.Local)
	createCustomImage := &v1alpha1.Image{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: v1alpha1.ImageCrdSpec{
			Url:         "",
			UrlForGpu:   "",
			Type:        "cpu",
			DisplayName: "Image with custom build",
			Description: "Image with custom build",
			PullSecret:  "",
			GroupName:   "",
			ImageSpec: v1alpha1.ImageCrdSpecImageSpec{
				BaseImage:  "unit-test/base-image",
				PullSecret: "",
				Packages:   v1alpha1.ImageSpecSpecPackages{},
				UpdateTime: &metav1.Time{imageCreateTime},
				Cancel:     false,
			},
		},
	}

	// For Cancel test case
	name = "cancel-custom-image"
	namespace = "hub"
	imageCreateTime = time.Date(2020, time.December, 31, 23, 59, 59, 0, time.Local)
	mockCancelImageSpecJob := &v1alpha1.ImageSpecJob{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: v1alpha1.ImageSpecJobSpec{
			BaseImage:   "unit-test/base-image",
			PullSecret:  "",
			Packages:    v1alpha1.ImageSpecSpecPackages{},
			TargetImage: "unit-test/base-image:test",
			PushSecret:  "",
			RepoPrefix:  "",
			UpdateTime:  &metav1.Time{Time: imageCreateTime},
		},
	}
	cancelCustomImage := &v1alpha1.Image{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: v1alpha1.ImageCrdSpec{
			Url:         "",
			UrlForGpu:   "",
			Type:        "cpu",
			DisplayName: name,
			Description: name,
			PullSecret:  "",
			GroupName:   "",
			ImageSpec: v1alpha1.ImageCrdSpecImageSpec{
				BaseImage:  "unit-test/base-image",
				PullSecret: "",
				Packages:   v1alpha1.ImageSpecSpecPackages{},
				UpdateTime: &metav1.Time{Time: imageCreateTime},
				Cancel:     true,
			},
		},
		Status: v1alpha1.ImageStatus{
			JobCondiction: v1alpha1.ImageSpecStatus{
				Phase:   "Running",
				JobName: name,
				Image:   "unit-test/base-image:1234",
			},
		},
	}

	// For Rebuild test case
	name = "rebuild-custom-image"
	namespace = "hub"
	imageCreateTime = time.Date(2020, time.December, 31, 23, 59, 59, 0, time.Local)
	mockRebuildImageSpecJob := &v1alpha1.ImageSpecJob{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: v1alpha1.ImageSpecJobSpec{
			BaseImage:   "unit-test/base-image",
			PullSecret:  "",
			Packages:    v1alpha1.ImageSpecSpecPackages{},
			TargetImage: "unit-test/base-image:test",
			PushSecret:  "",
			RepoPrefix:  "",
			UpdateTime:  &metav1.Time{Time: imageCreateTime},
		},
	}
	imageCreateTime = time.Date(2021, time.January, 1, 23, 59, 59, 0, time.Local)
	rebuildCustomImage := &v1alpha1.Image{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: v1alpha1.ImageCrdSpec{
			Url:         "unit-test/base-image:1234",
			UrlForGpu:   "",
			Type:        "cpu",
			DisplayName: name,
			Description: name,
			PullSecret:  "",
			GroupName:   "",
			ImageSpec: v1alpha1.ImageCrdSpecImageSpec{
				BaseImage:  "unit-test/base-image",
				PullSecret: "",
				Packages: v1alpha1.ImageSpecSpecPackages{
					Apt: []string{"curl"},
				},
				Cancel:     false,
				UpdateTime: &metav1.Time{Time: imageCreateTime},
			},
		},
		Status: v1alpha1.ImageStatus{
			JobCondiction: v1alpha1.ImageSpecStatus{
				Phase:   CustomImageJobStatusSucceeded,
				JobName: name,
				Image:   "unit-test/base-image:1234",
			},
		},
	}

	// For fake client
	fakeClientForCreate := fake.NewFakeClientWithScheme(scheme)
	fakeClientForCancel := fake.NewFakeClientWithScheme(scheme, []runtime.Object{mockCancelImageSpecJob}...)
	fakeClientForRebuild := fake.NewFakeClientWithScheme(scheme, []runtime.Object{mockRebuildImageSpecJob}...)

	type args struct {
		r     *ImageReconciler
		image *v1alpha1.Image
	}
	tests := []struct {
		name         string
		args         args
		action       ImageSpecJobAction
		imageSpecJob *v1alpha1.ImageSpecJob
	}{
		{
			name: "Create Custom Image",
			args: args{
				r:     &ImageReconciler{Client: fakeClientForCreate, Log: logger, Scheme: scheme},
				image: createCustomImage,
			},
			action:       create,
			imageSpecJob: nil,
		},
		{
			name: "Cancel Custom Image",
			args: args{
				r:     &ImageReconciler{fakeClientForCancel, logger, scheme},
				image: cancelCustomImage,
			},
			action:       cancel,
			imageSpecJob: mockCancelImageSpecJob,
		},
		{
			name: "Rebuild Custom Image when ImageSpecJob Success",
			args: args{
				r:     &ImageReconciler{fakeClientForRebuild, logger, scheme},
				image: rebuildCustomImage,
			},
			action:       rebuild,
			imageSpecJob: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := makeImageControllerAction(tt.args.r, ctx, tt.args.image)
			if got != tt.action {
				t.Errorf("makeImageControllerAction() got = %v, want %v", got, tt.action)
			}
			if !reflect.DeepEqual(got1, tt.imageSpecJob) {
				t.Errorf("makeImageControllerAction() got1 = %v, want %v", got1, tt.imageSpecJob)
			}
		})
	}
}
