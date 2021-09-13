package account

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/organizations"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/go-logr/logr"
	"github.com/openshift/aws-account-operator/pkg/controller/utils"
	"github.com/rkt/rkt/tests/testutils/logger"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	controllerutils "github.com/openshift/aws-account-operator/pkg/controller/utils"
	totalaccountwatcher "github.com/openshift/aws-account-operator/pkg/totalaccountwatcher"
)

var log = logf.Log.WithName("controller_account")

const (
	// Service Quota-related constants
	// vCPUQuotaCode
	vCPUQuotaCode = "L-1216C47A"
	// vCPUServiceCode
	vCPUServiceCode = "ec2"

	// createPendTime is the maximum time we allow an Account to sit in Creating state before we
	// time out and set it to Failed.
	createPendTime = utils.WaitTime * time.Minute
	// regionInitTime is the maximum time we allow an account CR to be in the InitializingRegions
	// state. This is based on async region init taking a theoretical maximum of WaitTime * 2
	// minutes plus a handful of AWS API calls (see asyncRegionInit).
	regionInitTime = (time.Minute * utils.WaitTime * time.Duration(2)) + time.Minute

	// AccountPending indicates an account is pending
	AccountPending = "Pending"
	// AccountCreating indicates an account is being created
	AccountCreating = "Creating"
	// AccountFailed indicates account creation has failed
	AccountFailed = "Failed"
	// AccountInitializingRegions indicates we've kicked off the process of creating and terminating
	// instances in all supported regions
	AccountInitializingRegions = "InitializingRegions"
	// AccountReady indicates account creation is ready
	AccountReady = "Ready"
	// AccountPendingVerification indicates verification (of AWS limits and Enterprise Support) is pending
	AccountPendingVerification = "PendingVerification"

	byocRole = "BYOCAdminAccess"

	adminAccessArn = "arn:aws:iam::aws:policy/AdministratorAccess"
	iamUserNameUHC = "osdManagedAdmin"

	controllerName = "account"
)

