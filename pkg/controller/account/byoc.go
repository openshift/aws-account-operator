package account

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/controller/utils"
)

// BYOC Accounts are determined by having no state set OR not being claimed
// Returns true if either are true AND Spec.BYOC is true
func newBYOCAccount(currentAcctInstance *awsv1alpha1.Account) bool {
	if currentAcctInstance.IsBYOC() {
		if !currentAcctInstance.HasState() || !currentAcctInstance.IsClaimed() {
			return true
		}
	}
	return false
}

// Checks whether or not the current account instance is claimed, and does so if not
func claimBYOCAccount(r *ReconcileAccount, reqLogger logr.Logger, currentAcctInstance *awsv1alpha1.Account) error {
	if !currentAcctInstance.IsClaimed() {
		reqLogger.Info("Marking BYOC account claimed")
		currentAcctInstance.Status.Claimed = true
		return r.statusUpdate(currentAcctInstance)
	}

	return nil
}

func (r *ReconcileAccount) initializeNewCCSAccount(reqLogger logr.Logger, account *awsv1alpha1.Account) (reconcile.Result, error) {
	accountClaim, acctClaimErr := r.getAccountClaim(account)
	if acctClaimErr != nil {
		// TODO: Unrecoverable
		// TODO: set helpful error message
		if accountClaim != nil {
			utils.SetAccountClaimStatus(
				accountClaim,
				"Failed to get AccountClaim for CSS account",
				"FailedRetrievingAccountClaim",
				awsv1alpha1.ClientError,
				awsv1alpha1.ClaimStatusError,
			)
			err := r.Client.Status().Update(context.TODO(), accountClaim)
			if err != nil {
				reqLogger.Error(err, "failed to update accountclaim status")
			}
		} else {
			reqLogger.Error(acctClaimErr, "accountclaim is nil")
		}
		return reconcile.Result{}, acctClaimErr
	}

	validateErr := accountClaim.Validate()
	if validateErr != nil {
		// Figure the reason for our failure
		errReason := validateErr.Error()
		// Update AccountClaim status
		utils.SetAccountClaimStatus(
			accountClaim,
			"Invalid AccountClaim",
			errReason,
			awsv1alpha1.InvalidAccountClaim,
			awsv1alpha1.ClaimStatusError,
		)
		err := r.Client.Status().Update(context.TODO(), accountClaim)
		if err != nil {
			reqLogger.Error(err, "Failed to Update AccountClaim Status")
		}

		// TODO: Recoverable?
		return reconcile.Result{}, validateErr
	}

	claimErr := claimBYOCAccount(r, reqLogger, account)
	if claimErr != nil {
		reqLogger.Error(claimErr, "Could not claim BYOC Account")
		claimStatusErr := r.setAccountClaimError(reqLogger, account, claimErr.Error())
		if claimStatusErr != nil {
			reqLogger.Error(claimStatusErr, "failed setting accountClaim error state")
		}
		// TODO: Recoverable?
		return reconcile.Result{}, claimErr
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileAccount) GetSREAccessARN(reqLogger logr.Logger) (string, error) {
	// Get SRE Access ARN from configmap
	configMap := &corev1.ConfigMap{}
	err := r.Client.Get(
		context.TODO(),
		types.NamespacedName{
			Namespace: awsv1alpha1.AccountCrNamespace,
			Name:      awsv1alpha1.DefaultConfigMap,
		},
		configMap,
	)
	if err != nil {
		reqLogger.Error(err, "There was an error getting the ConfigMap to get the SRE Access Role")
		return "", err
	}

	SREAccessARN := configMap.Data["CCS-Access-Arn"]
	if SREAccessARN == "" {
		reqLogger.Error(awsv1alpha1.ErrInvalidConfigMap, "configmap key missing", "keyName", "CCS-Access-Arn")
		return "", awsv1alpha1.ErrInvalidConfigMap
	}

	return SREAccessARN, nil
}

// Create role for BYOC IAM user to assume
func createBYOCAdminAccessRole(reqLogger logr.Logger, awsSetupClient awsclient.Client, byocAWSClient awsclient.Client, policyArn string, instanceID string, tags []*iam.Tag, SREAccessARN string) (roleID string, err error) {
	getUserOutput, err := awsSetupClient.GetUser(&iam.GetUserInput{})
	if err != nil {
		reqLogger.Error(err, "Failed to get non-BYOC IAM User info")
		return roleID, err
	}
	principalARN := *getUserOutput.User.Arn
	accessArnList := []string{principalARN, SREAccessARN}

	byocInstanceIDRole := fmt.Sprintf("%s-%s", byocRole, instanceID)

	existingRole, err := GetExistingRole(reqLogger, byocInstanceIDRole, byocAWSClient)
	if err != nil {
		return roleID, err
	}

	if (*existingRole != iam.GetRoleOutput{}) {
		reqLogger.Info(fmt.Sprintf("Found pre-existing role: %s", byocInstanceIDRole))
		err := DeleteBYOCAdminAccessRole(reqLogger, byocAWSClient, instanceID)
		if err != nil {
			return roleID, err
		}
	}

	// Create the base role
	roleID, croleErr := CreateRole(reqLogger, byocInstanceIDRole, accessArnList, byocAWSClient, tags)
	if croleErr != nil {
		return roleID, croleErr
	}
	reqLogger.Info(fmt.Sprintf("New RoleID created: %s", roleID))

	reqLogger.Info(fmt.Sprintf("Attaching policy %s to role %s", policyArn, byocInstanceIDRole))
	// Attach the specified policy to the BYOC role
	_, attachErr := byocAWSClient.AttachRolePolicy(&iam.AttachRolePolicyInput{
		RoleName:  aws.String(byocInstanceIDRole),
		PolicyArn: aws.String(policyArn),
	})

	if attachErr != nil {
		return roleID, attachErr
	}

	reqLogger.Info(fmt.Sprintf("Checking if policy %s has been attached", policyArn))

	// Attaching the policy suffers from an eventual consistency problem
	policyList, listErr := GetAttachedPolicies(reqLogger, byocInstanceIDRole, byocAWSClient)
	if listErr != nil {
		return roleID, err
	}

	for _, policy := range policyList.AttachedPolicies {
		if *policy.PolicyArn == policyArn {
			reqLogger.Info(fmt.Sprintf("Found attached policy %s", *policy.PolicyArn))
			break
		} else {
			err = fmt.Errorf("policy %s never attached to role %s", policyArn, byocInstanceIDRole)
			return roleID, err
		}
	}

	return roleID, err
}

func DeleteBYOCAdminAccessRole(reqLogger logr.Logger, byocAWSClient awsclient.Client, instanceID string) (err error) {
	byocInstanceIDRole := fmt.Sprintf("%s-%s", byocRole, instanceID)
	policyList, err := GetAttachedPolicies(reqLogger, byocInstanceIDRole, byocAWSClient)
	if err != nil {
		return err
	}

	for _, policy := range policyList.AttachedPolicies {
		err := DetachPolicyFromRole(reqLogger, policy, byocInstanceIDRole, byocAWSClient)
		if err != nil {
			return err
		}
	}

	return DeleteRole(reqLogger, byocInstanceIDRole, byocAWSClient)
}

// CreateRole creates the role with the correct assume policy for BYOC for a given roleName
func CreateRole(reqLogger logr.Logger, byocRole string, accessArnList []string, byocAWSClient awsclient.Client, tags []*iam.Tag) (string, error) {
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

	reqLogger.Info(fmt.Sprintf("Creating role: %s", byocRole))
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

// GetExistingRole checks to see if a given role exists in the AWS account already.  If it does not, we return an empty response and nil for an error.  If it does, we return the existing role.  Otherwise, we return any error we get.
func GetExistingRole(reqLogger logr.Logger, byocRole string, byocAWSClient awsclient.Client) (*iam.GetRoleOutput, error) {
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
				return &iam.GetRoleOutput{}, nil
			case iam.ErrCodeServiceFailureException:
				reqLogger.Error(
					aerr,
					fmt.Sprintf("AWS Internal Server Error (%s) checking for %s role existence: %s", aerr.Code(), byocRole, aerr.Message()),
				)
				return &iam.GetRoleOutput{}, err
			default:
				// Currently only two errors returned by AWS.  This is a catch-all for any that may appear in the future.
				reqLogger.Error(
					aerr,
					fmt.Sprintf("Unknown error (%s) checking for %s role existence: %s", aerr.Code(), byocRole, aerr.Message()),
				)
				return &iam.GetRoleOutput{}, err
			}
		} else {
			return &iam.GetRoleOutput{}, err
		}
	}

	return existingRole, err
}

