package account

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	controllerutils "github.com/openshift/aws-account-operator/pkg/controller/utils"
)

const (
	byocPolicy        = "BYOCEC2Policy"
	arnIAMPrefix      = "arn:aws:iam::"
	byocUserArnSuffix = ":user/byocSetupUser"
)

// ErrBYOCAccountIDMissing is an error for missing Account ID
var ErrBYOCAccountIDMissing = errors.New("BYOCAccountIDMissing")

// ErrBYOCSecretRefMissing is an error for missing Secret References
var ErrBYOCSecretRefMissing = errors.New("BYOCSecretRefMissing")

// Placeholder for the unique role id created by createRole
var roleID = ""

// Create role for BYOC IAM user to assume
func createBYOCAdminAccessRole(reqLogger logr.Logger, byocAWSClient awsclient.Client, policyArn string) (roleID string, err error) {
	getUserOutput, err := byocAWSClient.GetUser(&iam.GetUserInput{})
	if err != nil {
		reqLogger.Error(err, "Failed to get BYOC IAM User info")
		return roleID, err
	}

	// Lay out a basic AssumeRolePolicyDocument for BYOC
	assumeRolePolicyDoc := struct {
		Version   string
		Statement []awsStatement
	}{
		Version: "2012-10-17",
		Statement: []awsStatement{{
			Effect: "Allow",
			Action: []string{"sts:AssumeRole"},
			Principal: &awsv1alpha1.Principal{
				AWS: *getUserOutput.User.Arn,
			},
		}},
	}

	// Convert role to JSON
	jsonAssumeRolePolicyDoc, err := json.Marshal(&assumeRolePolicyDoc)
	if err != nil {
		return roleID, err
	}

	// Check if Role already exists
	existingRole, err := byocAWSClient.GetRole(&iam.GetRoleInput{
		RoleName: aws.String(byocRole),
	})

	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case iam.ErrCodeNoSuchEntityException:
				// This is OK and to be expected if the role hasn't been created yet
				reqLogger.Info(fmt.Sprintf("%s role does not yet exist", byocRole))
			case iam.ErrCodeServiceFailureException:
				reqLogger.Error(
					aerr,
					fmt.Sprintf("AWS Internal Server Error (%s) checking for %s role existence: %s", aerr.Code(), byocRole, aerr.Message()),
				)
				return roleID, err
			default:
				// Currently only two errors returned by AWS.  This is a catch-all for any that may appear in the future.
				reqLogger.Error(
					aerr,
					fmt.Sprintf("Unknown error (%s) checking for %s role existence: %s", aerr.Code(), byocRole, aerr.Message()),
				)
				return roleID, err
			}
		} else {
			return roleID, err
		}
	}

	if (*existingRole != iam.GetRoleOutput{}) {
		reqLogger.Info(fmt.Sprintf("Found pre-existing role: %s", byocRole))
		// existingRole is not empty
		listRoleInput := &iam.ListAttachedRolePoliciesInput{
			RoleName: aws.String(byocRole),
		}
		policyList, listErr := byocAWSClient.ListAttachedRolePolicies(listRoleInput)
		if listErr != nil {
			if aerr, ok := listErr.(awserr.Error); ok {
				switch aerr.Code() {
				default:
					reqLogger.Error(
						aerr,
						fmt.Sprintf(aerr.Error()),
					)
					return roleID, err
				}
			} else {
				return roleID, err
			}
		}

		for _, policy := range policyList.AttachedPolicies {
			reqLogger.Info(fmt.Sprintf("Detaching Policy %s from role %s", *policy.PolicyName, byocRole))
			// Must detach the RolePolicy before it can be deleted
			_, err := byocAWSClient.DetachRolePolicy(&iam.DetachRolePolicyInput{
				RoleName:  aws.String(byocRole),
				PolicyArn: aws.String(*policy.PolicyArn),
			})
			if err != nil {
				if aerr, ok := err.(awserr.Error); ok {
					switch aerr.Code() {
					default:
						reqLogger.Error(
							aerr,
							fmt.Sprintf(aerr.Error()),
						)
						reqLogger.Error(err, fmt.Sprintf("%v", err))
						return roleID, err
					}
				}
			}
		}

		reqLogger.Info(fmt.Sprintf("Deleting Role: %s", byocRole))
		_, delErr := byocAWSClient.DeleteRole(&iam.DeleteRoleInput{
			RoleName: aws.String(byocRole),
		})

		// Delete the existing role
		if delErr != nil {
			if aerr, ok := delErr.(awserr.Error); ok {
				switch aerr.Code() {
				default:
					reqLogger.Error(
						aerr,
						fmt.Sprintf(aerr.Error()),
					)
					reqLogger.Error(err, fmt.Sprintf("%v", err))
					return roleID, err
				}
			}
		}
	}

	// Create the base role
	reqLogger.Info(fmt.Sprintf("Creating role: %s", byocRole))
	createRoleOutput, err := byocAWSClient.CreateRole(&iam.CreateRoleInput{
		RoleName:                 aws.String(byocRole),
		Description:              aws.String("AdminAccess for BYOC"),
		AssumeRolePolicyDocument: aws.String(string(jsonAssumeRolePolicyDoc)),
	})
	if err != nil {
		return roleID, err
	}

	// Successfully created role gets a unique identifier
	roleID = *createRoleOutput.Role.RoleId
	reqLogger.Info(fmt.Sprintf("New RoleID: %s", roleID))

	reqLogger.Info(fmt.Sprintf("Attaching policy %s to role %s", policyArn, byocRole))
	// Attach the specified policy to the BYOC role
	_, err = byocAWSClient.AttachRolePolicy(&iam.AttachRolePolicyInput{
		RoleName:  aws.String(byocRole),
		PolicyArn: aws.String(policyArn),
	})

	reqLogger.Info(fmt.Sprintf("Checking if policy %s has been attached", policyArn))
	// Attaching the policy suffers from an eventual consistency problem
	listRoleInput := &iam.ListAttachedRolePoliciesInput{
		RoleName: aws.String(byocRole),
	}
	policyList, listErr := byocAWSClient.ListAttachedRolePolicies(listRoleInput)
	if listErr != nil {
		if aerr, ok := listErr.(awserr.Error); ok {
			switch aerr.Code() {
			default:
				reqLogger.Error(
					aerr,
					fmt.Sprintf(aerr.Error()),
				)
				return roleID, err
			}
		} else {
			return roleID, err
		}
	}

	for _, policy := range policyList.AttachedPolicies {
		if *policy.PolicyArn == policyArn {
			reqLogger.Info(fmt.Sprintf("Found attached policy %s", *policy.PolicyArn))
			break
		} else {
			err = fmt.Errorf("Policy %s never attached to role %s", policyArn, byocRole)
			return roleID, err
		}
	}

	return roleID, err
}

