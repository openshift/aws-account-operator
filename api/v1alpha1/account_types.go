package v1alpha1

import (
	"fmt"
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
	ClaimLinkNamespace    string                `json:"claimLinkNamespace,omitempty"`
	LegalEntity           LegalEntity           `json:"legalEntity,omitempty"`
	ManualSTSMode         bool                  `json:"manualSTSMode,omitempty"`
	AccountPool           string                `json:"accountPool,omitempty"`
	RegionalServiceQuotas RegionalServiceQuotas `json:"regionalServiceQuotas,omitempty"`
}

type RegionalServiceQuotas map[string]AccountServiceQuota

// +k8s:openapi-gen=true
type AccountServiceQuota map[SupportedServiceQuotas]*ServiceQuotaStatus

type ServiceQuotaStatus struct {
	Value  int                  `json:"value"`
	Status ServiceRequestStatus `json:"status"`
}

type ServiceRequestStatus string

const (
	ServiceRequestTodo       ServiceRequestStatus = "TODO"
	ServiceRequestInProgress ServiceRequestStatus = "IN_PROGRESS"
	ServiceRequestCompleted  ServiceRequestStatus = "COMPLETED"
	ServiceRequestDenied     ServiceRequestStatus = "DENIED"
)

type SupportedServiceQuotas string

const (
	RulesPerSecurityGroup     SupportedServiceQuotas = "L-0EA8095F"
	RunningStandardInstances  SupportedServiceQuotas = "L-1216C47A"
	NLBPerRegion              SupportedServiceQuotas = "L-69A177A2"
	EC2VPCElasticIPsQuotaCode SupportedServiceQuotas = "L-0263D0A3" // EC2-VPC Elastic IPs
	VPCNetworkAclQuotaCode    SupportedServiceQuotas = "L-2AEEBF1A" // VPC-Network ACL
	GeneralPurposeSSD         SupportedServiceQuotas = "L-7A658B76" // General Purpose SSD (gp3) volumes
)

type SupportedServiceQuotaServices string

const (
	EC2ServiceQuota      SupportedServiceQuotaServices = "ec2"
	VPCServiceQuota      SupportedServiceQuotaServices = "vpc"
	EBSServiceQuota      SupportedServiceQuotaServices = "ebs"
	Elasticloadbalancing SupportedServiceQuotaServices = "elasticloadbalancing"
)

type OptInRegions map[string]*OptInRegionStatus

type OptInRegionStatus struct {
	RegionCode string             `json:"regionCode"`
	Status     OptInRequestStatus `json:"status"`
}
type OptInRequestStatus string

const (
	OptInRequestTodo     OptInRequestStatus = "TODO"
	OptInRequestEnabling OptInRequestStatus = "ENABLING"
	OptInRequestEnabled  OptInRequestStatus = "ENABLED"
)

type SupportedOptInRegions string

const (
	CapeTownRegion  SupportedOptInRegions = "af-south-1"
	MelbourneRegion SupportedOptInRegions = "ap-southeast-4"
	HyderabadRegion SupportedOptInRegions = "ap-south-2"
	MilanRegion     SupportedOptInRegions = "eu-south-1"
	ZurichRegion    SupportedOptInRegions = "eu-central-2"
	HongKongRegion  SupportedOptInRegions = "ap-east-1"
	UAERegion       SupportedOptInRegions = "me-central-1"
	SpainRegion     SupportedOptInRegions = "eu-south-2"
	BahrainRegion   SupportedOptInRegions = "me-south-1"
	JakartaRegion   SupportedOptInRegions = "ap-southeast-3"
)

