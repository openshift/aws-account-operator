package account

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/controller/utils"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"k8s.io/apimachinery/pkg/types"

	retry "github.com/avast/retry-go"
)

// Type that represents JSON object of an AWS permissions statement
type awsStatement struct {
	Effect    string                 `json:"Effect"`
	Action    []string               `json:"Action"`
	Resource  []string               `json:"Resource,omitempty"`
	Principal *awsv1alpha1.Principal `json:"Principal,omitempty"`
}

// PolicyDocument represents JSON object of an AWS Policy Document
type PolicyDocument struct {
	Version   string
	Statement []StatementEntry
}

// StatementEntry represents JSON of a statement in a policy doc
type StatementEntry struct {
	Effect   string
	Action   []string
	Resource string
}

// CreateSecret creates a secret for placing IAM Credentials
// Takes a logger, the desired name of the secret, the Account CR
// that will own the secret, and pointer to an empty secret object to fill
func (r *ReconcileAccount) CreateSecret(reqLogger logr.Logger, account *awsv1alpha1.Account, secret *corev1.Secret) error {

	// Set controller as owner of secret
	if err := controllerutil.SetControllerReference(account, secret, r.scheme); err != nil {
		return err
	}

	createErr := r.Client.Create(context.TODO(), secret)
	if createErr != nil {
		failedToCreateUserSecretMsg := fmt.Sprintf("Failed to create secret %s", secret.Name)
		err := utils.SetAccountStatus(
			r.Client,
			reqLogger,
			account,
			failedToCreateUserSecretMsg,
			awsv1alpha1.AccountFailed,
		)
		if err != nil {
			return err
		}
		reqLogger.Info(failedToCreateUserSecretMsg)
		return createErr
	}
	reqLogger.Info(fmt.Sprintf("Created secret %s", secret.Name))
	return nil
}

// getSTSCredentials returns STS credentials for the specified account ARN
func getSTSCredentials(
	reqLogger logr.Logger,
	client awsclient.Client,
	roleArn string,
	externalID string,
	roleSessionName string) (*sts.AssumeRoleOutput, error) {
	// Default duration in seconds of the session token 3600. We need to have the roles policy
	// changed if we want it to be longer than 3600 seconds
	var roleSessionDuration int64 = 3600
	reqLogger.Info(fmt.Sprintf("Creating STS credentials for AWS ARN: %s", roleArn))
	// Build input for AssumeRole
	assumeRoleInput := sts.AssumeRoleInput{
		DurationSeconds: &roleSessionDuration,
		RoleArn:         &roleArn,
		RoleSessionName: &roleSessionName,
	}
	if externalID != "" {
		assumeRoleInput.ExternalId = &externalID
	}

	assumeRoleOutput := &sts.AssumeRoleOutput{}
	var err error
	for i := 0; i < 100; i++ {
		time.Sleep(500 * time.Millisecond)
		assumeRoleOutput, err = client.AssumeRole(&assumeRoleInput)
		if err == nil {
			break
		}
		if i == 99 {
			reqLogger.Info(fmt.Sprintf("Timed out while assuming role %s", roleArn))
		}
	}
	if err != nil {
		// Log AWS error
		if aerr, ok := err.(awserr.Error); ok {
			reqLogger.Error(aerr,
				fmt.Sprintf(`New AWS Error while getting STS credentials,
					AWS Error Code: %s,
					AWS Error Message: %s`,
					aerr.Code(),
					aerr.Message()))
		}
		return &sts.AssumeRoleOutput{}, err
	}

	return assumeRoleOutput, nil
}

func retryIfAwsServiceFailureOrInvalidToken(err error) bool {
	if aerr, ok := err.(awserr.Error); ok {
		switch aerr.Code() {
		// ServiceFailure may be an unspecified server-side error, and is worth retrying
		case "ServiceFailure":
			return true
		// InvalidClientTokenId may be a transient auth issue, retry
		case "InvalidClientTokenId":
			return true
		// AccessDenied happens when Eventual Consistency hasn't become consistent yet
		case "AccessDenied":
			return true
		}
	}
	// Otherwise, do not retry
	return false
}

