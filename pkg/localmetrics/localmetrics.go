// Copyright 2019 RedHat
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package localmetrics

import (
	"context"
	"net/http"
	neturl "net/url"
	"strings"

	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	operatorName = "aws-account-operator"
)

var (
	log = logf.Log.WithName("metrics-collector")
	// Collector is a pointer to the MetricsCollector struct describing a Prometheus collector
	Collector *MetricsCollector
)

// MetricsCollector is a struct describing a Prometheus collector
type MetricsCollector struct {
	store                           cache.Cache
	awsAccounts                     prometheus.Gauge
	accounts                        *prometheus.GaugeVec
	ccsAccounts                     *prometheus.GaugeVec
	accountClaims                   *prometheus.GaugeVec
	accountReuseAvailable           *prometheus.GaugeVec
	accountPoolSize                 *prometheus.GaugeVec
	accountReadyDuration            prometheus.Histogram
	ccsAccountReadyDuration         prometheus.Histogram
	accountClaimReadyDuration       prometheus.Histogram
	ccsAccountClaimReadyDuration    prometheus.Histogram
	accountReuseCleanupDuration     prometheus.Histogram
	accountReuseCleanupFailureCount prometheus.Counter
	reconcileDuration               *prometheus.HistogramVec
	apiCallDuration                 *prometheus.HistogramVec
}

// NewMetricsCollector creates a new instance of a Prometheus metrics collector
func NewMetricsCollector(store cache.Cache) *MetricsCollector {
	return &MetricsCollector{
		store: store,
		awsAccounts: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        "aws_account_operator_aws_accounts",
			Help:        "Report how many accounts have been created in AWS org",
			ConstLabels: prometheus.Labels{"name": operatorName},
		}),
		accounts: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        "aws_account_operator_account_crs",
			Help:        "Report how many account crs in the cluster",
			ConstLabels: prometheus.Labels{"name": operatorName},
		}, []string{"claimed", "reused", "state"}),
		ccsAccounts: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        "aws_account_operator_account_ccs_crs",
			Help:        "Report how many ccs account crs in the cluster",
			ConstLabels: prometheus.Labels{"name": operatorName},
		}, []string{"claimed", "reused", "state"}),
		accountClaims: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        "aws_account_operator_account_claim_crs",
			Help:        "Report how many account claim crs in the cluster",
			ConstLabels: prometheus.Labels{"name": operatorName},
		}, []string{"state"}),
		accountReuseAvailable: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        "aws_account_operator_aws_accounts_reusable",
			Help:        "Report the number of reused accounts available for claiming grouped by legal ID",
			ConstLabels: prometheus.Labels{"name": operatorName},
		}, []string{"legal_id"}),

		// pool_name is not a good label because it may cause
		// high cardinality. But in our use case it is okay
		// since we only have one account pool in the cluster.
		accountPoolSize: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        "aws_account_operator_account_pool_size",
			Help:        "Report the size of account pool cr",
			ConstLabels: prometheus.Labels{"name": operatorName},
		}, []string{"namespace", "pool_name"}),

		accountReadyDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:        "aws_account_operator_account_ready_duration_seconds",
			Help:        "The duration for account cr to get ready",
			ConstLabels: prometheus.Labels{"name": operatorName},
			// representing in minutes [1 3 5 10 20 30 60 120 240 300 480 600]
			Buckets: []float64{60, 180, 300, 600, 1200, 1800, 3600, 7200, 14400, 18000, 28800, 36000},
		}),
		ccsAccountReadyDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:        "aws_account_operator_account_ccs_ready_duration_seconds",
			Help:        "The duration for ccs account cr to get ready",
			ConstLabels: prometheus.Labels{"name": operatorName},
			Buckets:     []float64{5, 10, 20, 30, 60, 120, 240, 300, 480, 600},
		}),

		accountClaimReadyDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:        "aws_account_operator_account_claim_ready_duration_seconds",
			Help:        "The duration for account claim cr to get claimed",
			ConstLabels: prometheus.Labels{"name": operatorName},
			Buckets:     []float64{1, 5, 10, 20, 30, 45, 60, 120},
		}),
		ccsAccountClaimReadyDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:        "aws_account_operator_account_claim_ccs_ready_duration_seconds",
			Help:        "The duration for ccs account claim cr to get claimed",
			ConstLabels: prometheus.Labels{"name": operatorName},
			Buckets:     []float64{5, 10, 20, 30, 60, 120, 240, 300, 480, 600},
		}),

		accountReuseCleanupDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:        "aws_account_operator_account_reuse_cleanup_duration_seconds",
			Help:        "The duration for account reuse cleanup",
			ConstLabels: prometheus.Labels{"name": operatorName},
			Buckets:     []float64{1, 3, 5, 10, 15, 20, 30},
		}),

		accountReuseCleanupFailureCount: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "aws_account_operator_account_reuse_cleanup_failures_total",
			Help:        "Number of account reuse cleanup failures",
			ConstLabels: prometheus.Labels{"name": operatorName},
		}),
		reconcileDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        "aws_account_operator_reconcile_duration_seconds",
			Help:        "Distribution of the number of seconds a Reconcile takes, broken down by controller",
			ConstLabels: prometheus.Labels{"name": operatorName},
			Buckets:     []float64{0.001, 0.01, 0.1, 1, 5, 10, 20},
		}, []string{"controller", "error", "error_source"}),

		// apiCallDuration times API requests. Histogram also gives us a _count metric for free.
		apiCallDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        "aws_account_operator_api_request_duration_seconds",
			Help:        "Distribution of the number of seconds an API request takes",
			ConstLabels: prometheus.Labels{"name": operatorName},
			// We really don't care about quantiles, but omitting Buckets results in defaults.
			// This minimizes the number of unused data points we store.
			Buckets: []float64{1},
		}, []string{"controller", "method", "resource", "status", "error", "error_source"}),
	}
}

