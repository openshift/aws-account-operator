package awsfederatedaccountaccess

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	controllerutils "github.com/openshift/aws-account-operator/pkg/controller/utils"

	corev1 "k8s.io/api/core/v1"
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

// Custom errors

const (
	AWSFederatedAccountAccessFinalizer = "finalizer.aws.managed.openshift.io"
)

// ErrFederatedAccessRoleNotFound indicates the role requested by AWSFederatedAccountAccess Cr was not found as a AWSFederatedRole Cr
var ErrFederatedAccessRoleNotFound = errors.New("FederatedAccessRoleNotFound")

// ErrFederatedAccessRoleFailedCreate indicates that the AWSFederatedRole requested failed to be created in the account requested by the AWSFederatedAccountAccess CR
var ErrFederatedAccessRoleFailedCreate = errors.New("FederatedAccessRoleFailedCreate")

var log = logf.Log.WithName("controller_awsfederatedaccountaccess")

// Add creates a new AWSFederatedAccountAccess Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileAWSFederatedAccountAccess{
		client: mgr.GetClient(),
		scheme: mgr.GetScheme(),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("awsfederatedaccountaccess-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource AWSFederatedAccountAccess
	err = c.Watch(&source.Kind{Type: &awsv1alpha1.AWSFederatedAccountAccess{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileAWSFederatedAccountAccess implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileAWSFederatedAccountAccess{}

// ReconcileAWSFederatedAccountAccess reconciles a AWSFederatedAccountAccess object
type ReconcileAWSFederatedAccountAccess struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a AWSFederatedAccountAccess object and makes changes based on the state read
// and what is in the AWSFederatedAccountAccess.Spec
func (r *ReconcileAWSFederatedAccountAccess) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling AWSFederatedAccountAccess")

	// Fetch the AWSFederatedAccountAccess instance
	currentFAA := &awsv1alpha1.AWSFederatedAccountAccess{}
	err := r.client.Get(context.TODO(), request.NamespacedName, currentFAA)
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

	// Add finalizer to the CR in case it's not present (e.g. old accounts)
	if !contains(currentFAA.GetFinalizers(), AWSFederatedAccountAccessFinalizer) {
		err := r.addFinalizer(reqLogger, currentFAA)
		if err != nil {
			return reconcile.Result{}, err
		}

		return reconcile.Result{}, nil
	}

	if currentFAA.DeletionTimestamp != nil {
		if contains(currentFAA.GetFinalizers(), AWSFederatedAccountAccessFinalizer) {
			// Only remove roles and policies if the faa has an account reference
			if currentFAA.Spec.AccountReference != "" {
				err := r.finalizeAWSFederatedAccountAccess(reqLogger, currentFAA)
				if err != nil {
					// If the finalize/cleanup process fails for an account we don't want to return
					// we will flag the faac with the Failed Reuse condition, and with state = Failed

				}
			}

			// Remove finalizer to unlock deletion of the currentFAA
			err = r.removeFinalizer(reqLogger, currentFAA, AWSFederatedAccountAccessFinalizer)
			if err != nil {
				return reconcile.Result{}, err
			}

		}
		return reconcile.Result{}, nil
	}

	// If the state is ready or failed don't do anything
	if currentFAA.Status.State == awsv1alpha1.AWSFederatedAccountStateReady || currentFAA.Status.State == awsv1alpha1.AWSFederatedAccountStateFailed {
		return reconcile.Result{}, nil
	}

	// Get a list of all available roles
	federatedRoleList := &awsv1alpha1.AWSFederatedRoleList{}
	if r.client.List(context.TODO(), &client.ListOptions{}, federatedRoleList); err != nil {
		return reconcile.Result{}, err
	}

	// Create map to make finding role easier
	roleMap := make(map[string]awsv1alpha1.AWSFederatedRole)
	for _, role := range federatedRoleList.Items {
		roleMap[role.Name] = role
	}

	// See if requested role exists
	if _, ok := roleMap[currentFAA.Spec.AWSFederatedRoleName]; !ok {
		SetStatuswithCondition(currentFAA, "Requested role does not exist", awsv1alpha1.AWSFederatedAccountFailed, awsv1alpha1.AWSFederatedAccountStateFailed)
		reqLogger.Error(ErrFederatedAccessRoleNotFound, fmt.Sprintf("Resquested role '%s' not found", currentFAA.Spec.AWSFederatedRoleName))

		err := r.client.Status().Update(context.TODO(), currentFAA)
		if err != nil {
			reqLogger.Error(err, fmt.Sprintf("Status update for %s failed", currentFAA.Name))
			return reconcile.Result{}, err
		}

		return reconcile.Result{}, nil

	}

	// Use the account name reference to get the correct secret name
	secretName := currentFAA.Spec.AccountReference + "-secret"

	// Get aws client
	awsClient, err := awsclient.GetAWSClient(r.client, awsclient.NewAwsClientInput{
		SecretName: secretName,
		NameSpace:  awsv1alpha1.AccountCrNamespace,
		AwsRegion:  "us-east-1",
	})
	if err != nil {
		awsClientErr := fmt.Sprintf("Unable to create aws client for region ")
		reqLogger.Error(err, awsClientErr)
		return reconcile.Result{}, err
	}

	// Here create the custom policy in the cluster account
	err = r.createOrUpdateIAMPolicy(awsClient, roleMap[currentFAA.Spec.AWSFederatedRoleName], *currentFAA)
	if err != nil {
		// if we were unable to create the policy fail this CR.
		SetStatuswithCondition(currentFAA, "Failed to create custom policy", awsv1alpha1.AWSFederatedAccountFailed, awsv1alpha1.AWSFederatedAccountStateFailed)
		reqLogger.Error(err, fmt.Sprintf("Unable to create policy resquested by '%s'", currentFAA.Name))

		err := r.client.Status().Update(context.TODO(), currentFAA)
		if err != nil {
			reqLogger.Error(err, fmt.Sprintf("Status update for %s failed", currentFAA.Name))
			return reconcile.Result{}, err
		}

		return reconcile.Result{}, nil
	}

	// Create role and apply custom policys and awsmanagedpolicies
	err = r.createOrUpdateIAMRole(awsClient, roleMap[currentFAA.Spec.AWSFederatedRoleName], *currentFAA)
	if err != nil {
		SetStatuswithCondition(currentFAA, "Failed to create role", awsv1alpha1.AWSFederatedAccountFailed, awsv1alpha1.AWSFederatedAccountStateFailed)
		reqLogger.Error(ErrFederatedAccessRoleFailedCreate, fmt.Sprintf("Unable to create role requested by '%s'", currentFAA.Name), "AWS ERROR: ", err)

		err := r.client.Status().Update(context.TODO(), currentFAA)
		if err != nil {
			reqLogger.Error(err, fmt.Sprintf("Status update for %s failed", currentFAA.Name))
			return reconcile.Result{}, err
		}

		return reconcile.Result{}, nil
	}

	accountCR := &awsv1alpha1.Account{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: currentFAA.Spec.AccountReference, Namespace: awsv1alpha1.AccountCrNamespace}, accountCR)
	if err != nil {
		return reconcile.Result{}, err
	}

	accountID := accountCR.Spec.AwsAccountID
	// Add requested aws managed policies to the role
	awsManagedPolicyNames := []string{}
	// Add all aws managed policy names to a array
	for _, policy := range roleMap[currentFAA.Spec.AWSFederatedRoleName].Spec.AWSManagedPolicies {
		awsManagedPolicyNames = append(awsManagedPolicyNames, policy)
	}
	// Get policy arns for managed policies
	policyArns := createPolicyArns(accountID, awsManagedPolicyNames, true)
	// Get custom policy arns
	customPolicy := []string{roleMap[currentFAA.Spec.AWSFederatedRoleName].Spec.AWSCustomPolicy.Name}
	customerPolArns := createPolicyArns(accountID, customPolicy, false)
	policyArns = append(policyArns, customerPolArns[0])

	// Attach the requested policy to the newly created role
	err = r.attachIAMPolices(awsClient, currentFAA.Spec.AWSFederatedRoleName, policyArns)
	if err != nil {
		//TODO() role should be deleted here so that we leave nothing behind.

		SetStatuswithCondition(currentFAA, "Failed to attach policies to role", awsv1alpha1.AWSFederatedAccountFailed, awsv1alpha1.AWSFederatedAccountStateFailed)
		reqLogger.Error(err, fmt.Sprintf("Failed to attach policies to role requested by '%s'", currentFAA.Name))
		err := r.client.Status().Update(context.TODO(), currentFAA)
		if err != nil {
			reqLogger.Error(err, fmt.Sprintf("Status update for %s failed", currentFAA.Name))
			return reconcile.Result{}, err
		}

		return reconcile.Result{}, nil
	}
	// Mark AWSFederatedAccountAccess CR as Ready.
	SetStatuswithCondition(currentFAA, "Account Access Ready", awsv1alpha1.AWSFederatedAccountReady, awsv1alpha1.AWSFederatedAccountStateReady)
	reqLogger.Info(fmt.Sprintf("Successfully applied %s", currentFAA.Name))
	err = r.client.Status().Update(context.TODO(), currentFAA)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Status update for %s failed", currentFAA.Name))
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// createIAMPolicy creates the IAM policys in AWSFederatedRole inside of our cluster account
func (r *ReconcileAWSFederatedAccountAccess) createIAMPolicy(awsClient awsclient.Client, afr awsv1alpha1.AWSFederatedRole, afaa awsv1alpha1.AWSFederatedAccountAccess) (*iam.Policy, error) {
	// Same struct from the afr.Spec.AWSCustomPolicy.Statements , but with json tags as captials due to requirements for the policydoc
	type awsStatement struct {
		Effect    string                 `json:"Effect"`
		Action    []string               `json:"Action"`
		Resource  []string               `json:"Resource,omitempty"`
		Principal *awsv1alpha1.Principal `json:"Principal,omitempty"`
	}

	statements := []awsStatement{}

	for _, sm := range afr.Spec.AWSCustomPolicy.Statements {
		var a awsStatement = awsStatement(sm)
		statements = append(statements, a)
	}

	// Create an aws policydoc formated struct
	policyDoc := struct {
		Version   string
		Statement []awsStatement
	}{
		Version:   "2012-10-17",
		Statement: statements,
	}

	// Marshal policydoc to json
	jsonPolicyDoc, err := json.Marshal(&policyDoc)
	if err != nil {
		return &iam.Policy{}, fmt.Errorf("Error marshalling jsonPolicy doc : Error %s", err.Error())
	}

	output, err := awsClient.CreatePolicy(&iam.CreatePolicyInput{
		PolicyName:     aws.String(afr.Spec.AWSCustomPolicy.Name),
		Description:    aws.String(afr.Spec.AWSCustomPolicy.Description),
		PolicyDocument: aws.String(string(jsonPolicyDoc)),
	})
	if err != nil {
		return output.Policy, fmt.Errorf("Error creating awsCustomPolicy %s for AWSFederatedRole %s \n AWS Error %s", afr.Spec.AWSCustomPolicy.Name, afr.Name, err.Error())
	}

	return output.Policy, nil
}

func (r *ReconcileAWSFederatedAccountAccess) createIAMRole(awsClient awsclient.Client, afr awsv1alpha1.AWSFederatedRole, afaa awsv1alpha1.AWSFederatedAccountAccess) error {
	type awsStatement struct {
		Effect    string                 `json:"Effect"`
		Action    []string               `json:"Action"`
		Resource  []string               `json:"Resource,omitempty"`
		Principal *awsv1alpha1.Principal `json:"Principal,omitempty"`
	}

	assumeRolePolicyDoc := struct {
		Version   string
		Statement []awsStatement
	}{
		Version: "2012-10-17",
		Statement: []awsStatement{{
			Effect: "Allow",
			Action: []string{"sts:AssumeRole"},
			Principal: &awsv1alpha1.Principal{
				AWS: afaa.Spec.ExternalCustomerAWSIAMARN,
			},
		}},
	}

	// Marshal assumeRolePolicyDoc to json
	jsonAssumeRolePolicyDoc, err := json.Marshal(&assumeRolePolicyDoc)
	if err != nil {
		return fmt.Errorf("Error marshalling jsonPolicy doc : Error %s", err.Error())
	}

	_, err = awsClient.CreateRole(&iam.CreateRoleInput{
		RoleName:                 aws.String(afr.Name),
		Description:              aws.String(afr.Spec.RoleDescription),
		AssumeRolePolicyDocument: aws.String(string(jsonAssumeRolePolicyDoc)),
	})
	if err != nil {
		return err
	}

	return nil
}

func (r *ReconcileAWSFederatedAccountAccess) createOrUpdateIAMPolicy(awsClient awsclient.Client, afr awsv1alpha1.AWSFederatedRole, afaa awsv1alpha1.AWSFederatedAccountAccess) error {

	policy, err := r.createIAMPolicy(awsClient, afr, afaa)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == "EntityAlreadyExists" {

				// If the Role already exists, delete it and recreate it
				_, err = awsClient.DeletePolicy(&iam.DeletePolicyInput{PolicyArn: policy.Arn})
				_, err = r.createIAMPolicy(awsClient, afr, afaa)
			}
		}
	}

	return err
}

func (r *ReconcileAWSFederatedAccountAccess) createOrUpdateIAMRole(awsClient awsclient.Client, afr awsv1alpha1.AWSFederatedRole, afaa awsv1alpha1.AWSFederatedAccountAccess) error {

	err := r.createIAMRole(awsClient, afr, afaa)
	if err != nil {

		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == "EntityAlreadyExists" {

				// If the Role already exists, delete it and recreate it
				_, err = awsClient.DeleteRole(&iam.DeleteRoleInput{RoleName: aws.String(afr.Name)})
				err = r.createIAMRole(awsClient, afr, afaa)
			}
		}
	}

	return err
}

