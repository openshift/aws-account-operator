package account

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/organizations"
	"github.com/aws/aws-sdk-go/service/sts"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_account")

const (
	awsCredsUserName        = "aws_user_name"
	awsCredsSecretIDKey     = "aws_access_key_id"
	awsCredsSecretAccessKey = "aws_secret_access_key"
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
	Client           client.Client
	scheme           *runtime.Scheme
	awsClientBuilder func(kubeClient client.Client, awsAccessID, awsAccessSecret, token, region string) (awsclient.Client, error)
}

// secretInput is a struct that holds data required to create a new secret CR
type secretInput struct {
	SecretName, NameSpace, awsCredsUserName, awsCredsSecretIDKey, awsCredsSecretAccessKey string
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

	if (currentAcctInstance.Status.State == "") && (currentAcctInstance.Status.Claimed == false) {
		// set state creating
		updatedAccount := currentAcctInstance
		updatedAccount.Status.State = "Creating"
		reqLogger.Info("Creating Account")
		err = r.Client.Status().Update(context.TODO(), updatedAccount)
		if err != nil {
			return reconcile.Result{}, err
		}

		// get awsclient to setup  account
		//TODO: Pull from secret
		// awsSetupClient, err := r.getAWSClient(newAwsClientInput{
		// 	secretName: "aws-config",
		// 	nameSpace:  "default",
		// 	awsRegion:  "us-east-1",
		// })

		//Hardcoding pulling from enviroment
		awsSetupClient, err := r.getAWSClient(newAwsClientInput{
			awsCredsSecretIDKey:     os.Getenv(awsCredsSecretIDKey),
			awsCredsSecretAccessKey: os.Getenv(awsCredsSecretAccessKey),
			awsRegion:               "us-east-1",
		})
		if err != nil {
			return reconcile.Result{}, err
		}

		//email := "razevedo" + "+" + rand.String(6) + "@redhat.com"
		email := formatAccountEmail(updatedAccount.Name)
		orgOutput, err := CreateAccount(awsSetupClient, updatedAccount.Name, email)
		// if it failed to create account set the status to failed and return
		if err != nil && err.Error() == "Failed to create account" {
			updatedAccount.Status.State = "Failed"
			err = r.Client.Status().Update(context.TODO(), updatedAccount)
			if err != nil {
				return reconcile.Result{}, err
			}
			failReason := *orgOutput.CreateAccountStatus.FailureReason
			reqLogger.Info(failReason)
			return reconcile.Result{}, nil
		}
		// TODO: add better error handling in the future to handle retry getting a status before returning
		if err != nil {
			return reconcile.Result{}, err
		}

		reqLogger.Info("Account Created")

		// update account cr with accountID from aws
		updatedAccount.Spec.AwsAccountID = *orgOutput.CreateAccountStatus.AccountId
		err = r.Client.Update(context.TODO(), updatedAccount)
		if err != nil {
			return reconcile.Result{}, err
		}

		reqLogger.Info("Creating IAM User")

		// assume role
		time.Sleep(8 * time.Second) // needs time for the account to be ready before we can use roles
		creds, err := getStsCredentials(awsSetupClient, *orgOutput.CreateAccountStatus.AccountId)
		if err != nil {
			reqLogger.Info("Failed to get sts credentials")
			reqLogger.Info(err.Error())
			return reconcile.Result{}, err
		}
		awsAssumedRoleClient, err := r.getAWSClient(newAwsClientInput{
			awsCredsSecretIDKey:     *creds.Credentials.AccessKeyId,
			awsCredsSecretAccessKey: *creds.Credentials.SecretAccessKey,
			awsToken:                *creds.Credentials.SessionToken,
			awsRegion:               "us-east-1"})
		if err != nil {
			reqLogger.Info("Failed to assume role")
			reqLogger.Info(err.Error())
			return reconcile.Result{}, err
		}

		// create iam user
		_, err = CreateIAMUser(awsAssumedRoleClient, "osdManagedAdmin")
		// TODO: better error handling but for now scrap account
		if err != nil {
			updatedAccount.Status.State = "Failed"
			err = r.Client.Status().Update(context.TODO(), updatedAccount)
			if err != nil {
				return reconcile.Result{}, err
			}
			reqLogger.Info("Failed to create user")
			return reconcile.Result{}, nil
		}
		reqLogger.Info("IAM User Created")

		reqLogger.Info("Creating Secrets")
		// create user secrets
		userSecretInfo, err := CreateUserAccessKey(awsAssumedRoleClient, "osdManagedAdmin")
		if err != nil {
			updatedAccount.Status.State = "Failed"
			err = r.Client.Status().Update(context.TODO(), updatedAccount)
			if err != nil {
				return reconcile.Result{}, err
			}
			reqLogger.Info("Failed to create user Access Key + ID")
			return reconcile.Result{}, nil
		}

		// TODO: create secret details
		userSecretInput := secretInput{
			SecretName:              updatedAccount.Name,
			NameSpace:               request.Namespace,
			awsCredsUserName:        *userSecretInfo.AccessKey.UserName,
			awsCredsSecretIDKey:     *userSecretInfo.AccessKey.AccessKeyId,
			awsCredsSecretAccessKey: *userSecretInfo.AccessKey.SecretAccessKey,
		}
		userSecret := userSecretInput.newSecretforCR()
		err = r.Client.Create(context.TODO(), userSecret)
		if err != nil {
			updatedAccount.Status.State = "Failed"
			err = r.Client.Status().Update(context.TODO(), updatedAccount)
			if err != nil {
				return reconcile.Result{}, err
			}
			reqLogger.Info("Failed to create k8s user secret")
			return reconcile.Result{}, nil
		}

		updatedAccount.Spec.IAMUserSecret = userSecret.ObjectMeta.Name
		err = r.Client.Update(context.TODO(), updatedAccount)
		if err != nil {
			return reconcile.Result{}, err
		}

		updatedAccount.Status.State = "Ready"
		err = r.Client.Status().Update(context.TODO(), updatedAccount)
		if err != nil {
			return reconcile.Result{}, err
		}
		reqLogger.Info("Account Ready to be claimed")

		// set state to readys
		// create ec2 instance , delete ec2 instance

	}

	return reconcile.Result{}, nil
}

