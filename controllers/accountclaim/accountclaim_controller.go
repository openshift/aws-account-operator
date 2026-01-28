package accountclaim

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
	stsclient "github.com/openshift/aws-account-operator/pkg/awsclient/sts"

	"github.com/go-logr/logr"
	"github.com/openshift/aws-account-operator/config"
	"github.com/openshift/aws-account-operator/controllers/account"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/localmetrics"
	controllerutils "github.com/openshift/aws-account-operator/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
)

const (
	// AccountClaimed indicates the account has been claimed in the accountClaim status
	AccountClaimed = "AccountClaimed"
	// AccountUnclaimed indicates the account has not been claimed in the accountClaim status
	AccountUnclaimed = "AccountUnclaimed"

	awsCredsAccessKeyID     = "aws_access_key_id"     // #nosec G101 -- This is a false positive
	awsCredsSecretAccessKey = "aws_secret_access_key" // #nosec G101 -- This is a false positive
	accountClaimFinalizer   = "finalizer.aws.managed.openshift.io"
	byocSecretFinalizer     = accountClaimFinalizer + "/byoc"
	waitPeriod              = 30
	controllerName          = "accountclaim"
	fakeAnnotation          = "managed.openshift.com/fake"
	awsSTSSecret            = "sts-secret"
	stsRoleName             = "managed-sts-role"
	stsPolicyName           = "AAO-CustomPolicy"
	// PauseReconciliationAnnotation is the annotation key to pause all reconciliation for an account
	PauseReconciliationAnnotation = "aws.managed.openshift.com/pause-reconciliation"
)

var fleetManagerClaimEnabled = false

type Policy struct {
	Version   string `json:"Version"`
	Statement []struct {
		Sid      string   `json:"Sid"`
		Effect   string   `json:"Effect"`
		Action   []string `json:"Action"`
		Resource []string `json:"Resource"`
	} `json:"Statement"`
}

func generateInlinePolicy(accountID string) (string, error) {
	policy := Policy{
		Version: "2012-10-17",
		Statement: []struct {
			Sid      string   `json:"Sid"`
			Effect   string   `json:"Effect"`
			Action   []string `json:"Action"`
			Resource []string `json:"Resource"`
		}{
			{
				Sid:    "VisualEditor0",
				Effect: "Allow",
				Action: []string{
					"iam:GetPolicyVersion",
					"iam:DeletePolicyVersion",
					"iam:CreatePolicyVersion",
					"iam:UpdateAssumeRolePolicy",
					"secretsmanager:DescribeSecret",
					"iam:ListRoleTags",
					"secretsmanager:PutSecretValue",
					"secretsmanager:CreateSecret",
					"iam:TagRole",
					"secretsmanager:DeleteSecret",
					"iam:UpdateOpenIDConnectProviderThumbprint",
					"iam:DeletePolicy",
					"iam:CreateRole",
					"iam:AttachRolePolicy",
					"iam:ListInstanceProfilesForRole",
					"secretsmanager:GetSecretValue",
					"iam:DetachRolePolicy",
					"iam:ListAttachedRolePolicies",
					"iam:ListPolicyTags",
					"iam:ListRolePolicies",
					"iam:DeleteOpenIDConnectProvider",
					"iam:DeleteInstanceProfile",
					"iam:GetRole",
					"iam:GetPolicy",
					"iam:ListEntitiesForPolicy",
					"iam:DeleteRole",
					"iam:TagPolicy",
					"iam:CreateOpenIDConnectProvider",
					"iam:CreatePolicy",
					"secretsmanager:GetResourcePolicy",
					"iam:ListPolicyVersions",
					"iam:UpdateRole",
					"iam:GetOpenIDConnectProvider",
					"iam:TagOpenIDConnectProvider",
					"secretsmanager:TagResource",
					"sts:AssumeRoleWithWebIdentity",
					"iam:ListRoles",
				},
				Resource: []string{
					"arn:aws:iam::" + accountID + ":instance-profile/*",
					"arn:aws:iam::" + accountID + ":instance-profile/*",
					"arn:aws:iam::" + accountID + ":role/*",
					"arn:aws:iam::" + accountID + ":oidc-provider/*",
					"arn:aws:iam::" + accountID + ":policy/*",
				},
			},
			{
				Sid:    "VisualEditor1",
				Effect: "Allow",
				Action: []string{
					"s3:*",
				},
				Resource: []string{"*"},
			},
		},
	}

	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return "", err
	}

	return string(policyJSON), nil
}

var log = logf.Log.WithName("controller_accountclaim")

// AccountClaimReconciler reconciles a AccountClaim object
type AccountClaimReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	awsClientBuilder awsclient.IBuilder
}

//+kubebuilder:rbac:groups=aws.managed.openshift.io,resources=accountclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=aws.managed.openshift.io,resources=accountclaims/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=aws.managed.openshift.io,resources=accountclaims/finalizers,verbs=update

// NewReconcileAccountClaim initializes ReconcileAccountClaim
//
//go:generate mockgen -build_flags --mod=mod -destination ./mock/cr-client.go -package mock sigs.k8s.io/controller-runtime/pkg/client Client
func NewAccountClaimReconciler(client client.Client, scheme *runtime.Scheme, awsClientBuilder awsclient.IBuilder) *AccountClaimReconciler {
	return &AccountClaimReconciler{
		Client:           client,
		Scheme:           scheme,
		awsClientBuilder: awsClientBuilder,
	}
}

