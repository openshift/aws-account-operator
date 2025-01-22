package account

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/sts"
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

var sampleCIDR = "10.0.0.0/16"

// InitializeSupportedRegions concurrently calls InitializeRegion to create instances in all supported regions
// This should ensure we don't see any AWS API "PendingVerification" errors when launching instances
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
		go r.InitializeRegion(reqLogger, account, region.Name, amiOwner, vCPUQuota, ec2Notifications, ec2Errors, creds, managedTags, customerTags, kmsKeyId) //nolint:errcheck // Unable to do anything with the returned error
	}

	var regionInitFailedRegion []string
	regionInitFailed := false
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

// InitializeRegion sets up a connection to the AWS `region` and then creates and terminates an EC2 instance if necessary
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
		connErr := fmt.Sprintf("unable to connect to region %s when attempting to initialize it", region)
		reqLogger.Error(err, connErr)
		// Notify Error channel that this region has errored and to move on
		ec2Errors <- regionInitializationError{ErrorMsg: connErr, Region: region}

		return err
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
		ec2Notifications <- fmt.Sprintf("Region %s was already innitialized", region)
		return nil
	}

	// If in fedramp, create vpc
	if config.IsFedramp() {
		reqLogger.Info("Performing region initialization pre-cleanup of resources", "account", account.Name, "region", region)
		// Attempt to clean the region from any hanging fedramp resources
		fedrampCleaned, err := cleanFedrampInitializationResources(reqLogger, awsClient, account.Name, region)
		if err != nil {
			fedrampCleanedErr := fmt.Sprintf("Error while attempting to clean fedramp region: %v", err.Error())
			ec2Errors <- regionInitializationError{ErrorMsg: fedrampCleanedErr, Region: region}
			return err
		}
		if fedrampCleaned {
			ec2Notifications <- fmt.Sprintf("Region %s was already innitialized", region)
			return nil
		}

		reqLogger.Info("Creating vpc", "account", account.Name, "region", region)
		vpcID, err := createVpc(reqLogger, awsClient, account, managedTags, customerTags)
		if err != nil {
			vpcErr := fmt.Sprintf("Error while attempting to create VPC: %s", vpcID)
			controllerutils.LogAwsError(reqLogger, vpcErr, nil, err)
			ec2Errors <- regionInitializationError{ErrorMsg: vpcErr, Region: region}
			return err
		}
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

	instanceType, err := RetrieveAvailableMicroInstanceType(reqLogger, awsClient)
	if err != nil {
		determineTypesErr := fmt.Sprintf("Unable to determine available instance types in region: %s", region)
		controllerutils.LogAwsError(reqLogger, determineTypesErr, nil, err)
		ec2Errors <- regionInitializationError{ErrorMsg: determineTypesErr, Region: region}
	}
	ami, err := RetrieveAmi(awsClient, amiOwner)
	if err != nil {
		retrieveAmiErr := fmt.Sprintf("Unable to find suitable AMI in region: %s", region)
		controllerutils.LogAwsError(reqLogger, retrieveAmiErr, nil, err)
		ec2Errors <- regionInitializationError{ErrorMsg: retrieveAmiErr, Region: region}
	}
	instanceInfo := awsv1alpha1.AmiSpec{
		Ami:          ami,
		InstanceType: instanceType,
	}

	err = r.BuildAndDestroyEC2Instances(reqLogger, account, awsClient, instanceInfo, managedTags, customerTags, kmsKeyId)
	if err != nil {
		createErr := fmt.Sprintf("Unable to create instance in region: %s", region)
		controllerutils.LogAwsError(reqLogger, createErr, nil, err)
		// Notify Error channel that this region has errored and to move on
		ec2Errors <- regionInitializationError{ErrorMsg: createErr, Region: region}
		return err
	}

	successMsg := fmt.Sprintf("EC2 instance created and terminated successfully in region: %s", region)
	// If in fedramp cleans up VPC and attached resources
	if config.IsFedramp() {
		reqLogger.Info("Performing region post-initialization cleanup of resources", "account", account.Name, "region", region)
		_, err = cleanFedrampInitializationResources(reqLogger, awsClient, account.Name, region)
		if err != nil {
			fedrampCleanedErr := fmt.Sprintf("Error while attempting to clean fedramp region: %v", err.Error())
			ec2Errors <- regionInitializationError{ErrorMsg: fedrampCleanedErr, Region: region}
			return err
		}
	}

	// Notify Notifications channel that an instance has successfully been created and terminated and to move on
	ec2Notifications <- successMsg

	return nil
}

