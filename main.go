package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/operator-framework/operator-lib/leader"

	corev1 "k8s.io/api/core/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	aaoconfig "github.com/openshift/aws-account-operator/config"
	"github.com/openshift/aws-account-operator/controllers/account"
	"github.com/openshift/aws-account-operator/controllers/accountclaim"
	"github.com/openshift/aws-account-operator/controllers/accountpool"
	"github.com/openshift/aws-account-operator/controllers/awsfederatedaccountaccess"
	"github.com/openshift/aws-account-operator/controllers/awsfederatedrole"
	"github.com/openshift/aws-account-operator/controllers/validation"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/localmetrics"
	"github.com/openshift/aws-account-operator/pkg/totalaccountwatcher"
	"github.com/openshift/aws-account-operator/pkg/utils"
	"github.com/openshift/aws-account-operator/version"
	"github.com/openshift/operator-custom-metrics/pkg/metrics"
	//+kubebuilder:scaffold:imports
)

// Change below variables to serve metrics on different host or port.
var (
	customMetricsPort string = "8080"
	customMetricsPath string = "/metrics"

	totalWatcherInterval = time.Duration(5) * time.Minute

	scheme   = apiruntime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(awsv1alpha1.AddToScheme(scheme))
	utilruntime.Must(routev1.Install(scheme))
	//+kubebuilder:scaffold:scheme
}

func printVersion() {
	setupLog.Info(fmt.Sprintf("Operator Version: %s", version.Version))
	setupLog.Info(fmt.Sprintf("Operator-sdk Version: %v", version.SDKVersion))
	setupLog.Info(fmt.Sprintf("Go Version: %s", runtime.Version()))
	setupLog.Info(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8081", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":9081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	opts := zap.Options{
		Development: false,
	}
	if utils.DetectDevMode == utils.DevModeLocal {
		zap.UseDevMode(true)
	}

	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	printVersion()

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "c0d5a6d1.managed.openshift.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Become the leader before proceeding
	// This doesn't work locally, so only perform it when running on-cluster
	if utils.DetectDevMode != utils.DevModeLocal {
		err = leader.Become(context.TODO(), "aws-account-operator-lock")
		if err != nil {
			setupLog.Error(err, "Unable to become leader")
			os.Exit(1)
		}
	} else {
		setupLog.Info("bypassing leader election due to local execution")
	}

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		setupLog.Error(err, "Failed to get config to talk to the apiserver")
		os.Exit(1)
	}

	// Define a kubeClient for any processes that need to run during operator startup or independent routines to use
	// We should avoid using this kubeClient except for when necessary and utilize the operator-sdk provided client as much as possible.
	// The operator-sdk kube client provides a level of caching that we don't get with building our own this way.
	kubeClient, err := client.New(cfg, client.Options{})
	if err != nil {
		setupLog.Error(err, "Failed to create a kubernetes client")
		os.Exit(1)
	}

	errors := utils.InitControllerMaxReconciles(kubeClient)
	if len(errors) > 0 {
		setupLog.Info("There was at least one error initializing controller max reconcile values.")
		for _, err := range errors {
			setupLog.Error(err, "")
		}
	}

	if err = (&accountclaim.AccountClaimReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AccountClaim")
		os.Exit(1)
	}
	if err = (&awsfederatedrole.AWSFederatedRoleReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AWSFederatedRole")
		os.Exit(1)
	}
	if err = (&awsfederatedaccountaccess.AWSFederatedAccountAccessReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AWSFederatedAccountAccess")
		os.Exit(1)
	}
	if err = (&accountpool.AccountPoolReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AccountPool")
		os.Exit(1)
	}
	if err = (&account.AccountReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Account")
		os.Exit(1)
	}
	if err = (&validation.AccountValidationReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AccountValidation")
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

	// initialize metrics collector
	localmetrics.Collector = localmetrics.NewMetricsCollector(mgr.GetCache())
	switch utils.DetectDevMode {
	case utils.DevModeLocal:
		if err := prometheus.Register(localmetrics.Collector); err != nil {
			setupLog.Error(err, "Failed to register Prometheus metrics")
			os.Exit(1)
		}
		http.Handle(customMetricsPath, promhttp.Handler())
		go func() {
			if err := http.ListenAndServe(":"+customMetricsPort, nil); err != nil {
				setupLog.Error(err, "Failed to start metrics handler")
				os.Exit(1)
			}
		}()
	default:
		//Create metrics endpoint and register metrics
		metricsServer := metrics.NewBuilder("aws-account-operator", "aws-account-operator").WithPort(customMetricsPort).WithPath(customMetricsPath).
			WithCollector(localmetrics.Collector).
			WithRoute().
			GetConfig()

		// Configure metrics if it errors log the error but continue
		if err := metrics.ConfigureMetrics(context.TODO(), *metricsServer); err != nil {
			setupLog.Error(err, "Failed to configure Metrics")
			os.Exit(1)
		}
	}

	// Define stopCh which we'll use to notify the accountWatcher (any any other routine)
	// to stop work. This channel can also be used to signal routines to complete any cleanup
	// work
	stopCh := signals.SetupSignalHandler()

	// Initialize our ConfigMap with default values if necessary.
	initOperatorConfigMapVars(kubeClient)

	// Initialize the TotalAccountWatcher
	go totalaccountwatcher.TotalAccountWatcher.Start(setupLog, stopCh, kubeClient, totalWatcherInterval)

	setupLog.Info("starting manager")
	if err := mgr.Start(stopCh); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func initOperatorConfigMapVars(kubeClient client.Client) {
	// Check if config map exists.
	cm := &corev1.ConfigMap{}
	err := kubeClient.Get(context.TODO(), types.NamespacedName{Namespace: awsv1alpha1.AccountCrNamespace, Name: awsv1alpha1.DefaultConfigMap}, cm)
	if err != nil {
		setupLog.Error(err, "There was an error getting the default configmap.")
		return
	}

	// SetIsFedramp determines if operator is running in fedramp mode.
	err = aaoconfig.SetIsFedramp(cm)
	if err != nil {
		setupLog.Error(err, "Failed to set fedramp runtime status")
		os.Exit(1)
	}

	// Check if fedramp env
	if aaoconfig.IsFedramp() {
		setupLog.Info("Running in fedramp env")
	}

	awsRegion := aaoconfig.GetDefaultRegion()

	// Get aws client
	builder := &awsclient.Builder{}
	awsClient, err := builder.GetClient("", kubeClient, awsclient.NewAwsClientInput{
		SecretName: utils.AwsSecretName,
		NameSpace:  awsv1alpha1.AccountCrNamespace,
		AwsRegion:  awsRegion,
	})

	if err != nil {
		setupLog.Error(err, "Failed creating AWS client")
		return
	}

	// Get the SRE Admin Access role for CCS Accounts and populate the role name into the configmap
	role, err := awsClient.GetRole(&iam.GetRoleInput{
		RoleName: aws.String(awsv1alpha1.SREAccessRoleName),
	})
	if err != nil {
		setupLog.Error(err, "There was an error getting the SRE CCS Access Role")
		return
	}
	cm.Data["CCS-Access-Arn"] = *role.Role.Arn

	// Apply the changes to the ConfigMap
	err = kubeClient.Update(context.TODO(), cm)
	if err != nil {
		setupLog.Error(err, "There was an error updating the configmap.")
		return
	}
}
