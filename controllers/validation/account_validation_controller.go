package validation

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-logr/logr"
	accountcontroller "github.com/openshift/aws-account-operator/controllers/account"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"strconv"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/controller"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/organizations"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

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

const (
	controllerName = "accountvalidation"
	moveWaitTime   = 5 * time.Minute
	ownerKey       = "owner"
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
	listParentsOutput, err := client.ListParents(&listParentsInput)
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
	listParentsOutput, err := client.ListParents(&listParentsInput)
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
		_, err = client.MoveAccount(&moveAccountInput)
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
		TagKeys:    []*string{aws.String("owner")},
	}

	_, err := client.UntagResource(inputTags)
	return err
}

func ValidateAccountTags(client awsclient.Client, accountId *string, shardName string, accountTagEnabled bool) error {
	listTagsForResourceInput := &organizations.ListTagsForResourceInput{
		ResourceId: accountId,
	}

	resp, err := client.ListTagsForResource(listTagsForResourceInput)
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

					err = account.TagAccount(client, *accountId, shardName)
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
		err := account.TagAccount(client, *accountId, shardName)
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
			Err:  errors.New("Account is not tagged with an owner"),
		}
	}
}

func ValidateAccountOrigin(account awsv1alpha1.Account) error {
	// Perform basic short-circuit checks
	if account.IsBYOC() {
		log.Info("Will not validate a CCS account")
		return &AccountValidationError{
			Type: InvalidAccount,
			Err:  errors.New("Account is a CCS account"),
		}
	}
	if !account.IsReady() {
		log.Info("Will not validate account not in a ready state")
		return &AccountValidationError{
			Type: InvalidAccount,
			Err:  errors.New("Account is not in a ready state"),
		}
	}
	return nil
}

func ValidateAwsAccountId(account awsv1alpha1.Account) error {
	if account.Spec.AwsAccountID == "" {
		return &AccountValidationError{
			Type: MissingAWSAccount,
			Err:  errors.New("Account has not associated AWS account"),
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
			Err:  fmt.Errorf("Empty String when attempting to get correct OU"),
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
		listOut, err := client.ListOrganizationalUnitsForParent(&listOrgUnitsForParentID)
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				unexpectedErrorMsg := fmt.Sprintf("FindOUFromParentID: Unexpected AWS Error when attempting to find OU ID from Parent: %s", aerr.Code())
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

	cm, err := utils.GetOperatorConfigMap(r.Client)
	if err != nil {
		log.Error(err, "Could not retrieve the operator configmap")
		return utils.RequeueAfter(5 * time.Minute)
	}

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

	awsClientInput := awsclient.NewAwsClientInput{
		AwsRegion:  config.GetDefaultRegion(),
		SecretName: utils.AwsSecretName,
		NameSpace:  awsv1alpha1.AccountCrNamespace,
	}
	awsClient, err := r.awsClientBuilder.GetClient(controllerName, r.Client, awsClientInput)
	if err != nil {
		log.Error(err, "Could not retrieve AWS client.")
	}

	// Perform any checks we want
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
			err = ValidateAccountTags(awsClient, aws.String(account.Spec.AwsAccountID), shardName, accountTagEnabled)
			if err != nil {
				validationError, ok := err.(*AccountValidationError)
				if ok && (validationError.Type == MissingTag || validationError.Type == IncorrectOwnerTag) {
					log.Error(validationError, validationError.Err.Error())
				}
				return utils.RequeueWithError(err)
			}
		}
	}

	// check if account belongs to accountpool
	if !account.IsBYOC() {
		result, err := r.ValidateRegionalServiceQuotas(reqLogger, &account, r.awsClientBuilder)
		if err != nil {
			return utils.DoNotRequeue()
		}
		return result, nil

	}
	return utils.DoNotRequeue()
}

func (r *AccountValidationReconciler) ValidateRegionalServiceQuotas(reqLogger logr.Logger, account *awsv1alpha1.Account, awsClientBuilder awsclient.IBuilder) (reconcile.Result, error) {

	awsRegion := config.GetDefaultRegion()
	awsSetupClient, err := awsClientBuilder.GetClient(controllerName, r.Client, awsclient.NewAwsClientInput{
		SecretName: utils.AwsSecretName,
		NameSpace:  awsv1alpha1.AccountCrNamespace,
		AwsRegion:  awsRegion,
	})
	if err != nil {
		connErr := fmt.Sprintf("unable to connect to default region %s", awsRegion)
		reqLogger.Error(err, connErr)
		return utils.RequeueWithError(err)
	}

	if account.Spec.RegionalServiceQuotas == nil {
		return utils.DoNotRequeue()
	}
	if account.Status.RegionalServiceQuotas == nil {
		err = accountcontroller.SetCurrentAccountServiceQuotas(reqLogger, awsClientBuilder, awsSetupClient, account, r.Client)
		if err != nil {
			reqLogger.Error(err, "failed to set account service quotas")
			return reconcile.Result{}, err
		}
		err := r.statusUpdate(account)
		if err != nil {
			return reconcile.Result{}, err
		}

		return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
	} else {
		if account.HasOpenQuotaIncreaseRequests() && utils.DetectDevMode == utils.DevModeProduction {
			return accountcontroller.GetServiceQuotaRequest(reqLogger, awsClientBuilder, awsSetupClient, account, r.Client)
		}
	}
	for _, quotas := range account.Status.RegionalServiceQuotas {
		for _, quota := range quotas {
			if quota.Status == "TODO" || quota.Status == "IN_PROGRESS" {
				return reconcile.Result{RequeueAfter: 10 * time.Minute}, nil
			}
		}
	}
	return reconcile.Result{}, nil
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
