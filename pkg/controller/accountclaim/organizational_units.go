package accountclaim

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/organizations"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	awsclient "github.com/openshift/aws-account-operator/pkg/awsclient"
	controllerutils "github.com/openshift/aws-account-operator/pkg/controller/utils"
)

// MoveAccountToOU takes care of all the logic surrounding moving an account into an OU
func MoveAccountToOU(r *ReconcileAccountClaim, reqLogger logr.Logger, accountClaim *awsv1alpha1.AccountClaim, account *awsv1alpha1.Account) error {
	// aws client
	awsClient, err := awsclient.GetAWSClient(r.client, awsclient.NewAwsClientInput{
		SecretName: controllerutils.AwsSecretName,
		NameSpace:  awsv1alpha1.AccountCrNamespace,
		AwsRegion:  "us-east-1",
	})

	// Search for ConfigMap that holds OU mapping
	instance := &corev1.ConfigMap{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Namespace: awsv1alpha1.AccountCrNamespace, Name: awsv1alpha1.DefaultConfigMap}, instance)
	if err != nil {
		// If we failed to retrieve the ConfigMap, simply leave the account in Root
		unexpectedErrorMsg := fmt.Sprintf("OU: Failed to find OU mapping ConfigMap, leaving account in root")
		reqLogger.Info(unexpectedErrorMsg)
		accountClaim.Spec.AccountOU = "ROOT"
		return r.specUpdate(reqLogger, accountClaim)
	}

	// Get OU ID for root and base
	friendlyOUName := account.Spec.LegalEntity.ID
	baseID, rootID, err := checkOUMapping(instance)
	if err != nil {
		invalidOUErrorMsg := fmt.Sprintf("Invalid OU ConfigMap, missing root and/or base fields: %s", instance.Data)
		reqLogger.Error(err, invalidOUErrorMsg)
		return err
	}

	// Create/Find account OU
	OUID, err := CreateOrFindOU(reqLogger, awsClient, accountClaim, friendlyOUName, baseID)
	if err != nil {
		return err
	}
	err = MoveAccount(reqLogger, awsClient, account, OUID, rootID)
	if err != nil {
		// If error was cause by the account already being inside the OU, simply update the accountclaim cr and returns
		if err == awsv1alpha1.ErrAccAlreadyInOU {
			// Log account already in desired location
			accountMovedMsg := fmt.Sprintf("OU: Account %s was already in the desired OU %s", account.Name, account.Spec.LegalEntity.ID)
			reqLogger.Info(accountMovedMsg)
			// Update accountclaim spec
			accountClaim.Spec.AccountOU = OUID
			return r.specUpdate(reqLogger, accountClaim)
		}
		return err
	}

	// Log account moved successfully
	accountMovedMsg := fmt.Sprintf("OU: Account %s successfully moved to OU %s", account.Name, account.Spec.LegalEntity.ID)
	reqLogger.Info(accountMovedMsg)

	// Update unclaimedAccount.Spec.AwsAccountOU
	accountClaim.Spec.AccountOU = OUID
	return r.specUpdate(reqLogger, accountClaim)
}

// CreateOrFindOU will create or find an existing OU and return its ID
func CreateOrFindOU(reqLogger logr.Logger, client awsclient.Client, accountClaim *awsv1alpha1.AccountClaim, friendlyOUName string, baseID string) (string, error) {
	// Create/Find account OU
	createCreateOrganizationalUnitInput := organizations.CreateOrganizationalUnitInput{
		Name:     &friendlyOUName,
		ParentId: &baseID,
	}
	ouOutput, ouErr := client.CreateOrganizationalUnit(&createCreateOrganizationalUnitInput)
	if ouErr != nil {
		if aerr, ok := ouErr.(awserr.Error); ok {
			switch aerr.Code() {
			case "DuplicateOrganizationalUnitException":
				duplicateOUMsg := fmt.Sprintf("OU: %s Already exists", accountClaim.Spec.LegalEntity.ID)
				reqLogger.Info(duplicateOUMsg)
				return findOUIDFromName(reqLogger, client, baseID, friendlyOUName)
			default:
				unexpectedErrorMsg := fmt.Sprintf("OU: Unexpected AWS Error when attempting to create AWS OU: %s", aerr.Code())
				reqLogger.Info(unexpectedErrorMsg)
				return "", ouErr
			}
		}
	}
	return *ouOutput.OrganizationalUnit.Id, nil
}

