package account

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/organizations"
	"github.com/aws/aws-sdk-go/service/support"
	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	controllerutils "github.com/openshift/aws-account-operator/pkg/controller/utils"
	totalaccountwatcher "github.com/openshift/aws-account-operator/pkg/totalaccountwatcher"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	kubeclientpkg "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_account")

const (
	AwsLimit                = 4700
	awsCredsUserName        = "aws_user_name"
	awsCredsSecretIDKey     = "aws_access_key_id"
	awsCredsSecretAccessKey = "aws_secret_access_key"
	iamUserNameUHC          = "osdManagedAdmin"
	iamUserNameSRE          = "osdManagedAdminSRE"
	awsAMI                  = "ami-000db10762d0c4c05"
	awsInstanceType         = "t2.micro"
	createPendTime          = 10 * time.Minute
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
)

var awsAccountID string
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

	// Fetch the Account instance
	currentAcctInstance := &awsv1alpha1.Account{}
	err = r.Client.Get(context.TODO(), request.NamespacedName, currentAcctInstance)
	if err != nil {
		if k8serr.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

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
			SetAccountStatus(reqLogger, currentAcctInstance, "Account pending verification in AWS", awsv1alpha1.AccountPendingVerification, "PendingVerification")
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

			SetAccountStatus(reqLogger, currentAcctInstance, "Account ready to be claimed", awsv1alpha1.AccountReady, "Ready")
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
	if currentAcctInstance.Status.State == "Creating" && diff > createPendTime {
		r.setStatusFailed(reqLogger, currentAcctInstance, "Creation pending for longer then 10 minutes")
	}

	if (currentAcctInstance.Status.State == "") && (currentAcctInstance.Status.Claimed == false) {

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

			totalaccountwatcher.TotalAccountWatcher.Total += 1

			// set state creating if the account was able to create
			SetAccountStatus(reqLogger, currentAcctInstance, "Attempting to create account", awsv1alpha1.AccountCreating, "Creating")
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

			awsAccountID = currentAcctInstance.Spec.AwsAccountID

			// set state creating if the account was already created
			SetAccountStatus(reqLogger, currentAcctInstance, "AWS account already created", awsv1alpha1.AccountCreating, "Creating")
			err = r.Client.Status().Update(context.TODO(), currentAcctInstance)
			if err != nil {
				return reconcile.Result{}, err
			}

		}

		// Get STS credentials so that we can create an aws client with
		creds, credsErr := getStsCredentials(reqLogger, awsSetupClient, awsv1alpha1.AccountOperatorIAMRole, awsAccountID)
		if credsErr != nil {
			stsErrMsg := fmt.Sprintf("Failed to create STS Credentials for account ID %s", awsAccountID)
			reqLogger.Info(stsErrMsg, "Error", credsErr.Error())
			SetAccountStatus(reqLogger, currentAcctInstance, stsErrMsg, awsv1alpha1.AccountFailed, "Failed")
			r.setStatusFailed(reqLogger, currentAcctInstance, "Failed to get sts credentials")
			return reconcile.Result{}, credsErr
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
		_, err = r.BuildSTSUser(reqLogger, SREAWSClient, awsSetupClient, currentAcctInstance, request.Namespace, awsv1alpha1.AccountOperatorIAMRole)
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

		SetAccountStatus(reqLogger, currentAcctInstance, "Account pending AWS limits verification", awsv1alpha1.AccountPendingVerification, "PendingVerification")
		err = r.Client.Status().Update(context.TODO(), currentAcctInstance)
		if err != nil {
			return reconcile.Result{}, err
		}
		reqLogger.Info("Account pending AWS limits verification")
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

// BuildAccount take all parameters required and uses those to make an aws call to CreateAccount. It returns an account ID and and error
func (r *ReconcileAccount) BuildAccount(reqLogger logr.Logger, awsClient awsclient.Client, account *awsv1alpha1.Account) (string, error) {

	reqLogger.Info("Creating Account")

	email := formatAccountEmail(account.Name)
	orgOutput, orgErr := CreateAccount(reqLogger, awsClient, account.Name, email)
	// If it was an api or a limit issue don't modify account and exit if anything else set to failed
	if orgErr != nil {
		switch orgErr {
		case ErrAwsFailedCreateAccount:
			SetAccountStatus(reqLogger, account, "Failed to create AWS Account", awsv1alpha1.AccountFailed, "Failed")
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
		failedToCreateIAMUserMsg := fmt.Sprintf("Failed to create IAM user %s", iamUserName)
		SetAccountStatus(reqLogger, account, failedToCreateIAMUserMsg, awsv1alpha1.AccountFailed, "Failed")
		err := r.Client.Status().Update(context.TODO(), account)
		if err != nil {
			return "", err
		}
		reqLogger.Info(failedToCreateIAMUserMsg)
		return "", userErr
	}

	reqLogger.Info(fmt.Sprintf("Attaching Admin Policy to IAM user %s", iamUserName))
	// Setting user access policy
	_, policyErr := AttachAdminUserPolicy(reqLogger, awsClient, iamUserName)
	if policyErr != nil {
		failedToAttachUserPolicyMsg := fmt.Sprintf("Failed to attach admin policy to IAM user %s", iamUserName)
		SetAccountStatus(reqLogger, account, failedToAttachUserPolicyMsg, awsv1alpha1.AccountFailed, "Failed")
		r.setStatusFailed(reqLogger, account, failedToAttachUserPolicyMsg)
		return "", policyErr
	}

	reqLogger.Info(fmt.Sprintf("Creating Secrets for IAM user %s", iamUserName))
	// create user secrets
	userSecretInfo, userSecretErr := CreateUserAccessKey(reqLogger, awsClient, iamUserName)
	if userSecretErr != nil {
		failedToCreateUserAccessKeyMsg := fmt.Sprintf("Failed to create IAM access key for %s", iamUserName)
		SetAccountStatus(reqLogger, account, failedToCreateUserAccessKeyMsg, awsv1alpha1.AccountFailed, "Failed")
		err := r.Client.Status().Update(context.TODO(), account)
		if err != nil {
			return "", err
		}
		reqLogger.Info(failedToCreateUserAccessKeyMsg)
		return "", userSecretErr
	}

	secretName := account.Name

	// Append to secret name if we're if its the SRE admin user secret
	if iamUserName == iamUserNameSRE {
		secretName = account.Name + "-" + strings.ToLower(iamUserNameSRE)
	}

	userSecretInput := secretInput{
		SecretName:              fmt.Sprintf("%s-secret", secretName),
		NameSpace:               nameSpace,
		awsCredsUserName:        *userSecretInfo.AccessKey.UserName,
		awsCredsSecretIDKey:     *userSecretInfo.AccessKey.AccessKeyId,
		awsCredsSecretAccessKey: *userSecretInfo.AccessKey.SecretAccessKey,
	}
	userSecret := userSecretInput.newSecretforCR()
	createErr := r.Client.Create(context.TODO(), userSecret)
	if createErr != nil {
		failedToCreateUserSecretMsg := fmt.Sprintf("Failed to create secret for IAM user %s", iamUserName)
		SetAccountStatus(reqLogger, account, failedToCreateUserSecretMsg, awsv1alpha1.AccountFailed, "Failed")
		err := r.Client.Status().Update(context.TODO(), account)
		if err != nil {
			return "", err
		}
		reqLogger.Info(failedToCreateUserSecretMsg)
		return "", createErr
	}
	return userSecret.ObjectMeta.Name, nil
}

func (r *ReconcileAccount) setStatusFailed(reqLogger logr.Logger, awsAccount *awsv1alpha1.Account, message string) error {
	reqLogger.Info(message)
	awsAccount.Status.State = "Failed"
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

// CreateIAMUser takes a client and string and creates a IAMuser
func CreateIAMUser(reqLogger logr.Logger, client awsclient.Client, userName string) (*iam.CreateUserOutput, error) {

	// check if username exists for this account
	_, err := client.GetUser(&iam.GetUserInput{
		UserName: aws.String(userName),
	})

	var createUserOutput *iam.CreateUserOutput
	// handle errors
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case iam.ErrCodeNoSuchEntityException:
				createResult, err := client.CreateUser(&iam.CreateUserInput{
					UserName: aws.String(userName),
				})
				if err != nil {
					controllerutils.LogAwsError(reqLogger, "New AWS Error during creation of IAM user (ErrCodeNoSuchEntityException)", nil, err)
					return &iam.CreateUserOutput{}, err
				}
				createUserOutput = createResult
				return createUserOutput, nil

			default:
				controllerutils.LogAwsError(reqLogger, "New AWS Error during creation of IAM user (not ErrCodeNoSuchEntityException)", nil, err)
				return &iam.CreateUserOutput{}, err
			}
		} else {
			return &iam.CreateUserOutput{}, err
		}
	}
	return createUserOutput, nil
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
	result, err := client.CreateAccessKey(&iam.CreateAccessKeyInput{
		UserName: aws.String(userName),
	})

	if err != nil {
		controllerutils.LogAwsError(reqLogger, "New AWS Error while creating user access key", nil, err)
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

func createCase(reqLogger logr.Logger, accountID string, client awsclient.Client) (string, error) {
	// Initialize basic communication body and case subject
	caseCommunicationBody := fmt.Sprintf("Hi AWS, please add this account to Enterprise Support: %s\n\nAlso please apply the following limit increases:\n\n", accountID)
	caseSubject := fmt.Sprintf("Add account %s to Enterprise Support and increase limits", accountID)

	// Create list with supported EC2 instance types
	instanceTypeList := strings.Join(coveredInstanceTypes, ", ")

	// For each supported AWS region append to the communication a request of limit increase
	var i = 0
	for region := range coveredRegions {
		i++
		caseLimitIncreaseBody := fmt.Sprintf("Limit increase request %d\nService: EC2 Instances\nRegion: %s\nPrimary Instance Type: %s\nLimit name: Instance Limit\nNew limit value: %d\n------------\n", i, region, instanceTypeList, caseDesiredInstanceLimit)
		caseCommunicationBody += caseLimitIncreaseBody
	}

	// Per AWS suggestion add final msg to case body
	caseCommunicationBody += "\n\n**Once this is completed please resolve this case, do not set this case to Pending Customer Action.**"

	createCaseInput := support.CreateCaseInput{
		CategoryCode:      aws.String(caseCategoryCode),
		ServiceCode:       aws.String(caseServiceCode),
		IssueType:         aws.String(caseIssueType),
		CommunicationBody: aws.String(caseCommunicationBody),
		Subject:           aws.String(caseSubject),
		SeverityCode:      aws.String(caseSeverity),
		Language:          aws.String(caseLanguage),
	}

	reqLogger.Info("Creating the case", "CaseInput", createCaseInput)

	caseResult, caseErr := client.CreateCase(&createCaseInput)
	if caseErr != nil {
		var returnErr error
		if aerr, ok := caseErr.(awserr.Error); ok {
			switch aerr.Code() {
			case support.ErrCodeCaseCreationLimitExceeded:
				returnErr = ErrAwsCaseCreationLimitExceeded
			case support.ErrCodeInternalServerError:
				returnErr = ErrAwsInternalFailure
			default:
				returnErr = ErrAwsFailedCreateSupportCase
			}

			controllerutils.LogAwsError(reqLogger, "New AWS Error while creating case", returnErr, caseErr)
		}
		return "", returnErr
	}

	reqLogger.Info("Support case created", "AccountID", accountID, "CaseID", caseResult.CaseId)

	return *caseResult.CaseId, nil
}

func checkCaseResolution(reqLogger logr.Logger, caseID string, client awsclient.Client) (bool, error) {
	// Look for the case using the unique ID provided
	describeCasesInput := support.DescribeCasesInput{
		CaseIdList: []*string{
			aws.String(caseID),
		},
	}

	caseResult, caseErr := client.DescribeCases(&describeCasesInput)
	if caseErr != nil {

		var returnErr error
		if aerr, ok := caseErr.(awserr.Error); ok {
			switch aerr.Code() {
			case support.ErrCodeCaseIdNotFound:
				returnErr = ErrAwsSupportCaseIDNotFound
			case support.ErrCodeInternalServerError:
				returnErr = ErrAwsInternalFailure
			default:
				returnErr = ErrAwsFailedDescribeSupportCase
			}
			controllerutils.LogAwsError(reqLogger, "New AWS Error while checking case resolution", returnErr, caseErr)
		}

		return false, returnErr
	}

	// Since we are describing cases based on the unique ID, this list will have only 1 element
	if *caseResult.Cases[0].Status == caseStatusResolved {
		reqLogger.Info(fmt.Sprintf("Case Resolved: %s", caseID))
		return true, nil
	}

	// reqLogger.Info(fmt.Sprintf("Case [%s] not yet Resolved, waiting. Current Status: %s", caseID, *caseResult.Cases[0].Status))
	return false, nil

}
