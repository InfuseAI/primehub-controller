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
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/wait"
	"os"
	eecontrollers "primehub-controller/ee/controllers"
	phcache "primehub-controller/pkg/cache"
	"primehub-controller/pkg/graphql"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"strings"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	primehubv1alpha1 "primehub-controller/api/v1alpha1"
	"primehub-controller/controllers"
	eeprimehubv1alpha1 "primehub-controller/ee/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	ctrlzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(primehubv1alpha1.AddToScheme(scheme))
	utilruntime.Must(eeprimehubv1alpha1.AddToScheme(scheme))

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
	//stopChan := ctrl.SetupSignalHandler()
	stopChan := make(chan struct{})

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "7cf7afed.io",
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

	if err = (&controllers.ImageReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("Image"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Image")
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
		viper.GetString("graphqlEndpoint"),
		viper.GetString("graphqlSecret"))

	primehubCache := phcache.NewPrimeHubCache(graphqlClient)
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

	phfsEnabled := viper.GetBool("phfsEnabled")
	phfsPVC := viper.GetString("phfsPVC")
	// older settings compatibility
	if len(phfsPVC) <= 0 {
		phfsEnabled = viper.GetBool("jobSubmission.phfsEnabled")
		phfsPVC = viper.GetString("jobSubmission.phfsPVC")
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

	if err = (&eecontrollers.PhJobReconciler{
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
		PhfsEnabled:                    phfsEnabled,
		PhfsPVC:                        phfsPVC,
		ArtifactEnabled:                viper.GetBool("jobSubmission.artifact.enabled"),
		ArtifactLimitSizeMb:            viper.GetInt32("jobSubmission.artifact.limitSizeMb"),
		ArtifactLimitFiles:             viper.GetInt32("jobSubmission.artifact.limitFiles"),
		ArtifactRetentionSeconds:       viper.GetInt32("jobSubmission.artifact.retentionSeconds"),
		GrantSudo:                      viper.GetBool("jobSubmission.grantSudo"),
		MonitoringAgentImageRepository: viper.GetString("monitoringAgent.image.repository"),
		MonitoringAgentImageTag:        viper.GetString("monitoringAgent.image.tag"),
		MonitoringAgentImagePullPolicy: corev1.PullPolicy(viper.GetString("monitoringAgent.image.pullPolicy")),
		PrimeHubCache:                  primehubCache,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PhJob")
		os.Exit(1)
	}

	licenseReconciler := &eecontrollers.LicenseReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("License"),
		Scheme: mgr.GetScheme(),
	}
	if err := licenseReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "License")
		os.Exit(1)
	}

	if err = (&eecontrollers.PhScheduleReconciler{
		Client:            mgr.GetClient(),
		Log:               ctrl.Log.WithName("controllers").WithName("PhSchedule"),
		PhScheduleCronMap: make(map[string]*eecontrollers.PhScheduleCron),
		GraphqlClient:     graphqlClient,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PhSchedule")
		os.Exit(1)
	}

	createDeploymentReconciler(err, mgr, graphqlClient, phfsEnabled, phfsPVC)

	//+kubebuilder:scaffold:builder

	phJobScheduler := eecontrollers.PHJobScheduler{
		Client:        mgr.GetClient(),
		Log:           ctrl.Log.WithName("scheduler").WithName("PhJob"),
		GraphqlClient: graphqlClient,
		PrimeHubCache: primehubCache,
	}
	go wait.Until(phJobScheduler.Schedule, time.Second*1, stopChan)

	phJobCleaner := eecontrollers.PHJobCleaner{
		Client:        mgr.GetClient(),
		Log:           ctrl.Log.WithName("cleaner").WithName("PhJob"),
		JobTTLSeconds: viper.GetInt64("jobSubmission.jobTTLSeconds"),
		JobLimit:      viper.GetInt32("jobSubmission.jobLimit"),
	}
	go wait.Until(phJobCleaner.Clean, time.Second*30, stopChan)

	go func() {
		if err := licenseReconciler.EnsureLicense(mgr); err != nil {
			panic("unable to initialize license reconciler: " + err.Error())
		}
	}()

	setupLog.Info("starting manager")

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
	stopChan <- struct{}{}
}

