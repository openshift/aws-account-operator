package account

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/organizations"
	"github.com/aws/aws-sdk-go/service/sts"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_account")

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
	awsClientBuilder func(kubeClient client.Client, awsAccessID, awsAccessSecret, region string) (awsclient.Client, error)
}

// Reconcile reads that state of the cluster for a Account object and makes changes based on the state read
// and what is in the Account.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileAccount) Reconcile(request reconcile.Request) (reconcile.Result, error) {

	// pull from name secrets
	//	awsClient, err := r.getAWSClient("id", "secret", "region")
	//	if err != nil {
	//		return reconcile.Result{}, err
	//	}

	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling Account")

	// Fetch the Account instance
	currentAcct := &awsv1alpha1.Account{}
	err := r.Client.Get(context.TODO(), request.NamespacedName, currentAcct)
	if err != nil {
		if k8serr.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// see if instance state is blank if it is , set state pending ,
	// create account , create iam user, create ec2 instance , delete ec2 instance
	// update the account cr with accountID , create secret for IAm ,
	// set the secret string to that name
	// set state to ready
	//reqLogger.Info("Current Account: ", currentAcct)

	return reconcile.Result{}, nil
}

// getAWSClient generates an awsclient
func (r *ReconcileAccount) getAWSClient(awsAccessID, awsAccessSecret, region string) (awsclient.Client, error) {

	awsClient, err := r.awsClientBuilder(r.Client, awsAccessID, awsAccessSecret, region)
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
			fmt.Println("Error getting a list of accounts")
		}
		for _, accountStatus := range awsAccountList.Accounts {
			if *accountStatus.Name == awsAccountName {
				id = accountStatus.Id
				break
			}
		}
		if awsAccountList.NextToken != nil && id == nil {
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

		accountStatus := *status.CreateAccountStatus.State

		if accountStatus == "FAILED" {
			return &organizations.DescribeCreateAccountStatusOutput{}, errors.New("Failed to create account")
		}

		if accountStatus != "IN_PROGRESS" {
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
		return nil, err
	}

	var createUserOutput *iam.CreateUserOutput
	if ok && awserr.Code() == iam.ErrCodeNoSuchEntityException {
		createResult, err := client.CreateUser(&iam.CreateUserInput{
			UserName: aws.String(userName),
		})
		if err != nil {
			return nil, err
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
		return nil, err
	}

	return assumeRoleOutput, nil
}

// create a ec2 instance

// create a secret

// read from a secret
