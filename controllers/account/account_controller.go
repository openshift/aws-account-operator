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
	stsclient "github.com/openshift/aws-account-operator/pkg/awsclient/sts"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/config"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/totalaccountwatcher"
	"github.com/openshift/aws-account-operator/pkg/utils"
)

var log = logf.Log.WithName("controller_account")
var AssumeRoleAndCreateClient = stsclient.AssumeRoleAndCreateClient

const (
	// createPendTime is the maximum time we allow an Account to sit in Creating state before we
	// time out and set it to Failed.
	createPendTime = utils.WaitTime * time.Minute
	// regionInitTime is the maximum time we allow an account CR to be in the InitializingRegions
	// state. This is based on async region init taking a theoretical maximum of WaitTime * 2
	// minutes plus a handful of AWS API calls (see asyncRegionInit).
	regionInitTime = (time.Minute * utils.WaitTime * time.Duration(2)) + time.Minute
	// awsAccountInitRequeueDuration is the duration we want to wait for the next
	// reconcile loop after hitting an OptInRequired-error during region initialization.
	awsAccountInitRequeueDuration = 1 * time.Minute

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
	// AccountOptingInRegions indicates region enablement for supported Opt-In regions is in progress
	AccountOptingInRegions = "OptingInRegions"
	// AccountOptInRegionEnabled indicates that supported Opt-In regions have been enabled
	AccountOptInRegionEnabled    = "OptInRegionsEnabled"
	standardAdminAccessArnPrefix = "arn:aws:iam"
	adminAccessArnSuffix         = "::aws:policy/AdministratorAccess"
	iamUserNameUHC               = "osdManagedAdmin"

	controllerName = "account"

	// number of service quota requests we are allowed to open concurrently in AWS
	MaxOpenQuotaRequests = 20

	// MaxOptInRegionRequest maximum number of regions that AWS allows to be concurrently enabled
	MaxOptInRegionRequest = 6
	// MaxAccountRegionEnablement maximum number of AWS accounts allowed to enable all regions simultaneously
	MaxAccountRegionEnablement = 9
)

// AccountReconciler reconciles a Account object
type AccountReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	awsClientBuilder awsclient.IBuilder
	shardName        string
}

//+kubebuilder:rbac:groups=aws.managed.openshift.io,resources=accounts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=aws.managed.openshift.io,resources=accounts/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=aws.managed.openshift.io,resources=accounts/finalizers,verbs=update

