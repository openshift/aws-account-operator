package awsmanagedrole

import (
	"context"
	goerr "errors"
	"fmt"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"

	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/controller/utils"

	"github.com/openshift/aws-account-operator/pkg/awsclient"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	controllerName = "awsdmanagedrole"
)

var (
	log           = logf.Log.WithName("controller_awsmanagedrole")
	awsSecretName = "aws-account-operator-credentials"

	errInvalidManagedPolicy = goerr.New("InvalidManagedPolicy")
)

// Add creates a new AWSManagedRole Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	reconciler := &ReconcileAWSManagedRole{
		client:           utils.NewClientWithMetricsOrDie(log, mgr, controllerName),
		scheme:           mgr.GetScheme(),
		awsClientBuilder: &awsclient.Builder{},
	}
	return utils.NewReconcilerWithMetrics(reconciler, controllerName)
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("awsmanagedrole-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource AWSManagedRole
	err = c.Watch(&source.Kind{Type: &awsv1alpha1.AWSManagedRole{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileAWSManagedRole implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileAWSManagedRole{}

// ReconcileAWSManagedRole reconciles a AWSManagedRole object
type ReconcileAWSManagedRole struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client           client.Client
	scheme           *runtime.Scheme
	awsClientBuilder awsclient.IBuilder
}

// Reconcile reads that state of the cluster for a AWSManagedRole object and makes changes based on the state read
// and what is in the AWSManagedRole.Spec
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileAWSManagedRole) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Controller", controllerName, "Request.Namespace", request.Namespace, "Request.Name", request.Name)

	// Fetch the AWSManagedRole instance
	instance := &awsv1alpha1.AWSManagedRole{}
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

	// Ensure the role has a finalizer
	if !utils.Contains(instance.GetFinalizers(), utils.Finalizer) {

		err := r.addFinalizer(reqLogger, instance)
		if err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	if instance.DeletionTimestamp != nil {

		if utils.Contains(instance.GetFinalizers(), utils.Finalizer) {

			reqLogger.Info("Cleaning up ManagedAccountAccess Roles")
			err = r.finalizeFederateRole(reqLogger, instance)
			if err != nil {
				return reconcile.Result{}, err
			}

			reqLogger.Info("Removing Finalizer")
			err = r.removeFinalizer(reqLogger, instance, utils.Finalizer)
			if err != nil {
				return reconcile.Result{}, err
			}
		}
	}

	// Setup AWS client
	awsClient, err := r.awsClientBuilder.GetClient(controllerName, r.client, awsclient.NewAwsClientInput{
		SecretName: awsSecretName,
		NameSpace:  awsv1alpha1.AccountCrNamespace,
		AwsRegion:  "us-east-1",
	})
	if err != nil {
		return reconcile.Result{}, err
	}

	// Validates Custom IAM Policy

	log.Info("Validating Custom Policies")
	// Build custom policy in AWS-valid JSON and converts to string
	jsonPolicy, err := utils.MarshalManagedIAMPolicy(*instance)
	if err != nil {
		reqLogger.Error(err, "failed marshalling IAM Policy")
		utils.SetAWSManagedRoleCondition(
			instance.Status.Conditions,
			awsv1alpha1.AWSManagedRoleInvalid,
			"True",
			"MarshallingError",
			"UnableToMarshalJsonFromPolicy",
			utils.UpdateConditionNever)
		// We don't want to return the error here because we don't want to continue to retry if the policy is bad
		return reconcile.Result{}, nil
	}

	// If AWSCustomPolicy and AWSManagedPolicies don't exist, update condition and exit
	if len(instance.Spec.AWSManagedPolicies) == 0 && instance.Spec.AWSCustomPolicy.Name == "" {
		instance.Status.Conditions = utils.SetAWSManagedRoleCondition(
			instance.Status.Conditions,
			awsv1alpha1.AWSManagedRoleInvalid,
			"True",
			"NoAWSCustomPolicyOrAWSManagedPolicies",
			"AWSCustomPolicy and/or AWSManagedPolicies do not exist",
			utils.UpdateConditionNever)
		err = r.client.Status().Update(context.TODO(), instance)
		if err != nil {
			log.Error(err, "Error updating conditions")
			return reconcile.Result{}, err
		}

		// Log the error
		log.Error(err, fmt.Sprintf("AWSCustomPolicy %s and/or AWSManagedPolicies %+v empty", instance.Spec.AWSCustomPolicy.Name, instance.Spec.AWSManagedPolicies))
		return reconcile.Result{}, nil
	}

	// Attempts to create the policy to ensure its a valid policy
	createOutput, err := awsClient.CreatePolicy(&iam.CreatePolicyInput{
		Description:    &instance.Spec.AWSCustomPolicy.Description,
		PolicyName:     &instance.Spec.AWSCustomPolicy.Name,
		PolicyDocument: &jsonPolicy,
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == "MalformedPolicyDocument" {
				log.Error(err, "Malformed Policy Document")
				instance.Status.State = awsv1alpha1.AWSManagedRoleStateInvalid
				instance.Status.Conditions = utils.SetAWSManagedRoleCondition(
					instance.Status.Conditions,
					awsv1alpha1.AWSManagedRoleInvalid,
					"True",
					"InvalidCustomerPolicy",
					"Custom Policy is malformed",
					utils.UpdateConditionNever)
				err = r.client.Status().Update(context.TODO(), instance)
				if err != nil {
					log.Error(err, "Error updating conditions")
					return reconcile.Result{}, err
				}
				return reconcile.Result{}, nil
			}
			utils.LogAwsError(log, "", nil, err)
		} else {
			log.Error(err, "Non-AWS Error while creating Policy")
		}
		return reconcile.Result{}, err
	}

	// Cleanup the created policy since its only for validation
	_, err = awsClient.DeletePolicy(&iam.DeletePolicyInput{PolicyArn: createOutput.Policy.Arn})
	if err != nil {
		log.Error(err, "Error deleting custom policy")
		return reconcile.Result{}, err
	}
	log.Info("Validated Custom Policies")

	// Ensures the managed IAM Policies exist
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
			instance.Status.State = awsv1alpha1.AWSManagedRoleStateInvalid
			instance.Status.Conditions = utils.SetAWSManagedRoleCondition(
				instance.Status.Conditions,
				awsv1alpha1.AWSManagedRoleInvalid,
				"True",
				"InvalidManagedPolicy",
				"Managed policy does not exist",
				utils.UpdateConditionNever)
			err = r.client.Status().Update(context.TODO(), instance)
			if err != nil {
				log.Error(err, "Error updating conditions")
				return reconcile.Result{}, err
			}
			log.Error(errInvalidManagedPolicy, fmt.Sprintf("Managed Policy %s does not exist", policy))
			return reconcile.Result{}, nil
		}
	}
	log.Info("Validated Managed Policies")

	// Update Condition to Valid
	instance.Status.State = awsv1alpha1.AWSManagedRoleStateValid
	instance.Status.Conditions = utils.SetAWSManagedRoleCondition(
		instance.Status.Conditions,
		awsv1alpha1.AWSManagedRoleValid,
		"True",
		"AllPoliciesValid",
		"All managed and custom policies are validated",
		utils.UpdateConditionNever)
	err = r.client.Status().Update(context.TODO(), instance)
	if err != nil {
		log.Error(err, "Error updating conditions")
		return reconcile.Result{}, err
	}

	r.deployManagedRole(instance)

	return reconcile.Result{}, nil
}

