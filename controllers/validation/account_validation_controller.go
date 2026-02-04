package validation

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	organizationstypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"
	"github.com/aws/smithy-go"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/config"
	"github.com/openshift/aws-account-operator/controllers/account"
	"github.com/openshift/aws-account-operator/controllers/accountclaim"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/utils"
)

var log = logf.Log.WithName("controller_accountvalidation")

var accountMoveEnabled = false
var accountTagEnabled = false
var accountDeletionEnabled = false
var complianceTagsEnabled = false

const (
	controllerName = "accountvalidation"
	moveWaitTime   = 5 * time.Minute
	ownerKey       = "owner"
	// PauseReconciliationAnnotation is the annotation key to pause all reconciliation for an account
	PauseReconciliationAnnotation = "aws.managed.openshift.com/pause-reconciliation"
)

type AccountValidationReconciler struct {
	Client           client.Client
	Scheme           *runtime.Scheme
	awsClientBuilder awsclient.IBuilder
	OUNameIDMap      map[string]string
}

type ValidationError int64

const (
	InvalidAccount ValidationError = iota
	AccountMoveFailed
	MissingTag
	IncorrectOwnerTag
	AccountTagFailed
	MissingAWSAccount
	OULookupFailed
	AWSErrorConnecting
	SettingServiceQuotasFailed
	QuotaStatus
	NotAllServicequotasApplied
	AccountNotForCleanup
	OptInRegionStatus
	NotAllOptInRegionsEnabled
	TooManyActiveAccountRegionEnablements
)

type AccountValidationError struct {
	Type ValidationError
	Err  error
}

func NewAccountValidationReconciler(client client.Client, scheme *runtime.Scheme, awsClientBuilder awsclient.IBuilder) *AccountValidationReconciler {
	return &AccountValidationReconciler{
		Client:           client,
		Scheme:           scheme,
		awsClientBuilder: awsClientBuilder,
	}
}

func (r *AccountValidationReconciler) statusUpdate(account *awsv1alpha1.Account) error {
	err := r.Client.Status().Update(context.TODO(), account)
	return err
}

func (ave *AccountValidationError) Error() string {
	return ave.Err.Error()
}

// Retrieve all parents of the given awsId until the predicate returns true.
func ParentsTillPredicate(awsId string, client awsclient.Client, p func(s string) bool, parents *[]string) error {
	listParentsInput := organizations.ListParentsInput{
		ChildId: aws.String(awsId),
	}
	listParentsOutput, err := client.ListParents(context.TODO(), &listParentsInput)
	if err != nil {
		return err
	}
	if len(listParentsOutput.Parents) == 0 {
		log.Info("Exhausted search looking for root OU - root OU and account OU likely in separate subtrees.", "path", parents)
		return nil
	} else if len(listParentsOutput.Parents) > 1 {
		log.Info("More than 1 parent returned for an ID - unexpected.", "awsId", awsId)
		return errors.New("More than 1 parents found for Id " + awsId)
	} else {
		id := *listParentsOutput.Parents[0].Id
		*parents = append(*parents, id)
		if p(id) {
			return nil
		}
		return ParentsTillPredicate(id, client, p, parents)
	}
}

// Verify if the account is already in the root OU
// The predicate indicates if the parent considered the desired root was found.
func IsAccountInCorrectOU(account awsv1alpha1.Account, client awsclient.Client, isPoolOU func(s string) bool) bool {
	if account.Spec.AwsAccountID == "" {
		return false
	}
	parentList := []string{}
	err := ParentsTillPredicate(account.Spec.AwsAccountID, client, isPoolOU, &parentList)
	if err != nil {
		return false
	}
	if len(parentList) == 1 {
		return true
	}
	return false
}

