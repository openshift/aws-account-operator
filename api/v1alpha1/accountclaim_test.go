package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
				ObjectMeta: metav1.ObjectMeta{Namespace: "test"},
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
			name: "Testing CCS BYOCSecretRef namespace mismatch",
			accountClaim: &AccountClaim{
				ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-ns"},
				Spec: AccountClaimSpec{
					BYOC:             true,
					BYOCAWSAccountID: "123456789",
					BYOCSecretRef: SecretRef{
						Name:      "testBYOC",
						Namespace: "aws-account-operator",
					},
					AwsCredentialSecret: SecretRef{
						Name:      "testAWS",
						Namespace: "tenant-ns",
					},
				},
			},
			expectedErr: ErrBYOCSecretRefNamespaceMismatch,
		},
		{
			name: "Testing CCS AwsCredentialSecret namespace mismatch",
			accountClaim: &AccountClaim{
				ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-ns"},
				Spec: AccountClaimSpec{
					BYOC:             true,
					BYOCAWSAccountID: "123456789",
					BYOCSecretRef: SecretRef{
						Name:      "testBYOC",
						Namespace: "tenant-ns",
					},
					AwsCredentialSecret: SecretRef{
						Name:      "testAWS",
						Namespace: "different-ns",
					},
				},
			},
			expectedErr: ErrAWSSecretRefNamespaceMismatch,
		},
		{
			name: "Testing non-CCS AwsCredentialSecret namespace mismatch",
			accountClaim: &AccountClaim{
				ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-ns"},
				Spec: AccountClaimSpec{
					AwsCredentialSecret: SecretRef{
						Name:      "testAWS",
						Namespace: "aws-account-operator",
					},
				},
			},
			expectedErr: ErrAWSSecretRefNamespaceMismatch,
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
