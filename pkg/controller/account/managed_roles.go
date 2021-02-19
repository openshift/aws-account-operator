package account

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/controller/utils"
)

const BackplaneAccessRoleKey = "Backplane-Access-Arn"

func (r *ReconcileAccount) SyncManagedRoles(log logr.Logger, account *awsv1alpha1.Account, awsClient awsclient.Client) error {
	log.Info("Syncing Managed Roles on account.")

	managedRoles := &awsv1alpha1.AWSFederatedRoleList{}
	r.Client.List(context.TODO(), managedRoles)

	var roles []awsv1alpha1.AWSFederatedRole
	for _, role := range managedRoles.Items {
		if role.Spec.Managed {
			roles = append(roles, role)
		}
	}
	log.Info("Roles for Account", "roles", roles)
	// Create any Roles that exist
	// Get list of Roles from AWS for Account
	// Remove any Roles that are tagged as aao-managed and exist (also check role name for -prefix)
	return nil
}

// CreateRole creates the role with the correct assume policy for BYOC for a given roleName
func CreateRole(log logr.Logger, byocRole string, accessArnList []string, byocAWSClient awsclient.Client, tags []*iam.Tag) (string, error) {
	assumeRolePolicyDoc := struct {
		Version   string
		Statement []awsStatement
	}{
		Version: "2012-10-17",
		Statement: []awsStatement{{
			Effect: "Allow",
			Action: []string{"sts:AssumeRole"},
			Principal: &awsv1alpha1.Principal{
				AWS: accessArnList,
			},
		}},
	}

	// Convert role to JSON
	jsonAssumeRolePolicyDoc, err := json.Marshal(&assumeRolePolicyDoc)
	if err != nil {
		return "", err
	}

	log.Info(fmt.Sprintf("Creating role: %s", byocRole))
	createRoleOutput, err := byocAWSClient.CreateRole(&iam.CreateRoleInput{
		Tags:                     tags,
		RoleName:                 aws.String(byocRole),
		Description:              aws.String("AdminAccess for BYOC"),
		AssumeRolePolicyDocument: aws.String(string(jsonAssumeRolePolicyDoc)),
	})
	if err != nil {
		return "", err
	}

	// Successfully created role gets a unique identifier
	return *createRoleOutput.Role.RoleId, nil
}

// CreatePolicy creates a policy with tags based on a passed-in roleCR.
func CreatePolicy(log logr.Logger, awsClient awsclient.Client, roleCR *awsv1alpha1.AWSFederatedRole, tags []*iam.Tag) (string, error) {
	policyJSON, err := utils.MarshalIAMPolicy(*roleCR)
	if err != nil {
		log.Error(err, "Failed marshalling IAM Policy")
		return "", err
	}

	createOutput, err := awsClient.CreatePolicy(&iam.CreatePolicyInput{
		Description:    &roleCR.Spec.AWSCustomPolicy.Description,
		PolicyName:     aws.String(roleCR.Spec.ManagedRoleName),
		PolicyDocument: &policyJSON,
	})
	if err != nil {
		log.Error(err, "Unexpected Error creating Policy")
		return "", err
	}

	return *createOutput.Policy.Arn, nil
}
