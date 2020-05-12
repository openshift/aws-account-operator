package account

import (
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/go-logr/logr"
	"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	controllerutils "github.com/openshift/aws-account-operator/pkg/controller/utils"
)

// InitializeSupportedRegions concurrently calls InitalizeRegion to create instances in all supported regions
// This should ensure we don't see any AWS API "PendingVerification" errors when launching instances
func (r *ReconcileAccount) InitializeSupportedRegions(reqLogger logr.Logger, account *awsv1alpha1.Account, regions map[string]map[string]string, creds *sts.AssumeRoleOutput) error {
	// Create some channels to listen and error on when creating EC2 instances in all supported regions
	ec2Notifications, ec2Errors := make(chan string), make(chan string)

	// Make sure we close our channels when we're done
	defer close(ec2Notifications)
	defer close(ec2Errors)

	// Create go routines to initialize regions in parallel
	for region := range regions {
		go r.InitializeRegion(reqLogger, account, region, regions[region]["initializationAMI"], ec2Notifications, ec2Errors, creds)
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

	return nil
}

// InitializeRegion sets up a connection to the AWS `region` and then creates and terminates an EC2 instance
func (r *ReconcileAccount) InitializeRegion(reqLogger logr.Logger, account *awsv1alpha1.Account, region string, ami string, ec2Notifications chan string, ec2Errors chan string, creds *sts.AssumeRoleOutput) error {
	reqLogger.Info(fmt.Sprintf("Initializing region: %s", region))

	awsClient, err := awsclient.GetAWSClient(r.Client, awsclient.NewAwsClientInput{
		AwsCredsSecretIDKey:     *creds.Credentials.AccessKeyId,
		AwsCredsSecretAccessKey: *creds.Credentials.SecretAccessKey,
		AwsToken:                *creds.Credentials.SessionToken,
		AwsRegion:               region,
	})
	if err != nil {
		connErr := fmt.Sprintf("Unable to connect to region: %s when attempting to initialize it", region)
		reqLogger.Error(err, connErr)
		// Notify Error channel that this region has errored and to move on
		ec2Errors <- connErr

		return err
	}

	err = r.BuildandDestroyEC2Instances(reqLogger, account, awsClient, ami)

	if err != nil {
		createErr := fmt.Sprintf("Unable to create instance in region: %s", region)
		// Notify Error channel that this region has errored and to move on
		ec2Errors <- createErr

		return err
	}

	successMsg := fmt.Sprintf("EC2 instance created and terminated successfully in region: %s", region)

	// Notify Notifications channel that an instance has successfully been created and terminated and to move on
	ec2Notifications <- successMsg

	return nil
}

// BuildandDestroyEC2Instances runs and ec2 instance and terminates it
func (r *ReconcileAccount) BuildandDestroyEC2Instances(reqLogger logr.Logger, account *awsv1alpha1.Account, awsClient awsclient.Client, ami string) error {

	instanceID, err := CreateEC2Instance(reqLogger, account, awsClient, ami)
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
func CreateEC2Instance(reqLogger logr.Logger, account *awsv1alpha1.Account, client awsclient.Client, ami string) (string, error) {

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
		// Specify the details of the instance that you want to create.
		runResult, runErr := client.RunInstances(&ec2.RunInstancesInput{
			ImageId:      aws.String(ami),
			InstanceType: aws.String(awsInstanceType),
			MinCount:     aws.Int64(1),
			MaxCount:     aws.Int64(1),
			TagSpecifications: []*ec2.TagSpecification{
				{
					ResourceType: &awsv1alpha1.EC2ResourceType,
					Tags:         awsclient.AWSTags.BuildTags(account).GetEC2Tags(),
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
func DescribeEC2Instances(reqLogger logr.Logger, client awsclient.Client, instanceId string) (int, error) {
	// States and codes
	// 0 : pending
	// 16 : running
	// 32 : shutting-down
	// 48 : terminated
	// 64 : stopping
	// 80 : stopped

	result, err := client.DescribeInstanceStatus(&ec2.DescribeInstanceStatusInput{
		InstanceIds: aws.StringSlice([]string{instanceId}),
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
		controllerutils.LogAwsError(reqLogger, "New AWS Error while describing EC2 instance", nil, err)
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