func MoveAccount(awsAccountId string, client awsclient.Client, targetOU string, moveAccount bool) error {
	listParentsInput := organizations.ListParentsInput{
		ChildId: aws.String(awsAccountId),
	}
	listParentsOutput, err := client.ListParents(context.TODO(), &listParentsInput)
	if err != nil {
		log.Error(err, "Can not find parent for AWS account", "aws-account", awsAccountId)
		return err
	}
	oldOu := listParentsOutput.Parents[0].Id
	if moveAccount {
		log.Info("Moving aws account from old ou to new ou", "aws-account", awsAccountId, "old-ou", *oldOu, "new-ou", targetOU)
		moveAccountInput := organizations.MoveAccountInput{
			AccountId:           aws.String(awsAccountId),
			DestinationParentId: aws.String(targetOU),
			SourceParentId:      oldOu,
		}
		_, err = client.MoveAccount(context.TODO(), &moveAccountInput)
		if err != nil {
			log.Error(err, "Could not move aws account to new ou", "aws-account", awsAccountId, "ou", targetOU)
			return err
		}
	} else {
		log.Info("Not moving aws account from old ou to new ou (dry run)", "aws-account", awsAccountId, "old-ou", *oldOu, "new-ou", targetOU)
	}
	return nil
}

func untagAccountOwner(client awsclient.Client, accountId string) error {
	inputTags := &organizations.UntagResourceInput{
		ResourceId: aws.String(accountId),
		TagKeys:    []string{"owner"},
	}

	_, err := client.UntagResource(context.TODO(), inputTags)
	return err
}

// ValidateAccountTags validates the owner tag on an AWS account
func ValidateAccountTags(client awsclient.Client, accountId *string, shardName string, accountTagEnabled bool) error {
	listTagsForResourceInput := &organizations.ListTagsForResourceInput{
		ResourceId: accountId,
	}

	resp, err := client.ListTagsForResource(context.TODO(), listTagsForResourceInput)
	if err != nil {
		return err
	}

	for _, tag := range resp.Tags {
		if ownerKey == *tag.Key {
			if shardName != *tag.Value {
				if accountTagEnabled {
					err := untagAccountOwner(client, *accountId)
					if err != nil {
						log.Error(err, "Unable to remove incorrect owner tag from aws account.", "AWSAccountId", accountId)
						return &AccountValidationError{
							Type: AccountTagFailed,
							Err:  err,
						}
					}

					complianceTags := make(map[string]string)
					err = account.TagAccount(client, *accountId, shardName, complianceTags)
					if err != nil {
						log.Error(err, "Unable to tag aws account.", "AWSAccountID", accountId)
						return &AccountValidationError{
							Type: AccountTagFailed,
							Err:  err,
						}
					}

					return nil
				} else {
					log.Info(fmt.Sprintf("Account is not tagged with the correct owner, has %s; want %s", *tag.Value, shardName))
					return nil
				}
			} else {
				return nil
			}
		}
	}

	if accountTagEnabled {
		complianceTags := make(map[string]string)
		err := account.TagAccount(client, *accountId, shardName, complianceTags)
		if err != nil {
			log.Error(err, "Unable to tag aws account.", "AWSAccountID", accountId)
			return &AccountValidationError{
				Type: AccountTagFailed,
				Err:  err,
			}
		}
		return nil
	} else {
		return &AccountValidationError{
			Type: MissingTag,
			Err:  errors.New("account is not tagged with an owner"),
		}
	}
}

// buildTagMap converts a list of tags into a map for easier lookup
func buildTagMap(tags []organizationstypes.Tag) map[string]string {
	tagMap := make(map[string]string)
	for _, tag := range tags {
		tagMap[*tag.Key] = *tag.Value
	}
	return tagMap
}

// areComplianceTagsInSync checks if compliance tags match expected values
func areComplianceTagsInSync(existingTags map[string]string, appCode, servicePhase, costCenter string) bool {
	if appCode != "" && existingTags["app-code"] != appCode {
		return false
	}
	if servicePhase != "" && existingTags["service-phase"] != servicePhase {
		return false
	}
	if costCenter != "" && existingTags["cost-center"] != costCenter {
		return false
	}
	return true
}

