package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AWSFederatedAccountAccessState defines the various status an FederatedAccountAccess CR can have
type AWSFederatedAccountAccessState string

const (
	// AWSFederatedAccountAccessStateInProgress const for InProgress status state
	AWSFederatedAccountAccessStateInProgress AWSFederatedAccountAccessState = "InProgress"
	// AWSFederatedAccountStateReady const for Applied status state
	AWSFederatedAccountStateReady AWSFederatedAccountAccessState = "Ready"
	// AWSFederatedAccountStateFailed cont for Failed status state
	AWSFederatedAccountStateFailed AWSFederatedAccountAccessState = "Failed"
)

// AWSFederatedAccountAccessSpec defines the desired state of AWSFederatedAccountAccess
// +k8s:openapi-gen=true
type AWSFederatedAccountAccessSpec struct {
	// ExternalCustomerAWSARN holds the external AWS IAM ARN
	ExternalCustomerAWSIAMARN string `json:"externalCustomerAWSIAMARN"`
	// AWSCustomerCredentialSecret holds the credentials to the cluster account where the role wil be created
	AWSCustomerCredentialSecret AWSSecretReference `json:"awsCustomerCredentialSecret"`
	// FederatedRoleName must be the name of a federatedrole cr that currently exists
	AWSFederatedRole AWSFederatedRoleRef `json:"awsFederatedRole"`
}

// AWSFederatedAccountAccessStatus defines the observed state of AWSFederatedAccountAccess
// +k8s:openapi-gen=true
type AWSFederatedAccountAccessStatus struct {
	// +listType=map
	// +listMapKey=type
	Conditions []AWSFederatedAccountAccessCondition `json:"conditions"`
	State      AWSFederatedAccountAccessState       `json:"state"`
	ConsoleURL string                               `json:"consoleURL,omitempty"`
}

// AWSFederatedAccountAccessCondition defines a current condition state of the account
type AWSFederatedAccountAccessCondition struct {
	// Type is the type of the condition.
	Type AWSFederatedAccountAccessConditionType `json:"type"`
	// Status is the status of the condition
	Status corev1.ConditionStatus `json:"status"`
	// LastProbeTime is the last time we probed the condition.
	// +optional
	LastProbeTime metav1.Time `json:"lastProbeTime,omitempty"`
	// LastTransitionTime is the laste time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
	// Reason is a unique, one-word, CamelCase reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty"`
	// Message is a human-readable message indicating details about last transition.
	// +optional
	Message string `json:"message,omitempty"`
}

// AWSFederatedAccountAccessConditionType is a valid value for AccountCondition.Type
type AWSFederatedAccountAccessConditionType string

const (
	// AWSFederatedAccountInProgress is set when an Account access is in progress
	AWSFederatedAccountInProgress AWSFederatedAccountAccessConditionType = "InProgress"
	// AWSFederatedAccountReady is set when an Account access has been successfully applied
	AWSFederatedAccountReady AWSFederatedAccountAccessConditionType = "Ready"
	// AWSFederatedAccountFailed is set when account access has failed to apply
	AWSFederatedAccountFailed AWSFederatedAccountAccessConditionType = "Failed"
)

// AWSSecretReference holds the name and namespace of an secret containing credentials to cluster account
type AWSSecretReference struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// AWSFederatedRoleRef holds the name and namespace to reference an AWSFederatedRole CR
type AWSFederatedRoleRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AWSFederatedAccountAccess is the Schema for the awsfederatedaccountaccesses API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.state",description="Status the federated account access user"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age since federated account access user was created"
// +kubebuilder:resource:path=awsfederatedaccountaccesses,scope=Namespaced
type AWSFederatedAccountAccess struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AWSFederatedAccountAccessSpec   `json:"spec,omitempty"`
	Status AWSFederatedAccountAccessStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AWSFederatedAccountAccessList contains a list of AWSFederatedAccountAccess
type AWSFederatedAccountAccessList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AWSFederatedAccountAccess `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AWSFederatedAccountAccess{}, &AWSFederatedAccountAccessList{})
}
