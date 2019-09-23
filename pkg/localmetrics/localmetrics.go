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
	"fmt"
	"math"
	"time"

	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/controller/account"
	"github.com/prometheus/client_golang/prometheus"
	kubeclientpkg "sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("localmetrics")

var (
	MetricTotalAWSAccounts = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_account_operator_total_aws_accounts",
		Help: "Report how many accounts have been created in AWS org",
	}, []string{"name"})
	MetricTotalAccountCRs = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_account_operator_total_account_crs",
		Help: "Report how many account CRs have been created",
	}, []string{"name"})
	MetricTotalAccountCRsUnclaimed = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_account_operator_total_accounts_crs_unclaimed",
		Help: "Report how many account CRs are unclaimed",
	}, []string{"name"})
	MetricTotalAccountCRsClaimed = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_account_operator_total_accounts_crs_claimed",
		Help: "Report how many account CRs are claimed",
	}, []string{"name"})
	MetricTotalAccountCRsFailed = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_account_operator_total_accounts_crs_failed",
		Help: "Report how many account CRs are failed",
	}, []string{"name"})
	MetricTotalAccountClaimCRs = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_account_operator_total_aws_account_claim_crs",
		Help: "Report how many account claim CRs have been created",
	}, []string{"name"})
	MetricTotalAccountCRsReady = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_account_operator_total_aws_accounts_crs_ready",
		Help: "Report how many account CRs are ready",
	}, []string{"name"})
	MetricPoolSizeVsUnclaimed = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_account_operator_pool_size_vs_unclaimed",
		Help: "Report the difference between the pool size and the number of unclaimed account CRs",
	}, []string{"name"})
	MetricTotalAccountPendingVerification = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_account_operator_total_aws_account_pending_verification",
		Help: "Report the number of accounts waiting for enterprise support and EC2 limit increases in AWS",
	}, []string{"name"})
	MetricTotalAccountReusedAvailable = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "aws_account_operator_total_aws_account_reused_available",
		Help:        "Report the number of reused accounts available for claiming grouped by legal ID",
		ConstLabels: prometheus.Labels{"name": "aws-account-operator"},
	}, []string{"LegalID"})
	MetricTotalAccountReuseFailed = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_account_operator_total_aws_account_reused_failed",
		Help: "Report the number of accounts that failed during account reuse",
	}, []string{"name"})

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

// UpdateAWSMetrics updates the total AWS Accounts metric every N hours
func UpdateAWSMetrics(kubeClient kubeclientpkg.Client, hour int) {
	metricLogger := log.WithValues("Namespace", "aws-account-operator-operator")

	awsClient, err := awsclient.GetAWSClient(kubeClient, awsclient.NewAwsClientInput{
		SecretName: account.AwsSecretName,
		NameSpace:  awsv1alpha1.AccountCrNamespace,
		AwsRegion:  "us-east-1",
	})

	if err != nil {
		metricLogger.Error(err, "Failed to get awsClient")
		return
	}

	d := time.Duration(hour) * time.Hour
	for range time.Tick(d) {
		accountTotal, err := account.TotalAwsAccounts(awsClient)

		if err != nil {
			metricLogger.Error(err, fmt.Sprintf("Failed to get total number of AWS accounts: %s", err))
		} else {
			MetricTotalAWSAccounts.With(prometheus.Labels{"name": "aws-account-operator"}).Set(float64(accountTotal))
		}
	}
}

// UpdateAccountCRMetrics updates all metrics related to Account CRs
func UpdateAccountCRMetrics(accountList *awsv1alpha1.AccountList) {
	unclaimedAccountCount := 0
	claimedAccountCount := 0
	failedAccountCount := 0
	reuseAccountFailedCount := 0
	pendingVerificationAccountCount := 0
	readyAccountCount := 0
	idMap := make(map[string]int)
	for _, account := range accountList.Items {
		if account.Status.Claimed == false {
			// Ignore unclaimed accounts in Failed status
			if account.Status.State != "Failed" {
				// Accounts in Ready or PendingVerification status, that have not been reused
				if account.Status.Reused != true {
					unclaimedAccountCount++
				}
			}
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

	MetricTotalAccountCRs.With(prometheus.Labels{"name": "aws-account-operator"}).Set(float64(len(accountList.Items)))
	MetricTotalAccountCRsUnclaimed.With(prometheus.Labels{"name": "aws-account-operator"}).Set(float64(unclaimedAccountCount))
	MetricTotalAccountCRsClaimed.With(prometheus.Labels{"name": "aws-account-operator"}).Set(float64(claimedAccountCount))
	MetricTotalAccountPendingVerification.With(prometheus.Labels{"name": "aws-account-operator"}).Set(float64(pendingVerificationAccountCount))
	MetricTotalAccountCRsFailed.With(prometheus.Labels{"name": "aws-account-operator"}).Set(float64(failedAccountCount))
	MetricTotalAccountCRsReady.With(prometheus.Labels{"name": "aws-account-operator"}).Set(float64(readyAccountCount))
	MetricTotalAccountReuseFailed.With(prometheus.Labels{"name": "aws-account-operator"}).Set(float64(reuseAccountFailedCount))
	for id, val := range idMap {
		MetricTotalAccountReusedAvailable.With(prometheus.Labels{"LegalID": id}).Set(float64(val))
	}
}

// UpdateAccountClaimMetrics updates all metrics related to AccountClaim CRs
func UpdateAccountClaimMetrics(accountClaimList *awsv1alpha1.AccountClaimList) {

	MetricTotalAccountClaimCRs.With(prometheus.Labels{"name": "aws-account-operator"}).Set(float64(len(accountClaimList.Items)))
}

// UpdatePoolSizeVsUnclaimed updates the metric that measures the difference between Poolsize and Unclaimed Account CRs
func UpdatePoolSizeVsUnclaimed(poolSize int, unclaimedAccountCount int) {

	metric := math.Abs(float64(poolSize) - float64(unclaimedAccountCount))

	MetricPoolSizeVsUnclaimed.With(prometheus.Labels{"name": "aws-account-operator"}).Set(metric)
}
