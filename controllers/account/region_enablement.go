package account

import (
	"context"
	"fmt"
	"github.com/avast/retry-go"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/account"
	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"time"
)

func HandleOptInRegionRequests(reqLogger logr.Logger, awsClient awsclient.Client, optInRegion *awsv1alpha1.OptInRegionStatus, currentAcctInstance *awsv1alpha1.Account) error {
	reqLogger.Info("Handling Opt-In Region Requests")

	regionOptInRequired, err := RegionNeedsOptIn(reqLogger, awsClient, optInRegion.RegionCode)
	if err != nil {
		reqLogger.Error(err, "failed retrieving region Opt-In status from AWS")
	}

	// Region enablement is required
	if regionOptInRequired {
		reqLogger.Info(
			fmt.Sprintf("Region Enablement Require for RegionCode [%s]",
				optInRegion.RegionCode),
		)

		// Checks to see if region enablement was already requested
		requestStatus, err := checkOptInRegionStatus(reqLogger, awsClient, optInRegion.RegionCode)
		if err != nil {
			reqLogger.Error(err, "failed to get Opt-In status ")
		}

		switch requestStatus {
		case awsv1alpha1.OptInRequestEnabled:
			reqLogger.Info(
				fmt.Sprintf("Region Enablement COMPLETED for RegionCode [%s]",
					optInRegion.RegionCode),
			)
			optInRegion.Status = awsv1alpha1.OptInRequestEnabled
		case awsv1alpha1.OptInRequestEnabling:
			reqLogger.Info(
				fmt.Sprintf("Region Enablement IN_PROGRESS for for RegionCode [%s]",
					optInRegion.RegionCode),
			)
			optInRegion.Status = awsv1alpha1.OptInRequestEnabling
		case awsv1alpha1.OptInRequestTodo:
			submitted, err := enableOptInRegions(reqLogger, awsClient, optInRegion.RegionCode)
			if err != nil {
				reqLogger.Error(err, "failed to opt-in region", "RegionCode", optInRegion.RegionCode)
			}
			if submitted {
				reqLogger.Info(
					fmt.Sprintf("Opt-IN REQUESTED for RegionCode [%s]",
						optInRegion.RegionCode),
				)
				optInRegion.Status = awsv1alpha1.OptInRequestEnabling
			}
		}

	} else {
		reqLogger.Info(
			fmt.Sprintf("Region Enablement COMPLETED for RegionCode [%s]",
				optInRegion.RegionCode),
		)
		optInRegion.Status = awsv1alpha1.OptInRequestEnabled
	}

	regionName, _ := IsSupportedRegion(optInRegion.RegionCode)
	currentAcctInstance.Status.OptInRegions[string(regionName)].Status = optInRegion.Status

	return nil
}

func GetOptInRegionStatus(reqLogger logr.Logger, awsClientBuilder awsclient.IBuilder, awsSetupClient awsclient.Client, currentAcctInstance *awsv1alpha1.Account, client client.Client) (reconcile.Result, error) {
	// First we get all enablment request we need to get a status update on:
	// - Enablment requests that are not yet open on the AWS side
	// - Enablment requests that are open but not yet completed
	currentInFlightCount, inFlightOptInRequests := currentAcctInstance.GetOptInRequestsByStatus(awsv1alpha1.OptInRequestEnabling)
	_, onlyOpenOptInRequests := currentAcctInstance.GetOptInRequestsByStatus(awsv1alpha1.OptInRequestTodo)
	if currentInFlightCount <= MaxOptInRegionRequest {
		reqLogger.Info(fmt.Sprintf("currentInFlightCount (%d) <= maxOpenOptInRegionRequests (%d)", currentInFlightCount, MaxOptInRegionRequest))
		for region, onlyOpenOptInRequest := range onlyOpenOptInRequests {
			if _, ok := inFlightOptInRequests[region]; !ok {
				inFlightOptInRequests[region] = &awsv1alpha1.OptInRegionStatus{}
			}
			inFlightOptInRequests[region] = onlyOpenOptInRequest
			currentInFlightCount += 1
			if currentInFlightCount >= MaxOptInRegionRequest {
				break
			}
		}
	}
	reqLogger.Info("Handling region request", "current-in-flight-count", currentInFlightCount)
	err := updateOptInRegionRequests(reqLogger, awsClientBuilder, awsSetupClient, currentAcctInstance, client, inFlightOptInRequests, currentInFlightCount)
	if err != nil {
		return reconcile.Result{}, err
	}
	err = client.Status().Update(context.TODO(), currentAcctInstance)
	if err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{RequeueAfter: 30 * time.Second, Requeue: true}, err
}

func updateOptInRegionRequests(reqLogger logr.Logger, awsClientBuilder awsclient.IBuilder, awsSetupClient awsclient.Client, currentAcctInstance *awsv1alpha1.Account, client client.Client, optInRequests awsv1alpha1.OptInRegions, count int) error {
	for region, regionRequest := range optInRequests {
		regionLogger := reqLogger.WithValues("Region", region)
		roleToAssume := currentAcctInstance.GetAssumeRole()
		awsAssumedRoleClient, _, err := AssumeRoleAndCreateClient(reqLogger, awsClientBuilder, currentAcctInstance, client, awsSetupClient, region, roleToAssume, "")
		if err != nil {
			reqLogger.Error(err, "Could not impersonate AWS account", "aws-account", currentAcctInstance.Spec.AwsAccountID)
			return err
		}

		reqLogger.Info(fmt.Sprintf("Handling Opt-In region request for %s: %s", region, regionRequest))
		err = HandleOptInRegionRequests(regionLogger, awsAssumedRoleClient, regionRequest, currentAcctInstance)
		if err != nil {
			return err // TODO: For review, do we want to be handling the error like this?
		}

	}
	return nil
}

