package account

import (
	"context"

	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/controller/utils"
)

// Function to remove finalizer
func (r *ReconcileAccount) removeFinalizer(account *awsv1alpha1.Account, finalizerName string) error {
	account.SetFinalizers(utils.Remove(account.GetFinalizers(), finalizerName))
	err := r.Client.Update(context.TODO(), account)
	if err != nil {
		return err
	}
	return nil
}
