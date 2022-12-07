package controllers

import (
	"context"
	"primehub-controller/api/v1alpha1"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/spf13/viper"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	// +kubebuilder:scaffold:imports
)

var _ = Context("Inside of a new namespace", func() {
	ctx, cancel := context.WithCancel(context.TODO())
	ns := &core.Namespace{}

	BeforeEach(func() {
		*ns = core.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "testns-" + randStringRunes(5)},
		}

		err := k8sClient.Create(ctx, ns)
		Expect(err).NotTo(HaveOccurred(), "failed to create test namespace")

		mgr, err := ctrl.NewManager(cfg, ctrl.Options{})
		Expect(err).NotTo(HaveOccurred(), "failed to create manager")

		controller := &ImageSpecReconciler{
			Client: mgr.GetClient(),
			Log:    ctrl.Log.WithName("controllers").WithName("ImageSpec"),
			Scheme: mgr.GetScheme(),
		}
		err = controller.SetupWithManager(mgr)
		Expect(err).NotTo(HaveOccurred(), "failed to setup controller")

		go func() {
			err := mgr.Start(ctx)
			Expect(err).NotTo(HaveOccurred(), "failed to start manager")
		}()
	})

	AfterEach(func() {
		cancel()

		err := k8sClient.Delete(ctx, ns)
		Expect(err).NotTo(HaveOccurred(), "failed to delete test namespace")
	})

	Describe("when no push secret exists", func() {

		It("should not create a new ImageSpecJob resource after a new ImageSpec was created", func() {

			now := metav1.Now()
			imageSpec := &v1alpha1.ImageSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testresource",
					Namespace: ns.Name,
				},
				Spec: v1alpha1.ImageSpecSpec{
					BaseImage: "jupyter/base-notebook",
					Packages: v1alpha1.ImageSpecSpecPackages{
						Apt:   []string{"curl"},
						Pip:   []string{},
						Conda: []string{},
					},
					UpdateTime: &now,
				},
			}

			err := k8sClient.Create(ctx, imageSpec)
			Expect(err).NotTo(HaveOccurred(), "failed to create test ImageSpec resource")
			job := &v1alpha1.ImageSpecJob{}

			Consistently(
				func() metav1.StatusReason {
					err := getResourceFunc(ctx, client.ObjectKey{Name: imageSpec.Name + "-96ff7925", Namespace: imageSpec.Namespace}, job)()
					return errors.ReasonForError(err)
				},
				time.Second*5, time.Millisecond*500).Should(Equal(metav1.StatusReasonNotFound))
		})
	})

	Describe("when no existing resources exist", func() {

		It("should create a new ImageSpecJob resource after a new ImageSpec was created", func() {

			now := metav1.Now()
			secretName := "testsecret"
			secret := &core.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: ns.Name,
				},
				Data: map[string][]byte{
					".dockerconfigjson": []byte("{\"auths\": {\"https://example.com\": {\"auth\": \"dXNlcjpwYXNz\"}}}"),
				},
				Type: core.SecretTypeDockerConfigJson,
			}
			viper.Set("customImage.pushSecretName", secretName)

			imageSpec := &v1alpha1.ImageSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testresource",
					Namespace: ns.Name,
				},
				Spec: v1alpha1.ImageSpecSpec{
					BaseImage: "jupyter/base-notebook",
					Packages: v1alpha1.ImageSpecSpecPackages{
						Apt:   []string{"curl"},
						Pip:   []string{},
						Conda: []string{},
					},
					UpdateTime: &now,
				},
			}

			err := k8sClient.Create(ctx, secret)
			err = k8sClient.Create(ctx, imageSpec)
			Expect(err).NotTo(HaveOccurred(), "failed to create test ImageSpec resource")
			job := &v1alpha1.ImageSpecJob{}

			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: imageSpec.Name + "-96ff7925", Namespace: imageSpec.Namespace}, job),
				time.Second*5, time.Millisecond*500).Should(BeNil())

			Expect(job.ObjectMeta.Labels["imagespecs.primehub.io/name"]).To(Equal(imageSpec.Name))
		})
	})
})
