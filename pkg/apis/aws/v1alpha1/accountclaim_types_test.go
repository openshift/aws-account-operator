package v1alpha1

import (
	"testing"
)

func Test_AccountClaim_validateAWS(t *testing.T) {
	var accountClaim AccountClaim
	tests := []struct {
		name         string
		awsSecretRef SecretRef
		expectedErr  error
	}{
		{
			name:         "ValidateAWS_Empty_Fail",
			awsSecretRef: SecretRef{Name: "", Namespace: ""},
			expectedErr:  ErrAWSSecretRefMissing,
		},
		{
			name:         "ValidateAWS_NameMissing_Fail",
			awsSecretRef: SecretRef{Name: "", Namespace: "awsSecretRefNamespace"},
			expectedErr:  ErrAWSSecretRefMissing,
		},
		{
			name:         "ValidateAWS_NamespaceMissing_Fail",
			awsSecretRef: SecretRef{Name: "awsSecretRefName", Namespace: ""},
			expectedErr:  ErrAWSSecretRefMissing,
		},
		{
			name:         "ValidateAWS_SetOK_Success",
			awsSecretRef: SecretRef{Name: "awsSecretRefName", Namespace: "awsSecretRefNamespace"},
			expectedErr:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			accountClaim.Spec.AwsCredentialSecret = tt.awsSecretRef
			if validateErr := accountClaim.validateAWS(); validateErr != tt.expectedErr {
				t.Errorf("[AccountClaim.ValidateAWS()] Got %v, wanted %v", validateErr, tt.expectedErr)
			}
		})
	}
}

func Test_AccountClaim_Validate(t *testing.T) {

	var accountClaim AccountClaim

	tests := []struct {
		name             string
		awsSecretRef     SecretRef
		manualSTSMode    bool
		stsRoleARN       string
		byocAWSAccountID string
		byocSecretRef    SecretRef
		expectedErr      error
	}{
		{
			name:             "Validate_manualSTS_OK",
			awsSecretRef:     SecretRef{Name: "awsSecretRefName", Namespace: "awsSecretRefNamespace"},
			manualSTSMode:    true,
			stsRoleARN:       "stsRoleARN",
			byocAWSAccountID: "",
			byocSecretRef:    SecretRef{Name: "", Namespace: ""},
			expectedErr:      nil,
		},
		{
			name:             "Validate_ErrAWSSecretRefMissing_Name",
			awsSecretRef:     SecretRef{Name: "", Namespace: "awsSecretRefNamespace"},
			manualSTSMode:    true,
			stsRoleARN:       "stsRoleARN",
			byocAWSAccountID: "",
			byocSecretRef:    SecretRef{Name: "", Namespace: ""},
			expectedErr:      ErrAWSSecretRefMissing,
		},
		{
			name:             "Validate_ErrAWSSecretRefMissing_Namespace",
			awsSecretRef:     SecretRef{Name: "awsSecretRefName", Namespace: ""},
			manualSTSMode:    true,
			stsRoleARN:       "stsRoleARN",
			byocAWSAccountID: "",
			byocSecretRef:    SecretRef{Name: "", Namespace: ""},
			expectedErr:      ErrAWSSecretRefMissing,
		},
		{
			name:             "Validate_ErrSTSRoleARNMissing",
			awsSecretRef:     SecretRef{Name: "awsSecretRefName", Namespace: "awsSecretRefNamespace"},
			manualSTSMode:    true,
			stsRoleARN:       "",
			byocAWSAccountID: "",
			byocSecretRef:    SecretRef{Name: "", Namespace: ""},
			expectedErr:      ErrSTSRoleARNMissing,
		},
		{
			name:             "Validate_BYOC_OK",
			awsSecretRef:     SecretRef{Name: "awsSecretRefName", Namespace: "awsSecretRefNamespace"},
			manualSTSMode:    false,
			stsRoleARN:       "stsRoleARN",
			byocAWSAccountID: "byocAWSAccountID",
			byocSecretRef:    SecretRef{Name: "byocSecretRefName", Namespace: "byocSecretRefNamespace"},
			expectedErr:      nil,
		},
		{
			name:             "Validate_ErrBYOCAccountIDMissing",
			awsSecretRef:     SecretRef{Name: "awsSecretRefName", Namespace: "awsSecretRefNamespace"},
			manualSTSMode:    false,
			stsRoleARN:       "",
			byocAWSAccountID: "",
			byocSecretRef:    SecretRef{Name: "", Namespace: ""},
			expectedErr:      ErrBYOCAccountIDMissing,
		},
		{
			name:             "Validate_ErrBYOCSecretRefMissing_Name",
			awsSecretRef:     SecretRef{Name: "awsSecretRefName", Namespace: "awsSecretRefNamespace"},
			manualSTSMode:    false,
			stsRoleARN:       "",
			byocAWSAccountID: "byocAWSAccountID",
			byocSecretRef:    SecretRef{Name: "", Namespace: "byocSecretRefNamespace"},
			expectedErr:      ErrBYOCSecretRefMissing,
		},
		{
			name:             "Validate_ErrBYOCSecretRefMissing_Namespace",
			awsSecretRef:     SecretRef{Name: "awsSecretRefName", Namespace: "awsSecretRefNamespace"},
			manualSTSMode:    false,
			stsRoleARN:       "",
			byocAWSAccountID: "byocAWSAccountID",
			byocSecretRef:    SecretRef{Name: "byocSecretRefName", Namespace: ""},
			expectedErr:      ErrBYOCSecretRefMissing,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			accountClaim.Spec.AwsCredentialSecret = tt.awsSecretRef
			accountClaim.Spec.ManualSTSMode = tt.manualSTSMode
			accountClaim.Spec.STSRoleARN = tt.stsRoleARN
			accountClaim.Spec.BYOCAWSAccountID = tt.byocAWSAccountID
			accountClaim.Spec.BYOCSecretRef = tt.byocSecretRef
			if validateErr := accountClaim.Validate(); validateErr != tt.expectedErr {
				t.Errorf("[AccountClaim.Validate()] Got %v, wanted %v", validateErr, tt.expectedErr)
			}
		})
	}
}
