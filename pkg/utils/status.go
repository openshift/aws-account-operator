package utils

import (
	"fmt"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("status")

// SetAccountStatus sets the condition and state of an account
func SetAccountStatus(awsAccount *awsv1alpha1.Account, message string, ctype awsv1alpha1.AccountConditionType, state string) {
	awsAccount.Status.Conditions = SetAccountCondition(
		awsAccount.Status.Conditions,
		ctype,
		corev1.ConditionTrue,
		state,
		message,
		UpdateConditionNever,
		awsAccount.Spec.BYOC,
	)
	awsAccount.Status.State = state
	log.Info(fmt.Sprintf("Transitioned account %v/%v to state %v", awsAccount.Namespace, awsAccount.Name, awsAccount.Status.State))
}

// SetAccountClaimStatus sets the condition and state of an accountClaim
func SetAccountClaimStatus(awsAccountClaim *awsv1alpha1.AccountClaim, message string, reason string, ctype awsv1alpha1.AccountClaimConditionType, state awsv1alpha1.ClaimStatus) {
	if awsAccountClaim == nil {
		return
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
}
