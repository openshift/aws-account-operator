package v1alpha1

import (
	"testing"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name         string
		accountClaim *AccountClaim
		expectedErr  error
	}{
		{
			name: "Testing CCS AccountID Missing",
			accountClaim: &AccountClaim{
				Spec: AccountClaimSpec{
					BYOC: true,
				},
			},
			expectedErr: ErrBYOCAccountIDMissing,
		},
		{
			name: "Testing CCS Secret Ref Missing",
			accountClaim: &AccountClaim{
				Spec: AccountClaimSpec{
					BYOC:             true,
					BYOCAWSAccountID: "123456789",
				},
			},
			expectedErr: ErrBYOCSecretRefMissing,
		},
		{
			name: "Testing CCS AWS Secret Ref Missing",
			accountClaim: &AccountClaim{
				Spec: AccountClaimSpec{
					BYOC:             true,
					BYOCAWSAccountID: "123456789",
					BYOCSecretRef: SecretRef{
						Name:      "testBYOC",
						Namespace: "test",
					},
				},
			},
			expectedErr: ErrAWSSecretRefMissing,
		},
		{
			name: "Testing Valid CCS",
			accountClaim: &AccountClaim{
				Spec: AccountClaimSpec{
					BYOC:             true,
					BYOCAWSAccountID: "123456789",
					BYOCSecretRef: SecretRef{
						Name:      "testBYOC",
						Namespace: "test",
					},
					AwsCredentialSecret: SecretRef{
						Name:      "testAWS",
						Namespace: "test",
					},
				},
			},
			expectedErr: nil,
		},
		{
			name: "Testing STS Missing RoleARN",
			accountClaim: &AccountClaim{
				Spec: AccountClaimSpec{
					ManualSTSMode: true,
				},
			},
			expectedErr: ErrSTSRoleARNMissing,
		},
		{
			name: "Testing STS Valid",
			accountClaim: &AccountClaim{
				Spec: AccountClaimSpec{
					ManualSTSMode: true,
					STSRoleARN:    "arn:aws:whatever:something:role/whomever",
				},
			},
			expectedErr: nil,
		},
		{
			name: "Testing non-ccs Valid",
			accountClaim: &AccountClaim{
				Spec: AccountClaimSpec{},
			},
			expectedErr: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.accountClaim.Validate()

			if err != test.expectedErr {
				t.Errorf("got %s, wanted %s", err, test.expectedErr)
			}
		})
	}
}