// AccountStatus defines the observed state of Account
// +k8s:openapi-gen=true
type AccountStatus struct {
	Claimed       bool   `json:"claimed,omitempty"`
	SupportCaseID string `json:"supportCaseID,omitempty"`
	// +optional
	Conditions               []AccountCondition    `json:"conditions,omitempty"`
	State                    string                `json:"state,omitempty"`
	RotateCredentials        bool                  `json:"rotateCredentials,omitempty"`
	RotateConsoleCredentials bool                  `json:"rotateConsoleCredentials,omitempty"`
	Reused                   bool                  `json:"reused,omitempty"`
	RegionalServiceQuotas    RegionalServiceQuotas `json:"regionalServiceQuotas,omitempty"`
	OptInRegions             OptInRegions          `json:"optInRegions,omitempty"`
}

// AccountCondition contains details for the current condition of a AWS account
// +k8s:openapi-gen=true
type AccountCondition struct {
	// Type is the type of the condition.
	// +optional
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
	// FIXME: Have to call this different than "AccountClaimed", as that clashes
	// with the AccountClaimConditionType
	AccountIsClaimed AccountConditionType = "Claimed"
	// AccountReused is set when account is reused
	AccountReused AccountConditionType = "Reused"
	// AccountClientError is set when there was an issue getting a client
	AccountClientError AccountConditionType = "AccountClientError"
	// AccountAuthorizationError indicates an authorization error occurred
	AccountAuthorizationError AccountConditionType = "AuthorizationError"
	// AccountAuthenticationError indicates an authentication error occurred
	AccountAuthenticationError AccountConditionType = "AuthenticationError"
	// AccountUnhandledError indicates a error that isn't handled, probably a go error
	AccountUnhandledError AccountConditionType = "UnhandledError"
	// AccountInternalError is set when a serious internal issue arrises
	AccountInternalError AccountConditionType = "InternalError"
	// AccountInitializingRegions indicates we've kicked off the process of creating and terminating
	// instances in all supported regions
	AccountInitializingRegions = "InitializingRegions"
	// AccountOptingInRegions indicates region enablement for supported Opt-In regions is in progress
	AccountOptingInRegions AccountConditionType = "OptingInRegions"
	// AccountOptInRegionEnabled indicates that supported Opt-In regions have been enabled
	AccountOptInRegionEnabled AccountConditionType = "OptInRegionsEnabled"
)

// +genclient
// +kubebuilder:object:root=true

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

// +kubebuilder:object:root=true

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

// IsFailed returns true if an account is in a failed state
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

// HasState returns true if an account has a state set at all
func (a *Account) HasState() bool {
	return a.Status.State != ""
}

// HasSupportCaseID returns true if an account has a SupportCaseID Set
func (a *Account) HasSupportCaseID() bool {
	return a.Status.SupportCaseID != ""
}

// HasOpenOptInRegionRequests returns true if an account has any supported regions have not been enabled
func (a *Account) HasOpenOptInRegionRequests() bool {
	for _, region := range a.Status.OptInRegions {
		if region.Status != OptInRequestEnabled {
			return true
		}
	}
	return false
}

func (a *Account) GetOptInRequestsByStatus(stati OptInRequestStatus) (int, OptInRegions) {
	var returnRegionalOptInRequest = make(OptInRegions)
	var count = 0
	for region, optInRegionStatus := range a.Status.OptInRegions {
		if optInRegionStatus.Status == stati {
			_, ok := returnRegionalOptInRequest[region]
			if !ok {
				returnRegionalOptInRequest[region] = &OptInRegionStatus{
					RegionCode: optInRegionStatus.RegionCode,
					Status:     optInRegionStatus.Status,
				}
			} else {
				returnRegionalOptInRequest[region].Status = optInRegionStatus.Status
			}
			count++
		}
	}
	return count, returnRegionalOptInRequest
}

// HasOpenQuotaIncreaseRequests returns true if an account has any open quota increase requests
func (a *Account) HasOpenQuotaIncreaseRequests() bool {
	for _, accountServiceQuotas := range a.Status.RegionalServiceQuotas {
		for _, v := range accountServiceQuotas {
			if v.Status != ServiceRequestCompleted {
				return true
			}
		}
	}
	return false
}

