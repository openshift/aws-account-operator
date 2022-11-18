package account

import (
	"fmt"

	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GenerateAccountCR returns new account CR struct
func GenerateAccountCR(namespace string) *awsv1alpha1.Account {

	uuid := utils.GenerateShortUID()

	accountName := GenerateAccountCRName(uuid)

	return &awsv1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			Name:      accountName,
			Namespace: namespace,
			Labels:    utils.GenerateLabel(awsv1alpha1.IAMUserIDLabel, uuid),
		},
		Spec: awsv1alpha1.AccountSpec{
			AwsAccountID:       "",
			IAMUserSecret:      "",
			ClaimLink:          "",
			ClaimLinkNamespace: "",
		},
	}
}

// GenerateAccountCRName return a formatted Account CR name
func GenerateAccountCRName(uuid string) string {
	emailID := awsv1alpha1.EmailID
	return fmt.Sprintf("%s-%s", emailID, uuid)
}
