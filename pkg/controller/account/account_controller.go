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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	totalaccountwatcher "github.com/openshift/aws-account-operator/pkg/totalaccountwatcher"
)

var log = logf.Log.WithName("controller_account")

const (
	// AwsLimit tracks the hard limit to the number of accounts; exported for use in cmd/manager/main.go
	AwsLimit                = 4800
	awsCredsUserName        = "aws_user_name"
	awsCredsSecretIDKey     = "aws_access_key_id"
	awsCredsSecretAccessKey = "aws_secret_access_key"
	iamUserNameUHC          = "osdManagedAdmin"
	iamUserNameSRE          = "osdManagedAdminSRE"
	awsAMI                  = "ami-000db10762d0c4c05"
	awsInstanceType         = "t2.micro"
	createPendTime          = utils.WaitTime * time.Minute

	// AccountPending indicates an account is pending
	AccountPending = "Pending"
	// AccountCreating indicates an account is being created
	AccountCreating = "Creating"
	// AccountFailed indicates account creation has failed
	AccountFailed = "Failed"
	// AccountReady indicates account creation is ready
	AccountReady = "Ready"
	// AccountPendingVerification indicates verification (of AWS limits and Enterprise Support) is pending
	AccountPendingVerification = "PendingVerification"
	byocRole                   = "BYOCAdminAccess"

	adminAccessArn = "arn:aws:iam::aws:policy/AdministratorAccess"
)

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new Account Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileAccount{
		Client:           mgr.GetClient(),
		scheme:           mgr.GetScheme(),
		awsClientBuilder: awsclient.NewClient,
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
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	Client           kubeclientpkg.Client
	scheme           *runtime.Scheme
	awsClientBuilder func(awsAccessID, awsAccessSecret, token, region string) (awsclient.Client, error)
}

// secretInput is a struct that holds data required to create a new secret CR
type secretInput struct {
	SecretName, NameSpace, awsCredsUserName, awsCredsSecretIDKey, awsCredsSecretAccessKey string
}

// SRESecretInput is a struct that holds data required to create a new secret CR for SRE admins
type SRESecretInput struct {
	SecretName, NameSpace, awsCredsSecretIDKey, awsCredsSecretAccessKey, awsCredsSessionToken, awsCredsConsoleLoginURL string
}

// SREConsoleInput is a struct that holds data required to create a new secret CR for SRE admins
type SREConsoleInput struct {
	SecretName, NameSpace, awsCredsConsoleLoginURL string
}

// input for new aws client
type newAwsClientInput struct {
	awsCredsSecretIDKey, awsCredsSecretAccessKey, awsToken, awsRegion, secretName, nameSpace string
}

