package account

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/organizations"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/go-logr/logr"
	"github.com/openshift/aws-account-operator/pkg/controller/utils"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	kubeclientpkg "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	totalaccountwatcher "github.com/openshift/aws-account-operator/pkg/totalaccountwatcher"
)

var log = logf.Log.WithName("controller_account")

const (
	// AwsLimit tracks the hard limit to the number of accounts; exported for use in cmd/manager/main.go
	awsCredsUserName        = "aws_user_name"
	awsCredsSecretIDKey     = "aws_access_key_id"
	awsCredsSecretAccessKey = "aws_secret_access_key"
	awsAMI                  = "ami-000db10762d0c4c05"
	awsInstanceType         = "t2.micro"
	// createPendTime is the maximum time we allow an Account to sit in Creating state before we
	// time out and set it to Failed.
	createPendTime = utils.WaitTime * time.Minute
	// regionInitTime is the maximum time we allow an account CR to be in the InitializingRegions
	// state. This is based on async region init taking a theoretical maximum of WaitTime * 2
	// minutes plus a handful of AWS API calls (see asyncRegionInit).
	regionInitTime = (utils.WaitTime * time.Duration(2)) + time.Minute

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
	iamUserNameSRE = "osdManagedAdminSRE"

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
	Client           kubeclientpkg.Client
	scheme           *runtime.Scheme
	awsClientBuilder awsclient.IBuilder
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

	// Remove finalizer if account CR is BYOC as the accountclaim controller will delete the account CR
	// when the accountClaim CR is deleted as its set as the owner reference
	if currentAcctInstance.IsBYOCPendingDeletionWithFinalizer() {
		reqLogger.Info("removing account finalizer")
		err = r.removeFinalizer(currentAcctInstance, awsv1alpha1.AccountFinalizer)
		if err != nil {
			reqLogger.Error(err, "failed removing account finalizer")
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	// Log accounts that have failed and don't attempt to reconcile them
	if currentAcctInstance.IsFailed() {
		reqLogger.Info(fmt.Sprintf("Account %s is failed. Ignoring.", currentAcctInstance.Name))
		return reconcile.Result{}, nil
	}

	// Detect accounts for which we kicked off asynchronous region initialization
	if currentAcctInstance.IsInitializingRegions() {
		irCond := currentAcctInstance.GetCondition(awsv1alpha1.AccountInitializingRegions)
		if irCond == nil {
			// This should never happen: the thing that made IsInitializingRegions true
			// also added the Condition.
			errMsg := "Unexpectedly couldn't find the InitializingRegions Condition"
			return reconcile.Result{}, r.setStateFailed(reqLogger, currentAcctInstance, errMsg)
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
			return reconcile.Result{}, r.setStateFailed(reqLogger, currentAcctInstance, errMsg)
		}
		// Otherwise give it a chance to finish.
		reqLogger.Info(fmt.Sprintf("Account %s is initializing regions. Ignoring.", currentAcctInstance.Name))
		// No need to requeue. If the goroutine finishes, it changes the state, which will trigger
		// a Reconcile. If it hangs forever, we'll eventually get a freebie periodic Reconcile
		// that will hit the timeout condition above.
		return reconcile.Result{}, nil
	}

	// We expect this secret to exist in the same namespace Account CR's are created
	awsSetupClient, err := r.awsClientBuilder.GetClient(controllerName, r.Client, awsclient.NewAwsClientInput{
		SecretName: utils.AwsSecretName,
		NameSpace:  awsv1alpha1.AccountCrNamespace,
		AwsRegion:  "us-east-1",
	})
	if err != nil {
		reqLogger.Error(err, "failed building AWS client")
		return reconcile.Result{}, err
	}
	var ccsRoleID string
	// If the account is BYOC, needs some different set up
	if newBYOCAccount(currentAcctInstance) {
		// Need these to use = below, otherwise ccsRoleID fails syntax check as "unused"
		var result reconcile.Result
		var initErr error

		ccsRoleID, result, initErr = r.initializeNewCCSAccount(reqLogger, currentAcctInstance, awsSetupClient, adminAccessArn)
		if initErr != nil {
			// TODO: If we have recoverable results from above, how do we allow them to requeue if state is failed
			r.setStateFailed(reqLogger, currentAcctInstance, initErr.Error())
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
				err = r.statusUpdate(currentAcctInstance)
				if err != nil {
					return reconcile.Result{}, err
				}
				return reconcile.Result{}, nil
			}

			// Case not Resolved, log info and try again in pre-defined interval
			reqLogger.Info("case not yet resolved, retrying", "caseID", currentAcctInstance.Status.SupportCaseID, "retry delay", intervalBetweenChecksMinutes)
			return reconcile.Result{RequeueAfter: intervalBetweenChecksMinutes * time.Minute}, nil
		}

		// Update account Status.Claimed to true if the account is ready and the claim link is not empty
		if currentAcctInstance.IsReadyUnclaimedAndHasClaimLink() {
			currentAcctInstance.Status.Claimed = true
			return reconcile.Result{}, r.statusUpdate(currentAcctInstance)
		}

		// see if in creating for longer then default wait time
		if currentAcctInstance.IsCreating() && currentAcctInstance.IsOlderThan(createPendTime) {
			errMsg := fmt.Sprintf("Creation pending for longer then %d minutes", utils.WaitTime)
			r.setStateFailed(reqLogger, currentAcctInstance, errMsg)
		}

		if currentAcctInstance.IsUnclaimedAndHasNoState() {
			// Initialize the awsAccountID var here since we only use it now inside this condition
			var awsAccountID string

			if !currentAcctInstance.HasAwsAccountID() {
				// before doing anything make sure we are not over the limit if we are just error
				if !totalaccountwatcher.TotalAccountWatcher.AccountsCanBeCreated() {
					reqLogger.Error(awsv1alpha1.ErrAwsAccountLimitExceeded, "AWS Account limit reached")
					return reconcile.Result{}, awsv1alpha1.ErrAwsAccountLimitExceeded
				}

				// Build Aws Account
				awsAccountID, err = r.BuildAccount(reqLogger, awsSetupClient, currentAcctInstance)
				if err != nil {
					return reconcile.Result{}, err
				}

				// set state creating if the account was able to create
				utils.SetAccountStatus(currentAcctInstance, AccountCreating, awsv1alpha1.AccountCreating, AccountCreating)
				err = r.statusUpdate(currentAcctInstance)

				if err != nil {
					return reconcile.Result{}, err
				}

				if utils.DetectDevMode != utils.DevModeProduction {
					log.Info("Running in development mode, manually creating a case ID number: 11111111")
					currentAcctInstance.Status.SupportCaseID = "11111111"
				}

				if err != nil {
					return reconcile.Result{}, err
				}

				// update account cr with awsAccountID from aws
				currentAcctInstance.Spec.AwsAccountID = awsAccountID
				err = r.Client.Update(context.TODO(), currentAcctInstance)
				if err != nil {
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

	// Account init for both BYOC and Non-BYOC
	if currentAcctInstance.ReadyForInitialization() {

		reqLogger.Info("initializing account", "awsAccountID", currentAcctInstance.Spec.AwsAccountID)
		var roleToAssume string
		var iamUserUHC = iamUserNameUHC
		var iamUserSRE = iamUserNameSRE

		if currentAcctInstance.IsBYOC() {
			// Use the same ID applied to the account name for IAM usernames
			currentAccInstanceID := currentAcctInstance.Labels[fmt.Sprintf("%s", awsv1alpha1.IAMUserIDLabel)]
			iamUserUHC = fmt.Sprintf("%s-%s", iamUserNameUHC, currentAccInstanceID)
			iamUserSRE = fmt.Sprintf("%s-%s", iamUserNameSRE, currentAccInstanceID)
			byocRoleToAssume := fmt.Sprintf("%s-%s", byocRole, currentAccInstanceID)
			roleToAssume = byocRoleToAssume
		} else {
			roleToAssume = awsv1alpha1.AccountOperatorIAMRole
		}

		var creds *sts.AssumeRoleOutput
		var credsErr error

		for i := 0; i < 10; i++ {
			// Get STS credentials so that we can create an aws client with
			creds, credsErr = getStsCredentials(reqLogger, awsSetupClient, roleToAssume, currentAcctInstance.Spec.AwsAccountID)
			if credsErr != nil {
				errMsg := fmt.Sprintf("Failed to create STS Credentials for account ID %s: %s", currentAcctInstance.Spec.AwsAccountID, credsErr)
				r.setStateFailed(reqLogger, currentAcctInstance, errMsg)
				return reconcile.Result{}, credsErr
			}

			// If this is a BYOC account, check that BYOCAdminAccess role
			// was the one used in the AssumedRole
			// RoleID must exist in the AssumeRoleID string
			match, _ := matchSubstring(ccsRoleID, *creds.AssumedRoleUser.AssumedRoleId)
			if ccsRoleID != "" && match == false {
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
			reqLogger.Info(err.Error())
			errMsg := fmt.Sprintf("Failed to assume role: %s", err)
			r.setStateFailed(reqLogger, currentAcctInstance, errMsg)
			return reconcile.Result{}, err
		}

		secretName, err := r.BuildIAMUser(reqLogger, awsAssumedRoleClient, currentAcctInstance, iamUserUHC, request.Namespace)
		if err != nil {
			errMsg := fmt.Sprintf("Failed to build IAM UHC user %s: %s", iamUserUHC, err)
			r.setStateFailed(reqLogger, currentAcctInstance, errMsg)
			return reconcile.Result{}, err
		}
		currentAcctInstance.Spec.IAMUserSecret = *secretName
		err = r.Client.Update(context.TODO(), currentAcctInstance)
		if err != nil {
			return reconcile.Result{}, err
		}

		// Create SRE IAM user, we don't care about the credentials because they're saved inside of the build func
		_, err = r.BuildIAMUser(reqLogger, awsAssumedRoleClient, currentAcctInstance, iamUserSRE, request.Namespace)
		if err != nil {
			errMsg := fmt.Sprintf("Failed to build IAM SRE user %s: %s", iamUserSRE, err)
			r.setStateFailed(reqLogger, currentAcctInstance, errMsg)
			return reconcile.Result{}, err
		}

		// We're about to kick off region init in a goroutine. This status makes subsequent
		// Reconciles ignore the Account (unless it stays in this state for too long).
		utils.SetAccountStatus(currentAcctInstance, "Initializing Regions", awsv1alpha1.AccountInitializingRegions, AccountInitializingRegions)
		if err := r.statusUpdate(currentAcctInstance); err != nil {
			// statusUpdate logs
			return reconcile.Result{}, err
		}

		// This initializes supported regions, and updates Account state when that's done. There is
		// no error checking at this level.
		go r.asyncRegionInit(reqLogger, currentAcctInstance, creds)
	}
	return reconcile.Result{}, nil
}

// asyncRegionInit initializes supported regions by creating and destroying an instance in each.
// Upon completion, it *always* sets the Account status to either Ready or PendingVerification.
// There is no mechanism for this func to report errors to its parent. The only error paths
// currently possible are:
// - The Status update fails.
// - This goroutine dies in some horrible and unpredictable way.
// In either case we would expect the main reconciler to eventually notice that the Account has
// been in the InitializingRegions state for too long, and set it to Failed.
func (r *ReconcileAccount) asyncRegionInit(reqLogger logr.Logger, currentAcctInstance *awsv1alpha1.Account, creds *sts.AssumeRoleOutput) {
	// Initialize all supported regions by creating and terminating an instance in each
	r.InitializeSupportedRegions(reqLogger, currentAcctInstance, awsv1alpha1.CoveredRegions, creds)

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
			utils.SetAccountStatus(account, "Failed to create AWS Account", awsv1alpha1.AccountFailed, AccountFailed)
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

func (r *ReconcileAccount) setStateFailed(reqLogger logr.Logger, currentAcctInstance *awsv1alpha1.Account, msg string) error {
	reqLogger.Info(msg)
	// Upodate the account status (state and condition) to Failed
	utils.SetAccountStatus(currentAcctInstance, msg, v1alpha1.AccountFailed, AccountFailed)

	var err error

	// Set a failure condition in the accountClaim
	err = r.setAccountClaimError(reqLogger, currentAcctInstance, msg)
	if err != nil {
		return err
	}

	// Apply the update
	err = r.statusUpdate(currentAcctInstance)
	if err != nil {
		reqLogger.Error(err, "failed to update status")
	}
	// TODO: Need to cause this to requeue again
	return err
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
