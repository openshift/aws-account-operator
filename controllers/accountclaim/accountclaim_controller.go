package accountclaim

import (
	"context"
	"fmt"
	"time"

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
)

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
//go:generate mockgen -destination ./mock/cr-client.go -package mock sigs.k8s.io/controller-runtime/pkg/client Client
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
	err := r.Client.Get(context.TODO(), request.NamespacedName, accountClaim)
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
		reqLogger.Info(fmt.Sprintf("Claim %s has been satisfied ignoring", accountClaim.ObjectMeta.Name))
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
	} else {
		unclaimedAccount, err = r.getClaimedAccount(accountClaim.Spec.AccountLink, awsv1alpha1.AccountCrNamespace)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	// Set Account.Spec.ClaimLink
	// This will trigger the reconcile loop for the account which will mark the account as claimed in its status
	if unclaimedAccount.Spec.ClaimLink == "" {
		updateClaimedAccountFields(reqLogger, unclaimedAccount, accountClaim)
		err := r.accountSpecUpdate(reqLogger, unclaimedAccount)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	// Set awsAccountClaim.Spec.AccountLink
	if accountClaim.Spec.AccountLink == "" {
		setAccountLinkOnAccountClaim(reqLogger, unclaimedAccount, accountClaim)
		return reconcile.Result{}, r.specUpdate(reqLogger, accountClaim)
	}

	if !accountClaim.Spec.ManualSTSMode {
		err = r.setSupportRoleARNManagedOpenshift(reqLogger, accountClaim, unclaimedAccount)
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
	}

	// Create secret for OCM to consume
	if !r.checkIAMSecretExists(accountClaim.Spec.AwsCredentialSecret.Name, accountClaim.Spec.AwsCredentialSecret.Namespace) {
		err = r.createIAMSecret(reqLogger, accountClaim, unclaimedAccount)
		if err != nil {
			return reconcile.Result{}, nil
		}
	}

	if accountClaim.Status.State != awsv1alpha1.ClaimStatusReady && accountClaim.Spec.AccountLink != "" {
		// Set AccountClaim.Status.Conditions and AccountClaim.Status.State to Ready
		setAccountClaimStatus(reqLogger, unclaimedAccount, accountClaim)
		return reconcile.Result{}, r.statusUpdate(reqLogger, accountClaim)
	}

	return reconcile.Result{}, nil
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

	reqLogger.Info("Reconciling CCS AccountClaim")
	if !accountClaim.Spec.ManualSTSMode {
		// Ensure BYOC secret has finalizer
		reqLogger.Info("Ensuring byoc secret has finalizer")
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

		// Create a new account with BYOC flag
		err := r.createAccountForBYOCClaim(accountClaim)
		if err != nil {
			return reconcile.Result{}, err
		}
		// Requeue this claim request in 30 seconds as we need to check to see if the account is ready
		// so we can update the AccountClaim `status.state` to `true`
		return reconcile.Result{RequeueAfter: time.Second * waitPeriod}, nil
	}

	// Get the account and check if its Ready
	byocAccount := &awsv1alpha1.Account{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: accountClaim.Spec.AccountLink, Namespace: awsv1alpha1.AccountCrNamespace}, byocAccount)
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
		// Update the status on AccountClaim
		return reconcile.Result{}, r.statusUpdate(reqLogger, accountClaim)
	}

	if !accountClaim.Spec.ManualSTSMode {
		err = r.setSupportRoleARNManagedOpenshift(reqLogger, accountClaim, byocAccount)
		if err != nil {
			return reconcile.Result{}, err
		}

		// Create secret for OCM to consume
		if !r.checkIAMSecretExists(accountClaim.Spec.AwsCredentialSecret.Name, accountClaim.Spec.AwsCredentialSecret.Namespace) {
			err = r.createIAMSecret(reqLogger, accountClaim, byocAccount)
			if err != nil {
				return reconcile.Result{}, nil
			}
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
	err := r.Client.Create(context.TODO(), newAccount)
	if err != nil {
		return err
	}

	// Set the accountLink of the AccountClaim to the new account if create is successful
	accountClaim.Spec.AccountLink = newAccount.Name
	err = r.Client.Update(context.TODO(), accountClaim)
	return err
}

func (r *AccountClaimReconciler) getClaimedAccount(accountLink string, namespace string) (*awsv1alpha1.Account, error) {
	account := &awsv1alpha1.Account{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: accountLink, Namespace: namespace}, account)
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

	if err := r.Client.List(context.TODO(), accountList, listOpts...); err != nil {
		reqLogger.Error(err, "Unable to get accountList")
		return nil, err
	}

	defaultAccountPoolName, err := config.GetDefaultAccountPoolName(reqLogger, r.Client)

	if err != nil {
		return nil, err
	}

	if defaultAccountPoolName == "" {
		// We shouldn't really ever hit this, as GetDefaultAccountPoolName will return NotFound err if
		// defaultAccountPoolName is empty, more of a just in case something changes.
		return nil, fmt.Errorf("Cannot find default accountpool")
	}

	if accountClaim.Spec.AccountPool == defaultAccountPoolName || accountClaim.Spec.AccountPool == "" {
		for _, account := range accountList.Items {
			// Ensure we're pulling accounts from the default accountPool
			if account.Spec.AccountPool == defaultAccountPoolName || (account.IsOwnedByAccountPool() && account.Spec.AccountPool == "") {
				return checkClaimAccountValidity(reqLogger, account, accountClaim)
			}
		}
	} else {
		for _, account := range accountList.Items {
			if account.Spec.AccountPool == accountClaim.Spec.AccountPool {
				return checkClaimAccountValidity(reqLogger, account, accountClaim)
			}
		}
	}

	return nil, fmt.Errorf("can't find a suitable account to claim")
}

