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
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/config"
	"github.com/openshift/aws-account-operator/pkg/utils"
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

var (
	defaultDelay      = 3 * time.Second
	defaultSleepDelay = 500 * time.Millisecond
	// testSleepModifier is set to 0 in tests so that tests don't sleep and cause a slowdown
	testSleepModifier int = 1
)

// CreateSecret creates a secret for placing IAM Credentials
// Takes a logger, the desired name of the secret, the Account CR
// that will own the secret, and pointer to an empty secret object to fill
func (r *AccountReconciler) CreateSecret(reqLogger logr.Logger, account *awsv1alpha1.Account, secret *corev1.Secret) error {

	// Set controller as owner of secret
	if err := controllerutil.SetControllerReference(account, secret, r.Scheme); err != nil {
		return err
	}

	createErr := r.Client.Create(context.TODO(), secret)
	if createErr != nil {
		failedToCreateUserSecretMsg := fmt.Sprintf("Failed to create secret %s", secret.Name)
		utils.SetAccountStatus(account, failedToCreateUserSecretMsg, awsv1alpha1.AccountFailed, "Failed")
		err := r.Client.Status().Update(context.TODO(), account)
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
		time.Sleep(defaultSleepDelay)
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
	retry.DefaultDelay = defaultDelay
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
	retry.DefaultDelay = defaultDelay
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
				time.Sleep(time.Duration(time.Duration(attempt*5*testSleepModifier) * time.Second))
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
		time.Sleep(defaultSleepDelay)
		attachPolicyOutput, err = client.AttachUserPolicy(&iam.AttachUserPolicyInput{
			UserName:  iamUser.UserName,
			PolicyArn: aws.String(config.GetIAMArn("aws", config.AwsResourceTypePolicy, config.AwsResourceIDAdministratorAccessRole)),
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

func attachAndEnsureRolePolicies(reqLogger logr.Logger, client awsclient.Client, roleName string, policyArn string) error {
	reqLogger.Info(fmt.Sprintf("Attaching policy %s to role %s", policyArn, roleName))
	// Attach the specified policy to the Role
	_, attachErr := client.AttachRolePolicy(&iam.AttachRolePolicyInput{
		RoleName:  aws.String(roleName),
		PolicyArn: aws.String(policyArn),
	})

	if attachErr != nil {
		return attachErr
	}

	reqLogger.Info(fmt.Sprintf("Checking if policy %s has been attached", policyArn))

	// Attaching the policy suffers from an eventual consistency problem
	policyList, err := GetAttachedPolicies(reqLogger, roleName, client)
	if err != nil {
		return err
	}

	for _, policy := range policyList.AttachedPolicies {
		if *policy.PolicyArn == policyArn {
			reqLogger.Info(fmt.Sprintf("Found attached policy %s", *policy.PolicyArn))
			break
		} else {
			err = fmt.Errorf("policy %s never attached to role %s", policyArn, roleName)
			return err
		}
	}

	return nil
}

// CreateUserAccessKey creates a new IAM Access Key in AWS and returns aws.CreateAccessKeyOutput struct containing access key and secret
func CreateUserAccessKey(client awsclient.Client, iamUser *iam.User) (*iam.CreateAccessKeyOutput, error) {
	var result *iam.CreateAccessKeyOutput
	var err error

	// Default is 1/10 of a second, but any retries we need to make should be delayed a few seconds
	// This also defaults to an exponential backoff, so we only need to try ~5 times, default is 10
	retry.DefaultDelay = defaultDelay
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
func (r *AccountReconciler) BuildIAMUser(reqLogger logr.Logger, awsClient awsclient.Client, account *awsv1alpha1.Account, iamUserName string, nameSpace string) (*string, error) {
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

	iamUserSecretName = createIAMUserSecretName(account.Name)

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

func CleanUpIAM(reqLogger logr.Logger, awsClient awsclient.Client, accountCR *awsv1alpha1.Account) error {

	// We delete user policies, access keys and finally the IAM user themselves.
	if err := deleteIAMUsers(reqLogger, awsClient, accountCR); err != nil {
		return fmt.Errorf("failed deleting IAM users: %v", err)
	}

	// If user deletion is successful we can then clean role policies and roles.
	if err := cleanIAMRoles(reqLogger, awsClient, accountCR); err != nil {
		return fmt.Errorf("failed cleaning IAM roles: %v", err)
	}

	return nil
}

func deleteIAMUser(reqLogger logr.Logger, awsClient awsclient.Client, user *iam.User) error {
	var err error
	// Detach User Policies
	if err = detachUserPolicies(awsClient, user); err != nil {
		return fmt.Errorf("failed to detach user policies: %v", err)
	}

	// Detach User Access Keys
	if err = deleteAllAccessKeys(awsClient, user); err != nil {
		return fmt.Errorf("failed to delete all access keys: %v", err)
	}

	// Default is 1/10 of a second, but any retries we need to make should be delayed a few seconds
	// This also defaults to an exponential backoff, so we only need to try ~5 times, default is 10
	retry.DefaultDelay = defaultDelay
	retry.DefaultAttempts = uint(5)
	err = retry.Do(
		func() (err error) {
			_, err = awsClient.DeleteUser(&iam.DeleteUserInput{UserName: user.UserName})
			return err
		},

		// Retry if we receive some specific errors: access denied, rate limit or server-side error
		retry.RetryIf(retryIfAwsServiceFailureOrInvalidToken),
	)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("unable to delete IAM user %s", *user.UserName), err)
	}

	return nil
}

// listIAMUsers func pointer is required in order to patch this func for testing purposes.
var (
	listIAMUsers = awsclient.ListIAMUsers
)

func deleteIAMUsers(reqLogger logr.Logger, awsClient awsclient.Client, accountCR *awsv1alpha1.Account) error {
	reqLogger.Info("Cleaning up IAM users")

	users, err := listIAMUsers(reqLogger, awsClient)
	if err != nil {
		return fmt.Errorf("failed to list aws iam users: %v", err)
	}

	for _, user := range users {
		clusterNameTag := false
		clusterNamespaceTag := false
		getUser, err := awsClient.GetUser(&iam.GetUserInput{UserName: user.UserName})
		if err != nil {
			return fmt.Errorf("failed to get aws user: %v", err)
		}
		user = getUser.User
		for _, tag := range user.Tags {
			if *tag.Key == awsv1alpha1.ClusterAccountNameTagKey && *tag.Value == accountCR.Name {
				clusterNameTag = true
			}
			if *tag.Key == awsv1alpha1.ClusterNamespaceTagKey && *tag.Value == accountCR.Namespace {
				clusterNamespaceTag = true
			}
		}
		if clusterNameTag && clusterNamespaceTag {
			err = deleteIAMUser(reqLogger, awsClient, user)
			if err != nil {
				return err
			}
		} else {
			reqLogger.Info(fmt.Sprintf("not deleting user: %s", *user.UserName))
		}
	}
	return nil
}

func cleanIAMRole(reqLogger logr.Logger, awsClient awsclient.Client, role *iam.Role) error {
	// remove attached policies from the role before deletion
	if err := detachRolePolicies(awsClient, *role.RoleName); err != nil {
		return fmt.Errorf("failed to detach role policies: %v", err)
	}

	_, err := awsClient.DeleteRole(&iam.DeleteRoleInput{RoleName: role.RoleName})
	reqLogger.Info(fmt.Sprintf("Deleting IAM role: %s", *role.RoleName))
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("unable to delete IAM role %s", *role.RoleName), err)
	}

	return nil
}

func cleanIAMRoles(reqLogger logr.Logger, awsClient awsclient.Client, accountCR *awsv1alpha1.Account) error {
	reqLogger.Info("Cleaning up IAM roles")
	roles, err := awsclient.ListIAMRoles(reqLogger, awsClient)
	if err != nil {
		return err
	}

	for _, role := range roles {
		clusterNameTag := false
		clusterNamespaceTag := false
		getRole, err := awsClient.GetRole(&iam.GetRoleInput{RoleName: role.RoleName})
		if err != nil {
			return err
		}
		role = getRole.Role

		for _, tag := range role.Tags {
			if *tag.Key == awsv1alpha1.ClusterAccountNameTagKey && *tag.Value == accountCR.Name {
				clusterNameTag = true
			}
			if *tag.Key == awsv1alpha1.ClusterNamespaceTagKey && *tag.Value == accountCR.Namespace {
				clusterNamespaceTag = true
			}
		}

		if clusterNameTag && clusterNamespaceTag {
			err = cleanIAMRole(reqLogger, awsClient, role)
			if err != nil {
				return err
			}
		} else {
			reqLogger.Info(fmt.Sprintf("Not deleting role: %s", *getRole.Role.RoleName))
		}
	}

	return nil
}

// Detach User Policies
func detachUserPolicies(awsClient awsclient.Client, user *iam.User) error {
	attachedUserPolicies, err := awsClient.ListAttachedUserPolicies(&iam.ListAttachedUserPoliciesInput{UserName: user.UserName})
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("unable to list IAM user policies from user %s", *user.UserName), err)
	}

	for _, attachedPolicy := range attachedUserPolicies.AttachedPolicies {
		_, err := awsClient.DetachUserPolicy(&iam.DetachUserPolicyInput{UserName: user.UserName, PolicyArn: attachedPolicy.PolicyArn})
		if err != nil {
			return fmt.Errorf(fmt.Sprintf("unable to detach IAM user policy from user %s", *user.UserName), err)
		}
	}

	return nil
}

