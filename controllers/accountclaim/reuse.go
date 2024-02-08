package accountclaim

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/openshift/aws-account-operator/config"
	stsclient "github.com/openshift/aws-account-operator/pkg/awsclient/sts"

	"github.com/rkt/rkt/tests/testutils/logger"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/localmetrics"
	"github.com/openshift/aws-account-operator/pkg/utils"
)

const (
	// AccountReady indicates account creation is ready
	AccountReady = "Ready"
	// AccountFailed indicates account reuse has failed
	AccountFailed = "Failed"
)

func (r *AccountClaimReconciler) finalizeAccountClaim(reqLogger logr.Logger, accountClaim *awsv1alpha1.AccountClaim) error {

	// Get account claimed by deleted accountclaim
	reusedAccount, err := r.getClaimedAccount(accountClaim.Spec.AccountLink, awsv1alpha1.AccountCrNamespace)
	if err != nil {
		// This check ensures that if a BYOC Account CR gets deleted, the rest of the BYOC finalizer logic can still run
		if !accountClaim.Spec.BYOC {
			reqLogger.Error(err, "Failed to get claimed account")
			return err
		}
		// Cleanup BYOC secret
		secretErr := r.removeBYOCSecretFinalizer(accountClaim)
		if secretErr != nil {
			reqLogger.Error(err, "Failed to remove BYOC iamsecret finalizer")
			return secretErr
		}

		// Here we are returning nil, instead of a potential err,
		// as we only want to block if it's non-CCS where we can't cleanup.
		return nil
	}

	// If the reused account is STS, then we don't have to clean up
	if reusedAccount.Spec.ManualSTSMode {
		err := r.Client.Delete(context.TODO(), reusedAccount)
		if err != nil {
			reqLogger.Error(err, "Failed to delete STS account from accountclaim cleanup")
			return err
		}
		return nil
	}

	var awsClient awsclient.Client
	var awsClientInput awsclient.NewAwsClientInput

	clusterAwsRegion := accountClaim.Spec.Aws.Regions[0].Name
	if reusedAccount.IsBYOC() {
		// AWS credential comes from accountclaim object osdCcsAdmin user
		// We must use this user as we would other delete the osdManagedAdmin
		// user that we're going to delete
		// TODO: We should use the role here
		awsClientInput = awsclient.NewAwsClientInput{
			SecretName: accountClaim.Spec.BYOCSecretRef.Name,
			NameSpace:  accountClaim.Namespace,
			AwsRegion:  clusterAwsRegion,
		}
		awsClient, err = r.awsClientBuilder.GetClient(controllerName, r.Client, awsClientInput)
		if err != nil {
			connErr := fmt.Sprintf("Unable to create aws client for region %s", clusterAwsRegion)
			reqLogger.Error(err, connErr)
			return err
		}
	} else {
		defaultRegion := config.GetDefaultRegion()
		// We expect this secret to exist in the same namespace Account CR's are created
		awsSetupClient, err := r.awsClientBuilder.GetClient(controllerName, r.Client, awsclient.NewAwsClientInput{
			SecretName: utils.AwsSecretName,
			NameSpace:  awsv1alpha1.AccountCrNamespace,
			AwsRegion:  defaultRegion,
		})
		if err != nil {
			reqLogger.Error(err, "failed building operator AWS client")
			return err
		}

		// This can not be the default region us-east-1 when cleaning up S3 buckets that live in other regions (if the cluster is not in us-east-1):
		// e.g. https://github.com/parallelworks/interactive_session/pull/65
		awsClient, _, err = stsclient.HandleRoleAssumption(reqLogger, r.awsClientBuilder, reusedAccount, r.Client, awsSetupClient, clusterAwsRegion, awsv1alpha1.AccountOperatorIAMRole, "")
		if err != nil {
			connErr := fmt.Sprintf("Unable to create aws client for region %s", clusterAwsRegion)
			reqLogger.Error(err, connErr)
			return err
		}
	}

	if reusedAccount.IsBYOC() {
		err := r.Client.Delete(context.TODO(), reusedAccount)
		if err != nil {
			reqLogger.Error(err, "Failed to delete BYOC account from accountclaim cleanup")
			return err
		}

		// Cleanup BYOC secret
		err = r.removeBYOCSecretFinalizer(accountClaim)
		if err != nil {
			reqLogger.Error(err, "Failed to remove BYOC secret finalizer")
			return err
		}

		return nil
	}

	before := time.Now()
	// Perform account clean up in AWS
	err = r.cleanUpAwsAccount(reqLogger, awsClient)
	if err != nil {
		localmetrics.Collector.AddAccountReuseCleanupFailure()
		reqLogger.Error(err, "Failed to clean up AWS account")
		return err
	}
	localmetrics.Collector.SetAccountReusedCleanupDuration(time.Since(before).Seconds())

	err = r.resetAccountSpecStatus(reqLogger, reusedAccount, accountClaim, awsv1alpha1.AccountReused, "Ready")
	if err != nil {
		reqLogger.Error(err, "Failed to reset account entity")
		return err
	}

	reqLogger.Info("Successfully finalized AccountClaim")
	return nil
}

