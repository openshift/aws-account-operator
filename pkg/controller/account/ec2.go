package account

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/servicequotas"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/go-logr/logr"
	"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	controllerutils "github.com/openshift/aws-account-operator/pkg/controller/utils"

	retry "github.com/avast/retry-go"
)

// InitializeSupportedRegions concurrently calls InitializeRegion to create instances in all supported regions
// This should ensure we don't see any AWS API "PendingVerification" errors when launching instances
// NOTE: This function does not have any returns. In particular, error conditions from the
// goroutines are logged, but do not result in a failure up the stack.
func (r *ReconcileAccount) InitializeSupportedRegions(reqLogger logr.Logger, account *awsv1alpha1.Account, regions []v1alpha1.AwsRegions, creds *sts.AssumeRoleOutput, regionAMIs map[string]awsv1alpha1.AmiSpec) {
	// Create some channels to listen and error on when creating EC2 instances in all supported regions
	ec2Notifications, ec2Errors := make(chan string), make(chan string)

	// Make sure we close our channels when we're done
	defer close(ec2Notifications)
	defer close(ec2Errors)

	// We should not bomb out just because we can't retrieve the vCPU value
	// and we'll just continue with a "0"
	// Errors are logged already in getDesiredVCPUValue
	vCPUQuota, _ := r.getDesiredVCPUValue(reqLogger)
	reqLogger.Info("retrieved desired vCPU quota value from configMap", "quota.vcpu", vCPUQuota)

	managedTags := r.getManagedTags(reqLogger)
	customerTags := r.getCustomTags(reqLogger, account)

	// Create go routines to initialize regions in parallel

	for _, region := range regions {
		go r.InitializeRegion(reqLogger, account, region.Name, regionAMIs[region.Name], vCPUQuota, ec2Notifications, ec2Errors, creds, managedTags, customerTags) //nolint:errcheck // Unable to do anything with the returned error
	}

	// Wait for all go routines to send a message or error to notify that the region initialization has finished
	for i := 0; i < len(regions); i++ {
		select {
		case msg := <-ec2Notifications:
			reqLogger.Info(msg)
		case errMsg := <-ec2Errors:
			reqLogger.Error(errors.New(errMsg), errMsg)
		}
	}

	reqLogger.Info("Completed initializing all supported regions")
}