// Detaches all policies from the role
func detachRolePolicies(awsClient awsclient.Client, roleName string) error {
	attachedRolePolicies, err := awsClient.ListAttachedRolePolicies(&iam.ListAttachedRolePoliciesInput{RoleName: &roleName})
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("unable to list IAM role policies from role %s", roleName), err)
	}

	for _, attachedPolicy := range attachedRolePolicies.AttachedPolicies {
		_, err := awsClient.DetachRolePolicy(&iam.DetachRolePolicyInput{
			PolicyArn: attachedPolicy.PolicyArn,
			RoleName:  &roleName,
		})
		if err != nil {
			return fmt.Errorf(fmt.Sprintf("unable to detach IAM role policy from role %s", roleName), err)
		}
	}

	return nil
}

// RotateIAMAccessKeys will delete all AWS access keys assigned to the user and recreate them
func (r *AccountReconciler) RotateIAMAccessKeys(reqLogger logr.Logger, awsClient awsclient.Client, account *awsv1alpha1.Account, iamUser *iam.User) (*iam.CreateAccessKeyOutput, error) {

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
func (r *AccountReconciler) createIAMUserSecret(reqLogger logr.Logger, account *awsv1alpha1.Account, secretName types.NamespacedName, createAccessKeyOutput *iam.CreateAccessKeyOutput) error {

	// Fill in the secret data
	userSecretData := map[string][]byte{
		"aws_user_name":         []byte(*createAccessKeyOutput.AccessKey.UserName),
		"aws_access_key_id":     []byte(*createAccessKeyOutput.AccessKey.AccessKeyId),
		"aws_secret_access_key": []byte(*createAccessKeyOutput.AccessKey.SecretAccessKey),
	}

	// Create new secret
	iamUserSecret := CreateSecret(secretName.Name, secretName.Namespace, userSecretData)

	// Set controller as owner of secret
	if err := controllerutil.SetControllerReference(account, iamUserSecret, r.Scheme); err != nil {
		return err
	}

	// Return nil or err if we're unable to create the k8s secret
	return r.CreateSecret(reqLogger, account, iamUserSecret)
}

// DoesSecretExist checks to see if a given secret exists
func (r *AccountReconciler) DoesSecretExist(namespacedName types.NamespacedName) (bool, error) {

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

// createIAMUserSecretName returns a lower case concatenated string of the input separated by "-"
func createIAMUserSecretName(account string) string {
	suffix := "secret"
	return strings.ToLower(fmt.Sprintf("%s-%s", account, suffix))
}

func (r *AccountReconciler) createManagedOpenShiftSupportRole(reqLogger logr.Logger, setupClient awsclient.Client, client awsclient.Client, policyArn string, instanceID string, tags []*iam.Tag) (roleID string, err error) {
	reqLogger.Info("Creating ManagedOpenShiftSupportRole")

	getUserOutput, err := setupClient.GetUser(&iam.GetUserInput{})
	if err != nil {
		reqLogger.Error(err, "Failed to get IAM User info")
		return roleID, err
	}

	principalARN := *getUserOutput.User.Arn
	SREAccessARN, err := r.GetSREAccessARN(reqLogger, awsv1alpha1.SupportJumpRole)
	if err != nil {
		reqLogger.Error(err, "Unable to find STS JUMP ROLE in configmap")
		return roleID, err
	}

	accessArnList := []string{principalARN, SREAccessARN}

	managedSupRoleWithID := fmt.Sprintf("%s-%s", awsv1alpha1.ManagedOpenShiftSupportRole, instanceID)

	existingRole, err := GetExistingRole(reqLogger, managedSupRoleWithID, client)
	if err != nil {
		return roleID, err
	}

	roleIsValid := false
	// We found the role already exists, we need to ensure the policies attached are as expected.
	if (*existingRole != iam.GetRoleOutput{}) {
		reqLogger.Info(fmt.Sprintf("Found pre-existing role: %s", managedSupRoleWithID))
		reqLogger.Info("Verifying role policies are correct")
		roleID = *existingRole.Role.RoleId
		// existingRole is not empty
		policyList, err := GetAttachedPolicies(reqLogger, managedSupRoleWithID, client)
		if err != nil {
			return roleID, err
		}

		for _, policy := range policyList.AttachedPolicies {
			if policy.PolicyArn != &policyArn {
				reqLogger.Info("Found undesired policy, attempting removal")
				err := DetachPolicyFromRole(reqLogger, policy, managedSupRoleWithID, client)
				if err != nil {
					return roleID, err
				}
			} else {
				reqLogger.Info(fmt.Sprintf("Role already contains correct policy: %s", *policy.PolicyArn))
				roleIsValid = true
			}
		}
	}

	if roleIsValid {
		return roleID, nil
	}

	// Role doesn't exist, create new role and attach desired Policy.
	if roleID == "" {
		// Create the base role
		roleID, err = CreateRole(reqLogger, managedSupRoleWithID, accessArnList, client, tags)
		if err != nil {
			return roleID, err
		}
	}
	reqLogger.Info(fmt.Sprintf("New RoleID created: %s", roleID))
	err = attachAndEnsureRolePolicies(reqLogger, client, managedSupRoleWithID, policyArn)

	return roleID, err
}

func (r *AccountReconciler) ProbeSecret(reqLogger logr.Logger, currentAcctInstance *awsv1alpha1.Account,
	awsAssumedRoleClient awsclient.Client, iamUserUHC string, nameSpace string) error {

	iamUserSecretName := createIAMUserSecretName(currentAcctInstance.Name)
	kubeSecretNamespacedName := types.NamespacedName{Name: iamUserSecretName, Namespace: nameSpace}
	// Check if secret exist in Kubernetes
	secretExists, err := r.DoesSecretExist(kubeSecretNamespacedName)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Unable check if secret: %s exists", kubeSecretNamespacedName.String()))
		return err
	}

	if !secretExists {
		// If secret doesn't exist, create new one
		secretName, err := r.BuildIAMUser(reqLogger, awsAssumedRoleClient, currentAcctInstance, iamUserUHC, nameSpace)
		if err != nil {
			reason, errType := getBuildIAMUserErrorReason(err)
			errMsg := fmt.Sprintf("Failed to recreate IAM UHC user %s: %s", iamUserUHC, err)
			_, stateErr := r.setAccountFailed(
				reqLogger,
				currentAcctInstance,
				errType,
				reason,
				errMsg,
				AccountFailed,
			)
			if stateErr != nil {
				reqLogger.Error(err, "failed setting account state", "desiredState", AccountFailed)
			}
			return err
		}

		currentAcctInstance.Spec.IAMUserSecret = *secretName
		err = r.accountSpecUpdate(reqLogger, currentAcctInstance)
		if err != nil {
			return err
		}

	} else {
		// If secret exists, check that credentials are valid
		validSecret, err := r.IsKubeSecretValid(reqLogger, currentAcctInstance)
		if err != nil {
			reqLogger.Error(err, "failed secret validation")
		}
		if !validSecret {
			// If credentials aren't valid, make them valid again
			err = r.ValidateIAMSecret(reqLogger, awsAssumedRoleClient, currentAcctInstance, iamUserUHC, kubeSecretNamespacedName)
			if err != nil {
				reason, errType := getBuildIAMUserErrorReason(err)
				errMsg := fmt.Sprintf("Failed to revalidate IAM UHC user %s: %s", iamUserUHC, err)
				_, stateErr := r.setAccountFailed(
					reqLogger,
					currentAcctInstance,
					errType,
					reason,
					errMsg,
					AccountFailed,
				)
				if stateErr != nil {
					reqLogger.Error(err, "failed setting account state", "desiredState", AccountFailed)
				}
				return err
			}
		}
	}

	return nil
}

func (r *AccountReconciler) IsKubeSecretValid(reqLogger logr.Logger, currentAcctInstance *awsv1alpha1.Account) (bool, error) {

	// Build new aws client with credentials inside secret
	awsClient, err := r.awsClientBuilder.GetClient(controllerName, r.Client, awsclient.NewAwsClientInput{
		SecretName: currentAcctInstance.Spec.IAMUserSecret,
		NameSpace:  currentAcctInstance.Namespace,
		AwsRegion:  "us-east-1",
	})
	if err != nil {
		reqLogger.Error(err, "Unable to create aws client")
		return false, err
	}

	// Make aws call to check if credentials are valid
	_, err = awsClient.GetCallerIdentity(&sts.GetCallerIdentityInput{})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case "AccessDenied", "InvalidClientToken":
				reqLogger.Error(err, "invalid credentials provided")
				return false, err
			}
			reqLogger.Error(err, "failed to get caller identity")
			return false, err
		}
	}

	return true, nil
}

