package awsfederatedrole

import (
	"context"
	goerr "errors"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/sts"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	awsfaa "github.com/openshift/aws-account-operator/pkg/controller/awsfederatedaccountaccess"
	"github.com/openshift/aws-account-operator/pkg/controller/utils"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var (
	log           = logf.Log.WithName("controller_awsfederatedrole")
	awsSecretName = "aws-account-operator-credentials"

	ErrInvalidManagedPolicy = goerr.New("InvalidManagedPolicy")
)

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new AWSFederatedRole Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileAWSFederatedRole{client: mgr.GetClient(),
		scheme:           mgr.GetScheme(),
		awsClientBuilder: awsclient.NewClient}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("awsfederatedrole-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource AWSFederatedRole
	err = c.Watch(&source.Kind{Type: &awsv1alpha1.AWSFederatedRole{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileAWSFederatedRole implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileAWSFederatedRole{}

// ReconcileAWSFederatedRole reconciles a AWSFederatedRole object
type ReconcileAWSFederatedRole struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client           client.Client
	scheme           *runtime.Scheme
	awsClientBuilder func(awsAccessID, awsAccessSecret, token, region string) (awsclient.Client, error)
}

// Reconcile reads that state of the cluster for a AWSFederatedRole object and makes changes based on the state read
// and what is in the AWSFederatedRole.Spec
func (r *ReconcileAWSFederatedRole) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling AWSFederatedRole")

	// Fetch the AWSFederatedRole instance
	instance := &awsv1alpha1.AWSFederatedRole{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// If the CR is known to be Valid or Invalid, doesn't need to be reconciled.
	if instance.Status.State == awsv1alpha1.AWSFederatedRoleStateValid || instance.Status.State == awsv1alpha1.AWSFederatedRoleStateInvalid {
		return reconcile.Result{}, nil
	}

	// Setup AWS client
	awsClient, err := awsclient.GetAWSClient(r.client, awsclient.NewAwsClientInput{
		SecretName: awsSecretName,
		NameSpace:  awsv1alpha1.AccountCrNamespace,
		AwsRegion:  "us-east-1",
	})

	if err != nil {
		awsClientErr := fmt.Sprintf("Unable to create aws client")
		reqLogger.Error(err, awsClientErr)
		return reconcile.Result{}, err
	}
	// If AWSCustomPolicy and AWSManagedPolicies don't exist, update condition and exit
	if len(instance.Spec.AWSManagedPolicies) == 0 && instance.Spec.AWSCustomPolicy.Name == "" {
		instance.Status.Conditions = utils.SetAWSFederatedRoleCondition(
			instance.Status.Conditions,
			awsv1alpha1.AWSFederatedRoleInvalid,
			"True",
			"NoAWSCustomPolicyOrAWSManagedPolicies",
			"AWSCustomPolicy and/or AWSManagedPolicies do not exist",
			utils.UpdateConditionNever)
		instance.Status.State = awsv1alpha1.AWSFederatedRoleStateInvalid
		err = r.client.Status().Update(context.TODO(), instance)
		if err != nil {
			log.Error(err, "Error updating conditions")
			return reconcile.Result{}, err
		}

		// Log the error
		log.Error(err, fmt.Sprintf("AWSCustomPolicy %s and/or AWSManagedPolicies %+v empty", instance.Spec.AWSCustomPolicy.Name, instance.Spec.AWSManagedPolicies))
		return reconcile.Result{}, nil
	}

	// Get aws id and create policy arn
	gciOut, err := awsClient.GetCallerIdentity(&sts.GetCallerIdentityInput{})
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Unable to create account id requested"))
		return reconcile.Result{}, err
	}

	// policy arn template
	policyARN := fmt.Sprintf("arn:aws:iam::%s:policy/%s", *gciOut.Account, instance.Spec.AWSCustomPolicy.Name)

	// Validates Custom IAM Policy
	log.Info("Validating Custom Policies")
	err = awsfaa.CreateOrUpdateIAMPolicy(awsClient, *instance, policyARN)
	// Invalidates instance if unable not create policy
	if err != nil {
		log.Error(err, "Unable to create policy")
		instance.Status.State = awsv1alpha1.AWSFederatedRoleStateInvalid
		instance.Status.Conditions = utils.SetAWSFederatedRoleCondition(
			instance.Status.Conditions,
			awsv1alpha1.AWSFederatedRoleInvalid,
			"True",
			"InvalidCustomerPolicy",
			"Unable to create custom policy",
			utils.UpdateConditionNever)
		err = r.client.Status().Update(context.TODO(), instance)
		if err != nil {
			log.Error(err, "Error updating conditions")
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	// Cleanup the created policy since its only for validation
	_, err = awsClient.DeletePolicy(&iam.DeletePolicyInput{PolicyArn: aws.String(policyARN)})
	if err != nil {
		log.Error(err, "Error deleting custom policy")
		return reconcile.Result{}, err
	}
	log.Info("Validated Custom Policies")

	// Ensures the managed IAM Polcies exist
	log.Info("Validating Managed Policies")
	// List all policies from AWS
	managedPolicies, err := getAllPolicies(awsClient)
	if err != nil {
		utils.LogAwsError(log, "Error listing managed AWS policies", err, err)
		return reconcile.Result{}, err
	}

	// Build list of names of managed Policies
	managedPolicyNameList := buildPolicyNameSlice(managedPolicies)

	// Check all policies listed in the CR
	for _, policy := range instance.Spec.AWSManagedPolicies {
		// Check if policy is in the list of managed policies
		if !policyInSlice(policy, managedPolicyNameList) {
			// Update condition to Invalid
			instance.Status.State = awsv1alpha1.AWSFederatedRoleStateInvalid
			instance.Status.Conditions = utils.SetAWSFederatedRoleCondition(
				instance.Status.Conditions,
				awsv1alpha1.AWSFederatedRoleInvalid,
				"True",
				"InvalidManagedPolicy",
				"Managed policy does not exist",
				utils.UpdateConditionNever)
			err = r.client.Status().Update(context.TODO(), instance)
			if err != nil {
				log.Error(err, "Error updating conditions")
				return reconcile.Result{}, err
			}
			log.Error(ErrInvalidManagedPolicy, fmt.Sprintf("Managed Policy %s does not exist", policy))
			return reconcile.Result{}, nil
		}
	}
	log.Info("Validated Managed Policies")

	// Update Condition to Valid
	instance.Status.State = awsv1alpha1.AWSFederatedRoleStateValid
	instance.Status.Conditions = utils.SetAWSFederatedRoleCondition(
		instance.Status.Conditions,
		awsv1alpha1.AWSFederatedRoleValid,
		"True",
		"AllPoliciesValid",
		"All managed and custom policies are validated",
		utils.UpdateConditionNever)
	err = r.client.Status().Update(context.TODO(), instance)
	if err != nil {
		log.Error(err, "Error updating conditions")
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

// Paginate through ListPolicy results from AWS
func getAllPolicies(awsClient awsclient.Client) ([]iam.Policy, error) {

	var policies []iam.Policy
	truncated := true
	marker := ""
	// The first request shouldn't have a marker
	input := &iam.ListPoliciesInput{}

	// Paginate through results until IsTruncated is False
	for {
		output, err := awsClient.ListPolicies(input)
		if err != nil {
			return []iam.Policy{}, err
		}

		for _, policy := range output.Policies {
			policies = append(policies, *policy)
		}

		truncated = *output.IsTruncated
		if truncated {
			// Set the marker for the subsequent request
			marker = *output.Marker
			input = &iam.ListPoliciesInput{Marker: &marker}
		} else {
			break
		}
	}

	return policies, nil
}

// Create list of policy names from a Policy slice
func buildPolicyNameSlice(policies []iam.Policy) []string {

	var policyNames []string
	for _, policy := range policies {
		policyNames = append(policyNames, *policy.PolicyName)
	}

	return policyNames
}

// Check if a policy name is in a list of policy names
func policyInSlice(policy string, policyList []string) bool {
	for _, namedPolicy := range policyList {
		if namedPolicy == policy {
			return true
		}
	}
	return false
}
