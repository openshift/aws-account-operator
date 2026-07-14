package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAWSFederatedAccountAccessValidate(t *testing.T) {
	tests := []struct {
		name        string
		faa         *AWSFederatedAccountAccess
		expectedErr error
	}{
		{
			name: "matching namespace passes",
			faa: &AWSFederatedAccountAccess{
				ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-ns"},
				Spec: AWSFederatedAccountAccessSpec{
					AWSCustomerCredentialSecret: AWSSecretReference{
						Name:      "secret",
						Namespace: "tenant-ns",
					},
				},
			},
			expectedErr: nil,
		},
		{
			name: "mismatching namespace fails",
			faa: &AWSFederatedAccountAccess{
				ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-ns"},
				Spec: AWSFederatedAccountAccessSpec{
					AWSCustomerCredentialSecret: AWSSecretReference{
						Name:      "secret",
						Namespace: "aws-account-operator",
					},
				},
			},
			expectedErr: ErrAWSCustomerCredentialSecretNamespaceMismatch,
		},
		{
			name: "empty namespace passes",
			faa: &AWSFederatedAccountAccess{
				ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-ns"},
				Spec: AWSFederatedAccountAccessSpec{
					AWSCustomerCredentialSecret: AWSSecretReference{
						Name:      "secret",
						Namespace: "",
					},
				},
			},
			expectedErr: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.faa.Validate()
			if err != test.expectedErr {
				t.Errorf("got %v, wanted %v", err, test.expectedErr)
			}
		})
	}
}
