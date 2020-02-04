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
	"github.com/aws/aws-sdk-go/service/iam"
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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	controllerutils "github.com/openshift/aws-account-operator/pkg/controller/utils"
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
	createPendTime          = 30 * time.Minute
	// Fields used to create/monitor AWS case
	caseCategoryCode              = "other-account-issues"
	caseServiceCode               = "customer-account"
	caseIssueType                 = "customer-service"
	caseSeverity                  = "urgent"
	caseDesiredInstanceLimit      = 25
	caseStatusResolved            = "resolved"
	caseLanguage                  = "en"
	intervalAfterCaseCreationSecs = 30
	intervalBetweenChecksMinutes  = 10

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

var coveredRegions = map[string]map[string]string{
	"us-east-1": {
		"initializationAMI": "ami-000db10762d0c4c05",
	},
	"us-east-2": {
		"initializationAMI": "ami-094720ddca649952f",
	},
	"us-west-1": {
		"initializationAMI": "ami-04642fc8fca1e8e67",
	},
	"us-west-2": {
		"initializationAMI": "ami-0a7e1ebfee7a4570e",
	},
	"ca-central-1": {
		"initializationAMI": "ami-06ca3c0058d0275b3",
	},
	"eu-central-1": {
		"initializationAMI": "ami-09de4a4c670389e4b",
	},
	"eu-west-1": {
		"initializationAMI": "ami-0202869bdd0fc8c75",
	},
	"eu-west-2": {
		"initializationAMI": "ami-0188c0c5eddd2d032",
	},
	"eu-west-3": {
		"initializationAMI": "ami-0c4224e392ec4e440",
	},
	"ap-northeast-1": {
		"initializationAMI": "ami-00b95502a4d51a07e",
	},
	"ap-northeast-2": {
		"initializationAMI": "ami-041b16ca28f036753",
	},
	"ap-south-1": {
		"initializationAMI": "ami-0963937a03c01ecd4",
	},
	"ap-southeast-1": {
		"initializationAMI": "ami-055c55112e25b1f1f",
	},
	"ap-southeast-2": {
		"initializationAMI": "ami-036b423b657376f5b",
	},
	"sa-east-1": {
		"initializationAMI": "ami-05c1c16cac05a7c0b",
	},
}

// Instance types UHC supports
var coveredInstanceTypes = []string{
	"c5.xlarge",
	"c5.2xlarge",
	"c5.4xlarge",
	"m5.xlarge",
	"m5.2xlarge",
	"m5.4xlarge",
	"r5.xlarge",
	"r5.2xlarge",
	"r5.4xlarge",
}

// Custom errors

// ErrAwsAccountLimitExceeded indicates the orgnization account limit has been reached.
var ErrAwsAccountLimitExceeded = errors.New("AccountLimitExceeded")

// ErrAwsInternalFailure indicates that there was an internal failure on the aws api
var ErrAwsInternalFailure = errors.New("InternalFailure")

// ErrAwsFailedCreateAccount indicates that an account creation failed
var ErrAwsFailedCreateAccount = errors.New("FailedCreateAccount")

// ErrAwsTooManyRequests indicates that to many requests were sent in a short period
var ErrAwsTooManyRequests = errors.New("TooManyRequestsException")

// ErrAwsCaseCreationLimitExceeded indicates that the support case limit for the account has been reached
var ErrAwsCaseCreationLimitExceeded = errors.New("SupportCaseLimitExceeded")

// ErrAwsFailedCreateSupportCase indicates that a support case creation failed
var ErrAwsFailedCreateSupportCase = errors.New("FailedCreateSupportCase")

// ErrAwsSupportCaseIDNotFound indicates that the support case ID was not found
var ErrAwsSupportCaseIDNotFound = errors.New("SupportCaseIdNotfound")

// ErrAwsFailedDescribeSupportCase indicates that the support case describe failed
var ErrAwsFailedDescribeSupportCase = errors.New("FailedDescribeSupportCase")

// ErrFederationTokenOutputNil indicates that getting a federation token from AWS failed
var ErrFederationTokenOutputNil = errors.New("FederationTokenOutputNil")

