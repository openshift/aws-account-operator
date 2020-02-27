package structs

import "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"

type testAccountClaim struct {
	ac v1alpha1.AccountClaim
}

// func (t *testSecretBuilder) GetTestSecret() *corev1.Secret {
// 	return &t.s
// }

func getValidateBYOCClaimMockSpec(id, name, namespace string) *v1alpha1.AccountClaim {

	tempSpec := v1alpha1.AccountClaimSpec{
		BYOCAWSAccountID: "ID",
		BYOCSecretRef: v1alpha1.SecretRef{
			Name:      "SecretName",
			Namespace: "Namespace",
		},
	}

	return &v1alpha1.AccountClaim{
		Spec:   tempSpec,
		Status: v1alpha1.AccountClaimStatus{},
	}
}

// NewTestAccountClaimBuilder builds a mock AccountClaim objec
func NewTestAccountClaimBuilder() *v1alpha1.AccountClaim {
	return &v1alpha1.AccountClaim{}
}