// Reconcile reads that state of the cluster for a Account object and makes changes based on the state read
// and what is in the Account.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileAccount) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling Account")

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

	// We expect this secret to exist in the same namespace Account CR's are created
	awsSetupClient, err := awsclient.GetAWSClient(r.Client, awsclient.NewAwsClientInput{
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
	if accountIsNewBYOC(currentAcctInstance) {
		reqLogger.Info("BYOC account")
		// Create client for BYOC account
		byocAWSClient, accountClaim, err := r.getBYOCClient(currentAcctInstance)
		if err != nil {
			if accountClaim != nil {
				r.accountClaimBYOCError(reqLogger, accountClaim, err)
			}
			return reconcile.Result{}, err
		}

		err = validateBYOCClaim(accountClaim)
		if err != nil {
			r.accountClaimBYOCError(reqLogger, accountClaim, err)
			return reconcile.Result{}, err
		}

		// Ensure the account is marked as claimed
		if !accountIsClaimed(currentAcctInstance) {
			reqLogger.Info("Marking BYOC account claimed")
			currentAcctInstance.Status.Claimed = true
			return reconcile.Result{}, r.statusUpdate(reqLogger, currentAcctInstance)
		}

		// Create access key and role for BYOC account
		if !accountHasState(currentAcctInstance) {
			// Rotate access keys
			err = r.byocRotateAccessKeys(reqLogger, byocAWSClient, accountClaim)
			if err != nil {
				reqLogger.Info("Failed to rotate BYOC access keys")
				r.accountClaimBYOCError(reqLogger, accountClaim, err)
				return reconcile.Result{}, err
			}

			// Create BYOC role to assume
			byocRoleID, err = createBYOCAdminAccessRole(reqLogger, awsSetupClient, byocAWSClient, adminAccessArn)
			if err != nil {
				reqLogger.Error(err, "Failed to create BYOC role")
				r.accountClaimBYOCError(reqLogger, accountClaim, err)
				return reconcile.Result{}, err
			}

			// Update the account CR to creating
			reqLogger.Info("Updating BYOC to creating")
			currentAcctInstance.Status.State = AccountCreating
			SetAccountStatus(reqLogger, currentAcctInstance, "BYOC Account Creating", awsv1alpha1.AccountCreating, AccountCreating)
			err = r.Client.Status().Update(context.TODO(), currentAcctInstance)
			if err != nil {
				return reconcile.Result{}, err
			}
		}
	} else {
		// Normal account creation

		// Test PendingVerification state creating support case and checking for case status
		if accountIsPendingVerification(currentAcctInstance) {

			// If the supportCaseID is blank and Account State = PendingVerification, create a case
			if !accountHasSupportCaseID(currentAcctInstance) {
				switch utils.DetectDevMode {
				case "local":
					log.Info("Running Locally, Skipping Support Case Creation.")
				default:
					caseID, err := createCase(reqLogger, currentAcctInstance.Spec.AwsAccountID, awsSetupClient)
					if err != nil {
						return reconcile.Result{}, err
					}
					reqLogger.Info("Case created", "CaseID", caseID)

					// Update supportCaseId in CR
					currentAcctInstance.Status.SupportCaseID = caseID
					SetAccountStatus(reqLogger, currentAcctInstance, "Account pending verification in AWS", awsv1alpha1.AccountPendingVerification, AccountPendingVerification)
					err = r.statusUpdate(reqLogger, currentAcctInstance)
					if err != nil {
						return reconcile.Result{}, err
					}

					// After creating the support case requeue the request. To avoid flooding and being blacklisted by AWS when
					// starting the operator with a large AccountPool, add a randomInterval (between 0 and 30 secs) to the regular wait time
					randomInterval, err := strconv.Atoi(currentAcctInstance.Spec.AwsAccountID)
					randomInterval %= 30

					// This will requeue verification for between 30 and 60 (30+30) seconds, depending on the account
					return reconcile.Result{RequeueAfter: time.Duration(intervalAfterCaseCreationSecs+randomInterval) * time.Second}, nil
				}
			}

			var resolved bool

			switch utils.DetectDevMode {
			case "local":
				log.Info("Running Locally, Skipping case resolution check")
				resolved = true
			default:
				resolvedScoped, err := checkCaseResolution(reqLogger, currentAcctInstance.Status.SupportCaseID, awsSetupClient)
				if err != nil {
					reqLogger.Error(err, "Error checking for Case Resolution")
					return reconcile.Result{}, err
				}
				resolved = resolvedScoped
			}

			// Case Resolved, account is Ready
			if resolved {
				reqLogger.Info(fmt.Sprintf("Case %s resolved", currentAcctInstance.Status.SupportCaseID))

				SetAccountStatus(reqLogger, currentAcctInstance, "Account ready to be claimed", awsv1alpha1.AccountReady, AccountReady)
				err = r.statusUpdate(reqLogger, currentAcctInstance)
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
			return reconcile.Result{}, r.statusUpdate(reqLogger, currentAcctInstance)
		}

		// see if in creating for longer then default wait time
		if accountCreatingTooLong(currentAcctInstance) {
			r.setStatusFailed(reqLogger, currentAcctInstance, fmt.Sprintf("Creation pending for longer then %d minutes", utils.WaitTime))
		}

		if accountIsUnclaimedAndHasNoState(currentAcctInstance) {
			// Initialize the awsAccountID var here since we only use it now inside this condition
			var awsAccountID string

			if !accountHasAwsAccountID(currentAcctInstance) {
				// before doing anything make sure we are not over the limit if we are just error
				if totalaccountwatcher.TotalAccountWatcher.Total >= AwsLimit {
					reqLogger.Error(awsv1alpha1.ErrAwsAccountLimitExceeded, "AWS Account limit reached", "Account Total", totalaccountwatcher.TotalAccountWatcher.Total)
					return reconcile.Result{}, awsv1alpha1.ErrAwsAccountLimitExceeded
				}

				// Build Aws Account
				awsAccountID, err = r.BuildAccount(reqLogger, awsSetupClient, currentAcctInstance)
				if err != nil {
					return reconcile.Result{}, err
				}

				// set state creating if the account was able to create
				SetAccountStatus(reqLogger, currentAcctInstance, "Attempting to create account", awsv1alpha1.AccountCreating, AccountCreating)
				err = r.Client.Status().Update(context.TODO(), currentAcctInstance)

				switch utils.DetectDevMode {
				case "local":
					log.Info("Running Locally, manually creating a case ID number: 11111111")
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

				// set state creating if the account was alredy created
				SetAccountStatus(reqLogger, currentAcctInstance, "AWS account already created", awsv1alpha1.AccountCreating, AccountCreating)
				err = r.Client.Status().Update(context.TODO(), currentAcctInstance)
				if err != nil {
					return reconcile.Result{}, err
				}

			}
		}
	}

	// Account init for both BYOC and Non-BYOC
	if accountReadyForInitialization(currentAcctInstance) {

		reqLogger.Info(fmt.Sprintf("Initalizing account: %s AWS ID: %s", currentAcctInstance.Name, currentAcctInstance.Spec.AwsAccountID))
		//var awsAssumedRoleClient awsclient.Client
		var roleToAssume string

		if accountIsBYOC(currentAcctInstance) {
			roleToAssume = byocRole
		} else {
			roleToAssume = awsv1alpha1.AccountOperatorIAMRole
		}

		var creds *sts.AssumeRoleOutput
		var credsErr error

		for i := 0; i < 10; i++ {
			// Get STS credentials so that we can create an aws client with
			creds, credsErr = getStsCredentials(reqLogger, awsSetupClient, roleToAssume, currentAcctInstance.Spec.AwsAccountID)
			if credsErr != nil {
				stsErrMsg := fmt.Sprintf("Failed to create STS Credentials for account ID %s", currentAcctInstance.Spec.AwsAccountID)
				reqLogger.Info(stsErrMsg, "Error", credsErr.Error())
				SetAccountStatus(reqLogger, currentAcctInstance, stsErrMsg, awsv1alpha1.AccountFailed, AccountFailed)
				r.setStatusFailed(reqLogger, currentAcctInstance, "Failed to get sts credentials")
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

		awsAssumedRoleClient, err := awsclient.GetAWSClient(r.Client, awsclient.NewAwsClientInput{
			AwsCredsSecretIDKey:     *creds.Credentials.AccessKeyId,
			AwsCredsSecretAccessKey: *creds.Credentials.SecretAccessKey,
			AwsToken:                *creds.Credentials.SessionToken,
			AwsRegion:               "us-east-1",
		})
		if err != nil {
			reqLogger.Info(err.Error())
			r.setStatusFailed(reqLogger, currentAcctInstance, "Failed to assume role")
			return reconcile.Result{}, err
		}

		secretName, err := r.BuildIAMUser(reqLogger, awsAssumedRoleClient, currentAcctInstance, iamUserNameUHC, request.Namespace)
		if err != nil {
			r.setStatusFailed(reqLogger, currentAcctInstance, fmt.Sprintf("Failed to build IAM UHC user: %s", iamUserNameUHC))
			return reconcile.Result{}, err
		}
		currentAcctInstance.Spec.IAMUserSecret = secretName
		err = r.Client.Update(context.TODO(), currentAcctInstance)
		if err != nil {
			return reconcile.Result{}, err
		}

		// Create SRE IAM user and return the credentials
		SREIAMUserSecret, err := r.BuildIAMUser(reqLogger, awsAssumedRoleClient, currentAcctInstance, iamUserNameSRE, request.Namespace)
		if err != nil {
			r.setStatusFailed(reqLogger, currentAcctInstance, fmt.Sprintf("Failed to build IAM SRE user: %s", iamUserNameSRE))
			return reconcile.Result{}, err
		}

		// Intermittently our secret wont be ready before the next call, lets ensure it exists
		for i := 0; i < 10; i++ {
			secret := &corev1.Secret{}
			err := r.Client.Get(context.TODO(), types.NamespacedName{Name: SREIAMUserSecret, Namespace: request.Namespace}, secret)
			if err != nil {
				if k8serr.IsNotFound(err) {
					reqLogger.Info("SREIAMUserSecret not ready, trying again")
					time.Sleep(time.Duration(time.Duration(i*1) * time.Second))
				} else {
					reqLogger.Error(err, "unable to retrive SREIAMUserSecret secret")
					return reconcile.Result{}, err
				}
			}

		}

		// Create new awsClient with SRE IAM credentials so we can generate STS and Federation tokens from it
		SREAWSClient, err := awsclient.GetAWSClient(r.Client, awsclient.NewAwsClientInput{
			SecretName: SREIAMUserSecret,
			NameSpace:  awsv1alpha1.AccountCrNamespace,
			AwsRegion:  "us-east-1",
		})

		if err != nil {
			var returnErr error
			utils.LogAwsError(reqLogger, "Unable to create AWS connection with SRE credentials", returnErr, err)
			return reconcile.Result{}, err
		}

		// Create STS CLI Credentials for SRE
		_, err = r.BuildSTSUser(reqLogger, SREAWSClient, awsSetupClient, currentAcctInstance, request.Namespace, roleToAssume)
		if err != nil {
			r.setStatusFailed(reqLogger, currentAcctInstance, fmt.Sprintf("Failed to build SRE STS credentials: %s", iamUserNameSRE))
			return reconcile.Result{}, err
		}

		// Initialize all supported regions by creating and terminating an instance in each
		err = r.InitializeSupportedRegions(reqLogger, awsv1alpha1.CoveredRegions, creds)
		if err != nil {
			r.setStatusFailed(reqLogger, currentAcctInstance, "Failed to build and destroy ec2 instances")
			return reconcile.Result{}, err
		}

		if accountIsBYOC(currentAcctInstance) {
			SetAccountStatus(reqLogger, currentAcctInstance, "BYOC Account Ready", awsv1alpha1.AccountReady, AccountReady)

		} else {
			if utils.FindAccountCondition(currentAcctInstance.Status.Conditions, awsv1alpha1.AccountReady) != nil {
				SetAccountStatus(reqLogger, currentAcctInstance, "Account support case already resolved, Account Ready", awsv1alpha1.AccountReady, "Ready")
				reqLogger.Info("Account support case already resolved, Account Ready")
			} else {
				SetAccountStatus(reqLogger, currentAcctInstance, "Account pending AWS limits verification", awsv1alpha1.AccountPendingVerification, AccountPendingVerification)
				reqLogger.Info("Account pending AWS limits verification")
			}
		}
		err = r.Client.Status().Update(context.TODO(), currentAcctInstance)
		if err != nil {
			return reconcile.Result{}, err
		}

	}

	// If Account CR has `stats.rotateCredentials: true` we'll rotate the temporary credentials
	// the secretWatcher is what updates this status field by comparing the STS credentials secret `creationTimestamp`
	if accountNeedsCredentialsRotated(currentAcctInstance) {
		reqLogger.Info(fmt.Sprintf("rotating CLI credentials for %s", currentAcctInstance.Name))
		err = r.RotateCredentials(reqLogger, awsSetupClient, currentAcctInstance)
		if err != nil {
			return reconcile.Result{}, err
		}
	}
	if accountNeedsConsoleCredentialsRotated(currentAcctInstance) {
		reqLogger.Info(fmt.Sprintf("rotating console URL credentials for %s", currentAcctInstance.Name))
		err = r.RotateConsoleCredentials(reqLogger, awsSetupClient, currentAcctInstance)
		if err != nil {
			return reconcile.Result{}, err
		}
	}
	return reconcile.Result{}, nil
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
			SetAccountStatus(reqLogger, account, "Failed to create AWS Account", awsv1alpha1.AccountFailed, AccountFailed)
			err := r.Client.Status().Update(context.TODO(), account)
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
		reqLogger.Error(err, "Unable to get name and namespace of Acccount object")
	}
	err = r.Client.Get(context.TODO(), accountObjectKey, account)
	if err != nil {
		reqLogger.Error(err, "Unable to get updated Acccount object after status update")
	}

	reqLogger.Info("Account Created")

	return *orgOutput.CreateAccountStatus.AccountId, nil
}

func (r *ReconcileAccount) setStatusFailed(reqLogger logr.Logger, awsAccount *awsv1alpha1.Account, message string) error {
	reqLogger.Info(message)
	awsAccount.Status.State = AccountFailed
	err := r.Client.Status().Update(context.TODO(), awsAccount)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Account %s status failed to update", awsAccount.Name))
		return err
	}
	return nil
}

// CreateAccount creates an AWS account for the specified accountName and accountEmail in the orgnization
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

func (input SRESecretInput) newSTSSecret() *corev1.Secret {
	return &corev1.Secret{
		Type: "Opaque",
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      input.SecretName,
			Namespace: input.NameSpace,
		},
		Data: map[string][]byte{
			"aws_access_key_id":     []byte(input.awsCredsSecretIDKey),
			"aws_secret_access_key": []byte(input.awsCredsSecretAccessKey),
			"aws_session_token":     []byte(input.awsCredsSessionToken),
		},
	}

}

func (input SREConsoleInput) newConsoleSecret() *corev1.Secret {
	return &corev1.Secret{
		Type: "Opaque",
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      input.SecretName,
			Namespace: input.NameSpace,
		},
		Data: map[string][]byte{
			"aws_console_login_url": []byte(input.awsCredsConsoleLoginURL),
		},
	}

}

func (input secretInput) newSecretforCR() *corev1.Secret {
	return &corev1.Secret{
		Type: "Opaque",
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      input.SecretName,
			Namespace: input.NameSpace,
		},
		Data: map[string][]byte{
			"aws_user_name":         []byte(input.awsCredsUserName),
			"aws_access_key_id":     []byte(input.awsCredsSecretIDKey),
			"aws_secret_access_key": []byte(input.awsCredsSecretAccessKey),
		},
	}

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

// SetAccountStatus sets the status of an account
func SetAccountStatus(reqLogger logr.Logger, awsAccount *awsv1alpha1.Account, message string, ctype awsv1alpha1.AccountConditionType, state string) {
	awsAccount.Status.Conditions = utils.SetAccountCondition(
		awsAccount.Status.Conditions,
		ctype,
		corev1.ConditionTrue,
		state,
		message,
		utils.UpdateConditionNever)
	awsAccount.Status.State = state
	reqLogger.Info(fmt.Sprintf("Account %s status updated", awsAccount.Name))
}

func (r *ReconcileAccount) statusUpdate(reqLogger logr.Logger, account *awsv1alpha1.Account) error {
	err := r.Client.Status().Update(context.TODO(), account)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Status update for %s failed", account.Name))
	}
	reqLogger.Info(fmt.Sprintf("Status updated for %s", account.Name))
	return err
}