func listAccessKeys(client awsclient.Client, iamUser *iam.User) (*iam.ListAccessKeysOutput, error) {
	var result *iam.ListAccessKeysOutput
	var err error

	// Default is 1/10 of a second, but any retries we need to make should be delayed a few seconds
	// This also defaults to an exponential backoff, so we only need to try ~5 times, default is 10
	retry.DefaultDelay = 3 * time.Second
	retry.DefaultAttempts = uint(5)
	err = retry.Do(
		func() (err error) {
			result, err = client.ListAccessKeys(&iam.ListAccessKeysInput{UserName: iamUser.UserName})
			return err
		},

		// Retry if we receive some specific errors: access denied, rate limit or server-side error
		retry.RetryIf(retryIfAwsServiceFailureOrInvalidToken),
	)

	return result, err
}

func deleteAccessKey(client awsclient.Client, accessKeyID *string, username *string) (*iam.DeleteAccessKeyOutput, error) {
	var result *iam.DeleteAccessKeyOutput
	var err error

	// Default is 1/10 of a second, but any retries we need to make should be delayed a few seconds
	// This also defaults to an exponential backoff, so we only need to try ~5 times, default is 10
	retry.DefaultDelay = 3 * time.Second
	retry.DefaultAttempts = uint(5)
	err = retry.Do(
		func() (err error) {
			result, err = client.DeleteAccessKey(&iam.DeleteAccessKeyInput{
				AccessKeyId: accessKeyID,
				UserName:    username,
			})
			return err
		},

		// Retry if we receive some specific errors: access denied, rate limit or server-side error
		retry.RetryIf(retryIfAwsServiceFailureOrInvalidToken),
	)

	return result, err
}

// deleteAllAccessKeys deletes all access key pairs for a given user
// Takes a logger, an AWS client, and the target IAM user's username
func deleteAllAccessKeys(client awsclient.Client, iamUser *iam.User) error {
	accessKeyList, err := listAccessKeys(client, iamUser)
	if err != nil {
		return err
	}

	// Range through all AccessKeys for IAM user and delete them
	for index := range accessKeyList.AccessKeyMetadata {
		_, err = deleteAccessKey(client, accessKeyList.AccessKeyMetadata[index].AccessKeyId, iamUser.UserName)
		if err != nil {
			return err
		}
	}

	return nil
}

// CreateIAMUser creates a new IAM user in the target AWS account
// Takes a logger, an AWS client for the target account, and the desired IAM username
func CreateIAMUser(reqLogger logr.Logger, client awsclient.Client, userName string) (*iam.CreateUserOutput, error) {
	var createUserOutput *iam.CreateUserOutput
	var err error

	attempt := 1
	for i := 0; i < 10; i++ {

		createUserOutput, err = client.CreateUser(&iam.CreateUserInput{
			UserName: aws.String(userName),
		})

		attempt++
		// handle errors
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				switch aerr.Code() {
				// Since we're using the same credentials to create the user as we did to check if the user exists
				// we can continue to try without returning, also the outer loop will eventually return
				case "InvalidClientTokenId":
					invalidTokenMsg := fmt.Sprintf("Invalid Token error from AWS when attempting to create user %s, trying again", userName)
					reqLogger.Info(invalidTokenMsg)
					if attempt == 10 {
						return &iam.CreateUserOutput{}, err
					}
				case "AccessDenied":
					reqLogger.Info("Attempt to create user is Unauthorized. Trying Again due to AWS Eventual Consistency")
					if attempt == 10 {
						return &iam.CreateUserOutput{}, err
					}
				// createUserOutput inconsistently returns "InvalidClientTokenId" if that happens then the next call to
				// create the user will fail with EntitiyAlreadyExists. Since we verity the user doesn't exist before this
				// loop we can safely assume we created the user on our first loop.
				case iam.ErrCodeEntityAlreadyExistsException:
					invalidTokenMsg := fmt.Sprintf("IAM User %s was created", userName)
					reqLogger.Info(invalidTokenMsg)
					return &iam.CreateUserOutput{}, err
				default:
					utils.LogAwsError(reqLogger, "CreateIAMUser: Unexpected AWS Error during creation of IAM user", nil, err)
					return &iam.CreateUserOutput{}, err
				}
				time.Sleep(time.Duration(time.Duration(attempt*5) * time.Second))
			} else {
				return &iam.CreateUserOutput{}, err
			}
		} else {
			// Break for loop if no errors are present.
			break
		}
	}
	// User creation successful
	return createUserOutput, nil
}