// InitializeRegion sets up a connection to the AWS `region` and then creates and terminates an EC2 instance if necessary
func (r *ReconcileAccount) InitializeRegion(
	reqLogger logr.Logger,
	account *awsv1alpha1.Account,
	region string,
	instanceInfo awsv1alpha1.AmiSpec,
	vCPUQuota float64,
	ec2Notifications chan string,
	ec2Errors chan string,
	creds *sts.AssumeRoleOutput,
	managedTags []awsclient.AWSTag,
	customerTags []awsclient.AWSTag,
) error {
	var quotaIncreaseRequired bool
	var caseID string

	awsClient, err := r.awsClientBuilder.GetClient(controllerName, r.Client, awsclient.NewAwsClientInput{
		AwsCredsSecretIDKey:     *creds.Credentials.AccessKeyId,
		AwsCredsSecretAccessKey: *creds.Credentials.SecretAccessKey,
		AwsToken:                *creds.Credentials.SessionToken,
		AwsRegion:               region,
	})

	if err != nil {
		connErr := fmt.Sprintf("unable to connect to region %s when attempting to initialize it", region)
		reqLogger.Error(err, connErr)
		// Notify Error channel that this region has errored and to move on
		ec2Errors <- connErr

		return err
	}

	reqLogger.Info("initializing region", "region", region)

	// Attempt to clean the region from any hanging resources
	cleaned, err := cleanRegion(awsClient, reqLogger, account.Name, region)
	if err != nil {
		cleanErr := fmt.Sprintf("Error while attempting to clean region: %v", err.Error())
		ec2Errors <- cleanErr
		return err
	}
	if cleaned {
		// Getting here indicates that the current region is already initialized
		// and had hanging t2.micro instances that were cleaned. We can forgo creating any new resources
		ec2Notifications <- fmt.Sprintf("Region %s was already innitialized", region)
		return nil
	}

	// If the quota is 0, there was an error and we cannot act on it
	if vCPUQuota != 0 {
		// Check if a request is necessary
		// If there are errors, this will return false, and will not continue to try
		// to set the quota
		quotaIncreaseRequired, err = vCPUQuotaNeedsIncrease(awsClient, vCPUQuota)
		if err != nil {
			reqLogger.Error(err, "failed retriving current vCPU quota from AWS")
		}
	}

	if quotaIncreaseRequired {
		reqLogger.Info("vCPU quota increase required", "region", region)
		caseID, err = checkQuotaRequestHistory(awsClient, vCPUQuota)
		if err != nil {
			reqLogger.Error(err, "failed retriving quota change history")
		}

		// If a Case ID was found, log it - the request was already submitted
		if caseID != "" {
			reqLogger.Info("found matching quota change request", "caseID", caseID)
		}

		// If there is not matching request already,
		// and there were no errors trying to retrieve them,
		// then request a quota increase
		if caseID == "" && err == nil {
			reqLogger.Info("submitting vCPU quota increase request", "region", region)
			caseID, err = setVCPUQuota(awsClient, vCPUQuota)
			if err != nil {
				reqLogger.Error(err, "failed requesting vCPU quota increase")
			}
		}

		// If the caseID is set, a quota increase was requested, either just now or previously. Log it.
		// Can't update account conditions from within the asyncRegionInit goroutine, because
		// the account is being updated elsewhere and will conflict.
		if caseID != "" {
			reqLogger.Info("quota increase request submitted successfully", "region", region, "caseID", caseID)
		}
	}

	err = r.BuildAndDestroyEC2Instances(reqLogger, account, awsClient, instanceInfo, managedTags, customerTags)

	if err != nil {
		createErr := fmt.Sprintf("Unable to create instance in region: %s", region)
		controllerutils.LogAwsError(reqLogger, createErr, nil, err)
		// Notify Error channel that this region has errored and to move on
		ec2Errors <- createErr

		return err
	}

	successMsg := fmt.Sprintf("EC2 instance created and terminated successfully in region: %s", region)

	// Notify Notifications channel that an instance has successfully been created and terminated and to move on
	ec2Notifications <- successMsg

	return nil
}

// BuildAndDestroyEC2Instances runs an ec2 instance and terminates it
func (r *ReconcileAccount) BuildAndDestroyEC2Instances(reqLogger logr.Logger, account *awsv1alpha1.Account, awsClient awsclient.Client, instanceInfo awsv1alpha1.AmiSpec, managedTags []awsclient.AWSTag, customerTags []awsclient.AWSTag) error {
	instanceID, err := CreateEC2Instance(reqLogger, account, awsClient, instanceInfo, managedTags, customerTags)
	if err != nil {
		// Terminate instance id if it exists
		if instanceID != "" {
			// Log instance id of instance that will be terminated
			reqLogger.Error(err, fmt.Sprintf("Early termination of instance with ID: %s", instanceID))
			termErr := TerminateEC2Instance(reqLogger, awsClient, instanceID)
			if termErr != nil {
				controllerutils.LogAwsError(reqLogger, "AWS error while attempting to terminate instance", nil, termErr)
			}
		}
		return err
	}

	// Wait till instance is running
	var DescError error
	totalWait := controllerutils.WaitTime * 60
	currentWait := 1
	// Double the wait time until we reach totalWait seconds
	for totalWait > 0 {
		currentWait = currentWait * 2
		if currentWait > totalWait {
			currentWait = totalWait
		}
		totalWait -= currentWait
		time.Sleep(time.Duration(currentWait) * time.Second)
		var code int
		code, DescError = DescribeEC2Instances(reqLogger, awsClient, instanceID)
		if code == 16 {
			reqLogger.Info(fmt.Sprintf("EC2 Instance: %s Running", instanceID))
			break
		}

	}

	if DescError != nil {
		// Log an error and make sure that instance is terminated
		DescErrorMsg := fmt.Sprintf("Could not get EC2 instance state, terminating instance %s", instanceID)

		if DescError, ok := err.(awserr.Error); ok {
			DescErrorMsg = fmt.Sprintf("Could not get EC2 instance state: %s, terminating instance %s", DescError.Code(), instanceID)
		}

		reqLogger.Error(DescError, DescErrorMsg)
	}

	// Terminate Instance
	reqLogger.Info(fmt.Sprintf("Terminating EC2 Instance: %s", instanceID))

	err = TerminateEC2Instance(reqLogger, awsClient, instanceID)
	if err != nil {
		return err
	}

	reqLogger.Info(fmt.Sprintf("EC2 Instance: %s Terminated", instanceID))

	return nil
}