func matchSubstring(roleID, role string) (bool, error) {
	matched, err := regexp.MatchString(roleID, role)
	return matched, err
}

// Returns true of there is no state set
func accountHasState(currentAcctInstance *awsv1alpha1.Account) bool {
	if currentAcctInstance.Status.State != "" {
		return true
	}
	return false
}

// Returns true of there is no Status.SupportCaseID set
func accountHasSupportCaseID(currentAcctInstance *awsv1alpha1.Account) bool {
	if currentAcctInstance.Status.SupportCaseID != "" {
		return true
	}
	return false
}

func accountIsPendingVerification(currentAcctInstance *awsv1alpha1.Account) bool {
	if currentAcctInstance.Status.State == AccountPendingVerification {
		return true
	}
	return false
}

func accountIsReady(currentAcctInstance *awsv1alpha1.Account) bool {
	if currentAcctInstance.Status.State == AccountReady {
		return true
	}
	return false
}

func accountIsCreating(currentAcctInstance *awsv1alpha1.Account) bool {
	if currentAcctInstance.Status.State == AccountCreating {
		return true
	}

	return false
}

func accountHasClaimLink(currentAcctInstance *awsv1alpha1.Account) bool {
	if currentAcctInstance.Spec.ClaimLink != "" {
		return true
	}
	return false
}