func checkClaimAccountValidity(reqLogger logr.Logger, account awsv1alpha1.Account, accountClaim *awsv1alpha1.AccountClaim) (*awsv1alpha1.Account, error) {

	var unclaimedAccount awsv1alpha1.Account
	var unclaimedAccountFound = false

	if !account.Status.Claimed && account.Spec.ClaimLink == "" && account.Status.State == "Ready" {
		// Check for a reused account with matching legalEntity
		if account.Status.Reused {
			if matchAccountForReuse(&account, accountClaim) {
				reqLogger.Info(fmt.Sprintf("Reusing account: %s", account.ObjectMeta.Name))
				return &account, nil
			}
		} else {
			// If account is not reused, and we didn't claim one yet, do it
			if !unclaimedAccountFound {
				unclaimedAccount = account
				unclaimedAccountFound = true
			}
		}
	}
	// Go for unclaimed accounts
	if unclaimedAccountFound {
		reqLogger.Info(fmt.Sprintf("Claiming account: %s", unclaimedAccount.ObjectMeta.Name))
		return &unclaimedAccount, nil
	}
	// Neither unclaimed nor reused accounts found
	return nil, fmt.Errorf("can't find a ready account to claim")
}

func (r *AccountClaimReconciler) createIAMSecret(reqLogger logr.Logger, accountClaim *awsv1alpha1.AccountClaim, unclaimedAccount *awsv1alpha1.Account) error {
	// Get secret created by Account controller and copy it to the name/namespace combo that OCM is expecting
	accountIAMUserSecret := &corev1.Secret{}
	objectKey := client.ObjectKey{Namespace: unclaimedAccount.Namespace, Name: unclaimedAccount.Spec.IAMUserSecret}

	err := r.Client.Get(context.TODO(), objectKey, accountIAMUserSecret)
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

	err = r.Client.Create(context.TODO(), OCMSecret)
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
	err := r.Client.Get(context.TODO(), secretObjectKey, &secret)
	//nolint:gosimple // Ignores false-positive S1008 gosimple notice
	return err == nil
}

func (r *AccountClaimReconciler) statusUpdate(reqLogger logr.Logger, accountClaim *awsv1alpha1.AccountClaim) error {
	err := r.Client.Status().Update(context.TODO(), accountClaim)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Status update for %s failed", accountClaim.Name))
	}
	return err
}

func (r *AccountClaimReconciler) specUpdate(reqLogger logr.Logger, accountClaim *awsv1alpha1.AccountClaim) error {
	err := r.Client.Update(context.TODO(), accountClaim)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Spec update for %s failed", accountClaim.Name))
	}
	return err
}

func (r *AccountClaimReconciler) accountSpecUpdate(reqLogger logr.Logger, account *awsv1alpha1.Account) error {
	err := r.Client.Update(context.TODO(), account)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Account spec update for %s failed", account.Name))
	}
	return err
}

// updateClaimedAccountFields sets Account.Spec.ClaimLink to AccountClaim.ObjectMetadata.Name
func updateClaimedAccountFields(reqLogger logr.Logger, awsAccount *awsv1alpha1.Account, awsAccountClaim *awsv1alpha1.AccountClaim) {
	// Set link on Account
	awsAccount.Spec.ClaimLink = awsAccountClaim.ObjectMeta.Name
	awsAccount.Spec.ClaimLinkNamespace = awsAccountClaim.ObjectMeta.Namespace

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
		reqLogger.Info("AccountLink field is already populated for claim: %s, AWS account link is: %s\n", awsAccountClaim.ObjectMeta.Name, awsAccountClaim.Spec.AccountLink)
	}
	// Set link on AccountClaim
	awsAccountClaim.Spec.AccountLink = awsAccount.ObjectMeta.Name
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
	account.Spec.ClaimLink = accountClaim.ObjectMeta.Name
	account.Spec.ClaimLinkNamespace = accountClaim.ObjectMeta.Namespace
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