// createVpc creates a vpc and returns the VpcId
func createVpc(reqLogger logr.Logger, client awsclient.Client, account *awsv1alpha1.Account, managedTags []awsclient.AWSTag, customTags []awsclient.AWSTag) (string, error) {
	// Retain vpcID
	var timeoutVpcID string
	// Loop until VPC is created or timeout, double wait time until totalWait seconds
	totalWait := controllerutils.WaitTime * 60
	currentWait := 1
	for totalWait > 0 {
		currentWait = currentWait * 2
		if currentWait > totalWait {
			currentWait = totalWait
		}
		totalWait -= currentWait
		time.Sleep(time.Duration(currentWait) * time.Second)
		tags := awsclient.AWSTags.BuildTags(account, managedTags, customTags).GetEC2Tags()
		input := &ec2.CreateVpcInput{
			CidrBlock: aws.String(sampleCIDR),
			TagSpecifications: []*ec2.TagSpecification{
				{
					ResourceType: &awsv1alpha1.VpcResourceType,
					Tags:         tags,
				},
			},
		}
		result, vpcErr := client.CreateVpc(input)
		if vpcErr != nil {
			if aerr, ok := vpcErr.(awserr.Error); ok {
				if *result.Vpc.VpcId != "" {
					timeoutVpcID = *result.Vpc.VpcId
				}
				switch aerr.Code() {
				case "PendingVerification", "OptInRequired":
					continue
				default:
					vpcCreateFailed := fmt.Sprintf("Error while attempting to create VPC: %v", *result.Vpc.VpcId)
					controllerutils.LogAwsError(reqLogger, vpcCreateFailed, vpcErr, vpcErr)
					return timeoutVpcID, vpcErr
				}
			}
			return timeoutVpcID, awsv1alpha1.ErrFailedAWSTypecast
		}
		// No error found, vpc running, return vpcID
		return *result.Vpc.VpcId, nil
	}
	// Timeout occurred, return vpcID and timeout error
	return timeoutVpcID, awsv1alpha1.ErrFailedToCreateVpc
}

