/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"fmt"
	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"os"
	phcache "primehub-controller/pkg/cache"
	"primehub-controller/pkg/graphql"
	ctrlzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	"strings"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	batchv1 "k8s.io/api/batch/v1"
	primehubv1alpha1 "primehub-controller/api/v1alpha1"
	"primehub-controller/controllers"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(primehubv1alpha1.AddToScheme(scheme))
	_ = corev1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var debug bool

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
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

	opts := ctrlzap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)

	loadConfig()

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "8af5f5b6.io",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})

	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	graphqlClient := graphql.NewGraphqlClient(
		viper.GetString("graphqlEndpoint"),
		viper.GetString("graphqlSecret"))

	primehubCache := phcache.NewPrimeHubCache(graphqlClient)

	phfsEnabled := viper.GetBool("phfsEnabled")
	phfsPVC := viper.GetString("phfsPVC")

	if err = (&controllers.ImageReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("Image"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Image")
		os.Exit(1)
	}

	if err = (&controllers.PhApplicationReconciler{
		Client:        mgr.GetClient(),
		Log:           ctrl.Log.WithName("controllers").WithName("PhApplication"),
		Scheme:        mgr.GetScheme(),
		PrimeHubCache: primehubCache,
		PhfsEnabled:   phfsEnabled,
		PhfsPVC:       phfsPVC,
		PrimeHubURL:   viper.GetString("primehubUrl"),
		ImagePrefix:   viper.GetString("imagePrefix"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PhApplication")
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

	//+kubebuilder:scaffold:builder
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func loadConfig() {
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("/etc/primehub-controller/") // path to look for the config file in
	viper.AddConfigPath(".")                         // optionally look for config in the working directory
	err := viper.ReadInConfig()                      // Find and read the config file
	if err != nil {                                  // Handle errors reading the config file
		panic(fmt.Errorf("Fatal error config file: %s \n", err))
	}

	configs := []string{
		"primehubUrl",
		"graphqlEndpoint",
		"graphqlSecret",
		"customImage.pushSecretName",
	}
	for _, config := range configs {
		if viper.GetString(config) == "" {
			panic(config + " is required in config.yaml")
		}
	}

	customImageDefaultEphemeralStorage := "30Gi"
	viper.SetDefault("customImage.buildJob.ephemeralStorage", customImageDefaultEphemeralStorage)
	// Check customImage.buildJob.ephemeralStorage
	if _, err := resource.ParseQuantity(viper.GetString("customImage.buildJob.ephemeralStorage")); err != nil {
		setupLog.Info("cannot parse customImage.buildJob.ephemeralStorage, use default value")
		viper.Set("customImage.buildJob.ephemeralStorage", customImageDefaultEphemeralStorage)
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