// Add creates a new Account Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	reconciler := &ReconcileAccount{
		Client:           utils.NewClientWithMetricsOrDie(log, mgr, controllerName),
		scheme:           mgr.GetScheme(),
		awsClientBuilder: &awsclient.Builder{},
	}

	configMap, err := controllerutils.GetOperatorConfigMap(reconciler.Client)
	if err != nil {
		log.Error(err, "failed retrieving configmap")
	}

	hiveName, ok := configMap.Data["shard-name"]
	if !ok {
		log.Error(err, "shard-name key not available in configmap")
	}
	reconciler.shardName = hiveName
	return utils.NewReconcilerWithMetrics(reconciler, controllerName)
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("account-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Account
	err = c.Watch(&source.Kind{Type: &awsv1alpha1.Account{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileAccount{}

// ReconcileAccount reconciles a Account object
type ReconcileAccount struct {
	Client           client.Client
	scheme           *runtime.Scheme
	awsClientBuilder awsclient.IBuilder
	shardName        string
}

// Reconcile reads that state of the cluster for a Account object and makes changes based on the state read
// and what is in the Account.Spec
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileAccount) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Controller", controllerName, "Request.Namespace", request.Namespace, "Request.Name", request.Name)

	// Fetch the Account instance
	currentAcctInstance := &awsv1alpha1.Account{}
	err := r.Client.Get(context.TODO(), request.NamespacedName, currentAcctInstance)

	if err != nil {
		if k8serr.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// We expect this secret to exist in the same namespace Account CR's are created
	// We build the input here but the client later because we handle the error differently
	setupClientInput := awsclient.NewAwsClientInput{
		SecretName: utils.AwsSecretName,
		NameSpace:  awsv1alpha1.AccountCrNamespace,
		AwsRegion:  "us-east-1",
	}

	if currentAcctInstance.IsPendingDeletion() {
		if currentAcctInstance.Spec.ManualSTSMode {
			// if the account is STS, we don't need to do any additional cleanup aside from
			// removing the finalizer and exiting.
			err := r.removeFinalizer(currentAcctInstance, awsv1alpha1.AccountFinalizer)
			if err != nil {
				reqLogger.Error(err, "Failed removing account finalizer")
			}
			return reconcile.Result{}, err
		}

		awsSetupClient, err := r.awsClientBuilder.GetClient(controllerName, r.Client, setupClientInput)
		if err != nil {
			reqLogger.Error(err, "failed building operator AWS client")
			if aerr, ok := err.(awserr.Error); ok {
				switch aerr.Code() {
				// If it's AccessDenied we want to just delete the finalizer and continue as we assume
				// the credentials have been deleted by the customer
				case "AccessDenied":
					err = r.removeFinalizer(currentAcctInstance, awsv1alpha1.AccountFinalizer)
					if err != nil {
						reqLogger.Error(err, "failed removing account finalizer")
						return reconcile.Result{}, err
					}
					return reconcile.Result{}, nil
				}
			}
			return reconcile.Result{}, err
		}

		var awsClient awsclient.Client
		if currentAcctInstance.IsBYOC() {
			currentAccInstanceID := currentAcctInstance.Labels[awsv1alpha1.IAMUserIDLabel]
			roleToAssume := fmt.Sprintf("%s-%s", byocRole, currentAccInstanceID)
			awsClient, _, err = r.assumeRole(reqLogger, currentAcctInstance, awsSetupClient, roleToAssume, "")
		} else {
			awsClient, _, err = r.assumeRole(reqLogger, currentAcctInstance, awsSetupClient, awsv1alpha1.AccountOperatorIAMRole, "")
		}
		if err != nil {
			reqLogger.Error(err, "failed building operator AWS client")
			return reconcile.Result{}, err
		}
		r.finalizeAccount(reqLogger, awsClient, currentAcctInstance)
		//return reconcile.Result{}, nil

		// Remove finalizer if account CR is BYOC as the accountclaim controller will delete the account CR
		// when the accountClaim CR is deleted as its set as the owner reference
		if currentAcctInstance.IsBYOCPendingDeletionWithFinalizer() {
			reqLogger.Info("removing account finalizer")
			err = r.removeFinalizer(currentAcctInstance, awsv1alpha1.AccountFinalizer)
			if err != nil {
				reqLogger.Error(err, "failed removing account finalizer")
				return reconcile.Result{}, err
			}
		}
		return reconcile.Result{}, nil
	}

	// Log accounts that have failed and don't attempt to reconcile them
	if currentAcctInstance.IsFailed() {
		reqLogger.Info(fmt.Sprintf("Account %s is failed. Ignoring.", currentAcctInstance.Name))
		return reconcile.Result{}, nil
	}

	awsSetupClient, err := r.awsClientBuilder.GetClient(controllerName, r.Client, setupClientInput)
	if err != nil {
		reqLogger.Error(err, "failed building operator AWS client")
		return reconcile.Result{}, err
	}

	// Detect accounts for which we kicked off asynchronous region initialization
	if currentAcctInstance.IsInitializingRegions() {
		return r.handleAccountInitializingRegions(reqLogger, currentAcctInstance)
	}

	// If the account is BYOC, needs some different set up
	if newBYOCAccount(currentAcctInstance) {
		var result reconcile.Result
		var initErr error

		result, initErr = r.initializeNewCCSAccount(reqLogger, currentAcctInstance)
		if initErr != nil {
			// TODO: If we have recoverable results from above, how do we allow them to requeue if state is failed
			_, stateErr := r.setAccountFailed(
				reqLogger,
				currentAcctInstance,
				awsv1alpha1.AccountCreationFailed,
				initErr.Error(),
				"Failed to initialize new CCS account",
				AccountFailed,
			)
			if stateErr != nil {
				reqLogger.Error(stateErr, "failed setting account state", "desiredState", AccountFailed)
			}
			reqLogger.Error(initErr, "failed initializing new CCS account")
			return result, initErr
		}
		utils.SetAccountStatus(currentAcctInstance, AccountCreating, awsv1alpha1.AccountCreating, AccountCreating)
		updateErr := r.statusUpdate(currentAcctInstance)
		if updateErr != nil {
			// TODO: Validate this is retryable
			// TODO: Should be re-entrant because account will not have state
			reqLogger.Info("failed updating account state, retrying", "desired state", AccountCreating)
			return reconcile.Result{}, updateErr
		}

	} else {
		// Normal account creation

		// Test PendingVerification state creating support case and checking for case status
		if currentAcctInstance.IsPendingVerification() {
			return r.handleNonCCSPendingVerification(reqLogger, currentAcctInstance, awsSetupClient)
		}

		// Update account Status.Claimed to true if the account is ready and the claim link is not empty
		if currentAcctInstance.IsReadyUnclaimedAndHasClaimLink() {
			currentAcctInstance.Status.Claimed = true
			return reconcile.Result{}, r.statusUpdate(currentAcctInstance)
		}

		// see if in creating for longer than default wait time
		if currentAcctInstance.IsCreating() && utils.CreationConditionOlderThan(*currentAcctInstance, createPendTime) {
			errMsg := fmt.Sprintf("Creation pending for longer than %d minutes", utils.WaitTime)
			_, stateErr := r.setAccountFailed(
				reqLogger,
				currentAcctInstance,
				v1alpha1.AccountCreationFailed,
				"CreationTimeout",
				errMsg,
				AccountFailed,
			)
			if stateErr != nil {
				reqLogger.Error(stateErr, "failed setting account state", "desiredState", AccountFailed)
				return reconcile.Result{}, stateErr
			}
			return reconcile.Result{}, errors.New(errMsg)
		}

		if currentAcctInstance.IsUnclaimedAndHasNoState() {
			if !currentAcctInstance.HasAwsAccountID() {
				// before doing anything make sure we are not over the limit if we are just error
				if !totalaccountwatcher.TotalAccountWatcher.AccountsCanBeCreated() {
					reqLogger.Error(awsv1alpha1.ErrAwsAccountLimitExceeded, "AWS Account limit reached")
					// We don't expect the limit to change very frequently, so wait a while before requeueing to avoid hot lopping.
					return reconcile.Result{Requeue: true, RequeueAfter: time.Duration(5) * time.Minute}, nil
				}

				if err := r.nonCCSAssignAccountID(reqLogger, currentAcctInstance, awsSetupClient); err != nil {
					return reconcile.Result{}, err
				}
			} else {
				// set state creating if the account was already created
				utils.SetAccountStatus(currentAcctInstance, "AWS account already created", awsv1alpha1.AccountCreating, AccountCreating)
				err = r.statusUpdate(currentAcctInstance)

				if err != nil {
					return reconcile.Result{}, err
				}
			}
		}
	}

	configMap, err := controllerutils.GetOperatorConfigMap(r.Client)
	stringRegions, ok := configMap.Data["regions"]
	if !ok {
		err = awsv1alpha1.ErrInvalidConfigMap
		return reconcile.Result{}, err
	}
	if err != nil {
		reqLogger.Error(err, "failed getting regions from configmap data")
		return reconcile.Result{}, err
	}
	regionAMIs := processConfigMapRegions(stringRegions)

	// Account init for both BYOC and Non-BYOC
	if currentAcctInstance.ReadyForInitialization() {
		reqLogger.Info("initializing account", "awsAccountID", currentAcctInstance.Spec.AwsAccountID)

		// STS mode doesn't need IAM user init, so just get the creds necessary, init regions, and exit
		if currentAcctInstance.Spec.ManualSTSMode {
			accountClaim, acctClaimErr := r.getAccountClaim(currentAcctInstance)
			if acctClaimErr != nil {
				reqLogger.Error(acctClaimErr, "unable to get accountclaim for sts account")
				utils.SetAccountClaimStatus(
					accountClaim,
					"Failed to get AccountClaim for CSS account",
					"FailedRetrievingAccountClaim",
					awsv1alpha1.ClientError,
					awsv1alpha1.ClaimStatusError,
				)
				err := r.Client.Status().Update(context.TODO(), accountClaim)
				if err != nil {
					reqLogger.Error(err, "failed to update accountclaim status")
				}
				return reconcile.Result{}, acctClaimErr
			}

			_, creds, err := r.getSTSClient(reqLogger, accountClaim, awsSetupClient)
			if err != nil {
				reqLogger.Error(err, "error getting sts client to initialize regions")
				return reconcile.Result{}, err
			}

			if err = r.initializeRegions(reqLogger, currentAcctInstance, creds, regionAMIs); err != nil {
				// initializeRegions logs
				return reconcile.Result{}, err
			}
			return reconcile.Result{}, nil
		}

		// Set IAMUserIDLabel if not there, and requeue
		if !utils.AccountCRHasIAMUserIDLabel(currentAcctInstance) {
			utils.AddLabels(
				currentAcctInstance,
				utils.GenerateLabel(
					awsv1alpha1.IAMUserIDLabel,
					utils.GenerateShortUID(),
				),
			)
			return reconcile.Result{Requeue: true}, r.Client.Update(context.TODO(), currentAcctInstance)
		}

		awsAssumedRoleClient, creds, err := r.handleCreateAdminAccessRole(reqLogger, currentAcctInstance, awsSetupClient)
		if err != nil {
			return reconcile.Result{}, err
		}

		// Use the same ID applied to the account name for IAM usernames
		iamUserUHC := fmt.Sprintf("%s-%s", iamUserNameUHC, currentAcctInstance.Labels[awsv1alpha1.IAMUserIDLabel])
		secretName, err := r.BuildIAMUser(reqLogger, awsAssumedRoleClient, currentAcctInstance, iamUserUHC, request.Namespace)
		if err != nil {
			reason, errType := getBuildIAMUserErrorReason(err)
			errMsg := fmt.Sprintf("Failed to build IAM UHC user %s: %s", iamUserUHC, err)
			_, stateErr := r.setAccountFailed(
				reqLogger,
				currentAcctInstance,
				errType,
				reason,
				errMsg,
				AccountFailed,
			)
			if stateErr != nil {
				reqLogger.Error(err, "failed setting account state", "desiredState", AccountFailed)
			}
			return reconcile.Result{}, err
		}

		currentAcctInstance.Spec.IAMUserSecret = *secretName
		err = r.accountSpecUpdate(reqLogger, currentAcctInstance)
		if err != nil {
			return reconcile.Result{}, err
		}

		if err = r.initializeRegions(reqLogger, currentAcctInstance, creds, regionAMIs); err != nil {
			// initializeRegions logs
			return reconcile.Result{}, err
		}
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileAccount) handleAccountInitializingRegions(reqLogger logr.Logger, currentAcctInstance *awsv1alpha1.Account) (reconcile.Result, error) {
	irCond := currentAcctInstance.GetCondition(awsv1alpha1.AccountInitializingRegions)
	if irCond == nil {
		// This should never happen: the thing that made IsInitializingRegions true
		// also added the Condition.
		errMsg := "Unexpectedly couldn't find the InitializingRegions Condition"
		_, stateErr := r.setAccountFailed(
			reqLogger,
			currentAcctInstance,
			awsv1alpha1.AccountInternalError,
			"MissingCondition",
			errMsg,
			AccountFailed,
		)
		return reconcile.Result{}, stateErr
	}
	// Detect whether we set the InitializingRegions condition in *this* invocation of the
	// operator or a previous one.
	if irCond.LastTransitionTime.Before(utils.GetOperatorStartTime()) {
		// This means the region init goroutine(s) for this account were still running when an
		// earlier invocation of the operator died. We want to recover those, so set them back
		// to Creating, which should cause us to hit the region init code path again.
		// TODO(efried): There's still a small hole here: If the controller was dead for
		// too long, this can still land us in `accountCreatingTooLong` and fail the Account.
		// At the time of this writing, that specifically applies to a) non-CCS accounts, and
		// b) more than 25 minutes between initial Account creation and the Reconcile after
		// this one.
		msg := "Recovering from stale region initialization."
		// We're no longer InitializingRegions
		utils.SetAccountCondition(
			currentAcctInstance.Status.Conditions,
			awsv1alpha1.AccountInitializingRegions,
			// Switch the Condition off
			corev1.ConditionFalse,
			AccountInitializingRegions,
			msg,
			// Make sure the existing condition is updated
			utils.UpdateConditionAlways,
			currentAcctInstance.Spec.BYOC)
		// TODO(efried): This doesn't change the lastTransitionTime, which it really should.
		// In fact, since the Creating condition is guaranteed to already be present, this
		// is currently not doing anything more than
		//    currentAcctInstance.Status.State = AccountCreating
		utils.SetAccountStatus(currentAcctInstance, msg, v1alpha1.AccountCreating, AccountCreating)
		// The status update will trigger another Reconcile, but be explicit. The requests get
		// collapsed anyway.
		return reconcile.Result{Requeue: true}, r.statusUpdate(currentAcctInstance)
	}
	// The goroutines happened in this invocation. Time out if that has taken too long.
	if time.Since(irCond.LastTransitionTime.Time) > regionInitTime {
		errMsg := fmt.Sprintf("Initializing regions for longer than %d seconds", regionInitTime/time.Second)
		_, stateErr := r.setAccountFailed(
			reqLogger,
			currentAcctInstance,
			awsv1alpha1.AccountCreationFailed,
			"RegionInitializationTimeout",
			errMsg,
			AccountFailed,
		)
		return reconcile.Result{}, stateErr
	}
	// Otherwise give it a chance to finish.
	reqLogger.Info(fmt.Sprintf("Account %s is initializing regions. Ignoring.", currentAcctInstance.Name))
	// No need to requeue. If the goroutine finishes, it changes the state, which will trigger
	// a Reconcile. If it hangs forever, we'll eventually get a freebie periodic Reconcile
	// that will hit the timeout condition above.
	return reconcile.Result{}, nil
}

func (r *ReconcileAccount) handleNonCCSPendingVerification(reqLogger logr.Logger, currentAcctInstance *awsv1alpha1.Account, awsSetupClient awsclient.Client) (reconcile.Result, error) {
	// If the supportCaseID is blank and Account State = PendingVerification, create a case
	if !currentAcctInstance.HasSupportCaseID() {
		switch utils.DetectDevMode {
		case utils.DevModeProduction:
			caseID, err := createCase(reqLogger, currentAcctInstance, awsSetupClient)
			if err != nil {
				return reconcile.Result{}, err
			}
			reqLogger.Info("case created", "CaseID", caseID)

			// Update supportCaseId in CR
			currentAcctInstance.Status.SupportCaseID = caseID
			utils.SetAccountStatus(currentAcctInstance, "Account pending verification in AWS", awsv1alpha1.AccountPendingVerification, AccountPendingVerification)
			err = r.statusUpdate(currentAcctInstance)
			if err != nil {
				reqLogger.Error(err, "failed to update account state, retrying", "desired state", AccountPendingVerification)
				return reconcile.Result{}, err
			}

			// After creating the support case requeue the request. To avoid flooding and being blacklisted by AWS when
			// starting the operator with a large AccountPool, add a randomInterval (between 0 and 30 secs) to the regular wait time
			randomInterval, err := strconv.Atoi(currentAcctInstance.Spec.AwsAccountID)
			if err != nil {
				reqLogger.Error(err, "failed converting AwsAccountID string to int")
				return reconcile.Result{}, err
			}
			randomInterval %= 30

			// This will requeue verification for between 30 and 60 (30+30) seconds, depending on the account
			return reconcile.Result{RequeueAfter: time.Duration(intervalAfterCaseCreationSecs+randomInterval) * time.Second}, nil
		default:
			log.Info("Running in development mode, Skipping Support Case Creation.")
		}
	}

	var resolved bool

	switch utils.DetectDevMode {
	case utils.DevModeProduction:
		resolvedScoped, err := checkCaseResolution(reqLogger, currentAcctInstance.Status.SupportCaseID, awsSetupClient)
		if err != nil {
			reqLogger.Error(err, "Error checking for Case Resolution")
			return reconcile.Result{}, err
		}
		resolved = resolvedScoped
	default:
		log.Info("Running in development mode, Skipping case resolution check")
		resolved = true
	}

	// Case Resolved, account is Ready
	if resolved {
		reqLogger.Info("case resolved", "caseID", currentAcctInstance.Status.SupportCaseID)

		utils.SetAccountStatus(currentAcctInstance, "Account ready to be claimed", awsv1alpha1.AccountReady, AccountReady)
		return reconcile.Result{}, r.statusUpdate(currentAcctInstance)
	}

	// Case not Resolved, log info and try again in pre-defined interval
	reqLogger.Info("case not yet resolved, retrying", "caseID", currentAcctInstance.Status.SupportCaseID, "retry delay", intervalBetweenChecksMinutes)
	return reconcile.Result{RequeueAfter: intervalBetweenChecksMinutes * time.Minute}, nil
}

func (r *ReconcileAccount) finalizeAccount(reqLogger logr.Logger, awsClient awsclient.Client, account *awsv1alpha1.Account) {
	reqLogger.Info("Finalizing Account CR")
	if !account.Spec.ManualSTSMode && utils.AccountCRHasIAMUserIDLabel(account) {
		err := CleanUpIAM(reqLogger, awsClient, account)
		if err != nil {
			reqLogger.Error(err, "Failed to delete IAM user during finalizer cleanup")
		} else {
			reqLogger.Info(fmt.Sprintf("Account: %s has no label", account.Name))
		}
	}
}

func (r *ReconcileAccount) accountSpecUpdate(reqLogger logr.Logger, account *awsv1alpha1.Account) error {
	err := r.Client.Update(context.TODO(), account)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Account spec update for %s failed", account.Name))
	}
	return err
}

func (r *ReconcileAccount) nonCCSAssignAccountID(reqLogger logr.Logger, currentAcctInstance *awsv1alpha1.Account, awsSetupClient awsclient.Client) error {
	// Build Aws Account
	awsAccountID, err := r.BuildAccount(reqLogger, awsSetupClient, currentAcctInstance)
	if err != nil {
		return err
	}

	// set state creating if the account was able to create
	utils.SetAccountStatus(currentAcctInstance, AccountCreating, awsv1alpha1.AccountCreating, AccountCreating)
	err = r.statusUpdate(currentAcctInstance)

	if err != nil {
		return err
	}

	if utils.DetectDevMode != utils.DevModeProduction {
		log.Info("Running in development mode, manually creating a case ID number: 11111111")
		currentAcctInstance.Status.SupportCaseID = "11111111"
	}

	// update account cr with awsAccountID from aws
	currentAcctInstance.Spec.AwsAccountID = awsAccountID

	// tag account with hive shard name
	err = r.tagAccount(reqLogger, awsSetupClient, awsAccountID)
	if err != nil {
		reqLogger.Info("Unable to tag aws account.", "account", currentAcctInstance.Name, "AWSAccountID", awsAccountID, "Error", error.Error(err))
	}

	return r.accountSpecUpdate(reqLogger, currentAcctInstance)
}

func (r *ReconcileAccount) tagAccount(reqLogger logr.Logger, awsSetupClient awsclient.Client, awsAccountID string) error {

	inputTag := &organizations.TagResourceInput{
		ResourceId: aws.String(awsAccountID),
		Tags: []*organizations.Tag{
			{
				Key:   aws.String("owner"),
				Value: aws.String(r.shardName),
			},
		},
	}

	_, err := awsSetupClient.TagResource(inputTag)
	if err != nil {
		return err
	}

	return nil
}

func (r *ReconcileAccount) assumeRole(
	reqLogger logr.Logger,
	currentAcctInstance *awsv1alpha1.Account,
	awsSetupClient awsclient.Client,
	roleToAssume string,
	ccsRoleID string) (awsclient.Client, *sts.AssumeRoleOutput, error) {

	// The role ARN made up of the account number and the role which is the default role name
	// created in child accounts
	var roleArn = fmt.Sprintf("arn:aws:iam::%s:role/%s", currentAcctInstance.Spec.AwsAccountID, roleToAssume)
	// Use the role session name to uniquely identify a session when the same role
	// is assumed by different principals or for different reasons.
	var roleSessionName = "awsAccountOperator"

	var creds *sts.AssumeRoleOutput
	var credsErr error

	for i := 0; i < 10; i++ {

		// Get STS credentials so that we can create an aws client with
		creds, credsErr = getSTSCredentials(reqLogger, awsSetupClient, roleArn, "", roleSessionName)
		if credsErr != nil {
			// Get custom failure reason to update account status
			reason := ""
			if aerr, ok := credsErr.(awserr.Error); ok {
				reason = aerr.Code()
			}
			errMsg := fmt.Sprintf("Failed to create STS Credentials for account ID %s: %s", currentAcctInstance.Spec.AwsAccountID, credsErr)
			_, stateErr := r.setAccountFailed(
				reqLogger,
				currentAcctInstance,
				awsv1alpha1.AccountClientError,
				reason,
				errMsg,
				AccountFailed,
			)
			if stateErr != nil {
				reqLogger.Error(stateErr, "failed setting account state", "desiredState", AccountFailed)
			}
			return nil, nil, credsErr
		}

		// If this is a BYOC account, check that BYOCAdminAccess role was the one used in the AssumedRole.
		// RoleID must exist in the AssumeRoleID string. This is an eventual consistency work-around code.
		// It can take some varying amount of time to use the correct role if it had just been created.
		match, _ := matchSubstring(ccsRoleID, *creds.AssumedRoleUser.AssumedRoleId)
		if ccsRoleID != "" && !match {
			reqLogger.Info(fmt.Sprintf("Assumed RoleID:Session string does not match new RoleID: %s, %s", *creds.AssumedRoleUser.AssumedRoleId, ccsRoleID))
			reqLogger.Info(fmt.Sprintf("Sleeping %d seconds", i))
			time.Sleep(time.Duration(i) * time.Second)
		} else {
			break
		}
	}
	awsAssumedRoleClient, err := r.awsClientBuilder.GetClient(controllerName, r.Client, awsclient.NewAwsClientInput{
		AwsCredsSecretIDKey:     *creds.Credentials.AccessKeyId,
		AwsCredsSecretAccessKey: *creds.Credentials.SecretAccessKey,
		AwsToken:                *creds.Credentials.SessionToken,
		AwsRegion:               "us-east-1",
	})
	if err != nil {
		logger.Error(err, "Failed to assume role")
		reqLogger.Info(err.Error())
		errMsg := "Message Failed creating AWS Client with Assumed Role"
		_, stateErr := r.setAccountFailed(
			reqLogger,
			currentAcctInstance,
			awsv1alpha1.AccountClientError,
			"AWSClientCreationFailed",
			errMsg,
			AccountFailed,
		)
		if stateErr != nil {
			reqLogger.Error(err, "failed setting account state", "desiredState", AccountFailed)
		}
		return nil, nil, err
	}

	return awsAssumedRoleClient, creds, nil
}

func (r *ReconcileAccount) initializeRegions(reqLogger logr.Logger, currentAcctInstance *awsv1alpha1.Account, creds *sts.AssumeRoleOutput, regionAMIs map[string]awsv1alpha1.AmiSpec) error {
	// Instantiate a client with a default region to retrieve regions we want to initialize
	awsClient, err := r.awsClientBuilder.GetClient(controllerName, r.Client, awsclient.NewAwsClientInput{
		AwsCredsSecretIDKey:     *creds.Credentials.AccessKeyId,
		AwsCredsSecretAccessKey: *creds.Credentials.SecretAccessKey,
		AwsToken:                *creds.Credentials.SessionToken,
		AwsRegion:               awsv1alpha1.AwsUSEastOneRegion,
	})
	if err != nil {
		connErr := fmt.Sprintf("unable to connect to default region %s", awsv1alpha1.AwsUSEastOneRegion)
		reqLogger.Error(err, connErr)
		return err
	}

	// Get a list of regions enabled in the current account
	regionsEnabledInAccount, err := awsClient.DescribeRegions(&ec2.DescribeRegionsInput{
		AllRegions: aws.Bool(false),
	})
	if err != nil {
		// Retry on failures related to the slow AWS API
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == "OptInRequired" {
				return nil
			}
		}
		reqLogger.Error(err, "Failed to retrieve list of regions enabled in this account.")
		return err
	}

	// We're about to kick off region init in a goroutine. This status makes subsequent
	// Reconciles ignore the Account (unless it stays in this state for too long).
	utils.SetAccountStatus(currentAcctInstance, "Initializing Regions", awsv1alpha1.AccountInitializingRegions, AccountInitializingRegions)
	if err := r.statusUpdate(currentAcctInstance); err != nil {
		// statusUpdate logs
		return err
	}

	// For accounts created by the accountpool we want to ensure we initiate all regions
	if !currentAcctInstance.IsBYOC() {
		go r.asyncRegionInit(reqLogger, currentAcctInstance, creds, regionAMIs, castAWSRegionType(regionsEnabledInAccount.Regions))
		return nil
	}

	// For non OSD accounts we check the desired region from the accountclaim and ensure that the account has
	// that region enabled, fail otherwise
	accountClaim, acctClaimErr := r.getAccountClaim(currentAcctInstance)
	if acctClaimErr != nil {
		reqLogger.Info("Accountclaim not found")
		return acctClaimErr
	}
	for _, wantedRegion := range accountClaim.Spec.Aws.Regions {
		found := false
		for _, enabledRegion := range regionsEnabledInAccount.Regions {
			if wantedRegion.Name == *enabledRegion.RegionName {
				found = true
			}
		}
		if !found {
			utils.SetAccountStatus(
				currentAcctInstance,
				fmt.Sprintf("AWS region %s is not supported for AWS account %s", wantedRegion, currentAcctInstance.Name),
				awsv1alpha1.AccountInitializingRegions, AccountInitializingRegions)
			if err := r.statusUpdate(currentAcctInstance); err != nil {
				// statusUpdate logs
				return err
			}
			return err
		}
	}

	// This initializes supported regions, and updates Account state when that's done. There is
	// no error checking at this level.
	// Only initiate the one requested region
	go r.asyncRegionInit(reqLogger, currentAcctInstance, creds, regionAMIs, accountClaim.Spec.Aws.Regions)

	return nil
}

// asyncRegionInit initializes supported regions by creating and destroying an instance in each.
// Upon completion, it *always* sets the Account status to either Ready or PendingVerification.
// There is no mechanism for this func to report errors to its parent. The only error paths
// currently possible are:
// - The Status update fails.
// - This goroutine dies in some horrible and unpredictable way.
// In either case we would expect the main reconciler to eventually notice that the Account has
// been in the InitializingRegions state for too long, and set it to Failed.
func (r *ReconcileAccount) asyncRegionInit(reqLogger logr.Logger, currentAcctInstance *awsv1alpha1.Account, creds *sts.AssumeRoleOutput, regionAMIs map[string]awsv1alpha1.AmiSpec, regionsEnabledInAccount []v1alpha1.AwsRegions) {

	// Initialize all supported regions by creating and terminating an instance in each
	r.InitializeSupportedRegions(reqLogger, currentAcctInstance, regionsEnabledInAccount, creds, regionAMIs)

	if currentAcctInstance.IsBYOC() {
		utils.SetAccountStatus(currentAcctInstance, "BYOC Account Ready", awsv1alpha1.AccountReady, AccountReady)

	} else {
		if currentAcctInstance.GetCondition(awsv1alpha1.AccountReady) != nil {
			msg := "Account support case already resolved; Account Ready"
			utils.SetAccountStatus(currentAcctInstance, msg, awsv1alpha1.AccountReady, AccountReady)
			reqLogger.Info(msg)
		} else {
			msg := "Account pending AWS limits verification"
			utils.SetAccountStatus(currentAcctInstance, msg, awsv1alpha1.AccountPendingVerification, AccountPendingVerification)
			reqLogger.Info(msg)
		}
	}

	if err := r.statusUpdate(currentAcctInstance); err != nil {
		// If this happens, the Account should eventually get set to Failed by the
		// accountOlderThan check in the main controller.
		reqLogger.Error(err, "asyncRegionInit failed to update status")
	}
}

// BuildAccount take all parameters required and uses those to make an aws call to CreateAccount. It returns an account ID and and error
func (r *ReconcileAccount) BuildAccount(reqLogger logr.Logger, awsClient awsclient.Client, account *awsv1alpha1.Account) (string, error) {
	reqLogger.Info("Creating Account")

	email := formatAccountEmail(account.Name)
	orgOutput, orgErr := CreateAccount(reqLogger, awsClient, account.Name, email)
	// If it was an api or a limit issue don't modify account and exit if anything else set to failed
	if orgErr != nil {
		switch orgErr {
		case awsv1alpha1.ErrAwsFailedCreateAccount:
			utils.SetAccountStatus(account, "Failed to create AWS Account", awsv1alpha1.AccountCreationFailed, AccountFailed)
			err := r.statusUpdate(account)
			if err != nil {
				return "", err
			}

			reqLogger.Error(awsv1alpha1.ErrAwsFailedCreateAccount, "Failed to create AWS Account")
			return "", orgErr

		case awsv1alpha1.ErrAwsAccountLimitExceeded:
			log.Error(orgErr, "Failed to create AWS Account limit reached")
			return "", orgErr

		default:
			log.Error(orgErr, "Failed to create AWS Account nonfatal error")
			return "", orgErr
		}

	}

	accountObjectKey, err := client.ObjectKeyFromObject(account)
	if err != nil {
		reqLogger.Error(err, "Unable to get name and namespace of Account object")
	}
	err = r.Client.Get(context.TODO(), accountObjectKey, account)
	if err != nil {
		reqLogger.Error(err, "Unable to get updated Account object after status update")
	}

	reqLogger.Info("account created successfully")

	return *orgOutput.CreateAccountStatus.AccountId, nil
}

// CreateAccount creates an AWS account for the specified accountName and accountEmail in the organization
func CreateAccount(reqLogger logr.Logger, client awsclient.Client, accountName, accountEmail string) (*organizations.DescribeCreateAccountStatusOutput, error) {

	createInput := organizations.CreateAccountInput{
		AccountName: aws.String(accountName),
		Email:       aws.String(accountEmail),
	}

	createOutput, err := client.CreateAccount(&createInput)
	if err != nil {
		var returnErr error
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case organizations.ErrCodeConstraintViolationException:
				returnErr = awsv1alpha1.ErrAwsAccountLimitExceeded
			case organizations.ErrCodeServiceException:
				returnErr = awsv1alpha1.ErrAwsInternalFailure
			case organizations.ErrCodeTooManyRequestsException:
				returnErr = awsv1alpha1.ErrAwsTooManyRequests
			default:
				returnErr = awsv1alpha1.ErrAwsFailedCreateAccount
				utils.LogAwsError(reqLogger, "New AWS Error during account creation", returnErr, err)
			}

		}
		return &organizations.DescribeCreateAccountStatusOutput{}, returnErr
	}

	describeStatusInput := organizations.DescribeCreateAccountStatusInput{
		CreateAccountRequestId: createOutput.CreateAccountStatus.Id,
	}

	var accountStatus *organizations.DescribeCreateAccountStatusOutput
	for {
		status, err := client.DescribeCreateAccountStatus(&describeStatusInput)
		if err != nil {
			return &organizations.DescribeCreateAccountStatusOutput{}, err
		}

		accountStatus = status
		createStatus := *status.CreateAccountStatus.State

		if createStatus == "FAILED" {
			var returnErr error
			switch *status.CreateAccountStatus.FailureReason {
			case "ACCOUNT_LIMIT_EXCEEDED":
				returnErr = awsv1alpha1.ErrAwsAccountLimitExceeded
			case "INTERNAL_FAILURE":
				returnErr = awsv1alpha1.ErrAwsInternalFailure
			default:
				returnErr = awsv1alpha1.ErrAwsFailedCreateAccount
			}

			return &organizations.DescribeCreateAccountStatusOutput{}, returnErr
		}

		if createStatus != "IN_PROGRESS" {
			break
		}
	}

	return accountStatus, nil
}

