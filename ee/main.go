package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	primehubv1alpha1 "primehub-controller/ee/api/v1alpha1"

	"primehub-controller/ee/controllers"
	"primehub-controller/ee/pkg/license"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
	"github.com/spf13/viper"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)

	_ = primehubv1alpha1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

func main() {
	loadConfig()

	var metricsAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	ctrl.SetLogger(zap.New(func(o *zap.Options) {
		o.Development = true
	}))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		LeaderElection:     enableLeaderElection,
		Port:               9443,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	licenseReconciler := &controllers.LicenseReconciler{
		Client:            mgr.GetClient(),
		Log:               ctrl.Log.WithName("controllers").WithName("License"),
		Scheme:            mgr.GetScheme(),
		ResourceName:      license.RESOURCE_NAME,
		ResourceNamespace: license.RESOURCE_NAMESPACE,
		RequeueAfter:      license.CHECK_EXPIRY_INTERVAL,
	}
	if err := licenseReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "License")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder

	go func() {
		if err := licenseReconciler.EnsureLicense(mgr); err != nil {
			panic("unable to initialize license reconciler: " + err.Error())
		}
	}()

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func loadConfig() {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("/etc/primehub-controller/") // path to look for the config file in
	viper.AddConfigPath(".")                         // optionally look for config in the working directory
	err := viper.ReadInConfig()                      // Find and read the config file
	if err != nil {                                  // Handle errors reading the config file
		panic(fmt.Errorf("Fatal error config file: %s \n", err))
	}

	configs := []string{
		"customImage.pushSecretName",
		"customImage.pushRepoPrefix",
		"jobSubmission.graphqlEndpoint",
		"jobSubmission.graphqlSecret",
		"jobSubmission.workingDirSize",
		"jobSubmission.defaultActiveDeadlineSeconds",
		"jobSubmission.defaultTTLSecondsAfterFinished",
	}

	for _, config := range configs {
		if viper.GetString(config) == "" {
			panic(config + " is required in config.yaml")
		}
	}

	// Check jobSubmission.workingDirSize must correct
	if _, err := resource.ParseQuantity(viper.GetString("jobSubmission.workingDirSize")); err != nil {
		panic(fmt.Errorf("cannot parse jobSubmission.workingDirSize: %v", err))
	}

	// Check customImage.buildJob.resources
	resourceNames := []string{"cpu", "memory"}
	if len(viper.GetStringMap("customImage.buildJob.resources.requests")) > 0 {
		for _, resourceName := range resourceNames {
			if _, err := resource.ParseQuantity(viper.GetString("customImage.buildJob.resources.requests." + resourceName)); err != nil {
				panic(fmt.Errorf("cannot parse customImage.buildJob.resources.requests.%s: %v", resourceName, err))
			}
		}
	}
	if len(viper.GetStringMap("customImage.buildJob.resources.limits")) > 0 {
		for _, resourceName := range resourceNames {
			if _, err := resource.ParseQuantity(viper.GetString("customImage.buildJob.resources.limits." + resourceName)); err != nil {
				panic(fmt.Errorf("cannot parse customImage.buildJob.resources.limits.%s: %v", resourceName, err))
			}
		}
	}
}
