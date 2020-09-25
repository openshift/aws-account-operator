package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/openshift/aws-account-operator/pkg/apis"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"

	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/controller"
	"github.com/openshift/aws-account-operator/pkg/controller/utils"
	"github.com/openshift/aws-account-operator/pkg/localmetrics"
	"github.com/openshift/aws-account-operator/pkg/totalaccountwatcher"
	"github.com/openshift/aws-account-operator/version"
	"github.com/openshift/operator-custom-metrics/pkg/metrics"
	"github.com/operator-framework/operator-sdk/pkg/leader"
	"github.com/operator-framework/operator-sdk/pkg/log/zap"
	sdkVersion "github.com/operator-framework/operator-sdk/version"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/pflag"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

// Change below variables to serve metrics on different host or port.
var (
	metricsHost string = "0.0.0.0"
	metricsPort int32  = 8081

	customMetricsPort string = "8080"
	customMetricsPath string = "/metrics"

	totalWatcherInterval = time.Duration(5) * time.Minute
)

var log = logf.Log.WithName("cmd")

func printVersion() {
	log.Info(fmt.Sprintf("Operator Version: %s", version.Version))
	log.Info(fmt.Sprintf("Operator-sdk Version: %v", sdkVersion.Version))
	log.Info(fmt.Sprintf("Go Version: %s", runtime.Version()))
	log.Info(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))
}

func main() {
	// Add the zap logger flag set to the CLI. The flag set must
	// be added before calling pflag.Parse().
	pflag.CommandLine.AddFlagSet(zap.FlagSet())

	// Add flags registered by imported packages (e.g. glog and
	// controller-runtime)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)

	pflag.Parse()

	// Use a zap logr.Logger implementation. If none of the zap
	// flags are configured (or if the zap flag set is not being
	// used), this defaults to a production zap logger.
	//
	// The logger instantiated here can be changed to any logger
	// implementing the logr.Logger interface. This logger will
	// be propagated through the whole operator, generating
	// uniform and structured logs.
	logf.SetLogger(zap.Logger())

	printVersion()

	// This is used by controllers to detect whether conditions happened during the current
	// invocation of the operator or a previous one. Thus it *must* be done before controllers
	// are started.
	// It must also be done exactly once -- see the docstring.
	if err := utils.InitOperatorStartTime(); err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	ctx := context.TODO()

	// Become the leader before proceeding
	err = leader.Become(ctx, "aws-account-operator-lock")
	if err != nil {
		os.Exit(1)
	}

	// Create a new Cmd to provide shared dependencies and start components
	mgr, err := manager.New(cfg, manager.Options{
		Namespace:          "",
		MetricsBindAddress: fmt.Sprintf("%s:%d", metricsHost, metricsPort),
	})
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	log.Info("Registering Components.")

	// Setup Scheme for all resources
	if err := apis.AddToScheme(mgr.GetScheme()); err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	// Add Prometheus schemes to manager
	if err := routev1.AddToScheme(mgr.GetScheme()); err != nil {
		log.Error(err, "")
		os.Exit(1)
	}
	// Setup all Controllers
	if err := controller.AddToManager(mgr); err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	// initialize metrics collector
	localmetrics.Collector = localmetrics.NewMetricsCollector(mgr.GetCache())
	switch utils.DetectDevMode {
	case utils.DevModeLocal:
		if err := prometheus.Register(localmetrics.Collector); err != nil {
			log.Error(err, "failed to register Prometheus metrics")
			os.Exit(1)
		}
		http.Handle(customMetricsPath, promhttp.Handler())
		go func() {
			if err := http.ListenAndServe(":"+customMetricsPort, nil); err != nil {
				log.Error(err, "failed to start metrics handler")
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
			log.Error(err, "Failed to configure Metrics")
			os.Exit(1)
		}
	}

	// Define stopCh which we'll use to notify the accountWatcher (any any other routine)
	// to stop work. This channel can also be used to signal routines to complete any cleanup
	// work
	stopCh := signals.SetupSignalHandler()

	// Define an awsClient for any processes that need to run during operator startup or independent routines to use
	awsClient, err := client.New(cfg, client.Options{})
	if err != nil {
		log.Error(err, "")
	}

	// Initialize our ConfigMap with default values if necessary.
	initOperatorConfigMapVars(awsClient)

	// Initialize the TotalAccountWatcher
	go totalaccountwatcher.TotalAccountWatcher.Start(log, stopCh, awsClient, totalWatcherInterval)

	log.Info("Starting the Cmd.")

	// Start the Cmd
	if err := mgr.Start(stopCh); err != nil {
		log.Error(err, "Manager exited non-zero")
		os.Exit(1)
	}
}

func initOperatorConfigMapVars(kubeClient client.Client) {
	builder := &awsclient.Builder{}
	awsClient, err := builder.GetClient("", kubeClient, awsclient.NewAwsClientInput{
		SecretName: utils.AwsSecretName,
		NameSpace:  awsv1alpha1.AccountCrNamespace,
		AwsRegion:  "us-east-1",
	})

	if err != nil {
		log.Error(err, "failed creating AWS client")
		return
	}

	// Check if config map exists.
	cm := &corev1.ConfigMap{}
	err = kubeClient.Get(context.TODO(), types.NamespacedName{Namespace: awsv1alpha1.AccountCrNamespace, Name: awsv1alpha1.DefaultConfigMap}, cm)
	if err != nil {
		log.Error(err, "There was an error getting the default configmap.")
		return
	}

	// Get the SRE Admin Access role for CCS Accounts and populate the role name into the configmap
	role, err := awsClient.GetRole(&iam.GetRoleInput{
		RoleName: aws.String(awsv1alpha1.SREAccessRoleName),
	})
	if err != nil {
		log.Error(err, "There was an error getting the SRE CCS Access Role")
		return
	}
	cm.Data["CCS-Access-Arn"] = *role.Role.Arn

	// Apply the changes to the ConfigMap
	err = kubeClient.Update(context.TODO(), cm)
	if err != nil {
		log.Error(err, "There was an error updating the configmap.")
		return
	}
}
