package v1alpha1

import (
	"time"

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
	// AccountCrNamespace namespace where AWS accounts will be created
	AccountCrNamespace = "aws-account-operator"
	// AccountOperatorIAMRole is the name for IAM user creating resources in account
	AccountOperatorIAMRole = "OrganizationAccountAccessRole"
	// SREAccessRoleName for CCS Account Access
	SREAccessRoleName = "RH-SRE-CCS-Access"
	// AccountFinalizer is the string finalizer name
	AccountFinalizer = "finalizer.aws.managed.openshift.io"
)

// AccountSpec defines the desired state of Account
// +k8s:openapi-gen=true
type AccountSpec struct {
	AwsAccountID  string `json:"awsAccountID"`
	IAMUserSecret string `json:"iamUserSecret"`
	BYOC          bool   `json:"byoc,omitempty"`
	// +optional
	ClaimLink string `json:"claimLink"`
	// +optional
	ClaimLinkNamespace string      `json:"claimLinkNamespace,omitempty"`
	LegalEntity        LegalEntity `json:"legalEntity,omitempty"`
	ManualSTSMode      bool        `json:"manualSTSMode,omitempty"`
}

// AccountStatus defines the observed state of Account
// +k8s:openapi-gen=true
type AccountStatus struct {
	Claimed       bool   `json:"claimed,omitempty"`
	SupportCaseID string `json:"supportCaseID,omitempty"`
	// +listType=map
	// +listMapKey=type
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
	// AccountCreationFailed is set during AWS account creation
	AccountCreationFailed AccountConditionType = "AccountCreationFailed"
	// AccountPending is set when account creation is pending
	AccountPending AccountConditionType = "Pending"
	// AccountPendingVerification is set when account creation is pending
	AccountPendingVerification AccountConditionType = "PendingVerification"
	// AccountReused is set when account is reused
	AccountReused AccountConditionType = "Reused"
	// AccountClientError is set when there was an issue getting a client
	AccountClientError AccountConditionType = "AccountClientError"
	// AccountAuthorizationError indicates an autherization error occured
	AccountAuthorizationError AccountConditionType = "AuthorizationError"
	// AccountAuthenticationError indicates an authentication error occured
	AccountAuthenticationError AccountConditionType = "AuthenticationError"
	// AccountUnhandledError indicates a error that isn't handled, probably a go error
	AccountUnhandledError AccountConditionType = "UnhandledError"
	// AccountInternalError is set when a serious internal issue arrises
	AccountInternalError AccountConditionType = "InternalError"
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
// +kubebuilder:resource:path=accounts,scope=Namespaced
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

// Helper Functions

//IsFailed returns true if an account is in a failed state
func (a *Account) IsFailed() bool {
	failedStates := [7]string{
		string(AccountFailed),
		string(AccountCreationFailed),
		string(AccountClientError),
		string(AccountAuthorizationError),
		string(AccountAuthenticationError),
		string(AccountUnhandledError),
		string(AccountInternalError),
	}
	for _, state := range failedStates {
		if a.Status.State == state {
			return true
		}
	}
	return false
}

//HasState returns true if an account has a state set at all
func (a *Account) HasState() bool {
	return a.Status.State != ""
}

//HasSupportCaseID returns true if an account has a SupportCaseID Set
func (a *Account) HasSupportCaseID() bool {
	return a.Status.SupportCaseID != ""
}

//IsPendingVerification returns true if the account is in a PendingVerification state
func (a *Account) IsPendingVerification() bool {
	return a.Status.State == string(AccountPendingVerification)
}

//IsReady returns true if an account is ready
func (a *Account) IsReady() bool {
	return a.Status.State == string(AccountReady)
}

//IsCreating returns true if an account is creating
func (a *Account) IsCreating() bool {
	return a.Status.State == string(AccountCreating)
}

//HasClaimLink returns true if an accounts claim link is not empty
func (a *Account) HasClaimLink() bool {
	return a.Spec.ClaimLink != ""
}

//IsClaimed returns true if account Status.Claimed is false
func (a *Account) IsClaimed() bool {
	return a.Status.Claimed
}

//IsPendingDeletion returns true if a DeletionTimestamp has been set
func (a *Account) IsPendingDeletion() bool {
	return a.DeletionTimestamp != nil
}

//IsBYOC returns true if account is a BYOC account
func (a *Account) IsBYOC() bool {
	return a.Spec.BYOC
}

//HasAwsAccountID returns true if awsAccountID is set
func (a *Account) HasAwsAccountID() bool {
	return a.Spec.AwsAccountID != ""
}

//IsReadyUnclaimedAndHasClaimLink returns true if an account is ready, unclaimed, and has a claim link
func (a *Account) IsReadyUnclaimedAndHasClaimLink() bool {
	return a.IsReady() &&
		a.HasClaimLink() &&
		!a.IsClaimed()
}

//HasAwsv1alpha1Finalizer returns true if the awsv1alpha1 finalizer is set on the account
func (a *Account) HasAwsv1alpha1Finalizer() bool {
	for _, v := range a.GetFinalizers() {
		if v == AccountFinalizer {
			return true
		}
	}
	return false
}

//IsOlderThan takes a parameter of a time and returns true if the creation timestamp is longer than
//the passed in time.
func (a *Account) IsOlderThan(maxDuration time.Duration) bool {
	return time.Since(a.GetCreationTimestamp().Time) > maxDuration
}

//IsBYOCPendingDeletionWithFinalizer returns true if account is a BYOC Account,
// has been marked for deletion (deletion timestamp set), and has a finalizer set.
func (a *Account) IsBYOCPendingDeletionWithFinalizer() bool {
	return a.IsPendingDeletion() &&
		a.IsBYOC() &&
		a.HasAwsv1alpha1Finalizer()
}

//IsBYOCAndNotReady returns true if account is BYOC and the state is not AccountReady
func (a *Account) IsBYOCAndNotReady() bool {
	return a.IsBYOC() && !a.IsReady()
}

//ReadyForInitialization returns true if account is a BYOC Account and the state is not ready OR
// accout state is creating, and has not been claimed
func (a *Account) ReadyForInitialization() bool {
	return a.IsBYOCAndNotReady() ||
		a.IsUnclaimedAndIsCreating()
}

//IsUnclaimedAndHasNoState returns true if account has not set state and has not been claimed
func (a *Account) IsUnclaimedAndHasNoState() bool {
	return !a.HasState() &&
		!a.IsClaimed()
}

//IsUnclaimedAndIsCreating returns true if account state is AccountCreating and has not been claimed
func (a *Account) IsUnclaimedAndIsCreating() bool {
	return a.IsCreating() &&
		!a.IsClaimed()
}

//IsInitializingRegions returns true if the account state is InitalizingRegions
func (a *Account) IsInitializingRegions() bool {
	return a.Status.State == AccountInitializingRegions
}

// GetCondition finds the condition that has the
// specified condition type in the given list. If none exists, then returns nil.
func (a *Account) GetCondition(conditionType AccountConditionType) *AccountCondition {
	for i, condition := range a.Status.Conditions {
		if condition.Type == conditionType {
			return &a.Status.Conditions[i]
		}
	}
	return nil
}
