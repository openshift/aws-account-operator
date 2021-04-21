package account

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
)

// Create AWSManaged read only role
func CreateAWSManagedRole(reqLogger logr.Logger, setupClient awsclient.Client, client awsclient.Client, policyArn string, tags []*iam.Tag, SREAccessARN string) (string, error) {
	reqLogger.Info("Creating AWSManagedRole")

	roleComplete := false
	getUserOutput, err := setupClient.GetUser(&iam.GetUserInput{})
	if err != nil {
		reqLogger.Error(err, "Failed to get non-BYOC IAM User info")
		return "", err
	}
	principalARN := *getUserOutput.User.Arn
	accessArnList := []string{principalARN, SREAccessARN}

	var roleID string
	existingRole, err := GetExistingRole(reqLogger, awsv1alpha1.AWSManagedRoleName, client)
	if err != nil {
		return roleID, err
	}

	if (*existingRole != iam.GetRoleOutput{}) {
		reqLogger.Info(fmt.Sprintf("Found pre-existing role: %s", awsv1alpha1.AWSManagedRoleName))
		reqLogger.Info("Verifying role policies are correct")
		roleID = *existingRole.Role.RoleId
		// existingRole is not empty
		policyList, err := GetAttachedPolicies(reqLogger, awsv1alpha1.AWSManagedRoleName, client)
		if err != nil {
			return roleID, err
		}

		for _, policy := range policyList.AttachedPolicies {
			if policy.PolicyArn != &policyArn {
				reqLogger.Info("Found undesired policy, attempting removal")
			} else {
				reqLogger.Info(fmt.Sprintf("Role already contains correct policy: %s", *policy.PolicyArn))
				roleComplete = true
			}
			err := DetachPolicyFromRole(reqLogger, policy, awsv1alpha1.AWSManagedRoleName, client)
			if err != nil {
				return roleID, err
			}
		}
	}

	if roleComplete {
		return roleID, nil
	}

	if roleID == "" {
		// Create the base role
		roleID, err = CreateRole(reqLogger, awsv1alpha1.AWSManagedRoleName, accessArnList, client, tags)
		if err != nil {
			return roleID, err
		}
	}
	reqLogger.Info(fmt.Sprintf("New RoleID created: %s", roleID))

	reqLogger.Info(fmt.Sprintf("Attaching policy %s to role %s", policyArn, awsv1alpha1.AWSManagedRoleName))
	// Attach the specified policy to the BYOC role
	_, attachErr := client.AttachRolePolicy(&iam.AttachRolePolicyInput{
		RoleName:  aws.String(awsv1alpha1.AWSManagedRoleName),
		PolicyArn: aws.String(policyArn),
	})

	if attachErr != nil {
		return roleID, attachErr
	}

	reqLogger.Info(fmt.Sprintf("Checking if policy %s has been attached", policyArn))

	// Attaching the policy suffers from an eventual consistency problem
	policyList, listErr := GetAttachedPolicies(reqLogger, awsv1alpha1.AWSManagedRoleName, client)
	if listErr != nil {
		return roleID, err
	}

	for _, policy := range policyList.AttachedPolicies {
		if *policy.PolicyArn == policyArn {
			reqLogger.Info(fmt.Sprintf("Found attached policy %s", *policy.PolicyArn))
			break
		} else {
			err = fmt.Errorf("Policy %s never attached to role %s", policyArn, awsv1alpha1.AWSManagedRoleName)
			return roleID, err
		}
	}

	return roleID, err
}
