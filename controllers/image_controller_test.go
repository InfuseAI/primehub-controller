package controllers

import (
	"context"
	log "github.com/go-logr/logr/testing"
	"github.com/spf13/viper"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"primehub-controller/api/v1alpha1"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"testing"
	"time"
)

func Test_createImageSpecJob(t *testing.T) {
	var name string
	var namespace string
	var imageCreateTime time.Time
	ctx := context.Background()
	logger := log.NullLogger{}
	scheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(scheme)
	_ = v1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewFakeClientWithScheme(scheme)
	name = "create-custom-image"
	namespace = "hub"
	imageCreateTime = time.Date(2020, time.December, 31, 23, 59, 59, 0, time.Local)
	imageWithCustomBuild := &v1alpha1.Image{
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
				Packages: v1alpha1.ImageSpecSpecPackages{
					Apt:   []string{"curl", "nmap"},
					Pip:   []string{"youtube-dl"},
					Conda: nil,
				},
				UpdateTime: &metav1.Time{Time: imageCreateTime},
				Cancel:     false,
			},
		},
	}
	name = "create-regular-image"
	namespace = "hub"
	imageWithoutCustomBuild := &v1alpha1.Image{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
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
		image *v1alpha1.Image
	}
	tests := []struct {
		name                        string
		args                        args
		wantErr                     bool
		wantImageSpecJobName        string
		wantImageSpecJobTargetImage string
	}{
		{
			name: "Create Image CRD without custom build",
			args: args{
				r:     &ImageReconciler{Client: fakeClient, Log: logger, Scheme: scheme},
				image: imageWithoutCustomBuild,
			},
			wantErr: true,
		},
		{
			name: "Create Image CRD with custom build",
			args: args{
				r:     &ImageReconciler{Client: fakeClient, Log: logger, Scheme: scheme},
				image: imageWithCustomBuild,
			},
			wantErr:                     false,
			wantImageSpecJobName:        imageWithCustomBuild.Name,
			wantImageSpecJobTargetImage: imageWithCustomBuild.Name + ":" + computeHash(imageWithCustomBuild.Spec.ImageSpec.BaseImage, imageWithCustomBuild.Spec.ImageSpec.Packages),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := createImageSpecJob(tt.args.r, ctx, tt.args.image); (err != nil) != tt.wantErr {
				t.Errorf("createImageSpecJob() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr == false {
				createdImageSpecJob := &v1alpha1.ImageSpecJob{}
				err := tt.args.r.Get(ctx, client.ObjectKey{Name: tt.args.image.Name, Namespace: tt.args.image.Namespace}, createdImageSpecJob)
				if err != nil {
					t.Errorf("createImageSpecJob() doesn't create ImageSpecJob")
				}

				if tt.wantImageSpecJobName != "" &&
					tt.wantImageSpecJobName != createdImageSpecJob.Name {
					t.Errorf("createImageSpecJob() name = %v, wantImageSpecJobName %v", createdImageSpecJob.Name, tt.wantImageSpecJobName)
				}

				if tt.wantImageSpecJobTargetImage != "" &&
					tt.wantImageSpecJobTargetImage != createdImageSpecJob.Spec.TargetImage {
					t.Errorf("createImageSpecJob() TargetImage = %v, wantImageSpecJobTargetImage %v", createdImageSpecJob.Spec.TargetImage, tt.wantImageSpecJobTargetImage)
				}
			}

		})
	}
}