func formatAccountEmail(name string) string {
	// osd-creds-mgmt
	// libra-ops
	splitString := strings.Split(name, "-")
	prefix := splitString[0]
	for i := 1; i < (len(splitString) - 1); i++ {
		prefix = prefix + "-" + splitString[i]
	}

	email := prefix + "+" + splitString[len(splitString)-1] + "@redhat.com"
	return email
}

func (r *ReconcileAccount) statusUpdate(account *awsv1alpha1.Account) error {
	err := r.Client.Status().Update(context.TODO(), account)
	return err
}

func (r *ReconcileAccount) setAccountFailed(reqLogger logr.Logger, account *awsv1alpha1.Account, ctype v1alpha1.AccountConditionType, reason string, message string, state string) (reconcile.Result, error) {
	reqLogger.Info(message)
	// Update account status and condition
	account.Status.Conditions = utils.SetAccountCondition(
		account.Status.Conditions,
		ctype,
		corev1.ConditionTrue,
		reason,
		message,
		utils.UpdateConditionNever,
		account.Spec.BYOC,
	)
	account.Status.State = state

	// Set the failure in the accountClaim as well
	err := r.accountClaimError(reqLogger, account, reason, message)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Apply update
	err = r.statusUpdate(account)
	if err != nil {
		reqLogger.Error(err, "failed to update account status")
	}

	return reconcile.Result{Requeue: true}, nil
}