// ValidateComplianceTags validates compliance tags (app-code, service-phase, cost-center) on an AWS account
func ValidateComplianceTags(client awsclient.Client, accountId *string, shardName string, accountTagEnabled bool, appCode, servicePhase, costCenter string, complianceTagsEnabled bool) error {
	// Only validate if feature is enabled
	if !complianceTagsEnabled {
		return nil
	}

	// Fetch existing tags
	listTagsForResourceInput := &organizations.ListTagsForResourceInput{
		ResourceId: accountId,
	}
	resp, err := client.ListTagsForResource(context.TODO(), listTagsForResourceInput)
	if err != nil {
		return err
	}

	// Build tag map for easy lookup
	existingTags := buildTagMap(resp.Tags)

	// Check if compliance tags are correct
	if areComplianceTagsInSync(existingTags, appCode, servicePhase, costCenter) {
		// All compliance tags are correct, nothing to do
		return nil
	}

	// Compliance tags are missing or incorrect
	if !accountTagEnabled {
		// Just log, don't fix
		log.Info("Compliance tags are missing or incorrect but account tagging is disabled", "accountId", *accountId)
		return nil
	}

	// Re-tag to add/update compliance tags
	complianceTags := make(map[string]string)
	if complianceTagsEnabled {
		if appCode != "" {
			complianceTags["app-code"] = appCode
		}
		if servicePhase != "" {
			complianceTags["service-phase"] = servicePhase
		}
		if costCenter != "" {
			complianceTags["cost-center"] = costCenter
		}
	}
	err = account.TagAccount(client, *accountId, shardName, complianceTags)
	if err != nil {
		log.Error(err, "Unable to update compliance tags on aws account.", "AWSAccountID", accountId)
		return &AccountValidationError{
			Type: AccountTagFailed,
			Err:  err,
		}
	}

	return nil
}

func ValidateAccountOrigin(account awsv1alpha1.Account) error {
	// Perform basic short-circuit checks
	if account.IsBYOC() {
		log.Info("Will not validate a CCS account")
		return &AccountValidationError{
			Type: InvalidAccount,
			Err:  errors.New("account is a CCS account"),
		}
	}
	if !account.IsReady() {
		log.Info("Will not validate account not in a ready state")
		return &AccountValidationError{
			Type: InvalidAccount,
			Err:  errors.New("account is not in a ready state"),
		}
	}
	return nil
}

func ValidateAwsAccountId(account awsv1alpha1.Account) error {
	if account.Spec.AwsAccountID == "" {
		return &AccountValidationError{
			Type: MissingAWSAccount,
			Err:  errors.New("account has not associated AWS account"),
		}
	}
	return nil
}

func (r *AccountValidationReconciler) ValidateAccountOU(awsClient awsclient.Client, account awsv1alpha1.Account, poolOU string, baseOU string) error {
	// Default OU should be the aao-managed-accounts OU.
	correctOU := poolOU

	ouNeedsCreating := false

	// If the legal entity is not empty, it should go into the legalEntity's OU
	if account.Spec.LegalEntity.ID != "" {
		claimedOU, err := r.GetOUIDFromName(awsClient, baseOU, account.Spec.LegalEntity.ID)
		if err != nil {
			if errors.Is(err, awsv1alpha1.ErrNonexistentOU) {
				log.Info("OU doesn't exist. Will need to create it.", "OU Name", account.Spec.LegalEntity.ID)
				ouNeedsCreating = true
			} else {
				log.Info("Unexpected error attempting to get OU ID for Legal Entity", "legal_entity", account.Spec.LegalEntity.ID)
				return &AccountValidationError{
					Type: OULookupFailed,
					Err:  errors.New("unexpected error attempting to get OU ID for legal entity"),
				}
			}
		}

		correctOU = claimedOU
	}

	if ouNeedsCreating {
		if accountMoveEnabled {
			createdOU, err := accountclaim.CreateOrFindOU(log, awsClient, account.Spec.LegalEntity.ID, baseOU)
			if err != nil {
				return err
			}
			correctOU = createdOU
		} else {
			log.Info("Would attempt to create the OU here, but AccountMoving is disabled.")
		}
	}

	if correctOU == "" {
		log.Info("Error attempting to get correct OU. Got empty string.")
		return &AccountValidationError{
			Type: OULookupFailed,
			Err:  fmt.Errorf("empty String when attempting to get correct OU"),
		}
	}

	inCorrectOU := IsAccountInCorrectOU(account, awsClient, func(s string) bool {
		return s == correctOU
	})
	if inCorrectOU {
		log.Info("Account is already in the correct OU.")
	} else {
		log.Info("Account is not in the correct OU - it will be moved.")
		err := MoveAccount(account.Spec.AwsAccountID, awsClient, correctOU, accountMoveEnabled)
		if err != nil {
			log.Error(err, "Could not move account")
			return &AccountValidationError{
				Type: AccountMoveFailed,
				Err:  err,
			}
		}
	}
	return nil
}