func accountCreatingTooLong(currentAcctInstance *awsv1alpha1.Account) bool {
	if accountIsCreating(currentAcctInstance) && time.Now().Sub(currentAcctInstance.GetCreationTimestamp().Time) > createPendTime {
		return true
	}
	return false
}

// Returns true if account Status.Claimed is false
func accountIsClaimed(currentAcctInstance *awsv1alpha1.Account) bool {
	if currentAcctInstance.Status.Claimed {
		return true
	}
	return false
}

// Returns true if a DeletionTimestamp has been set
func accountIsPendingDeletion(currentAcctInstance *awsv1alpha1.Account) bool {
	if currentAcctInstance.DeletionTimestamp != nil {
		return true
	}
	return false
}

// Returns true of Spec.BYOC is true, ie: account is a BYOC account
func accountIsBYOC(currentAcctInstance *awsv1alpha1.Account) bool {
	if currentAcctInstance.Spec.BYOC == true {
		return true
	}
	return false
}

// Returns true if the awsv1alpha1 finalizer is set on the account
func accountHasAwsv1alpha1Finalizer(currentAcctInstance *awsv1alpha1.Account) bool {
	if utils.Contains(currentAcctInstance.GetFinalizers(), awsv1alpha1.AccountFinalizer) {
		return true
	}
	return false
}