func (a *Account) GetQuotaRequestsByStatus(stati ...ServiceRequestStatus) (int, RegionalServiceQuotas) {
	// var returnRegionalServiceQuotaRequest RegionalServiceQuotas
	var returnRegionalServiceQuotaRequest = make(RegionalServiceQuotas)
	var count = 0
	for region, accountServiceQuota := range a.Status.RegionalServiceQuotas {
		for quotaCode, v := range accountServiceQuota {
			for _, status := range stati {
				if v.Status == status {
					_, ok := returnRegionalServiceQuotaRequest[region]
					if !ok {
						returnRegionalServiceQuotaRequest[region] = make(AccountServiceQuota)
						returnRegionalServiceQuotaRequest[region][quotaCode] = accountServiceQuota[quotaCode]
					} else {
						returnRegionalServiceQuotaRequest[region][quotaCode] = accountServiceQuota[quotaCode]
					}
					count++
				}
			}
		}
	}
	return count, returnRegionalServiceQuotaRequest
}

// IsReusedAccountMissingIAMUser returns true if the account is in a ready state and a reused non-byoc account without a IAMUser secret and claimlink
func (a *Account) IsReusedAccountMissingIAMUser() bool {
	return a.IsReady() && a.Status.Reused && a.Spec.IAMUserSecret == "" && !a.IsBYOC() && !a.HasClaimLink() && !a.IsSTS()
}

// IsPendingVerification returns true if the account is in a PendingVerification state
func (a *Account) IsPendingVerification() bool {
	return a.Status.State == string(AccountPendingVerification)
}

// IsOptingInRegions returns true if an account is in a OptingInRegions state
func (a *Account) IsOptingInRegions() bool {
	return a.Status.State == string(AccountOptingInRegions)
}

// HasOptedInRegions returns true if an account is in a OptInRegionsEnabled state
func (a *Account) HasOptedInRegions() bool {
	return a.Status.State == string(AccountOptInRegionEnabled)
}

// IsReady returns true if an account is ready
func (a *Account) IsReady() bool {
	return a.Status.State == string(AccountReady)
}

// IsCreating returns true if an account is creating
func (a *Account) IsCreating() bool {
	return a.Status.State == string(AccountCreating)
}

// HasClaimLink returns true if an accounts claim link is not empty
func (a *Account) HasClaimLink() bool {
	return a.Spec.ClaimLink != ""
}

// IsClaimed returns true if account Status.Claimed is false
func (a *Account) IsClaimed() bool {
	return a.Status.Claimed
}

// IsPendingDeletion returns true if a DeletionTimestamp has been set
func (a *Account) IsPendingDeletion() bool {
	return a.DeletionTimestamp != nil
}

// IsBYOC returns true if account is a BYOC account
func (a *Account) IsBYOC() bool {
	return a.Spec.BYOC
}

// HasAwsAccountID returns true if awsAccountID is set
func (a *Account) HasAwsAccountID() bool {
	return a.Spec.AwsAccountID != ""
}

// IsReadyUnclaimedAndHasClaimLink returns true if an account is ready, unclaimed, and has a claim link
func (a *Account) IsReadyUnclaimedAndHasClaimLink() bool {
	return a.IsReady() &&
		a.HasClaimLink() &&
		!a.IsClaimed()
}

// HasAwsv1alpha1Finalizer returns true if the awsv1alpha1 finalizer is set on the account
func (a *Account) HasAwsv1alpha1Finalizer() bool {
	for _, v := range a.GetFinalizers() {
		if v == AccountFinalizer {
			return true
		}
	}
	return false
}

func (a *Account) IsSTS() bool {
	return a.Spec.ManualSTSMode
}

func (a *Account) IsNonSTSPendingDeletionWithFinalizer() bool {
	return a.IsPendingDeletion() &&
		!a.IsSTS() &&
		a.HasAwsv1alpha1Finalizer()
}