// Describe implements the prometheus.Collector interface.
func (c *MetricsCollector) Describe(ch chan<- *prometheus.Desc) {
	c.awsAccounts.Describe(ch)
	c.accounts.Describe(ch)
	c.ccsAccounts.Describe(ch)
	c.accountClaims.Describe(ch)
	c.accountPoolSize.Describe(ch)
	c.accountReuseAvailable.Describe(ch)
	c.accountReadyDuration.Describe(ch)
	c.ccsAccountReadyDuration.Describe(ch)
	c.accountClaimReadyDuration.Describe(ch)
	c.ccsAccountClaimReadyDuration.Describe(ch)
	c.accountReuseCleanupDuration.Describe(ch)
	c.accountReuseCleanupFailureCount.Describe(ch)
	c.reconcileDuration.Describe(ch)
	c.apiCallDuration.Describe(ch)
}

// Collect implements the prometheus.Collector interface.
func (c *MetricsCollector) Collect(ch chan<- prometheus.Metric) {
	c.collect()
	c.awsAccounts.Collect(ch)
	c.accounts.Collect(ch)
	c.ccsAccounts.Collect(ch)
	c.accountClaims.Collect(ch)
	c.accountPoolSize.Collect(ch)
	c.accountReuseAvailable.Collect(ch)
	c.accountReadyDuration.Collect(ch)
	c.ccsAccountReadyDuration.Collect(ch)
	c.accountClaimReadyDuration.Collect(ch)
	c.ccsAccountClaimReadyDuration.Collect(ch)
	c.accountReuseCleanupDuration.Collect(ch)
	c.accountReuseCleanupFailureCount.Collect(ch)
	c.reconcileDuration.Collect(ch)
	c.apiCallDuration.Collect(ch)
}

// collect will cleanup the gauge metrics first, then getting all the
// CRs in the cluster and update metrics
func (c *MetricsCollector) collect() {
	c.accounts.Reset()
	c.ccsAccounts.Reset()
	c.accountClaims.Reset()
	c.accountPoolSize.Reset()
	c.accountReuseAvailable.Reset()

	ctx := context.TODO()
	var (
		accounts      awsv1alpha1.AccountList
		accountClaims awsv1alpha1.AccountClaimList
		accountPool   awsv1alpha1.AccountPoolList
		claimed       string
		reused        string
	)
	if err := c.store.List(ctx, &accounts, []client.ListOption{
		client.InNamespace(awsv1alpha1.AccountCrNamespace)}...); err != nil {
		log.Error(err, "failed to list accounts")
		return
	}

	if err := c.store.List(ctx, &accountClaims, []client.ListOption{}...); err != nil {
		log.Error(err, "failed to list account claims")
		return
	}

	if err := c.store.List(ctx, &accountPool, []client.ListOption{}...); err != nil {
		log.Error(err, "failed to list account pools")
		return
	}

	for _, account := range accounts.Items {
		if account.Spec.Claimed {
			claimed = "true"
		} else {
			claimed = "false"
		}

		if account.Spec.Reused {
			reused = "true"
		} else {
			reused = "false"
		}

		if !account.Spec.Claimed && account.Spec.Reused &&
			account.Spec.State == "Ready" {
			c.accountReuseAvailable.WithLabelValues(account.Spec.LegalEntity.ID).Inc()
		}

		if account.Spec.BYOC {
			c.ccsAccounts.WithLabelValues(claimed, reused, account.Spec.State).Inc()
		} else {
			c.accounts.WithLabelValues(claimed, reused, account.Spec.State).Inc()
		}
	}

	for _, accountClaim := range accountClaims.Items {
		c.accountClaims.WithLabelValues(string(accountClaim.Status.State)).Inc()
	}

	for _, pool := range accountPool.Items {
		c.accountPoolSize.WithLabelValues(pool.Namespace, pool.Name).Set(float64(pool.Spec.PoolSize))
	}
}

// SetTotalAWSAccounts sets the metric watching the total number of AWS accounts known by the operator
func (c *MetricsCollector) SetTotalAWSAccounts(total int) {
	c.awsAccounts.Set(float64(total))
}