func (r *AccountClaimReconciler) resetAccountSpecStatus(reqLogger logr.Logger, reusedAccount *awsv1alpha1.Account, deletedAccountClaim *awsv1alpha1.AccountClaim, accountState awsv1alpha1.AccountConditionType, conditionStatus string) error {

	// Reset claimlink and carry over legal entity from deleted claim
	reusedAccount.Spec.ClaimLink = ""
	reusedAccount.Spec.ClaimLinkNamespace = ""

	// LegalEntity is being carried over here to support older accounts, that were claimed
	// prior to the introduction of reuse (their account's legalEntity will be blank )
	if reusedAccount.Spec.LegalEntity.ID == "" {
		reusedAccount.Spec.LegalEntity.ID = deletedAccountClaim.Spec.LegalEntity.ID
		reusedAccount.Spec.LegalEntity.Name = deletedAccountClaim.Spec.LegalEntity.Name
	}

	err := r.accountSpecUpdate(reqLogger, reusedAccount)
	if err != nil {
		reqLogger.Error(err, "Failed to update account spec for reuse")
		return err
	}

	reqLogger.Info(fmt.Sprintf(
		"Setting RotateCredentials and RotateConsoleCredentials for account %s", reusedAccount.Spec.AwsAccountID))
	reusedAccount.Status.RotateConsoleCredentials = true
	reusedAccount.Status.RotateCredentials = true

	// Update account status and add conditions indicating account reuse
	reusedAccount.Status.State = conditionStatus
	reusedAccount.Status.Claimed = false
	reusedAccount.Status.Reused = true
	conditionMsg := fmt.Sprintf("Account Reuse - %s", conditionStatus)
	utils.SetAccountStatus(reusedAccount, conditionMsg, accountState, conditionStatus)
	err = r.accountStatusUpdate(reqLogger, reusedAccount)
	if err != nil {
		reqLogger.Error(err, "Failed to update account status for reuse")
		return err
	}

	return nil
}

func (r *AccountClaimReconciler) cleanUpAwsAccount(reqLogger logr.Logger, awsClient awsclient.Client) error {
	// Clean up status, used to store an error if any of the cleanup functions received one
	cleanUpStatusFailed := false

	// Channels to track clean up functions
	awsNotifications, awsErrors := make(chan string), make(chan string)

	defer close(awsNotifications)
	defer close(awsErrors)

	// Declare un array of cleanup functions
	cleanUpFunctions := []func(logr.Logger, awsclient.Client, chan string, chan string) error{
		r.cleanUpAwsAccountSnapshots,
		r.cleanUpAwsAccountEbsVolumes,
		r.cleanUpAwsAccountS3,
		r.CleanUpAwsAccountVpcEndpointServiceConfigurations,
		r.cleanUpAwsRoute53,
	}

	// Call the clean up functions in parallel
	for _, cleanUpFunc := range cleanUpFunctions {
		//nolint:errcheck // Not checking return value of goroutine
		go cleanUpFunc(reqLogger, awsClient, awsNotifications, awsErrors)
	}

	var err error
	// Wait for clean up functions to end
	for i := 0; i < len(cleanUpFunctions); i++ {
		select {
		case msg := <-awsNotifications:
			reqLogger.Info(msg)
		case errMsg := <-awsErrors:
			err = errors.New(errMsg)
			reqLogger.Error(err, errMsg)
			cleanUpStatusFailed = true
		}
	}

	// Return an error if we saw any errors on the awsErrors channel so we can make the reused account as failed
	if cleanUpStatusFailed {
		cleanUpStatusFailedMsg := "failed to clean up AWS account"
		reqLogger.Error(err, cleanUpStatusFailedMsg)
		return err
	}

	reqLogger.Info("AWS account cleanup completed")

	return nil
}