// IsBYOCPendingDeletionWithFinalizer returns true if account is a BYOC Account,
// has been marked for deletion (deletion timestamp set), and has a finalizer set.
func (a *Account) IsBYOCPendingDeletionWithFinalizer() bool {
	return a.IsPendingDeletion() &&
		a.IsBYOC() &&
		a.HasAwsv1alpha1Finalizer()
}

// IsBYOCAndNotReady returns true if account is BYOC and the state is not AccountReady
func (a *Account) IsBYOCAndNotReady() bool {
	return a.IsBYOC() && !a.IsReady()
}

// ReadyForInitialization returns true if account is a BYOC Account and the state is not ready OR
// accout state is creating, and has not been claimed
func (a *Account) ReadyForInitialization() bool {
	return a.IsBYOCAndNotReady() ||
		a.IsUnclaimedAndIsCreating() ||
		a.IsUnclaimedAndIsOptingInRegion() ||
		a.IsUnclaimedAndHasOptedInRegion()
}

// IsUnclaimedAndHasNoState returns true if account has not set state and has not been claimed
func (a *Account) IsUnclaimedAndHasNoState() bool {
	return !a.HasState() &&
		!a.IsClaimed()
}

// IsUnclaimedAndIsOptingInRegion returns true if account state is OptingInRegions and has not been claimed
func (a *Account) IsUnclaimedAndIsOptingInRegion() bool {
	return a.IsOptingInRegions() &&
		!a.IsClaimed()
}

// IsUnclaimedAndHasOptedInRegion returns true if account state is OptInRegionsEnabled and has not been claimed
func (a *Account) IsUnclaimedAndHasOptedInRegion() bool {
	return a.HasOptedInRegions() &&
		!a.IsClaimed()
}

// IsUnclaimedAndIsCreating returns true if account state is AccountCreating and has not been claimed
func (a *Account) IsUnclaimedAndIsCreating() bool {
	return a.IsCreating() &&
		!a.IsClaimed()
}

// IsInitializingRegions returns true if the account state is InitalizingRegions
func (a *Account) IsInitializingRegions() bool {
	return a.Status.State == AccountInitializingRegions
}

// IsProgressing returns true if the account state is Creating, Pending Verification, or InitializingRegions
func (a *Account) IsProgressing() bool {
	if a.Status.State == string(AccountCreating) ||
		a.Status.State == string(AccountPendingVerification) ||
		a.Status.State == string(AccountInitializingRegions) {
		return true
	}
	return false
}

// HasBeenClaimed lets us know if an account has been claimed at some point and can only be reused by clusters in the same legal entity
func (a *Account) HasBeenClaimedAtLeastOnce() bool {
	return a.Spec.LegalEntity.ID != "" || a.Status.Reused
}

// HasNeverBeenClaimed returns true if the account is not claimed AND has no legalEntity set, meaning it hasn't been claimed before and is not available for reuse
func (a *Account) HasNeverBeenClaimed() bool {
	return !a.Status.Claimed && a.Spec.LegalEntity.ID == ""
}

// IsOwnedByAccountPool returns true if the account has an ownerreference type that is the accountpool or if the accountpool is defined in the account spec
func (a *Account) IsOwnedByAccountPool() bool {
	if a.ObjectMeta.OwnerReferences == nil {
		if a.Spec.AccountPool != "" {
			return true
		}
		return false
	}
	for _, ref := range a.ObjectMeta.OwnerReferences {
		if ref.Kind == "AccountPool" {
			return true
		}
	}
	return false
}

func (a *Account) GetAssumeRole() string {
	// If the account is a CCS account, return the ManagedOpenShiftSupport role
	if a.IsBYOC() {
		return fmt.Sprintf("%s-%s", ManagedOpenShiftSupportRole, a.Labels[IAMUserIDLabel])
	}
	// Else return the default role
	return AccountOperatorIAMRole
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