// AttachAdminUserPolicy attaches the AdministratorAccess policy to a target user
// Takes a logger, an AWS client for the target account, and the target IAM user's username
func AttachAdminUserPolicy(client awsclient.Client, iamUser *iam.User) (*iam.AttachUserPolicyOutput, error) {

	attachPolicyOutput := &iam.AttachUserPolicyOutput{}
	var err error
	for i := 0; i < 100; i++ {
		time.Sleep(500 * time.Millisecond)
		attachPolicyOutput, err = client.AttachUserPolicy(&iam.AttachUserPolicyInput{
			UserName:  iamUser.UserName,
			PolicyArn: aws.String("arn:aws:iam::aws:policy/AdministratorAccess"),
		})
		if err == nil {
			break
		}
	}
	if err != nil {
		return &iam.AttachUserPolicyOutput{}, err
	}

	return attachPolicyOutput, nil
}

// CreateUserAccessKey creates a new IAM Access Key in AWS and returns aws.CreateAccessKeyOutput struct containing access key and secret
func CreateUserAccessKey(client awsclient.Client, iamUser *iam.User) (*iam.CreateAccessKeyOutput, error) {
	var result *iam.CreateAccessKeyOutput
	var err error

	// Default is 1/10 of a second, but any retries we need to make should be delayed a few seconds
	// This also defaults to an exponential backoff, so we only need to try ~5 times, default is 10
	retry.DefaultDelay = 3 * time.Second
	retry.DefaultAttempts = uint(5)
	err = retry.Do(
		func() (err error) {
			// Create new access key for user
			result, err = client.CreateAccessKey(
				&iam.CreateAccessKeyInput{
					UserName: iamUser.UserName,
				},
			)
			return err
		},

		// Retry if we receive some specific errors: access denied, rate limit or server-side error
		retry.RetryIf(retryIfAwsServiceFailureOrInvalidToken),
	)

	if err != nil {
		return &iam.CreateAccessKeyOutput{}, err
	}

	return result, nil
}

// BuildIAMUser creates and initializes all resources needed for a new IAM user
// Takes a logger, an AWS client, an Account CR, the desired IAM username and a namespace to create resources in
func (r *ReconcileAccount) BuildIAMUser(reqLogger logr.Logger, awsClient awsclient.Client, account *awsv1alpha1.Account, iamUserName string, nameSpace string) (*string, error) {
	var iamUserSecretName string
	var createdIAMUser *iam.User

	// Check if IAM User exists for this account
	iamUserExists, iamUserExistsOutput, err := awsclient.CheckIAMUserExists(reqLogger, awsClient, iamUserName)
	if err != nil {
		return nil, err
	}

	// Get list of managed tags.
	managedTags := r.getManagedTags(reqLogger)
	customTags := r.getCustomTags(reqLogger, account)

	// Create IAM user in AWS if it doesn't exist
	if iamUserExists {
		// If user exists extract iam.User pointer
		createdIAMUser = iamUserExistsOutput.User
	} else {
		CreateUserOutput, err := awsclient.CreateIAMUser(reqLogger, awsClient, account, iamUserName, managedTags, customTags)
		// Err is handled within the function and returns a error message
		if err != nil {
			return nil, err
		}

		// Extract iam.User as pointer
		createdIAMUser = CreateUserOutput.User
	}

	// Determine the kubernetes secret name as its different if the IAM user is osdManagedAdminSRE
	if isIAMUserOsdManagedAdminSRE(createdIAMUser.UserName) {
		// Use iamUserNameSRE constant here to ensure we don't double up on suffix for secret name
		iamUserSecretName = createIAMUserSecretName(fmt.Sprintf("%s-%s", account.Name, iamUserNameSRE))
	} else {
		iamUserSecretName = createIAMUserSecretName(account.Name)
	}

	reqLogger.Info(fmt.Sprintf("Attaching Admin Policy to IAM user %s", aws.StringValue(createdIAMUser.UserName)))

	// Setting IAM user policy
	_, err = AttachAdminUserPolicy(awsClient, createdIAMUser)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to attach admin policy to IAM user %s", aws.StringValue(createdIAMUser.UserName))
		reqLogger.Error(err, errMsg)
		return nil, err
	}

	reqLogger.Info(fmt.Sprintf("Creating Secrets for IAM user %s", aws.StringValue(createdIAMUser.UserName)))

	// Create a NamespacedName for the secret
	secretNamespacedName := types.NamespacedName{Name: iamUserSecretName, Namespace: nameSpace}

	secretExists, err := r.DoesSecretExist(secretNamespacedName)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Unable check if secret: %s exists", secretNamespacedName.String()))
		return nil, err
	}

	if !secretExists {
		iamAccessKeyOutput, err := r.RotateIAMAccessKeys(reqLogger, awsClient, account, createdIAMUser)
		if err != nil {
			errMsg := fmt.Sprintf("Unable to rotate access keys for IAM user: %s", aws.StringValue(createdIAMUser.UserName))
			reqLogger.Error(err, errMsg)
			return nil, err
		}

		err = r.createIAMUserSecret(reqLogger, account, secretNamespacedName, iamAccessKeyOutput)
		if err != nil {
			errMsg := fmt.Sprintf("Unable to create secret: %s", secretNamespacedName.Name)
			reqLogger.Error(err, errMsg)
			return nil, err
		}
	}

	// Return secret name
	return &iamUserSecretName, nil
}

