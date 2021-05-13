package v1alpha1

import (
	"testing"
)

func TestAccountIsFailed(t *testing.T) {
	// test all AccountConditionType values

	var a Account
	tests := []struct {
		name                 string
		accountConditionType string
		expectedResult       bool
	}{
		{
			name:                 "AccountCreating",
			accountConditionType: string(AccountCreating),
			expectedResult:       false,
		},
		{
			name:                 "AccountReady",
			accountConditionType: string(AccountReady),
			expectedResult:       false,
		},
		{
			name:                 "AccountFailed",
			accountConditionType: string(AccountFailed),
			expectedResult:       true,
		},
		{
			name:                 "AccountCreationFailed",
			accountConditionType: string(AccountCreationFailed),
			expectedResult:       true,
		},
		{
			name:                 "AccountPending",
			accountConditionType: string(AccountPending),
			expectedResult:       false,
		},
		{
			name:                 "AccountPendingVerification",
			accountConditionType: string(AccountPendingVerification),
			expectedResult:       false,
		},
		{
			name:                 "AccountReused",
			accountConditionType: string(AccountReused),
			expectedResult:       false,
		},
		{
			name:                 "AccountClientError",
			accountConditionType: string(AccountClientError),
			expectedResult:       true,
		},
		{
			name:                 "AccountAuthorizationError",
			accountConditionType: string(AccountAuthorizationError),
			expectedResult:       true,
		},
		{
			name:                 "AccountAuthenticationError",
			accountConditionType: string(AccountAuthenticationError),
			expectedResult:       true,
		},
		{
			name:                 "AccountUnhandledError",
			accountConditionType: string(AccountUnhandledError),
			expectedResult:       true,
		},
		{
			name:                 "AccountInternalError",
			accountConditionType: string(AccountInternalError),
			expectedResult:       true,
		},
		{
			name:                 "AccountInitializingRegions",
			accountConditionType: string(AccountInitializingRegions),
			expectedResult:       false,
		},
		{
			name:                 "AccountQuotaIncreaseRequested",
			accountConditionType: string(AccountQuotaIncreaseRequested),
			expectedResult:       false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a.Status.State = tt.accountConditionType
			if got := a.IsFailed(); got != tt.expectedResult {
				t.Errorf("[Account.IsFailed()] Got %v, want %v for state %s", got, tt.expectedResult, tt.accountConditionType)
			}
		})
	}
}

func TestAccountHasState(t *testing.T) {
	// test all AccountConditionType values

	var a Account
	tests := []struct {
		name                 string
		accountConditionType string
		expectedResult       bool
	}{
		{
			name:                 "StateReady",
			accountConditionType: string(AccountReady),
			expectedResult:       true,
		},
		{
			name:                 "StateFailed",
			accountConditionType: string(AccountFailed),
			expectedResult:       true,
		},
		{
			name:                 "StateEmpty",
			accountConditionType: "",
			expectedResult:       false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a.Status.State = tt.accountConditionType
			if got := a.HasState(); got != tt.expectedResult {
				t.Errorf("[Account.HasState()] Got %v, want %v for state %s", got, tt.expectedResult, tt.accountConditionType)
			}
		})
	}
}

func TestAccountHasSupportCaseID(t *testing.T) {
	// test all AccountConditionType values

	var a Account
	tests := []struct {
		name           string
		supportCaseID  string
		expectedResult bool
	}{
		{
			name:           "SetSupportCaseID",
			supportCaseID:  "SupportedCaseID",
			expectedResult: true,
		},
		{
			name:           "UnsetSupportCaseID",
			supportCaseID:  "",
			expectedResult: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a.Status.SupportCaseID = tt.supportCaseID
			if got := a.HasSupportCaseID(); got != tt.expectedResult {
				t.Errorf("[Account.HasSupportCaseID()] Got %v, want %v for state %s", got, tt.expectedResult, tt.supportCaseID)
			}
		})
	}
}

