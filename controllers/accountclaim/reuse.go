package accountclaim

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/openshift/aws-account-operator/config"
	stsclient "github.com/openshift/aws-account-operator/pkg/awsclient/sts"

	"github.com/rkt/rkt/tests/testutils/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	orgtypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	route53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/localmetrics"
	"github.com/openshift/aws-account-operator/pkg/utils"
	corev1 "k8s.io/api/core/v1"
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
		err := r.Delete(context.TODO(), reusedAccount)
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
		err := r.Delete(context.TODO(), reusedAccount)
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

	// CRITICAL SAFETY CHECK: Prevent cleanup on payer/root accounts
	// This protects against accidentally deleting critical infrastructure in payer accounts
	isPayer, err := config.IsPayerAccount(reusedAccount.Spec.AwsAccountID, r.Client)
	if err != nil {
		reqLogger.Error(err, "Failed to check if account is a payer account",
			"accountID", reusedAccount.Spec.AwsAccountID)
		return err
	}
	if isPayer {
		reqLogger.Error(nil, fmt.Sprintf("Warning: protected payer account %s - skipping all operations on payer/root account", reusedAccount.Spec.AwsAccountID),
			"accountID", reusedAccount.Spec.AwsAccountID,
			"accountCR", reusedAccount.Name,
			"accountClaim", accountClaim.Name,
			"action", "blocked")
		localmetrics.Collector.AddAccountReuseCleanupFailure()
		return fmt.Errorf("cannot clean up payer account %s - protected by blocklist", reusedAccount.Spec.AwsAccountID)
	}

	before := time.Now()
	err = r.cleanUpAwsAccount(reqLogger, awsClient)
	if err != nil {
		localmetrics.Collector.AddAccountReuseCleanupFailure()
		reqLogger.Error(err, "Failed to clean up AWS account")
		return err
	}
	localmetrics.Collector.SetAccountReusedCleanupDuration(time.Since(before).Seconds())

	// Check if close-on-release feature is enabled
	configMap, cmErr := utils.GetOperatorConfigMap(r.Client)
	if cmErr != nil {
		reqLogger.Info("Could not get operator configmap, defaulting to reuse behavior", "error", cmErr)
	}

	// Close account instead of reusing if feature is enabled
	// Skip for FedRAMP (different compliance requirements)
	if utils.IsCloseOnReleaseEnabled(configMap) && !config.IsFedramp() {
		closeErr := r.closeAndDeleteAccount(reqLogger, reusedAccount, awsClient, configMap)
		if closeErr != nil {
			// If close fails, log and fall back to reuse behavior
			reqLogger.Error(closeErr, "Failed to close account, falling back to reuse",
				"accountID", reusedAccount.Spec.AwsAccountID)
			// Continue to reuse logic below
		} else {
			reqLogger.Info("Successfully closed and deleted account",
				"accountID", reusedAccount.Spec.AwsAccountID)
			return nil
		}
	}

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