// cleanFedrampSubnet removes all subnet in a given vpc
func cleanFedrampSubnet(reqLogger logr.Logger, client awsclient.Client, vpcIDtoDelete string) error {
	// Make dry run to certify required authentication
	_, err := client.DescribeSubnets(&ec2.DescribeSubnetsInput{
		DryRun: aws.Bool(true),
	})

	// If we receive AuthFailure, do not attempt to clean resources
	if aerr, ok := err.(awserr.Error); ok {
		if aerr.Code() == "AuthFailure" {
			reqLogger.Error(err, "We do not have the correct authentication to clean or initialize region. Backing out gracefully")
			return err
		}
	}

	// Get a list of all subnet
	result, err := client.DescribeSubnets(&ec2.DescribeSubnetsInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String("vpc-id"),
				Values: []*string{
					aws.String(vpcIDtoDelete),
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
		reqLogger.Error(err, "Error while describing subnets")
		return err
	}

	for _, subnet := range result.Subnets {
		reqLogger.Info("Delete hanging subnet", "subnet", subnet.SubnetId)
		subnetErr := deleteSubnet(reqLogger, client, *subnet.SubnetId)
		if subnetErr != nil {
			subnetErrMsg := fmt.Sprintf("Error while attempting to delete subnet: %s", *subnet.SubnetId)
			controllerutils.LogAwsError(reqLogger, subnetErrMsg, nil, subnetErr)
			return awsv1alpha1.ErrFailedToDeleteSubnet
		}
	}
	return nil
}

// deleteVpc deletes a vpc and returns err
func deleteFedrampInitializationResources(reqLogger logr.Logger, client awsclient.Client, vpcIDtoDelete string) error {
	subnetErr := cleanFedrampSubnet(reqLogger, client, vpcIDtoDelete)
	if subnetErr != nil {
		subnetErrMsg := fmt.Sprintf("Error while handling subnet deletion in vpc: %s", vpcIDtoDelete)
		controllerutils.LogAwsError(reqLogger, subnetErrMsg, subnetErr, subnetErr)
		return subnetErr
	}

	totalWait := controllerutils.WaitTime * 60
	currentWait := 1
	for totalWait > 0 {
		currentWait = currentWait * 2
		if currentWait > totalWait {
			currentWait = totalWait
		}
		totalWait -= currentWait
		time.Sleep(time.Duration(currentWait) * time.Second)
		reqLogger.Info("Attempting to delete", "vpc", vpcIDtoDelete)
		_, vpcErr := client.DeleteVpc(&ec2.DeleteVpcInput{
			VpcId: aws.String(vpcIDtoDelete),
		})
		if vpcErr != nil {
			if aerr, ok := vpcErr.(awserr.Error); ok {
				switch aerr.Code() {
				case "PendingVerification", "OptInRequired", "DependencyViolation":
					continue
				default:
					vpcErrMsg := fmt.Sprintf("Error while attempting to delete VPC: %s", vpcIDtoDelete)
					controllerutils.LogAwsError(reqLogger, vpcErrMsg, vpcErr, vpcErr)
					return vpcErr
				}
			}
			return awsv1alpha1.ErrFailedAWSTypecast
		}
		return nil
	}
	return awsv1alpha1.ErrFailedToDeleteVpc
}

// retrieveVpcs list of all VPCs with appropriate tag
func retrieveVpcs(reqLogger logr.Logger, client awsclient.Client, region string) (*ec2.DescribeVpcsOutput, bool, error) {
	cleaned := false
	// Make dry run to certify required authentication
	_, err := client.DescribeVpcs(&ec2.DescribeVpcsInput{
		DryRun: aws.Bool(true),
	})

	// If we receive AuthFailure, do not attempt to clean resources
	if aerr, ok := err.(awserr.Error); ok {
		if aerr.Code() == "AuthFailure" {
			reqLogger.Error(err, fmt.Sprintf("We do not have the correct authentication to clean or initialize region: %s backing out gracefully", region))
			return nil, cleaned, err
		}
	}

	// Get a list of all VPCs with appropriate tag
	result, err := client.DescribeVpcs(&ec2.DescribeVpcsInput{
		MaxResults: aws.Int64(5),
		Filters: []*ec2.Filter{
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
		reqLogger.Error(err, "Error while describing VPCs")
		return nil, cleaned, err
	}
	return result, cleaned, err
}

// cleanFedrampInitializationResources removes all hanging fedramp resources
func cleanFedrampInitializationResources(reqLogger logr.Logger, client awsclient.Client, accountName, region string) (bool, error) {
	cleaned := false
	result, cleaned, err := retrieveVpcs(reqLogger, client, region)
	if err != nil {
		return cleaned, err
	}
	reqLogger.Info("Retrieved %v vpcs", len(result.Vpcs))

	for _, vpc := range result.Vpcs {
		cleaned = true
		reqLogger.Info("Delete hanging resources", "vpc", vpc.VpcId, "account", accountName)
		err = deleteFedrampInitializationResources(reqLogger, client, *vpc.VpcId)
		if err != nil {
			reqLogger.Error(err, "Error while attempting to delete fedramp initialization resources", "vpcID", *vpc.VpcId)
			return false, err
		}
	}
	return cleaned, nil
}

// createSubnet takes in a cirdBlock and vpcID and returns the subnetID
func createSubnet(reqLogger logr.Logger, client awsclient.Client, account *awsv1alpha1.Account, managedTags []awsclient.AWSTag, customTags []awsclient.AWSTag, cirdBlock, vpcID string) (string, error) {
	tags := awsclient.AWSTags.BuildTags(account, managedTags, customTags).GetEC2Tags()
	input := &ec2.CreateSubnetInput{
		CidrBlock: aws.String(cirdBlock),
		VpcId:     aws.String(vpcID),
		TagSpecifications: []*ec2.TagSpecification{
			{
				ResourceType: &awsv1alpha1.SubnetResourceType,
				Tags:         tags,
			},
		},
	}

	result, subnetErr := client.CreateSubnet(input)
	if subnetErr != nil {
		if aerr, ok := subnetErr.(awserr.Error); ok {
			switch aerr.Code() {
			default:
				subnetCreateFailed := fmt.Sprintf("Error while attempting to create subnet: %v", *result.Subnet.SubnetId)
				controllerutils.LogAwsError(reqLogger, subnetCreateFailed, subnetErr, subnetErr)
				return *result.Subnet.SubnetId, subnetErr
			}
		}
		return *result.Subnet.SubnetId, awsv1alpha1.ErrFailedAWSTypecast
	}
	return *result.Subnet.SubnetId, nil
}

// deleteSubnet takes in subnetID and returns err
func deleteSubnet(reqLogger logr.Logger, client awsclient.Client, subnetToDelete string) error {
	totalWait := controllerutils.WaitTime * 60
	currentWait := 1
	for totalWait > 0 {
		currentWait = currentWait * 2
		if currentWait > totalWait {
			currentWait = totalWait
		}
		totalWait -= currentWait
		time.Sleep(time.Duration(currentWait) * time.Second)

		_, subnetErr := client.DeleteSubnet(&ec2.DeleteSubnetInput{
			SubnetId: aws.String(subnetToDelete),
		})
		if subnetErr != nil {
			if aerr, ok := subnetErr.(awserr.Error); ok {
				switch aerr.Code() {
				case "PendingVerification", "OptInRequired", "DependencyViolation":
					continue
				default:
					subnetDeleteErr := fmt.Sprintf("Error while attempting to delete subnet: %s", subnetToDelete)
					controllerutils.LogAwsError(reqLogger, subnetDeleteErr, subnetErr, subnetErr)
					return subnetErr
				}
			}
			return awsv1alpha1.ErrFailedAWSTypecast
		}
		// No error found, subnet deleted, return nil
		return nil
	}
	// Timeout occurred, return err
	return awsv1alpha1.ErrFailedToDeleteSubnet
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

		ebsBlockDeviceSetup := &ec2.EbsBlockDevice{
			VolumeSize:          aws.Int64(10),
			DeleteOnTermination: aws.Bool(true),
			Encrypted:           aws.Bool(true),
		}
		if customerKmsKeyId != "" {
			ebsBlockDeviceSetup.KmsKeyId = aws.String(customerKmsKeyId)
		}
		// Specify the details of the instance that you want to create.
		input := &ec2.RunInstancesInput{
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
			// We specify block devices mainly to enable EBS encryption
			BlockDeviceMappings: []*ec2.BlockDeviceMapping{
				{
					DeviceName: aws.String("/dev/sda1"),
					Ebs:        ebsBlockDeviceSetup,
				},
			},
		}

		// If fedramp, create subnet and set value for RunInstancesInput
		if config.IsFedramp() {
			// Get a list of all VPCs with appropriate tag
			result, _, err := retrieveVpcs(reqLogger, client, "")
			if err != nil {
				controllerutils.LogAwsError(reqLogger, "Error finding vpcID", nil, err)
			}

			if result != nil && len(result.Vpcs) > 0 {
				subnetID, err := createSubnet(reqLogger, client, account, managedTags, customerTags, sampleCIDR, *result.Vpcs[0].VpcId)

				if err != nil {
					subnetErr := fmt.Sprintf("Error while trying to create subnet: %s", subnetID)
					controllerutils.LogAwsError(reqLogger, subnetErr, nil, err)
				}
				input.SubnetId = aws.String(subnetID)
			}
		}
		runResult, runErr := client.RunInstances(input)

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

	result, err := client.DescribeInstanceStatus(&ec2.DescribeInstanceStatusInput{
		InstanceIds: aws.StringSlice([]string{instanceID}),
	})

	if err != nil {
		controllerutils.LogAwsError(reqLogger, "New AWS Error while describing EC2 instance", nil, err)
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == "UnauthorizedOperation" {
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
	// Get the instance type that will be used for this region and filter by that one.
	instanceType, err := RetrieveAvailableMicroInstanceType(logger, client)
	if err != nil {
		return cleaned, err
	}
	// Get a list of all running t2.micro instances
	output, err := client.DescribeInstances(&ec2.DescribeInstancesInput{
		MaxResults: aws.Int64(100),
		Filters: []*ec2.Filter{
			{
				Name: aws.String("instance-type"),
				Values: []*string{
					aws.String(instanceType),
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

// Get the free instance type for the client's region
func RetrieveAvailableMicroInstanceType(logger logr.Logger, awsClient awsclient.Client) (string, error) {
	// FIXME: For unknown reasons attempting to use the free-tier-eligible
	// filter from go returns *nothing*, but works fine from the CLI.
	// HTTP-requests looks the same using both options.
	availableTypes, err := awsClient.DescribeInstanceTypes(&ec2.DescribeInstanceTypesInput{
		InstanceTypes: []*string{aws.String(T3INSTANCETYPE)},
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case "InvalidInstanceType":
				logger.Info("Did not find t3.micro - falling back to t2.micro")
				availableTypes, err := awsClient.DescribeInstanceTypes(&ec2.DescribeInstanceTypesInput{
					InstanceTypes: []*string{aws.String(T2INSTANCETYPE)},
				})
				if err != nil {
					return "", err
				}
				return *availableTypes.InstanceTypes[0].InstanceType, nil
			default:
				return "", err
			}
		}
		return "", err
	}
	return *availableTypes.InstanceTypes[0].InstanceType, nil
}

func RetrieveAmi(awsClient awsclient.Client, amiOwner string) (string, error) {
	var ami string
	input := ec2.DescribeImagesInput{
		ExecutableUsers: []*string{aws.String(EXECUTABLEBY)},
		Owners:          []*string{&amiOwner},
	}
	availableAmis, err := awsClient.DescribeImages(&input)
	if err != nil {
		return "", err
	}
	for _, image := range availableAmis.Images {
		if *image.Architecture != "x86_64" {
			continue
		}
		if strings.Contains(*image.Name, "SAP") {
			continue
		}
		if strings.Contains(*image.Name, "BETA") {
			continue
		}
		ami = *image.ImageId
		break
	}
	if ami == "" {
		return "", errors.New("Could not find a matching AMI.")
	}
	return ami, nil
}
