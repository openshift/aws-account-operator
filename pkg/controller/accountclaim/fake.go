package accountclaim

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	controllerutils "github.com/openshift/aws-account-operator/pkg/controller/utils"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *ReconcileAccountClaim) processFake(reqLogger logr.Logger, accountClaim *awsv1alpha1.AccountClaim) (bool, error) {
	// Add finalizer to the CR in case it's not present (e.g. old accounts)
	if !controllerutils.Contains(accountClaim.GetFinalizers(), accountClaimFinalizer) {
		err := r.addFinalizer(reqLogger, accountClaim)
		if err != nil {
			return true, err
		}
		return true, nil
	}

	// Check if accountClaim is being deleted, and remove the fakesecret
	if accountClaim.DeletionTimestamp != nil {
		// Delete fake secret if it exists
		// Create secret for OCM to consume
		if r.checkIAMSecretExists(accountClaim.Spec.AwsCredentialSecret.Name, accountClaim.Spec.AwsCredentialSecret.Namespace) {

			// Need to check if the secret exists AND that it matches what we're expecting
			secret := corev1.Secret{}
			secretObjectKey := client.ObjectKey{Name: accountClaim.Spec.AwsCredentialSecret.Name, Namespace: accountClaim.Spec.AwsCredentialSecret.Namespace}
			err := r.client.Get(context.TODO(), secretObjectKey, &secret)
			if err != nil { //nolint; gosimple // Ignores false-positive S1008 gosimple notice
				return true, err
			}

			err = r.client.Delete(context.TODO(), &secret)
			if err != nil {
				reqLogger.Error(err, "Failed to Fake Secret During Fake cleanup")
				return true, err
			}
		}

		// Remove finalizer to unlock deletion of the accountClaim
		err := r.removeFinalizer(reqLogger, accountClaim, accountClaimFinalizer)
		if err != nil {
			return true, err
		}
		return false, nil
	}

	// Create Fake Secret if it doesnt exist
	if !r.checkIAMSecretExists(accountClaim.Spec.AwsCredentialSecret.Name, accountClaim.Spec.AwsCredentialSecret.Namespace) {
		err := r.client.Create(context.TODO(), newSecretforCR(accountClaim.Spec.AwsCredentialSecret.Name, accountClaim.Spec.AwsCredentialSecret.Namespace, []byte("fakeAccessKey"), []byte("FakeSecretAccesskey")))
		if err != nil {
			reqLogger.Error(err, "Unable to create secret for OCM")
			return true, err
		}
	}

	// Set to Ready
	if accountClaim.Status.State != awsv1alpha1.ClaimStatusReady {
		// Set AccountClaim.Status.Conditions and AccountClaim.Status.State to Ready
		accountClaim.Status.Conditions = controllerutils.SetAccountClaimCondition(
			accountClaim.Status.Conditions,
			awsv1alpha1.AccountClaimed,
			corev1.ConditionTrue,
			AccountClaimed,
			"Fake ccount claim fulfilled",
			controllerutils.UpdateConditionNever,
			accountClaim.Spec.BYOCAWSAccountID != "")
		accountClaim.Status.State = awsv1alpha1.ClaimStatusReady
		reqLogger.Info(fmt.Sprintf("Fake Account %s condition status updated", accountClaim.Name))
		err := r.statusUpdate(reqLogger, accountClaim)
		if err != nil {
			return true, err
		}
		return false, nil
	}

	return false, nil
}
