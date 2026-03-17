// Copyright 2018 RedHat
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

package config

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/utils"
	"github.com/openshift/aws-account-operator/test/fixtures"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// OperatorName stores the name used by this code for the AWS Account Operator
	OperatorName string = "aws-account-operator"

	// OperatorNamespace stores a string indicating the Kubernetes namespace in which the operator runs
	OperatorNamespace string = "aws-account-operator"

	EnableOLMSkipRange string = "true"

	// used in constructing ARNs
	AwsResourceTypeRole                  string = "role"
	AwsResourceTypePolicy                string = "policy"
	AwsResourceIDAdministratorAccessRole string = "AdministratorAccess"
)

var (
	isFedramp = false
)

// SetIsFedramp sets the var isFedramp to value in default configmap
func SetIsFedramp(configMap *corev1.ConfigMap) error {
	fedramp, ok := configMap.Data["fedramp"]
	if !ok {
		// Since fedramp param is not required, if fedramp param does not exist then assume fedramp=false
		isFedramp = false
		return nil
	}
	frBool, err := strconv.ParseBool(fedramp)
	if err != nil {
		return fmt.Errorf("invalid value for configmap fedramp. %w", err)
	}
	isFedramp = frBool
	return nil
}

// IsFedramp returns value of isFedramp var
func IsFedramp() bool {
	return isFedramp
}

func GetDefaultRegion() (regionName string) {
	regionName = awsv1alpha1.AwsUSEastOneRegion
	if isFedramp {
		regionName = awsv1alpha1.AwsUSGovEastOneRegion
	}
	return
}

// construct an ARN
func GetIAMArn(awsAccountID, awsResourceType, awsResourceID string) (arn string) {
	awsAPI := "aws"
	if isFedramp {
		awsAPI = "aws-us-gov"
	}

	// arn:partition:service:region:account-id:resource-type/resource-id
	arn = strings.Join([]string{"arn:", awsAPI, ":iam::", awsAccountID, ":", awsResourceType, "/", awsResourceID}, "")
	return
}

func GetDefaultAccountPoolName(reqLogger logr.Logger, kubeClient client.Client) (string, error) {

	cm, err := utils.GetOperatorConfigMap(kubeClient)
	if err != nil {
		reqLogger.Error(err, "failed retrieving configmap")
		return "", err
	}

	accountpoolString := cm.Data["accountpool"]

	type AccountPool struct {
		IsDefault bool `yaml:"default,omitempty"`
	}

	data := make(map[string]AccountPool)
	err = yaml.Unmarshal([]byte(accountpoolString), &data)

	if err != nil {
		reqLogger.Error(err, "failed unmarshalling the accountpool data")
		return "", err
	}

	for poolName, poolData := range data {
		if poolData.IsDefault {
			return poolName, nil
		}
	}

	return "", fixtures.NotFound
}

// GetPayerAccountIDs returns the list of payer account IDs from the ConfigMap
// These are root/payer accounts in AWS Organizations that should never have
// cleanup operations performed on them
func GetPayerAccountIDs(kubeClient client.Client) ([]string, error) {
	cm, err := utils.GetOperatorConfigMap(kubeClient)
	if err != nil {
		// If ConfigMap doesn't exist (e.g., in test environments), return empty list
		// This allows the operator to function without the payer account blocklist
		return []string{}, nil
	}

	payerAccountsString, ok := cm.Data["payer-account-ids"]
	if !ok {
		// If not configured, return empty list (no payer accounts to block)
		return []string{}, nil
	}

	// Parse comma-separated list of account IDs
	payerAccounts := strings.Split(payerAccountsString, ",")

	// Trim whitespace from each account ID
	result := make([]string, 0, len(payerAccounts))
	for _, accountID := range payerAccounts {
		trimmed := strings.TrimSpace(accountID)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result, nil
}

// IsPayerAccount checks if the given AWS account ID is a payer/root account
// that should be protected from all operations
func IsPayerAccount(accountID string, kubeClient client.Client) (bool, error) {
	payerAccounts, err := GetPayerAccountIDs(kubeClient)
	if err != nil {
		return false, err
	}
	return slices.Contains(payerAccounts, accountID), nil
}
