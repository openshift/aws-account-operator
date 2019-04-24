package account

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/organizations"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	controllerutils "github.com/openshift/aws-account-operator/pkg/controller/utils"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	iamUserName             = "osdManagedAdmin"
	awsSecretName           = "aws-account-operator-credentials"
	awsAMI                  = "ami-000db10762d0c4c05"
	awsInstanceType         = "t2.micro"
	createPendTime          = 10 * time.Minute
	// AccountPending indicates an account is pending
	AccountPending = "Pending"
	// AccountCreating indicates an account is being created
	AccountCreating = "Creating"
	// AccountFailed indicates account creation has failed
	AccountFailed = "Failed"
	// AccountReady indicates account creation is ready
	AccountReady = "Ready"
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

	// see if in creating for longer then 10 minutes
	now := time.Now()
	diff := now.Sub(currentAcctInstance.ObjectMeta.CreationTimestamp.Time)
	if currentAcctInstance.Status.State == "Creating" && diff > createPendTime {
		r.setStatusFailed(reqLogger, currentAcctInstance, "Creation pending for longer then 10 minutes")
	}

	if (currentAcctInstance.Status.State == "") && (currentAcctInstance.Status.Claimed == false) {
		// set state creating
		setAccountClaimStatus(reqLogger, currentAcctInstance, "Attempting to create account", awsv1alpha1.AccountCreating, "Creating")
		err = r.Client.Status().Update(context.TODO(), currentAcctInstance)
		if err != nil {
			return reconcile.Result{}, err
		}

		awsSetupClient, err := r.getAWSClient(newAwsClientInput{
			secretName: awsSecretName,
			nameSpace:  request.Namespace,
			awsRegion:  "us-east-1",
		})
		if err != nil {
			reqLogger.Info(err.Error())
			r.setStatusFailed(reqLogger, currentAcctInstance, "Failed to get AWS client")
			return reconcile.Result{}, err
		}

		// Build Aws Account
		accountID, err := r.BuildAccount(reqLogger, awsSetupClient, currentAcctInstance)
		if err != nil {
			r.setStatusFailed(reqLogger, currentAcctInstance, "Failed to build account")
			return reconcile.Result{}, err
		}

		// update account cr with accountID from aws
		currentAcctInstance.Spec.AwsAccountID = accountID
		err = r.Client.Update(context.TODO(), currentAcctInstance)
		if err != nil {
			return reconcile.Result{}, err
		}

		// Get STS credentials so that we can create an aws client with
		creds, credsErr := getStsCredentials(awsSetupClient, accountID)
		if credsErr != nil {
			reqLogger.Info(err.Error())
			setAccountClaimStatus(reqLogger, currentAcctInstance, "Failed to create account", awsv1alpha1.AccountFailed, "Failed")
			r.setStatusFailed(reqLogger, currentAcctInstance, "Failed to get sts credentials")
			return reconcile.Result{}, credsErr
		}

		awsAssumedRoleClient, err := r.getAWSClient(newAwsClientInput{
			awsCredsSecretIDKey:     *creds.Credentials.AccessKeyId,
			awsCredsSecretAccessKey: *creds.Credentials.SecretAccessKey,
			awsToken:                *creds.Credentials.SessionToken,
			awsRegion:               "us-east-1"})
		if err != nil {
			reqLogger.Info(err.Error())
			r.setStatusFailed(reqLogger, currentAcctInstance, "Failed to assume role")
			return reconcile.Result{}, err
		}

		secretName, err := r.BuildUser(reqLogger, awsAssumedRoleClient, currentAcctInstance, request.Namespace)
		if err != nil {
			r.setStatusFailed(reqLogger, currentAcctInstance, "Failed to build user")
			return reconcile.Result{}, err
		}
		currentAcctInstance.Spec.IAMUserSecret = secretName
		err = r.Client.Update(context.TODO(), currentAcctInstance)
		if err != nil {
			return reconcile.Result{}, err
		}

		// create ec2 instance , delete ec2 instance [WIP]
		err = r.BulidandDestroyEC2Instances(reqLogger, awsAssumedRoleClient)
		if err != nil {
			r.setStatusFailed(reqLogger, currentAcctInstance, "Failed to build and destroy ec2 instances")
			return reconcile.Result{}, err
		}

		setAccountClaimStatus(reqLogger, currentAcctInstance, "Account ready to be claimed", awsv1alpha1.AccountReady, "Ready")
		err = r.Client.Status().Update(context.TODO(), currentAcctInstance)
		if err != nil {
			return reconcile.Result{}, err
		}
		reqLogger.Info("Account Ready to be claimed")
	}

	return reconcile.Result{}, nil
}