// Reconcile reads that state of the cluster for a AccountClaim object and makes changes based on the state read
// and what is in the AccountClaim.Spec
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *AccountClaimReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	reqLogger := log.WithValues("Controller", controllerName, "Request.Namespace", request.Namespace, "Request.Name", request.Name)
	// Watch AccountClaim
	accountClaim := &awsv1alpha1.AccountClaim{}
	err := r.Get(context.TODO(), request.NamespacedName, accountClaim)
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

	// Fake Account Claim Process for Hive Testing ..
	// Fake account claims are account claims which have the label `managed.openshift.com/fake: true`
	// These fake claims are used for testing within hive
	if accountClaim.Annotations[fakeAnnotation] == "true" {
		requeue, err := r.processFake(reqLogger, accountClaim)
		if err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{Requeue: requeue}, nil
	}

	// Add finalizer to the CR in case it's not present (e.g. old accounts)
	if !controllerutils.Contains(accountClaim.GetFinalizers(), accountClaimFinalizer) {
		err := r.addFinalizer(reqLogger, accountClaim)
		if err != nil {
			return reconcile.Result{}, err
		}

		return reconcile.Result{}, nil
	}

	if accountClaim.DeletionTimestamp != nil {
		if accountClaim.Spec.FleetManagerConfig.TrustedARN != "" {
			if r.checkIAMSecretExists(accountClaim.Spec.AwsCredentialSecret.Name, accountClaim.Spec.AwsCredentialSecret.Namespace) {
				err = r.deleteIAMSecret(reqLogger, accountClaim.Spec.AwsCredentialSecret.Name, accountClaim.Spec.AwsCredentialSecret.Namespace)
				if err != nil {
					return reconcile.Result{}, err
				}
				reqLogger.V(1).Info("successfully deleted IAM secret", "accountclaim", accountClaim.Name)
			}
			currentAcctInstance, accountErr := r.getClaimedAccount(accountClaim.Spec.AccountLink, awsv1alpha1.AccountCrNamespace)
			if accountErr != nil {
				reqLogger.Error(accountErr, "Unable to get claimed account")
			}
			reqLogger.V(1).Info("successfully got claimed account", "accountclaim", accountClaim.Name)
			if currentAcctInstance != nil && !currentAcctInstance.IsBYOC() {
				awsRegion := config.GetDefaultRegion()

				awsSetupClient, err := r.awsClientBuilder.GetClient(controllerName, r.Client, awsclient.NewAwsClientInput{
					SecretName: controllerutils.AwsSecretName,
					NameSpace:  awsv1alpha1.AccountCrNamespace,
					AwsRegion:  awsRegion,
				})
				if err != nil {
					reqLogger.Error(err, "failed building operator AWS client")
					return reconcile.Result{}, err
				}
				awsClient, _, err := stsclient.HandleRoleAssumption(reqLogger, r.awsClientBuilder, currentAcctInstance, r.Client, awsSetupClient, "", awsv1alpha1.AccountOperatorIAMRole, "")
				if err != nil {
					reqLogger.Error(err, "failed building AWS client from assume_role")
					return reconcile.Result{}, err
				}
				err = r.CleanUpIAMRoleAndPolicies(reqLogger, awsClient, stsRoleName)
				if err != nil {
					return reconcile.Result{}, err
				}
				reqLogger.V(1).Info("successfully cleaned up IAM role and policies", "accountclaim", accountClaim.Name)
			}
		}
		return reconcile.Result{}, r.handleAccountClaimDeletion(reqLogger, accountClaim)
	}

	isCCS := accountClaim.Spec.BYOCAWSAccountID != ""

	if accountClaim.Status.State == awsv1alpha1.ClaimStatusPending {
		now := metav1.Now()
		pendingDuration := now.Sub(accountClaim.GetObjectMeta().GetCreationTimestamp().Time)
		localmetrics.Collector.SetAccountClaimPendingDuration(isCCS, pendingDuration.Seconds())
	}

	if accountClaim.Spec.BYOC {
		return r.handleBYOCAccountClaim(reqLogger, accountClaim)
	}

	// Return if this claim has been satisfied
	if claimIsSatisfied(accountClaim) {
		reqLogger.Info(fmt.Sprintf("Claim %s has been satisfied ignoring", accountClaim.Name))
		return reconcile.Result{}, nil
	}

	if accountClaim.Status.State == "" {
		message := "Attempting to claim account"
		reqLogger.Info(message)
		accountClaim.Status.State = awsv1alpha1.ClaimStatusPending

		accountClaim.Status.Conditions = controllerutils.SetAccountClaimCondition(
			accountClaim.Status.Conditions,
			awsv1alpha1.AccountUnclaimed,
			corev1.ConditionTrue,
			AccountClaimed,
			message,
			controllerutils.UpdateConditionNever,
			isCCS,
		)

		// Update the Spec on AccountClaim
		return reconcile.Result{}, r.statusUpdate(reqLogger, accountClaim)
	}

	var unclaimedAccount *awsv1alpha1.Account

	// Get an unclaimed account from the pool
	if accountClaim.Spec.AccountLink == "" {
		unclaimedAccount, err = r.getUnclaimedAccount(reqLogger, accountClaim)
		if err != nil {
			reqLogger.Error(err, "Unable to select an unclaimed account from the pool")
			return reconcile.Result{}, err
		}
		reqLogger.V(1).Info("successfully got unclaimed account", "accountclaim", accountClaim.Name)
	} else {
		unclaimedAccount, err = r.getClaimedAccount(accountClaim.Spec.AccountLink, awsv1alpha1.AccountCrNamespace)
		if err != nil {
			return reconcile.Result{}, err
		}
		reqLogger.V(1).Info("successfully got claimed account", "accountclaim", accountClaim.Name)
	}

	// Set Account.Spec.ClaimLink
	// This will trigger the reconcile loop for the account which will mark the account as claimed in its status
	if unclaimedAccount.Spec.ClaimLink == "" {
		updateClaimedAccountFields(reqLogger, unclaimedAccount, accountClaim)
		err := r.accountSpecUpdate(reqLogger, unclaimedAccount)
		if err != nil {
			return reconcile.Result{}, err
		}
		reqLogger.V(1).Info("successfully updated claimLink", "accountclaim", accountClaim.Name)
	}

	// Set awsAccountClaim.Spec.AccountLink
	if accountClaim.Spec.AccountLink == "" {
		setAccountLinkOnAccountClaim(reqLogger, unclaimedAccount, accountClaim)
		reqLogger.V(1).Info("successfully set AccountLink", "accountclaim", accountClaim.Name)
		return reconcile.Result{}, r.specUpdate(reqLogger, accountClaim)
	}

	if !accountClaim.Spec.ManualSTSMode {
		err = r.setSupportRoleARNManagedOpenshift(reqLogger, accountClaim, unclaimedAccount)
		reqLogger.V(1).Info("successfully set the support role ARN", "accountclaim", accountClaim.Name)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	// Set awsAccountClaim.Spec.AwsAccountOU
	if accountClaim.Spec.AccountOU == "" || accountClaim.Spec.AccountOU == "ROOT" {
		// Determine if in fedramp env
		awsRegion := config.GetDefaultRegion()

		// aws client
		awsClient, err := r.awsClientBuilder.GetClient(controllerName, r.Client, awsclient.NewAwsClientInput{
			SecretName: controllerutils.AwsSecretName,
			NameSpace:  awsv1alpha1.AccountCrNamespace,
			AwsRegion:  awsRegion,
		})
		if err != nil {
			unexpectedErrorMsg := "OU: Failed to build aws client"
			reqLogger.Info(unexpectedErrorMsg)
			return reconcile.Result{}, err
		}

		err = MoveAccountToOU(r, reqLogger, awsClient, accountClaim, unclaimedAccount)
		if err != nil {
			if err == awsv1alpha1.ErrAccMoveRaceCondition {
				// Due to a race condition, we need to requeue the reconcile to ensure that the account was correctly moved into the correct OU
				return reconcile.Result{Requeue: true}, nil
			}
			return reconcile.Result{}, err
		}
		reqLogger.V(1).Info("successfully moved account to OU", "accountclaimName", accountClaim.Name, "account", unclaimedAccount.Name)
	}
	cm, err := controllerutils.GetOperatorConfigMap(r.Client)
	if err != nil {
		log.Error(err, "Could not retrieve the operator configmap")
		return controllerutils.RequeueAfter(5 * time.Minute)
	}

	enabled, err := strconv.ParseBool(cm.Data["feature.accountclaim_fleet_manager_trusted_arn"])
	if err != nil {
		log.Info("Could not retrieve feature flag 'feature.accountclaim_fleet_manager_trusted_arn' - fleet manager accountclaim is disabled")
	} else {
		fleetManagerClaimEnabled = enabled
	}
	log.Info("Is fleet manager accountclaim enabled?", "enabled", fleetManagerClaimEnabled)

	// This will trigger role and secret creation which will enable AccountCLaims to be able to gain access via an AWS STS tokens
	if accountClaim.Spec.FleetManagerConfig.TrustedARN != "" && (accountClaim.Spec.AccountPool != "" && accountClaim.Spec.AccountPool != "default") {
		if fleetManagerClaimEnabled {
			awsRegion := config.GetDefaultRegion()

			awsSetupClient, err := r.awsClientBuilder.GetClient(controllerName, r.Client, awsclient.NewAwsClientInput{
				SecretName: controllerutils.AwsSecretName,
				NameSpace:  awsv1alpha1.AccountCrNamespace,
				AwsRegion:  awsRegion,
			})
			if err != nil {
				reqLogger.Error(err, "failed building operator AWS client")
				return reconcile.Result{}, err
			}
			awsClient, _, err := stsclient.HandleRoleAssumption(reqLogger, r.awsClientBuilder, unclaimedAccount, r.Client, awsSetupClient, "", awsv1alpha1.AccountOperatorIAMRole, "")
			if err != nil {
				reqLogger.Error(err, "failed building AWS client from assume_role")
				return reconcile.Result{}, err
			}

			err = r.CleanUpIAMRoleAndPolicies(reqLogger, awsClient, stsRoleName)
			if err != nil {
				return reconcile.Result{}, err
			}

			roleARN, err := r.createIAMRoleWithPermissions(reqLogger, awsClient, stsRoleName, accountClaim.Spec.FleetManagerConfig.TrustedARN)
			if err != nil {
				return reconcile.Result{}, err
			}

			// Implement IAM user deletion logic
			if err := account.DeleteIAMUsers(reqLogger, awsClient, unclaimedAccount); err != nil {
				return reconcile.Result{}, fmt.Errorf("failed deleting IAM users: %v", err)
			}

			// Deletes account IAM user Secret
			if r.checkIAMSecretExists(unclaimedAccount.Spec.IAMUserSecret, unclaimedAccount.Namespace) {
				err := r.deleteIAMSecret(reqLogger, unclaimedAccount.Spec.IAMUserSecret, unclaimedAccount.Namespace)
				if err != nil {
					return reconcile.Result{}, err
				}
			}
			// Remove IAM user Secret from Account Spec
			unclaimedAccount.Spec.IAMUserSecret = ""
			err = r.accountSpecUpdate(reqLogger, unclaimedAccount)
			if err != nil {
				return reconcile.Result{}, err
			}

			// Creates IAM role secret
			if !r.checkIAMSecretExists(accountClaim.Spec.AwsCredentialSecret.Name, accountClaim.Spec.AwsCredentialSecret.Namespace) {
				if err := r.createIAMRoleSecret(reqLogger, accountClaim, roleARN); err != nil {
					return reconcile.Result{}, err
				}
			} else {
				err = r.deleteIAMSecret(reqLogger, accountClaim.Spec.AwsCredentialSecret.Name, accountClaim.Spec.AwsCredentialSecret.Namespace)
				if err != nil {
					return reconcile.Result{}, err
				}
				err = r.createIAMRoleSecret(reqLogger, accountClaim, roleARN)
				if err != nil {
					return reconcile.Result{}, err
				}
			}
			reqLogger.V(1).Info("successfully created role and secret for fleet manager accountclaim", "accountclaim", accountClaim.Name)
		} else {
			log.Info("Would attempt to create IAM Role with permission here, but fleet manager accountclaim is disabled.")
		}
	} else {

		// Create secret for OCM to consume
		if !r.checkIAMSecretExists(accountClaim.Spec.AwsCredentialSecret.Name, accountClaim.Spec.AwsCredentialSecret.Namespace) {
			err = r.createIAMSecret(reqLogger, accountClaim, unclaimedAccount)
			if err != nil {
				return reconcile.Result{}, nil
			}
			reqLogger.V(1).Info("successfully created IAM secret", "accountclaim", accountClaim.Name)
		}
	}

	if accountClaim.Status.State != awsv1alpha1.ClaimStatusReady && accountClaim.Spec.AccountLink != "" {
		// Set AccountClaim.Status.Conditions and AccountClaim.Status.State to Ready
		setAccountClaimStatus(reqLogger, unclaimedAccount, accountClaim)
		reqLogger.V(1).Info("successfully updated accountclaim status to Ready", "accountclaim", accountClaim.Name)
		return reconcile.Result{}, r.statusUpdate(reqLogger, accountClaim)
	}

	return reconcile.Result{}, nil
}

