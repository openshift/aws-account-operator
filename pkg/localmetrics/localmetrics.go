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
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

const (
	operatorName = "aws-account-operator"
)

var (
	log       = logf.Log.WithName("metrics-collector")
	Collector *MetricsCollector
)

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
}

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
			Buckets:     []float64{1, 5, 10, 20, 30, 45, 60},
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
		}, []string{"controller"}),
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
	if err := c.store.List(ctx, &client.ListOptions{
		Namespace: awsv1alpha1.AccountCrNamespace}, &accounts); err != nil {
		log.Error(err, "failed to list accounts")
		return
	}

	if err := c.store.List(ctx, &client.ListOptions{}, &accountClaims); err != nil {
		log.Error(err, "failed to list account claims")
		return
	}

	if err := c.store.List(ctx, &client.ListOptions{}, &accountPool); err != nil {
		log.Error(err, "failed to list account pools")
		return
	}

	for _, account := range accounts.Items {
		if account.Status.Claimed {
			claimed = "true"
		} else {
			claimed = "false"
		}

		if account.Status.Reused {
			reused = "true"
		} else {
			reused = "false"
		}

		if account.Status.Claimed == false && account.Status.Reused == true &&
			account.Status.State == "Ready" {
			c.accountReuseAvailable.WithLabelValues(account.Spec.LegalEntity.ID).Inc()
		}

		if account.Spec.BYOC {
			c.ccsAccounts.WithLabelValues(claimed, reused, account.Status.State).Inc()
		} else {
			c.accounts.WithLabelValues(claimed, reused, account.Status.State).Inc()
		}
	}

	for _, accountClaim := range accountClaims.Items {
		c.accountClaims.WithLabelValues(string(accountClaim.Status.State)).Inc()
	}

	for _, pool := range accountPool.Items {
		c.accountPoolSize.WithLabelValues(pool.Namespace, pool.Name).Set(float64(pool.Spec.PoolSize))
	}
}

func (c *MetricsCollector) SetTotalAWSAccounts(total int) {
	c.awsAccounts.Set(float64(total))
}

func (c *MetricsCollector) SetAccountReadyDuration(ccs bool, duration float64) {
	if ccs {
		c.ccsAccountReadyDuration.Observe(duration)
	} else {
		c.accountReadyDuration.Observe(duration)
	}
}

func (c *MetricsCollector) SetAccountClaimReadyDuration(ccs bool, duration float64) {
	if ccs {
		c.ccsAccountClaimReadyDuration.Observe(duration)
	} else {
		c.accountClaimReadyDuration.Observe(duration)
	}
}

func (c *MetricsCollector) SetAccountReusedCleanupDuration(duration float64) {
	c.accountReuseCleanupDuration.Observe(duration)
}

func (c *MetricsCollector) AddAccountReuseCleanupFailure() {
	c.accountReuseCleanupFailureCount.Inc()
}

func (c *MetricsCollector) SetReconcileDuration(controller string, duration float64) {
	c.reconcileDuration.WithLabelValues(controller).Observe(duration)
}