func (r *AccountValidationReconciler) GetOUIDFromName(client awsclient.Client, parentid string, ouName string) (string, error) {
	// Check in-memory storage first
	if ouID, ok := r.OUNameIDMap[ouName]; ok {
		return ouID, nil
	}

	// Loop through all OUs in the parent until we find the ID of the OU with the given name
	ouID := ""
	listOrgUnitsForParentID := organizations.ListOrganizationalUnitsForParentInput{
		ParentId: &parentid,
	}
	for ouID == "" {
		// Get a list with a fraction of the OUs in this parent starting from NextToken
		listOut, err := client.ListOrganizationalUnitsForParent(context.TODO(), &listOrgUnitsForParentID)
		if err != nil {
			var aerr smithy.APIError
			if errors.As(err, &aerr) {
				unexpectedErrorMsg := fmt.Sprintf("FindOUFromParentID: Unexpected AWS Error when attempting to find OU ID from Parent: %s", aerr.ErrorCode())
				log.Info(unexpectedErrorMsg)
			}
			return "", err
		}
		for _, element := range listOut.OrganizationalUnits {
			if *element.Name == ouName {
				// We've found the OU, so let's map this to the in-memory store
				r.OUNameIDMap[ouName] = *element.Id
				// and return it
				return *element.Id, nil
			}
		}
		// If the OU is not found we should update the input for the next list call
		if listOut.NextToken != nil {
			listOrgUnitsForParentID.NextToken = listOut.NextToken
			continue
		}
		return "", awsv1alpha1.ErrNonexistentOU
	}
	return ouID, nil
}

func ValidateRemoval(account awsv1alpha1.Account) error {
	if account.Status.State != string(awsv1alpha1.AccountFailed) {
		return &AccountValidationError{
			Type: AccountNotForCleanup,
			Err:  errors.New("non-failed accounts are never to be cleaned up"),
		}
	}
	if err := ValidateAwsAccountId(account); err == nil {
		return &AccountValidationError{
			Type: AccountNotForCleanup,
			Err:  errors.New("accounts with an associated AWS account are never cleaned up"),
		}
	}
	return nil
}