func (r *ReconcileAccount) getAccountClaim(account *awsv1alpha1.Account) (*awsv1alpha1.AccountClaim, error) {
	accountClaim := &awsv1alpha1.AccountClaim{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{
		Name: account.Spec.ClaimLink, Namespace: account.Spec.ClaimLinkNamespace}, accountClaim)

	if err != nil {
		return nil, err
	}
	return accountClaim, nil
}

func (r *ReconcileAccount) accountClaimError(reqLogger logr.Logger, account *awsv1alpha1.Account, reason string, message string) error {
	// Retrieve accountClaim
	accountClaim, err := r.getAccountClaim(account)
	if err != nil {
		if k8serr.IsNotFound(err) {
			return nil
		}
		reqLogger.Error(err, "Internal error occurred, updating accountclaim to reflect this")
		return err
	}

	accountClaim = r.failAllAccountClaimStatus(accountClaim)
	accountClaim.Status.Conditions = utils.SetAccountClaimCondition(
		accountClaim.Status.Conditions,
		awsv1alpha1.InternalError,
		corev1.ConditionTrue,
		reason,
		message,
		utils.UpdateConditionIfReasonOrMessageChange,
		accountClaim.Spec.BYOCAWSAccountID != "",
	)
	accountClaim.Status.State = awsv1alpha1.ClaimStatusError

	// Update the *accountClaim* status (not the account status)
	err = r.Client.Status().Update(context.TODO(), accountClaim)
	if err != nil {
		reqLogger.Error(err, "failed to update accountclaim status", "accountclaim", accountClaim.Name)
	}

	return err

}

