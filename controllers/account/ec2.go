package account

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/config"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	controllerutils "github.com/openshift/aws-account-operator/pkg/utils"
)

type regionInitializationError struct {
	ErrorMsg string
	Region   string
}

// Constants used to retrieve instance types and AMIs:
// AMIs we use should be executable by everyone
const EXECUTABLEBY = "all"

// T3 and T2 micro instanes are free to start
const T3INSTANCETYPE = "t3.micro"
const T2INSTANCETYPE = "t2.micro"

// InitializeSupportedRegions concurrently calls InitializeRegion to create instances in all supported regions
// This should ensure we don't see any AWS API "PendingVerification" errors when launching instances
// NOTE: GovCloud regions skip initialization entirely as they are always BYOVPC.
// NOTE: This function does not have any returns. In particular, error conditions from the
// goroutines are logged, but do not result in a failure up the stack.
func (r *AccountReconciler) InitializeSupportedRegions(reqLogger logr.Logger, account *awsv1alpha1.Account, regions []awsv1alpha1.AwsRegions, creds *sts.AssumeRoleOutput, amiOwner string) {
	// Create some channels to listen and error on when creating EC2 instances in all supported regions
	ec2Notifications, ec2Errors := make(chan string), make(chan regionInitializationError)

	// Make sure we close our channels when we're done
	defer close(ec2Notifications)
	defer close(ec2Errors)

	// We should not bomb out just because we can't retrieve the vCPU value
	// and we'll just continue with a "0"
	// Errors are logged already in getDesiredVCPUValue
	vCPUQuota, _ := r.getDesiredServiceQuotaValue(reqLogger, "vcpu")
	reqLogger.Info("retrieved desired vCPU quota value from configMap", "quota.vcpu", vCPUQuota)

	var kmsKeyId string
	accountClaim, accountClaimError := r.getAccountClaim(account)
	if accountClaimError != nil {
		reqLogger.Info("Could not retrieve account claim for account.", "account", account.Name)
		kmsKeyId = ""
	} else {
		kmsKeyId = accountClaim.Spec.KmsKeyId
		reqLogger.Info("Retrieved KMS key to use", "KmsKeyID", kmsKeyId)
	}
	managedTags := r.getManagedTags(reqLogger)
	customerTags := r.getCustomTags(reqLogger, account)

	// Create go routines to initialize regions in parallel
	for _, region := range regions {
		go func() {
			// Errors are returned on the ec2Errors channel
			_ = r.InitializeRegion(reqLogger, account, region.Name, amiOwner, vCPUQuota, ec2Notifications, ec2Errors, creds, managedTags, customerTags, kmsKeyId)
		}()
	}

	var regionInitFailedRegion []string
	var regionInitFailed bool
	// Wait for all go routines to send a message or error to notify that the region initialization has finished
	for i := 0; i < len(regions); i++ {
		select {
		case msg := <-ec2Notifications:
			reqLogger.Info(msg)
		case errMsg := <-ec2Errors:
			regionInitFailed = true
			// If we fail to initialize the desired region we want to fail the account
			reqLogger.Error(errors.New(errMsg.ErrorMsg), errMsg.ErrorMsg)
			regionInitFailedRegion = append(regionInitFailedRegion, errMsg.Region)
		}
	}
	// If an account is BYOC or CCS and region initialization fails for the region expected, we want to fail the account else output success log
	if regionInitFailed && len(regions) == 1 {
		controllerutils.SetAccountStatus(
			account,
			fmt.Sprintf("Account %s failed to initialize expected region %v", account.Name, regionInitFailedRegion),
			awsv1alpha1.AccountInitializingRegions,
			AccountFailed,
		)
	} else {
		reqLogger.Info("Successfully completed initializing desired regions")
	}
}