// closeAndDeleteAccount closes the AWS account via Organizations API and deletes the Account CR
// This is called when close-on-release feature is enabled instead of resetting for reuse
func (r *AccountClaimReconciler) closeAndDeleteAccount(
	reqLogger logr.Logger,
	account *awsv1alpha1.Account,
	awsClient awsclient.Client,
	configMap *corev1.ConfigMap,
) error {
	awsAccountID := account.Spec.AwsAccountID

	// Check if we're currently rate limited
	if retryAfter := utils.GetCloseAccountRetryAfter(account.Annotations); retryAfter > 0 {
		reqLogger.Info("Account closure is rate limited, will retry later",
			"accountID", awsAccountID,
			"retryAfter", retryAfter.Round(time.Minute))
		return fmt.Errorf("rate limited, retry after %v", retryAfter)
	}

	// Check dry-run mode
	if utils.IsCloseAccountDryRun(configMap) {
		reqLogger.Info("DRY-RUN: Would close account",
			"accountID", awsAccountID,
			"accountCR", account.Name)
		// In dry-run mode, return an error so we fall back to reuse
		return fmt.Errorf("dry-run mode enabled, not closing account")
	}

	// Call CloseAccount API
	reqLogger.Info("Closing AWS account", "accountID", awsAccountID)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := awsClient.CloseAccount(ctx, &organizations.CloseAccountInput{
		AccountId: aws.String(awsAccountID),
	})

	if err != nil {
		// Check if account is already closed
		var alreadyClosedErr *orgtypes.AccountAlreadyClosedException
		if errors.As(err, &alreadyClosedErr) {
			reqLogger.Info("Account already closed, proceeding with CR deletion",
				"accountID", awsAccountID)
			// Continue to delete the Account CR
		} else if isCloseAccountRateLimitError(err) {
			// Rate limit hit - set backoff and return error
			reqLogger.Info("CloseAccount rate limit exceeded, setting backoff",
				"accountID", awsAccountID)

			account.Annotations = utils.SetCloseAccountRateLimited(account.Annotations)
			if updateErr := r.Update(context.TODO(), account); updateErr != nil {
				reqLogger.Error(updateErr, "Failed to update account annotations for rate limit")
			}

			retryAfter := utils.GetCloseAccountRetryAfter(account.Annotations)
			return fmt.Errorf("AWS CloseAccount rate limit exceeded, will retry after %v", retryAfter)
		} else {
			// Other error - log and return
			reqLogger.Error(err, "Failed to close AWS account", "accountID", awsAccountID)
			return err
		}
	} else {
		reqLogger.Info("Successfully initiated account closure",
			"accountID", awsAccountID,
			"note", "Account will be in PENDING_CLOSURE for 90 days")

		// Clear any rate limit state on success
		account.Annotations = utils.ClearCloseAccountRateLimited(account.Annotations)
	}

	// Delete the Account CR
	// This triggers the Account controller's finalizer which cleans up IAM resources
	reqLogger.Info("Deleting Account CR", "accountCR", account.Name)
	if err := r.Delete(context.TODO(), account); err != nil {
		reqLogger.Error(err, "Failed to delete Account CR after closing AWS account",
			"accountID", awsAccountID,
			"accountCR", account.Name)
		return err
	}

	localmetrics.Collector.AddAccountClosed()
	return nil
}

