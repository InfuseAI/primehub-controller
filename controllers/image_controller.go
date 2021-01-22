package controllers

import (
	"context"
	"fmt"
	"github.com/spf13/viper"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"primehub-controller/api/v1alpha1"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ImageReconciler reconciles a Image object
type ImageReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

type ImageSpecJobAction int

const (
	unknown ImageSpecJobAction = iota
	create
	cancel
	rebuild
	update
)

func makeImageControllerAction(r *ImageReconciler, ctx context.Context, image *v1alpha1.Image) (ImageSpecJobAction, *v1alpha1.ImageSpecJob) {
	if image.Spec.ImageSpec.BaseImage != "" {
		imageSpecJob := &v1alpha1.ImageSpecJob{}
		imageSpecJobName := getImageSpecJobName(image)
		err := r.Get(ctx, client.ObjectKey{Namespace: image.Namespace, Name: imageSpecJobName}, imageSpecJob)
		if apierrors.IsNotFound(err) && image.Spec.ImageSpec.Cancel == false {
			return create, nil
		}
		if image.Spec.ImageSpec.Cancel == true && image.Status.JobCondiction.Phase != CustomImageJobStatusCanceled {
			return cancel, imageSpecJob
		}
		return update, imageSpecJob
	}
	return unknown, nil
}

func getImageSpecJobName(image *v1alpha1.Image) string {
	if image == nil {
		return ""
	}
	if image.Spec.ImageSpec.BaseImage == "" {
		return ""
	}
	return image.Name
}

func createImageSpecJob(r *ImageReconciler, ctx context.Context, image *v1alpha1.Image) error {
	if image == nil {
		return fmt.Errorf("creiateImageSpecJob do not provide image")
	}

	pushSecretName := viper.GetString("customImage.pushSecretName")
	repoPrefix := viper.GetString("customImage.pushRepoPrefix")

	hash := computeHash(image.Spec.ImageSpec.BaseImage, image.Spec.ImageSpec.Packages)

	imageSpecJob := &v1alpha1.ImageSpecJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:        getImageSpecJobName(image),
			Namespace:   image.Namespace,
			Annotations: map[string]string{"image.primehub.io/hash": hash},
			Labels: map[string]string{
				"image.primehub.io/name":  image.ObjectMeta.Name,
				"image.primehub.io/group": image.Spec.GroupName,
				"app":                     "primehub-custom-group-image",
			},
		},
		Spec: v1alpha1.ImageSpecJobSpec{
			BaseImage:   image.Spec.ImageSpec.BaseImage,
			PullSecret:  image.Spec.PullSecret,
			Packages:    image.Spec.ImageSpec.Packages,
			TargetImage: image.ObjectMeta.Name + ":" + hash,
			PushSecret:  pushSecretName,
			RepoPrefix:  repoPrefix,
			UpdateTime:  image.Spec.ImageSpec.UpdateTime,
		},
	}

	if err := ctrl.SetControllerReference(image, imageSpecJob, r.Scheme); err != nil {
		return err
	}
	if err := r.Client.Create(ctx, imageSpecJob); err != nil {
		return err
	}

	return nil
}

func cancelImageSpecJob(r *ImageReconciler, ctx context.Context, image *v1alpha1.Image, imageSpecJob *v1alpha1.ImageSpecJob) error {
	if image == nil {
		return fmt.Errorf("cancelImageSpecJob do not provide image")
	}
	if imageSpecJob == nil {
		return fmt.Errorf("cancelImageSpecJob do not provide imageSpecJob")
	}

	if err := r.Delete(ctx, imageSpecJob, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
		return err
	}

	imageClone := image.DeepCopy()
	imageClone.Status.JobCondiction.Phase = CustomImageJobStatusCanceled

	if err := r.Status().Update(ctx, imageClone); err != nil {
		return err
	}

	return nil
}

func updateImageStatus(r *ImageReconciler, ctx context.Context, image *v1alpha1.Image, imageSpecJob *v1alpha1.ImageSpecJob) error {
	if image == nil {
		return fmt.Errorf("updateImage do not provide image")
	}

	if imageSpecJob != nil {
		imageClone := image.DeepCopy()
		imageClone.Status.JobCondiction.JobName = imageSpecJob.Name
		imageClone.Status.JobCondiction.Phase = imageSpecJob.Status.Phase
		if imageClone.Status.JobCondiction.Phase == CustomImageJobStatusSucceeded {
			url := imageSpecJob.Spec.RepoPrefix + "/" + imageSpecJob.Spec.TargetImage
			imageClone.Status.JobCondiction.Image = url
			imageClone.Spec.Url = url
		}
		if err := r.Update(ctx, imageClone); err != nil {
			return err
		}
	}

	return nil
}

// +kubebuilder:rbac:groups=primehub.io,resources=images,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=primehub.io,resources=images/status,verbs=get;update;patch

func (r *ImageReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	var image v1alpha1.Image
	var err error
	ctx := context.Background()
	log := r.Log.WithValues("image", req.NamespacedName)

	if err = r.Get(ctx, req.NamespacedName, &image); err != nil {
		if ignoreNotFound(err) != nil {
			log.Error(err, "unable to fetch Image")
		}
		return ctrl.Result{}, ignoreNotFound(err)
	}

	// Skip when image CRD without imageSpec
	if image.Spec.ImageSpec.BaseImage == "" {
		log.Info("Regular Image", "GroupName", image.Spec.GroupName, "URL", image.Spec.Url, "GPU URL", image.Spec.UrlForGpu)
		return ctrl.Result{}, nil
	}

	// Create / Update / Rebuild / Cancel ImageSpecJob
	action, imageSpecJob := makeImageControllerAction(r, ctx, &image)
	switch action {
	case create:
		log.Info("Create ImageSpecJob")
		err = createImageSpecJob(r, ctx, &image)
		if err != nil {
			return ctrl.Result{}, err
		}
	case cancel:
		log.Info("Cancel ImageSpecJob")
		err := cancelImageSpecJob(r, ctx, &image, imageSpecJob)
		if err != nil {
			return ctrl.Result{}, nil
		}
	case update:
		log.Info("Update Image Status")
		err = updateImageStatus(r, ctx, &image, imageSpecJob)
		if err != nil {
			return ctrl.Result{}, err
		}
	default:
		log.Info("Unknown action")
	}

	return ctrl.Result{}, nil
}

func (r *ImageReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Image{}).
		Owns(&v1alpha1.ImageSpecJob{}).
		Complete(r)
}