// InitializeRegion initializes AWS regions for non-GovCloud environments by creating and terminating a test EC2 instance
// For GovCloud (FedRAMP), initialization is skipped entirely as regions are always BYOVPC
func (r *AccountReconciler) InitializeRegion(
	reqLogger logr.Logger,
	account *awsv1alpha1.Account,
	region string,
	amiOwner string,
	vCPUQuota float64,
	ec2Notifications chan string,
	ec2Errors chan regionInitializationError,
	creds *sts.AssumeRoleOutput,
	managedTags []awsclient.AWSTag,
	customerTags []awsclient.AWSTag,
	kmsKeyId string,
) error {
	awsClient, err := r.awsClientBuilder.GetClient(controllerName, r.Client, awsclient.NewAwsClientInput{
		AwsCredsSecretIDKey:     *creds.Credentials.AccessKeyId,
		AwsCredsSecretAccessKey: *creds.Credentials.SecretAccessKey,
		AwsToken:                *creds.Credentials.SessionToken,
		AwsRegion:               region,
	})

	if err != nil {
		connErr := fmt.Sprintf("unable to get AWS client when attempting to initialize region %s", region)
		reqLogger.Error(err, connErr)
		// Notify Error channel that this region has errored and to move on
		ec2Errors <- regionInitializationError{ErrorMsg: connErr, Region: region}

		return err
	}

	// Skip region initialization for GovCloud as it is always BYOVPC and never non-CCS
	// Customers in FedRAMP often do not have quota for extra VPCs
	if config.IsFedramp() {
		reqLogger.Info("Skipping region initialization for GovCloud (BYOVPC)", "region", region)
		ec2Notifications <- fmt.Sprintf("Region %s initialization skipped for GovCloud (BYOVPC)", region)
		return nil
	}

	reqLogger.Info("initializing region", "region", region)

	// Attempt to clean the region from any hanging resources
	cleaned, err := cleanRegion(awsClient, reqLogger, account.Name, region)
	if err != nil {
		cleanErr := fmt.Sprintf("Error while attempting to clean region: %v", err.Error())
		ec2Errors <- regionInitializationError{ErrorMsg: cleanErr, Region: region}
		return err
	}
	if cleaned {
		// Getting here indicates that the current region is already initialized
		// and had hanging t2.micro instances that were cleaned. We can forgo creating any new resources
		ec2Notifications <- fmt.Sprintf("Region %s was already initialized", region)
		return nil
	}

	// Attempt to gather data needed to launch the init EC2 instance
	instanceType, err := RetrieveAvailableMicroInstanceType(reqLogger, awsClient)
	if err != nil {
		determineTypesErr := fmt.Sprintf("Unable to determine available instance types in region: %s", region)
		controllerutils.LogAwsError(reqLogger, determineTypesErr, nil, err)
		ec2Errors <- regionInitializationError{ErrorMsg: determineTypesErr, Region: region}
		return err
	}
	ami, err := RetrieveAmi(awsClient, amiOwner)
	if err != nil {
		retrieveAmiErr := fmt.Sprintf("Unable to find suitable AMI in region: %s", region)
		controllerutils.LogAwsError(reqLogger, retrieveAmiErr, nil, err)
		ec2Errors <- regionInitializationError{ErrorMsg: retrieveAmiErr, Region: region}
		return err
	}
	instanceInfo := awsv1alpha1.AmiSpec{
		Ami:          ami,
		InstanceType: instanceType,
	}

	// If the quota is 0, there was an error and we cannot act on it
	if vCPUQuota != 0 {
		// ServiceQuotaStatus is not used for this code to track the status of this specific request.
		// That's why we pass an unused reference.
		err := HandleServiceQuotaRequests(reqLogger, awsClient, awsv1alpha1.RunningStandardInstances, &awsv1alpha1.ServiceQuotaStatus{
			Value: int(vCPUQuota),
		})
		if err != nil {
			return err
		}
	}

	err = r.BuildAndDestroyEC2Instances(reqLogger, account, awsClient, instanceInfo, managedTags, customerTags, kmsKeyId)
	if err != nil {
		createErr := fmt.Sprintf("Unable to create instance in region: %s", region)
		controllerutils.LogAwsError(reqLogger, createErr, nil, err)
		// Notify Error channel that this region has errored and to move on
		ec2Errors <- regionInitializationError{ErrorMsg: createErr, Region: region}
		return err
	}

	// Notify Notifications channel that an instance has successfully been created and terminated and to move on
	ec2Notifications <- fmt.Sprintf("EC2 instance created and terminated successfully in region: %s", region)

	return nil
}

