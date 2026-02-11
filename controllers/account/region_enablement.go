package account

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/avast/retry-go"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/account"
	accounttypes "github.com/aws/aws-sdk-go-v2/service/account/types"
	"github.com/aws/smithy-go"
	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func HandleOptInRegionRequests(reqLogger logr.Logger, awsClient awsclient.Client, optInRegion string, optInRegionRequest *awsv1alpha1.OptInRegionStatus, currentAcctInstance *awsv1alpha1.Account) error {
	reqLogger.Info("Handling Opt-In Region Requests")

	regionOptInRequired, err := RegionNeedsOptIn(reqLogger, awsClient, optInRegion)
	if err != nil {
		reqLogger.Error(err, "failed retrieving region Opt-In status from AWS")
		if strings.Contains(err.Error(), "AccessDeniedException") {
			optInRegionRequest.Status = awsv1alpha1.OptInRequestUnknown
		}
	}

	// Region enablement is required
	if regionOptInRequired {
		reqLogger.Info(
			fmt.Sprintf("Region Enablement Required for RegionCode [%s]",
				optInRegion),
		)

		// Checks to see if region enablement was already requested
		requestStatus, err := checkOptInRegionStatus(reqLogger, awsClient, optInRegion)
		if err != nil {
			reqLogger.Error(err, "failed to get Opt-In status ")
		}

		switch requestStatus {
		case awsv1alpha1.OptInRequestEnabled:
			reqLogger.Info(
				fmt.Sprintf("Region Enablement COMPLETED for RegionCode [%s]",
					optInRegion),
			)
			optInRegionRequest.Status = awsv1alpha1.OptInRequestEnabled
		case awsv1alpha1.OptInRequestEnabling:
			reqLogger.Info(
				fmt.Sprintf("Region Enablement IN_PROGRESS for for RegionCode [%s]",
					optInRegion),
			)
			optInRegionRequest.Status = awsv1alpha1.OptInRequestEnabling
		case awsv1alpha1.OptInRequestTodo:
			submitted, err := enableOptInRegions(reqLogger, awsClient, optInRegion)
			if err != nil {
				reqLogger.Error(err, "failed to opt-in region", "RegionCode", optInRegion)
			}
			if submitted {
				reqLogger.Info(
					fmt.Sprintf("Opt-IN REQUESTED for RegionCode [%s]",
						optInRegion),
				)
				optInRegionRequest.Status = awsv1alpha1.OptInRequestEnabling
			}
		}

	} else {
		if err != nil {
			if strings.Contains(err.Error(), "ValidationException") {
				delete(currentAcctInstance.Status.OptInRegions, optInRegion)
				return nil
			}

		} else {
			reqLogger.Info(
				fmt.Sprintf("Region Enablement COMPLETED for RegionCode [%s]",
					optInRegion),
			)
			optInRegionRequest.Status = awsv1alpha1.OptInRequestEnabled
		}
	}
	currentAcctInstance.Status.OptInRegions[optInRegion].Status = optInRegionRequest.Status

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
	return reconcile.Result{RequeueAfter: 60 * time.Second, Requeue: true}, err
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
		reqLogger.Info(fmt.Sprintf("Handling Opt-In region request for %s", region))
		err = HandleOptInRegionRequests(regionLogger, awsAssumedRoleClient, region, regionRequest, currentAcctInstance)
		if err != nil {
			return err
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
			result, err = client.EnableRegion(context.TODO(), &account.EnableRegionInput{
				RegionName: aws.String(regionCode),
			})
			if err != nil {
				// Check for ConflictException
				var conflictErr *accounttypes.ConflictException
				if errors.As(err, &conflictErr) {
					alreadySubmitted = true
					return nil
				}
			}
			return err
		},

		retry.RetryIf(func(err error) bool {
			// Check for specific AWS Account exception types
			var accessDeniedErr *accounttypes.AccessDeniedException
			var tooManyRequestsErr *accounttypes.TooManyRequestsException
			var internalServerErr *accounttypes.InternalServerException

			switch {
			// AccessDenied may indicate the BYOCAdminAccess role has not yet propagated
			case errors.As(err, &accessDeniedErr):
				return true
			case errors.As(err, &tooManyRequestsErr):
				return true
			case errors.As(err, &internalServerErr):
				return true
			}

			// Check for generic errors not specific to account service
			var aerr smithy.APIError
			if errors.As(err, &aerr) {
				// Can be caused by the client token not yet propagated
				if aerr.ErrorCode() == "UnrecognizedClientException" {
					return true
				}
			}

			// Otherwise, do not retry
			return false
		}),
	)

	// If the attempt to submit a request returns ConflictException
	// then a request has already been submitted, since we first polled. No further action.
	if alreadySubmitted {
		return true, nil
	}

	// Otherwise, if there is an error, return the error to be handled
	if err != nil {
		return false, err
	}

	if result == nil {
		err := fmt.Errorf("returned EnableRegionOutput is nil")
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
			result, err = client.GetRegionOptStatus(context.TODO(), &account.GetRegionOptStatusInput{
				RegionName: aws.String(regionCode),
			})
			return err
		},

		// Retry if we receive some specific errors: access denied, rate limit or server-side error
		retry.RetryIf(func(err error) bool {
			// Check for specific AWS Account exception types
			var accessDeniedErr *accounttypes.AccessDeniedException
			var internalServerErr *accounttypes.InternalServerException
			var tooManyRequestsErr *accounttypes.TooManyRequestsException

			switch {
			// AccessDenied may indicate the BYOCAdminAccess role has not yet propagated
			case errors.As(err, &accessDeniedErr):
				return true
			case errors.As(err, &internalServerErr):
				return true
			case errors.As(err, &tooManyRequestsErr):
				return true
			}

			// Check for generic errors not specific to account service
			var aerr smithy.APIError
			if errors.As(err, &aerr) {
				// Can be caused by the client token not yet propagated
				if aerr.ErrorCode() == "UnrecognizedClientException" {
					return true
				}
			}

			// Otherwise, do not retry
			return false
		}),
	)

	if result.RegionOptStatus != "" {
		if result.RegionOptStatus != "ENABLED" {
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
		// Poll the region status with retries until we get a valid response
		var result *account.GetRegionOptStatusOutput

		err := retry.Do(
			func() (err error) {
				result, err = awsClient.GetRegionOptStatus(context.TODO(), &account.GetRegionOptStatusInput{
					RegionName: aws.String(regionCode),
				})
				return err
			},

			// Retry if we receive some specific errors: access denied, rate limit or server-side error
			retry.RetryIf(func(err error) bool {
				// Check for specific AWS Account exception types
				var accessDeniedErr *accounttypes.AccessDeniedException
				var internalServerErr *accounttypes.InternalServerException
				var tooManyRequestsErr *accounttypes.TooManyRequestsException

				switch {
				// AccessDenied may indicate the BYOCAdminAccess role has not yet propagated
				case errors.As(err, &accessDeniedErr):
					return true
				case errors.As(err, &internalServerErr):
					return true
				case errors.As(err, &tooManyRequestsErr):
					return true
				}

				// Check for generic errors not specific to account service
				var aerr smithy.APIError
				if errors.As(err, &aerr) {
					// Can be caused by the client token not yet propagated
					if aerr.ErrorCode() == "UnrecognizedClientException" {
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

		if result.RegionOptStatus != "" {
			switch result.RegionOptStatus {
			case "ENABLING":
				return awsv1alpha1.OptInRequestEnabling, nil
			case "ENABLED", "ENABLED_BY_DEFAULT":
				return awsv1alpha1.OptInRequestEnabled, nil
			case "DISABLED", "DISABLING":
				return awsv1alpha1.OptInRequestTodo, nil
			}
		}
	}
}

func SetOptRegionStatus(reqLogger logr.Logger, optInRegions []string, currentAcctInstance *awsv1alpha1.Account) error {
	reqLogger.Info("Setting Opt-In region status to todo of all Supported Opt-In regions")
	currentAcctInstance.Status.OptInRegions = make(awsv1alpha1.OptInRegions)
	for _, region := range optInRegions {
		currentAcctInstance.Status.OptInRegions[region] = &awsv1alpha1.OptInRegionStatus{
			Status: awsv1alpha1.OptInRequestTodo,
		}
	}
	return nil
}

func CalculateOptingInRegionAccounts(reqLogger logr.Logger, c client.Client) (int, error) {
	// Retrieve a list of accounts with region enablement in progress for supported Opt-In regions
	accountList := &awsv1alpha1.AccountList{}
	listOpts := []client.ListOption{
		client.InNamespace(awsv1alpha1.AccountCrNamespace),
	}
	numberOfAccountsOptingIn := 0

	if err := c.List(context.TODO(), accountList, listOpts...); err != nil {
		log.Error(err, "Failed to list accounts")
		if k8serr.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return numberOfAccountsOptingIn, err
		}
		// Error reading the object - requeue the request.
		return numberOfAccountsOptingIn, err
	}

	// since it's not possible to filter on custom field values when listing using the golang client
	// manual filtering of accounts opting-in is required to ensure the account limit is not reached

	for _, acct := range accountList.Items {
		if acct.Status.State == "OptingInRegions" || (acct.IsReady() && acct.HasOpenOptInRegionRequests()) {
			numberOfAccountsOptingIn += 1
		}
	}
	reqLogger.Info(
		fmt.Sprintf("Current number of accounts opting into regions: [%d]",
			numberOfAccountsOptingIn),
	)

	return numberOfAccountsOptingIn, nil
}