// CreateEC2Instance creates ec2 instance and returns its instance ID
func CreateEC2Instance(reqLogger logr.Logger, account *awsv1alpha1.Account, client awsclient.Client, instanceInfo awsv1alpha1.AmiSpec, managedTags []awsclient.AWSTag, customerTags []awsclient.AWSTag) (string, error) {

	// Retain instance id
	var timeoutInstanceID string

	// Loop until an EC2 instance is created or timeout.
	totalWait := controllerutils.WaitTime * 60
	currentWait := 1
	for totalWait > 0 {
		currentWait = currentWait * 2
		if currentWait > totalWait {
			currentWait = totalWait
		}
		totalWait -= currentWait
		time.Sleep(time.Duration(currentWait) * time.Second)
		tags := awsclient.AWSTags.BuildTags(account, managedTags, customerTags).GetEC2Tags()
		// Specify the details of the instance that you want to create.
		runResult, runErr := client.RunInstances(&ec2.RunInstancesInput{
			ImageId:      aws.String(instanceInfo.Ami),
			InstanceType: aws.String(instanceInfo.InstanceType),
			MinCount:     aws.Int64(1),
			MaxCount:     aws.Int64(1),
			TagSpecifications: []*ec2.TagSpecification{
				{
					ResourceType: &awsv1alpha1.InstanceResourceType,
					Tags:         tags,
				},
				{
					ResourceType: &awsv1alpha1.VolumeResourceType,
					Tags:         tags,
				},
			},
		})

		// Return on unexpected errors:
		if runErr != nil {
			if aerr, ok := runErr.(awserr.Error); ok {
				// We want to ensure that we don't leave any instances around when there is an error
				// possible that there is no instance here
				if len(runResult.Instances) > 0 {
					timeoutInstanceID = *runResult.Instances[0].InstanceId
				}
				switch aerr.Code() {
				case "PendingVerification", "OptInRequired":
					continue
				default:
					controllerutils.LogAwsError(reqLogger, "Failed while trying to create EC2 instance", runErr, runErr)
					return timeoutInstanceID, runErr
				}
			}
			return timeoutInstanceID, v1alpha1.ErrFailedAWSTypecast
		}

		// No error was found, instance is running, return instance id
		return *runResult.Instances[0].InstanceId, nil
	}

	// Timeout occurred, return instance id and timeout error
	return timeoutInstanceID, v1alpha1.ErrCreateEC2Instance
}

// DescribeEC2Instances returns the InstanceState code
func DescribeEC2Instances(reqLogger logr.Logger, client awsclient.Client, instanceID string) (int, error) {
	// States and codes
	// 0 : pending
	// 16 : running
	// 32 : shutting-down
	// 48 : terminated
	// 64 : stopping
	// 80 : stopped

	result, err := client.DescribeInstanceStatus(&ec2.DescribeInstanceStatusInput{
		InstanceIds: aws.StringSlice([]string{instanceID}),
	})

	if err != nil {
		controllerutils.LogAwsError(reqLogger, "New AWS Error while describing EC2 instance", nil, err)
		return 0, err
	}

	if len(result.InstanceStatuses) > 1 {
		return 0, errors.New("More than one EC2 instance found")
	}

	if len(result.InstanceStatuses) == 0 {
		return 0, errors.New("No EC2 instances found")
	}
	return int(*result.InstanceStatuses[0].InstanceState.Code), nil
}