// CleanUpIAMRoleAndPolicies  is responsible for cleaning up existing IAM roles and their associated policies.
func (r *AccountClaimReconciler) CleanUpIAMRoleAndPolicies(reqLogger logr.Logger, awsClient awsclient.Client, roleName string) error {
	// Retrieve the existing IAM role by its name.
	_, err := awsClient.GetRole(&iam.GetRoleInput{
		RoleName: aws.String(roleName),
	})
	if err != nil {
		return nil
	}

	respPolicy, err := awsClient.ListRolePolicies(&iam.ListRolePoliciesInput{
		RoleName: aws.String(roleName),
	})

	if err != nil {
		_, err = awsClient.DeleteRole(&iam.DeleteRoleInput{
			RoleName: aws.String(roleName),
		})
		return err
	}

	for _, policyName := range respPolicy.PolicyNames {
		_, err = awsClient.DeleteRolePolicy(&iam.DeleteRolePolicyInput{
			RoleName:   aws.String(roleName),
			PolicyName: policyName,
		})

		if err != nil {
			reqLogger.Error(err, "failed to delete inline policy")
			return err
		}
	}
	_, err = awsClient.DeleteRole(&iam.DeleteRoleInput{
		RoleName: aws.String(roleName),
	})
	if err != nil {
		reqLogger.Error(err, "failed to delete IAM role")
		return err
	}

	return nil
}