func (r *AccountReconciler) ValidateIAMSecret(reqLogger logr.Logger, awsClient awsclient.Client, account *awsv1alpha1.Account,
	iamUserName string, kubeSecretNamespacedName types.NamespacedName) error {

	// Check if osdadmin User exists for this aws account
	iamUserExists, iamUserExistsOutput, err := awsclient.CheckIAMUserExists(reqLogger, awsClient, iamUserName)
	if err != nil {
		return err
	}

	// Get list of managed tags.
	managedTags := r.getManagedTags(reqLogger)
	customTags := r.getCustomTags(reqLogger, account)

	var iamAccessKeyOutput *iam.CreateAccessKeyOutput

	if !iamUserExists {
		// If user doesn't exist, create new IAM user
		CreateUserOutput, err := awsclient.CreateIAMUser(reqLogger, awsClient, account, iamUserName, managedTags, customTags)
		// Err is handled within the function and returns a error message
		if err != nil {
			return err
		}

		// Extract iam.User as pointer
		newIAMUser := CreateUserOutput.User

		reqLogger.Info(fmt.Sprintf("Attaching Admin Policy to IAM user %s", aws.StringValue(newIAMUser.UserName)))

		// Setting IAM user policy
		_, err = AttachAdminUserPolicy(awsClient, newIAMUser)
		if err != nil {
			errMsg := fmt.Sprintf("Failed to attach admin policy to IAM user %s", aws.StringValue(newIAMUser.UserName))
			reqLogger.Error(err, errMsg)
			return err
		}

		// Create new access key
		iamAccessKeyOutput, err = CreateUserAccessKey(awsClient, newIAMUser)
		if err != nil {
			reqLogger.Error(err, "failed to create IAM access key", "IAMUser", newIAMUser.UserName)
			return err
		}
	} else {
		// If user exists extract iam.User pointer and rotate access key
		currentIAMUser := iamUserExistsOutput.User
		iamAccessKeyOutput, err = r.RotateIAMAccessKeys(reqLogger, awsClient, account, currentIAMUser)
		if err != nil {
			errMsg := fmt.Sprintf("Unable to rotate access keys for IAM user: %s", aws.StringValue(currentIAMUser.UserName))
			reqLogger.Error(err, errMsg)
			return err
		}
	}
	// Update access keys on kubernetes secrets
	err = r.updateIAMUserSecret(reqLogger, account, kubeSecretNamespacedName, iamAccessKeyOutput)
	if err != nil {
		errMsg := fmt.Sprintf("Unable to update kubernetes secret: %s", kubeSecretNamespacedName.Name)
		reqLogger.Error(err, errMsg)
		return err
	}

	// Return secret name
	return nil
}

