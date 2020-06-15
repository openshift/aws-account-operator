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
	"math"
	"sync"

	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/prometheus/client_golang/prometheus"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

const operatorName = "aws-account-operator"

var log = logf.Log.WithName("localmetrics")

var (
	MetricTotalAWSAccounts = prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "aws_account_operator_total_aws_accounts",
		Help:        "Report how many accounts have been created in AWS org",
		ConstLabels: prometheus.Labels{"name": operatorName},
	})
	MetricTotalAccountCRs = prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "aws_account_operator_total_account_crs",
		Help:        "Report how many account CRs have been created",
		ConstLabels: prometheus.Labels{"name": operatorName},
	})
	MetricTotalAccountCRsUnclaimed = prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "aws_account_operator_total_accounts_crs_unclaimed",
		Help:        "Report how many account CRs are unclaimed",
		ConstLabels: prometheus.Labels{"name": operatorName},
	})
	MetricTotalAccountCRsClaimed = prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "aws_account_operator_total_accounts_crs_claimed",
		Help:        "Report how many account CRs are claimed",
		ConstLabels: prometheus.Labels{"name": operatorName},
	})
	MetricTotalAccountCRsFailed = prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "aws_account_operator_total_accounts_crs_failed",
		Help:        "Report how many account CRs are failed",
		ConstLabels: prometheus.Labels{"name": operatorName},
	})
	MetricTotalAccountClaimCRs = prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "aws_account_operator_total_aws_account_claim_crs",
		Help:        "Report how many account claim CRs have been created",
		ConstLabels: prometheus.Labels{"name": operatorName},
	})
	MetricTotalAccountCRsReady = prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "aws_account_operator_total_aws_accounts_crs_ready",
		Help:        "Report how many account CRs are ready",
		ConstLabels: prometheus.Labels{"name": operatorName},
	})
	MetricPoolSizeVsUnclaimed = prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "aws_account_operator_pool_size_vs_unclaimed",
		Help:        "Report the difference between the pool size and the number of unclaimed account CRs",
		ConstLabels: prometheus.Labels{"name": operatorName},
	})
	MetricTotalAccountPendingVerification = prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "aws_account_operator_total_aws_account_pending_verification",
		Help:        "Report the number of accounts waiting for enterprise support and EC2 limit increases in AWS",
		ConstLabels: prometheus.Labels{"name": operatorName},
	})
	MetricTotalAccountReusedAvailable = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "aws_account_operator_total_aws_account_reused_available",
		Help:        "Report the number of reused accounts available for claiming grouped by legal ID",
		ConstLabels: prometheus.Labels{"name": operatorName},
	}, []string{"LegalID"})
	MetricTotalAccountReuseFailed = prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "aws_account_operator_total_aws_account_reused_failed",
		Help:        "Report the number of accounts that failed during account reuse",
		ConstLabels: prometheus.Labels{"name": operatorName},
	})

	MetricsList = []prometheus.Collector{
		MetricTotalAWSAccounts,
		MetricTotalAccountCRs,
		MetricTotalAccountCRsUnclaimed,
		MetricTotalAccountCRsClaimed,
		MetricTotalAccountCRsFailed,
		MetricTotalAccountClaimCRs,
		MetricTotalAccountCRsReady,
		MetricPoolSizeVsUnclaimed,
		MetricTotalAccountPendingVerification,
		MetricTotalAccountReusedAvailable,
		MetricTotalAccountReuseFailed,
	}
)

// UpdateAccountCRUnclaimedMetric updates the unclaimed account metric
func UpdateAccountCRUnclaimedMetric(accountList awsv1alpha1.AccountList, wg *sync.WaitGroup) {

	wg.Add(1)
	unclaimedAccountCount := 0

	for _, account := range accountList.Items {
		if account.Status.Claimed == false && account.Status.Reused == false {
			if account.Status.State == "Ready" || account.Status.State == string(awsv1alpha1.AccountPendingVerification) {
				unclaimedAccountCount++
			}
		}
	}

	MetricTotalAccountCRsUnclaimed.Set(float64(unclaimedAccountCount))
	wg.Done()
}

// UpdateAccountCRMetrics updates all metrics related to Account CRs
func UpdateAccountCRMetrics(accountList *awsv1alpha1.AccountList) {

	var wg sync.WaitGroup
	go UpdateAccountCRUnclaimedMetric(*accountList, &wg)

	claimedAccountCount := 0
	failedAccountCount := 0
	reuseAccountFailedCount := 0
	pendingVerificationAccountCount := 0
	readyAccountCount := 0
	idMap := make(map[string]int)
	for _, account := range accountList.Items {
		if account.Status.Claimed == false {
			if account.Status.State == "Ready" {
				// Reused accounts in Ready state are counted in separate metric
				if account.Status.Reused == true {
					if idMap[account.Spec.LegalEntity.ID] == 0 {
						idMap[account.Spec.LegalEntity.ID] = 1
					} else {
						idMap[account.Spec.LegalEntity.ID] = idMap[account.Spec.LegalEntity.ID] + 1
					}
				} else {
					// Regular account (non-reused) in Ready state
					readyAccountCount++
				}
			}
		} else {
			claimedAccountCount++
		}

		if account.Status.State == "Failed" {
			if account.Status.Reused == true {
				// Account failed during reuse process
				reuseAccountFailedCount++
			} else {
				// Other types of failed account
				failedAccountCount++
			}
		} else if account.Status.State == "PendingVerification" {
			pendingVerificationAccountCount++
		}
	}

	MetricTotalAccountCRs.Set(float64(len(accountList.Items)))
	MetricTotalAccountCRsClaimed.Set(float64(claimedAccountCount))
	MetricTotalAccountPendingVerification.Set(float64(pendingVerificationAccountCount))
	MetricTotalAccountCRsFailed.Set(float64(failedAccountCount))
	MetricTotalAccountCRsReady.Set(float64(readyAccountCount))
	MetricTotalAccountReuseFailed.Set(float64(reuseAccountFailedCount))

	for id, val := range idMap {
		MetricTotalAccountReusedAvailable.WithLabelValues(id).Set(float64(val))
	}

	wg.Wait()
}

// UpdateAccountClaimMetrics updates all metrics related to AccountClaim CRs
func UpdateAccountClaimMetrics(accountClaimList *awsv1alpha1.AccountClaimList) {

	MetricTotalAccountClaimCRs.Set(float64(len(accountClaimList.Items)))
}

// UpdatePoolSizeVsUnclaimed updates the metric that measures the difference between Poolsize and Unclaimed Account CRs
func UpdatePoolSizeVsUnclaimed(poolSize int, unclaimedAccountCount int) {

	metric := math.Abs(float64(poolSize) - float64(unclaimedAccountCount))

	MetricPoolSizeVsUnclaimed.Set(metric)
}
