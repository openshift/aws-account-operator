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
	"github.com/openshift/aws-account-operator/pkg/localmetrics"
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

	// Service Quota-related constants
	// vCPUQuotaCode
	vCPUQuotaCode = "L-1216C47A"
	// vCPUServiceCode
	vCPUServiceCode = "ec2"

	// createPendTime is the maximum time we allow an Account to sit in Creating state before we
	// time out and set it to Failed.
	createPendTime = utils.WaitTime * time.Minute
	// initPendTime is the maximum time we allow between Account creation and the end of region
	// initialization.
	// NOTE(efried): We use quite a long timeout here. Within reason, we would rather go too
	// long than too short.
	// - We want to give region init every opportunity to finish. We ignore the Account anyway,
	//   regardless of whether it's failed or initializing; but if we set it to Failed too soon,
	//   and the goroutine actually finishes, it will (*should*) bounce off of the
	//   resourceVersion.
	// - The async region init takes a theoretical maximum of WaitTime * 2 minutes plus a handful
	//   of AWS API calls. See asyncRegionInit.
	// - Ideally it would be nice to start timing from right before we kicked off the region
	//   init, but for simplicity we just use the Account's creation time. So we add the
	//   maximum time it could take in the Creating state, which is WaitTime minutes (see
	//   accountCreatingTooLong).
	// - We add an extra WaitTime for other miscellaneous steps that are otherwise not
	//   accounted for, hopefully with plenty of buffer.
	initPendTime = utils.WaitTime * time.Minute * time.Duration(4)

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
	return &ReconcileAccount{
		Client:           utils.NewClientWithMetricsOrDie(log, mgr, controllerName),
		scheme:           mgr.GetScheme(),
		awsClientBuilder: &awsclient.Builder{},
	}
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
	start := time.Now()
	reqLogger := log.WithValues("Controller", controllerName, "Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling")

	defer func() {
		dur := time.Since(start)
		localmetrics.Collector.SetReconcileDuration(controllerName, dur.Seconds())
		reqLogger.WithValues("Duration", dur).Info("Reconcile complete")
	}()

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
	if accountIsBYOCPendingDeletionWithFinalizer(currentAcctInstance) {
		// Remove finalizer to unlock deletion of the accountClaim
		err = r.removeFinalizer(reqLogger, currentAcctInstance, awsv1alpha1.AccountFinalizer)
		if err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	// Log accounts that have failed and don't attempt to reconcile them
	if accountIsFailed(currentAcctInstance) {
		reqLogger.Info(fmt.Sprintf("Account %s is failed. Ignoring.", currentAcctInstance.Name))
		return reconcile.Result{}, nil
	}

	// Detect accounts for which we kicked off asynchronous region initialization
	if accountIsInitializingRegions(currentAcctInstance) {
		// Detect whether we set the InitializingRegions condition in *this* invocation of the
		// operator or a previous one.
		if regionInitStale(currentAcctInstance) {
			// This means the region init goroutine(s) for this account were still running when an
			// earlier invocation of the operator died. We want to recover those, so set them back
			// to Creating, which should cause us to hit the region init code path again.
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
		if accountOlderThan(currentAcctInstance, initPendTime) {
			errMsg := fmt.Sprintf("Initializing regions for longer than %d seconds", initPendTime)
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
		reqLogger.Error(err, "Failed to get AWS client")
		return reconcile.Result{}, err
	}
	var byocRoleID string

	// If the account is BYOC, needs some different set up
	if newBYOCAccount(currentAcctInstance) {
		byocRoleID, err = r.initializeNewBYOCAccount(reqLogger, currentAcctInstance, awsSetupClient, adminAccessArn)
		if err != nil || byocRoleID == "" {
			r.setStateFailed(reqLogger, currentAcctInstance, err.Error())
			reqLogger.Error(err, "Failed setting up new BYOC account")
			return reconcile.Result{}, err
		}
	} else {
		// Normal account creation

		// Test PendingVerification state creating support case and checking for case status
		if accountIsPendingVerification(currentAcctInstance) {

			// If the supportCaseID is blank and Account State = PendingVerification, create a case
			if !accountHasSupportCaseID(currentAcctInstance) {
				switch utils.DetectDevMode {
				case utils.DevModeProduction:
					caseID, err := createCase(reqLogger, currentAcctInstance, awsSetupClient)
					if err != nil {
						return reconcile.Result{}, err
					}
					reqLogger.Info("Case created", "CaseID", caseID)

					// Update supportCaseId in CR
					currentAcctInstance.Status.SupportCaseID = caseID
					utils.SetAccountStatus(currentAcctInstance, "Account pending verification in AWS", awsv1alpha1.AccountPendingVerification, AccountPendingVerification)
					err = r.statusUpdate(currentAcctInstance)
					if err != nil {
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
				reqLogger.Info(fmt.Sprintf("Case %s resolved", currentAcctInstance.Status.SupportCaseID))

				utils.SetAccountStatus(currentAcctInstance, "Account ready to be claimed", awsv1alpha1.AccountReady, AccountReady)
				err = r.statusUpdate(currentAcctInstance)
				if err != nil {
					return reconcile.Result{}, err
				}
				return reconcile.Result{}, nil
			}

			// Case not Resolved, log info and try again in pre-defined interval
			reqLogger.Info(fmt.Sprintf(`Case %s not resolved,
			trying again in %d minutes`,
				currentAcctInstance.Status.SupportCaseID,
				intervalBetweenChecksMinutes))
			return reconcile.Result{RequeueAfter: intervalBetweenChecksMinutes * time.Minute}, nil
		}

		// Update account Status.Claimed to true if the account is ready and the claim link is not empty
		if accountIsReadyUnclaimedAndHasClaimLink(currentAcctInstance) {
			currentAcctInstance.Status.Claimed = true
			return reconcile.Result{}, r.statusUpdate(currentAcctInstance)
		}

		// see if in creating for longer then default wait time
		if accountCreatingTooLong(currentAcctInstance) {
			errMsg := fmt.Sprintf("Creation pending for longer then %d minutes", utils.WaitTime)
			r.setStateFailed(reqLogger, currentAcctInstance, errMsg)
		}

		if accountIsUnclaimedAndHasNoState(currentAcctInstance) {
			// Initialize the awsAccountID var here since we only use it now inside this condition
			var awsAccountID string

			if !accountHasAwsAccountID(currentAcctInstance) {
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
				utils.SetAccountStatus(currentAcctInstance, "Attempting to create account", awsv1alpha1.AccountCreating, AccountCreating)
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
	if accountReadyForInitialization(currentAcctInstance) {

		reqLogger.Info(fmt.Sprintf("Initalizing account: %s AWS ID: %s", currentAcctInstance.Name, currentAcctInstance.Spec.AwsAccountID))
		var roleToAssume string
		var iamUserUHC = iamUserNameUHC
		var iamUserSRE = iamUserNameSRE

		if accountIsBYOC(currentAcctInstance) {
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
			match, _ := matchSubstring(byocRoleID, *creds.AssumedRoleUser.AssumedRoleId)
			if byocRoleID != "" && match == false {
				reqLogger.Info(fmt.Sprintf("Assumed RoleID:Session string does not match new RoleID: %s, %s", *creds.AssumedRoleUser.AssumedRoleId, byocRoleID))
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

	if accountIsBYOC(currentAcctInstance) {
		utils.SetAccountStatus(currentAcctInstance, "BYOC Account Ready", awsv1alpha1.AccountReady, AccountReady)

	} else {
		if utils.FindAccountCondition(currentAcctInstance.Status.Conditions, awsv1alpha1.AccountReady) != nil {
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

	reqLogger.Info("Account Created")

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
		reqLogger.Error(err, fmt.Sprintf("Unable to get accountClaim for %s", currentAccountInstance.Name))
		return err
	}

	var reason string
	var conditionType awsv1alpha1.AccountClaimConditionType

	if accountIsBYOC(currentAccountInstance) {
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
		reqLogger.Error(err, fmt.Sprintf("Status update for %s failed", accountClaim.Name))
	}

	return err
}

func matchSubstring(roleID, role string) (bool, error) {
	matched, err := regexp.MatchString(roleID, role)
	return matched, err
}

// getConfigDataByKey gets the desired key from the default configmap or returns an empty string and error
func getConfigDataByKey(kubeClient client.Client, key string) (string, error) {
	configMap := &corev1.ConfigMap{}
	err := kubeClient.Get(
		context.TODO(),
		types.NamespacedName{Namespace: awsv1alpha1.AccountCrNamespace,
			Name: awsv1alpha1.DefaultConfigMap}, configMap)
	if err != nil {
		return "", err
	}

	value, ok := configMap.Data[key]
	if !ok {
		return "", awsv1alpha1.ErrInvalidConfigMap
	}

	return value, nil
}

// Returns true if account CR is Failed
func accountIsFailed(currentAcctInstance *awsv1alpha1.Account) bool {
	return currentAcctInstance.Status.State == AccountFailed
}

// Returns true of there is no state set
func accountHasState(currentAcctInstance *awsv1alpha1.Account) bool {
	return currentAcctInstance.Status.State != ""
}

// Returns true of there is no Status.SupportCaseID set
func accountHasSupportCaseID(currentAcctInstance *awsv1alpha1.Account) bool {
	return currentAcctInstance.Status.SupportCaseID != ""
}

func accountIsPendingVerification(currentAcctInstance *awsv1alpha1.Account) bool {
	return currentAcctInstance.Status.State == AccountPendingVerification
}

func accountIsReady(currentAcctInstance *awsv1alpha1.Account) bool {
	return currentAcctInstance.Status.State == AccountReady
}

func accountIsCreating(currentAcctInstance *awsv1alpha1.Account) bool {
	return currentAcctInstance.Status.State == AccountCreating
}

func accountIsInitializingRegions(currentAcctInstance *awsv1alpha1.Account) bool {
	return currentAcctInstance.Status.State == AccountInitializingRegions
}

func regionInitStale(currentAcctInstance *awsv1alpha1.Account) bool {
	cond := utils.FindAccountCondition(currentAcctInstance.Status.Conditions, awsv1alpha1.AccountInitializingRegions)
	if cond == nil {
		// Assuming the caller checks accountIsInitializingRegions beforehand, this really should
		// never happen.
		return false
	}
	return cond.LastTransitionTime.Before(utils.GetOperatorStartTime())
}

func accountHasClaimLink(currentAcctInstance *awsv1alpha1.Account) bool {
	return currentAcctInstance.Spec.ClaimLink != ""
}

func accountOlderThan(currentAcctInstance *awsv1alpha1.Account, minutes time.Duration) bool {
	return time.Now().Sub(currentAcctInstance.GetCreationTimestamp().Time) > minutes
}

func accountCreatingTooLong(currentAcctInstance *awsv1alpha1.Account) bool {
	return accountIsCreating(currentAcctInstance) && accountOlderThan(currentAcctInstance, createPendTime)
}

// Returns true if account Status.Claimed is false
func accountIsClaimed(currentAcctInstance *awsv1alpha1.Account) bool {
	return currentAcctInstance.Status.Claimed
}

// Returns true if a DeletionTimestamp has been set
func accountIsPendingDeletion(currentAcctInstance *awsv1alpha1.Account) bool {
	return currentAcctInstance.DeletionTimestamp != nil
}

// Returns true of Spec.BYOC is true, ie: account is a BYOC account
func accountIsBYOC(currentAcctInstance *awsv1alpha1.Account) bool {
	return currentAcctInstance.Spec.BYOC
}

// Returns true if the awsv1alpha1 finalizer is set on the account
func accountHasAwsv1alpha1Finalizer(currentAcctInstance *awsv1alpha1.Account) bool {
	return utils.Contains(currentAcctInstance.GetFinalizers(), awsv1alpha1.AccountFinalizer)
}

// Returns true if awsAccountID is set
func accountHasAwsAccountID(currentAcctInstance *awsv1alpha1.Account) bool {
	return currentAcctInstance.Spec.AwsAccountID != ""
}

func accountIsReadyUnclaimedAndHasClaimLink(currentAcctInstance *awsv1alpha1.Account) bool {
	return accountIsReady(currentAcctInstance) &&
		accountHasClaimLink(currentAcctInstance) &&
		!accountIsClaimed(currentAcctInstance)
}

// Returns true if account is a BYOC Account, has been marked for deletion (deletion
// timestamp set), and has a finalizer set.
func accountIsBYOCPendingDeletionWithFinalizer(currentAcctInstance *awsv1alpha1.Account) bool {
	return accountIsPendingDeletion(currentAcctInstance) &&
		accountIsBYOC(currentAcctInstance) &&
		accountHasAwsv1alpha1Finalizer(currentAcctInstance)
}

// Returns true if account is BYOC and the state is not AccountReady
func accountIsBYOCAndNotReady(currentAcctInstance *awsv1alpha1.Account) bool {
	return accountIsBYOC(currentAcctInstance) && !accountIsReady(currentAcctInstance)
}

// Returns true if account is a BYOC Account and the state is not ready OR
// accout state is creating, and has not been claimed
func accountReadyForInitialization(currentAcctInstance *awsv1alpha1.Account) bool {
	return accountIsBYOCAndNotReady(currentAcctInstance) ||
		accountIsUnclaimedAndIsCreating(currentAcctInstance)
}

// Returns true if account has not set state and has not been claimed
func accountIsUnclaimedAndHasNoState(currentAcctInstance *awsv1alpha1.Account) bool {
	return !accountHasState(currentAcctInstance) &&
		!accountIsClaimed(currentAcctInstance)
}

// Return true if account state is AccountCreating and has not been claimed
func accountIsUnclaimedAndIsCreating(currentAcctInstance *awsv1alpha1.Account) bool {
	return accountIsCreating(currentAcctInstance) &&
		!accountIsClaimed(currentAcctInstance)
}

type accountInstance interface {
	accountIsUnclaimedAndIsCreating() bool
}