// RotateIAMAccessKeys will delete all AWS access keys assigned to the user and recreate them
func (r *ReconcileAccount) RotateIAMAccessKeys(reqLogger logr.Logger, awsClient awsclient.Client, account *awsv1alpha1.Account, iamUser *iam.User) (*iam.CreateAccessKeyOutput, error) {

	// Delete all current access keys
	err := deleteAllAccessKeys(awsClient, iamUser)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Failed to delete IAM access keys for %s", aws.StringValue(iamUser.UserName)))
		return nil, err
	}
	// Create new access key
	accessKeyOutput, err := CreateUserAccessKey(awsClient, iamUser)
	if err != nil {
		reqLogger.Error(err, "failed to create IAM access key", "IAMUser", iamUser.UserName)
		return nil, err
	}

	return accessKeyOutput, nil
}

// createIAMUserSecret creates a K8s secret from iam.createAccessKeyOuput and sets the owner reference to the controller
func (r *ReconcileAccount) createIAMUserSecret(reqLogger logr.Logger, account *awsv1alpha1.Account, secretName types.NamespacedName, createAccessKeyOutput *iam.CreateAccessKeyOutput) error {

	// Fill in the secret data
	userSecretData := map[string][]byte{
		"aws_user_name":         []byte(*createAccessKeyOutput.AccessKey.UserName),
		"aws_access_key_id":     []byte(*createAccessKeyOutput.AccessKey.AccessKeyId),
		"aws_secret_access_key": []byte(*createAccessKeyOutput.AccessKey.SecretAccessKey),
	}

	// Create new secret
	iamUserSecret := CreateSecret(secretName.Name, secretName.Namespace, userSecretData)

	// Set controller as owner of secret
	if err := controllerutil.SetControllerReference(account, iamUserSecret, r.scheme); err != nil {
		return err
	}

	// Return nil or err if we're unable to create the k8s secret
	return r.CreateSecret(reqLogger, account, iamUserSecret)
}

// DoesSecretExist checks to see if a given secret exists
func (r *ReconcileAccount) DoesSecretExist(namespacedName types.NamespacedName) (bool, error) {

	secret := &corev1.Secret{}
	err := r.Client.Get(context.TODO(), namespacedName, secret)
	if err != nil {
		if k8serr.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

// isIAMUserOsdManagedAdminSRE returns true if the username begins with osdManagedAdminSRE
func isIAMUserOsdManagedAdminSRE(userName *string) bool {
	return strings.HasPrefix(*userName, iamUserNameSRE)
}

// createIAMUserSecretName returns a lower case concatinated string of the input separated by "-"
func createIAMUserSecretName(account string) string {
	suffix := "secret"
	return strings.ToLower(fmt.Sprintf("%s-%s", account, suffix))
}