// Returns true if awsAccountID is set
func accountHasAwsAccountID(currentAcctInstance *awsv1alpha1.Account) bool {
	if currentAcctInstance.Spec.AwsAccountID != "" {
		return true
	}
	return false
}

// Returns true if Status.RotateCredentials is true
func accountNeedsCredentialsRotated(currentAcctInstance *awsv1alpha1.Account) bool {
	if currentAcctInstance.Status.RotateCredentials {
		return true
	}
	return false
}

// Returns true if Status.RotateConsoleCredentials is true
func accountNeedsConsoleCredentialsRotated(currentAcctInstance *awsv1alpha1.Account) bool {
	if currentAcctInstance.Status.RotateConsoleCredentials {
		return true
	}
	return false
}
func accountIsReadyUnclaimedAndHasClaimLink(currentAcctInstance *awsv1alpha1.Account) bool {
	if accountIsReady(currentAcctInstance) && accountHasClaimLink(currentAcctInstance) && !accountIsClaimed(currentAcctInstance) {
		return true
	}
	return false
}

// Returns true if account is a BYOC Account, has been marked for deletion (deletion
// timestamp set), and has a finalizer set.
func accountIsBYOCPendingDeletionWithFinalizer(currentAcctInstance *awsv1alpha1.Account) bool {
	if accountIsPendingDeletion(currentAcctInstance) && accountIsBYOC(currentAcctInstance) && accountHasAwsv1alpha1Finalizer(currentAcctInstance) {
		return true
	}
	return false
}

