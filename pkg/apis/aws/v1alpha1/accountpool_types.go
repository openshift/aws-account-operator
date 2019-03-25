package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AccountPoolSpec defines the desired state of AccountPool
// +k8s:openapi-gen=true
type AccountPoolSpec struct {
	PoolSize int `json:"poolsize"`
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book.kubebuilder.io/beyond_basics/generating_crd.html
}

// AccountPoolStatus defines the observed state of AccountPool
// +k8s:openapi-gen=true
type AccountPoolStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book.kubebuilder.io/beyond_basics/generating_crd.html
	PoolSize          int `json:"poolsize"`
	UnclaimedAccounts int `json:"unclaimedaccounts"`
	ClaimedAccounts   int `json:"claimedaccounts"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AccountPool is the Schema for the accountpools API
// +k8s:openapi-gen=true
type AccountPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AccountPoolSpec   `json:"spec,omitempty"`
	Status AccountPoolStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AccountPoolList contains a list of AccountPool
type AccountPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AccountPool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AccountPool{}, &AccountPoolList{})
}
