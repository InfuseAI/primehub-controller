package controllers

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	// +kubebuilder:scaffold:imports

	primehubv1alpha1 "primehub-controller/api/v1alpha1"
)

var _ = Context("Inside of a new namespace", func() {
	ctx := context.TODO()
	ns := &core.Namespace{}

	var stopCh chan struct{}

	BeforeEach(func() {
		stopCh = make(chan struct{})
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
			err := mgr.Start(stopCh)
			Expect(err).NotTo(HaveOccurred(), "failed to start manager")
		}()
	})

	AfterEach(func() {
		close(stopCh)

		err := k8sClient.Delete(ctx, ns)
		Expect(err).NotTo(HaveOccurred(), "failed to delete test namespace")
	})

	Describe("when no existing resources exist", func() {

		It("should create a new ImageSpecJob resource after a new ImageSpec was created", func() {

			now := metav1.Now()
			imageSpec := &primehubv1alpha1.ImageSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testresource",
					Namespace: ns.Name,
				},
				Spec: primehubv1alpha1.ImageSpecSpec{
					BaseImage: "jupyter/base-notebook",
					Packages: primehubv1alpha1.ImageSpecSpecPackages{
						Apt:   []string{"curl"},
						Pip:   []string{},
						Conda: []string{},
					},
					UpdateTime: &now,
				},
			}

			err := k8sClient.Create(ctx, imageSpec)
			Expect(err).NotTo(HaveOccurred(), "failed to create test ImageSpec resource")
			job := &primehubv1alpha1.ImageSpecJob{}
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: imageSpec.Name + "-96ff7925", Namespace: imageSpec.Namespace}, job),
				time.Second*5, time.Millisecond*500).Should(BeNil())

			Expect(job.ObjectMeta.Labels["imagespecs.primehub.io/name"]).To(Equal(imageSpec.Name))
		})
	})
})