func enableOptInRegions(reqLogger logr.Logger, client awsclient.Client, regionCode string) (bool, error) {
	var result *account.EnableRegionOutput
	var alreadySubmitted bool

	// Default is 1/10 of a second, but any retries we need to make should be delayed a few seconds
	// This also defaults to an exponential backoff, so we only need to try ~5 times, default is 10
	retry.DefaultDelay = 3 * time.Second
	retry.DefaultAttempts = uint(5)
	err := retry.Do(
		func() (err error) {
			result, err = client.EnableRegion(&account.EnableRegionInput{
				RegionName: aws.String(regionCode),
			})
			if err != nil {
				if aerr, ok := err.(awserr.Error); ok {
					if aerr.Code() == "ResourceAlreadyExistsException" {
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
				case "InternalServerException":
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

	if (account.EnableRegionOutput{}) != *result {
		err := fmt.Errorf("returned EnableRegionOutput is not nil")
		return false, err
	}

	return true, nil
}

func RegionNeedsOptIn(reqLogger logr.Logger, client awsclient.Client, regionCode string) (bool, error) {
	var result *account.GetRegionOptStatusOutput

	// Default is 1/10 of a second, but any retries we need to make should be delayed a few seconds
	// This also defaults to an exponential backoff, so we only need to try ~5 times, default is 10
	retry.DefaultDelay = 3 * time.Second
	retry.DefaultAttempts = uint(5)
	err := retry.Do(
		func() (err error) {
			result, err = client.GetRegionOptStatus(&account.GetRegionOptStatusInput{
				RegionName: aws.String(regionCode),
			})
			return err
		},

		// Retry if we receive some specific errors: access denied, rate limit or server-side error
		retry.RetryIf(func(err error) bool {
			if aerr, ok := err.(awserr.Error); ok {
				switch aerr.Code() {
				// AccessDenied may indicate the BYOCAdminAccess role has not yet propagated
				case "AccessDeniedException":
					return true
				case "InternalServerException":
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

	if result.RegionOptStatus != nil {
		if *result.RegionOptStatus != "ENABLED" {
			reqLogger.Info(fmt.Sprintf("Region: %s requires enablement\n", regionCode))
			return true, err
		}

	}

	// Otherwise return false (doesn't need enablement) and any errors
	return false, err
}

func checkOptInRegionStatus(reqLogger logr.Logger, awsClient awsclient.Client, regionCode string) (awsv1alpha1.OptInRequestStatus, error) {

	// Default is 1/10 of a second, but any retries we need to make should be delayed a few seconds
	// This also defaults to an exponential backoff, so we only need to try ~5 times, default is 10
	retry.DefaultDelay = 3 * time.Second
	retry.DefaultAttempts = uint(5)

	for {
		// This returns with pagination, so we have to iterate over the pagination data
		var result *account.GetRegionOptStatusOutput

		err := retry.Do(
			func() (err error) {
				result, err = awsClient.GetRegionOptStatus(&account.GetRegionOptStatusInput{
					RegionName: aws.String(regionCode),
				})
				return err
			},

			// Retry if we receive some specific errors: access denied, rate limit or server-side error
			retry.RetryIf(func(err error) bool {
				if aerr, ok := err.(awserr.Error); ok {
					switch aerr.Code() {
					// AccessDenied may indicate the BYOCAdminAccess role has not yet propagated
					case "AccessDeniedException":
						return true
					case "InternalServerException":
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
			return awsv1alpha1.OptInRequestTodo, err
		}

		if result.RegionOptStatus != nil {
			switch *result.RegionOptStatus {
			case "ENABLING":
				return awsv1alpha1.OptInRequestEnabling, nil
			case "ENABLED", "ENABLED_BY_DEFAULT":
				return awsv1alpha1.OptInRequestEnabled, nil
			case "DISABLED":
				return awsv1alpha1.OptInRequestTodo, nil
			}
		}
	}
}

func IsSupportedRegion(region string) (awsv1alpha1.SupportedOptInRegions, bool) {
	r := awsv1alpha1.SupportedOptInRegions(region)

	regionMap := map[awsv1alpha1.SupportedOptInRegions]awsv1alpha1.SupportedOptInRegions{
		awsv1alpha1.CapeTownRegion:  "CapeTown",
		awsv1alpha1.MelbourneRegion: "Melbourne",
		awsv1alpha1.HyderabadRegion: "Hyderabad",
		awsv1alpha1.MilanRegion:     "Milan",

		awsv1alpha1.JakartaRegion:  "Jakarta",
		awsv1alpha1.ZurichRegion:   "Zurich",
		awsv1alpha1.HongKongRegion: "HongKong",
		awsv1alpha1.SpainRegion:    "Spain",
		awsv1alpha1.BahrainRegion:  "Bahrain",
		awsv1alpha1.UAERegion:      "UAE",
	}
	v, found := regionMap[r]
	return v, found
}
func SetOptRegionStatus(reqLogger logr.Logger, optInRegions map[string]string, currentAcctInstance *awsv1alpha1.Account) error {
	reqLogger.Info("Setting Opt-In region status to todo of all Supported Opt-In regions")
	currentAcctInstance.Status.OptInRegions = make(awsv1alpha1.OptInRegions)
	for key, value := range optInRegions {
		currentAcctInstance.Status.OptInRegions[key] = &awsv1alpha1.OptInRegionStatus{
			RegionCode: value,
			Status:     awsv1alpha1.OptInRequestTodo,
		}
	}
	return nil
}
