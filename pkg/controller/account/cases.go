package account

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/support"
	"github.com/go-logr/logr"

	"github.com/openshift/aws-account-operator/pkg/awsclient"
	controllerutils "github.com/openshift/aws-account-operator/pkg/controller/utils"
)

func createCase(reqLogger logr.Logger, accountID string, client awsclient.Client) (string, error) {
	// Initialize basic communication body and case subject
	caseCommunicationBody := fmt.Sprintf(
		"Hello AWS,\n\nPlease enable Enterprise Support on AWS account %s.\n\nOnce this has been completed and the default EC2 limits are ready for use, please resolve this support case. Please do not set the case to Pending Customer Action.\n\nThanks.", accountID,
	)

	caseSubject := fmt.Sprintf("Add account %s to Enterprise Support", accountID)

	createCaseInput := support.CreateCaseInput{
		CategoryCode:      aws.String(caseCategoryCode),
		ServiceCode:       aws.String(caseServiceCode),
		IssueType:         aws.String(caseIssueType),
		CommunicationBody: aws.String(caseCommunicationBody),
		Subject:           aws.String(caseSubject),
		SeverityCode:      aws.String(caseSeverity),
		Language:          aws.String(caseLanguage),
	}

	reqLogger.Info("Creating the case", "CaseInput", createCaseInput)

	caseResult, caseErr := client.CreateCase(&createCaseInput)
	if caseErr != nil {
		var returnErr error
		if aerr, ok := caseErr.(awserr.Error); ok {
			switch aerr.Code() {
			case support.ErrCodeCaseCreationLimitExceeded:
				returnErr = ErrAwsCaseCreationLimitExceeded
			case support.ErrCodeInternalServerError:
				returnErr = ErrAwsInternalFailure
			default:
				returnErr = ErrAwsFailedCreateSupportCase
			}

			controllerutils.LogAwsError(reqLogger, "New AWS Error while creating case", returnErr, caseErr)
		}
		return "", returnErr
	}

	reqLogger.Info("Support case created", "AccountID", accountID, "CaseID", caseResult.CaseId)

	return *caseResult.CaseId, nil
}

func checkCaseResolution(reqLogger logr.Logger, caseID string, client awsclient.Client) (bool, error) {
	// Look for the case using the unique ID provided
	describeCasesInput := support.DescribeCasesInput{
		CaseIdList: []*string{
			aws.String(caseID),
		},
	}

	caseResult, caseErr := client.DescribeCases(&describeCasesInput)
	if caseErr != nil {

		var returnErr error
		if aerr, ok := caseErr.(awserr.Error); ok {
			switch aerr.Code() {
			case support.ErrCodeCaseIdNotFound:
				returnErr = ErrAwsSupportCaseIDNotFound
			case support.ErrCodeInternalServerError:
				returnErr = ErrAwsInternalFailure
			default:
				returnErr = ErrAwsFailedDescribeSupportCase
			}
			controllerutils.LogAwsError(reqLogger, "New AWS Error while checking case resolution", returnErr, caseErr)
		}

		return false, returnErr
	}

	// Since we are describing cases based on the unique ID, this list will have only 1 element
	if *caseResult.Cases[0].Status == caseStatusResolved {
		reqLogger.Info(fmt.Sprintf("Case Resolved: %s", caseID))
		return true, nil
	}

	// reqLogger.Info(fmt.Sprintf("Case [%s] not yet Resolved, waiting. Current Status: %s", caseID, *caseResult.Cases[0].Status))
	return false, nil
}