func (r *AccountReconciler) updateIAMUserSecret(reqLogger logr.Logger, account *awsv1alpha1.Account, secretName types.NamespacedName, createAccessKeyOutput *iam.CreateAccessKeyOutput) error {

	// Fill in the secret data
	userSecretData := map[string][]byte{
		"aws_user_name":         []byte(*createAccessKeyOutput.AccessKey.UserName),
		"aws_access_key_id":     []byte(*createAccessKeyOutput.AccessKey.AccessKeyId),
		"aws_secret_access_key": []byte(*createAccessKeyOutput.AccessKey.SecretAccessKey),
	}

	// Create new secret
	iamUserSecret := CreateSecret(secretName.Name, secretName.Namespace, userSecretData)

	// Set controller as owner of secret
	if err := controllerutil.SetControllerReference(account, iamUserSecret, r.Scheme); err != nil {
		return err
	}

	// Update secret
	updateErr := r.Client.Update(context.TODO(), iamUserSecret)
	if updateErr != nil {
		failedToUpdateUserSecretMsg := fmt.Sprintf("Failed to update secret %s", iamUserSecret.Name)
		utils.SetAccountStatus(account, failedToUpdateUserSecretMsg, awsv1alpha1.AccountFailed, "Failed")
		err := r.Client.Status().Update(context.TODO(), account)
		if err != nil {
			reqLogger.Error(updateErr, failedToUpdateUserSecretMsg)
			return err
		}
		reqLogger.Info(failedToUpdateUserSecretMsg)
		return updateErr
	}
	reqLogger.Info(fmt.Sprintf("Updated secret %s", iamUserSecret.Name))
	return nil
}
