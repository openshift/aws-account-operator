package v1alpha1

import (
	"testing"
)

func TestAccountClaimValidateAWS(t *testing.T) {
	var accountClaim AccountClaim
	tests := []struct {
		name         string
		awsSecretRef SecretRef
		expectedErr  error
	}{
		{
			name:         "ValidateAWSEmptyFail",
			awsSecretRef: SecretRef{Name: "", Namespace: ""},
			expectedErr:  ErrAWSSecretRefMissing,
		},
		{
			name:         "ValidateAWSNameMissingFail",
			awsSecretRef: SecretRef{Name: "", Namespace: "awsSecretRefNamespace"},
			expectedErr:  ErrAWSSecretRefMissing,
		},
		{
			name:         "ValidateAWSNamespaceMissingFail",
			awsSecretRef: SecretRef{Name: "awsSecretRefName", Namespace: ""},
			expectedErr:  ErrAWSSecretRefMissing,
		},
		{
			name:         "ValidateAWSSetOKSuccess",
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

func TestAccountClaimValidate(t *testing.T) {

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
			name:             "ValidatemanualSTSOK",
			awsSecretRef:     SecretRef{Name: "awsSecretRefName", Namespace: "awsSecretRefNamespace"},
			manualSTSMode:    true,
			stsRoleARN:       "stsRoleARN",
			byocAWSAccountID: "",
			byocSecretRef:    SecretRef{Name: "", Namespace: ""},
			expectedErr:      nil,
		},
		{
			name:             "ValidateErrAWSSecretRefMissingName",
			awsSecretRef:     SecretRef{Name: "", Namespace: "awsSecretRefNamespace"},
			manualSTSMode:    true,
			stsRoleARN:       "stsRoleARN",
			byocAWSAccountID: "",
			byocSecretRef:    SecretRef{Name: "", Namespace: ""},
			expectedErr:      ErrAWSSecretRefMissing,
		},
		{
			name:             "ValidateErrAWSSecretRefMissingNamespace",
			awsSecretRef:     SecretRef{Name: "awsSecretRefName", Namespace: ""},
			manualSTSMode:    true,
			stsRoleARN:       "stsRoleARN",
			byocAWSAccountID: "",
			byocSecretRef:    SecretRef{Name: "", Namespace: ""},
			expectedErr:      ErrAWSSecretRefMissing,
		},
		{
			name:             "ValidateErrSTSRoleARNMissing",
			awsSecretRef:     SecretRef{Name: "awsSecretRefName", Namespace: "awsSecretRefNamespace"},
			manualSTSMode:    true,
			stsRoleARN:       "",
			byocAWSAccountID: "",
			byocSecretRef:    SecretRef{Name: "", Namespace: ""},
			expectedErr:      ErrSTSRoleARNMissing,
		},
		{
			name:             "ValidateBYOCOK",
			awsSecretRef:     SecretRef{Name: "awsSecretRefName", Namespace: "awsSecretRefNamespace"},
			manualSTSMode:    false,
			stsRoleARN:       "stsRoleARN",
			byocAWSAccountID: "byocAWSAccountID",
			byocSecretRef:    SecretRef{Name: "byocSecretRefName", Namespace: "byocSecretRefNamespace"},
			expectedErr:      nil,
		},
		{
			name:             "ValidateErrBYOCAccountIDMissing",
			awsSecretRef:     SecretRef{Name: "awsSecretRefName", Namespace: "awsSecretRefNamespace"},
			manualSTSMode:    false,
			stsRoleARN:       "",
			byocAWSAccountID: "",
			byocSecretRef:    SecretRef{Name: "", Namespace: ""},
			expectedErr:      ErrBYOCAccountIDMissing,
		},
		{
			name:             "ValidateErrBYOCSecretRefMissingName",
			awsSecretRef:     SecretRef{Name: "awsSecretRefName", Namespace: "awsSecretRefNamespace"},
			manualSTSMode:    false,
			stsRoleARN:       "",
			byocAWSAccountID: "byocAWSAccountID",
			byocSecretRef:    SecretRef{Name: "", Namespace: "byocSecretRefNamespace"},
			expectedErr:      ErrBYOCSecretRefMissing,
		},
		{
			name:             "ValidateErrBYOCSecretRefMissingNamespace",
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