// TerminateEC2Instance terminates the ec2 instance from the instanceID provided
func TerminateEC2Instance(reqLogger logr.Logger, client awsclient.Client, instanceID string) error {
	_, err := client.TerminateInstances(&ec2.TerminateInstancesInput{
		InstanceIds: aws.StringSlice([]string{instanceID}),
	})
	if err != nil {
		controllerutils.LogAwsError(reqLogger, "New AWS Error while terminating EC2 instance", nil, err)
		return err
	}

	return nil
}

// ListEC2InstanceStatus returns a slice of EC2 instance statuses
func ListEC2InstanceStatus(reqLogger logr.Logger, client awsclient.Client) (*ec2.DescribeInstanceStatusOutput, error) {
	result, err := client.DescribeInstanceStatus(nil)

	if err != nil {
		controllerutils.LogAwsError(reqLogger, "New AWS Error Listing EC2 instance status", nil, err)
		return nil, err
	}

	return result, nil
}

// getDesiredVCPUValue retrieves the desired quota information from the operator configmap and converts it to a float64
func (r *ReconcileAccount) getDesiredVCPUValue(reqLogger logr.Logger) (float64, error) {
	var err error
	var vCPUQuota float64

	configMap, err := controllerutils.GetOperatorConfigMap(r.Client)
	v, ok := configMap.Data["quota.vcpu"]
	if !ok {
		err = awsv1alpha1.ErrInvalidConfigMap
	}
	if err != nil {
		reqLogger.Info("Failed getting desired vCPU quota from configmap data, defaulting quota to 0")
		return vCPUQuota, err
	}

	vCPUQuota, err = strconv.ParseFloat(v, 64)
	if err != nil {
		reqLogger.Info("Failed converting vCPU quota from configmap string to float64, defaulting quota to 0")
		return vCPUQuota, err
	}

	return vCPUQuota, nil
}

// getVCPUQUota returns the current set vCPU quota for the region
func vCPUQuotaNeedsIncrease(client awsclient.Client, desiredQuota float64) (bool, error) {
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
					QuotaCode:   aws.String(vCPUQuotaCode),
					ServiceCode: aws.String(vCPUServiceCode),
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
			return true, err
		}
	}

	// Otherwise return false (doesn't need increase) and any errors
	return false, err
}

// setRegionVCPUQuota sets the AWS quota limit for vCPUs in the region
// This just sends the request, and checks that it was submitted, and does not wait
func setVCPUQuota(client awsclient.Client, desiredQuota float64) (string, error) {
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
					ServiceCode:  aws.String(vCPUServiceCode),
					QuotaCode:    aws.String(vCPUQuotaCode),
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
		return "RequestAlreadyExists", nil
	}

	// Otherwise, if there is an error, return the error to be handled
	if err != nil {
		return "", err
	}

	if (servicequotas.RequestServiceQuotaIncreaseOutput{}) == *result {
		err := fmt.Errorf("returned RequestServiceQuotaIncreaseOutput is nil")
		return "", err
	}

	if (servicequotas.RequestedServiceQuotaChange{}) == *result.RequestedQuota {
		err := fmt.Errorf("returned RequestedServiceQuotasIncreaseOutput field RequestedServiceQuotaChange is nil")
		return "", err
	}

	err = retry.Do(
		func() (err error) {
			// If we were returned a Case ID, then the request was submitted
			var nilString *string
			if result.RequestedQuota.CaseId == nilString {
				err := fmt.Errorf("returned CaseID is nil")
				return err

			}
			return nil
		},
	)

	if err != nil {
		return "", err
	}

	return *result.RequestedQuota.CaseId, nil

}

