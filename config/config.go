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
		return fmt.Errorf("Invalid value for configmap fedramp. %w", err)
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
		IsDefault     bool              `yaml:"default,omitempty"`
		Servicequotas map[string]string `yaml:"servicequotas,omitempty"`
	}

	data := make(map[string]AccountPool)
	err = yaml.Unmarshal([]byte(accountpoolString), &data)

	if err != nil {
		return "", err
	}

	for poolName, poolData := range data {
		if poolData.IsDefault {
			return poolName, nil
		}
	}

	return "", fixtures.NotFound
}
