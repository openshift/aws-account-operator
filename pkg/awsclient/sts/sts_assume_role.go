package sts

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/config"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/rkt/rkt/tests/testutils/logger"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	defaultSleepDelay = 500 * time.Millisecond
)

const (
	controllerName = "account"
)

func matchSubstring(roleID, role string) (bool, error) {
	matched, err := regexp.MatchString(roleID, role)
	return matched, err
}

// getSTSCredentials returns STS credentials for the specified account ARN
func GetSTSCredentials(
	reqLogger logr.Logger,
	client awsclient.Client,
	roleArn string,
	externalID string,
	roleSessionName string) (*sts.AssumeRoleOutput, error) {
	// Default duration in seconds of the session token 3600. We need to have the roles policy
	// changed if we want it to be longer than 3600 seconds
	reqLogger.Info(fmt.Sprintf("Creating STS credentials for AWS ARN: %s", roleArn))
	// Build input for AssumeRole
	assumeRoleInput := sts.AssumeRoleInput{
		DurationSeconds: aws.Int32(3600),
		RoleArn:         &roleArn,
		RoleSessionName: &roleSessionName,
	}
	if externalID != "" {
		assumeRoleInput.ExternalId = &externalID
	}

	assumeRoleOutput := &sts.AssumeRoleOutput{}
	var err error
	for i := 0; i < 100; i++ {
		time.Sleep(defaultSleepDelay)
		assumeRoleOutput, err = client.AssumeRole(context.TODO(), &assumeRoleInput)
		if err == nil {
			break
		}
		if i == 99 {
			reqLogger.Info(fmt.Sprintf("Timed out while assuming role %s", roleArn))
		}
	}
	if err != nil {
		// Log AWS error
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			reqLogger.Error(err,
				fmt.Sprintf(`New AWS Error while getting STS credentials,
					AWS Error Code: %s,
					AWS Error Message: %s`,
					apiErr.ErrorCode(),
					apiErr.ErrorMessage()))
		} else {
			reqLogger.Error(err,
				fmt.Sprintf(`Unknown error while getting STS credentials: %s`, err))
		}
		return &sts.AssumeRoleOutput{}, err
	}
	return assumeRoleOutput, err
}

func AssumeRoleAndCreateClient(
	reqLogger logr.Logger,
	awsClientBuilder awsclient.IBuilder,
	currentAcctInstance *awsv1alpha1.Account,
	client client.Client,
	awsSetupClient awsclient.Client,
	region string,
	roleToAssume string,
	ccsRoleID string) (awsclient.Client, *sts.AssumeRoleOutput, error) {
	return HandleRoleAssumption(reqLogger, awsClientBuilder, currentAcctInstance, client, awsSetupClient, region, roleToAssume, ccsRoleID)
}

func HandleRoleAssumption(
	reqLogger logr.Logger,
	awsClientBuilder awsclient.IBuilder,
	currentAcctInstance *awsv1alpha1.Account,
	client client.Client,
	awsSetupClient awsclient.Client,
	region string,
	roleToAssume string,
	ccsRoleID string) (awsclient.Client, *sts.AssumeRoleOutput, error) {

	// The role ARN made up of the account number and the role which is the default role name
	// created in child accounts
	roleArn := config.GetIAMArn(currentAcctInstance.Spec.AwsAccountID, config.AwsResourceTypeRole, roleToAssume)

	// Use the role session name to uniquely identify a session when the same role
	// is assumed by different principals or for different reasons.
	var roleSessionName = "awsAccountOperator"

	var creds *sts.AssumeRoleOutput
	var credsErr error

	for i := 0; i < 10; i++ {

		// Get STS credentials so that we can create an aws client with
		creds, credsErr = GetSTSCredentials(reqLogger, awsSetupClient, roleArn, "", roleSessionName)
		if credsErr != nil {
			return nil, nil, credsErr
		}

		// If this is a BYOC account, check that BYOCAdminAccess role was the one used in the AssumedRole.
		// RoleID must exist in the AssumeRoleID string. This is an eventual consistency work-around code.
		// It can take some varying amount of time to use the correct role if it had just been created.
		match, _ := matchSubstring(ccsRoleID, *creds.AssumedRoleUser.AssumedRoleId)
		if ccsRoleID != "" && !match {
			reqLogger.Info(fmt.Sprintf("Assumed RoleID:Session string does not match new RoleID: %s, %s", *creds.AssumedRoleUser.AssumedRoleId, ccsRoleID))
			reqLogger.Info(fmt.Sprintf("Sleeping %d seconds", i))
			time.Sleep(time.Duration(i) * time.Second)
		} else {
			break
		}
	}

	var awsRegion string
	if region != "" {
		awsRegion = region
	} else {
		awsRegion = config.GetDefaultRegion()
	}
	// create an awsclientbuilder function in the accountReconciler struct

	// pass in awsclient or pass in the AwsClientBuilder
	awsAssumedRoleClient, err := awsClientBuilder.GetClient(controllerName, client, awsclient.NewAwsClientInput{
		AwsCredsSecretIDKey:     *creds.Credentials.AccessKeyId,
		AwsCredsSecretAccessKey: *creds.Credentials.SecretAccessKey,
		AwsToken:                *creds.Credentials.SessionToken,
		AwsRegion:               awsRegion,
	})
	if err != nil {
		logger.Error(err, "Failed to assume role")
		reqLogger.Info(err.Error())
		return nil, nil, err
	}
	return awsAssumedRoleClient, creds, nil
}