func Test_cancelImageSpecJob(t *testing.T) {
	var name string
	var namespace string
	var imageCreateTime time.Time
	ctx := context.Background()
	logger := log.NullLogger{}
	scheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(scheme)
	_ = v1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	name = "cancel-custom-image"
	namespace = "hub"
	imageCreateTime = time.Date(2020, time.December, 31, 23, 59, 59, 0, time.Local)
	customImageSpecJob := &v1alpha1.ImageSpecJob{
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
	customImage := &v1alpha1.Image{
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

	fakeClient := fake.NewFakeClientWithScheme(scheme, []runtime.Object{customImageSpecJob, customImage}...)

	type args struct {
		r            *ImageReconciler
		image        *v1alpha1.Image
		imageSpecJob *v1alpha1.ImageSpecJob
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "Cancel ImageSpecJob",
			args: args{
				r: &ImageReconciler{
					Client: fakeClient,
					Log:    logger,
					Scheme: scheme,
				},
				image:        customImage,
				imageSpecJob: customImageSpecJob,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := cancelImageSpecJob(tt.args.r, ctx, tt.args.image, tt.args.imageSpecJob); (err != nil) != tt.wantErr {
				t.Errorf("cancelImageSpecJob() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_rebuildImageSpecJob(t *testing.T) {
	var name string
	var namespace string
	var imageCreateTime time.Time
	ctx := context.Background()
	logger := log.NullLogger{}
	scheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(scheme)
	_ = v1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	name = "rebbuild-custom-image"
	namespace = "hub"
	imageCreateTime = time.Date(2020, time.December, 31, 23, 59, 59, 0, time.Local)
	customImageSpecJob := &v1alpha1.ImageSpecJob{
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
	imageCreateTime = time.Date(2021, time.December, 31, 23, 59, 59, 0, time.Local)
	customImage := &v1alpha1.Image{
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
				Cancel:     false,
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

	fakeClient := fake.NewFakeClientWithScheme(scheme, []runtime.Object{customImageSpecJob, customImage}...)

	type args struct {
		r            *ImageReconciler
		image        *v1alpha1.Image
		imageSpecJob *v1alpha1.ImageSpecJob
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "Rebuild Custom Image",
			args: args{
				r:            &ImageReconciler{fakeClient, logger, scheme},
				image:        customImage,
				imageSpecJob: customImageSpecJob,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := rebuildImageSpecJob(tt.args.r, ctx, tt.args.image, tt.args.imageSpecJob); (err != nil) != tt.wantErr {
				t.Errorf("rebuildImageSpecJob() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_updateImageStatus(t *testing.T) {
	var name string
	var namespace string
	var imageCreateTime time.Time
	ctx := context.Background()
	logger := log.NullLogger{}
	scheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(scheme)
	_ = v1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	name = "update-custom-image"
	namespace = "hub"
	imageCreateTime = time.Date(2020, time.December, 31, 23, 59, 59, 0, time.Local)
	customImageSpecJobRunning := &v1alpha1.ImageSpecJob{
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
		Status: v1alpha1.ImageSpecJobStatus{
			Phase:      "Running",
			StartTime:  &metav1.Time{Time: imageCreateTime},
			FinishTime: nil,
			PodName:    name,
		},
	}

	customImageSpecJobSuccesseded := &v1alpha1.ImageSpecJob{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: v1alpha1.ImageSpecJobSpec{
			BaseImage:   "unit-test/base-image",
			PullSecret:  "",
			Packages:    v1alpha1.ImageSpecSpecPackages{},
			TargetImage: name + ":1234",
			PushSecret:  "test-secret",
			RepoPrefix:  "infuseai",
			UpdateTime:  &metav1.Time{Time: imageCreateTime},
		},
		Status: v1alpha1.ImageSpecJobStatus{
			Phase:      CustomImageJobStatusSucceeded,
			StartTime:  &metav1.Time{Time: imageCreateTime},
			FinishTime: nil,
			PodName:    name,
		},
	}
	customImage := &v1alpha1.Image{
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
				Cancel:     false,
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

	fakeClient := fake.NewFakeClientWithScheme(scheme, []runtime.Object{customImage}...)

	type args struct {
		r            *ImageReconciler
		image        *v1alpha1.Image
		imageSpecJob *v1alpha1.ImageSpecJob
	}
	tests := []struct {
		name           string
		args           args
		wantErr        bool
		wantPhase      string
		wantURL        string
		wantPullSecret string
	}{
		{
			name: "Update Custom Image - Phase Running",
			args: args{
				r:            &ImageReconciler{fakeClient, logger, scheme},
				image:        customImage,
				imageSpecJob: customImageSpecJobRunning,
			},
			wantErr:   false,
			wantPhase: "Running",
		},
		{
			name: "Update Custom Image - Phase Successeded",
			args: args{
				r:            &ImageReconciler{fakeClient, logger, scheme},
				image:        customImage,
				imageSpecJob: customImageSpecJobSuccesseded,
			},
			wantErr:        false,
			wantPhase:      CustomImageJobStatusSucceeded,
			wantURL:        "infuseai/" + customImage.Name + ":1234",
			wantPullSecret: customImageSpecJobSuccesseded.Spec.PushSecret,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := updateImageStatus(tt.args.r, ctx, tt.args.image, tt.args.imageSpecJob); (err != nil) != tt.wantErr {
				t.Errorf("updateImageStatus() error = %v, wantErr %v", err, tt.wantErr)
			}

			updatedImage := &v1alpha1.Image{}
			_ = tt.args.r.Get(ctx, client.ObjectKey{Namespace: customImage.Namespace, Name: customImage.Name}, updatedImage)
			if updatedImage.Status.JobCondiction.Phase != tt.wantPhase {
				t.Errorf("updateImageStatus() phase = %v, wantPhase %v", updatedImage.Status.JobCondiction.Phase, tt.wantPhase)
			}

			if tt.wantURL != "" && tt.wantURL != updatedImage.Spec.Url && tt.wantURL != updatedImage.Spec.UrlForGpu {
				t.Errorf("updateImageStatus() URL = %v, wantURL %v", updatedImage.Spec.Url, tt.wantURL)
			}

			if tt.wantPullSecret != "" && tt.wantPullSecret != updatedImage.Spec.PullSecret {
				t.Errorf("updateImageStatus() PullSecret = %v, wantPullSecret %v", updatedImage.Spec.PullSecret, tt.wantPullSecret)
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
				UpdateTime: &metav1.Time{Time: imageCreateTime},
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

	// For Update test case
	name = "update-custom-image"
	namespace = "hub"
	imageCreateTime = time.Date(2020, time.December, 31, 23, 59, 59, 0, time.Local)
	mockUpdateImageSpecJob := &v1alpha1.ImageSpecJob{
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
		Status: v1alpha1.ImageSpecJobStatus{
			Phase:      "Running",
			StartTime:  &metav1.Time{Time: imageCreateTime},
			FinishTime: nil,
			PodName:    name,
		},
	}
	updateCustomImage := &v1alpha1.Image{
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
				Phase:   "",
				JobName: name,
				Image:   "",
			}},
	}

	// For Unknown test case
	name = "no-custom-image"
	namespace = "hub"
	noCustomImage := &v1alpha1.Image{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
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

	// For Skip test case
	name = "canceled-custom-image"
	namespace = "hub"
	canceledCustomImage := &v1alpha1.Image{
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
				Phase:   CustomImageJobStatusCanceled,
				JobName: name,
				Image:   "unit-test/base-image:1234",
			},
		},
	}

	// For fake client
	fakeClientForCreate := fake.NewFakeClientWithScheme(scheme)
	fakeClientForCancel := fake.NewFakeClientWithScheme(scheme, []runtime.Object{mockCancelImageSpecJob}...)
	fakeClientForRebuild := fake.NewFakeClientWithScheme(scheme, []runtime.Object{mockRebuildImageSpecJob}...)
	fakeClientForUpdate := fake.NewFakeClientWithScheme(scheme, []runtime.Object{mockUpdateImageSpecJob}...)
	fakeClientForUnknown := fake.NewFakeClientWithScheme(scheme)
	fakeClientForSkip := fake.NewFakeClientWithScheme(scheme)

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
			imageSpecJob: mockRebuildImageSpecJob,
		},
		{
			name: "Update Custom Image when ImageSpecJob Created",
			args: args{
				r:     &ImageReconciler{fakeClientForUpdate, logger, scheme},
				image: updateCustomImage,
			},
			action:       update,
			imageSpecJob: mockUpdateImageSpecJob,
		},
		{
			name: "Unknown Action when no Custom Image",
			args: args{
				r:     &ImageReconciler{fakeClientForUnknown, logger, scheme},
				image: noCustomImage,
			},
			action:       unknown,
			imageSpecJob: nil,
		},
		{
			name: "Skip Action when image had been canceled",
			args: args{
				r:     &ImageReconciler{fakeClientForSkip, logger, scheme},
				image: canceledCustomImage,
			},
			action:       skip,
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

func TestImageReconciler_Reconcile(t *testing.T) {
	ctx := context.Background()
	logger := log.NullLogger{}
	scheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(scheme)
	_ = v1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	var name string
	var namespace string
	var imageCreateTime time.Time

	name = "custom-image"
	namespace = "hub"
	imageCreateTime = time.Date(2020, time.December, 31, 23, 59, 59, 0, time.Local)

	image := &v1alpha1.Image{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: v1alpha1.ImageCrdSpec{
			Url:         "",
			UrlForGpu:   "",
			Type:        "cpu",
			DisplayName: "Custom Image build",
			Description: "Custom Image Build",
			PullSecret:  "",
			GroupName:   "unit-test",
			ImageSpec: v1alpha1.ImageCrdSpecImageSpec{
				BaseImage:  "unit-test/base-notebook",
				PullSecret: "",
				Packages: v1alpha1.ImageSpecSpecPackages{
					Apt:   []string{"curl", "nmap"},
					Pip:   []string{"youtube-dl"},
					Conda: nil,
				},
				UpdateTime: &metav1.Time{Time: imageCreateTime},
				Cancel:     false,
			},
		},
	}

	fakeClient := fake.NewFakeClientWithScheme(scheme, []runtime.Object{image}...)
	r := &ImageReconciler{fakeClient, logger, scheme}
	req := ctrl.Request{}

	pushSecret := "test-secret"
	prefix := "infuseai"
	viper.Set("customImage.pushSecretName", pushSecret)
	viper.Set("customImage.pushRepoPrefix", prefix)

	createdImageSpecJob := &v1alpha1.ImageSpecJob{}
	var targetImage string
	t.Run("Create Image Build", func(t *testing.T) {
		var err error
		_, err = r.Reconcile(req)
		if err != nil {
			t.Errorf("ImageReconciler should create without error")
		}

		err = r.Get(ctx, client.ObjectKey{Name: image.Name, Namespace: image.Namespace}, createdImageSpecJob)
		if err != nil {
			t.Errorf("ImageReconciler should create ImageSpecJob without error")
		}

		if createdImageSpecJob.Name != image.Name {
			t.Errorf("ImageReconciler should create ImageSpecJob with name %v not %v", image.Name, createdImageSpecJob.Name)
		}

		hash := computeHash(createdImageSpecJob.Spec.BaseImage, createdImageSpecJob.Spec.Packages)
		targetImage = createdImageSpecJob.Name + ":" + hash
		if targetImage != createdImageSpecJob.Spec.TargetImage {
			t.Errorf("ImageReconciler should create ImageSpecJob with TargetImage %v not %v", targetImage, createdImageSpecJob.Spec.TargetImage)
		}
	})

	// Update Image Phase
	var url string
	updatedImage := &v1alpha1.Image{}
	t.Run("Update Image Status", func(t *testing.T) {
		var err error

		createdImageSpecJob.Status.Phase = "Running"
		_ = r.Update(ctx, createdImageSpecJob)
		_, err = r.Reconcile(req)
		if err != nil {
			t.Errorf("ImageReconciler should update without error")
		}

		// Update Running status
		_ = r.Get(ctx, client.ObjectKey{Name: image.Name, Namespace: image.Namespace}, updatedImage)
		if updatedImage.Status.JobCondiction.Phase != createdImageSpecJob.Status.Phase {
			t.Errorf("ImageReconciler should update Image status as %v not %v", createdImageSpecJob.Status.Phase, updatedImage.Status.JobCondiction.Phase)
		}

		// Update Successeded status with URL
		createdImageSpecJob.Status.Phase = CustomImageJobStatusSucceeded
		_ = r.Update(ctx, createdImageSpecJob)
		_, err = r.Reconcile(req)
		if err != nil {
			t.Errorf("ImageReconciler should update without error")
		}

		// Update Running status
		_ = r.Get(ctx, client.ObjectKey{Name: image.Name, Namespace: image.Namespace}, updatedImage)
		if updatedImage.Status.JobCondiction.Phase != createdImageSpecJob.Status.Phase {
			t.Errorf("ImageReconciler should update Image status as %v not %v", createdImageSpecJob.Status.Phase, updatedImage.Status.JobCondiction.Phase)
		}

		url = prefix + "/" + targetImage
		if url != updatedImage.Spec.Url {
			t.Errorf("ImageReconciler should update Image URL as %v not %v", url, updatedImage.Spec.Url)
		}

		if pushSecret != updatedImage.Spec.PullSecret {
			t.Errorf("ImageReconciler should update Image PullSecret as %v not %v", pushSecret, updatedImage.Spec.PullSecret)
		}
	})

	// Rebuild
	rebuildImageSpecJob := &v1alpha1.ImageSpecJob{}
	t.Run("Rebuild Image Status", func(t *testing.T) {
		var err error
		imageCreateTime = time.Date(2021, time.December, 31, 23, 59, 59, 0, time.Local)
		updatedImage.Spec.ImageSpec.UpdateTime = &metav1.Time{Time: imageCreateTime}
		_ = r.Update(ctx, updatedImage)

		_, err = r.Reconcile(req)
		if err != nil {
			t.Errorf("ImageReconciler should rebuild without error")
		}

		_ = r.Get(ctx, client.ObjectKey{Name: updatedImage.Name, Namespace: updatedImage.Namespace}, rebuildImageSpecJob)
		if rebuildImageSpecJob.Spec.UpdateTime.Unix() != imageCreateTime.Unix() {
			t.Errorf("ImageReconciler should rebuild with new updateTime")
		}
	})

	// Cancel
	canceledImageSpecJob := &v1alpha1.ImageSpecJob{}
	t.Run("Cancel Image Build", func(t *testing.T) {
		var err error
		updatedImage.Spec.ImageSpec.Cancel = true
		_ = r.Update(ctx, updatedImage)

		_, err = r.Reconcile(req)
		if err != nil {
			t.Errorf("ImageReconciler should cancel without error")
		}

		err = r.Get(ctx, client.ObjectKey{Name: updatedImage.Name, Namespace: updatedImage.Namespace}, canceledImageSpecJob)
		if apierrors.IsNotFound(err) != true {
			t.Errorf("ImageReconciler should delete ImageJobSpec")
		}
	})

	// Skip
	t.Run("Skip Image Build if Already cCanceled", func(t *testing.T) {
		var err error
		_, err = r.Reconcile(req)
		if err != nil {
			t.Errorf("ImageReconciler should do skip without error")
		}
	})
}
