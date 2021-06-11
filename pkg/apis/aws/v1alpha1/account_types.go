package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

const (
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
	// +optional
	Conditions               []AccountCondition `json:"conditions,omitempty"`
	State                    AccountStateStatus `json:"state,omitempty"`
	RotateCredentials        bool               `json:"rotateCredentials,omitempty"`
	RotateConsoleCredentials bool               `json:"rotateConsoleCredentials,omitempty"`
	Reused                   bool               `json:"reused,omitempty"`
}

// AccountStateStatus defines the various status an Account CR can have
type AccountStateStatus string

const (
	// AccountPending indicates an account is pending
	AccountStatusPending AccountStateStatus = "Pending"
	// AccountCreating indicates an account is being created
	AccountStatusCreating AccountStateStatus = "Creating"
	// AccountFailed indicates account creation has failed
	AccountStatusFailed AccountStateStatus = "Failed"
	// AccountInitializingRegions indicates we've kicked off the process of creating and terminating
	// instances in all supported regions
	AccountStatusInitializingRegions AccountStateStatus = "InitializingRegions"
	// AccountReady indicates account creation is ready
	AccountStatusReady AccountStateStatus = "Ready"
	// AccountPendingVerification indicates verification (of AWS limits and Enterprise Support) is pending
	AccountStatusPendingVerification AccountStateStatus = "PendingVerification"
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

// TODO - Here we're comparing AccountStateStatus to a list of AccountConditionType
// instead of a list of AccountStateStatus. Previously a.Status.State was a regular string.
// Should investigate what potential failed states a.Status.State can be and clean up
// accordingly.
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
		if string(a.Status.State) == state {
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
	return a.Status.State == AccountStatusPendingVerification
}

//IsReady returns true if an account is ready
func (a *Account) IsReady() bool {
	return a.Status.State == AccountStatusReady
}

//IsCreating returns true if an account is creating
func (a *Account) IsCreating() bool {
	return a.Status.State == AccountStatusCreating
}

//IsInitializingRegions returns true if the account state is InitalizingRegions
func (a *Account) IsInitializingRegions() bool {
	return a.Status.State == AccountStatusInitializingRegions
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

//IsProgressing returns true if the account state is Creating, Pending Verification, or InitializingRegions
func (a *Account) IsProgressing() bool {
	if a.Status.State == AccountStatusCreating ||
		a.Status.State == AccountStatusPendingVerification ||
		a.Status.State == AccountStatusInitializingRegions {
		return true
	}
	return false
}

// HasBeenClaimed lets us know if an account has been claimed at some point and can only be reused by clusters in the same legal entity
func (a *Account) HasBeenClaimedAtLeastOnce() bool {
	return a.Spec.LegalEntity.ID != "" || a.Status.Reused
}

//HasNeverBeenClaimed returns true if the account is not claimed AND has no legalEntity set, meaning it hasn't been claimed before and is not available for reuse
func (a *Account) HasNeverBeenClaimed() bool {
	return !a.Status.Claimed && a.Spec.LegalEntity.ID == ""
}

//IsOwnedByAccountPool returns true if the account has an ownerreference type that is the accountpool
func (a *Account) IsOwnedByAccountPool() bool {
	if a.ObjectMeta.OwnerReferences == nil {
		return false
	}
	for _, ref := range a.ObjectMeta.OwnerReferences {
		if ref.Kind == "AccountPool" {
			return true
		}
	}
	return false
}

const MaximumAccountStateStatus = 6

func getAllAccountStateStatus() [MaximumAccountStateStatus]AccountStateStatus {
	return [MaximumAccountStateStatus]AccountStateStatus{
		AccountStatusPending,
		AccountStatusCreating,
		AccountStatusFailed,
		AccountStatusInitializingRegions,
		AccountStatusReady,
		AccountStatusPendingVerification,
	}
}

func IsValidAccountStateStatus(stateStatus AccountStateStatus) bool {
	for _, sStat := range getAllAccountStateStatus() {
		if sStat == stateStatus {
			return true
		}
	}
	return false
}
