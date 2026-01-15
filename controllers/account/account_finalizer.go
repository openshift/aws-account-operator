package account

import (
	"context"

	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	controllerutils "github.com/openshift/aws-account-operator/pkg/utils"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

func (r *AccountReconciler) addFinalizer(reqLogger logr.Logger, account *awsv1alpha1.Account) error {

	if !controllerutils.Contains(account.GetFinalizers(), awsv1alpha1.AccountFinalizer) {
		reqLogger.Info("Adding Finalizer for the Account")
		account.SetFinalizers(append(account.GetFinalizers(), awsv1alpha1.AccountFinalizer))

		// Update CR
		err := r.Update(context.TODO(), account)
		if err != nil {
			reqLogger.Error(err, "Failed to update Account with finalizer")
			return err
		}
	}
	return nil
}

// Function to remove finalizer
func (r *AccountReconciler) removeFinalizer(account *awsv1alpha1.Account, finalizerName string) error {
	log.Info("Attempting to remove finalizer from Account", "account", account.Name, "finalizer", finalizerName)

	maxRetries := 5
	for attempt := range maxRetries {
		if attempt > 0 {
			log.Info("Retrying finalizer removal due to conflict", "account", account.Name, "attempt", attempt+1, "maxRetries", maxRetries)
			err := r.Get(context.TODO(), types.NamespacedName{
				Namespace: account.Namespace,
				Name:      account.Name,
			}, account)
			if err != nil {
				if k8serr.IsNotFound(err) {
					return nil
				}
				log.Error(err, "Failed to refetch Account for finalizer retry", "account", account.Name)
				return err
			}
		}

		account.SetFinalizers(controllerutils.Remove(account.GetFinalizers(), finalizerName))

		err := r.Update(context.TODO(), account)
		if err != nil {
			if k8serr.IsNotFound(err) {
				return nil
			}
			if k8serr.IsConflict(err) && attempt < maxRetries-1 {
				continue
			}
			log.Error(err, "Failed to remove finalizer after retries", "account", account.Name, "attempt", attempt+1, "error", err.Error())
			return err
		}

		log.Info("Successfully removed finalizer from Account", "account", account.Name, "finalizer", finalizerName)
		return nil
	}

	err := k8serr.NewConflict(awsv1alpha1.GroupVersion.WithResource("account").GroupResource(), account.Name, nil)
	log.Error(err, "Failed to remove finalizer after max retries", "account", account.Name, "maxRetries", maxRetries)
	return err
}
