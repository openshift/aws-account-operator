package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AWSManagedRoleState defines the various status an AWSManagedRole can have
type AWSManagedRoleState string

const (
	// AWSManagedRoleStateValid const for Requested status state
	AWSManagedRoleStateValid AWSManagedRoleState = "Valid"
	// AWSManagedRoleStateInvalid const for Invliad status state
	AWSManagedRoleStateInvalid AWSManagedRoleState = "Invalid"
	// AWSManagedRoleAnnotationPrefix const for account-role linking
	AWSManagedRoleAnnotationPrefix string = "role.managed.openshift.io/"
)

// AWSManagedRoleSpec defines the desired state of AWSManagedRole
// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
type AWSManagedRoleSpec struct {
	// DisplayName - The display name to use for on-account IAM resources
	DisplayName string `json:"displayName"`
	// Description - The description of the role and policy (if created) that will appear in the customer's account
	Description string `json:"description"`
	// AWSCustomPolicy is the definition of a custom aws permission policy that will be associated with this role
	// AWSCustomPolicy struct is defined in AWSManagedRole Types
	// +optional
	AWSCustomPolicy AWSCustomPolicy `json:"awsCustomPolicy,omitempty"`
	// AWSManagedPolicies is a list of amazong managed policies that exist in aws
	// +optional
	// +listType=atomic
	AWSManagedPolicies []string `json:"awsManagedPolicies,omitempty"`
}

// AWSManagedRoleStatus defines the observed state of AWSManagedRole
type AWSManagedRoleStatus struct {
	State AWSManagedRoleState `json:"state"`
	// +listType=map
	// +listMapKey=type
	Conditions []AWSManagedRoleCondition `json:"conditions"`
}

// AWSManagedRoleCondition is a Kubernetes condition type for tracking AWS Managed Role status changes
type AWSManagedRoleCondition struct {
	// Type is the type of the condition.
	Type AWSManagedRoleConditionType `json:"type"`
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

// AWSManagedRoleConditionType is a valid value for AWSManagedStateCondition Type
type AWSManagedRoleConditionType string

const (
	// AWSManagedRoleInProgress is set when an awsfederated role is InProgress
	AWSManagedRoleInProgress AWSManagedRoleConditionType = "InProgress"
	// AWSManagedRoleValid is set when an awsfederated role is valid
	AWSManagedRoleValid AWSManagedRoleConditionType = "Valid"
	// AWSManagedRoleInvalid is set when an awsfederated role is invalid
	AWSManagedRoleInvalid AWSManagedRoleConditionType = "Invalid"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AWSManagedRole is the Schema for the awsmanagedroles API
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=awsmanagedroles,scope=Namespaced
type AWSManagedRole struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AWSManagedRoleSpec   `json:"spec,omitempty"`
	Status AWSManagedRoleStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AWSManagedRoleList contains a list of AWSManagedRole
type AWSManagedRoleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AWSManagedRole `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AWSManagedRole{}, &AWSManagedRoleList{})
}
