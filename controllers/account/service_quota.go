package account

import (
	"fmt"
	"strconv"
	"time"

	retry "github.com/avast/retry-go"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/servicequotas"
	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	controllerutils "github.com/openshift/aws-account-operator/pkg/utils"
	"github.com/openshift/aws-account-operator/test/fixtures"
)

func HandleServiceQuotaRequests(reqLogger logr.Logger, awsClient awsclient.Client, quotaCode awsv1alpha1.SupportedServiceQuotas, serviceQuotaStatus *awsv1alpha1.ServiceQuotaStatus) error {

	reqLogger.Info("Handling ServiceQuota Requests")
	serviceCode, found := getServiceCode(quotaCode)
	if !found {
		reqLogger.Error(fixtures.NotFound, "cannot find corresponding ServiceCode for QuotaCode", "QuotaCode", string(quotaCode))
		return fixtures.NotFound
	}

	quotaIncreaseRequired, err := serviceQuotaNeedsIncrease(reqLogger, awsClient, string(quotaCode), serviceCode, float64(serviceQuotaStatus.Value))
	if err != nil {
		reqLogger.Error(err, "failed retrieving current vCPU quota from AWS")
		return err
	}

	// We need a SQ increase
	if quotaIncreaseRequired {
		reqLogger.Info(
			fmt.Sprintf("Quota Increase required for QuotaCode [%s] ServiceCode [%s] Requested Value [%d]",
				string(quotaCode), serviceCode, serviceQuotaStatus.Value),
		)

		// Check to see have we already requested this increase
		requestStatus, err := checkQuotaRequestStatus(reqLogger, awsClient, string(quotaCode), serviceCode, float64(serviceQuotaStatus.Value))
		if err != nil {
			reqLogger.Error(err, "failed to get quota change history")
			return err
		}

		switch requestStatus {
		case awsv1alpha1.ServiceRequestCompleted:
			reqLogger.Info(
				fmt.Sprintf("Quota Increase COMPLETED for QuotaCode [%s] ServiceCode [%s] Requested Value [%d]",
					string(quotaCode), serviceCode, serviceQuotaStatus.Value),
			)
			serviceQuotaStatus.Status = awsv1alpha1.ServiceRequestCompleted
			return nil
		case awsv1alpha1.ServiceRequestInProgress:
			reqLogger.Info(
				fmt.Sprintf("Quota Increase IN_PROGRESS for QuotaCode [%s] ServiceCode [%s] Requested Value [%d]",
					string(quotaCode), serviceCode, serviceQuotaStatus.Value),
			)
			return nil
		case awsv1alpha1.ServiceRequestDenied:
			reqLogger.Info(
				fmt.Sprintf("Quota Increase DENIED for QuotaCode [%s] ServiceCode [%s] Requested Value [%d]",
					string(quotaCode), serviceCode, serviceQuotaStatus.Value),
			)
			serviceQuotaStatus.Status = awsv1alpha1.ServiceRequestDenied
			return nil
		case awsv1alpha1.ServiceRequestTodo:
			submitted, err := setServiceQuota(reqLogger, awsClient, string(quotaCode), serviceCode, float64(serviceQuotaStatus.Value))
			if err != nil {
				reqLogger.Error(err, "failed requesting quota increase", "QuotaCode", string(quotaCode))
			}
			if submitted {
				reqLogger.Info(
					fmt.Sprintf("Quota Increase REQUESTED for QuotaCode [%s] ServiceCode [%s] Requested Value [%d]",
						string(quotaCode), serviceCode, serviceQuotaStatus.Value),
				)
			}
			serviceQuotaStatus.Status = awsv1alpha1.ServiceRequestInProgress
		}

	} else {
		reqLogger.Info(
			fmt.Sprintf("Quota Increase COMPLETED for QuotaCode [%s] ServiceCode [%s] Requested Value [%d]",
				string(quotaCode), serviceCode, serviceQuotaStatus.Value),
		)
		serviceQuotaStatus.Status = awsv1alpha1.ServiceRequestCompleted
	}
	return nil
}