func (r *AccountClaimReconciler) deleteIAMSecret(reqLogger logr.Logger, secretName string, namespace string) error {
	accountIAMUserSecret := &corev1.Secret{}
	objectKey := client.ObjectKey{Namespace: namespace, Name: secretName}

	err := r.Get(context.TODO(), objectKey, accountIAMUserSecret)
	if err != nil {
		reqLogger.Error(err, "Unable to find secret")
		return err
	}

	err = r.Delete(context.TODO(), accountIAMUserSecret)
	if err != nil {
		reqLogger.Error(err, "Unable to delete IAM secret")
		return err
	}
	reqLogger.Info("IAM secret deleted", "SecretName", secretName)
	return nil
}

func newStsSecretforCR(secretName string, secretNameSpace string, arn []byte) *corev1.Secret {
	return &corev1.Secret{
		Type: "Opaque",
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: secretNameSpace,
		},
		Data: map[string][]byte{
			"role_arn": arn,
		},
	}

}

// CreateOrUpdateSecret creates a secret in AWS Secrets Manager or updates it if it already exists.
func (r *AccountClaimReconciler) createIAMRoleSecret(reqLogger logr.Logger, accountClaim *awsv1alpha1.AccountClaim, roleARN string) error {
	var OCMSecretNamespace string
	var OCMSecretName string

	if accountClaim.Spec.AwsCredentialSecret.Name == "" {
		OCMSecretName = accountClaim.Name + "-" + awsSTSSecret

	} else {
		OCMSecretName = accountClaim.Spec.AwsCredentialSecret.Name
	}

	if accountClaim.Spec.AwsCredentialSecret.Namespace == "" {
		OCMSecretNamespace = accountClaim.Namespace

	} else {
		OCMSecretNamespace = accountClaim.Spec.AwsCredentialSecret.Namespace
	}

	OCMSecret := newStsSecretforCR(OCMSecretName, OCMSecretNamespace, []byte(roleARN))

	err := r.Create(context.TODO(), OCMSecret)
	if err != nil {
		reqLogger.Error(err, "Unable to create secret for OCM")
		return err
	}
	reqLogger.Info(fmt.Sprintf("Secret %s created for claim %s", OCMSecret.Name, accountClaim.Name))

	accountClaim.Spec.AwsCredentialSecret.Name = OCMSecretName
	err = r.Update(context.TODO(), accountClaim)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("AccountClaim spec update for %s failed", accountClaim.Name))
	}
	return nil
}

