package utils

import (
	"net/http"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/openshift/aws-account-operator/pkg/localmetrics"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

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
		localmetrics.Collector.AddAPICall(cmt.Controller, req, resp, time.Since(start).Seconds())
	}

	return resp, err
}