func (r *AccountClaimReconciler) cleanUpAwsAccountSnapshots(reqLogger logr.Logger, awsClient awsclient.Client, awsNotifications chan string, awsErrors chan string) error {

	// Filter only for snapshots owned by the account
	selfOwnerFilter := ec2.Filter{
		Name: aws.String("owner-alias"),
		Values: []*string{
			aws.String("self"),
		},
	}
	describeSnapshotsInput := ec2.DescribeSnapshotsInput{
		Filters: []*ec2.Filter{
			&selfOwnerFilter,
		},
	}
	ebsSnapshots, err := awsClient.DescribeSnapshots(&describeSnapshotsInput)
	if err != nil {
		descError := "Failed describing EBS snapshots"
		awsErrors <- descError
		return err
	}

	for _, snapshot := range ebsSnapshots.Snapshots {

		deleteSnapshotInput := ec2.DeleteSnapshotInput{
			SnapshotId: aws.String(*snapshot.SnapshotId),
		}

		_, err = awsClient.DeleteSnapshot(&deleteSnapshotInput)
		if err != nil {
			delError := fmt.Errorf("failed deleting EBS snapshot: %s: %w", *snapshot.SnapshotId, err).Error()
			awsErrors <- delError
			return err
		}
	}

	successMsg := "Snapshot cleanup finished successfully"
	awsNotifications <- successMsg
	return nil
}

func (r *AccountClaimReconciler) CleanUpAwsAccountVpcEndpointServiceConfigurations(reqLogger logr.Logger, awsClient awsclient.Client, awsNotifications chan string, awsErrors chan string) error {
	describeVpcEndpointServiceConfigurationsInput := ec2.DescribeVpcEndpointServiceConfigurationsInput{}
	vpcEndpointServiceConfigurations, err := awsClient.DescribeVpcEndpointServiceConfigurations(&describeVpcEndpointServiceConfigurationsInput)
	if vpcEndpointServiceConfigurations == nil || err != nil {
		descError := "Failed describing VPC endpoint service configurations"
		awsErrors <- descError
		return err
	}

	serviceIds := []*string{}

	for _, config := range vpcEndpointServiceConfigurations.ServiceConfigurations {
		serviceIds = append(serviceIds, config.ServiceId)
	}

	successMsg := "VPC endpoint service configuration cleanup finished successfully"
	if len(serviceIds) == 0 {
		awsNotifications <- successMsg + " (nothing to do)"
		return nil
	}

	deleteVpcEndpointServiceConfigurationsInput := ec2.DeleteVpcEndpointServiceConfigurationsInput{
		ServiceIds: serviceIds,
	}

	output, err := awsClient.DeleteVpcEndpointServiceConfigurations(&deleteVpcEndpointServiceConfigurationsInput)
	if err != nil {
		unsuccessfulList := ""
		for i, unsuccessfulEndpoint := range output.Unsuccessful {
			if i > 0 {
				unsuccessfulList += ", "
			}
			unsuccessfulList += *unsuccessfulEndpoint.ResourceId
		}
		delError := fmt.Sprintf("Failed deleting VPC endpoint service configurations: %s", unsuccessfulList)
		awsErrors <- delError
		return err
	}

	awsNotifications <- successMsg
	return nil
}

func (r *AccountClaimReconciler) cleanUpAwsAccountEbsVolumes(reqLogger logr.Logger, awsClient awsclient.Client, awsNotifications chan string, awsErrors chan string) error {

	describeVolumesInput := ec2.DescribeVolumesInput{}
	ebsVolumes, err := awsClient.DescribeVolumes(&describeVolumesInput)
	if err != nil {
		descError := "Failed describing EBS volumes"
		awsErrors <- descError
		return err
	}

	for _, volume := range ebsVolumes.Volumes {

		deleteVolumeInput := ec2.DeleteVolumeInput{
			VolumeId: aws.String(*volume.VolumeId),
		}

		_, err = awsClient.DeleteVolume(&deleteVolumeInput)
		if err != nil {
			delError := fmt.Errorf("failed deleting EBS volume: %s: %w", *volume.VolumeId, err).Error()
			logger.Error(delError)
			awsErrors <- delError
			return err
		}

	}

	successMsg := "EBS Volume cleanup finished successfully"
	awsNotifications <- successMsg
	return nil
}