// CreateIAMRoleWithPermissions creates an IAM role with the specified permissions' policy.
func (r *AccountClaimReconciler) createIAMRoleWithPermissions(reqLogger logr.Logger, awsClient awsclient.Client, roleName string, trustedARN string) (string, error) {
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
				AWS: []string{trustedARN},
			},
		}},
	}
	// Convert role to JSON
	jsonAssumeRolePolicyDoc, err := json.Marshal(assumeRolePolicyDoc)
	if err != nil {
		return "", err
	}

	reqLogger.Info(fmt.Sprintf("Creating role: %s", roleName))
	createRoleOutput, err := awsClient.CreateRole(&iam.CreateRoleInput{
		RoleName:                 aws.String(roleName),
		Description:              aws.String("Managed by AAO"),
		AssumeRolePolicyDocument: aws.String(string(jsonAssumeRolePolicyDoc)),
	})
	if err != nil {
		return "", err
	}
	reqLogger.Info(fmt.Sprintf("Role %s created", createRoleOutput))

	arnComponents := strings.Split(*createRoleOutput.Role.Arn, ":")
	accountId := arnComponents[4]

	policyDocument, err := generateInlinePolicy(accountId)
	if err != nil {
		return "", err
	}

	// Attach the permissions policy to the role
	_, err = awsClient.PutRolePolicy(&iam.PutRolePolicyInput{
		PolicyName:     aws.String(stsPolicyName),
		RoleName:       aws.String(roleName),
		PolicyDocument: aws.String(policyDocument),
	})

	if err != nil {
		// If there was an error, clean up by deleting the role
		_, roleDeleteErr := awsClient.DeleteRole(&iam.DeleteRoleInput{
			RoleName: aws.String(roleName),
		})
		if roleDeleteErr != nil {
			reqLogger.Error(roleDeleteErr, "Failed to delete role")
		}
		return ``, err
	}

	return *createRoleOutput.Role.Arn, nil
}
func (r *AccountClaimReconciler) setSupportRoleARNManagedOpenshift(reqLogger logr.Logger, accountClaim *awsv1alpha1.AccountClaim, account *awsv1alpha1.Account) error {
	if accountClaim.Spec.STSRoleARN == "" {
		instanceID := account.Labels[awsv1alpha1.IAMUserIDLabel]
		accountClaim.Spec.SupportRoleARN = config.GetIAMArn(account.Spec.AwsAccountID, config.AwsResourceTypeRole, fmt.Sprintf("ManagedOpenShift-Support-%s", instanceID))
		return r.specUpdate(reqLogger, accountClaim)
	}
	return nil
}

func (r *AccountClaimReconciler) handleAccountClaimDeletion(reqLogger logr.Logger, accountClaim *awsv1alpha1.AccountClaim) error {

	if !controllerutils.Contains(accountClaim.GetFinalizers(), accountClaimFinalizer) {
		return nil
	}

	// Workaround for FleetManagers special account handling, see
	// https://issues.redhat.com/browse/OSD-19093
	if len(accountClaim.GetFinalizers()) > 1 {
		reqLogger.Info("Found additional finalizers on AccountClaim. Not attempting cleanup.")
		return nil
	}

	// Only do AWS cleanup and account reset if accountLink is not empty
	// We will not attempt AWS cleanup if the account is BYOC since we're not going to reuse these accounts
	if accountClaim.Spec.AccountLink != "" {
		err := r.finalizeAccountClaim(reqLogger, accountClaim)
		if err != nil {
			// If the finalize/cleanup process fails for an account we don't want to return
			// we will flag the account with the Failed Reuse condition, and with state = Failed

			// First we want to see if this was an update race condition where the credentials rotator will update the CR while the finalizer is trying to run.  If that's the case, we want to requeue and retry, before outright failing the account.
			if k8serr.IsConflict(err) {
				reqLogger.Info("Account CR Modified during CR reset.")
				return fmt.Errorf("account CR modified during reset: %w", err)
			}

			// Get account claimed by deleted accountclaim
			failedReusedAccount, accountErr := r.getClaimedAccount(accountClaim.Spec.AccountLink, awsv1alpha1.AccountCrNamespace)
			if accountErr != nil {
				reqLogger.Error(accountErr, "Failed to get claimed account")
				return fmt.Errorf("failed to get claimed account: %w", err)
			}
			// Update account status and add "Reuse Failed" condition
			accountErr = r.resetAccountSpecStatus(reqLogger, failedReusedAccount, accountClaim, awsv1alpha1.AccountFailed, "Failed")
			if accountErr != nil {
				reqLogger.Error(accountErr, "Failed updating account status for failed reuse")
				return fmt.Errorf("failed updating account status for failed reuse: %w", err)
			}

			return err
		}
	}

	// Remove finalizer to unlock deletion of the accountClaim
	return r.removeFinalizer(reqLogger, accountClaim, accountClaimFinalizer)
}

