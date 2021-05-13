package utils

import (
	"context"

	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	kubeclientpkg "sigs.k8s.io/controller-runtime/pkg/client"
)

// SetAccountStatus sets the condition and state of an account
func SetAccountStatus(client kubeclientpkg.Client, reqLogger logr.Logger, awsAccount *awsv1alpha1.Account, message string, ctype awsv1alpha1.AccountConditionType) error {
	awsAccount.Status.Conditions = SetAccountCondition(
		awsAccount.Status.Conditions,
		ctype,
		corev1.ConditionTrue,
		string(ctype),
		message,
		UpdateConditionNever,
		awsAccount.Spec.BYOC,
	)

	awsAccount.Status.State = string(ctype)

	err := client.Status().Update(context.TODO(), awsAccount)
	if err != nil {
		reqLogger.Error(err, "Failed to update Account Status")
	}

	return err
}

// SetAccountClaimStatus sets the condition and state of an accountClaim
func SetAccountClaimStatus(client kubeclientpkg.Client, reqLogger logr.Logger, awsAccountClaim *awsv1alpha1.AccountClaim, message string, reason string, ctype awsv1alpha1.AccountClaimConditionType, state awsv1alpha1.ClaimStatus) error {
	if awsAccountClaim == nil {
		return nil
	}

	awsAccountClaim.Status.Conditions = SetAccountClaimCondition(
		awsAccountClaim.Status.Conditions,
		ctype,
		corev1.ConditionTrue,
		reason,
		message,
		UpdateConditionNever,
		awsAccountClaim.Spec.BYOC,
	)

	awsAccountClaim.Status.State = state

	err := client.Status().Update(context.TODO(), awsAccountClaim)
	if err != nil {
		reqLogger.Error(err, "Failed to update AccountClaim Status")
	}

	return err
}
