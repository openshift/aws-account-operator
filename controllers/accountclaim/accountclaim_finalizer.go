package accountclaim

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"

	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
)

func (r *AccountClaimReconciler) addFinalizer(reqLogger logr.Logger, accountClaim *awsv1alpha1.AccountClaim) error {
	reqLogger.Info("Adding Finalizer for the AccountClaim")
	accountClaim.SetFinalizers(append(accountClaim.GetFinalizers(), accountClaimFinalizer))

	// Update CR
	err := r.Update(context.TODO(), accountClaim)
	if err != nil {
		reqLogger.Error(err, "Failed to update AccountClaim with finalizer")
		return err
	}
	return nil
}

func (r *AccountClaimReconciler) removeFinalizer(reqLogger logr.Logger, accountClaim *awsv1alpha1.AccountClaim, finalizerName string) error {
	reqLogger.Info("Removing Finalizer for the AccountClaim")
	accountClaim.SetFinalizers(utils.Remove(accountClaim.GetFinalizers(), finalizerName))

	// Update CR
	err := r.Update(context.TODO(), accountClaim)
	if err != nil {
		reqLogger.Error(err, "Failed to remove AccountClaim finalizer")
		return err
	}
	return nil
}

func (r *AccountClaimReconciler) addBYOCSecretFinalizer(accountClaim *awsv1alpha1.AccountClaim) error {

	byocSecret := &corev1.Secret{}
	err := r.Get(context.TODO(),
		types.NamespacedName{
			Name:      accountClaim.Spec.BYOCSecretRef.Name,
			Namespace: accountClaim.Spec.BYOCSecretRef.Namespace},
		byocSecret)
	if err != nil {
		return err
	}

	if !utils.Contains(byocSecret.GetFinalizers(), byocSecretFinalizer) {
		utils.AddFinalizer(byocSecret, byocSecretFinalizer)
		err = r.Update(context.TODO(), byocSecret)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *AccountClaimReconciler) removeBYOCSecretFinalizer(accountClaim *awsv1alpha1.AccountClaim) error {

	byocSecret := &corev1.Secret{}
	err := r.Get(context.TODO(),
		types.NamespacedName{
			Name:      accountClaim.Spec.BYOCSecretRef.Name,
			Namespace: accountClaim.Spec.BYOCSecretRef.Namespace},
		byocSecret)
	if err != nil {
		// If the secret can't be found, don't error, just return
		if k8serr.IsNotFound(err) {
			return nil
		}
		return err
	}

	byocSecret.Finalizers = utils.Remove(byocSecret.Finalizers, byocSecretFinalizer)
	err = r.Update(context.TODO(), byocSecret)
	if err != nil {
		return err
	}

	return nil
}