func createDeploymentReconciler(err error, mgr manager.Manager, graphqlClient graphql.AbstractGraphqlClient, phfsEnabled bool, phfsPVC string) {
	// Create Model Deployment Reconciler
	ingress := eecontrollers.PhIngress{}

	// get the ingress from the config which is from the helm value.
	err = viper.UnmarshalKey("ingress", &ingress)
	if err != nil {
		panic(err.Error() + " cannot UnmarshalKey ingress")
	}
	if ingress.Hosts == nil {
		panic(fmt.Errorf("should provide ingress in config.yaml if enable model deployment"))
	}
	if ingress.Annotations == nil {
		ingress.Annotations = make(map[string]string)
	}

	var engineContainerImage string
	engineContainerRepository := viper.GetString("modelDeployment.engineContainer.image.repository")
	engineContainerTag := viper.GetString("modelDeployment.engineContainer.image.tag")
	engineContainerPullPolicy := corev1.PullPolicy(viper.GetString("modelDeployment.engineContainer.image.pullPolicy"))
	engineContainerImage = fmt.Sprintf("%s:%s", engineContainerRepository, engineContainerTag)

	var modelStorageInitializerImage string
	modelStorageInitializerRepository := viper.GetString("modelDeployment.modelStorageInitializer.image.repository")
	modelStorageInitializerTag := viper.GetString("modelDeployment.modelStorageInitializer.image.tag")
	modelStorageInitializerPullPolicy := corev1.PullPolicy(viper.GetString("modelDeployment.modelStorageInitializer.image.pullPolicy"))
	modelStorageInitializerImage = fmt.Sprintf("%s:%s", modelStorageInitializerRepository, modelStorageInitializerTag)

	var mlflowModelStorageInitializerImage string
	mlflowModelStorageInitializerRepository := viper.GetString("modelDeployment.mlflowModelStorageInitializer.image.repository")
	mlflowModelStorageInitializerTag := viper.GetString("modelDeployment.mlflowModelStorageInitializer.image.tag")
	mlflowModelStorageInitializerPullPolicy := corev1.PullPolicy(viper.GetString("modelDeployment.mlflowModelStorageInitializer.image.pullPolicy"))
	mlflowModelStorageInitializerImage = fmt.Sprintf("%s:%s", mlflowModelStorageInitializerRepository, mlflowModelStorageInitializerTag)

	if err = (&eecontrollers.PhDeploymentReconciler{
		Client:                                  mgr.GetClient(),
		Log:                                     ctrl.Log.WithName("controllers").WithName("PhDeployment"),
		Scheme:                                  mgr.GetScheme(),
		GraphqlClient:                           graphqlClient,
		Ingress:                                 ingress,
		PrimehubUrl:                             viper.GetString("primehubUrl"),
		EngineImage:                             engineContainerImage,
		EngineImagePullPolicy:                   engineContainerPullPolicy,
		ModelStorageInitializerImage:            modelStorageInitializerImage,
		ModelStorageInitializerPullPolicy:       modelStorageInitializerPullPolicy,
		MlflowModelStorageInitializerImage:      mlflowModelStorageInitializerImage,
		MlflowModelStorageInitializerPullPolicy: mlflowModelStorageInitializerPullPolicy,
		PhfsEnabled:                             phfsEnabled,
		PhfsPVC:                                 phfsPVC,
		ImagePrefix:                             viper.GetString("imagePrefix"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PhDeployment")
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
		"jobSubmission.workingDirSize",
		"jobSubmission.defaultActiveDeadlineSeconds",
		"jobSubmission.defaultTTLSecondsAfterFinished",
	}

	modelConfigs := []string{
		"modelDeployment.engineContainer.image.repository",
		"modelDeployment.engineContainer.image.tag",
		"modelDeployment.engineContainer.image.pullPolicy",
		"modelDeployment.modelStorageInitializer.image.repository",
		"modelDeployment.modelStorageInitializer.image.tag",
		"modelDeployment.modelStorageInitializer.image.pullPolicy",
		"modelDeployment.mlflowModelStorageInitializer.image.repository",
		"modelDeployment.mlflowModelStorageInitializer.image.tag",
		"modelDeployment.mlflowModelStorageInitializer.image.pullPolicy",
	}

	if viper.GetBool("modelDeployment.enabled") {
		configs = append(configs, modelConfigs...)
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
