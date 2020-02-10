package main

import (
	"flag"
	"fmt"
	"os"
	"primehub-controller/pkg/graphql"
	"time"

	primehubv1alpha1 "primehub-controller/api/v1alpha1"
	eeprimehubv1alpha1 "primehub-controller/ee/api/v1alpha1"

	"primehub-controller/controllers"
	eecontrollers "primehub-controller/ee/controllers"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlzap "sigs.k8s.io/controller-runtime/pkg/log/zap"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

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
	_ = eeprimehubv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var debug bool

	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&debug, "debug", false, "enables debug logs")
	flag.Parse()

	// Levels in logr correspond to custom debug levels in Zap.
	// Any given level in logr is represents by its inverse in Zap (zapLevel = -1*logrLevel).
	// For example V(2) is equivalent to log level -2 in Zap, while V(1) is equivalent to Zap's DebugLevel.
	// zap.InfoLevel = 0; zap.DebugLevel = -1
	// r.Log.Info() is INFO in Zap
	// r.Log.V(1).Info() is DEBUG in Zap
	// ref: https://github.com/go-logr/zapr
	l := zap.NewAtomicLevelAt(zap.InfoLevel)
	if debug == true {
		l = zap.NewAtomicLevelAt(zap.DebugLevel)
	}

	ctrl.SetLogger(ctrlzap.New(func(o *ctrlzap.Options) {
		o.Development = true
		o.Level = &l
	}))

	loadConfig()

	stopChan := ctrl.SetupSignalHandler()

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

	if err = (&controllers.ImageSpecReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("ImageSpec"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ImageSpec")
		os.Exit(1)
	}
	if err = (&controllers.ImageSpecJobReconciler{
		Client:           mgr.GetClient(),
		Log:              ctrl.Log.WithName("controllers").WithName("ImageSpecJob"),
		Scheme:           mgr.GetScheme(),
		EphemeralStorage: resource.MustParse(viper.GetString("customImage.buildJob.ephemeralStorage")),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ImageSpecJob")
		os.Exit(1)
	}

	graphqlClient := graphql.NewGraphqlClient(
		viper.GetString("jobSubmission.graphqlEndpoint"),
		viper.GetString("jobSubmission.graphqlSecret"))

	nodeSelector := viper.GetStringMapString("jobSubmission.nodeSelector")

	var tolerationsSlice []corev1.Toleration

	err = viper.UnmarshalKey("jobSubmission.tolerations", &tolerationsSlice)
	if err != nil {
		panic(err.Error() + " cannot UnmarshalKey toleration")
	}

	var affinity corev1.Affinity
	err = viper.UnmarshalKey("jobSubmission.affinity", &affinity)
	if err != nil {
		panic(err.Error() + " cannot UnmarshalKey affinity")
	}

	if err = (&controllers.PhJobReconciler{
		Client:                         mgr.GetClient(),
		Log:                            ctrl.Log.WithName("controllers").WithName("PhJob"),
		Scheme:                         mgr.GetScheme(),
		GraphqlClient:                  graphqlClient,
		WorkingDirSize:                 resource.MustParse(viper.GetString("jobSubmission.workingDirSize")),
		DefaultActiveDeadlineSeconds:   viper.GetInt64("jobSubmission.defaultActiveDeadlineSeconds"),
		DefaultTTLSecondsAfterFinished: viper.GetInt32("jobSubmission.defaultTTLSecondsAfterFinished"),
		NodeSelector:                   nodeSelector,
		Tolerations:                    tolerationsSlice,
		Affinity:                       affinity,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PhJob")
		os.Exit(1)
	}

	phJobScheduler := controllers.PHJobScheduler{
		Client:        mgr.GetClient(),
		Log:           ctrl.Log.WithName("scheduler").WithName("PhJob"),
		GraphqlClient: graphqlClient,
	}
	go wait.Until(phJobScheduler.Schedule, time.Second*1, stopChan)

	licenseReconciler := &eecontrollers.LicenseReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("License"),
		Scheme: mgr.GetScheme(),
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
	if err := mgr.Start(stopChan); err != nil {
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

	viper.SetDefault("customImage.buildJob.ephemeralStorage", "10Gi")
	// Check customImage.buildJob.ephemeralStorage
	if _, err := resource.ParseQuantity(viper.GetString("customImage.buildJob.ephemeralStorage")); err != nil {
		setupLog.Info("cannot parse customImage.buildJob.ephemeralStorage, use default value")
		viper.Set("customImage.buildJob.ephemeralStorage", "10Gi")
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
