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
	"context"
	"fmt"
	"strconv"

	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// OperatorName stores the name used by this code for the AWS Account Operator
	OperatorName string = "aws-account-operator"

	// OperatorNamespace stores a string indicating the Kubernetes namespace in which the operator runs
	OperatorNamespace string = "aws-account-operator"
)

var (
	log       = logf.Log.WithName("config")
	isFedramp = false
)

// SetIsFedramp sets the var isFedramp to value in default configmap
func SetIsFedramp(kubeClient client.Client) error {
	configMap := &corev1.ConfigMap{}
	err := kubeClient.Get(context.TODO(), types.NamespacedName{Namespace: awsv1alpha1.AccountCrNamespace, Name: awsv1alpha1.DefaultConfigMap}, configMap)
	if err != nil {
		return fmt.Errorf("Error getting configmap. Could not set fedramp var. %w", err)
	}

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