func (r *AccountClaimReconciler) cleanUpAwsAccountS3(reqLogger logr.Logger, awsClient awsclient.Client, awsNotifications chan string, awsErrors chan string) error {
	listBucketsInput := s3.ListBucketsInput{}
	s3Buckets, err := awsClient.ListBuckets(&listBucketsInput)
	if err != nil {
		listError := fmt.Errorf("failed listing S3 buckets: %w", err).Error()
		awsErrors <- listError
		return err
	}

	for _, bucket := range s3Buckets.Buckets {

		deleteBucketInput := s3.DeleteBucketInput{
			Bucket: aws.String(*bucket.Name),
		}

		// delete any content if any
		err := DeleteBucketContent(awsClient, *bucket.Name)
		if err != nil {
			ContentDelErr := fmt.Errorf("failed to delete bucket content: %s: %w", *bucket.Name, err).Error()
			if aerr, ok := err.(awserr.Error); ok {
				switch aerr.Code() {
				case s3.ErrCodeNoSuchBucket:
					//ignore these errors
				default:
					awsErrors <- ContentDelErr
					return err
				}
			}
		}
		_, err = awsClient.DeleteBucket(&deleteBucketInput)
		if err != nil {
			DelError := fmt.Errorf("failed deleting S3 bucket: %s: %w", *bucket.Name, err).Error()
			if aerr, ok := err.(awserr.Error); ok {
				switch aerr.Code() {
				case s3.ErrCodeNoSuchBucket:
					//ignore these errors
				default:
					awsErrors <- DelError
					return err
				}
			}
		}

	}

	successMsg := "S3 cleanup finished successfully"
	awsNotifications <- successMsg
	return nil
}

func (r *AccountClaimReconciler) cleanUpAwsRoute53(reqLogger logr.Logger, awsClient awsclient.Client, awsNotifications chan string, awsErrors chan string) error {

	var nextZoneMarker *string

	// Paginate through hosted zones
	for {
		// Get list of hosted zones by page
		hostedZonesOutput, err := awsClient.ListHostedZones(&route53.ListHostedZonesInput{Marker: nextZoneMarker})
		if err != nil {
			listError := fmt.Errorf("failed to list Hosted Zones: %w", err).Error()
			awsErrors <- listError
			return err
		}

		for _, zone := range hostedZonesOutput.HostedZones {

			// List and delete all Record Sets for the current zone
			var nextRecordName *string
			// Pagination again!!!!!
			for {
				recordSet, listRecordsError := awsClient.ListResourceRecordSets(&route53.ListResourceRecordSetsInput{HostedZoneId: zone.Id, StartRecordName: nextRecordName})
				if listRecordsError != nil {
					recordSetListError := fmt.Errorf("failed to list Record sets for hosted zone %s: %w", *zone.Name, err).Error()
					awsErrors <- recordSetListError
					return listRecordsError
				}

				changeBatch := &route53.ChangeBatch{}
				for _, record := range recordSet.ResourceRecordSets {
					// Build ChangeBatch
					// https://docs.aws.amazon.com/sdk-for-go/api/service/route53/#ChangeBatch
					//https://docs.aws.amazon.com/sdk-for-go/api/service/route53/#Change
					if *record.Type != "NS" && *record.Type != "SOA" {
						changeBatch.Changes = append(changeBatch.Changes, &route53.Change{
							Action:            aws.String("DELETE"),
							ResourceRecordSet: record,
						})
					}
				}

				if changeBatch.Changes != nil {
					_, changeErr := awsClient.ChangeResourceRecordSets(&route53.ChangeResourceRecordSetsInput{HostedZoneId: zone.Id, ChangeBatch: changeBatch})
					if changeErr != nil {
						recordDeleteError := fmt.Errorf("failed to delete record sets for hosted zone %s: %w", *zone.Name, err).Error()
						awsErrors <- recordDeleteError
						return changeErr
					}
				}
				if *recordSet.IsTruncated {
					nextRecordName = recordSet.NextRecordName
				} else {
					break
				}

			}

			_, deleteError := awsClient.DeleteHostedZone(&route53.DeleteHostedZoneInput{Id: zone.Id})
			if deleteError != nil {
				zoneDelErr := fmt.Errorf("failed to delete hosted zone: %s: %w", *zone.Name, err).Error()
				awsErrors <- zoneDelErr
				return deleteError
			}
		}

		if *hostedZonesOutput.IsTruncated {
			nextZoneMarker = hostedZonesOutput.Marker
		} else {
			break
		}
	}

	successMsg := "Route53 cleanup finished successfully"
	awsNotifications <- successMsg
	return nil
}

// DeleteBucketContent deletes any content in a bucket if it is not empty
func DeleteBucketContent(awsClient awsclient.Client, bucketName string) error {
	// check if objects exits
	objects, err := awsClient.ListObjectsV2(&s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		return err
	}
	if len((*objects).Contents) == 0 {
		return nil
	}

	err = awsClient.BatchDeleteBucketObjects(aws.String(bucketName))
	if err != nil {
		return err
	}
	return nil
}

func (r *AccountClaimReconciler) accountStatusUpdate(reqLogger logr.Logger, account *awsv1alpha1.Account) error {
	err := r.Client.Status().Update(context.TODO(), account)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Status update for %s failed", account.Name))
	}
	return err
}
