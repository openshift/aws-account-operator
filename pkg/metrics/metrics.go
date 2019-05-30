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

package metrics

import (
	"math"
	"net/http"

	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	// MetricsEndpoint is the port to export metrics on
	MetricsEndpoint = ":8080"
)

var (
	metricTotalAWSAccounts = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_account_operator_total_aws_accounts",
		Help: "Report how many accounts have been created in AWS org",
	}, []string{"name"})
	metricTotalAccountCRs = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_account_operator_total_account_crs",
		Help: "Report how many account CRs have been created",
	}, []string{"name"})
	metricTotalAccountCRsUnclaimed = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_account_operator_total_accounts_crs_unclaimed",
		Help: "Report how many account CRs are unclaimed",
	}, []string{"name"})
	metricTotalAccountCRsClaimed = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_account_operator_total_accounts_crs_claimed",
		Help: "Report how many account CRs are claimed",
	}, []string{"name"})
	metricTotalAccountCRsFailed = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_account_operator_total_accounts_crs_failed",
		Help: "Report how many account  CRs are failed",
	}, []string{"name"})
	metricTotalAccountClaimCRs = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_account_operator_total_aws_account_claim_crs",
		Help: "Report how many account claim CRs have been created",
	}, []string{"name"})
	metricPoolSizeVsUnclaimed = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_account_operator_pool_size_vs_unclaimed",
		Help: "Report the difference between the pool size and the number of unclaimed account CRs",
	}, []string{"name"})

	metricsList = []prometheus.Collector{
		metricTotalAWSAccounts,
		metricTotalAccountCRs,
		metricTotalAccountCRsUnclaimed,
		metricTotalAccountCRsClaimed,
		metricTotalAccountCRsFailed,
		metricTotalAccountClaimCRs,
		metricPoolSizeVsUnclaimed,
	}
)

// StartMetrics register metrics and exposes them
func StartMetrics() {
	// Register metrics and start serving them on /metrics endpoint
	RegisterMetrics()
	http.Handle("/metrics", prometheus.Handler())
	go http.ListenAndServe(MetricsEndpoint, nil)
}

// RegisterMetrics for the operator
func RegisterMetrics() error {
	for _, metric := range metricsList {
		err := prometheus.Register(metric)
		if err != nil {
			return err
		}
	}
	return nil
}

// UpdateAWSMetrics updates all AWS related metrics
func UpdateAWSMetrics(totalAccounts int) {
	metricTotalAWSAccounts.With(prometheus.Labels{"name": "aws-account-operator"}).Set(float64(totalAccounts))
}

// UpdateAccountCRMetrics updates all metrics related to Account CRs
func UpdateAccountCRMetrics(accountList *awsv1alpha1.AccountList) {

	unclaimedAccountCount := 0
	claimedAccountCount := 0
	failedAccountCount := 0
	for _, account := range accountList.Items {
		if account.Status.Claimed == false {
			if account.Status.State != "Failed" {
				unclaimedAccountCount++
			}
		} else {
			claimedAccountCount++
		}
		if account.Status.State == "Failed" {
			failedAccountCount++
		}
	}

	metricTotalAccountCRs.With(prometheus.Labels{"name": "aws-account-operator"}).Set(float64(len(accountList.Items)))
	metricTotalAccountCRsUnclaimed.With(prometheus.Labels{"name": "aws-account-operator"}).Set(float64(unclaimedAccountCount))
	metricTotalAccountCRsClaimed.With(prometheus.Labels{"name": "aws-account-operator"}).Set(float64(claimedAccountCount))
	metricTotalAccountCRsFailed.With(prometheus.Labels{"name": "aws-account-operator"}).Set(float64(failedAccountCount))
}

// UpdateAccountClaimMetrics updates all metrics related to AccountClaim CRs
func UpdateAccountClaimMetrics(accountClaimList *awsv1alpha1.AccountClaimList) {

	metricTotalAccountClaimCRs.With(prometheus.Labels{"name": "aws-account-operator"}).Set(float64(len(accountClaimList.Items)))
}

// UpdatePoolSizeVsUnclaimed updates the metric that measures the difference between Poolsize and Unclaimed Account CRs
func UpdatePoolSizeVsUnclaimed(poolSize int, unclaimedAccountCount int) {

	metric := math.Abs(float64(poolSize) - float64(unclaimedAccountCount))

	metricPoolSizeVsUnclaimed.With(prometheus.Labels{"name": "aws-account-operator"}).Set(metric)
}