// GetAttachedPolicies gets a list of policies attached to a role
func GetAttachedPolicies(reqLogger logr.Logger, byocRole string, byocAWSClient awsclient.Client) (*iam.ListAttachedRolePoliciesOutput, error) {
	listRoleInput := &iam.ListAttachedRolePoliciesInput{
		RoleName: aws.String(byocRole),
	}
	policyList, err := byocAWSClient.ListAttachedRolePolicies(listRoleInput)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			default:
				reqLogger.Error(
					aerr,
					aerr.Error(),
				)
				return &iam.ListAttachedRolePoliciesOutput{}, err
			}
		} else {
			return &iam.ListAttachedRolePoliciesOutput{}, err
		}
	}
	return policyList, nil
}

// DetachPolicyFromRole detaches a given AttachedPolicy from a role
func DetachPolicyFromRole(reqLogger logr.Logger, policy *iam.AttachedPolicy, byocRole string, byocAWSClient awsclient.Client) error {
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
					aerr.Error(),
				)
				reqLogger.Error(err, err.Error())
			}
		}
	}
	return err
}

// DeleteRole deletes an existing role from AWS and handles the error
func DeleteRole(reqLogger logr.Logger, byocRole string, byocAWSClient awsclient.Client) error {
	reqLogger.Info(fmt.Sprintf("Deleting Role: %s", byocRole))
	_, err := byocAWSClient.DeleteRole(&iam.DeleteRoleInput{
		RoleName: aws.String(byocRole),
	})

	// Delete the existing role
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			default:
				reqLogger.Error(
					aerr,
					aerr.Error(),
				)
				reqLogger.Error(err, err.Error())
			}
		}
	}
	return err
}

