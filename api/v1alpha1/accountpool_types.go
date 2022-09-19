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
	PoolSize int `json:"poolSize"`

	// UnclaimedAccounts is an approximate value representing the amount of non-failed accounts
	UnclaimedAccounts int `json:"unclaimedAccounts"`

	// ClaimedAccounts is an approximate value representing the amount of accounts that are currently claimed
	ClaimedAccounts int `json:"claimedAccounts"`

	// AvailableAccounts denotes accounts that HAVE NEVER BEEN CLAIMED, so NOT reused, and are READY to be claimed.  This differs from the UnclaimedAccounts, who similarly HAVE NEVER BEEN CLAIMED, but include ALL non-FAILED states
	AvailableAccounts int `json:"availableAccounts"`

	// AccountsProgressing shows the approximate value of the number of accounts that are in the creation workflow (Creating, PendingVerification, InitializingRegions)
	AccountsProgressing int `json:"accountsProgressing"`

	// AWSLimitDelta shows the approximate difference between the number of AWS accounts currently created and the limit. This should be the same across all hive shards in an environment
	AWSLimitDelta int `json:"awsLimitDelta"`
}

// +genclient
// +kubebuilder:object:root=true

// AccountPool is the Schema for the accountpools API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Pool Size",type="integer",JSONPath=".status.poolSize",description="Desired pool size"
// +kubebuilder:printcolumn:name="Unclaimed Accounts",type="integer",JSONPath=".status.unclaimedAccounts",description="Number of unclaimed accounts"
// +kubebuilder:printcolumn:name="Claimed Accounts",type="integer",JSONPath=".status.claimedAccounts",description="Number of claimed accounts"
// +kubebuilder:printcolumn:name="Available Accounts",type="integer",JSONPath=".status.availableAccounts",description="Number of ready accounts"
// +kubebuilder:printcolumn:name="Accounts Progressing",type="integer",JSONPath=".status.accountsProgressing",description="Number of accounts progressing towards ready"
// +kubebuilder:printcolumn:name="AWS Limit Delta",type="integer",JSONPath=".status.awsLimitDelta",description="Difference between accounts created and soft limit"
// +kubebuilder:resource:path=accountpools,scope=Namespaced
type AccountPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AccountPoolSpec   `json:"spec,omitempty"`
	Status AccountPoolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AccountPoolList contains a list of AccountPool
type AccountPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AccountPool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AccountPool{}, &AccountPoolList{})
}
