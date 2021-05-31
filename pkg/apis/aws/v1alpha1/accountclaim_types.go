package v1alpha1

import (
	"errors"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AccountClaimSpec defines the desired state of AccountClaim
// +k8s:openapi-gen=true
type AccountClaimSpec struct {
	LegalEntity         LegalEntity `json:"legalEntity"`
	AwsCredentialSecret SecretRef   `json:"awsCredentialSecret"`
	Aws                 Aws         `json:"aws"`
	AccountLink         string      `json:"accountLink"`
	AccountOU           string      `json:"accountOU,omitempty"`
	BYOC                bool        `json:"byoc,omitempty"`
	BYOCSecretRef       SecretRef   `json:"byocSecretRef,omitempty"`
	BYOCAWSAccountID    string      `json:"byocAWSAccountID,omitempty"`
	ManualSTSMode       bool        `json:"manualSTSMode,omitempty"`
	STSRoleARN          string      `json:"stsRoleARN,omitempty"`
	STSExternalID       string      `json:"stsExternalID,omitempty"`
	SupportRoleARN      string      `json:"supportRoleARN,omitempty"`
	CustomTags          string      `json:"customTags,omitempty"`
}

// AccountClaimStatus defines the observed state of AccountClaim
// +k8s:openapi-gen=true
type AccountClaimStatus struct {
	// +listType=map
	// +listMapKey=type
	Conditions []AccountClaimCondition `json:"conditions"`

	State ClaimStatus `json:"state"`
}

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

// AccountClaimConditionType is a valid value for AccountClaimCondition.Type
type AccountClaimConditionType string

const (
	// AccountClaimed is set when an Account is claimed
	AccountClaimed AccountClaimConditionType = "Claimed"
	// CCSAccountClaimFailed is set when a CCS Account Fails
	CCSAccountClaimFailed AccountClaimConditionType = "CCSAccountClaimFailed"
	// AccountClaimFailed is set when a standard Account Fails
	AccountClaimFailed AccountClaimConditionType = "AccountClaimFailed"
	// AccountUnclaimed is set when an Account is not claimed
	AccountUnclaimed AccountClaimConditionType = "Unclaimed"
	// BYOCAWSAccountInUse is set when a BYOC AWS Account is in use
	BYOCAWSAccountInUse AccountClaimConditionType = "BYOCAWSAccountInUse"
	// ClientError is set when an Error regarding the client occured
	ClientError AccountClaimConditionType = "ClientError"
	// AuthenticationFailed is set when we get an AWS error from STS role assumption
	AuthenticationFailed AccountClaimConditionType = "AuthenticationFailed"
	// InvalidAccountClaim is set when the account claim CR is missing required values
	InvalidAccountClaim AccountClaimConditionType = "InvalidAccountClaim"
	// InternalError is set when a serious internal issue arrises
	InternalError AccountClaimConditionType = "InternalError"
)

// ClaimStatus is a valid value from AccountClaim.Status
type ClaimStatus string

const (
	// ClaimStatusPending pending status for a claim
	ClaimStatusPending ClaimStatus = "Pending"
	// ClaimStatusReady ready status for a claim
	ClaimStatusReady ClaimStatus = "Ready"
	// ClaimStatusError error status for a claim
	ClaimStatusError ClaimStatus = "Error"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AccountClaim is the Schema for the accountclaims API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.state",description="Status the account claim"
// +kubebuilder:printcolumn:name="Account",type="string",JSONPath=".spec.accountLink",description="Account CR link for the account claim"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age since the account claim was created"
// +kubebuilder:resource:path=accountclaims,scope=Namespaced
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
	Name string `json:"name"`
	ID   string `json:"id"`
}

// SecretRef contains the name of a secret and its namespace
type SecretRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// Aws struct contains specific AWS account configuration options
type Aws struct {
	Regions []AwsRegions `json:"regions"`
}

// AwsRegions struct contains specific AwsRegion information, at the moment its just
// name but in the future it will contain specific resource limits etc.
type AwsRegions struct {
	Name string `json:"name"`
}

func init() {
	SchemeBuilder.Register(&AccountClaim{}, &AccountClaimList{})
}

// ErrBYOCAccountIDMissing is an error for missing Account ID
var ErrBYOCAccountIDMissing = errors.New("BYOCAccountIDMissing")

// ErrBYOCSecretRefMissing is an error for missing Secret References
var ErrBYOCSecretRefMissing = errors.New("BYOCSecretRefMissing")

// ErrSTSRoleARNMissing is an error for missing STS Role ARN definition in the AccountClaim
var ErrSTSRoleARNMissing = errors.New("STSRoleARNMissing")

// Validates an AccountClaim object
func (a *AccountClaim) Validate() error {
	if a.Spec.ManualSTSMode {
		return a.validateSTS()
	}
	return a.validateBYOC()
}

func (a *AccountClaim) validateSTS() error {
	if a.Spec.STSRoleARN == "" {
		return ErrSTSRoleARNMissing
	}
	return nil
}

func (a *AccountClaim) validateBYOC() error {
	if a.Spec.BYOCAWSAccountID == "" {
		return ErrBYOCAccountIDMissing
	}
	if a.Spec.BYOCSecretRef.Name == "" || a.Spec.BYOCSecretRef.Namespace == "" {
		return ErrBYOCSecretRefMissing
	}

	return nil
}
