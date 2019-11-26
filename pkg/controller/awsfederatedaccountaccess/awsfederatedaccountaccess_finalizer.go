package awsfederatedaccountaccess

import (
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go/aws/awserr"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
)

func (r *ReconcileAWSFederatedAccountAccess) finalizeAWSFederatedAccountAccess(reqLogger logr.Logger, awsfederatedAccountAccess *awsv1alpha1.AWSFederatedAccountAccess) error {

	// Perform account clean up in AWS
	err := r.cleanUpAwsAccount(reqLogger, awsfederatedAccountAccess)
	if err != nil {
		reqLogger.Error(err, "Failed to clean up AWS account")
		return err
	}

	reqLogger.Info("Successfully finalized AccountClaim")
	return nil
}

func (r *ReconcileAWSFederatedAccountAccess) cleanUpAwsAccount(reqLogger logr.Logger, awsFederatedAccountAccess *awsv1alpha1.AWSFederatedAccountAccess) error {
	// Clean up status, used to store an error if any of the cleanup functions received one
	cleanUpStatusFailed := false

	// Channels to track clean up functions
	awsNotifications, awsErrors := make(chan string), make(chan string)

	defer close(awsNotifications)
	defer close(awsErrors)

	// Use the account name reference to get the secret
	secretName := awsFederatedAccountAccess.Spec.AccountReference + "-secret"

	// Get aws client
	awsClient, err := awsclient.GetAWSClient(r.client, awsclient.NewAwsClientInput{
		SecretName: secretName,
		NameSpace:  awsv1alpha1.AccountCrNamespace,
		AwsRegion:  "us-east-1",
	})
	if err != nil {
		awsClientErr := fmt.Sprintf("Unable to create aws client for region ")
		reqLogger.Error(err, awsClientErr)
		return err
	}

	// Declare un array of cleanup functions
	cleanUpFunctions := []func(logr.Logger, awsclient.Client, *awsv1alpha1.AWSFederatedAccountAccess, chan string, chan string) error{
		r.cleanUpAwsFederatedRole,
	}

	// Call the clean up functions in parallel
	for _, cleanUpFunc := range cleanUpFunctions {
		go cleanUpFunc(reqLogger, awsClient, awsFederatedAccountAccess, awsNotifications, awsErrors)
	}

	// Wait for clean up functions to end
	for i := 0; i < len(cleanUpFunctions); i++ {
		select {
		case msg := <-awsNotifications:
			reqLogger.Info(msg)
		case errMsg := <-awsErrors:
			err = errors.New(errMsg)
			reqLogger.Error(err, errMsg)
			cleanUpStatusFailed = true
		}
	}

	// Return an error if we saw any errors on the awsErrors channel so we can make the reused account as failed
	if cleanUpStatusFailed {
		cleanUpStatusFailedMsg := "Failed to clean up AWS account"
		err = errors.New(cleanUpStatusFailedMsg)
		reqLogger.Error(err, cleanUpStatusFailedMsg)
	}

	reqLogger.Info("AWS account cleanup completed")

	return nil
}

func (r *ReconcileAWSFederatedAccountAccess) cleanUpAwsFederatedRole(reqLogger logr.Logger, awsClient awsclient.Client, awsfederatedAccountAccess *awsv1alpha1.AWSFederatedAccountAccess, awsNotifications chan string, awsErrors chan string) error {

	_, err := awsClient.DeleteRole(&iam.DeleteRoleInput{RoleName: aws.String(awsfederatedAccountAccess.Spec.AWSFederatedRoleName)})

	if err != nil {
		descError := "Failed deleting Federated Account Role"
		if awsErr, ok := err.(awserr.Error); ok {
			// process SDK error

			awsErrors <- descError
			return awsErr
		}
		return err
	}

	successMsg := fmt.Sprintf("Federated Account Role cleanup finished successfully")
	awsNotifications <- successMsg
	return nil
}
