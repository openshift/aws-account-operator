package account

import (
	"context"

	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/controller/utils"
)

// Function to remove finalizer
func (r *ReconcileAccount) removeFinalizer(reqLogger logr.Logger, account *awsv1alpha1.Account, finalizerName string) error {
	reqLogger.Info("Removing Finalizer from the Account")
	account.SetFinalizers(utils.Remove(account.GetFinalizers(), finalizerName))

	// Update CR
	err := r.Client.Update(context.TODO(), account)
	if err != nil {
		reqLogger.Error(err, "Failed to remove AccountClaim finalizer")
		return err
	}
	return nil
}
