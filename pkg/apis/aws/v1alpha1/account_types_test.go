package v1alpha1

import (
	"testing"
)

func Test_Account_IsFailed(t *testing.T) {
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

func Test_Account_HasState(t *testing.T) {
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

func Test_Account_HasSupportCaseID(t *testing.T) {
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

func Test_Account_IsPendingReadyCreation(t *testing.T) {
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
			name:                  "AccountPendingVerification_true",
			accountConditionType:  string(AccountPendingVerification),
			verifyAccountFunction: a.IsPendingVerification,
			expectedResult:        true,
		},
		{
			name:                  "AccountPendingVerification_wrongstate",
			accountConditionType:  string(AccountClaimed),
			verifyAccountFunction: a.IsPendingVerification,
			expectedResult:        false,
		},
		{
			name:                  "AccountPendingVerification_emptystring",
			accountConditionType:  "",
			verifyAccountFunction: a.IsPendingVerification,
			expectedResult:        false,
		},
		// Is Ready tests
		{
			name:                  "AccountReady_true",
			accountConditionType:  string(AccountReady),
			verifyAccountFunction: a.IsReady,
			expectedResult:        true,
		},
		{
			name:                  "AccountReady_wrongstate",
			accountConditionType:  string(AccountClaimed),
			verifyAccountFunction: a.IsReady,
			expectedResult:        false,
		},
		{
			name:                  "AccountReady_emptystring",
			accountConditionType:  "",
			verifyAccountFunction: a.IsReady,
			expectedResult:        false,
		},
		// Is Creating tests
		{
			name:                  "AccountCreating_true",
			accountConditionType:  string(AccountCreating),
			verifyAccountFunction: a.IsCreating,
			expectedResult:        true,
		},
		{
			name:                  "AccountCreating_wrongstate",
			accountConditionType:  string(AccountClaimed),
			verifyAccountFunction: a.IsCreating,
			expectedResult:        false,
		},
		{
			name:                  "AccountCreating_emptystring",
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

func Test_IsReadyUnclaimedAndHasClaimLink(t *testing.T) {

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
			name:                 "Ready_Unclaimed_ClaimLinkFilled",
			accountConditionType: string(AccountReady),
			claimLink:            "Linked",
			isClaimed:            false,
			expectedResult:       true,
		},
		{
			name:                 "NotReady_Unclaimed_ClaimLinkFilled",
			accountConditionType: string(AccountClientError),
			claimLink:            "Linked",
			isClaimed:            false,
			expectedResult:       false,
		},
		{
			name:                 "Ready_Claimed_ClaimLinkFilled",
			accountConditionType: string(AccountReady),
			claimLink:            "Linked",
			isClaimed:            true,
			expectedResult:       false,
		},
		{
			name:                 "Ready_Unlaimed_ClaimLinkEmpty",
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
