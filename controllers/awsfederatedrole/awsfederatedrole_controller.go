package awsfederatedrole

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/config"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/utils"
)

const (
	controllerName = "awsfederatedrole"
)

var (
	log           = logf.Log.WithName("controller_awsfederatedrole")
	awsSecretName = "aws-account-operator-credentials" //  #nosec G101 -- This is a false positive

	errInvalidManagedPolicy = errors.New("InvalidManagedPolicy")
)

// AWSFederatedRoleReconciler reconciles a AWSFederatedRole object
type AWSFederatedRoleReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	awsClientBuilder awsclient.IBuilder
}

//+kubebuilder:rbac:groups=aws.managed.openshift.io,resources=awsfederatedroles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=aws.managed.openshift.io,resources=awsfederatedroles/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=aws.managed.openshift.io,resources=awsfederatedroles/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.2/pkg/reconcile
func (r *AWSFederatedRoleReconciler) Reconcile(_ context.Context, request ctrl.Request) (ctrl.Result, error) {
	reqLogger := log.WithValues("Controller", controllerName, "Request.Namespace", request.Namespace, "Request.Name", request.Name)

	if config.IsFedramp() {
		log.Info("Running in fedramp mode, skip AWSFederatedRole controller")
		return reconcile.Result{}, nil
	}

	// Fetch the AWSFederatedRole instance
	instance := &awsv1alpha1.AWSFederatedRole{}
	err := r.Get(context.TODO(), request.NamespacedName, instance)
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

			reqLogger.Info("Cleaning up FederatedAccountAccess Roles")
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
	awsRegion := config.GetDefaultRegion()
	awsClient, err := r.awsClientBuilder.GetClient(controllerName, r.Client, awsclient.NewAwsClientInput{
		SecretName: awsSecretName,
		NameSpace:  awsv1alpha1.AccountCrNamespace,
		AwsRegion:  awsRegion,
	})
	if err != nil {
		return reconcile.Result{}, err
	}

	// Validates Custom IAM Policy
	log.Info("Validating Custom Policies")
	// Build custom policy in AWS-valid JSON and converts to string
	jsonPolicy, err := utils.MarshalIAMPolicy(*instance)
	if err != nil {
		reqLogger.Error(err, "failed marshalling IAM Policy", "instanceRoleName", instance.Spec.RoleDisplayName)
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
		err = r.Client.Status().Update(context.TODO(), instance)
		if err != nil {
			log.Error(err, "Error updating conditions")
			return reconcile.Result{}, err
		}

		// Log the error
		log.Error(err, fmt.Sprintf("AWSCustomPolicy %s and/or AWSManagedPolicies %+v empty", instance.Spec.AWSCustomPolicy.Name, instance.Spec.AWSManagedPolicies))
		return reconcile.Result{}, nil
	}

	// Attempts to create the policy to ensure it's a valid policy
	createOutput, err := awsClient.CreatePolicy(context.TODO(), &iam.CreatePolicyInput{
		Description:    &instance.Spec.AWSCustomPolicy.Description,
		PolicyName:     &instance.Spec.AWSCustomPolicy.Name,
		PolicyDocument: &jsonPolicy,
	})
	if err != nil {
		// Check for specific IAM exception types
		var malformedPolicyErr *iamtypes.MalformedPolicyDocumentException
		if errors.As(err, &malformedPolicyErr) {
			log.Error(err, "Malformed Policy Document")
			instance.Status.State = awsv1alpha1.AWSFederatedRoleStateInvalid
			instance.Status.Conditions = utils.SetAWSFederatedRoleCondition(
				instance.Status.Conditions,
				awsv1alpha1.AWSFederatedRoleInvalid,
				"True",
				"InvalidCustomerPolicy",
				"Custom Policy is malformed",
				utils.UpdateConditionNever)
			err = r.Client.Status().Update(context.TODO(), instance)
			if err != nil {
				log.Error(err, "Error updating conditions")
				return reconcile.Result{}, err
			}
			return reconcile.Result{}, nil
		}
		// Log other AWS errors
		utils.LogAwsError(log, "AWS Error while creating Policy", nil, err)
		return reconcile.Result{}, err
	}

	// Cleanup the created policy since it's only for validation
	_, err = awsClient.DeletePolicy(context.TODO(), &iam.DeletePolicyInput{PolicyArn: createOutput.Policy.Arn})
	if err != nil {
		log.Error(err, "Error deleting custom policy")
		return reconcile.Result{}, err
	}
	log.Info("Valided Custom Policies")

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
			instance.Status.State = awsv1alpha1.AWSFederatedRoleStateInvalid
			instance.Status.Conditions = utils.SetAWSFederatedRoleCondition(
				instance.Status.Conditions,
				awsv1alpha1.AWSFederatedRoleInvalid,
				"True",
				"InvalidManagedPolicy",
				"Managed policy does not exist",
				utils.UpdateConditionNever)
			err = r.Client.Status().Update(context.TODO(), instance)
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
	instance.Status.State = awsv1alpha1.AWSFederatedRoleStateValid
	instance.Status.Conditions = utils.SetAWSFederatedRoleCondition(
		instance.Status.Conditions,
		awsv1alpha1.AWSFederatedRoleValid,
		"True",
		"AllPoliciesValid",
		"All managed and custom policies are validated",
		utils.UpdateConditionNever)
	err = r.Client.Status().Update(context.TODO(), instance)
	if err != nil {
		log.Error(err, "Error updating conditions")
		return reconcile.Result{}, err
	}

	if err := annotateAccountAccesses(r.Client, instance.Name); err != nil {
		return ctrl.Result{}, err
	}

	return reconcile.Result{}, nil
}