// getAWSClient generates an awsclient
// function must include region
// Pass in token if sessions requires a token
// if it includes a secretName and nameSpace it will create credentials from that secret data
// If it includes awsCredsSecretIDKey and awsCredsSecretAccessKey it will build credentials from those
func (r *ReconcileAccount) getAWSClient(input newAwsClientInput) (awsclient.Client, error) {

	// error is region is not included
	if input.awsRegion == "" {
		return nil, fmt.Errorf("getAWSClient:NoRegion: %v", input.awsRegion)
	}

	if input.secretName != "" && input.nameSpace != "" {
		secret := &corev1.Secret{}
		err := r.Client.Get(context.TODO(),
			types.NamespacedName{
				Name:      input.secretName,
				Namespace: input.nameSpace,
			},
			secret)
		if err != nil {
			return nil, err
		}
		accessKeyID, ok := secret.Data[awsCredsSecretIDKey]
		if !ok {
			return nil, fmt.Errorf("AWS credentials secret %v did not contain key %v",
				input.secretName, awsCredsSecretIDKey)
		}
		secretAccessKey, ok := secret.Data[awsCredsSecretAccessKey]
		if !ok {
			return nil, fmt.Errorf("AWS credentials secret %v did not contain key %v",
				input.secretName, awsCredsSecretAccessKey)
		}

		awsClient, err := r.awsClientBuilder(r.Client, string(accessKeyID), string(secretAccessKey), input.awsToken, input.awsRegion)
		if err != nil {
			return nil, err
		}
		return awsClient, nil
	}

	if input.awsCredsSecretIDKey == "" && input.awsCredsSecretAccessKey != "" {
		return nil, fmt.Errorf("getAWSClient: NoAwsCredentials or Secret %v", input)
	}

	awsClient, err := r.awsClientBuilder(r.Client, input.awsCredsSecretIDKey, input.awsCredsSecretAccessKey, input.awsToken, input.awsRegion)
	if err != nil {
		return nil, err
	}
	return awsClient, nil
}

