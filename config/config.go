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

const (
	// OperatorName stores the name used by this code for the AWS Account Operator
	OperatorName string = "aws-account-operator"

	// OperatorNamespace stores a string indicating the Kubernetes namespace in which the operator runs
	OperatorNamespace string = "aws-account-operator"
)

// for use across the operator
var IsFedramp = false