func (r *AccountClaimReconciler) handleBYOCAccountClaim(reqLogger logr.Logger, accountClaim *awsv1alpha1.AccountClaim) (reconcile.Result, error) {
	if !accountClaim.Spec.BYOC {
		return reconcile.Result{}, nil
	}

	reqLogger.Info("Reconciling CCS AccountClaim", "accountclaim", accountClaim.Name)
	if !accountClaim.Spec.ManualSTSMode {
		// Ensure BYOC secret has finalizer
		reqLogger.Info("Ensuring byoc secret has finalizer", "accountclaim", accountClaim.Name)
		err := r.addBYOCSecretFinalizer(accountClaim)
		if err != nil {
			reqLogger.Error(err, "Unable to add finalizer to byoc secret")
		}
	}

	// Check, if already associated with an Account
	if accountClaim.Spec.AccountLink == "" {
		validateErr := accountClaim.Validate()
		if validateErr != nil {
			// Figure the reason for our failure
			errReason := validateErr.Error()
			// Update AccountClaim status
			controllerutils.SetAccountClaimStatus(
				accountClaim,
				"Invalid AccountClaim",
				errReason,
				awsv1alpha1.InvalidAccountClaim,
				awsv1alpha1.ClaimStatusError,
			)
			err := r.Client.Status().Update(context.TODO(), accountClaim)
			if err != nil {
				reqLogger.Error(err, "Failed to Update AccountClaim Status")
			}

			// TODO: Recoverable?
			return reconcile.Result{}, validateErr
		}
		reqLogger.V(1).Info("successfully validated account linked to accountclaim ", "accountclaim", accountClaim.Name)

		// Create a new account with BYOC flag
		err := r.createAccountForBYOCClaim(accountClaim)
		if err != nil {
			return reconcile.Result{}, err
		}
		reqLogger.V(1).Info("successfully created account for BYOC claim", "accountclaim", accountClaim.Name, "account", accountClaim.Spec.AccountLink)
		// Requeue this claim request in 30 seconds as we need to check to see if the account is ready
		// so we can update the AccountClaim `status.state` to `true`
		return reconcile.Result{RequeueAfter: time.Second * waitPeriod}, nil
	}

	// Get the account and check if its Ready
	byocAccount := &awsv1alpha1.Account{}
	err := r.Get(context.TODO(), types.NamespacedName{Name: accountClaim.Spec.AccountLink, Namespace: awsv1alpha1.AccountCrNamespace}, byocAccount)
	if err != nil {
		return reconcile.Result{}, err
	}

	if !byocAccount.IsReady() {
		if byocAccount.IsFailed() {
			accountClaim.Status.State = awsv1alpha1.ClaimStatusError
			message := "CCS Account Failed"
			accountClaim.Status.Conditions = controllerutils.SetAccountClaimCondition(
				accountClaim.Status.Conditions,
				awsv1alpha1.CCSAccountClaimFailed,
				corev1.ConditionTrue,
				string(awsv1alpha1.CCSAccountClaimFailed),
				message,
				controllerutils.UpdateConditionNever,
				accountClaim.Spec.BYOCAWSAccountID != "",
			)
			// Update the status on AccountClaim
			return reconcile.Result{}, r.statusUpdate(reqLogger, accountClaim)
		}
		waitMsg := fmt.Sprintf("%s is not Ready yet, requeuing in %d seconds", byocAccount.Name, waitPeriod)
		reqLogger.Info(waitMsg, "Account Status", byocAccount.Status.State)
		return reconcile.Result{RequeueAfter: time.Second * waitPeriod}, nil
	}

	if byocAccount.IsReady() && accountClaim.Status.State != awsv1alpha1.ClaimStatusReady {
		accountClaim.Status.State = awsv1alpha1.ClaimStatusReady
		message := "BYOC account ready"
		accountClaim.Status.Conditions = controllerutils.SetAccountClaimCondition(
			accountClaim.Status.Conditions,
			awsv1alpha1.AccountClaimed,
			corev1.ConditionTrue,
			AccountClaimed,
			message,
			controllerutils.UpdateConditionNever,
			accountClaim.Spec.BYOCAWSAccountID != "",
		)
		reqLogger.V(1).Info(fmt.Sprintf("%s is Ready", byocAccount.Name), "accountclaim", accountClaim.Name, "Account Status", byocAccount.Status.State)
		// Update the status on AccountClaim
		return reconcile.Result{}, r.statusUpdate(reqLogger, accountClaim)
	}

	if !accountClaim.Spec.ManualSTSMode {
		err = r.setSupportRoleARNManagedOpenshift(reqLogger, accountClaim, byocAccount)
		if err != nil {
			return reconcile.Result{}, err
		}
		reqLogger.V(1).Info("successfully set support role ARN", "accountclaim", accountClaim.Name)

		// Create secret for OCM to consume
		if !r.checkIAMSecretExists(accountClaim.Spec.AwsCredentialSecret.Name, accountClaim.Spec.AwsCredentialSecret.Namespace) {
			err = r.createIAMSecret(reqLogger, accountClaim, byocAccount)
			if err != nil {
				return reconcile.Result{}, nil
			}
			reqLogger.V(1).Info("successfully created IAM secret", "accountclaim", accountClaim.Name)
		}
	}

	return reconcile.Result{}, nil

}

func (r *AccountClaimReconciler) createAccountForBYOCClaim(accountClaim *awsv1alpha1.AccountClaim) error {
	// Create a new account with BYOC flag
	newAccount := account.GenerateAccountCR(awsv1alpha1.AccountCrNamespace)
	populateBYOCSpec(newAccount, accountClaim)
	controllerutils.AddFinalizer(newAccount, accountClaimFinalizer)

	// Create the new account
	err := r.Create(context.TODO(), newAccount)
	if err != nil {
		return err
	}

	// Set the accountLink of the AccountClaim to the new account if create is successful
	accountClaim.Spec.AccountLink = newAccount.Name
	err = r.Update(context.TODO(), accountClaim)
	return err
}

func (r *AccountClaimReconciler) getClaimedAccount(accountLink string, namespace string) (*awsv1alpha1.Account, error) {
	account := &awsv1alpha1.Account{}
	err := r.Get(context.TODO(), types.NamespacedName{Name: accountLink, Namespace: namespace}, account)
	if err != nil {
		return nil, err
	}
	return account, nil
}