// Reconcile reads that state of the cluster for a Account object and makes changes based on the state read
// and what is in the Account.Spec
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *AccountReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	reqLogger := log.WithValues("Controller", controllerName, "Request.Namespace", request.Namespace, "Request.Name", request.Name)

	// Fetch the Account instance
	currentAcctInstance := &awsv1alpha1.Account{}
	err := r.Get(context.TODO(), request.NamespacedName, currentAcctInstance)

	if err != nil {
		if k8serr.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	configMap, err := utils.GetOperatorConfigMap(r.Client)
	if err != nil {
		log.Error(err, "Failed retrieving configmap")
		return reconcile.Result{}, err
	}

	// Read compliance tags from ConfigMap
	complianceTags, err := r.generateAccountTags(reqLogger, configMap)
	if err != nil {
		return reconcile.Result{}, err
	}
	reqLogger.Info("Compliance tags loaded", "count", len(complianceTags))

	isOptInRegionFeatureEnabled, err := utils.GetFeatureFlagValue(configMap, "feature.opt_in_regions")
	if err != nil {
		reqLogger.Info("Could not retrieve feature flag 'feature.opt_in_regions' - region Opt-In is disabled")
		isOptInRegionFeatureEnabled = false
	}
	reqLogger.Info("Is feature.opt_in_regions enabled?", "enabled", isOptInRegionFeatureEnabled)

	optInRegions, ok := configMap.Data["opt-in-regions"]
	if !ok {
		reqLogger.Info("Could not retrieve opt-in-regions from configMap")
	}

	awsRegion := config.GetDefaultRegion()
	// We expect this secret to exist in the same namespace Account CR's are created
	awsSetupClient, err := r.awsClientBuilder.GetClient(controllerName, r.Client, awsclient.NewAwsClientInput{
		SecretName: utils.AwsSecretName,
		NameSpace:  awsv1alpha1.AccountCrNamespace,
		AwsRegion:  awsRegion,
	})
	if err != nil {
		reqLogger.Error(err, "failed building operator AWS client")
		return reconcile.Result{}, err
	}

	// Add finalizer to non-sts account cr
	if !currentAcctInstance.Spec.ManualSTSMode {
		err := r.addFinalizer(reqLogger, currentAcctInstance)
		if err != nil {
			return reconcile.Result{}, err
		}
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

		var awsClient awsclient.Client
		if currentAcctInstance.IsBYOC() {
			roleToAssume := currentAcctInstance.GetAssumeRole()
			awsClient, _, err = stsclient.HandleRoleAssumption(reqLogger, r.awsClientBuilder, currentAcctInstance, r.Client, awsSetupClient, "", roleToAssume, "")
			if err != nil {
				reqLogger.Error(err, "failed building BYOC client from assume_role")
				_, err = r.handleAWSClientError(reqLogger, currentAcctInstance, err)
				if aerr, ok := err.(awserr.Error); ok {
					switch aerr.Code() {
					// If it's AccessDenied we want to just delete the finalizer and continue as we assume
					// the credentials have been deleted by the customer. For additional safety we also only
					// want to do this for CCS accounts.
					case "AccessDenied":
						if currentAcctInstance.IsBYOC() {
							err = r.removeFinalizer(currentAcctInstance, awsv1alpha1.AccountFinalizer)
							if err != nil {
								reqLogger.Error(err, "failed removing account finalizer")
								return reconcile.Result{}, err
							}
							reqLogger.Info("Finalizer Removed on CCS Account with ACCESSDENIED")
							return reconcile.Result{}, nil
						}
					}
				}
				return reconcile.Result{}, err
			}
		} else {
			awsClient, _, err = stsclient.HandleRoleAssumption(reqLogger, r.awsClientBuilder, currentAcctInstance, r.Client, awsSetupClient, "", awsv1alpha1.AccountOperatorIAMRole, "")
			if err != nil {
				reqLogger.Error(err, "failed building AWS client from assume_role")
				return r.handleAWSClientError(reqLogger, currentAcctInstance, err)
			}
		}
		r.finalizeAccount(reqLogger, awsClient, currentAcctInstance)
		//return reconcile.Result{}, nil

		// Remove finalizer if account CR is non STS. For CCS accounts, the accountclaim controller will delete the account CR
		// when the accountClaim CR is deleted as its set as the owner reference.
		if currentAcctInstance.IsNonSTSPendingDeletionWithFinalizer() {
			reqLogger.Info("removing account finalizer")
			err = r.removeFinalizer(currentAcctInstance, awsv1alpha1.AccountFinalizer)
			if err != nil {
				reqLogger.Error(err, "failed removing account finalizer")
				return reconcile.Result{}, err
			}
		}
		return reconcile.Result{}, nil
	}

	// Handles IAM user and secret recreation for accounts that are reused, non-BYOC, and in a ready state
	// This function is essential because a Fleet Manager AWS account should not possess any long-lived IAM credentials; instead, it should only require STS IAM access.
	// However, once a Fleet Manager account claim is deleted, the AWS account no longer has long-lived IAM credentials and cannot be claimed by non-Fleet Manager account claims.
	if currentAcctInstance.IsReusedAccountMissingIAMUser() {
		if _, _, err = r.handleIAMUserCreation(reqLogger, currentAcctInstance, awsSetupClient, request.Namespace); err != nil {
			reqLogger.Error(err, "Error during IAM user creation for reused account")
			return reconcile.Result{}, err
		}
		reqLogger.Info(fmt.Sprintf("Account %s IAM user and secret has been recreated.", currentAcctInstance.Name))
	}

	// Log accounts that have failed and don't attempt to reconcile them
	if currentAcctInstance.IsFailed() {
		reqLogger.Info(fmt.Sprintf("Account %s is failed. Ignoring.", currentAcctInstance.Name))
		return reconcile.Result{}, nil
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
			return r.HandleNonCCSPendingVerification(reqLogger, currentAcctInstance, awsSetupClient)
		}

		// Update account Status.Claimed to true if the account is ready and the claim link is not empty
		if currentAcctInstance.IsReadyUnclaimedAndHasClaimLink() {
			return reconcile.Result{}, ClaimAccount(r, currentAcctInstance)
		}

		// see if in creating for longer than default wait time
		if currentAcctInstance.IsCreating() && utils.CreationConditionOlderThan(*currentAcctInstance, createPendTime) {
			errMsg := fmt.Sprintf("Creation pending for longer than %d minutes", utils.WaitTime)
			_, stateErr := r.setAccountFailed(
				reqLogger,
				currentAcctInstance,
				awsv1alpha1.AccountCreationFailed,
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
					// fedramp clusters are all CCS, so the account limit is irrelevant there
					if !config.IsFedramp() {
						reqLogger.Info("AWS Account limit reached. This does not always indicate a problem, it's a limit we enforce in the configmap to prevent runaway account creation")
						// We don't expect the limit to change very frequently, so wait a while before requeueing to avoid hot lopping.
						return reconcile.Result{Requeue: true, RequeueAfter: time.Duration(5) * time.Minute}, nil
					}
				}

				if err := r.nonCCSAssignAccountID(reqLogger, currentAcctInstance, awsSetupClient, complianceTags); err != nil {
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

	// Handles account region enablement for non-BYOC accounts
	if (currentAcctInstance.ReadyForRegionEnablement() || currentAcctInstance.IsEnablingOptInRegions()) && isOptInRegionFeatureEnabled && optInRegions != "" {
		return r.handleOptInRegionEnablement(reqLogger, currentAcctInstance, awsSetupClient, optInRegions)
	}

	// Get the owner of the Red Hat amis from the configmap
	amiOwner, ok := configMap.Data["ami-owner"]
	if !ok {
		err = awsv1alpha1.ErrInvalidConfigMap
		return reconcile.Result{}, err
	}
	if err != nil {
		reqLogger.Error(err, "failed getting ami-owner from configmap data")
		return reconcile.Result{}, err
	}

	// Account init for both BYOC and Non-BYOC
	if currentAcctInstance.ReadyForInitialization() {
		reqLogger.Info("initializing account", "awsAccountID", currentAcctInstance.Spec.AwsAccountID)

		var creds *sts.AssumeRoleOutput

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

			_, creds, err = r.getSTSClient(reqLogger, accountClaim, awsSetupClient)
			if err != nil {
				reqLogger.Error(err, "error getting sts client to initialize regions")
				return reconcile.Result{}, err
			}

		} else {
			// Set IAMUserIDLabel if not there, and requeue
			if !utils.AccountCRHasIAMUserIDLabel(currentAcctInstance) {
				utils.AddLabels(
					currentAcctInstance,
					utils.GenerateLabel(
						awsv1alpha1.IAMUserIDLabel,
						utils.GenerateShortUID(),
					),
				)
				return reconcile.Result{Requeue: true}, r.Update(context.TODO(), currentAcctInstance)
			}

			_, newCredentials, err := r.handleIAMUserCreation(reqLogger, currentAcctInstance, awsSetupClient, request.Namespace)
			if err != nil {
				reqLogger.Error(err, "Error during IAM user creation")
				return reconcile.Result{}, err
			}
			creds = newCredentials

		}

		err = r.initializeRegions(reqLogger, currentAcctInstance, creds, amiOwner)

		if isAwsOptInError(err) {
			reqLogger.Info("Aws Account not ready yet, requeuing.")
			return reconcile.Result{
				RequeueAfter: awsAccountInitRequeueDuration,
			}, nil
		}

		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// generateAccountTags reads compliance tag values from the ConfigMap and returns a map of tag key-value pairs
func (r *AccountReconciler) generateAccountTags(reqLogger logr.Logger, configMap *corev1.ConfigMap) (map[string]string, error) {
	tags := make(map[string]string)

	// Check feature flag
	enabled, err := strconv.ParseBool(configMap.Data["feature.compliance_tags"])
	if err != nil {
		reqLogger.Info("Could not retrieve feature flag 'feature.compliance_tags' - compliance tagging is disabled")
		return tags, nil
	}

	if !enabled {
		reqLogger.Info("Compliance tagging is disabled")
		return tags, nil
	}

	// Read tag values and add to map only if non-empty
	if appCode, ok := configMap.Data["app-code"]; ok && appCode != "" {
		tags["app-code"] = appCode
	} else {
		reqLogger.Info("Could not retrieve configuration map value 'app-code' - compliance tag will be skipped")
	}

	if servicePhase, ok := configMap.Data["service-phase"]; ok && servicePhase != "" {
		tags["service-phase"] = servicePhase
	} else {
		reqLogger.Info("Could not retrieve configuration map value 'service-phase' - compliance tag will be skipped")
	}

	if costCenter, ok := configMap.Data["cost-center"]; ok && costCenter != "" {
		tags["cost-center"] = costCenter
	} else {
		reqLogger.Info("Could not retrieve configuration map value 'cost-center' - compliance tag will be skipped")
	}

	return tags, nil
}

func (r *AccountReconciler) handleOptInRegionEnablement(reqLogger logr.Logger, currentAcctInstance *awsv1alpha1.Account, awsSetupClient awsclient.Client, optInRegions string) (reconcile.Result, error) {
	numberOfAccountsOptingIn, err := CalculateOptingInRegionAccounts(reqLogger, r.Client)
	if err != nil {
		return reconcile.Result{}, nil
	}

	if currentAcctInstance.Status.OptInRegions == nil {
		switch utils.DetectDevMode {
		case utils.DevModeProduction:
			if numberOfAccountsOptingIn >= MaxAccountRegionEnablement {
				return reconcile.Result{RequeueAfter: intervalBetweenChecksMinutes * time.Minute}, nil
			}
			var regionList []string
			regions := strings.Split(optInRegions, ",")
			for _, region := range regions {
				regionList = append(regionList, strings.TrimSpace(region))
			}
			//updates account status to indicate supported opt-in region are pending enablement
			err = SetOptRegionStatus(reqLogger, regionList, currentAcctInstance)
			if err != nil {
				reqLogger.Error(err, "failed to set account opt-in region status")
				return reconcile.Result{}, err
			}
			utils.SetAccountStatus(currentAcctInstance, "Opting-In Regions", awsv1alpha1.AccountOptingInRegions, AccountOptingInRegions)

			err = r.statusUpdate(currentAcctInstance)
			if err != nil {
				reqLogger.Error(err, "failed to update account status, retrying")
				return reconcile.Result{}, err
			}

		default:
			log.Info("Running in development mode, Skipping Opt-In Region Enablement.")
		}
	}

	if currentAcctInstance.HasOpenOptInRegionRequests() {
		switch utils.DetectDevMode {
		case utils.DevModeProduction:
			return GetOptInRegionStatus(reqLogger, r.awsClientBuilder, awsSetupClient, currentAcctInstance, r.Client)
		}
	}

	openCaseCount, _ := currentAcctInstance.GetOptInRequestsByStatus(awsv1alpha1.OptInRequestEnabling)

	if openCaseCount == 0 {
		reqLogger.Info("All Opt-In Regions have been enabled", "AccountID", currentAcctInstance.Spec.AwsAccountID)
		utils.SetAccountStatus(currentAcctInstance, "Opting-In Regions", awsv1alpha1.AccountOptInRegionEnabled, AccountOptInRegionEnabled)
		_ = r.statusUpdate(currentAcctInstance)
		return reconcile.Result{}, nil
	}
	return reconcile.Result{RequeueAfter: intervalBetweenChecksMinutes * time.Minute}, nil
}

// isAwsOptInError checks weather the error passed in is an instance of an Aws
// Error with the error code "OptInRequired". This usually indicates that a
// newly created aws account is not yet fully operational.
//
// returns true only if the error can be cast to an instance of awserr.Error and has the appropriate code set. Passing in `nil` also returns false.
func isAwsOptInError(err error) bool {
	if err == nil {
		return false
	}

	awsError, ok := err.(awserr.Error)
	if !ok {
		return false
	}

	return awsError.Code() == "OptInRequired"
}

func (r *AccountReconciler) handleIAMUserCreation(reqLogger logr.Logger, currentAcctInstance *awsv1alpha1.Account, awsSetupClient awsclient.Client, namespace string) (reconcile.Result, *sts.AssumeRoleOutput, error) {
	var awsAssumedRoleClient awsclient.Client
	awsAssumedRoleClient, creds, err := r.handleCreateAdminAccessRole(reqLogger, currentAcctInstance, awsSetupClient)
	if err != nil {
		return reconcile.Result{}, nil, err
	}

	// Use the same ID applied to the account name for IAM usernames
	iamUserUHC := fmt.Sprintf("%s-%s", iamUserNameUHC, currentAcctInstance.Labels[awsv1alpha1.IAMUserIDLabel])
	secretName, err := r.BuildIAMUser(reqLogger, awsAssumedRoleClient, currentAcctInstance, iamUserUHC, namespace)
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
		return reconcile.Result{}, nil, err
	}

	currentAcctInstance.Spec.IAMUserSecret = *secretName
	err = r.accountSpecUpdate(reqLogger, currentAcctInstance)
	if err != nil {
		reqLogger.Error(err, "Error updating Secret Ref in Account CR")
		return reconcile.Result{}, nil, err
	}
	reqLogger.Info("IAM User created and saved", "user", iamUserUHC)
	return reconcile.Result{}, creds, nil
}

func (r *AccountReconciler) handleAWSClientError(reqLogger logr.Logger, currentAcctInstance *awsv1alpha1.Account, err error) (reconcile.Result, error) {
	// Get custom failure reason to update account status
	reason := ""
	if aerr, ok := err.(awserr.Error); ok {
		reason = aerr.Code()
	}
	errMsg := fmt.Sprintf("Failed to create STS Credentials for account ID %s: %s", currentAcctInstance.Spec.AwsAccountID, err)
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

	return reconcile.Result{}, err
}

func (r *AccountReconciler) handleAccountInitializingRegions(reqLogger logr.Logger, currentAcctInstance *awsv1alpha1.Account) (reconcile.Result, error) {
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
		utils.SetAccountStatus(currentAcctInstance, msg, awsv1alpha1.AccountCreating, AccountCreating)
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

func (r *AccountReconciler) HandleNonCCSPendingVerification(reqLogger logr.Logger, currentAcctInstance *awsv1alpha1.Account, awsSetupClient awsclient.Client) (reconcile.Result, error) {
	// If the supportCaseID is blank and Account State = PendingVerification, create a case
	if currentAcctInstance.Spec.BYOC {
		err := errors.New("account is BYOC - should not be handled in NonCCS method")
		reqLogger.Error(err, "a BYOC account passed to non-CCS function", "account", currentAcctInstance.Name)
		return reconcile.Result{}, err
	}
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
			err = SetCurrentAccountServiceQuotas(reqLogger, r.awsClientBuilder, awsSetupClient, currentAcctInstance, r.Client)
			if err != nil {
				reqLogger.Error(err, "failed to set account service quotas")
				return reconcile.Result{}, err
			}

			err = r.statusUpdate(currentAcctInstance)
			if err != nil {
				reqLogger.Error(err, "failed to update account state, retrying", "desired state", AccountPendingVerification)
				return reconcile.Result{}, err
			}

			// After creating the support case or increasing quotas requeue the request. To avoid flooding
			// and being blacklisted by AWS when starting the operator with a large AccountPool, add a
			// randomInterval (between 0 and 30 secs) to the regular wait time
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

	var supportCaseResolved bool
	switch utils.DetectDevMode {
	case utils.DevModeProduction:
		resolvedScoped, err := checkCaseResolution(reqLogger, currentAcctInstance.Status.SupportCaseID, awsSetupClient)
		if err != nil {
			reqLogger.Error(err, "Error checking for Case Resolution")
			return reconcile.Result{}, err
		}
		supportCaseResolved = resolvedScoped
	default:
		log.Info("Running in development mode, Skipping case resolution check")
		supportCaseResolved = true
	}

	if currentAcctInstance.HasOpenQuotaIncreaseRequests() {
		switch utils.DetectDevMode {
		case utils.DevModeProduction:
			return GetServiceQuotaRequest(reqLogger, r.awsClientBuilder, awsSetupClient, currentAcctInstance, r.Client)
		}
	}

	openCaseCount, _ := currentAcctInstance.GetQuotaRequestsByStatus(awsv1alpha1.ServiceRequestInProgress)
	// Case Resolved and quota increases are all done: account is Ready
	if supportCaseResolved && openCaseCount == 0 {
		reqLogger.Info("case and quota increases resolved", "caseID", currentAcctInstance.Status.SupportCaseID)
		utils.SetAccountStatus(currentAcctInstance, "Account ready to be claimed", awsv1alpha1.AccountReady, AccountReady)
		_ = r.statusUpdate(currentAcctInstance)
		return reconcile.Result{}, nil
	}

	// Case not Resolved, log info and try again in pre-defined interval
	if !supportCaseResolved {
		reqLogger.Info("case not yet resolved, retrying", "caseID", currentAcctInstance.Status.SupportCaseID, "retry delay", intervalBetweenChecksMinutes)
	}

	return reconcile.Result{RequeueAfter: intervalBetweenChecksMinutes * time.Minute}, nil
}

// This function takes any service quotas defined in the account CR spec and builds them out in the status. The struct for the service quoats in spec and status will differ
// as the spec uses a 'default' region to reduce configuation complexity, whereas the status lists all regions and their service quoata values as it's easier to iterate over.
func SetCurrentAccountServiceQuotas(reqLogger logr.Logger, awsClientBuilder awsclient.IBuilder, awsSetupClient awsclient.Client, currentAcctInstance *awsv1alpha1.Account, client client.Client) error {

	// If standard account, return early
	if currentAcctInstance.Spec.RegionalServiceQuotas == nil {
		return nil
	}

	var defaultAccountServiceQuotas awsv1alpha1.AccountServiceQuota
	var ok bool
	if defaultAccountServiceQuotas, ok = currentAcctInstance.Spec.RegionalServiceQuotas["default"]; !ok {
		err := fmt.Errorf("could not find default key in RegionalServiceQuotas for Account")
		reqLogger.Error(err, "Could not find default key in RegionalServiceQuotas for Account")
		return err
	}

	// Need to assume role into the cluster account
	roleToAssume := currentAcctInstance.GetAssumeRole()
	awsAssumedRoleClient, _, err := AssumeRoleAndCreateClient(reqLogger, awsClientBuilder, currentAcctInstance, client, awsSetupClient, "", roleToAssume, "")
	if err != nil {
		reqLogger.Error(err, "Could not impersonate AWS account", "aws-account", currentAcctInstance.Spec.AwsAccountID)
		return err
	}

	// Get a list of regions enabled in the current account
	regionsEnabledInAccount, err := awsAssumedRoleClient.DescribeRegions(&ec2.DescribeRegionsInput{
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

	currentAcctInstance.Status.RegionalServiceQuotas = make(awsv1alpha1.RegionalServiceQuotas)
	// By iterating over the regions returned by AWS as opposed to what's in the Account CR Spec, we
	// won't set the SQ for a region the account doesn't support by mistake.
	for _, region := range regionsEnabledInAccount.Regions {
		// Take the default service quota values and apply to all regions - save to CR status
		currentAcctInstance.Status.RegionalServiceQuotas[*region.RegionName] = defaultAccountServiceQuotas

		// If we've specified another value for a specific region, set it in the status.
		if currentAcctInstance.Spec.RegionalServiceQuotas[*region.RegionName] != nil {
			// For each value in the spec, set it in the status
			for k, v := range currentAcctInstance.Spec.RegionalServiceQuotas[*region.RegionName] {
				currentAcctInstance.Status.RegionalServiceQuotas[*region.RegionName][k] = v
			}
		}
	}

	// Blanket setting all status values to TODO.
	for _, quota := range currentAcctInstance.Status.RegionalServiceQuotas {
		for k := range quota {
			quota[k].Status = awsv1alpha1.ServiceRequestTodo
		}
	}
	return nil
}

func (r *AccountReconciler) finalizeAccount(reqLogger logr.Logger, awsClient awsclient.Client, account *awsv1alpha1.Account) {
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

func (r *AccountReconciler) accountSpecUpdate(reqLogger logr.Logger, account *awsv1alpha1.Account) error {
	err := r.Update(context.TODO(), account)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Account spec update for %s failed", account.Name))
	}
	return err
}

func (r *AccountReconciler) nonCCSAssignAccountID(reqLogger logr.Logger, currentAcctInstance *awsv1alpha1.Account, awsSetupClient awsclient.Client, complianceTags map[string]string) error {
	// Build Aws Account
	var awsAccountID string

	switch utils.DetectDevMode {
	case utils.DevModeProduction:
		var err error
		awsAccountID, err = r.BuildAccount(reqLogger, awsSetupClient, currentAcctInstance)
		if err != nil {
			return err
		}
	default:
		log.Info("Running in development mode, skipping account creation")
		awsAccountID = "123456789012"
	}

	// set state creating if the account was able to create
	utils.SetAccountStatus(currentAcctInstance, AccountCreating, awsv1alpha1.AccountCreating, AccountCreating)
	err := r.statusUpdate(currentAcctInstance)

	if err != nil {
		return err
	}

	if utils.DetectDevMode != utils.DevModeProduction {
		log.Info("Running in development mode, manually creating a case ID number: 11111111")
		currentAcctInstance.Status.SupportCaseID = "11111111"
	}

	// update account cr with awsAccountID from aws
	currentAcctInstance.Spec.AwsAccountID = awsAccountID

	// tag account with hive shard name and compliance tags
	err = TagAccount(awsSetupClient, awsAccountID, r.shardName, complianceTags)
	if err != nil {
		reqLogger.Info("Unable to tag aws account.", "account", currentAcctInstance.Name, "AWSAccountID", awsAccountID, "Error", error.Error(err))
	}

	return r.accountSpecUpdate(reqLogger, currentAcctInstance)
}

func TagAccount(awsSetupClient awsclient.Client, awsAccountID string, shardName string, complianceTags map[string]string) error {
	// Start with the owner tag
	tags := []*organizations.Tag{
		{
			Key:   aws.String("owner"),
			Value: aws.String(shardName),
		},
	}

	// Add compliance tags from the map
	for key, value := range complianceTags {
		tags = append(tags, &organizations.Tag{
			Key:   aws.String(key),
			Value: aws.String(value),
		})
	}

	inputTag := &organizations.TagResourceInput{
		ResourceId: aws.String(awsAccountID),
		Tags:       tags,
	}

	_, err := awsSetupClient.TagResource(inputTag)
	if err != nil {
		return err
	}

	return nil
}

func (r *AccountReconciler) initializeRegions(reqLogger logr.Logger, currentAcctInstance *awsv1alpha1.Account, creds *sts.AssumeRoleOutput, amiOwner string) error {
	awsRegion := config.GetDefaultRegion()
	// Instantiate a client with a default region to retrieve regions we want to initialize
	awsClient, err := r.awsClientBuilder.GetClient(controllerName, r.Client, awsclient.NewAwsClientInput{
		AwsCredsSecretIDKey:     *creds.Credentials.AccessKeyId,
		AwsCredsSecretAccessKey: *creds.Credentials.SecretAccessKey,
		AwsToken:                *creds.Credentials.SessionToken,
		AwsRegion:               awsRegion,
	})
	if err != nil {
		connErr := fmt.Sprintf("unable to connect to default region %s", awsRegion)
		reqLogger.Error(err, connErr)
		return err
	}

	reqLogger.Info("Created AWS Client for region initialization")

	// Get a list of regions enabled in the current account
	regionsEnabledInAccount, err := awsClient.DescribeRegions(&ec2.DescribeRegionsInput{
		AllRegions: aws.Bool(false),
	})
	if err != nil {
		reqLogger.Error(err, "Failed to retrieve list of regions enabled in this account.")
		return err
	}

	reqLogger.Info("Setting account status to Initializing Regions")
	// We're about to kick off region init in a goroutine. This status makes subsequent
	// Reconciles ignore the Account (unless it stays in this state for too long).
	utils.SetAccountStatus(currentAcctInstance, "Initializing Regions", awsv1alpha1.AccountInitializingRegions, AccountInitializingRegions)
	if err := r.statusUpdate(currentAcctInstance); err != nil {
		reqLogger.Error(err, "Could not update status to Initializing Regions")
		return err
	}

	reqLogger.Info("Initializing Regions")

	// For accounts created by the accountpool we want to ensure we initiate all regions
	if !currentAcctInstance.IsBYOC() {
		go r.asyncRegionInit(reqLogger, currentAcctInstance, creds, amiOwner, castAWSRegionType(regionsEnabledInAccount.Regions))
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
	go r.asyncRegionInit(reqLogger, currentAcctInstance, creds, amiOwner, accountClaim.Spec.Aws.Regions)

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
func (r *AccountReconciler) asyncRegionInit(reqLogger logr.Logger, currentAcctInstance *awsv1alpha1.Account, creds *sts.AssumeRoleOutput, amiOwner string, regionsEnabledInAccount []awsv1alpha1.AwsRegions) {

	// Initialize all supported regions by creating and terminating an instance in each
	r.InitializeSupportedRegions(reqLogger, currentAcctInstance, regionsEnabledInAccount, creds, amiOwner)

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
func (r *AccountReconciler) BuildAccount(reqLogger logr.Logger, awsClient awsclient.Client, account *awsv1alpha1.Account) (string, error) {
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

	accountObjectKey := client.ObjectKeyFromObject(account)
	err := r.Get(context.TODO(), accountObjectKey, account)
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
		errMsg := "Error creating account"
		var returnErr error
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case organizations.ErrCodeConcurrentModificationException:
				returnErr = awsv1alpha1.ErrAwsConcurrentModification
			case organizations.ErrCodeConstraintViolationException:
				returnErr = awsv1alpha1.ErrAwsAccountLimitExceeded
			case organizations.ErrCodeServiceException:
				returnErr = awsv1alpha1.ErrAwsInternalFailure
			case organizations.ErrCodeTooManyRequestsException:
				returnErr = awsv1alpha1.ErrAwsTooManyRequests
			default:
				returnErr = awsv1alpha1.ErrAwsFailedCreateAccount
			}

		}
		utils.LogAwsError(reqLogger, errMsg, returnErr, err)
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

func ClaimAccount(r *AccountReconciler, currentAcctInstance *awsv1alpha1.Account) error {
	currentAcctInstance.Status.Claimed = true
	msg := fmt.Sprintf("Account %s was claimed: %s (Namespace: %s)",
		currentAcctInstance.Name,
		currentAcctInstance.Spec.ClaimLink,
		currentAcctInstance.Spec.ClaimLinkNamespace)
	currentAcctInstance.Status.Conditions = utils.SetAccountCondition(
		currentAcctInstance.Status.Conditions,
		awsv1alpha1.AccountConditionType(awsv1alpha1.AccountIsClaimed),
		// Switch the Condition off
		corev1.ConditionTrue,
		AccountInitializingRegions,
		msg,
		// Make sure the existing condition is updated
		utils.UpdateConditionAlways,
		currentAcctInstance.Spec.BYOC)
	return r.statusUpdate(currentAcctInstance)
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

func (r *AccountReconciler) statusUpdate(account *awsv1alpha1.Account) error {
	err := r.Client.Status().Update(context.TODO(), account)
	return err
}

func (r *AccountReconciler) setAccountFailed(reqLogger logr.Logger, account *awsv1alpha1.Account, ctype awsv1alpha1.AccountConditionType, reason string, message string, state string) (reconcile.Result, error) {
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

func (r *AccountReconciler) getAccountClaim(account *awsv1alpha1.Account) (*awsv1alpha1.AccountClaim, error) {
	accountClaim := &awsv1alpha1.AccountClaim{}
	err := r.Get(context.TODO(), types.NamespacedName{
		Name: account.Spec.ClaimLink, Namespace: account.Spec.ClaimLinkNamespace}, accountClaim)

	if err != nil {
		return nil, err
	}
	return accountClaim, nil
}

func (r *AccountReconciler) accountClaimError(reqLogger logr.Logger, account *awsv1alpha1.Account, reason string, message string) error {
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

func (r *AccountReconciler) failAllAccountClaimStatus(accountClaim *awsv1alpha1.AccountClaim) *awsv1alpha1.AccountClaim {
	for _, condition := range accountClaim.Status.Conditions {
		condition.Status = corev1.ConditionFalse
	}
	return accountClaim
}

func (r *AccountReconciler) setAccountClaimError(reqLogger logr.Logger, currentAccountInstance *awsv1alpha1.Account, message string) error {
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

// getManagedTags retrieves a list of managed tags from the configmap
// returns an empty list on any failure.
func (r *AccountReconciler) getManagedTags(log logr.Logger) []awsclient.AWSTag {
	tags := []awsclient.AWSTag{}

	cm := &corev1.ConfigMap{}
	err := r.Get(context.TODO(), types.NamespacedName{Namespace: awsv1alpha1.AccountCrNamespace, Name: awsv1alpha1.DefaultConfigMap}, cm)
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

// getCustomTags retrieves a list of tags from the linked accountclaim
// these tags can be tags specified by the customer or set by other pieces of the OSD stack
func (r *AccountReconciler) getCustomTags(log logr.Logger, account *awsv1alpha1.Account) []awsclient.AWSTag {
	tags := []awsclient.AWSTag{}

	accountClaim, err := r.getAccountClaim(account)
	if err != nil {
		// We expect this error for non-ccs accounts
		if account.IsBYOC() {
			log.Error(err, "Error getting AccountClaim to get custom tags")
		}
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
//
//	key=value
//	my-tag=true
//	base64-is-accepted=eWVzIQ==
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

func castAWSRegionType(regions []*ec2.Region) []awsv1alpha1.AwsRegions {
	var awsRegions []awsv1alpha1.AwsRegions
	for _, region := range regions {
		awsRegions = append(awsRegions, awsv1alpha1.AwsRegions{Name: *region.RegionName})
	}
	return awsRegions
}

func (r *AccountReconciler) handleCreateAdminAccessRole(
	reqLogger logr.Logger,
	currentAcctInstance *awsv1alpha1.Account,
	awsSetupClient awsclient.Client) (awsclient.Client, *sts.AssumeRoleOutput, error) {

	var err error
	var awsAssumedRoleClient awsclient.Client
	var creds *sts.AssumeRoleOutput
	currentAccInstanceID := currentAcctInstance.Labels[awsv1alpha1.IAMUserIDLabel]
	roleToAssume := currentAcctInstance.GetAssumeRole()

	adminAccessArn := config.GetIAMArn("aws", config.AwsResourceTypePolicy, config.AwsResourceIDAdministratorAccessRole)

	// Build the tags required to create the Admin Access Role
	tags := awsclient.AWSTags.BuildTags(
		currentAcctInstance,
		r.getManagedTags(reqLogger),
		r.getCustomTags(reqLogger, currentAcctInstance),
	).GetIAMTags()

	// In this block we are creating the ManagedOpenShift-Support-XYZ for both CCS and non-CCS accounts.
	// The dependency on the roleID validation within the handleRoleAssumption, and the different aws clients
	// required between CCS and non-CCS, is what has caused these steps to be done independently.
	if currentAcctInstance.Spec.BYOC {
		// The CCS uses the CCS client for creating the ManagedOpenShift-Support and then utilizes the RoleID
		// generated from that in the handleRoleAssumption func for role validation

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

		roleID, err := r.createManagedOpenShiftSupportRole(
			reqLogger,
			awsSetupClient,
			ccsClient,
			adminAccessArn,
			currentAccInstanceID,
			tags,
		)

		if err != nil {
			reqLogger.Error(err, "Encountered error while creating ManagedOpenShiftSupportRole for CCS Account", "roleID", roleID)
			return nil, nil, err
		}

		awsAssumedRoleClient, creds, err = AssumeRoleAndCreateClient(reqLogger, r.awsClientBuilder, currentAcctInstance, r.Client, awsSetupClient, "", roleToAssume, roleID)
		if err != nil {
			return nil, nil, err
		}

	} else {
		// Unlike the CCS block, the non-CCS block does not have a dependency on the RoleID to handleRoleAssumption. The
		// awsAssumedRoleClient is what is needed to create the ManagedOpenShift-Support in the non-CCS account.
		awsAssumedRoleClient, creds, err = AssumeRoleAndCreateClient(reqLogger, r.awsClientBuilder, currentAcctInstance, r.Client, awsSetupClient, "", roleToAssume, "")
		if err != nil {
			return nil, nil, err
		}

		roleID, err := r.createManagedOpenShiftSupportRole(
			reqLogger,
			awsSetupClient,
			awsAssumedRoleClient,
			adminAccessArn,
			currentAccInstanceID,
			tags,
		)

		if err != nil {
			reqLogger.Error(err, "Encountered error while creating ManagedOpenShiftSupportRole for non-CCS Account", "roleID", roleID)
			return nil, nil, err
		}
	}

	return awsAssumedRoleClient, creds, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *AccountReconciler) SetupWithManager(mgr ctrl.Manager) error {

	r.awsClientBuilder = &awsclient.Builder{}

	maxReconciles, err := utils.GetControllerMaxReconciles(controllerName)
	if err != nil {
		log.Error(err, "missing max reconciles for controller", "controller", controllerName)
	}

	// AlexVulaj: We're seeing errors here on startup during local testing, we may need to move this to later in the startup process
	// ERROR   controller_account      failed retrieving configmap     {"error": "the cache is not started, can not read objects"}
	configMap, err := utils.GetOperatorConfigMap(r.Client)
	if err != nil {
		log.Error(err, "failed retrieving configmap")
	}

	hiveName, ok := configMap.Data["shard-name"]
	if !ok {
		log.Error(err, "shard-name key not available in configmap")
	}
	r.shardName = hiveName

	rwm := utils.NewReconcilerWithMetrics(r, controllerName)
	return ctrl.NewControllerManagedBy(mgr).
		For(&awsv1alpha1.Account{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxReconciles,
		}).Complete(rwm)
}