// MoveAccount will take an account and move it into the specified OU
func MoveAccount(reqLogger logr.Logger, client awsclient.Client, account *awsv1alpha1.Account, OUID string, parentID string) error {
	// Move account
	moveAccountInput := organizations.MoveAccountInput{
		AccountId:           &account.Spec.AwsAccountID,
		DestinationParentId: &OUID,
		SourceParentId:      &parentID,
	}
	_, err := client.MoveAccount(&moveAccountInput)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case "AccountNotFoundException":
				// if the account has been moved out of root we check if it is in the desired OU and update the accountclaim spec
				accountNotFound := fmt.Sprintf("Account %s was not found in root, checking if the account already in the correct OU", account.Spec.LegalEntity.Name)
				reqLogger.Info(accountNotFound)
				childType := "ACCOUNT"
				check, accErr := findChildInOU(reqLogger, client, OUID, childType, account.Spec.AwsAccountID)
				if accErr != nil {
					return err
				}
				if check {
					return awsv1alpha1.ErrAccAlreadyInOU
				}
			case "ConcurrentModificationException":
				// if we encounter a race condition we simply log the condition and return
				ConcurrentModificationExceptionMsg := fmt.Sprintf("OU:CreateOrganizationalUnit:ConcurrentModificationException: Race condition while attempting to move Account: %s to OU: %s", account.Spec.AwsAccountID, OUID)
				reqLogger.Info(ConcurrentModificationExceptionMsg)
				return nil
			default:
				unexpectedErrorMsg := fmt.Sprintf("CreateOrganizationalUnit: Unexpected AWS Error when attempting to move AWS Account: %s to OU: %s, Error: %s", account.Spec.AwsAccountID, OUID, aerr.Code())
				reqLogger.Info(unexpectedErrorMsg)
			}
		}
		return err
	}
	return nil
}

func findChildInOU(reqLogger logr.Logger, client awsclient.Client, parentid string, childType string, childID string) (bool, error) {
	// Loop through all children in the parent
	check := ""
	listChildrenInput := organizations.ListChildrenInput{
		ChildType: &childType,
		ParentId:  &parentid,
	}
	for check == "" {
		// Loop until we find the location of the child
		listOut, err := client.ListChildren(&listChildrenInput)
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				unexpectedErrorMsg := fmt.Sprintf("FindOUNameFromChildID: Unexpected AWS Error when attempting to list children from %s OU: %s", parentid, aerr.Code())
				reqLogger.Info(unexpectedErrorMsg)
			}
			return false, err
		}
		for _, element := range listOut.Children {
			if *element.Id == childID {
				return true, nil
			}
		}
		// If the OU is not found we should update the input for the next list call
		if listOut.NextToken != nil {
			listChildrenInput.NextToken = listOut.NextToken
			continue
		}
		break
	}
	return false, awsv1alpha1.ErrChildNotFound
}

func findOUIDFromName(reqLogger logr.Logger, client awsclient.Client, parentid string, ouName string) (string, error) {
	// Loop through all OUs in the parent until we find the ID of the OU with the given name
	ouID := ""
	listOrgUnitsForParentID := organizations.ListOrganizationalUnitsForParentInput{
		ParentId: &parentid,
	}
	for ouID == "" {
		// Get a list with a fraction of the OUs in this parent starting from NextToken
		listOut, err := client.ListOrganizationalUnitsForParent(&listOrgUnitsForParentID)
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				unexpectedErrorMsg := fmt.Sprintf("FindOUFromParentID: Unexpected AWS Error when attempting to find OU ID from Parent: %s", aerr.Code())
				reqLogger.Info(unexpectedErrorMsg)
			}
			return "", err
		}
		for _, element := range listOut.OrganizationalUnits {
			if *element.Name == ouName {
				return *element.Id, nil
			}
		}
		// If the OU is not found we should update the input for the next list call
		if listOut.NextToken != nil {
			listOrgUnitsForParentID.NextToken = listOut.NextToken
			continue
		}
		return "", awsv1alpha1.ErrNonexistentOU
	}
	return ouID, nil
}

func checkOUMapping(cMap *corev1.ConfigMap) (string, string, error) {
	if _, ok := cMap.Data["base"]; !ok {
		return "", "", awsv1alpha1.ErrInvalidConfigMap
	}
	if _, ok := cMap.Data["root"]; !ok {
		return "", "", awsv1alpha1.ErrInvalidConfigMap
	}
	return cMap.Data["base"], cMap.Data["root"], nil
}
