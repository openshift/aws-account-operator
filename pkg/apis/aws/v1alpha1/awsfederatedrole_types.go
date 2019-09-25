package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AWSFederatedRoleState defines the various status an AWSFederatedRole CR can have
type AWSFederatedRoleState string

const (
	// AWSFederatedRoleStateInProgress const for InProgress status state
	AWSFederatedRoleStateInProgress AWSFederatedAccountAccessState = "InProgress"
	// AWSFederatedRoleStateValid const for Requested status state
	AWSFederatedRoleStateValid AWSFederatedRoleState = "Valid"
	// AWSFederatedRoleStateInvalid const for Invliad status state
	AWSFederatedRoleStateInvalid AWSFederatedRoleState = "Invalid"
)

// AWSFederatedRoleSpec defines the desired state of AWSFederatedRole
// +k8s:openapi-gen=true
type AWSFederatedRoleSpec struct {
	RoleDescription string `json:"roleDescription"`
	// AWSCustomPolicy is the defenition of a custom aws permission policy that will be associated with this role
	// +optional
	AWSCustomPolicy AWSCustomPolicy `json:"awsCustomPolicy,omitempty"`
	// AWSManagedPolicies is a list of amazong managed policies that exist in aws
	// +optional
	AWSManagedPolicies []string `json:"awsManagedPolicies,omitempty"`
}

// AWSCustomPolicy holds the data required to create a custom policy in aws.
type AWSCustomPolicy struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Statements  []StatementEntry `json:"awsStatements"`
}

// StatementEntry is the smallest gourping of permissions required to create an aws policy
type StatementEntry struct {
	Effect    string     `json:"effect"`
	Action    []string   `json:"action"`
	Resource  []string   `json:"resourcce,omitempty"`
	Principal *Principal `json:"principal,omitempty"`
}

// Principal  contains the aws account id for the principle entity of a role
type Principal struct {
	// aws account id
	AWS string `json:"AWS"`
}

// AWSFederatedRoleStatus defines the observed state of AWSFederatedRole
// +k8s:openapi-gen=true
type AWSFederatedRoleStatus struct {
	State AWSFederatedRoleState `json:"state"`
}

type AWSFederatedStateCondition struct {
	// Type is the type of the condition.
	Type AWSFederatedRoleConditionType `json:"type"`
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

// AWSFederatedRoleConditionType is a valid value for AWSFederatedStateCondition Type
type AWSFederatedRoleConditionType string

const (
	// AWSFederatedRoleInProgress is set when an awsfederated role is InProgress
	AWSFederatedRoleInProgress AWSFederatedRoleConditionType = "InProgress"
	// AWSFederatedRoleValid is set when an awsfederated role is valid
	AWSFederatedRoleValid AWSFederatedRoleConditionType = "Valid"
	// AWSFederatedRoleInvalid is set when an awsfederated role is invalid
	AWSFederatedRoleInvalid AWSFederatedRoleConditionType = "Invalid"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AWSFederatedRole is the Schema for the awsfederatedroles API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
type AWSFederatedRole struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AWSFederatedRoleSpec   `json:"spec,omitempty"`
	Status AWSFederatedRoleStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AWSFederatedRoleList contains a list of AWSFederatedRole
type AWSFederatedRoleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AWSFederatedRole `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AWSFederatedRole{}, &AWSFederatedRoleList{})
}