// Returns true if account is BYOC and the state is not AccountReady
func accountIsBYOCAndNotReady(currentAcctInstance *awsv1alpha1.Account) bool {
	if accountIsBYOC(currentAcctInstance) && !accountIsReady(currentAcctInstance) {
		return true
	}
	return false
}

// Returns true if account is a BYOC Account and the state is not ready OR
// accout state is creating, and has not been claimed
func accountReadyForInitialization(currentAcctInstance *awsv1alpha1.Account) bool {
	if accountIsBYOCAndNotReady(currentAcctInstance) || accountIsUnclaimedAndIsCreating(currentAcctInstance) {
		return true
	}
	return false
}

// Returns true if account has not set state and has not been claimed
func accountIsUnclaimedAndHasNoState(currentAcctInstance *awsv1alpha1.Account) bool {
	if !accountHasState(currentAcctInstance) && !accountIsClaimed(currentAcctInstance) {
		return true
	}
	return false
}

// Return true if account state is AccountCreeating and has not been claimed
func accountIsUnclaimedAndIsCreating(currentAcctInstance *awsv1alpha1.Account) bool {
	if accountIsCreating(currentAcctInstance) && !accountIsClaimed(currentAcctInstance) {
		return true
	}
	return false
}

type accountInstance interface {
	accountIsUnclaimedAndIsCreating() bool
}
