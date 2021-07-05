package utils

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SetAccountStatus sets the condition and state of an account
func SetAccountStatus(
	client client.Client,
	reqLogger logr.Logger,
	awsAccount *v1alpha1.Account,
	message string,
	conditionType v1alpha1.AccountConditionType,
	state v1alpha1.AccountStateStatus) error {

	var err error
	if awsAccount == nil {
		err = v1alpha1.ErrUnexpectedValue
		reqLogger.Error(err, "SetAccountStatus was passed a nil awsAccount instance")
		return err
	}

	if !v1alpha1.IsValidAccountConditionType(conditionType) {
		err = v1alpha1.ErrUnexpectedAccountState
		reqLogger.Error(err, fmt.Sprintf("Invalid AccountConditionType received [%s]", string(conditionType)))
		return err
	}

	if !v1alpha1.IsValidAccountStateStatus(state) {
		err = v1alpha1.ErrUnexpectedAccountState
		reqLogger.Error(err, fmt.Sprintf("Invalid AccountStateStatus received [%s]", string(state)))
		return err
	}

	awsAccount.Status.Conditions = SetAccountCondition(
		awsAccount.Status.Conditions,
		conditionType,
		corev1.ConditionTrue,
		string(state),
		message,
		UpdateConditionNever,
		awsAccount.Spec.BYOC,
	)
	awsAccount.Status.State = state

	err = client.Status().Update(context.TODO(), awsAccount)
	if err != nil {
		reqLogger.Error(err, "Failed to update Account Status")
	}

	return err
}

// SetAccountClaimStatus sets the condition and state of an accountClaim
func SetAccountClaimStatus(
	client client.Client,
	reqLogger logr.Logger,
	awsAccountClaim *v1alpha1.AccountClaim,
	message string,
	reason string,
	conditionType v1alpha1.AccountClaimConditionType,
	state v1alpha1.ClaimStatus) error {

	if awsAccountClaim == nil {
		return nil
	}

	awsAccountClaim.Status.Conditions = SetAccountClaimCondition(
		awsAccountClaim.Status.Conditions,
		conditionType,
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