// BuildAndDestroyEC2Instances runs an ec2 instance and terminates it
func (r *AccountReconciler) BuildAndDestroyEC2Instances(
	reqLogger logr.Logger,
	account *awsv1alpha1.Account,
	awsClient awsclient.Client,
	instanceInfo awsv1alpha1.AmiSpec,
	managedTags []awsclient.AWSTag,
	customerTags []awsclient.AWSTag,
	kmsKeyId string) error {
	instanceID, err := CreateEC2Instance(reqLogger, account, awsClient, instanceInfo, managedTags, customerTags, kmsKeyId)
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
		if code == 16 { // 16 represents a successful region initialization
			reqLogger.Info(fmt.Sprintf("EC2 Instance: %s Running", instanceID))
			break
		} else if code == 401 { // 401 represents an UnauthorizedOperation error
			// Missing permission to perform operations, account needs to fail
			reqLogger.Error(DescError, fmt.Sprintf("Missing required permissions for account %s", account.Name))
			return err
		}

	}

	if DescError != nil {
		// Log an error and make sure that instance is terminated
		DescErrorMsg := fmt.Sprintf("Could not get EC2 instance state, terminating instance %s", instanceID)

		var descApiErr smithy.APIError
		if errors.As(DescError, &descApiErr) {
			DescErrorMsg = fmt.Sprintf("Could not get EC2 instance state: %s, terminating instance %s", descApiErr.ErrorCode(), instanceID)
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
func CreateEC2Instance(reqLogger logr.Logger, account *awsv1alpha1.Account, client awsclient.Client, instanceInfo awsv1alpha1.AmiSpec, managedTags []awsclient.AWSTag, customerTags []awsclient.AWSTag, customerKmsKeyId string) (string, error) {

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

		ebsBlockDeviceSetup := &ec2types.EbsBlockDevice{
			VolumeSize:          aws.Int32(10),
			DeleteOnTermination: aws.Bool(true),
			Encrypted:           aws.Bool(true),
		}
		if customerKmsKeyId != "" {
			ebsBlockDeviceSetup.KmsKeyId = aws.String(customerKmsKeyId)
		}
		// Specify the details of the instance that you want to create.
		input := &ec2.RunInstancesInput{
			ImageId:      aws.String(instanceInfo.Ami),
			InstanceType: ec2types.InstanceType(instanceInfo.InstanceType),
			MinCount:     aws.Int32(1),
			MaxCount:     aws.Int32(1),
			TagSpecifications: []ec2types.TagSpecification{
				{
					ResourceType: ec2types.ResourceType(awsv1alpha1.InstanceResourceType),
					Tags:         tags,
				},
				{
					ResourceType: ec2types.ResourceType(awsv1alpha1.VolumeResourceType),
					Tags:         tags,
				},
			},
			// We specify block devices mainly to enable EBS encryption
			BlockDeviceMappings: []ec2types.BlockDeviceMapping{
				{
					DeviceName: aws.String("/dev/sda1"),
					Ebs:        ebsBlockDeviceSetup,
				},
			},
		}

		runResult, runErr := client.RunInstances(context.TODO(), input)

		// Return on unexpected errors:
		if runErr != nil {
			var aerr smithy.APIError
			if errors.As(runErr, &aerr) {
				// We want to ensure that we don't leave any instances around when there is an error
				// possible that there is no instance here
				if len(runResult.Instances) > 0 {
					timeoutInstanceID = *runResult.Instances[0].InstanceId
				}
				switch aerr.ErrorCode() {
				case "PendingVerification", "OptInRequired":
					continue
				default:
					controllerutils.LogAwsError(reqLogger, "Failed while trying to create EC2 instance", runErr, runErr)
					return timeoutInstanceID, runErr
				}
			}
			return timeoutInstanceID, awsv1alpha1.ErrFailedAWSTypecast
		}

		// No error was found, instance is running, return instance id
		return *runResult.Instances[0].InstanceId, nil
	}

	// Timeout occurred, return instance id and timeout error
	return timeoutInstanceID, awsv1alpha1.ErrCreateEC2Instance
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
	// 401 : failed

	result, err := client.DescribeInstanceStatus(context.TODO(), &ec2.DescribeInstanceStatusInput{
		InstanceIds: []string{instanceID},
	})

	if err != nil {
		controllerutils.LogAwsError(reqLogger, "New AWS Error while describing EC2 instance", nil, err)
		var aerr smithy.APIError
		if errors.As(err, &aerr) {
			if aerr.ErrorCode() == "UnauthorizedOperation" {
				return 401, err
			}
		}
		return 0, err
	}

	if len(result.InstanceStatuses) > 1 {
		return 0, errors.New("more than one EC2 instance found")
	}

	if len(result.InstanceStatuses) == 0 {
		return 0, errors.New("no EC2 instances found")
	}
	return int(*result.InstanceStatuses[0].InstanceState.Code), nil
}

// TerminateEC2Instance terminates the ec2 instance from the instanceID provided
func TerminateEC2Instance(reqLogger logr.Logger, client awsclient.Client, instanceID string) error {
	_, err := client.TerminateInstances(context.TODO(), &ec2.TerminateInstancesInput{
		InstanceIds: []string{instanceID},
	})
	if err != nil {
		controllerutils.LogAwsError(reqLogger, "New AWS Error while terminating EC2 instance", nil, err)
		return err
	}

	return nil
}

// cleanRegion will remove all hanging account creation t2.micro instances running in the current region
func cleanRegion(client awsclient.Client, logger logr.Logger, accountName string, region string) (bool, error) {
	var cleaned bool
	// Make a dry run to certify we have required authentication
	_, err := client.DescribeInstances(context.TODO(), &ec2.DescribeInstancesInput{
		DryRun: aws.Bool(true),
	})

	// If we receive an AuthFailure alert we do not attempt to clean this region
	var aerr smithy.APIError
	if errors.As(err, &aerr) {
		if aerr.ErrorCode() == "AuthFailure" {
			logger.Error(err, fmt.Sprintf("We do not have the correct authentication to clean or initialize region: %s backing out gracefully", region))
			return cleaned, err
		}
	}

	// Get the instance type that will be used for this region and filter by that one.
	instanceType, err := RetrieveAvailableMicroInstanceType(logger, client)
	if err != nil {
		return cleaned, err
	}
	// Get a list of all running t2.micro instances
	output, err := client.DescribeInstances(context.TODO(), &ec2.DescribeInstancesInput{
		MaxResults: aws.Int32(100),
		Filters: []ec2types.Filter{
			{
				Name: aws.String("instance-type"),
				Values: []string{
					instanceType,
				},
			},
			{
				Name: aws.String("instance-state-name"),
				Values: []string{
					"running",
				},
			},
			{
				Name: aws.String("tag-key"),
				Values: []string{
					awsv1alpha1.ClusterAccountNameTagKey,
				},
			},
			{
				Name: aws.String("tag-key"),
				Values: []string{
					awsv1alpha1.ClusterNamespaceTagKey,
				},
			},
			{
				Name: aws.String("tag-key"),
				Values: []string{
					awsv1alpha1.ClusterClaimLinkTagKey,
				},
			},
			{
				Name: aws.String("tag-key"),
				Values: []string{
					awsv1alpha1.ClusterClaimLinkNamespaceTagKey,
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
			logger.Info("Terminating hanging instance", "instance", instance.InstanceId, "account", accountName)
			err = TerminateEC2Instance(logger, client, *instance.InstanceId)
			if err != nil {
				logger.Error(err, "Error while attempting to terminate instance", "instance", *instance.InstanceId)
				return false, err
			}
			cleaned = true
		}
	}
	return cleaned, nil
}

// RetrieveAvailableMicroInstanceType finds the EC2 free tier instance type for a given region
func RetrieveAvailableMicroInstanceType(logger logr.Logger, awsClient awsclient.Client) (string, error) {
	// FIXME: For unknown reasons attempting to use the free-tier-eligible
	// filter from go returns *nothing*, but works fine from the CLI.
	// HTTP-requests looks the same using both options.
	availableTypes, err := awsClient.DescribeInstanceTypes(context.TODO(), &ec2.DescribeInstanceTypesInput{
		InstanceTypes: []ec2types.InstanceType{ec2types.InstanceType(T3INSTANCETYPE)},
	})
	if err != nil {
		var aerr smithy.APIError
		if errors.As(err, &aerr) {
			switch aerr.ErrorCode() {
			case "InvalidInstanceType":
				logger.Info("Did not find t3.micro - falling back to t2.micro")
				availableTypes, err := awsClient.DescribeInstanceTypes(context.TODO(), &ec2.DescribeInstanceTypesInput{
					InstanceTypes: []ec2types.InstanceType{ec2types.InstanceType(T2INSTANCETYPE)},
				})
				if err != nil {
					return "", err
				}
				return string(availableTypes.InstanceTypes[0].InstanceType), nil
			default:
				return "", err
			}
		}
		return "", err
	}
	return string(availableTypes.InstanceTypes[0].InstanceType), nil
}

func RetrieveAmi(awsClient awsclient.Client, amiOwner string) (string, error) {
	var imageId string
	input := ec2.DescribeImagesInput{
		ExecutableUsers: []string{EXECUTABLEBY},
		Owners:          []string{amiOwner},
		Filters: []ec2types.Filter{
			{
				Name: aws.String("architecture"),
				Values: []string{
					"x86_64",
				},
			},
		},
	}
	availableAmis, err := awsClient.DescribeImages(context.TODO(), &input)
	if err != nil {
		return "", err
	}
	for _, image := range availableAmis.Images {
		if strings.Contains(*image.Name, "SAP") || strings.Contains(*image.Name, "BETA") {
			continue
		}

		imageId = *image.ImageId
		break
	}
	if imageId == "" {
		return "", errors.New("could not find a valid AMI")
	}
	return imageId, nil
}