// isCloseAccountRateLimitError checks if the error is a rate limit error from CloseAccount
func isCloseAccountRateLimitError(err error) bool {
	var constraintErr *orgtypes.ConstraintViolationException
	if errors.As(err, &constraintErr) {
		switch constraintErr.Reason {
		case orgtypes.ConstraintViolationExceptionReasonCloseAccountQuotaExceeded,
			orgtypes.ConstraintViolationExceptionReasonCloseAccountRequestsLimitExceeded:
			return true
		}
	}
	return false
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
	selfOwnerFilter := ec2types.Filter{
		Name: aws.String("owner-alias"),
		Values: []string{
			"self",
		},
	}
	describeSnapshotsInput := ec2.DescribeSnapshotsInput{
		Filters: []ec2types.Filter{
			selfOwnerFilter,
		},
	}
	ebsSnapshots, err := awsClient.DescribeSnapshots(context.TODO(), &describeSnapshotsInput)
	if err != nil {
		descError := "Failed describing EBS snapshots"
		awsErrors <- descError
		return err
	}

	for _, snapshot := range ebsSnapshots.Snapshots {

		deleteSnapshotInput := ec2.DeleteSnapshotInput{
			SnapshotId: snapshot.SnapshotId,
		}

		_, err = awsClient.DeleteSnapshot(context.TODO(), &deleteSnapshotInput)
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
	vpcEndpointServiceConfigurations, err := awsClient.DescribeVpcEndpointServiceConfigurations(context.TODO(), &describeVpcEndpointServiceConfigurationsInput)
	if vpcEndpointServiceConfigurations == nil || err != nil {
		descError := "Failed describing VPC endpoint service configurations"
		awsErrors <- descError
		return err
	}

	serviceIds := []string{}

	for _, config := range vpcEndpointServiceConfigurations.ServiceConfigurations {
		serviceIds = append(serviceIds, *config.ServiceId)
	}

	successMsg := "VPC endpoint service configuration cleanup finished successfully"
	if len(serviceIds) == 0 {
		awsNotifications <- successMsg + " (nothing to do)"
		return nil
	}

	deleteVpcEndpointServiceConfigurationsInput := ec2.DeleteVpcEndpointServiceConfigurationsInput{
		ServiceIds: serviceIds,
	}

	output, err := awsClient.DeleteVpcEndpointServiceConfigurations(context.TODO(), &deleteVpcEndpointServiceConfigurationsInput)
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
	ebsVolumes, err := awsClient.DescribeVolumes(context.TODO(), &describeVolumesInput)
	if err != nil {
		descError := "Failed describing EBS volumes"
		awsErrors <- descError
		return err
	}

	for _, volume := range ebsVolumes.Volumes {

		deleteVolumeInput := ec2.DeleteVolumeInput{
			VolumeId: volume.VolumeId,
		}

		_, err = awsClient.DeleteVolume(context.TODO(), &deleteVolumeInput)
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
	s3Buckets, err := awsClient.ListBuckets(context.TODO(), &listBucketsInput)
	if err != nil {
		listError := fmt.Errorf("failed listing S3 buckets: %w", err).Error()
		awsErrors <- listError
		return err
	}

	for _, bucket := range s3Buckets.Buckets {

		deleteBucketInput := s3.DeleteBucketInput{
			Bucket: bucket.Name,
		}

		// delete any content if any
		err := DeleteBucketContent(awsClient, *bucket.Name)
		if err != nil {
			ContentDelErr := fmt.Errorf("failed to delete bucket content: %s: %w", *bucket.Name, err).Error()
			// Check for specific S3 exception types
			var noSuchBucketErr *s3types.NoSuchBucket
			if !errors.As(err, &noSuchBucketErr) {
				// If it's not NoSuchBucket, it's an error we care about
				awsErrors <- ContentDelErr
				return err
			}
			// NoSuchBucket - ignore this error
		}
		_, err = awsClient.DeleteBucket(context.TODO(), &deleteBucketInput)
		if err != nil {
			DelError := fmt.Errorf("failed deleting S3 bucket: %s: %w", *bucket.Name, err).Error()
			// Check for specific S3 exception types
			var noSuchBucketErr *s3types.NoSuchBucket
			if !errors.As(err, &noSuchBucketErr) {
				// If it's not NoSuchBucket, it's an error we care about
				awsErrors <- DelError
				return err
			}
			// NoSuchBucket - ignore this error
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
		hostedZonesOutput, err := awsClient.ListHostedZones(context.TODO(), &route53.ListHostedZonesInput{Marker: nextZoneMarker})
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
				recordSet, listRecordsError := awsClient.ListResourceRecordSets(context.TODO(), &route53.ListResourceRecordSetsInput{HostedZoneId: zone.Id, StartRecordName: nextRecordName})
				if listRecordsError != nil {
					recordSetListError := fmt.Errorf("failed to list Record sets for hosted zone %s: %w", *zone.Name, err).Error()
					awsErrors <- recordSetListError
					return listRecordsError
				}

				changeBatch := &route53types.ChangeBatch{}
				for _, record := range recordSet.ResourceRecordSets {
					// Build ChangeBatch
					// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/route53/types#ChangeBatch
					// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/route53/types#Change
					if record.Type != "NS" && record.Type != "SOA" {
						deleteAction := route53types.ChangeActionDelete
						changeBatch.Changes = append(changeBatch.Changes, route53types.Change{
							Action:            deleteAction,
							ResourceRecordSet: &record,
						})
					}
				}

				if changeBatch.Changes != nil {
					_, changeErr := awsClient.ChangeResourceRecordSets(context.TODO(), &route53.ChangeResourceRecordSetsInput{HostedZoneId: zone.Id, ChangeBatch: changeBatch})
					if changeErr != nil {
						recordDeleteError := fmt.Errorf("failed to delete record sets for hosted zone %s: %w", *zone.Name, err).Error()
						awsErrors <- recordDeleteError
						return changeErr
					}
				}
				if recordSet.IsTruncated {
					nextRecordName = recordSet.NextRecordName
				} else {
					break
				}

			}

			_, deleteError := awsClient.DeleteHostedZone(context.TODO(), &route53.DeleteHostedZoneInput{Id: zone.Id})
			if deleteError != nil {
				zoneDelErr := fmt.Errorf("failed to delete hosted zone: %s: %w", *zone.Name, err).Error()
				awsErrors <- zoneDelErr
				return deleteError
			}
		}

		if hostedZonesOutput.IsTruncated {
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
	objects, err := awsClient.ListObjectsV2(context.TODO(), &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		return err
	}
	if len((*objects).Contents) == 0 {
		return nil
	}

	err = awsClient.BatchDeleteBucketObjects(context.TODO(), aws.String(bucketName))
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
