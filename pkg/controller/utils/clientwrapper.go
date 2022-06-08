package utils

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/localmetrics"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var controllerMaxReconciles map[string]int = map[string]int{}

func NewControllerWithMaxReconciles(log logr.Logger, controllerName string, mgr manager.Manager, r reconcile.Reconciler) (controller.Controller, error) {
	maxConcurrentReconciles, err := getControllerMaxReconciles(controllerName)
	if err != nil {
		fmt.Printf("%+v", controllerMaxReconciles)
		log.Error(err, "")
	}
	return controller.New(fmt.Sprintf("%s-controller", controllerName), mgr, controller.Options{Reconciler: r, MaxConcurrentReconciles: maxConcurrentReconciles})
}

func InitControllerMaxReconciles(kubeClient client.Client) []error {
	controllers := []string{
		"account",
		"accountclaim",
		"accountpool",
		"accountvalidation",
		"awsfederatedaccountaccess",
		"awsfederatedrole",
	}
	controllerErrors := []error{}
	cm, err := GetOperatorConfigMap(kubeClient)
	if err != nil {
		controllerErrors = append(controllerErrors, err)
		return controllerErrors
	}

	for _, controller := range controllers {
		val, err := getControllerMaxReconcilesFromCM(cm, controller)
		if err != nil {
			controllerErrors = append(controllerErrors, fmt.Errorf("Error getting Max Reconciles for %s controller", controller))
			continue
		}
		controllerMaxReconciles[controller] = val
	}

	return controllerErrors
}

// getControllerMaxReconcilesFromCM gets the max reconciles for a given controller out of the config map
func getControllerMaxReconcilesFromCM(cm *corev1.ConfigMap, controllerName string) (int, error) {
	cmKey := fmt.Sprintf("MaxConcurrentReconciles.%s", controllerName)
	if val, ok := cm.Data[cmKey]; ok {
		return strconv.Atoi(val)
	}
	return 0, awsv1alpha1.ErrInvalidConfigMap
}

// getControllerMaxReconciles gets the default configMap and then gets the amount of concurrent reconciles to run from it
func getControllerMaxReconciles(controllerName string) (int, error) {
	if _, ok := controllerMaxReconciles[controllerName]; !ok {
		return 1, fmt.Errorf("Controller %s not present in config data", controllerName)
	}
	return controllerMaxReconciles[controllerName], nil
}

// NewClientWithMetricsOrDie creates a new controller-runtime client with a wrapper which increments
// metrics for requests by controller name, HTTP method, URL path, and HTTP status. The client will
// re-use the manager's cache. This should be used in all controllers.
func NewClientWithMetricsOrDie(log logr.Logger, mgr manager.Manager, controller string) client.Client {
	// Copy the rest.Config as we want our round trippers to be controller-specific.
	cfg := rest.CopyConfig(mgr.GetConfig())
	AddControllerMetricsTransportWrapper(cfg, controller)

	options := client.Options{
		Scheme: mgr.GetScheme(),
		Mapper: mgr.GetRESTMapper(),
	}
	c, err := client.New(cfg, options)
	if err != nil {
		log.Error(err, "Unable to initialize metrics-wrapped client")
		os.Exit(1)
	}

	return &client.DelegatingClient{
		Reader: &client.DelegatingReader{
			CacheReader:  mgr.GetCache(),
			ClientReader: c,
		},
		Writer:       c,
		StatusClient: c,
	}
}

// AddControllerMetricsTransportWrapper adds a transport wrapper to the given rest config which
// exposes metrics based on the requests being made.
func AddControllerMetricsTransportWrapper(cfg *rest.Config, controllerName string) {
	// If the restConfig already has a transport wrapper, wrap it.
	if cfg.WrapTransport != nil {
		origFunc := cfg.WrapTransport
		cfg.WrapTransport = func(rt http.RoundTripper) http.RoundTripper {
			return &ControllerMetricsTripper{
				RoundTripper: origFunc(rt),
				Controller:   controllerName,
			}
		}
	}

	cfg.WrapTransport = func(rt http.RoundTripper) http.RoundTripper {
		return &ControllerMetricsTripper{
			RoundTripper: rt,
			Controller:   controllerName,
		}
	}
}

// ControllerMetricsTripper is a RoundTripper implementation which tracks our metrics for client requests.
type ControllerMetricsTripper struct {
	http.RoundTripper
	Controller string
}

// RoundTrip implements the http RoundTripper interface. We simply call the wrapped RoundTripper
// and register the call with our apiCallCount metric.
func (cmt *ControllerMetricsTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	// Call the nested RoundTripper.
	resp, err := cmt.RoundTripper.RoundTrip(req)

	// Count this call, if it worked (where "worked" includes HTTP errors).
	if err == nil {
		localmetrics.Collector.AddAPICall(cmt.Controller, req, resp, time.Since(start).Seconds(), nil)
	}

	return resp, err
}