// ErrCreateEC2Instance indicates that the CreateEC2Instance function timed out
var ErrCreateEC2Instance = errors.New("EC2CreationTimeout")

// ErrFailedAWSTypecast indicates that there was a failure while typecasting to aws error
var ErrFailedAWSTypecast = errors.New("FailedToTypecastAWSError")

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
	if currentAcctInstance.DeletionTimestamp != nil && currentAcctInstance.Spec.BYOC {
		if contains(currentAcctInstance.GetFinalizers(), awsv1alpha1.AccountFinalizer) {
			// Remove finalizer to unlock deletion of the accountClaim
			err = r.removeFinalizer(reqLogger, currentAcctInstance, awsv1alpha1.AccountFinalizer)
			if err != nil {
				return reconcile.Result{}, err
			}

			return reconcile.Result{}, nil
		}
	}

	// We expect this secret to exist in the same namespace Account CR's are created
	awsSetupClient, err := awsclient.GetAWSClient(r.Client, awsclient.NewAwsClientInput{
		SecretName: controllerutils.AwsSecretName,
		NameSpace:  awsv1alpha1.AccountCrNamespace,
		AwsRegion:  "us-east-1",
	})
	if err != nil {
		reqLogger.Error(err, "Failed to get AWS client")
		return reconcile.Result{}, err
	}

	var byocRoleID string

	// If the account is BYOC, needs some different set up
	if currentAcctInstance.Spec.BYOC {
		reqLogger.Info("BYOC account")
		if currentAcctInstance.Status.State == "" || currentAcctInstance.Status.Claimed != true {
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
			if currentAcctInstance.Status.Claimed != true {
				reqLogger.Info("Marking BYOC account claimed")
				currentAcctInstance.Status.Claimed = true
				return reconcile.Result{}, r.statusUpdate(reqLogger, currentAcctInstance)
			}

			// Create access key and role for BYOC account
			if currentAcctInstance.Status.State == "" {
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
		}
	} else {
		// Normal account creation

		// Test PendingVerification state creating support case and checking for case status
		if currentAcctInstance.Status.State == AccountPendingVerification {
			// reqLogger.Info("Account in PendingVerification state", "AccountID", currentAcctInstance.Spec.AwsAccountID)

			// If the supportCaseID is blank and Account State = PendingVerification, create a case
			if currentAcctInstance.Status.SupportCaseID == "" {
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

			resolved, err := checkCaseResolution(reqLogger, currentAcctInstance.Status.SupportCaseID, awsSetupClient)
			if err != nil {
				reqLogger.Error(err, "Error checking for Case Resolution")
				return reconcile.Result{}, err
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
		if currentAcctInstance.Status.State == AccountReady && currentAcctInstance.Spec.ClaimLink != "" {
			if currentAcctInstance.Status.Claimed != true {
				currentAcctInstance.Status.Claimed = true
				return reconcile.Result{}, r.statusUpdate(reqLogger, currentAcctInstance)
			}
		}

		// see if in creating for longer then 10 minutes
		now := time.Now()
		diff := now.Sub(currentAcctInstance.ObjectMeta.CreationTimestamp.Time)
		if currentAcctInstance.Status.State == AccountCreating && diff > createPendTime {
			r.setStatusFailed(reqLogger, currentAcctInstance, "Creation pending for longer then 10 minutes")
		}

		if (currentAcctInstance.Status.State == "") && (currentAcctInstance.Status.Claimed == false) {

			// Initialize the awsAccountID var here since we only use it now inside this condition
			var awsAccountID string

			if currentAcctInstance.Spec.AwsAccountID == "" {

				// before doing anything make sure we are not over the limit if we are just error
				if totalaccountwatcher.TotalAccountWatcher.Total >= AwsLimit {
					reqLogger.Error(ErrAwsAccountLimitExceeded, "AWS Account limit reached", "Account Total", totalaccountwatcher.TotalAccountWatcher.Total)
					return reconcile.Result{}, ErrAwsAccountLimitExceeded
				}

				// Build Aws Account
				awsAccountID, err = r.BuildAccount(reqLogger, awsSetupClient, currentAcctInstance)
				if err != nil {
					return reconcile.Result{}, err
				}

				// set state creating if the account was able to create
				SetAccountStatus(reqLogger, currentAcctInstance, "Attempting to create account", awsv1alpha1.AccountCreating, AccountCreating)
				err = r.Client.Status().Update(context.TODO(), currentAcctInstance)

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
	if (currentAcctInstance.Spec.BYOC && currentAcctInstance.Status.State != AccountReady) || ((currentAcctInstance.Status.State == AccountReady) && (currentAcctInstance.Status.Claimed == false)) {
		reqLogger.Info(fmt.Sprintf("Initalizing account: %s AWS ID: %s", currentAcctInstance.Name, currentAcctInstance.Spec.AwsAccountID))

		//var awsAssumedRoleClient awsclient.Client
		var roleToAssume string

		if currentAcctInstance.Spec.BYOC {
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
			controllerutils.LogAwsError(reqLogger, "Unable to create AWS connection with SRE credentials", returnErr, err)
			return reconcile.Result{}, err
		}

		// Create STS CLI Credentials for SRE
		_, err = r.BuildSTSUser(reqLogger, SREAWSClient, awsSetupClient, currentAcctInstance, request.Namespace, roleToAssume)
		if err != nil {
			r.setStatusFailed(reqLogger, currentAcctInstance, fmt.Sprintf("Failed to build SRE STS credentials: %s", iamUserNameSRE))
			return reconcile.Result{}, err
		}

		// Initialize all supported regions by creating and terminating an instance in each
		err = r.InitializeSupportedRegions(reqLogger, coveredRegions, creds)
		if err != nil {
			r.setStatusFailed(reqLogger, currentAcctInstance, "Failed to build and destroy ec2 instances")
			return reconcile.Result{}, err
		}

		if currentAcctInstance.Spec.BYOC {
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
	if currentAcctInstance.Status.RotateCredentials == true {
		reqLogger.Info(fmt.Sprintf("rotating CLI credentials for %s", currentAcctInstance.Name))
		err = r.RotateCredentials(reqLogger, awsSetupClient, currentAcctInstance)
		if err != nil {
			return reconcile.Result{}, err
		}
	}
	if currentAcctInstance.Status.RotateConsoleCredentials == true {
		reqLogger.Info(fmt.Sprintf("rotating console URL credentials for %s", currentAcctInstance.Name))
		err = r.RotateConsoleCredentials(reqLogger, awsSetupClient, currentAcctInstance)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

// Function to remove finalizer
// TODO: This function removeFinalizer, contains and remove are the same as the claim controller and should be moved to the utils pkg
func (r *ReconcileAccount) removeFinalizer(reqLogger logr.Logger, account *awsv1alpha1.Account, finalizerName string) error {
	reqLogger.Info("Removing Finalizer from the Account")
	account.SetFinalizers(remove(account.GetFinalizers(), finalizerName))

	// Update CR
	err := r.Client.Update(context.TODO(), account)
	if err != nil {
		reqLogger.Error(err, "Failed to remove AccountClaim finalizer")
		return err
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func remove(list []string, s string) []string {
	for i, v := range list {
		if v == s {
			list = append(list[:i], list[i+1:]...)
		}
	}
	return list
}

// BuildAccount take all parameters required and uses those to make an aws call to CreateAccount. It returns an account ID and and error
func (r *ReconcileAccount) BuildAccount(reqLogger logr.Logger, awsClient awsclient.Client, account *awsv1alpha1.Account) (string, error) {

	reqLogger.Info("Creating Account")

	email := formatAccountEmail(account.Name)
	orgOutput, orgErr := CreateAccount(reqLogger, awsClient, account.Name, email)
	// If it was an api or a limit issue don't modify account and exit if anything else set to failed
	if orgErr != nil {
		switch orgErr {
		case ErrAwsFailedCreateAccount:
			SetAccountStatus(reqLogger, account, "Failed to create AWS Account", awsv1alpha1.AccountFailed, AccountFailed)
			err := r.Client.Status().Update(context.TODO(), account)
			if err != nil {
				return "", err
			}

			reqLogger.Error(ErrAwsFailedCreateAccount, "Failed to create AWS Account")
			return "", orgErr
		case ErrAwsAccountLimitExceeded:
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

// BuildIAMUser takes all parameters required to create a user, user secret
func (r *ReconcileAccount) BuildIAMUser(reqLogger logr.Logger, awsClient awsclient.Client, account *awsv1alpha1.Account, iamUserName string, nameSpace string) (string, error) {
	_, userErr := CreateIAMUser(reqLogger, awsClient, iamUserName)
	// TODO: better error handling but for now scrap account
	if userErr != nil {
		//If the user already exists, don't erro
		if aerr, ok := userErr.(awserr.Error); ok {
			switch aerr.Code() {
			case "EntityAlreadyExists":
				break
			default:
				failedToCreateIAMUserMsg := fmt.Sprintf("Failed to create IAM user %s due to AWS Error: %s", iamUserName, aerr.Message())
				utils.LogAwsError(reqLogger, "", nil, userErr)
				SetAccountStatus(reqLogger, account, failedToCreateIAMUserMsg, awsv1alpha1.AccountFailed, AccountFailed)
				err := r.Client.Status().Update(context.TODO(), account)
				if err != nil {
					return "", err
				}
				reqLogger.Info(failedToCreateIAMUserMsg)
				return "", userErr
			}
		} else {
			failedToCreateIAMUserMsg := fmt.Sprintf("Failed to create IAM user %s due to non-AWS Error", iamUserName)
			SetAccountStatus(reqLogger, account, failedToCreateIAMUserMsg, awsv1alpha1.AccountFailed, AccountFailed)
			err := r.Client.Status().Update(context.TODO(), account)
			if err != nil {
				return "", err
			}
			reqLogger.Info(failedToCreateIAMUserMsg)
			return "", userErr
		}
	}

	reqLogger.Info(fmt.Sprintf("Attaching Admin Policy to IAM user %s", iamUserName))
	// Setting user access policy
	_, policyErr := AttachAdminUserPolicy(reqLogger, awsClient, iamUserName)
	if policyErr != nil {
		failedToAttachUserPolicyMsg := fmt.Sprintf("Failed to attach admin policy to IAM user %s", iamUserName)
		SetAccountStatus(reqLogger, account, failedToAttachUserPolicyMsg, awsv1alpha1.AccountFailed, AccountFailed)
		r.setStatusFailed(reqLogger, account, failedToAttachUserPolicyMsg)
		return "", policyErr
	}

	reqLogger.Info(fmt.Sprintf("Creating Secrets for IAM user %s", iamUserName))

	secretName := account.Name

	// Append to secret name if we're if its the SRE admin user secret
	if iamUserName == iamUserNameSRE {
		secretName = account.Name + "-" + strings.ToLower(iamUserName)
	}

	// create user secrets if needed
	userExistingSecret := &corev1.Secret{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: fmt.Sprintf("%s-secret", secretName), Namespace: nameSpace}, userExistingSecret)
	if err != nil {
		if !k8serr.IsNotFound(err) {
			getSecretErr := fmt.Sprintf("Error getting secret for user %s, err: %+v", iamUserName, err)
			reqLogger.Info(getSecretErr)
			return "", err
		}
	}

	if k8serr.IsNotFound(err) {
		//Delete all current access keys
		deleteKeysErr := deleteAllAccessKeys(reqLogger, awsClient, iamUserName)
		if deleteKeysErr != nil {
			deleteAccessKeysMsg := fmt.Sprintf("Failed to delete IAM access keys for %s, err: %+v", iamUserName, deleteKeysErr)
			reqLogger.Info(deleteAccessKeysMsg)
			return "", deleteKeysErr
		}
		//Create new access key
		accessKeyOutput, createKeyErr := CreateUserAccessKey(reqLogger, awsClient, iamUserName)
		if createKeyErr != nil {
			failedToCreateUserAccessKeyMsg := fmt.Sprintf("Failed to create IAM access key for %s, err: %+v", iamUserName, createKeyErr)
			SetAccountStatus(reqLogger, account, failedToCreateUserAccessKeyMsg, awsv1alpha1.AccountFailed, AccountFailed)
			err := r.Client.Status().Update(context.TODO(), account)
			if err != nil {
				return "", err
			}
			reqLogger.Info(failedToCreateUserAccessKeyMsg)
			return "", createKeyErr
		}

		//Fill in the secret data
		userSecretInput := secretInput{
			SecretName:              fmt.Sprintf("%s-secret", secretName),
			NameSpace:               nameSpace,
			awsCredsUserName:        *accessKeyOutput.AccessKey.UserName,
			awsCredsSecretIDKey:     *accessKeyOutput.AccessKey.AccessKeyId,
			awsCredsSecretAccessKey: *accessKeyOutput.AccessKey.SecretAccessKey,
		}

		//Create new secret
		userSecret := userSecretInput.newSecretforCR()

		// Set controller as owner of secret
		if err := controllerutil.SetControllerReference(account, userSecret, r.scheme); err != nil {
			return "", err
		}

		createErr := r.Client.Create(context.TODO(), userSecret)
		if createErr != nil {
			failedToCreateUserSecretMsg := fmt.Sprintf("Failed to create secret for IAM user %s", iamUserName)
			SetAccountStatus(reqLogger, account, failedToCreateUserSecretMsg, awsv1alpha1.AccountFailed, AccountFailed)
			err := r.Client.Status().Update(context.TODO(), account)
			if err != nil {
				return "", err
			}
			reqLogger.Info(failedToCreateUserSecretMsg)
			return "", createErr
		}

		return userSecret.ObjectMeta.Name, nil
	}

	//Secret already exists
	return fmt.Sprintf("%s-secret", secretName), nil
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
				returnErr = ErrAwsAccountLimitExceeded
			case organizations.ErrCodeServiceException:
				returnErr = ErrAwsInternalFailure
			case organizations.ErrCodeTooManyRequestsException:
				returnErr = ErrAwsTooManyRequests
			default:
				returnErr = ErrAwsFailedCreateAccount
				controllerutils.LogAwsError(reqLogger, "New AWS Error during account creation", returnErr, err)
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
				returnErr = ErrAwsAccountLimitExceeded
			case "INTERNAL_FAILURE":
				returnErr = ErrAwsInternalFailure
			default:
				returnErr = ErrAwsFailedCreateAccount
			}

			return &organizations.DescribeCreateAccountStatusOutput{}, returnErr
		}

		if createStatus != "IN_PROGRESS" {
			break
		}

	}

	return accountStatus, nil
}

func checkIAMUserExists(reqLogger logr.Logger, client awsclient.Client, userName string) (bool, error) {
	// Retry when getting IAM user information
	// Sometimes we see a delay before credentials are ready to be user resulting in the AWS API returning 404's
	attempt := 1
	for i := 0; i < 10; i++ {
		// check if username exists for this account
		_, err := client.GetUser(&iam.GetUserInput{
			UserName: aws.String(userName),
		})

		attempt++
		// handle errors
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				switch aerr.Code() {
				case iam.ErrCodeNoSuchEntityException:
					return false, nil
				case "InvalidClientTokenId":
					invalidTokenMsg := fmt.Sprintf("Invalid Token error from AWS when attempting get IAM user %s, trying again", userName)
					reqLogger.Info(invalidTokenMsg)
					if attempt == 10 {
						return false, err
					}
				case "AccessDenied":
					checkUserMsg := fmt.Sprintf("AWS Error while checking IAM user %s exists, trying again", userName)
					controllerutils.LogAwsError(reqLogger, checkUserMsg, nil, err)
					// We may have bad credentials so return an error if so
					if attempt == 10 {
						return false, err
					}
				default:
					controllerutils.LogAwsError(reqLogger, "checkIAMUserExists: Unexpected AWS Error when checking IAM user exists", nil, err)
					return false, err
				}
				time.Sleep(time.Duration(time.Duration(attempt*5) * time.Second))
			} else {
				return false, fmt.Errorf("Unable to check if user %s exists error: %s", userName, err)
			}
		}
	}

	// User exists return
	return true, nil
}

// CreateIAMUser takes a client and string and creates a IAMuser
func CreateIAMUser(reqLogger logr.Logger, client awsclient.Client, userName string) (*iam.CreateUserOutput, error) {

	// check if username exists for this account
	userExists, err := checkIAMUserExists(reqLogger, client, userName)
	if err != nil {
		return &iam.CreateUserOutput{}, err
	}

	// return the error if the IAM user already exists
	if userExists {
		return &iam.CreateUserOutput{}, err
	}

	var createUserOutput = &iam.CreateUserOutput{}

	attempt := 1
	for i := 0; i < 10; i++ {

		createUserOutput, err = client.CreateUser(&iam.CreateUserInput{
			UserName: aws.String(userName),
		})

		attempt++
		// handle errors
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				switch aerr.Code() {
				// Since we're using the same credentials to create the user as we did to check if the user exists
				// we can continue to try without returning, also the outer loop will eventually return
				case "InvalidClientTokenId":
					invalidTokenMsg := fmt.Sprintf("Invalid Token error from AWS when attempting to create user %s, trying again", userName)
					reqLogger.Info(invalidTokenMsg)
					if attempt == 10 {
						return &iam.CreateUserOutput{}, err
					}
				// createUserOutput inconsistently returns "InvalidClientTokenId" if that happens then the next call to
				// create the user will fail with EntitiyAlreadyExists. Since we verity the user doesn't exist before this
				// loop we can safely assume we created the user on our first loop.
				case iam.ErrCodeEntityAlreadyExistsException:
					invalidTokenMsg := fmt.Sprintf("IAM User %s was created", userName)
					reqLogger.Info(invalidTokenMsg)
					return &iam.CreateUserOutput{}, nil
				default:
					controllerutils.LogAwsError(reqLogger, "CreateIAMUser: Unexpect AWS Error during creation of IAM user", nil, err)
					return &iam.CreateUserOutput{}, err
				}
				time.Sleep(time.Duration(time.Duration(attempt*5) * time.Second))
			} else {
				return &iam.CreateUserOutput{}, err
			}
		}
	}

	return createUserOutput, err
}

// AttachAdminUserPolicy takes a client and string attaches the admin policy to the user
func AttachAdminUserPolicy(reqLogger logr.Logger, client awsclient.Client, userName string) (*iam.AttachUserPolicyOutput, error) {

	attachPolicyOutput := &iam.AttachUserPolicyOutput{}
	var err error
	for i := 0; i < 100; i++ {
		time.Sleep(500 * time.Millisecond)
		attachPolicyOutput, err = client.AttachUserPolicy(&iam.AttachUserPolicyInput{
			UserName:  aws.String(userName),
			PolicyArn: aws.String("arn:aws:iam::aws:policy/AdministratorAccess"),
		})
		if err == nil {
			break
		}
	}
	if err != nil {
		controllerutils.LogAwsError(reqLogger, "New AWS Error while attaching admin user policy", nil, err)
		return &iam.AttachUserPolicyOutput{}, err
	}

	return attachPolicyOutput, nil
}

// CreateUserAccessKey creates an IAM user's secret and returns the accesskey id and secret for that user in a aws.CreateAccessKeyOutput struct
func CreateUserAccessKey(reqLogger logr.Logger, client awsclient.Client, userName string) (*iam.CreateAccessKeyOutput, error) {

	// Create new access key for user
	input := &iam.CreateAccessKeyInput{}
	input.SetUserName(userName)
	result, err := client.CreateAccessKey(input)
	if err != nil {
		controllerutils.LogAwsError(reqLogger, "New AWS Error while creating user access key", nil, err)
		return &iam.CreateAccessKeyOutput{}, err
	}

	return result, nil
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
	awsAccount.Status.Conditions = controllerutils.SetAccountCondition(
		awsAccount.Status.Conditions,
		ctype,
		corev1.ConditionTrue,
		state,
		message,
		controllerutils.UpdateConditionNever)
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