// checkQuotaRequestHistory checks to see if a request for a quota increase has already been submitted
// This is not ideal, as each region has to check the history, since we have to intialize by region
// Ideally this would happen outside the region-specific init, but this requires the awsclient for the
// specific region.
func checkQuotaRequestHistory(awsClient awsclient.Client, vCPUQuota float64) (string, error) {
	var err error
	var nextToken *string
	var caseID string

	// Default is 1/10 of a second, but any retries we need to make should be delayed a few seconds
	// This also defaults to an exponential backoff, so we only need to try ~5 times, default is 10
	retry.DefaultDelay = 3 * time.Second
	retry.DefaultAttempts = uint(5)

	for {
		// This returns with pagination, so we have to iterate over the pagination data

		var result *servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaOutput
		var err error
		var submitted bool

		err = retry.Do(
			func() (err error) {
				// Get a (possibly paginated) list of quota change requests by quota
				result, err = awsClient.ListRequestedServiceQuotaChangeHistoryByQuota(
					&servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaInput{
						NextToken:   nextToken,
						ServiceCode: aws.String(vCPUServiceCode),
						QuotaCode:   aws.String(vCPUQuotaCode),
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
			return "", err
		}

		// Check all the returned requests to see if one matches the quota increase we'd request
		// If so, it's already been submitted
		for _, change := range result.RequestedQuotas {
			if changeRequestMatches(change, vCPUQuota) {
				submitted = true
				caseID = *change.CaseId
				break
			}
		}

		// If request has already been submitted, then break out
		if submitted {
			break
		}

		// If NextToken is empty, no more to try.  Break out
		if result.NextToken == nil {
			break
		}

		// Set NextToken to retrieve the next page and loop again
		nextToken = result.NextToken
	}

	return caseID, err

}

// changeRequestMatches returns true if the QuotaCode, ServiceCode and desired value match
func changeRequestMatches(change *servicequotas.RequestedServiceQuotaChange, quota float64) bool {
	if *change.ServiceCode != vCPUServiceCode {
		return false
	}

	if *change.QuotaCode != vCPUQuotaCode {
		return false
	}

	if *change.DesiredValue != quota {
		return false
	}

	return true
}

// cleanRegion will remove all hanging account creation t2.micro instances running in the current region
func cleanRegion(client awsclient.Client, logger logr.Logger, accountName string, region string) (bool, error) {
	cleaned := false
	// Make a dry run to certify we have required authentication
	_, err := client.DescribeInstances(&ec2.DescribeInstancesInput{
		DryRun: aws.Bool(true),
	})
	// If we receive an AuthFailure alert we do not attempt to clean this region
	if aerr, ok := err.(awserr.Error); ok {
		if aerr.Code() == "AuthFailure" {
			logger.Error(err, fmt.Sprintf("We do not have the correct authentication to clean or initialize region: %s backing out gracefully", region))
			return cleaned, err
		}
	}
	// Get a list of all running t2.micro instances
	output, err := client.DescribeInstances(&ec2.DescribeInstancesInput{
		MaxResults: aws.Int64(100),
		Filters: []*ec2.Filter{
			{
				Name: aws.String("instance-type"),
				Values: []*string{
					aws.String("t2.micro"),
				},
			},
			{
				Name: aws.String("instance-state-name"),
				Values: []*string{
					aws.String("running"),
				},
			},
			{
				Name: aws.String("tag-key"),
				Values: []*string{
					aws.String(awsv1alpha1.ClusterAccountNameTagKey),
				},
			},
			{
				Name: aws.String("tag-key"),
				Values: []*string{
					aws.String(awsv1alpha1.ClusterNamespaceTagKey),
				},
			},
			{
				Name: aws.String("tag-key"),
				Values: []*string{
					aws.String(awsv1alpha1.ClusterClaimLinkTagKey),
				},
			},
			{
				Name: aws.String("tag-key"),
				Values: []*string{
					aws.String(awsv1alpha1.ClusterClaimLinkNamespaceTagKey),
				},
			},
		},
	})
	if err != nil {
		logger.Error(err, "Error while describing instances")
		return cleaned, err
	}
	// Remove any hanging instances

	for _, reservation := range output.Reservations {
		for _, instance := range reservation.Instances {
			cleaned = true
			logger.Info("Terminating hanging instance", "instance", instance.InstanceId, "account", accountName)
			err = TerminateEC2Instance(logger, client, *instance.InstanceId)
			if err != nil {
				logger.Error(err, "Error while attempting to terminate instance", "instance", *instance.InstanceId)
				return false, err
			}
		}
	}
	return cleaned, nil
}