// getAWSClient generates an awsclient
// function must include region
// Pass in token if sessions requires a token
// if it includes a secretName and nameSpace it will create credentials from that secret data
// If it includes awsCredsSecretIDKey and awsCredsSecretAccessKey it will build credentials from those
func (r *ReconcileAccount) getAWSClient(input newAwsClientInput) (awsclient.Client, error) {

	// error if region is not included
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

// BuildAccount take all parameters required and uses those to make an aws call to CreateAccount. It returns an account ID and and error
func (r *ReconcileAccount) BuildAccount(reqLogger logr.Logger, awsClient awsclient.Client, account *awsv1alpha1.Account) (string, error) {

	reqLogger.Info("Creating Account")

	email := formatAccountEmail(account.Name)
	orgOutput, orgErr := CreateAccount(awsClient, account.Name, email)
	// if it failed to create account set the status to failed and return
	if orgErr != nil && orgErr.Error() == "Failed to create account" {
		setAccountClaimStatus(reqLogger, account, "Failed to create account", awsv1alpha1.AccountFailed, "Failed")
		err := r.Client.Status().Update(context.TODO(), account)
		if err != nil {
			return "", err
		}

		failReason := *orgOutput.CreateAccountStatus.FailureReason
		reqLogger.Info(failReason)
		return "", orgErr
	}
	// TODO: add better error handling in the future to handle retry getting a status before returning
	if orgErr != nil {
		return "", orgErr
	}

	reqLogger.Info("Account Created")

	return *orgOutput.CreateAccountStatus.AccountId, nil
}

// BuildUser takea all parameters required to create a user, user secret
func (r *ReconcileAccount) BuildUser(reqLogger logr.Logger, awsClient awsclient.Client, account *awsv1alpha1.Account, nameSpace string) (string, error) {
	reqLogger.Info("IAM User Created")

	// create iam user
	_, userErr := CreateIAMUser(awsClient, iamUserName)
	// TODO: better error handling but for now scrap account
	if userErr != nil {
		setAccountClaimStatus(reqLogger, account, "Failed to create account", awsv1alpha1.AccountFailed, "Failed")
		err := r.Client.Status().Update(context.TODO(), account)
		if err != nil {
			return "", err
		}
		reqLogger.Info("Failed to create user")
		return "", userErr
	}

	reqLogger.Info("Creating Secrets")
	// create user secrets
	userSecretInfo, userSecretErr := CreateUserAccessKey(awsClient, iamUserName)
	if userSecretErr != nil {
		setAccountClaimStatus(reqLogger, account, "Failed to create account", awsv1alpha1.AccountFailed, "Failed")
		err := r.Client.Status().Update(context.TODO(), account)
		if err != nil {
			return "", err
		}
		reqLogger.Info("Failed to create user Access Key + ID")
		return "", userSecretErr
	}

	userSecretInput := secretInput{
		SecretName:              account.Name,
		NameSpace:               nameSpace,
		awsCredsUserName:        *userSecretInfo.AccessKey.UserName,
		awsCredsSecretIDKey:     *userSecretInfo.AccessKey.AccessKeyId,
		awsCredsSecretAccessKey: *userSecretInfo.AccessKey.SecretAccessKey,
	}
	userSecret := userSecretInput.newSecretforCR()
	createErr := r.Client.Create(context.TODO(), userSecret)
	if createErr != nil {
		setAccountClaimStatus(reqLogger, account, "Failed to create account", awsv1alpha1.AccountFailed, "Failed")
		err := r.Client.Status().Update(context.TODO(), account)
		if err != nil {
			return "", err
		}
		reqLogger.Info("Failed to create k8s user secret")
		return "", createErr
	}
	return userSecret.ObjectMeta.Name, nil
}

//BulidandDestroyEC2Instances runs and ec2 instance and terminates it
func (r *ReconcileAccount) BulidandDestroyEC2Instances(reqLogger logr.Logger, awsClient awsclient.Client) error {
	//wait a bit for account to be ready to create

	//Create instance
	reqLogger.Info("Creating EC2 Instance")
	instanceID, err := CreateEC2Instance(awsClient)
	if err != nil {
		return err
	}

	// Wait till instance is running
	var DescError error
	for i := 0; i < 300; i++ {
		var code int
		time.Sleep(1 * time.Second)
		code, DescError = DescribeEC2Instances(awsClient)
		if code == 16 {
			reqLogger.Info("EC2 Instance Running")
			break
		}

	}

	if DescError != nil {
		return errors.New("Could not get ec3 instance state")
	}

	// Terminate Instance
	reqLogger.Info("Terminating EC2 Instance")
	err = DeleteEC2Instance(awsClient, instanceID)
	if err != nil {
		return err
	}

	reqLogger.Info("EC2 Instance Terminated")

	return nil
}

func (r *ReconcileAccount) setStatusFailed(reqLogger logr.Logger, awsAccount *awsv1alpha1.Account, message string) {
	reqLogger.Info(message)
	awsAccount.Status.State = "Failed"
	_ = r.Client.Status().Update(context.TODO(), awsAccount)
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

	assumeRoleOutput := &sts.AssumeRoleOutput{}
	var err error
	for i := 0; i < 100; i++ {
		time.Sleep(500 * time.Millisecond)
		assumeRoleOutput, err = client.AssumeRole(&assumeRoleInput)
		if err == nil {
			break
		}
	}
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

//CreateEC2Instance creates ec2 instance and returns its instance ID
func CreateEC2Instance(client awsclient.Client) (string, error) {
	// Create EC2 service client

	var instanceID string
	var runErr error
	attempt := 1
	for i := 0; i < 300; i++ {
		time.Sleep(time.Duration(attempt*5) * time.Second)
		attempt++
		if attempt%5 == 0 {
			attempt = attempt * 2
		}
		// Specify the details of the instance that you want to create.
		runResult, runErr := client.RunInstances(&ec2.RunInstancesInput{
			// An Amazon Linux AMI ID for t2.micro instances in the us-west-2 region
			ImageId:      aws.String(awsAMI),
			InstanceType: aws.String(awsInstanceType),
			MinCount:     aws.Int64(1),
			MaxCount:     aws.Int64(1),
		})
		if runErr == nil {
			instanceID = *runResult.Instances[0].InstanceId
			break
		}
	}

	if runErr != nil {
		return "", runErr
	}

	return instanceID, nil

}

//DescribeEC2Instances returns the InstanceState code
func DescribeEC2Instances(client awsclient.Client) (int, error) {
	// States and codes
	// 0 : pending
	// 16 : running
	// 32 : shutting-down
	// 48 : terminated
	// 64 : stopping
	// 80 : stopped

	result, err := client.DescribeInstanceStatus(nil)
	if err != nil {
		return 0, err
	}

	if len(result.InstanceStatuses) > 1 {
		return 0, errors.New("More than one EC2 instance found")
	}

	if len(result.InstanceStatuses) == 0 {
		return 0, errors.New("No EC2 instances found")
	}
	return int(*result.InstanceStatuses[0].InstanceState.Code), nil
}

//DeleteEC2Instance terminates the ec2 instance from the instanceID provided
func DeleteEC2Instance(client awsclient.Client, instanceID string) error {
	_, err := client.TerminateInstances(&ec2.TerminateInstancesInput{
		InstanceIds: aws.StringSlice([]string{instanceID}),
	})
	if err != nil {
		return err
	}

	return nil
}

func setAccountClaimStatus(reqLogger logr.Logger, awsAccount *awsv1alpha1.Account, message string, ctype awsv1alpha1.AccountConditionType, state string) {
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
