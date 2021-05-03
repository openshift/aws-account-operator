package v1alpha1

import (
	"testing"
)

func Test_Accountclaim_Validate(t *testing.T) {

	var accountClaim AccountClaim

	tests := []struct {
		name                   string
		manualSTSMode          bool
		stsRoleARN             string
		byocAWSAccountID       string
		byocSecretRefName      string
		byocSecretRefNamespace string
		expectedErr            error
	}{
		{
			name:                   "Validate_manualSTS_OK",
			manualSTSMode:          true,
			stsRoleARN:             "stsRoleARN",
			byocSecretRefName:      "",
			byocSecretRefNamespace: "",
			expectedErr:            nil,
		},
		{
			name:                   "Validate_BYOC_OK",
			manualSTSMode:          true,
			stsRoleARN:             "stsRoleARN",
			byocAWSAccountID:       "byocAWSAccountID",
			byocSecretRefName:      "byocSecretRefName",
			byocSecretRefNamespace: "byocSecretRefNamespace",
			expectedErr:            nil,
		},
		{
			name:                   "Validate_ErrSTSRoleARNMissing",
			manualSTSMode:          true,
			stsRoleARN:             "",
			byocSecretRefName:      "",
			byocSecretRefNamespace: "",
			expectedErr:            ErrSTSRoleARNMissing,
		},
		{
			name:                   "Validate_ErrBYOCAccountIDMissing",
			manualSTSMode:          false,
			stsRoleARN:             "",
			byocAWSAccountID:       "",
			byocSecretRefName:      "",
			byocSecretRefNamespace: "",
			expectedErr:            ErrBYOCAccountIDMissing,
		},
		{
			name:                   "Validate_ErrBYOCSecretRefMissing_Name",
			manualSTSMode:          false,
			stsRoleARN:             "",
			byocAWSAccountID:       "byocAWSAccountID",
			byocSecretRefName:      "",
			byocSecretRefNamespace: "byocSecretRefNamespace",
			expectedErr:            ErrBYOCSecretRefMissing,
		},
		{
			name:                   "Validate_ErrBYOCSecretRefMissing_Namespace",
			manualSTSMode:          false,
			stsRoleARN:             "",
			byocAWSAccountID:       "byocAWSAccountID",
			byocSecretRefName:      "byocSecretRefName",
			byocSecretRefNamespace: "",
			expectedErr:            ErrBYOCSecretRefMissing,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			accountClaim.Spec.ManualSTSMode = tt.manualSTSMode
			accountClaim.Spec.STSRoleARN = tt.stsRoleARN
			accountClaim.Spec.BYOCAWSAccountID = tt.byocAWSAccountID
			accountClaim.Spec.BYOCSecretRef.Name = tt.byocSecretRefName
			accountClaim.Spec.BYOCSecretRef.Namespace = tt.byocSecretRefNamespace

			if validateErr := accountClaim.Validate(); validateErr != tt.expectedErr {
				t.Errorf("[AccountClaim.Validate()] Got %v, wanted %v", validateErr, tt.expectedErr)
			}
		})
	}
}
