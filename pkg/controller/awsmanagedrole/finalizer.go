package awsmanagedrole

import (
	"context"

	"github.com/go-logr/logr"

	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/controller/utils"
)

func (r *ReconcileAWSManagedRole) addFinalizer(reqLogger logr.Logger, awsManagedRole *awsv1alpha1.AWSManagedRole) error {
	reqLogger.Info("Adding Finalizer for the ManagedRole")
	awsManagedRole.SetFinalizers(append(awsManagedRole.GetFinalizers(), utils.Finalizer))

	// Update CR
	err := r.client.Update(context.TODO(), awsManagedRole)
	if err != nil {
		reqLogger.Error(err, "Failed to update ManagedRole with finalizer")
		return err
	}
	return nil
}

func (r *ReconcileAWSManagedRole) removeFinalizer(reqLogger logr.Logger, awsManagedRole *awsv1alpha1.AWSManagedRole, finalizerName string) error {
	reqLogger.Info("Removing Finalizer for the AWSManagedRole")
	awsManagedRole.SetFinalizers(utils.Remove(awsManagedRole.GetFinalizers(), finalizerName))

	// Update CR
	err := r.client.Update(context.TODO(), awsManagedRole)
	if err != nil {
		reqLogger.Error(err, "Failed to remove ManagedRole finalizer")
		return err
	}
	return nil
}

func (r *ReconcileAWSManagedRole) finalizeFederateRole(reqLogger logr.Logger, awsManagedRole *awsv1alpha1.AWSManagedRole) error {
	// If the role is managed, remove the managed role annotation from all accounts
	// otherwise this role might have associated ManagedAccountAccesses that need to be removed
	r.deleteManagedRole(awsManagedRole)

	return nil
}
