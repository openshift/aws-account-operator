package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AccountPoolSpec defines the desired state of AccountPool
// +k8s:openapi-gen=true
type AccountPoolSpec struct {
	PoolSize int `json:"poolSize"`
}

// AccountPoolStatus defines the observed state of AccountPool
// +k8s:openapi-gen=true
type AccountPoolStatus struct {
	PoolSize          int `json:"poolSize"`
	UnclaimedAccounts int `json:"unclaimedAccounts"`
	ClaimedAccounts   int `json:"claimedAccounts"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AccountPool is the Schema for the accountpools API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Pool Size",type="integer",JSONPath=".status.poolSize",description="Desired pool size"
// +kubebuilder:printcolumn:name="Unclaimed Accounts",type="integer",JSONPath=".status.unclaimedAccounts",description="Number of unclaimed accounts"
// +kubebuilder:printcolumn:name="Claimed Accounts",type="integer",JSONPath=".status.claimedAccounts",description="Number of claimed accounts"
// +kubebuilder:resource:path=accountpools,scope=Namespaced
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
