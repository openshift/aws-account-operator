package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AccountClaimSpec defines the desired state of AccountClaim
// +k8s:openapi-gen=true
type AccountClaimSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book.kubebuilder.io/beyond_basics/generating_crd.html
	LegalEntity         LegalEntity         `json:"legalentity"`
	AwsCredentialSecret AwsCredentialSecret `json:"awscredentialsecret"`
	Aws                 Aws                 `json:"aws"`
	AccountLink         string              `json:"acconutlink"`
}

// AccountClaimStatus defines the observed state of AccountClaim
// +k8s:openapi-gen=true
type AccountClaimStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book.kubebuilder.io/beyond_basics/generating_crd.html
	BillingAccountID string                  `json:"billingAcountID"`
	Conditions       []AccountClaimCondition `json:"conditions"`
}

// +genclient

// AccountClaimCondition contains details for the current condition of a AWS account claim
type AccountClaimCondition struct {
	// Type is the type of the condition.
	Type AccountClaimConditionType `json:"type"`
	// Status is the status of the condition.
	Status corev1.ConditionStatus `json:"status"`
	// LastProbeTime is the last time we probed the condition.
	// +optional
	LastProbeTime metav1.Time `json:"lastProbeTime,omitempty"`
	// LastTransitionTime is the last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
	// Reason is a unique, one-word, CamelCase reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty"`
	// Message is a human-readable message indicating details about last transition.
	// +optional
	Message string `json:"message,omitempty"`
}

// AccountClaimConditionType  is a valid value for AccountClaimCondition.Type
type AccountClaimConditionType string

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AccountClaim is the Schema for the accountclaims API
// +k8s:openapi-gen=true
type AccountClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AccountClaimSpec   `json:"spec,omitempty"`
	Status AccountClaimStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AccountClaimList contains a list of AccountClaim
type AccountClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AccountClaim `json:"items"`
}

// LegalEntity contains Red Hat specific identifiers to the original creator the clusters
type LegalEntity struct {
	Name string `json:,"name"`
	Id   int    `json:,"id"`
}

// AwsCredentialSecret contains the name of the secret and name of the namespace
// where UHC would like the AWS credentials secret to be placed
type AwsCredentialSecret struct {
	Name      string `json:,"name"`
	Namespace string `json:,"namespace"`
}

// Aws struct contains specific AWS account configuration options
type Aws struct {
	Regions []AwsRegions `json:,"regions"`
}

// AwsRegions struct contains specific AwsRegion information, at the moment its just
// name but in the future it will contain specific resource limits etc.
type AwsRegions struct {
	Name string `json:,"name"`
}

func init() {
	SchemeBuilder.Register(&AccountClaim{}, &AccountClaimList{})
}
