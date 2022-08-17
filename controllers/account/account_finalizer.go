package account

import (
	"context"

	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	controllerutils "github.com/openshift/aws-account-operator/pkg/utils"
)

func (r *AccountReconciler) addFinalizer(reqLogger logr.Logger, account *awsv1alpha1.Account) error {

	if !controllerutils.Contains(account.GetFinalizers(), awsv1alpha1.AccountFinalizer) {
		reqLogger.Info("Adding Finalizer for the Account")
		account.SetFinalizers(append(account.GetFinalizers(), awsv1alpha1.AccountFinalizer))

		// Update CR
		err := r.Client.Update(context.TODO(), account)
		if err != nil {
			reqLogger.Error(err, "Failed to update Account with finalizer")
			return err
		}
	}
	return nil
}

// Function to remove finalizer
func (r *AccountReconciler) removeFinalizer(account *awsv1alpha1.Account, finalizerName string) error {
	account.SetFinalizers(controllerutils.Remove(account.GetFinalizers(), finalizerName))
	err := r.Client.Update(context.TODO(), account)
	if err != nil {
		return err
	}
	return nil
}