func (r *ReconcileAccount) getBYOCClient(currentAcct *awsv1alpha1.Account) (awsclient.Client, *awsv1alpha1.AccountClaim, error) {
	// Get associated AccountClaim
	accountClaim := &awsv1alpha1.AccountClaim{}

	err := r.Client.Get(context.TODO(),
		types.NamespacedName{Name: currentAcct.Spec.ClaimLink, Namespace: currentAcct.Spec.ClaimLinkNamespace},
		accountClaim)
	if err != nil {
		if k8serr.IsNotFound(err) {
			return nil, nil, err
		}
		return nil, nil, err
	}

	// Get credentials
	byocAWSClient, err := awsclient.GetAWSClient(r.Client, awsclient.NewAwsClientInput{
		SecretName: accountClaim.Spec.BYOCSecretRef.Name,
		NameSpace:  accountClaim.Spec.BYOCSecretRef.Namespace,
		AwsRegion:  "us-east-1",
	})
	if err != nil {
		return nil, accountClaim, err
	}

	return byocAWSClient, accountClaim, nil
}

func (r *ReconcileAccount) byocRotateAccessKeys(reqLogger logr.Logger, byocAWSClient awsclient.Client, accountClaim *awsv1alpha1.AccountClaim) error {

	getBYOCUserOutput, err := byocAWSClient.GetUser(&iam.GetUserInput{})
	if err != nil {
		reqLogger.Error(err, "Failed to get BYOC IAM User info")
		return err
	}

	// Get the BYOC credentials secret to update
	accountClaimSecret := &corev1.Secret{}

	err = r.Client.Get(context.TODO(),
		types.NamespacedName{Name: accountClaim.Spec.BYOCSecretRef.Name, Namespace: accountClaim.Spec.BYOCSecretRef.Namespace},
		accountClaimSecret)

	accessKeyID := string(accountClaimSecret.Data["aws_access_key_id"])

	// List and delete any other access keys
	accessKeyList, err := byocAWSClient.ListAccessKeys(&iam.ListAccessKeysInput{})

	for _, accessKey := range accessKeyList.AccessKeyMetadata {
		if *accessKey.AccessKeyId != accessKeyID {
			_, err = byocAWSClient.DeleteAccessKey(&iam.DeleteAccessKeyInput{AccessKeyId: accessKey.AccessKeyId})
			if err != nil {
				reqLogger.Error(err, "Failed to delete BYOC access keys")
				return err
			}
		}
	}

	// Create new BYOC access keys
	userSecretInfo, err := CreateUserAccessKey(reqLogger, byocAWSClient, *getBYOCUserOutput.User.UserName)
	if err != nil {
		failedToCreateUserAccessKeyMsg := fmt.Sprintf("Failed to create IAM access key for %s", *getBYOCUserOutput.User.UserName)
		reqLogger.Info(failedToCreateUserAccessKeyMsg)
		return err
	}

	// Delete original BYOC access key
	_, err = byocAWSClient.DeleteAccessKey(&iam.DeleteAccessKeyInput{AccessKeyId: &accessKeyID})
	if err != nil {
		reqLogger.Error(err, "Failed to delete BYOC access keys")
		return err
	}

	accountClaimSecret.Data =
		map[string][]byte{
			"aws_access_key_id":     []byte(*userSecretInfo.AccessKey.AccessKeyId),
			"aws_secret_access_key": []byte(*userSecretInfo.AccessKey.SecretAccessKey),
		}

	reqLogger.Info("BYOC updating secret")
	err = r.Client.Update(context.TODO(), accountClaimSecret)
	if err != nil {
		reqLogger.Error(err, "Failed to update BYOC access keys")
		return err
	}

	return nil
}

func (r *ReconcileAccount) accountClaimBYOCError(reqLogger logr.Logger, accountClaim *awsv1alpha1.AccountClaim, claimError error) {

	message := fmt.Sprintf("BYOC Account Failed: %+v", claimError)
	accountClaim.Status.Conditions = controllerutils.SetAccountClaimCondition(
		accountClaim.Status.Conditions,
		awsv1alpha1.AccountClaimed,
		corev1.ConditionTrue,
		"AccountFailed",
		message,
		controllerutils.UpdateConditionNever)
	accountClaim.Status.State = awsv1alpha1.ClaimStatusError
	err := r.Client.Status().Update(context.TODO(), accountClaim)
	if err != nil {
		reqLogger.Error(err, "Error updating BYOC Account Claim")
	}
}

func validateBYOCClaim(accountClaim *awsv1alpha1.AccountClaim) error {

	if accountClaim.Spec.BYOCAWSAccountID == "" {
		return ErrBYOCAccountIDMissing
	}
	if accountClaim.Spec.BYOCSecretRef.Name == "" || accountClaim.Spec.BYOCSecretRef.Namespace == "" {
		return ErrBYOCSecretRefMissing
	}

	return nil
}
