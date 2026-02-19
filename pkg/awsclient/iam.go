package awsclient

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/smithy-go"
	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/utils"
)

// ListIAMUserTags returns a list of the tags assigned to an IAM user in AWS
func ListIAMUserTags(reqLogger logr.Logger, client Client, userName string) (*iam.ListUserTagsOutput, error) {
	input := &iam.ListUserTagsInput{
		UserName: aws.String(userName),
	}

	result, err := client.ListUserTags(context.TODO(), input)
	if err != nil {
		// Check for specific IAM exception types
		var noSuchEntityErr *types.NoSuchEntityException
		var serviceFailureErr *types.ServiceFailureException

		switch {
		case errors.As(err, &noSuchEntityErr):
			fmt.Println("NoSuchEntity", noSuchEntityErr.ErrorMessage())
		case errors.As(err, &serviceFailureErr):
			fmt.Println("ServiceFailure", serviceFailureErr.ErrorMessage())
		default:
			var apiErr smithy.APIError
			if errors.As(err, &apiErr) {
				fmt.Println(apiErr.ErrorMessage())
			} else {
				fmt.Println(err.Error())
			}
		}
		return result, err
	}

	return result, nil
}

// ListIAMUsers returns a types.User list of users from the current account
func ListIAMUsers(reqLogger logr.Logger, client Client) ([]types.User, error) {
	input := &iam.ListUsersInput{}
	// List of IAM users to return
	iamUserList := []types.User{}

	err := client.ListUsersPages(context.TODO(), input,
		func(page *iam.ListUsersOutput, lastPage bool) bool {
			iamUserList = append(iamUserList, page.Users...)
			return !lastPage
		})

	if err != nil {
		// Check for specific IAM exception types
		var serviceFailureErr *types.ServiceFailureException
		if errors.As(err, &serviceFailureErr) {
			msg := "Service failure exception"
			utils.LogAwsError(reqLogger, msg, nil, err)
			return iamUserList, err
		}

		// Unexpected error
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			msg := "Unexpected AWS error"
			utils.LogAwsError(reqLogger, msg, nil, err)
		} else {
			utils.LogAwsError(reqLogger, "Unexpected error when listing IAM users", nil, err)
			fmt.Println(err.Error())
		}
		return iamUserList, err
	}

	return iamUserList, nil

}

// CheckIAMUserExists checks if a given IAM user exists within an account
// Takes a logger, an AWS client for the target account, and a target IAM username
func CheckIAMUserExists(reqLogger logr.Logger, client Client, userName string) (bool, *iam.GetUserOutput, error) {
	// Retry when getting IAM user information
	// Sometimes we see a delay before credentials are ready to be user resulting in the AWS API returning 404's
	var iamGetUserOutput *iam.GetUserOutput
	var err error

	for i := 0; i < 10; i++ {
		// check if username exists for this account
		iamGetUserOutput, err = client.GetUser(context.TODO(), &iam.GetUserInput{
			UserName: aws.String(userName),
		})

		// handle errors
		if err != nil {
			// Check for specific IAM exception types
			var noSuchEntityErr *types.NoSuchEntityException
			if errors.As(err, &noSuchEntityErr) {
				return false, nil, nil
			}

			// Check for generic AWS auth errors (no typed exceptions)
			var apiErr smithy.APIError
			if errors.As(err, &apiErr) {
				switch apiErr.ErrorCode() {
				case "InvalidClientTokenId":
					invalidTokenMsg := fmt.Sprintf("Invalid Token error from AWS when attempting get IAM user %s, trying again", userName)
					reqLogger.Info(invalidTokenMsg)
					if i == 10 {
						return false, nil, awsv1alpha1.ErrInvalidToken
					}
				case "AccessDenied":
					checkUserMsg := fmt.Sprintf("AWS Error while checking IAM user %s exists, trying again", userName)
					utils.LogAwsError(reqLogger, checkUserMsg, nil, err)
					// We may have bad credentials so return an error if so
					if i == 10 {
						return false, nil, err
					}
				default:
					utils.LogAwsError(reqLogger, "checkIAMUserExists: Unexpected AWS Error when checking IAM user exists", nil, err)
					return false, nil, awsv1alpha1.ErrAccessDenied
				}
				time.Sleep(time.Duration(time.Duration(i*5) * time.Second))
			} else {
				return false, nil, fmt.Errorf("unable to check if user %s exists error: %s", userName, err)
			}
		} else {
			break
		}
	}

	// User exists return
	return true, iamGetUserOutput, nil
}

// CreateIAMUser creates a new IAM user in the target AWS account
func CreateIAMUser(reqLogger logr.Logger, client Client, account *awsv1alpha1.Account, userName string, managedTags []AWSTag, customTags []AWSTag) (*iam.CreateUserOutput, error) {
	var createUserOutput = &iam.CreateUserOutput{}
	var err error

	for i := 0; i < 10; i++ {

		createUserOutput, err = client.CreateUser(context.TODO(), &iam.CreateUserInput{
			UserName: aws.String(userName),
			Tags:     AWSTags.BuildTags(account, managedTags, customTags).GetIAMTags(),
		})

		// handle errors
		if err != nil {
			// Check for specific IAM exception types
			var entityExistsErr *types.EntityAlreadyExistsException
			if errors.As(err, &entityExistsErr) {
				// createUserOutput inconsistently returns "InvalidClientTokenId" if that happens then the next call to
				// create the user will fail with EntityAlreadyExists. Since we verify the user doesn't exist before this
				// loop we can safely assume we created the user on our first loop.
				invalidTokenMsg := fmt.Sprintf("IAM User %s was created", userName)
				reqLogger.Info(invalidTokenMsg)
				return &iam.CreateUserOutput{}, err
			}

			// Check for generic AWS auth errors (no typed exceptions)
			var apiErr smithy.APIError
			if errors.As(err, &apiErr) {
				switch apiErr.ErrorCode() {
				// Since we're using the same credentials to create the user as we did to check if the user exists
				// we can continue to try without returning, also the outer loop will eventually return
				case "InvalidClientTokenId":
					invalidTokenMsg := fmt.Sprintf("Invalid Token error from AWS when attempting to create user %s, trying again", userName)
					reqLogger.Info(invalidTokenMsg)
					if i == 10 {
						return &iam.CreateUserOutput{}, err
					}
				case "AccessDenied":
					reqLogger.Info("Attempt to create user is Unauthorized. Trying Again due to AWS Eventual Consistency")
					if i == 10 {
						return &iam.CreateUserOutput{}, err
					}
				default:
					utils.LogAwsError(reqLogger, "CreateIAMUser: Unexpected AWS Error during creation of IAM user", nil, err)
					return &iam.CreateUserOutput{}, err
				}
				time.Sleep(time.Duration(time.Duration(i*5) * time.Second))
			} else {
				return &iam.CreateUserOutput{}, err
			}
		} else {
			break
		}
	}

	return createUserOutput, err
}

// ListIAMRoles returns a types.Role list of roles in the AWS account
func ListIAMRoles(reqLogger logr.Logger, client Client) ([]types.Role, error) {

	// List of IAM roles to return
	iamRoleList := []types.Role{}
	var marker *string

	for {
		output, err := client.ListRoles(context.TODO(), &iam.ListRolesInput{Marker: marker})
		if err != nil {
			return nil, err
		}

		iamRoleList = append(iamRoleList, output.Roles...)

		if output.IsTruncated {
			marker = output.Marker
		} else {
			return iamRoleList, nil
		}
	}
}