func (r *AccountClaimReconciler) getUnclaimedAccount(reqLogger logr.Logger, accountClaim *awsv1alpha1.AccountClaim) (*awsv1alpha1.Account, error) {

	accountList := &awsv1alpha1.AccountList{}

	listOpts := []client.ListOption{
		client.InNamespace(awsv1alpha1.AccountCrNamespace),
	}

	if err := r.List(context.TODO(), accountList, listOpts...); err != nil {
		reqLogger.Error(err, "Unable to get accountList")
		return nil, err
	}

	defaultAccountPoolName, err := config.GetDefaultAccountPoolName(reqLogger, r.Client)
	if err != nil {
		reqLogger.Error(err, "Failed getting default AccountPool name")
		return nil, err
	}

	if defaultAccountPoolName == "" {
		// We shouldn't really ever hit this, as GetDefaultAccountPoolName will return NotFound err if
		// defaultAccountPoolName is empty, more of a just in case something changes.
		err = fmt.Errorf("cannot find default accountpool")
		reqLogger.Error(err, "Default AccountPool name is empty")
		return nil, err
	} else {
		reqLogger.Info(fmt.Sprintf("defaultAccountPoolName: %s", defaultAccountPoolName))
	}

	var unusedAccount *awsv1alpha1.Account

	for _, loopAccount := range accountList.Items {
		// assign to new variable to prevent issues with using a pointer to the loop var later
		account := loopAccount
		if !IsSameAccountPoolNames(account.Spec.AccountPool, accountClaim.Spec.AccountPool, defaultAccountPoolName) {
			continue
		}

		if !CanAccountBeClaimedByAccountClaim(&account, accountClaim) {
			continue
		}

		if account.Status.Reused {
			reqLogger.Info(fmt.Sprintf("Reusing account: %s", account.Name))
			return &account, nil
		} else {
			unusedAccount = &account
		}
	}

	if unusedAccount != nil {
		reqLogger.Info(fmt.Sprintf("Claiming account: %s", unusedAccount.Name))
		return unusedAccount, nil
	}
	return nil, fmt.Errorf("can't find a suitable account to claim")
}

// IsSameAccountPoolNames is used to determine if two accountpool names
// reference the same accountpool, given a defaultAccountPool name. When
// referencing an accountpool using the empty string as the name, the aao uses
// the default accounpool instead. So we can not just check, weather the two
// pool names match, we also first need to subsitute "" with the default
// accountpool name, before comparing the strings. This function does exactly
// that.
//
// Note that it returns false when no default accountpool is given
func IsSameAccountPoolNames(first string, second string, defaultAccountPool string) bool {
	// when the default defaultAccountPool isn't specifies, we default to false
	if defaultAccountPool == "" {
		return false
	}

	// now, we just treat "" as defaultAccountPool and compare the strings
	var firstDefault string
	var secondDefault string
	if first == "" {
		firstDefault = defaultAccountPool
	} else {
		firstDefault = first
	}

	if second == "" {
		secondDefault = defaultAccountPool
	} else {
		secondDefault = second
	}

	return firstDefault == secondDefault
}

// CanAccountBeClaimedByAccountClaim returns true when the account matches the
// given accountclaim. This is the case when the account is currently unclaimed
// and ready and additionally, one of the following applies:
// * The account has never been used before and therefore has it's LegalEntityID unset, or
// * The account has been used before and has the same legalEntityID as the accountclaim
// In all other cases, this Function returns false.
func CanAccountBeClaimedByAccountClaim(account *awsv1alpha1.Account, accountclaim *awsv1alpha1.AccountClaim) bool {
	// nil accounts can't be claimed
	if account == nil || accountclaim == nil {
		return false
	}

	// Accounts with pause reconciliation annotation can't be claimed
	if account.Annotations[PauseReconciliationAnnotation] == "true" {
		return false
	}

	// Accounts that aren't ready can't be claimed
	if account.Status.State != AccountReady {
		return false
	}

	// claimed accounts can't be claimed
	if account.Status.Claimed || account.Spec.ClaimLink != "" {
		return false
	}

	// Unused accounts always match
	if !account.Status.Reused {
		return true
	}

	return account.Spec.LegalEntity.ID == accountclaim.Spec.LegalEntity.ID
}

func (r *AccountClaimReconciler) createIAMSecret(reqLogger logr.Logger, accountClaim *awsv1alpha1.AccountClaim, unclaimedAccount *awsv1alpha1.Account) error {
	// Get secret created by Account controller and copy it to the name/namespace combo that OCM is expecting
	accountIAMUserSecret := &corev1.Secret{}
	objectKey := client.ObjectKey{Namespace: unclaimedAccount.Namespace, Name: unclaimedAccount.Spec.IAMUserSecret}

	err := r.Get(context.TODO(), objectKey, accountIAMUserSecret)
	if err != nil {
		reqLogger.Error(err, "Unable to find AWS account STS secret")
		return err
	}

	OCMSecretName := accountClaim.Spec.AwsCredentialSecret.Name
	OCMSecretNamespace := accountClaim.Spec.AwsCredentialSecret.Namespace
	awsAccessKeyID := accountIAMUserSecret.Data[awsCredsAccessKeyID]
	awsSecretAccessKey := accountIAMUserSecret.Data[awsCredsSecretAccessKey]

	if string(awsAccessKeyID) == "" || string(awsSecretAccessKey) == "" {
		reqLogger.Error(err, fmt.Sprintf("Cannot get AWS Credentials from secret %s referenced from Account", unclaimedAccount.Spec.IAMUserSecret))
	}

	OCMSecret := newSecretforCR(OCMSecretName, OCMSecretNamespace, awsAccessKeyID, awsSecretAccessKey)

	err = r.Create(context.TODO(), OCMSecret)
	if err != nil {
		reqLogger.Error(err, "Unable to create secret for OCM")
		return err
	}

	reqLogger.Info(fmt.Sprintf("Secret %s created for claim %s", OCMSecret.Name, accountClaim.Name))
	return nil
}