func getServiceCode(quotaCode awsv1alpha1.SupportedServiceQuotas) (string, bool) {

	servicesMap := map[awsv1alpha1.SupportedServiceQuotas]string{
		awsv1alpha1.RunningStandardInstances:  string(awsv1alpha1.EC2ServiceQuota),
		awsv1alpha1.EC2VPCElasticIPsQuotaCode: string(awsv1alpha1.EC2ServiceQuota),
		awsv1alpha1.NLBPerRegion:              string(awsv1alpha1.Elasticloadbalancing),
		awsv1alpha1.RulesPerSecurityGroup:     string(awsv1alpha1.VPCServiceQuota),
		awsv1alpha1.EC2NetworkAclQuotaCode:    string(awsv1alpha1.EC2ServiceQuota),
		awsv1alpha1.GeneralPurposeSSD:         string(awsv1alpha1.EC2ServiceQuota),
	}

	v, found := servicesMap[quotaCode]
	return v, found
}

// getDesiredServiceQuotaValue retrieves the desired quota information from the operator configmap and converts it to a float64
func (r *AccountReconciler) getDesiredServiceQuotaValue(reqLogger logr.Logger, quota string) (float64, error) {
	var err error
	var vCPUQuota float64

	configMap, err := controllerutils.GetOperatorConfigMap(r.Client)
	v, ok := configMap.Data[fmt.Sprintf("quota.%s", quota)]
	if !ok {
		err = awsv1alpha1.ErrInvalidConfigMap
	}
	if err != nil {
		reqLogger.Info("Failed getting desired vCPU quota from configmap data, defaulting quota to 0") // TODO change vCPU to param
		return vCPUQuota, err
	}

	vCPUQuota, err = strconv.ParseFloat(v, 64)
	if err != nil {
		reqLogger.Info("Failed converting vCPU quota from configmap string to float64, defaulting quota to 0") // TODO change vCPU to param
		return vCPUQuota, err
	}

	return vCPUQuota, nil
}

func serviceQuotaNeedsIncrease(reqLogger logr.Logger, client awsclient.Client, quotaCode string, serviceCode string, desiredQuota float64) (bool, error) {
	var result *servicequotas.GetServiceQuotaOutput

	// Default is 1/10 of a second, but any retries we need to make should be delayed a few seconds
	// This also defaults to an exponential backoff, so we only need to try ~5 times, default is 10
	retry.DefaultDelay = 3 * time.Second
	retry.DefaultAttempts = uint(5)
	err := retry.Do(
		func() (err error) {
			// Get the current existing quota setting
			result, err = client.GetServiceQuota(
				&servicequotas.GetServiceQuotaInput{
					QuotaCode:   aws.String(quotaCode),
					ServiceCode: aws.String(serviceCode),
				},
			)
			return err
		},

		// Retry if we receive some specific errors: access denied, rate limit or server-side error
		retry.RetryIf(func(err error) bool {
			if aerr, ok := err.(awserr.Error); ok {
				switch aerr.Code() {
				// AccessDenied may indicate the BYOCAdminAccess role has not yet propagated
				case "AccessDeniedException":
					return true
				case "ServiceException":
					return true
				case "TooManyRequestsException":
					return true
				// Can be caused by the client token not yet propagated
				case "UnrecognizedClientException":
					return true
				}
			}
			// Otherwise, do not retry
			return false
		}),
	)

	// Regardless of errors, if we got the result for the actual quota,
	// then compare it to the desired quota.
	if result.Quota != nil {
		if *result.Quota.Value < desiredQuota {
			reqLogger.Info(fmt.Sprintf("Requiring a servicequota increase: current [%.1f] wanted [%.1f]\n", *result.Quota.Value, desiredQuota))
			return true, err
		}
	}

	// Otherwise return false (doesn't need increase) and any errors
	return false, err
}