func (r *AccountValidationReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	log.WithValues("Controller", controllerName, "Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger := log.WithValues("Controller", controllerName, "Request.Namespace", request.Namespace, "Request.Name", request.Name)

	// Setup: retrieve account and awsClient
	var account awsv1alpha1.Account
	err := r.Client.Get(context.TODO(), request.NamespacedName, &account)
	if err != nil {
		log.Info("Account does not exist", "account-request", request.NamespacedName, "error", err)
		return utils.DoNotRequeue()
	}
	if account.DeletionTimestamp != nil {
		log.Info("Account is being deleted - not running any validations", "account", account.Name)
		return utils.DoNotRequeue()
	}

	// Check if reconciliation is paused for this account
	if account.Annotations[PauseReconciliationAnnotation] == "true" {
		log.Info("Reconciliation paused for account - skipping all validations", "account", account.Name)
		return utils.DoNotRequeue()
	}

	cm, err := utils.GetOperatorConfigMap(r.Client)
	if err != nil {
		log.Error(err, "Could not retrieve the operator configmap")
		return utils.RequeueAfter(5 * time.Minute)
	}

	isOptInRegionFeatureEnabled, err := utils.GetFeatureFlagValue(cm, "feature.opt_in_regions")
	if err != nil {
		reqLogger.Info("Could not retrieve feature flag 'feature.opt_in_regions' - region Opt-In is disabled")
		isOptInRegionFeatureEnabled = false
	}
	reqLogger.Info("Is feature.opt_in_regions enabled?", "enabled", isOptInRegionFeatureEnabled)

	enabled, err := strconv.ParseBool(cm.Data["feature.validation_move_account"])
	if err != nil {
		log.Info("Could not retrieve feature flag 'feature.validation_move_account' - account moving is disabled")
	} else {
		accountMoveEnabled = enabled
	}
	log.Info("Is moving accounts enabled?", "enabled", accountMoveEnabled)

	enabled, err = strconv.ParseBool(cm.Data["feature.validation_tag_account"])
	if err != nil {
		log.Info("Could not retrieve feature flag 'feature.validation_tag_account' - account tagging is disabled")
	} else {
		accountTagEnabled = enabled
	}
	log.Info("Is tagging accounts enabled?", "enabled", accountTagEnabled)

	enabled, err = strconv.ParseBool(cm.Data["feature.compliance_tags"])
	if err != nil {
		log.Info("Could not retrieve feature flag 'feature.compliance_tags' - compliance tagging is disabled")
	} else {
		complianceTagsEnabled = enabled
	}
	log.Info("Is compliance tagging enabled?", "enabled", complianceTagsEnabled)

	enabled, err = strconv.ParseBool(cm.Data["feature.validation_delete_account"])
	if err != nil {
		log.Info("Could not retrieve feature flag 'feature.validation_delete_account' - account deletion is disabled")
	} else {
		accountDeletionEnabled = enabled
	}
	log.Info("Is deleting accounts enabled?", "enabled", accountDeletionEnabled)

	awsClientInput := awsclient.NewAwsClientInput{
		AwsRegion:  config.GetDefaultRegion(),
		SecretName: utils.AwsSecretName,
		NameSpace:  awsv1alpha1.AccountCrNamespace,
	}
	awsClient, err := r.awsClientBuilder.GetClient(controllerName, r.Client, awsClientInput)
	if err != nil {
		log.Error(err, "Could not retrieve AWS client.")
	}

	// Here the actual checks start:

	if err := ValidateRemoval(account); err == nil {
		if accountDeletionEnabled {
			log.Info("Cleaning up account that is failed & has no AWS account", "account", account)
			err := r.Client.Delete(ctx, &account)
			if err != nil {
				log.Error(err, "failed to delete account", "account", account.Name)
				return utils.RequeueWithError(err)
			}
		} else {
			log.Info("Not cleaning up account that is failed & has no AWS account (dry run)", "account", account.Name)
		}
	}

	err = ValidateAccountOrigin(account)
	if err != nil {
		// Decide who we will requeue now
		validationError, ok := err.(*AccountValidationError)
		if ok && validationError.Type == InvalidAccount {
			return utils.DoNotRequeue()
		}
		return utils.RequeueWithError(err)
	}

	err = ValidateAwsAccountId(account)
	if err != nil {
		validationError, ok := err.(*AccountValidationError)
		if ok && validationError.Type == MissingAWSAccount {
			return utils.DoNotRequeue()
		}
		return utils.RequeueWithError(err)
	}

	err = r.ValidateAccountOU(awsClient, account, cm.Data["root"], cm.Data["base"])
	if err != nil {
		// Decide who we will requeue now
		validationError, ok := err.(*AccountValidationError)
		if ok && validationError.Type == AccountMoveFailed {
			return utils.RequeueAfter(moveWaitTime)
		}
		return utils.RequeueWithError(err)
	}

	shardName, ok := cm.Data["shard-name"]
	if !ok {
		log.Info("Could not retrieve configuration map value 'shard-name' - account tagging is disabled")
	} else {
		if shardName == "" {
			log.Info("Cluster configuration is missing a shardName value.  Skipping validation for this tag.")
		} else {
			// Validate owner tag
			err = ValidateAccountTags(awsClient, aws.String(account.Spec.AwsAccountID), shardName, accountTagEnabled)
			if err != nil {
				validationError, ok := err.(*AccountValidationError)
				if ok && (validationError.Type == MissingTag || validationError.Type == IncorrectOwnerTag) {
					log.Error(validationError, validationError.Err.Error())
				}
				return utils.RequeueWithError(err)
			}
		}

		// check if account belongs to accountpool
		if !account.IsBYOC() {
			// Validate compliance tags
			var appCode, servicePhase, costCenter string

			// Read ConfigMap values if complianceTagsEnabled
			if complianceTagsEnabled {
				var ok bool
				appCode, ok = cm.Data["app-code"]
				if !ok {
					log.Info("Could not retrieve configuration map value 'app-code' - compliance tag will be skipped")
				}
				servicePhase, ok = cm.Data["service-phase"]
				if !ok {
					log.Info("Could not retrieve configuration map value 'service-phase' - compliance tag will be skipped")
				}
				costCenter, ok = cm.Data["cost-center"]
				if !ok {
					log.Info("Could not retrieve configuration map value 'cost-center' - compliance tag will be skipped")
				}
			}

			err = ValidateComplianceTags(awsClient, aws.String(account.Spec.AwsAccountID), shardName, accountTagEnabled, appCode, servicePhase, costCenter, complianceTagsEnabled)
			if err != nil {
				log.Error(err, "Failed to validate compliance tags")
				return utils.RequeueWithError(err)
			}
		}

		optInRegions, ok := cm.Data["opt-in-regions"]
		// ValidateOptInRegions
		if ok && isOptInRegionFeatureEnabled {
			err = r.ValidateOptInRegions(reqLogger, &account, r.awsClientBuilder, optInRegions)
			if err != nil {
				validationError, ok := err.(*AccountValidationError)
				if ok && validationError.Type == NotAllOptInRegionsEnabled {
					return reconcile.Result{RequeueAfter: 10 * time.Minute}, nil
				}
				return utils.RequeueWithError(err)
			}

		}

		err = r.ValidateRegionalServiceQuotas(reqLogger, &account, r.awsClientBuilder)
		if err != nil {
			validationError, ok := err.(*AccountValidationError)
			if ok && validationError.Type == NotAllServicequotasApplied {
				return reconcile.Result{RequeueAfter: 10 * time.Minute}, nil
			}
			return utils.RequeueWithError(err)
		}

	}
	return utils.DoNotRequeue()
}
func (r *AccountValidationReconciler) ValidateOptInRegions(reqLogger logr.Logger, currentAcctInstance *awsv1alpha1.Account, awsClientBuilder awsclient.IBuilder, optInRegions string) error {
	var regionList []string
	regions := strings.Split(optInRegions, ",")
	for _, region := range regions {
		regionList = append(regionList, strings.TrimSpace(region))
	}

	numberOfAccountsOptingIn, err := account.CalculateOptingInRegionAccounts(reqLogger, r.Client)
	if err != nil {
		return &AccountValidationError{
			Type: NotAllOptInRegionsEnabled,
			Err:  err,
		}
	}

	if currentAcctInstance.Status.OptInRegions == nil || !currentAcctInstance.AllRegionsExistInOptInRegions(regionList) {
		if numberOfAccountsOptingIn >= account.MaxAccountRegionEnablement {
			return &AccountValidationError{
				Type: TooManyActiveAccountRegionEnablements,
				Err:  errors.New("the request quota for the number of concurrent account region-OptIn requests has been reached"),
			}
		}
		//updates account status to indicate supported opt-in region are pending enablement
		err = account.SetOptRegionStatus(reqLogger, regionList, currentAcctInstance)
		if err != nil {
			return &AccountValidationError{
				Type: OptInRegionStatus,
				Err:  errors.New("failed to set account opt-in region status"),
			}
		}

		if currentAcctInstance.Spec.RegionalServiceQuotas != nil {
			currentAcctInstance.Status.RegionalServiceQuotas = make(awsv1alpha1.RegionalServiceQuotas)

		}
		err = r.statusUpdate(currentAcctInstance)
		if err != nil {
			return &AccountValidationError{
				Type: OptInRegionStatus,
				Err:  errors.New("failed to set account opt-in region status"),
			}
		}
	}
	awsRegion := config.GetDefaultRegion()
	awsSetupClient, err := awsClientBuilder.GetClient(controllerName, r.Client, awsclient.NewAwsClientInput{
		SecretName: utils.AwsSecretName,
		NameSpace:  awsv1alpha1.AccountCrNamespace,
		AwsRegion:  awsRegion,
	})
	if err != nil {
		connErr := fmt.Sprintf("unable to connect to default region %s", awsRegion)
		reqLogger.Error(err, connErr)
		return &AccountValidationError{
			Type: AWSErrorConnecting,
			Err:  errors.New("unexpected error attempting to connect to AWS in default region"),
		}
	}

	if currentAcctInstance.HasOpenOptInRegionRequests() && utils.DetectDevMode == utils.DevModeProduction {
		_, err := account.GetOptInRegionStatus(reqLogger, r.awsClientBuilder, awsSetupClient, currentAcctInstance, r.Client)
		if err != nil {
			return &AccountValidationError{
				Type: NotAllOptInRegionsEnabled,
				Err:  err,
			}
		}
		return &AccountValidationError{
			Type: NotAllOptInRegionsEnabled,
			Err:  errors.New("not all Opt-In regions have been enabled yet"),
		}
	}
	return nil

}

func (r *AccountValidationReconciler) ValidateRegionalServiceQuotas(reqLogger logr.Logger, awsAccount *awsv1alpha1.Account, awsClientBuilder awsclient.IBuilder) error {
	awsRegion := config.GetDefaultRegion()
	awsSetupClient, err := awsClientBuilder.GetClient(controllerName, r.Client, awsclient.NewAwsClientInput{
		SecretName: utils.AwsSecretName,
		NameSpace:  awsv1alpha1.AccountCrNamespace,
		AwsRegion:  awsRegion,
	})
	if err != nil {
		connErr := fmt.Sprintf("unable to connect to default region %s", awsRegion)
		reqLogger.Error(err, connErr)
		return &AccountValidationError{
			Type: AWSErrorConnecting,
			Err:  errors.New("unexpected error attempting to connect to AWS in default region"),
		}
	}

	if awsAccount.Spec.RegionalServiceQuotas == nil {
		return nil
	}

	if awsAccount.Status.RegionalServiceQuotas == nil {
		err = account.SetCurrentAccountServiceQuotas(reqLogger, awsClientBuilder, awsSetupClient, awsAccount, r.Client)
		if err != nil {
			reqLogger.Error(err, "failed to set account service quotas")
			return &AccountValidationError{
				Type: SettingServiceQuotasFailed,
				Err:  errors.New("failed to set account service quotas"),
			}
		}
		err = r.statusUpdate(awsAccount)
		if err != nil {
			return &AccountValidationError{
				Type: QuotaStatus,
				Err:  errors.New("failed to update account status"),
			}
		}

		return &AccountValidationError{
			Type: QuotaStatus,
			Err:  errors.New("service quota status updated, increase request needs to be sent to aws"),
		}
	} else {
		if awsAccount.HasOpenQuotaIncreaseRequests() && utils.DetectDevMode == utils.DevModeProduction {
			_, err = account.GetServiceQuotaRequest(reqLogger, awsClientBuilder, awsSetupClient, awsAccount, r.Client)
			if err != nil {
				return &AccountValidationError{
					Type: NotAllServicequotasApplied,
					Err:  err,
				}
			}

			return &AccountValidationError{
				Type: NotAllServicequotasApplied,
				Err:  errors.New("service quotas not yet applied"),
			}
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AccountValidationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.awsClientBuilder = &awsclient.Builder{}
	r.OUNameIDMap = map[string]string{}
	maxReconciles, err := utils.GetControllerMaxReconciles(controllerName)
	if err != nil {
		log.Error(err, "missing max reconciles for controller", "controller", controllerName)
	}

	rwm := utils.NewReconcilerWithMetrics(r, controllerName)
	return ctrl.NewControllerManagedBy(mgr).
		For(&awsv1alpha1.Account{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxReconciles,
		}).Complete(rwm)
}