// SetAccountReadyDuration sets the metric describing the time it takes for an account to go into the Ready state
func (c *MetricsCollector) SetAccountReadyDuration(ccs bool, duration float64) {
	if ccs {
		c.ccsAccountReadyDuration.Observe(duration)
	} else {
		c.accountReadyDuration.Observe(duration)
	}
}

// SetAccountClaimReadyDuration sets the metric describing the time it takes for an accountClaim to go into the Ready state
func (c *MetricsCollector) SetAccountClaimReadyDuration(ccs bool, duration float64) {
	if ccs {
		c.ccsAccountClaimReadyDuration.Observe(duration)
	} else {
		c.accountClaimReadyDuration.Observe(duration)
	}
}

// SetAccountReusedCleanupDuration sets the metric describing the time it takes for an account to complete the reuse process
func (c *MetricsCollector) SetAccountReusedCleanupDuration(duration float64) {
	c.accountReuseCleanupDuration.Observe(duration)
}

// AddAccountReuseCleanupFailure describes the number of accounts that have failed reuse
func (c *MetricsCollector) AddAccountReuseCleanupFailure() {
	c.accountReuseCleanupFailureCount.Inc()
}

type ReportedError struct {
	Source string
	Code   string
}

func (e *ReportedError) Parse(err error) {
	if err == nil {
		return
	}

	// attempt to see if it's an AWS Error
	if aerr, ok := err.(awserr.Error); ok {
		e.Code = aerr.Code()
		e.Source = "aws"
		return
	}

	// TODO: See if it's a k8s error

	// default with an error is {OTHER}
	e.Code = "{OTHER}"
	e.Source = "{OTHER}"
}

// SetReconcileDuration describes the time it takes for the operator to complete a single reconcile loop
func (c *MetricsCollector) SetReconcileDuration(controller string, duration float64, err error) {
	e := &ReportedError{}
	e.Parse(err)
	c.reconcileDuration.WithLabelValues(controller, e.Code, e.Source).Observe(duration)
}

// AddAPICall observes metrics for a call to an external API
// - param controller: The name of the controller making the API call
// - param req: The HTTP Request structure
// - param resp: The HTTP Response structure
// - param duration: The number of seconds the call took.
func (c *MetricsCollector) AddAPICall(controller string, req *http.Request, resp *http.Response, duration float64, err error) {
	var status string
	if resp == nil {
		status = "{ERROR}"
	} else {
		status = resp.Status
	}

	e := &ReportedError{}
	e.Parse(err)

	c.apiCallDuration.With(prometheus.Labels{
		"controller":   controller,
		"method":       req.Method,
		"resource":     resourceFrom(req.URL),
		"status":       status,
		"error":        e.Code,
		"error_source": e.Source,
	}).Observe(duration)
}

// resourceFrom normalizes an API Request.
// If the Request is to an AWS service, we just return the Host, which indicates which service.
// Otherwise, we assume the request is for a kube resource, and we remove individual namespace and
// resource names, to yield a string of the form:
//     $group/$version/$kind[/{NAME}[/...]]
// or
//     $group/$version/namespaces/{NAMESPACE}/$kind[/{NAME}[/...]]
// ...where $foo is variable, {FOO} is actually {FOO}, and [foo] is optional.
// This is so we can use it as a dimension for the apiCallCount metric, without ending up
// with separate labels for each {namespace x name}.
func resourceFrom(url *neturl.URL) (resource string) {
	defer func() {
		// If we can't parse, return a general bucket. This includes paths that don't start with
		// /api or /apis.
		if r := recover(); r != nil {
			// TODO(efried): Should we be logging these? I guess if we start to see a lot of them...
			resource = "{OTHER}"
		}
	}()

	// Special case for AWS URLs: just record the host, which tells us which service we hit
	if strings.Contains(url.Host, ".amazonaws.") {
		resource = url.Host
		return
	}

	// Otherwise we're dealing with a kube resource. Normalize the URL.
	tokens := strings.Split(url.Path[1:], "/")

	// First normalize to $group/$version/...
	switch tokens[0] {
	case "api":
		// Core resources: /api/$version/...
		// => core/$version/...
		tokens[0] = "core"
	case "apis":
		// Extensions: /apis/$group/$version/...
		// => $group/$version/...
		tokens = tokens[1:]
	default:
		// Something else. Punt.
		panic(1)
	}

	// Single resource, non-namespaced (including a namespace itself): $group/$version/$kind/$name
	if len(tokens) == 4 {
		// Factor out the resource name
		tokens[3] = "{NAME}"
	}

	// Kind or single resource, namespaced: $group/$version/namespaces/$nsname/$kind[/$name[/...]]
	if len(tokens) > 4 && tokens[2] == "namespaces" {
		// Factor out the namespace name
		tokens[3] = "{NAMESPACE}"

		// Single resource, namespaced: $group/$version/namespaces/$nsname/$kind/$name[/...]
		if len(tokens) > 5 {
			// Factor out the resource name
			tokens[5] = "{NAME}"
		}
	}

	resource = strings.Join(tokens, "/")

	return
}