func TestAccountIsPendingReadyCreation(t *testing.T) {
	// Unit test for the following Account Functions
	//// IsPendingVerification
	//// IsReady
	//// IsCreating

	var a Account
	tests := []struct {
		name                  string
		accountConditionType  string
		verifyAccountFunction func() bool
		expectedResult        bool
	}{
		// Pending Verification tests
		{
			name:                  "AccountPendingVerificationTrue",
			accountConditionType:  string(AccountPendingVerification),
			verifyAccountFunction: a.IsPendingVerification,
			expectedResult:        true,
		},
		{
			name:                  "AccountPendingVerificationWrongState",
			accountConditionType:  string(AccountClaimed),
			verifyAccountFunction: a.IsPendingVerification,
			expectedResult:        false,
		},
		{
			name:                  "AccountPendingVerificationEmptyString",
			accountConditionType:  "",
			verifyAccountFunction: a.IsPendingVerification,
			expectedResult:        false,
		},
		// Is Ready tests
		{
			name:                  "AccountReadyTrue",
			accountConditionType:  string(AccountReady),
			verifyAccountFunction: a.IsReady,
			expectedResult:        true,
		},
		{
			name:                  "AccountReadyWrongState",
			accountConditionType:  string(AccountClaimed),
			verifyAccountFunction: a.IsReady,
			expectedResult:        false,
		},
		{
			name:                  "AccountReadyEmptyString",
			accountConditionType:  "",
			verifyAccountFunction: a.IsReady,
			expectedResult:        false,
		},
		// Is Creating tests
		{
			name:                  "AccountCreatingTrue",
			accountConditionType:  string(AccountCreating),
			verifyAccountFunction: a.IsCreating,
			expectedResult:        true,
		},
		{
			name:                  "AccountCreatingWrongState",
			accountConditionType:  string(AccountClaimed),
			verifyAccountFunction: a.IsCreating,
			expectedResult:        false,
		},
		{
			name:                  "AccountCreatingEmptyString",
			accountConditionType:  "",
			verifyAccountFunction: a.IsCreating,
			expectedResult:        false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a.Status.State = tt.accountConditionType
			if got := tt.verifyAccountFunction(); got != tt.expectedResult {
				t.Errorf("[Account] Got %v, want %v for state %s", got, tt.expectedResult, tt.accountConditionType)
			}
		})
	}
}

func TestIsReadyUnclaimedAndHasClaimLink(t *testing.T) {

	var a Account
	tests := []struct {
		name                 string
		accountConditionType string
		claimLink            string
		isClaimed            bool
		expectedResult       bool
	}{
		// Pending Verification tests
		{
			name:                 "ReadyUnclaimedClaimLinkFilled",
			accountConditionType: string(AccountReady),
			claimLink:            "Linked",
			isClaimed:            false,
			expectedResult:       true,
		},
		{
			name:                 "NotReadyUnclaimedClaimLinkFilled",
			accountConditionType: string(AccountClientError),
			claimLink:            "Linked",
			isClaimed:            false,
			expectedResult:       false,
		},
		{
			name:                 "ReadyClaimedClaimLinkFilled",
			accountConditionType: string(AccountReady),
			claimLink:            "Linked",
			isClaimed:            true,
			expectedResult:       false,
		},
		{
			name:                 "ReadyUnlaimedClaimLinkEmpty",
			accountConditionType: string(AccountReady),
			claimLink:            "",
			isClaimed:            false,
			expectedResult:       false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a.Status.State = tt.accountConditionType
			a.Spec.ClaimLink = tt.claimLink
			a.Status.Claimed = tt.isClaimed
			if got := a.IsReadyUnclaimedAndHasClaimLink(); got != tt.expectedResult {
				t.Errorf("[Account.IsReadyUnclaimedAndHasClaimLink] Got %v, want %v", got, tt.expectedResult)
			}
		})
	}
}
