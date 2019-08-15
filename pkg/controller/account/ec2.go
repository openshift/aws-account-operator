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
	"github.com/openshift/aws-account-operator/pkg/awsclient"
)

// InitializeSupportedRegions concurrently calls InitalizeRegion to create instances in all supported regions
// This should ensure we don't see any AWS API "PendingVerification" errors when launching instances
func (r *ReconcileAccount) InitializeSupportedRegions(reqLogger logr.Logger, regions map[string]map[string]string, creds *sts.AssumeRoleOutput) error {
	// Create some channels to listen and error on when creating EC2 instances in all supported regions
	ec2Notifications, ec2Errors := make(chan string), make(chan string)

	// Make sure we close our channels when we're done
	defer close(ec2Notifications)
	defer close(ec2Errors)

	// Create go routines to initialize regions in parallel
	for region := range regions {
		go r.InitializeRegion(reqLogger, region, regions[region]["initializationAMI"], ec2Notifications, ec2Errors, creds)
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
func (r *ReconcileAccount) InitializeRegion(reqLogger logr.Logger, region string, ami string, ec2Notifications chan string, ec2Errors chan string, creds *sts.AssumeRoleOutput) error {
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

	err = r.BuildandDestroyEC2Instances(reqLogger, awsClient, ami)

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
func (r *ReconcileAccount) BuildandDestroyEC2Instances(reqLogger logr.Logger, awsClient awsclient.Client, ami string) error {

	instanceID, err := CreateEC2Instance(reqLogger, awsClient, ami)
	if err != nil {
		return err
	}

	// Wait till instance is running
	var DescError error
	for i := 0; i < 300; i++ {
		var code int
		time.Sleep(1 * time.Second)
		code, DescError = DescribeEC2Instances(reqLogger, awsClient)
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
func CreateEC2Instance(reqLogger logr.Logger, client awsclient.Client, ami string) (string, error) {
	// Create EC2 service client

	var instanceID string
	var runErr error
	attempt := 1
	for i := 0; i < 300; i++ {
		time.Sleep(time.Duration(attempt*5) * time.Second)
		attempt++
		if attempt%5 == 0 {
			attempt = attempt * 2
		}
		// Specify the details of the instance that you want to create.
		runResult, runErr := client.RunInstances(&ec2.RunInstancesInput{
			// An Amazon Linux AMI ID for t2.micro instances in the us-west-2 region
			ImageId:      aws.String(ami),
			InstanceType: aws.String(awsInstanceType),
			MinCount:     aws.Int64(1),
			MaxCount:     aws.Int64(1),
		})
		if runErr == nil {
			instanceID = *runResult.Instances[0].InstanceId
			break
		}

	}

	if runErr != nil {
		return "", runErr
	}

	return instanceID, nil

}

// DescribeEC2Instances returns the InstanceState code
func DescribeEC2Instances(reqLogger logr.Logger, client awsclient.Client) (int, error) {
	// States and codes
	// 0 : pending
	// 16 : running
	// 32 : shutting-down
	// 48 : terminated
	// 64 : stopping
	// 80 : stopped

	result, err := ListEC2InstanceStatus(reqLogger, client)

	if err != nil {
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

		if aerr, ok := err.(awserr.Error); ok {
			terminateErrMsg := fmt.Sprintf("Unable to terminate EC2 instance %s: %s", instanceID, aerr.Code())
			reqLogger.Error(aerr, terminateErrMsg)
			return err
		}

		return err
	}

	return nil
}

// ListEC2InstanceStatus returns a slice of EC2 instance statuses
func ListEC2InstanceStatus(reqLogger logr.Logger, client awsclient.Client) (*ec2.DescribeInstanceStatusOutput, error) {
	result, err := client.DescribeInstanceStatus(nil)

	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			descErrMsg := fmt.Sprintf("Unable to describe EC2 instances %s", aerr.Code())
			reqLogger.Error(aerr, descErrMsg)
			return nil, err
		}

		return nil, err
	}

	return result, nil
}
