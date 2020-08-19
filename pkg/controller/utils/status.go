package utils

import (
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

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
}
