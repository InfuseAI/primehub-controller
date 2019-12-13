/*

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
	"os"
	"primehub-controller/pkg/graphql"
	"time"

	primehubv1alpha1 "primehub-controller/api/v1alpha1"

	"primehub-controller/controllers"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

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
	_ = corev1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)
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
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("ImageSpecJob"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ImageSpecJob")
		os.Exit(1)
	}

	graphqlClient := graphql.NewGraphqlClient(
		viper.GetString("jobSubmission.graphqlEndpoint"),
		viper.GetString("jobSubmission.graphqlSecret"))
	if err = (&controllers.PhJobReconciler{
		Client:                         mgr.GetClient(),
		Log:                            ctrl.Log.WithName("controllers").WithName("PhJob"),
		Scheme:                         mgr.GetScheme(),
		GraphqlClient:                  graphqlClient,
		WorkingDirSize:                 resource.MustParse(viper.GetString("jobSubmission.workingDirSize")),
		DefaultActiveDeadlineSeconds:   viper.GetInt64("jobSubmission.defaultActiveDeadlineSeconds"),
		DefaultTTLSecondsAfterFinished: viper.GetInt32("jobSubmission.defaultTTLSecondsAfterFinished"),
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

	// +kubebuilder:scaffold:builder

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
}