func setServiceQuota(reqLogger logr.Logger, client awsclient.Client, quotaCode string, serviceCode string, desiredQuota float64) (bool, error) {
	// Request a service quota increase for vCPU quota
	var result *servicequotas.RequestServiceQuotaIncreaseOutput
	var alreadySubmitted bool

	// Default is 1/10 of a second, but any retries we need to make should be delayed a few seconds
	// This also defaults to an exponential backoff, so we only need to try ~5 times, default is 10
	retry.DefaultDelay = 3 * time.Second
	retry.DefaultAttempts = uint(5)
	err := retry.Do(
		func() (err error) {
			result, err = client.RequestServiceQuotaIncrease(
				&servicequotas.RequestServiceQuotaIncreaseInput{
					DesiredValue: aws.Float64(desiredQuota),
					ServiceCode:  aws.String(serviceCode), // TODO change to param
					QuotaCode:    aws.String(quotaCode),   // TODO change to param
				})
			if err != nil {
				if aerr, ok := err.(awserr.Error); ok {
					if aerr.Code() == "ResourceAlreadyExistsException" {
						// This error means a request has already been submitted, and we do not have the CaseID, but
						// we should also *not* return an error - this is a no-op.
						alreadySubmitted = true
						return nil
					}
				}
			}
			return err
		},

		retry.RetryIf(func(err error) bool {
			if aerr, ok := err.(awserr.Error); ok {
				switch aerr.Code() {
				// AccessDenied may indicate the BYOCAdminAccess role has not yet propagated
				case "AccessDeniedException":
					return true
				case "TooManyRequestsException":
					// Retry
					return true
				case "ServiceException":
					// Retry
					return true
				// Can be caused by the client token not yet propagated
				case "UnrecognizedClientException":
					return true
				}
			}
			// Otherwise, do not retry
			return false
		}),
	)

	// If the attempt to submit a request returns "ResourceAlreadyExistsException"
	// then a request has already been submitted, since we first polled. No further action.
	if alreadySubmitted {
		return true, nil
	}

	// Otherwise, if there is an error, return the error to be handled
	if err != nil {
		return false, err
	}

	if (servicequotas.RequestServiceQuotaIncreaseOutput{}) == *result {
		err := fmt.Errorf("returned RequestServiceQuotaIncreaseOutput is nil")
		return false, err
	}

	if (servicequotas.RequestedServiceQuotaChange{}) == *result.RequestedQuota {
		err := fmt.Errorf("returned RequestedServiceQuotasIncreaseOutput field RequestedServiceQuotaChange is nil")
		return false, err
	}

	if err != nil {
		return false, err
	}
	return true, nil
}

func checkQuotaRequestStatus(reqLogger logr.Logger, awsClient awsclient.Client, quotaCode string, serviceCode string, expectedQuota float64) (awsv1alpha1.ServiceRequestStatus, error) {

	var nextToken *string
	// Default is 1/10 of a second, but any retries we need to make should be delayed a few seconds
	// This also defaults to an exponential backoff, so we only need to try ~5 times, default is 10
	retry.DefaultDelay = 3 * time.Second
	retry.DefaultAttempts = uint(5)

	for {
		// This returns with pagination, so we have to iterate over the pagination data

		var result *servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaOutput

		err := retry.Do(
			func() (err error) {
				// Get a (possibly paginated) list of quota change requests by quota
				result, err = awsClient.ListRequestedServiceQuotaChangeHistoryByQuota(
					&servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaInput{
						NextToken:   nextToken,
						ServiceCode: aws.String(serviceCode),
						QuotaCode:   aws.String(quotaCode),
					},
				)
				return err
			},

			// Retry if we receive some specific errors: access denied, rate limit or server-side error
			retry.RetryIf(func(err error) bool {
				if aerr, ok := err.(awserr.Error); ok {
					switch aerr.Code() {
					// AccessDenied may indicate the BYOCAdminAccess role has not yet propagated
					case "AccessDeniedException":
						return true
					case "ServiceException":
						return true
					case "TooManyRequestsException":
						return true
					// Can be caused by the client token not yet propagated
					case "UnrecognizedClientException":
						return true
					}
				}
				// Otherwise, do not retry
				return false
			}),
		)

		if err != nil {
			// Return an error if retrieving the change history fails
			return awsv1alpha1.ServiceRequestTodo, err
		}

		// Check all the returned requests to see if one matches the quota increase we'd request
		// If so, it's already been submitted
		for _, change := range result.RequestedQuotas {
			if changeRequestMatches(change, quotaCode, serviceCode, expectedQuota) {
				switch *change.Status {
				case "PENDING", "CASE_OPENED":
					return awsv1alpha1.ServiceRequestInProgress, nil
				case "APPROVED", "CASE_CLOSED":
					return awsv1alpha1.ServiceRequestCompleted, nil
				case "DENIED":
					return awsv1alpha1.ServiceRequestDenied, nil
				}
			}
		}

		// Set NextToken to retrieve the next page and loop again
		nextToken = result.NextToken
		if nextToken == nil {
			return awsv1alpha1.ServiceRequestTodo, nil
		}
	}
}

// changeRequestMatches returns true if the QuotaCode, ServiceCode and desired value match
func changeRequestMatches(change *servicequotas.RequestedServiceQuotaChange, quotaCode string, serviceCode string, quota float64) bool {
	if *change.ServiceCode != serviceCode {
		return false
	}

	if *change.QuotaCode != quotaCode {
		return false
	}

	if *change.DesiredValue != quota {
		return false
	}

	return true
}