func (r *ReconcileAccount) failAllAccountClaimStatus(accountClaim *awsv1alpha1.AccountClaim) *awsv1alpha1.AccountClaim {
	for _, condition := range accountClaim.Status.Conditions {
		condition.Status = corev1.ConditionFalse
	}
	return accountClaim
}

func (r *ReconcileAccount) setAccountClaimError(reqLogger logr.Logger, currentAccountInstance *awsv1alpha1.Account, message string) error {
	accountClaim, err := r.getAccountClaim(currentAccountInstance)
	if err != nil {
		if k8serr.IsNotFound(err) {
			// If the accountClaim is not found, no need to update the accountClaim
			return nil
		}
		reqLogger.Error(err, "unable to get accountclaim")
		return err
	}

	var reason string
	var conditionType awsv1alpha1.AccountClaimConditionType

	if currentAccountInstance.IsBYOC() {
		message = fmt.Sprintf("CCS Account Failed: %s", message)
		conditionType = awsv1alpha1.CCSAccountClaimFailed
		reason = string(awsv1alpha1.CCSAccountClaimFailed)
	} else {
		message = fmt.Sprintf("Account Failed: %s", message)
		conditionType = awsv1alpha1.AccountClaimFailed
		reason = string(awsv1alpha1.AccountClaimFailed)
	}

	accountClaim.Status.Conditions = utils.SetAccountClaimCondition(
		accountClaim.Status.Conditions,
		conditionType,
		corev1.ConditionTrue,
		reason,
		message,
		utils.UpdateConditionIfReasonOrMessageChange,
		accountClaim.Spec.BYOCAWSAccountID != "",
	)

	accountClaim.Status.State = awsv1alpha1.ClaimStatusError

	// Update the *accountClaim* status (not the account status)
	err = r.Client.Status().Update(context.TODO(), accountClaim)
	if err != nil {
		reqLogger.Error(err, "failed to update accountclaim status", "accountclaim", accountClaim.Name)
	}

	return err
}

