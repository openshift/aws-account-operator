package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
	AccountInitializingRegions AccountConditionType = "InitializingRegions"
	// AccountQuotaIncreaseRequested is set when a quota increase has been requested
	AccountQuotaIncreaseRequested AccountConditionType = "QuotaIncreaseRequested"
)

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

const MaximumAccountConditionTypes = 14

func getAllAccountConditionTypes() [MaximumAccountConditionTypes]AccountConditionType {
	return [MaximumAccountConditionTypes]AccountConditionType{
		AccountCreating,
		AccountReady,
		AccountFailed,
		AccountCreationFailed,
		AccountPending,
		AccountPendingVerification,
		AccountReused,
		AccountClientError,
		AccountAuthorizationError,
		AccountAuthenticationError,
		AccountUnhandledError,
		AccountInternalError,
		AccountQuotaIncreaseRequested,
		AccountInitializingRegions,
	}
}

func IsValidAccountConditionType(conditionType AccountConditionType) bool {
	for _, ct := range getAllAccountConditionTypes() {
		if ct == conditionType {
			return true
		}
	}
	return false
}