// getAwsAccountId searches the list of accounts in the orgnaization and returns the
// AWS account ID for the account which matches the AWS account name
func getAwsAccountID(client awsclient.Client, awsAccountName string) (*string, error) {
	var id *string

	var nextToken *string

	// Ensure we paginate through the account list
	for {
		awsAccountList, err := client.ListAccounts(&organizations.ListAccountsInput{NextToken: nextToken})
		if err != nil {
			return aws.String(""), fmt.Errorf("Error getting a list of accounts: %s ", err.Error())
		}
		for _, accountStatus := range awsAccountList.Accounts {
			if *accountStatus.Name == awsAccountName {
				if id != nil {
					return aws.String(""), fmt.Errorf("more than one account with the name: %s found", *id)
				}
				id = accountStatus.Id
			}
		}
		if awsAccountList.NextToken != nil {
			nextToken = awsAccountList.NextToken
		} else {
			break
		}
	}

	// Check to see if we found an account
	if id == nil {
		return id, fmt.Errorf("No account named %s", awsAccountName)
	}
	return id, nil
}

// CreateAccount creates an AWS account for the specified accountName and accountEmail in the orgnization
func CreateAccount(client awsclient.Client, accountName, accountEmail string) (*organizations.DescribeCreateAccountStatusOutput, error) {

	createInput := organizations.CreateAccountInput{
		AccountName: aws.String(accountName),
		Email:       aws.String(accountEmail),
	}

	createOutput, err := client.CreateAccount(&createInput)
	if err != nil {
		return &organizations.DescribeCreateAccountStatusOutput{}, err
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
			return &organizations.DescribeCreateAccountStatusOutput{}, errors.New("Failed to create account")
		}

		if createStatus != "IN_PROGRESS" {
			break
		}

	}

	return accountStatus, nil
}

// CreateIAMUser takes a client and string and creates a IAMuser
func CreateIAMUser(client awsclient.Client, userName string) (*iam.CreateUserOutput, error) {

	// check if username exists for this account
	_, err := client.GetUser(&iam.GetUserInput{
		UserName: aws.String(userName),
	})

	awserr, ok := err.(awserr.Error)

	if err != nil && awserr.Code() != iam.ErrCodeNoSuchEntityException {
		return &iam.CreateUserOutput{}, err
	}

	var createUserOutput *iam.CreateUserOutput
	if ok && awserr.Code() == iam.ErrCodeNoSuchEntityException {
		createResult, err := client.CreateUser(&iam.CreateUserInput{
			UserName: aws.String(userName),
		})
		if err != nil {
			return &iam.CreateUserOutput{}, err
		}
		createUserOutput = createResult
	}

	return createUserOutput, nil
}

// CreateUserAccessKey creates an IAM user's secret and returns the accesskey id and secret for that user in a aws.CreateAccessKeyOutput struct
func CreateUserAccessKey(client awsclient.Client, userName string) (*iam.CreateAccessKeyOutput, error) {

	// Create new access key for user
	result, err := client.CreateAccessKey(&iam.CreateAccessKeyInput{
		UserName: aws.String(userName),
	})
	if err != nil {
		return &iam.CreateAccessKeyOutput{}, errors.New("Error creating access key")
	}

	return result, nil
}

// getStsCredentials returns sts credentials for the specified account ARN
func getStsCredentials(client awsclient.Client, awsAccountID string) (*sts.AssumeRoleOutput, error) {
	// Use the role session name to uniquely identify a session when the same role
	// is assumed by different principals or for different reasons.
	var roleSessionName = "awsAccountOperator"
	// Default duration in seconds of the session token 3600. We need to have the roles policy
	// changed if we want it to be longer than 3600 seconds
	var roleSessionDuration int64 = 3600
	// The role ARN made up of the account number and the role which is the default role name
	// created in child accounts
	var roleArn = fmt.Sprintf("arn:aws:iam::%s:role/OrganizationAccountAccessRole", awsAccountID)
	// Build input for AssumeRole
	assumeRoleInput := sts.AssumeRoleInput{
		DurationSeconds: &roleSessionDuration,
		RoleArn:         &roleArn,
		RoleSessionName: &roleSessionName,
	}

	assumeRoleOutput, err := client.AssumeRole(&assumeRoleInput)
	if err != nil {
		return &sts.AssumeRoleOutput{}, err
	}

	return assumeRoleOutput, nil
}

func (input secretInput) newSecretforCR() *corev1.Secret {
	return &corev1.Secret{
		Type: "Opaque",
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      input.SecretName + "-secret",
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

// create a ec2 instance

//result requeue after duration.