func matchSubstring(roleID, role string) (bool, error) {
	matched, err := regexp.MatchString(roleID, role)
	return matched, err
}

func getAssumeRole(c *awsv1alpha1.Account) string {
	// If the account is a CCS account, return the CCS role
	if c.IsBYOC() {
		return fmt.Sprintf("%s-%s", byocRole, c.Labels[awsv1alpha1.IAMUserIDLabel])
	}

	// Else return the default role
	return awsv1alpha1.AccountOperatorIAMRole
}

func getBuildIAMUserErrorReason(err error) (string, awsv1alpha1.AccountConditionType) {
	if err == awsv1alpha1.ErrInvalidToken {
		return "InvalidClientTokenId", awsv1alpha1.AccountAuthenticationError
	} else if err == awsv1alpha1.ErrAccessDenied {
		return "AccessDenied", awsv1alpha1.AccountAuthorizationError
	} else if _, ok := err.(awserr.Error); ok {
		return "ClientError", awsv1alpha1.AccountClientError
	} else {
		return "UnhandledError", awsv1alpha1.AccountUnhandledError
	}
}

// processConfigMapRegions is a very hacky way of turning the region ami data we store in the configmap into an region-ami map
func processConfigMapRegions(regionString string) map[string]awsv1alpha1.AmiSpec {
	output := make(map[string]awsv1alpha1.AmiSpec)
	regionsDelimited := strings.Split(regionString, "\n")
	for _, value := range regionsDelimited {
		tempArr := strings.Split(value, ":")
		if len(tempArr) == 3 {
			output[strings.ReplaceAll(tempArr[0], " ", "")] = awsv1alpha1.AmiSpec{
				Ami:          strings.ReplaceAll(tempArr[1], " ", ""),
				InstanceType: strings.ReplaceAll(tempArr[2], " ", ""),
			}
		}
	}
	return output
}

