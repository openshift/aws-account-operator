package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AccountStateStatus defines the various status an Account CR can have
type AccountStateStatus string

const (
	// AccountStatusRequested const for Requested status
	AccountStatusRequested AccountStateStatus = "Requested"
	// AccountStatusClaimed const for Claimed status
	AccountStatusClaimed AccountStateStatus = "Claimed"
	// AccountStatusTransfering const for Transfering status
	accountStatusTransfering AccountStateStatus = "Transfering"
	// AccountStatusTransfered const for Transfering status
	accountStatusTransfered AccountStateStatus = "Transfered"
	// AccountStatusDeleting const for Deleting status
	accountStatusDeleting AccountStateStatus = "Deleting"
	// AccountStatusPendingVerification const for Pending Verification status
	accountStatusPendingVerification AccountStateStatus = "PendingVerification"
	// AccountCrNamespace namespace where AWS accounts will be created
	AccountCrNamespace = "aws-account-operator"
	// AccountOperatorIAMRole is the name for IAM user creating resources in account
	AccountOperatorIAMRole = "OrganizationAccountAccessRole"
	// AccountFinalizer is the string finalizer name
	AccountFinalizer = "finalizer.aws.managed.openshift.io"
)

// AccountSpec defines the desired state of Account
// +k8s:openapi-gen=true
type AccountSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book.kubebuilder.io/beyond_basics/generating_crd.html
	AwsAccountID  string `json:"awsAccountID"`
	IAMUserSecret string `json:"iamUserSecret"`
	BYOC          bool   `json:"byoc,omitempty"`
	// +optional
	ClaimLink string `json:"claimLink"`
	// +optional
	ClaimLinkNamespace string      `json:"claimLinkNamespace,omitempty"`
	LegalEntity        LegalEntity `json:"legalEntity,omitempty"`
}

// AccountStatus defines the observed state of Account
// +k8s:openapi-gen=true
type AccountStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book.kubebuilder.io/beyond_basics/generating_crd.html
	Claimed                  bool               `json:"claimed,omitempty"`
	SupportCaseID            string             `json:"supportCaseID,omitempty"`
	Conditions               []AccountCondition `json:"conditions,omitempty"`
	State                    string             `json:"state,omitempty"`
	RotateCredentials        bool               `json:"rotateCredentials,omitempty"`
	RotateConsoleCredentials bool               `json:"rotateConsoleCredentials,omitempty"`
	Reused                   bool               `json:"reused,omitempty"`
}

// AccountCondition contains details for the current condition of a AWS account
type AccountCondition struct {
	// Type is the type of the condition.
	Type AccountConditionType `json:"type,omitempty"`
	// Status is the status of the condition
	Status corev1.ConditionStatus `json:"status,omitempty"`
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

// AccountConditionType is a valid value for AccountCondition.Type
type AccountConditionType string

const (
	// AccountCreating is set when an Account is being created
	AccountCreating AccountConditionType = "Creating"
	// AccountReady is set when an Account creation is ready
	AccountReady AccountConditionType = "Ready"
	// AccountFailed is set when account creation has failed
	AccountFailed AccountConditionType = "Failed"
	// AccountPending is set when account creation is pending
	AccountPending AccountConditionType = "Pending"
	// AccountPendingVerification is set when account creation is pending
	AccountPendingVerification AccountConditionType = "PendingVerification"
	// AccountReused is set when account is reused
	AccountReused AccountConditionType = "Reused"
	// AccountInitializingRegions indicates we've kicked off the process of creating and terminating
	// instances in all supported regions
	AccountInitializingRegions = "InitializingRegions"
	// AccountQuotaIncreaseRequested is set when a quota increase has been requested
	AccountQuotaIncreaseRequested AccountConditionType = "QuotaIncreaseRequested"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Account is the Schema for the accounts API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.state",description="Status the account"
// +kubebuilder:printcolumn:name="Claimed",type="boolean",JSONPath=".status.claimed",description="True if the account has been claimed"
// +kubebuilder:printcolumn:name="Claim",type="string",JSONPath=".spec.claimLink",description="Link to the account claim CR"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age since the account was created"
type Account struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AccountSpec   `json:"spec,omitempty"`
	Status AccountStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AccountList contains a list of Account
type AccountList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Account `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Account{}, &AccountList{})
}