func (r *ReconcileAWSFederatedAccountAccess) attachIAMPolices(awsClient awsclient.Client, roleName string, policyArns []string) error {
	for _, pol := range policyArns {
		_, err := awsClient.AttachRolePolicy(&iam.AttachRolePolicyInput{
			PolicyArn: aws.String(pol),
			RoleName:  aws.String(roleName),
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// Pass in the account id of the account where you the policies live.
func createPolicyArns(accountID string, policyNames []string, awsManaged bool) []string {
	awsPolicyArnPrefix := ""

	if awsManaged == true {
		awsPolicyArnPrefix = "arn:aws:iam::aws:policy/"
	} else {
		awsPolicyArnPrefix = fmt.Sprintf("arn:aws:iam::%s:policy/", accountID)
	}
	policyArns := []string{}
	for _, policy := range policyNames {
		policyArns = append(policyArns, fmt.Sprintf("%s%s", awsPolicyArnPrefix, policy))
	}
	return policyArns
}

// SetStatuswithCondition sets the status of an account
func SetStatuswithCondition(afaa *awsv1alpha1.AWSFederatedAccountAccess, message string, ctype awsv1alpha1.AWSFederatedAccountAccessConditionType, state awsv1alpha1.AWSFederatedAccountAccessState) {
	afaa.Status.Conditions = controllerutils.SetAWSFederatedAccountAccessCondition(
		afaa.Status.Conditions,
		ctype,
		corev1.ConditionTrue,
		string(state),
		message,
		controllerutils.UpdateConditionNever)
	afaa.Status.State = state
}

func (r *ReconcileAWSFederatedAccountAccess) addFinalizer(reqLogger logr.Logger, AWSFederatedAccountAccess *awsv1alpha1.AWSFederatedAccountAccess) error {
	reqLogger.Info("Adding Finalizer for the AWSFederatedAccountAccess")
	AWSFederatedAccountAccess.SetFinalizers(append(AWSFederatedAccountAccess.GetFinalizers(), AWSFederatedAccountAccessFinalizer))

	// Update CR
	err := r.client.Update(context.TODO(), AWSFederatedAccountAccess)
	if err != nil {
		reqLogger.Error(err, "Failed to update AWSFederatedAccountAccess with finalizer")
		return err
	}
	return nil
}

func (r *ReconcileAWSFederatedAccountAccess) removeFinalizer(reqLogger logr.Logger, AWSFederatedAccountAccess *awsv1alpha1.AWSFederatedAccountAccess, finalizerName string) error {
	reqLogger.Info("Removing Finalizer for the AWSFederatedAccountAccess")
	AWSFederatedAccountAccess.SetFinalizers(remove(AWSFederatedAccountAccess.GetFinalizers(), finalizerName))

	// Update CR
	err := r.client.Update(context.TODO(), AWSFederatedAccountAccess)
	if err != nil {
		reqLogger.Error(err, "Failed to remove AWSFederatedAccountAccess finalizer")
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