// deleteManagedRole will update Account CRs to remove the annotation and therefore remove the role from the account
func (r *ReconcileAWSManagedRole) deleteManagedRole(role *awsv1alpha1.AWSManagedRole) {
	// First get a list of all accounts
	accounts := &awsv1alpha1.AccountList{}
	r.client.List(context.TODO(), accounts)

	annotationName := awsv1alpha1.AWSManagedRoleAnnotationPrefix + role.Name
	log.Info("Deleting annotation from all accounts", "annotation", annotationName)
	for _, account := range accounts.Items {
		delete(account.ObjectMeta.Annotations, annotationName)
		err := r.client.Update(context.TODO(), &account)
		if err != nil {
			log.Error(err, "There was an error updating the account")
		}
		log.Info("Account Annotation Deleted", "account", account.Name, "annotation", annotationName)
	}
}

// deployManagedRole will update Account CRs in order to get them to re-deploy managed roles if necessary
func (r *ReconcileAWSManagedRole) deployManagedRole(role *awsv1alpha1.AWSManagedRole) {
	// First, get a list of all accounts
	accounts := &awsv1alpha1.AccountList{}
	r.client.List(context.TODO(), accounts)

	// Then add/update the role annotation on the account
	annotationName := awsv1alpha1.AWSManagedRoleAnnotationPrefix + role.Name
	log.Info("Updating All Accounts with annotation", "annotation", annotationName)
	for _, account := range accounts.Items {
		account.ObjectMeta.Annotations[annotationName] = role.ObjectMeta.ResourceVersion
		err := r.client.Update(context.TODO(), &account)
		if err != nil {
			log.Error(err, "There was an error updating the account")
		}
		log.Info("Account Annotation Updated", "account", account.Name, "annotation", annotationName)
	}
}

// Paginate through ListPolicy results from AWS
func getAllPolicies(awsClient awsclient.Client) ([]iam.Policy, error) {

	var policies []iam.Policy
	var truncated bool
	var marker string
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