func annotateAccountAccesses(kubeClient client.Client, roleName string) error {
	accountAccesses := &awsv1alpha1.AWSFederatedAccountAccessList{}
	err := kubeClient.List(context.TODO(), accountAccesses, client.MatchingLabels{awsv1alpha1.FederatedRoleNameLabel: roleName})
	if err != nil {
		return err
	}
	for i := range accountAccesses.Items {
		if accountAccesses.Items[i].Annotations == nil {
			accountAccesses.Items[i].Annotations = make(map[string]string)
		}

		accountAccesses.Items[i].Annotations[awsv1alpha1.LastRoleUpdateAnnotation] = time.Now().UTC().Format(time.RFC850)
		err = kubeClient.Update(context.TODO(), &accountAccesses.Items[i])
		if err != nil {
			return err
		}
	}
	return nil
}

// Paginate through ListPolicy results from AWS
func getAllPolicies(awsClient awsclient.Client) ([]iamtypes.Policy, error) {

	var policies []iamtypes.Policy
	var truncated bool
	var marker string
	// The first request shouldn't have a marker
	input := &iam.ListPoliciesInput{}

	// Paginate through results until IsTruncated is False
	for {
		output, err := awsClient.ListPolicies(context.TODO(), input)
		if err != nil {
			return []iamtypes.Policy{}, err
		}

		policies = append(policies, output.Policies...)

		truncated = output.IsTruncated
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
func buildPolicyNameSlice(policies []iamtypes.Policy) []string {

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

// SetupWithManager sets up the controller with the Manager.
func (r *AWSFederatedRoleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.awsClientBuilder = &awsclient.Builder{}
	maxReconciles, err := utils.GetControllerMaxReconciles(controllerName)
	if err != nil {
		log.Error(err, "missing max reconciles for controller", "controller", controllerName)
	}

	rwm := utils.NewReconcilerWithMetrics(r, controllerName)
	return ctrl.NewControllerManagedBy(mgr).
		For(&awsv1alpha1.AWSFederatedRole{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxReconciles,
		}).Complete(rwm)
}