// getManagedTags retrieves a list of managed tags from the configmap
// returns an empty list on any failure.
func (r *ReconcileAccount) getManagedTags(log logr.Logger) []awsclient.AWSTag {
	tags := []awsclient.AWSTag{}

	cm := &corev1.ConfigMap{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Namespace: awsv1alpha1.AccountCrNamespace, Name: awsv1alpha1.DefaultConfigMap}, cm)
	if err != nil {
		log.Info("There was an error getting the default configmap.", "error", err)
		return tags
	}

	managedTags, ok := cm.Data[awsv1alpha1.ManagedTagsConfigMapKey]
	if !ok {
		log.Info("There are no Managed Tags defined.")
		return tags
	}

	return parseTagsFromString(managedTags)
}

// getCustomerTags retrieves a list of customer-provided tags from the linked accountclaim
func (r *ReconcileAccount) getCustomTags(log logr.Logger, account *awsv1alpha1.Account) []awsclient.AWSTag {
	tags := []awsclient.AWSTag{}

	accountClaim, err := r.getAccountClaim(account)
	if err != nil {
		log.Error(err, "Error getting AccountClaim to get custom tags")
		return tags
	}

	if accountClaim.Spec.CustomTags == "" {
		return tags
	}

	return parseTagsFromString(accountClaim.Spec.CustomTags)
}