func (r *AccountClaimReconciler) checkIAMSecretExists(name string, namespace string) bool {
	// Need to check if the secret exists AND that it matches what we're expecting
	secret := corev1.Secret{}
	secretObjectKey := client.ObjectKey{Name: name, Namespace: namespace}
	if err := r.Get(context.TODO(), secretObjectKey, &secret); err != nil {
		// The secret does not exist
		return false
	}
	return true
}

func (r *AccountClaimReconciler) statusUpdate(reqLogger logr.Logger, accountClaim *awsv1alpha1.AccountClaim) error {
	err := r.Client.Status().Update(context.TODO(), accountClaim)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Status update for %s failed", accountClaim.Name))
	}
	return err
}

func (r *AccountClaimReconciler) specUpdate(reqLogger logr.Logger, accountClaim *awsv1alpha1.AccountClaim) error {
	err := r.Update(context.TODO(), accountClaim)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Spec update for %s failed", accountClaim.Name))
	}
	return err
}

func (r *AccountClaimReconciler) accountSpecUpdate(reqLogger logr.Logger, account *awsv1alpha1.Account) error {
	err := r.Update(context.TODO(), account)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Account spec update for %s failed", account.Name))
	}
	return err
}

// updateClaimedAccountFields sets Account.Spec.ClaimLink to AccountClaim.ObjectMetadata.Name
func updateClaimedAccountFields(reqLogger logr.Logger, awsAccount *awsv1alpha1.Account, awsAccountClaim *awsv1alpha1.AccountClaim) {
	// Set link on Account
	awsAccount.Spec.ClaimLink = awsAccountClaim.Name
	awsAccount.Spec.ClaimLinkNamespace = awsAccountClaim.Namespace

	// Carry over LegalEntity data from the claim to the account
	awsAccount.Spec.LegalEntity.ID = awsAccountClaim.Spec.LegalEntity.ID
	awsAccount.Spec.LegalEntity.Name = awsAccountClaim.Spec.LegalEntity.Name

	reqLogger.Info(fmt.Sprintf("Account %s ClaimLink set to AccountClaim %s and carried over LegalEntity ID %s", awsAccount.Name, awsAccountClaim.Name, awsAccount.Spec.LegalEntity.ID))
}

func setAccountClaimStatus(reqLogger logr.Logger, awsAccount *awsv1alpha1.Account, awsAccountClaim *awsv1alpha1.AccountClaim) {
	message := fmt.Sprintf("Account claim fulfilled by %s", awsAccount.Name)
	awsAccountClaim.Status.Conditions = controllerutils.SetAccountClaimCondition(
		awsAccountClaim.Status.Conditions,
		awsv1alpha1.AccountClaimed,
		corev1.ConditionTrue,
		AccountClaimed,
		message,
		controllerutils.UpdateConditionNever,
		awsAccountClaim.Spec.BYOCAWSAccountID != "",
	)
	awsAccountClaim.Status.State = awsv1alpha1.ClaimStatusReady
	reqLogger.Info(fmt.Sprintf("Account %s condition status updated", awsAccountClaim.Name))
}

// setAccountLink sets AccountClaim.Spec.AccountLink to Account.ObjectMetadata.Name
func setAccountLinkOnAccountClaim(reqLogger logr.Logger, awsAccount *awsv1alpha1.Account, awsAccountClaim *awsv1alpha1.AccountClaim) {
	// This shouldn't error but lets log it just incase
	if awsAccountClaim.Spec.AccountLink != "" {
		reqLogger.Info("AccountLink field is already populated for claim: %s, AWS account link is: %s\n", awsAccountClaim.Name, awsAccountClaim.Spec.AccountLink)
	}
	// Set link on AccountClaim
	awsAccountClaim.Spec.AccountLink = awsAccount.Name
	reqLogger.Info(fmt.Sprintf("Linked claim %s to account %s", awsAccountClaim.Name, awsAccount.Name))
}

func claimIsSatisfied(accountClaim *awsv1alpha1.AccountClaim) bool {
	return accountClaim.Spec.AccountLink != "" && accountClaim.Status.State == awsv1alpha1.ClaimStatusReady && accountClaim.Spec.AccountOU != ""
}

func newSecretforCR(secretName string, secretNameSpace string, awsAccessKeyID []byte, awsSecretAccessKey []byte) *corev1.Secret {
	return &corev1.Secret{
		Type: "Opaque",
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: secretNameSpace,
		},
		Data: map[string][]byte{
			"aws_access_key_id":     awsAccessKeyID,
			"aws_secret_access_key": awsSecretAccessKey,
		},
	}

}

// Add BYOC data to an account CR
func populateBYOCSpec(account *awsv1alpha1.Account, accountClaim *awsv1alpha1.AccountClaim) {
	account.Spec.BYOC = true
	account.Spec.AwsAccountID = accountClaim.Spec.BYOCAWSAccountID
	account.Spec.ClaimLink = accountClaim.Name
	account.Spec.ClaimLinkNamespace = accountClaim.Namespace
	account.Spec.LegalEntity = accountClaim.Spec.LegalEntity
	account.Spec.ManualSTSMode = accountClaim.Spec.ManualSTSMode
}

// SetupWithManager sets up the controller with the Manager.
func (r *AccountClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.awsClientBuilder = &awsclient.Builder{}
	maxReconciles, err := controllerutils.GetControllerMaxReconciles(controllerName)
	if err != nil {
		log.Error(err, "missing max reconciles for controller", "controller", controllerName)
	}

	rwm := controllerutils.NewReconcilerWithMetrics(r, controllerName)
	return ctrl.NewControllerManagedBy(mgr).
		For(&awsv1alpha1.AccountClaim{}).
		Owns(&awsv1alpha1.Account{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxReconciles,
		}).Complete(rwm)
}
