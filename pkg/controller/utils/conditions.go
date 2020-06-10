package utils

import (
	"fmt"
	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/localmetrics"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// UpdateConditionCheck tests whether a condition should be updated from the
// old condition to the new condition. Returns true if the condition should
// be updated.
type UpdateConditionCheck func(oldReason, oldMessage, newReason, newMessage string) bool

// UpdateConditionAlways returns true. The condition will always be updated.
func UpdateConditionAlways(_, _, _, _ string) bool {
	return true
}

// UpdateConditionNever return false. The condition will never be updated,
// unless there is a change in the status of the condition.
func UpdateConditionNever(_, _, _, _ string) bool {
	return false
}

// UpdateConditionIfReasonOrMessageChange returns true if there is a change
// in the reason or the message of the condition.
func UpdateConditionIfReasonOrMessageChange(oldReason, oldMessage, newReason, newMessage string) bool {
	return oldReason != newReason ||
		oldMessage != newMessage
}

func shouldUpdateCondition(
	oldStatus corev1.ConditionStatus, oldReason, oldMessage string,
	newStatus corev1.ConditionStatus, newReason, newMessage string,
	updateConditionCheck UpdateConditionCheck,
) bool {
	if oldStatus != newStatus {
		return true
	}
	return updateConditionCheck(oldReason, oldMessage, newReason, newMessage)
}

// SetAccountClaimCondition sets a condition on a AccountClaim resource's status
func SetAccountClaimCondition(
	conditions []awsv1alpha1.AccountClaimCondition,
	conditionType awsv1alpha1.AccountClaimConditionType,
	status corev1.ConditionStatus,
	reason string,
	message string,
	updateConditionCheck UpdateConditionCheck,
	ccs bool,
) []awsv1alpha1.AccountClaimCondition {
	now := metav1.Now()
	existingCondition := FindAccountClaimCondition(conditions, conditionType)
	if existingCondition == nil {
		if status == corev1.ConditionTrue {
			conditions = append(
				conditions,
				awsv1alpha1.AccountClaimCondition{
					Type:               conditionType,
					Status:             status,
					Reason:             reason,
					Message:            message,
					LastTransitionTime: now,
					LastProbeTime:      now,
				},
			)
		}
	} else {
		if shouldUpdateCondition(
			existingCondition.Status, existingCondition.Reason, existingCondition.Message,
			status, reason, message,
			updateConditionCheck,
		) {
			if existingCondition.Status != status {
				existingCondition.LastTransitionTime = now
			}
			existingCondition.Status = status
			existingCondition.Reason = reason
			existingCondition.Message = message
			existingCondition.LastProbeTime = now
		}
	}

	if conditionType == awsv1alpha1.AccountClaimed {
		unclaimedCondition := FindAccountClaimCondition(conditions, awsv1alpha1.AccountUnclaimed)
		if unclaimedCondition != nil {
			readyDuration := now.Sub(unclaimedCondition.LastProbeTime.Time)
			localmetrics.Collector.SetAccountClaimReadyDuration(ccs, readyDuration.Seconds())
		}
	}
	return conditions
}

// FindAccountClaimCondition finds in the condition that has the
// specified condition type in the given list. If none exists, then returns nil.
func FindAccountClaimCondition(conditions []awsv1alpha1.AccountClaimCondition, conditionType awsv1alpha1.AccountClaimConditionType) *awsv1alpha1.AccountClaimCondition {
	for i, condition := range conditions {
		if condition.Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

// SetAccountCondition sets a condition on a Account resource's status
func SetAccountCondition(
	conditions []awsv1alpha1.AccountCondition,
	conditionType awsv1alpha1.AccountConditionType,
	status corev1.ConditionStatus,
	reason string,
	message string,
	updateConditionCheck UpdateConditionCheck,
) []awsv1alpha1.AccountCondition {
	now := metav1.Now()
	existingCondition := FindAccountCondition(conditions, conditionType)
	if existingCondition == nil {
		if status == corev1.ConditionTrue {
			conditions = append(
				conditions,
				awsv1alpha1.AccountCondition{
					Type:               conditionType,
					Status:             status,
					Reason:             reason,
					Message:            message,
					LastTransitionTime: now,
					LastProbeTime:      now,
				},
			)
		}
	} else {
		if shouldUpdateCondition(
			existingCondition.Status, existingCondition.Reason, existingCondition.Message,
			status, reason, message,
			updateConditionCheck,
		) {
			if existingCondition.Status != status {
				existingCondition.LastTransitionTime = now
			}
			existingCondition.Status = status
			existingCondition.Reason = reason
			existingCondition.Message = message
		}
		// Need to always update the probe time, so if the condition occurs again
		// or we probe and the condition is still active, the date is updated.
		existingCondition.LastProbeTime = now
	}

	if conditionType == awsv1alpha1.AccountReady {
		creatingCondition := FindAccountCondition(conditions, awsv1alpha1.AccountCreating)
		if creatingCondition != nil {
			readyDuration := now.Sub(creatingCondition.LastProbeTime.Time)
			localmetrics.Collector.SetAccountReadyDuration(readyDuration.Seconds())
		}
	}

	return conditions
}

// FindAccountCondition finds in the condition that has the
// specified condition type in the given list. If none exists, then returns nil.
func FindAccountCondition(conditions []awsv1alpha1.AccountCondition, conditionType awsv1alpha1.AccountConditionType) *awsv1alpha1.AccountCondition {
	for i, condition := range conditions {
		if condition.Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

// SetAWSFederatedRoleCondition sets a condition on a AWSFederatedRole resource's status
func SetAWSFederatedRoleCondition(
	conditions []awsv1alpha1.AWSFederatedRoleCondition,
	conditionType awsv1alpha1.AWSFederatedRoleConditionType,
	status corev1.ConditionStatus,
	reason string,
	message string,
	updateConditionCheck UpdateConditionCheck,
) []awsv1alpha1.AWSFederatedRoleCondition {
	now := metav1.Now()
	existingCondition := FindAWSFederatedRoleCondition(conditions, conditionType)
	if existingCondition == nil {
		if status == corev1.ConditionTrue {
			conditions = append(
				conditions,
				awsv1alpha1.AWSFederatedRoleCondition{
					Type:               conditionType,
					Status:             status,
					Reason:             reason,
					Message:            message,
					LastTransitionTime: now,
					LastProbeTime:      now,
				},
			)
		}
	} else {
		if shouldUpdateCondition(
			existingCondition.Status, existingCondition.Reason, existingCondition.Message,
			status, reason, message,
			updateConditionCheck,
		) {
			if existingCondition.Status != status {
				existingCondition.LastTransitionTime = now
			}
			existingCondition.Status = status
			existingCondition.Reason = reason
			existingCondition.Message = message
			existingCondition.LastProbeTime = now
		}
	}
	return conditions
}

// FindAWSFederatedRoleCondition Condition finds in the condition that has the
// specified condition type in the given list. If none exists, then returns nil.
func FindAWSFederatedRoleCondition(conditions []awsv1alpha1.AWSFederatedRoleCondition, conditionType awsv1alpha1.AWSFederatedRoleConditionType) *awsv1alpha1.AWSFederatedRoleCondition {
	for i, condition := range conditions {
		if condition.Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

// SetAWSFederatedAccountAccessCondition sets a condition on a Account resource's status
func SetAWSFederatedAccountAccessCondition(
	conditions []awsv1alpha1.AWSFederatedAccountAccessCondition,
	conditionType awsv1alpha1.AWSFederatedAccountAccessConditionType,
	status corev1.ConditionStatus,
	reason string,
	message string,
	updateConditionCheck UpdateConditionCheck,
) []awsv1alpha1.AWSFederatedAccountAccessCondition {
	now := metav1.Now()
	existingCondition := FindAWSFederatedAccountAccessCondition(conditions, conditionType)
	if existingCondition == nil {
		if status == corev1.ConditionTrue {
			conditions = append(
				conditions,
				awsv1alpha1.AWSFederatedAccountAccessCondition{
					Type:               conditionType,
					Status:             status,
					Reason:             reason,
					Message:            message,
					LastTransitionTime: now,
					LastProbeTime:      now,
				},
			)
		}
	} else {
		if shouldUpdateCondition(
			existingCondition.Status, existingCondition.Reason, existingCondition.Message,
			status, reason, message,
			updateConditionCheck,
		) {
			if existingCondition.Status != status {
				existingCondition.LastTransitionTime = now
			}
			existingCondition.Status = status
			existingCondition.Reason = reason
			existingCondition.Message = message
			existingCondition.LastProbeTime = now
		}
	}
	return conditions
}

// FindAWSFederatedAccountAccessCondition Condition finds in the condition that has the
// specified condition type in the given list. If none exists, then returns nil.
func FindAWSFederatedAccountAccessCondition(conditions []awsv1alpha1.AWSFederatedAccountAccessCondition, conditionType awsv1alpha1.AWSFederatedAccountAccessConditionType) *awsv1alpha1.AWSFederatedAccountAccessCondition {
	for i, condition := range conditions {
		if condition.Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

const (
	AwsSecretName = "aws-account-operator-credentials"
)

// SetBYOCAccountClaimStatusAWSAccountInUse Sets the Status.State and appends an account Status.Condition
func SetBYOCAccountClaimStatusAWSAccountInUse(reqLogger logr.Logger, accountClaim *awsv1alpha1.AccountClaim) {
	message := fmt.Sprintf("AWS Account %s already in use", accountClaim.Spec.BYOCAWSAccountID)
	accountClaim.Status.Conditions = SetAccountClaimCondition(
		accountClaim.Status.Conditions,
		awsv1alpha1.BYOCAWSAccountInUse,
		corev1.ConditionTrue,
		string(awsv1alpha1.BYOCAWSAccountInUse),
		message,
		UpdateConditionNever,
		accountClaim.Spec.BYOCAWSAccountID != "",
	)
	accountClaim.Status.State = awsv1alpha1.ClaimStatusError
	reqLogger.Info(fmt.Sprintf("AccountClaim %s condition status updated", accountClaim.Name))
}