// processTagsFromString accepts a set of strings, each being a key=value pair, one per line.  This is typically defined in YAML similar to:
//
// myTags: |
//   key=value
//   my-tag=true
//   base64-is-accepted=eWVzIQ==
//
// Specifically, we are splitting on the FIRST "=" to deliniate key=value, so any equals signs after the first will go into the value.
func parseTagsFromString(tags string) []awsclient.AWSTag {
	parsedTags := []awsclient.AWSTag{}

	// Split on Newline to get key-value pairs
	kvpairs := strings.Split(tags, "\n")

	for _, tagString := range kvpairs {
		// Sometimes the last value is an empty string.  Don't process those.
		if tagString == "" {
			continue
		}

		// Use strings.SplitN to only split on the first "="
		tagKV := strings.SplitN(tagString, "=", 2)
		parsedTags = append(parsedTags, awsclient.AWSTag{
			Key:   tagKV[0],
			Value: tagKV[1],
		})
	}

	return parsedTags
}

func castAWSRegionType(regions []*ec2.Region) []v1alpha1.AwsRegions {
	var awsRegions []v1alpha1.AwsRegions
	for _, region := range regions {
		awsRegions = append(awsRegions, v1alpha1.AwsRegions{Name: *region.RegionName})
	}
	return awsRegions
}

func (r *ReconcileAccount) handleCreateAdminAccessRole(
	reqLogger logr.Logger,
	currentAcctInstance *awsv1alpha1.Account,
	awsSetupClient awsclient.Client) (awsclient.Client, *sts.AssumeRoleOutput, error) {

	var awsAssumedRoleClient awsclient.Client
	var creds *sts.AssumeRoleOutput
	currentAccInstanceID := currentAcctInstance.Labels[awsv1alpha1.IAMUserIDLabel]
	roleToAssume := getAssumeRole(currentAcctInstance)

	SREAccessARN, err := r.GetSREAccessARN(reqLogger)
	if err != nil {
		reqLogger.Error(err, "An error was encountered retrieving the SRE Access ARN")
		return nil, nil, err
	}

	// Build the tags required to create the Admin Access Role
	tags := awsclient.AWSTags.BuildTags(
		currentAcctInstance,
		r.getManagedTags(reqLogger),
		r.getCustomTags(reqLogger, currentAcctInstance),
	).GetIAMTags()

	// In this block we are creating the BYOCAdminAccessRole-XYZ for both CCS and non-CCS accounts.
	// The dependency on the roleID validation within the assumeRole, and the different aws clients
	// required between CCS and non-CCS, is what has caused these steps to be done independently.
	if currentAcctInstance.Spec.BYOC {
		// The CCS uses the CCS client for creating the BYOCAdminAccessRole and then utilizes the RoleID
		// generated from that in the assumeRole func for role validation

		// Get the AccountClaim in Order to retrieve the CCSClient
		accountClaim, acctClaimErr := r.getAccountClaim(currentAcctInstance)
		if acctClaimErr != nil {
			if accountClaim != nil {
				utils.SetAccountClaimStatus(
					accountClaim,
					"Failed to get AccountClaim for Account",
					"FailedRetrievingAccountClaim",
					awsv1alpha1.ClientError,
					awsv1alpha1.ClaimStatusError,
				)
				err := r.Client.Status().Update(context.TODO(), accountClaim)
				if err != nil {
					reqLogger.Error(err, "failed to update accountclaim status")
				}
			} else {
				reqLogger.Error(acctClaimErr, "accountclaim is nil")
			}
			return nil, nil, acctClaimErr
		}
		ccsClient, err := r.getCCSClient(currentAcctInstance, accountClaim)
		if err != nil {
			reqLogger.Error(err, "An error was encountered retrieving CCS Client")
			return nil, nil, err
		}

		roleID, err := createBYOCAdminAccessRole(
			reqLogger,
			awsSetupClient,
			ccsClient,
			adminAccessArn,
			currentAccInstanceID,
			tags,
			SREAccessARN,
		)

		if err != nil {
			reqLogger.Error(err, "createBYOCAdminAccessRole err", roleID)
			return nil, nil, err
		}

		awsAssumedRoleClient, creds, err = r.assumeRole(reqLogger, currentAcctInstance, awsSetupClient, roleToAssume, roleID)
		if err != nil {
			return nil, nil, err
		}

	} else {
		// Unlike the CCS block, the non-CCS block does not have a dependency on the RoleID to assumeRole. The
		// awsAssumedRoleClient is what is needed to create the BYOCAdminAccessRole in the non-CCS account.
		awsAssumedRoleClient, creds, err = r.assumeRole(reqLogger, currentAcctInstance, awsSetupClient, roleToAssume, "")
		if err != nil {
			return nil, nil, err
		}

		roleID, err := createBYOCAdminAccessRole(
			reqLogger,
			awsSetupClient,
			awsAssumedRoleClient,
			adminAccessArn,
			currentAccInstanceID,
			tags,
			SREAccessARN,
		)

		if err != nil {
			reqLogger.Error(err, "createBYOCAdminAccessRole err", roleID)
			return nil, nil, err
		}
	}

	return awsAssumedRoleClient, creds, err
}