func (r *ReconcileAccount) getSTSClient(log logr.Logger, accountClaim *awsv1alpha1.AccountClaim, operatorAWSClient awsclient.Client) (awsclient.Client, *sts.AssumeRoleOutput, error) {
	// Get SRE Access ARN from configmap
	cm := &corev1.ConfigMap{}
	cmErr := r.Client.Get(context.TODO(), types.NamespacedName{Namespace: awsv1alpha1.AccountCrNamespace, Name: awsv1alpha1.DefaultConfigMap}, cm)
	if cmErr != nil {
		log.Error(cmErr, "There was an error getting the ConfigMap to get the STS Jump Role")
		return nil, nil, cmErr
	}

	stsAccessARN := cm.Data["sts-jump-role"]
	if stsAccessARN == "" {
		log.Error(awsv1alpha1.ErrInvalidConfigMap, "configmap key missing", "keyName", "sts-jump-role")
		return nil, nil, cmErr
	}

	jumpRoleCreds, err := getSTSCredentials(log, operatorAWSClient, stsAccessARN, "", "awsAccountOperator")
	if err != nil {
		return nil, nil, err
	}

	jumpRoleClient, err := r.awsClientBuilder.GetClient(controllerName, r.Client, awsclient.NewAwsClientInput{
		AwsCredsSecretIDKey:     *jumpRoleCreds.Credentials.AccessKeyId,
		AwsCredsSecretAccessKey: *jumpRoleCreds.Credentials.SecretAccessKey,
		AwsToken:                *jumpRoleCreds.Credentials.SessionToken,
		AwsRegion:               "us-east-1",
	})
	if err != nil {
		return nil, nil, err
	}

	customerAccountCreds, err := getSTSCredentials(log, jumpRoleClient,
		accountClaim.Spec.STSRoleARN, accountClaim.Spec.STSExternalID, "RH-Account-Initialization")
	if err != nil {
		return nil, nil, err
	}

	customerClient, err := r.awsClientBuilder.GetClient(controllerName, r.Client, awsclient.NewAwsClientInput{
		AwsCredsSecretIDKey:     *customerAccountCreds.Credentials.AccessKeyId,
		AwsCredsSecretAccessKey: *customerAccountCreds.Credentials.SecretAccessKey,
		AwsToken:                *customerAccountCreds.Credentials.SessionToken,
		AwsRegion:               "us-east-1",
	})
	if err != nil {
		return nil, nil, err
	}

	return customerClient, customerAccountCreds, nil
}

func (r *ReconcileAccount) getCCSClient(currentAcct *awsv1alpha1.Account, accountClaim *awsv1alpha1.AccountClaim) (awsclient.Client, error) {
	// Get credentials
	ccsAWSClient, err := r.awsClientBuilder.GetClient(controllerName, r.Client, awsclient.NewAwsClientInput{
		SecretName: accountClaim.Spec.BYOCSecretRef.Name,
		NameSpace:  accountClaim.Spec.BYOCSecretRef.Namespace,
		AwsRegion:  "us-east-1",
	})
	if err != nil {
		return nil, err
	}

	return ccsAWSClient, nil
}
