package accountclaim

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/controller/account"
	controllerutils "github.com/openshift/aws-account-operator/pkg/controller/utils"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	// AccountClaimed indicates the account has been claimed in the accountClaim status
	AccountClaimed = "AccountClaimed"
	// AccountUnclaimed indicates the account has not been claimed in the accountClaim status
	AccountUnclaimed = "AccountUnclaimed"

	awsCredsAccessKeyID     = "aws_access_key_id"
	awsCredsSecretAccessKey = "aws_secret_access_key"
	accountClaimFinalizer   = "finalizer.aws.managed.openshift.io"
	byocSecretFinalizer     = accountClaimFinalizer + "/byoc"
	waitPeriod              = 30
	controllerName          = "accountclaim"
	fakeAnnotation          = "managed.openshift.com/fake"
)

var log = logf.Log.WithName("controller_accountclaim")

// Add creates a new AccountClaim Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	reconciler := &ReconcileAccountClaim{
		client:           controllerutils.NewClientWithMetricsOrDie(log, mgr, controllerName),
		scheme:           mgr.GetScheme(),
		awsClientBuilder: &awsclient.Builder{},
	}
	return controllerutils.NewReconcilerWithMetrics(reconciler, controllerName)
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("accountclaim-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource AccountClaim
	err = c.Watch(&source.Kind{Type: &awsv1alpha1.AccountClaim{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &awsv1alpha1.Account{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &awsv1alpha1.AccountClaim{},
	})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileAccountClaim{}

// ReconcileAccountClaim reconciles a AccountClaim object
type ReconcileAccountClaim struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client           client.Client
	scheme           *runtime.Scheme
	awsClientBuilder awsclient.IBuilder
}

// Reconcile reads that state of the cluster for a AccountClaim object and makes changes based on the state read
// and what is in the AccountClaim.Spec
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileAccountClaim) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Controller", controllerName, "Request.Namespace", request.Namespace, "Request.Name", request.Name)

	// Watch AccountClaim
	accountClaim := &awsv1alpha1.AccountClaim{}
	err := r.client.Get(context.TODO(), request.NamespacedName, accountClaim)
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

	// Fake Account Claim Process for Hive Testing
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

	// Check if accountClaim is being deleted, this will trigger the account reuse workflow
	if accountClaim.DeletionTimestamp != nil {
		if controllerutils.Contains(accountClaim.GetFinalizers(), accountClaimFinalizer) {
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
						return reconcile.Result{}, errors.Wrap(err, "account CR modified during reset")
					}

					// Get account claimed by deleted accountclaim
					failedReusedAccount, accountErr := r.getClaimedAccount(accountClaim.Spec.AccountLink, awsv1alpha1.AccountCrNamespace)
					if accountErr != nil {
						reqLogger.Error(accountErr, "Failed to get claimed account")
						return reconcile.Result{}, err
					}
					// Update account status and add "Reuse Failed" condition
					accountErr = r.resetAccountSpecStatus(reqLogger, failedReusedAccount, accountClaim, awsv1alpha1.AccountFailed, "Failed")
					if accountErr != nil {
						reqLogger.Error(accountErr, "Failed updating account status for failed reuse")
						return reconcile.Result{}, err
					}
				}
			}

			// Remove finalizer to unlock deletion of the accountClaim
			err = r.removeFinalizer(reqLogger, accountClaim, accountClaimFinalizer)
			if err != nil {
				return reconcile.Result{}, err
			}

		}
		return reconcile.Result{}, nil
	}

	if accountClaim.Spec.BYOC {
		reqLogger.Info("Reconciling CCS AccountClaim")

		if !accountClaim.Spec.ManualSTSMode {
			// Ensure BYOC secret has finalizer
			reqLogger.Info("Ensuring byoc secret has finalizer")
			err = r.addBYOCSecretFinalizer(accountClaim)
			if err != nil {
				reqLogger.Error(err, "Unable to add finalizer to byoc secret")
			}
		}

		if accountClaim.Spec.AccountLink == "" {

			// Create a new account with BYOC flag
			newAccount := account.GenerateAccountCR(awsv1alpha1.AccountCrNamespace)
			populateBYOCSpec(newAccount, accountClaim)
			controllerutils.AddFinalizer(newAccount, "finalizer.aws.managed.openshift.io")

			// Create the new account
			err = r.client.Create(context.TODO(), newAccount)
			if err != nil {
				return reconcile.Result{}, err
			}

			// Set the accountLink of the AccountClaim to the new account if create is successful
			accountClaim.Spec.AccountLink = newAccount.Name
			err = r.client.Update(context.TODO(), accountClaim)
			if err != nil {
				return reconcile.Result{}, err
			}
			// Requeue this claim request in 30 seconds as we need to check to see if the account is ready
			// so we can update the AccountClaim `status.state` to `true`
			return reconcile.Result{RequeueAfter: time.Second * waitPeriod}, nil
		}

		// Get the account and check if its Ready
		byocAccount := &awsv1alpha1.Account{}
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: accountClaim.Spec.AccountLink, Namespace: awsv1alpha1.AccountCrNamespace}, byocAccount)
		if err != nil {
			return reconcile.Result{}, err
		}

		if byocAccount.Status.State != string(awsv1alpha1.AccountReady) {
			if byocAccount.Status.State == string(awsv1alpha1.AccountFailed) {
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

		if byocAccount.Status.State == string(awsv1alpha1.AccountReady) && accountClaim.Status.State != awsv1alpha1.ClaimStatusReady {
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
			accountClaim.Spec.BYOCAWSAccountID != "",
		)
		// Update the Spec on AccountClaim
		return reconcile.Result{}, r.statusUpdate(reqLogger, accountClaim)
	}

	accountList := &awsv1alpha1.AccountList{}

	listOpts := []client.ListOption{
		client.InNamespace(awsv1alpha1.AccountCrNamespace),
	}
	if err = r.client.List(context.TODO(), accountList, listOpts...); err != nil {
		reqLogger.Error(err, "Unable to get accountList")
		return reconcile.Result{}, err
	}

	var unclaimedAccount *awsv1alpha1.Account

	// Get an unclaimed account from the pool
	if accountClaim.Spec.AccountLink == "" {
		unclaimedAccount, err = getUnclaimedAccount(reqLogger, accountList, accountClaim)
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

	// Set awsAccountClaim.Spec.AwsAccountOU
	if accountClaim.Spec.AccountOU == "" || accountClaim.Spec.AccountOU == "ROOT" {
		err = MoveAccountToOU(r, reqLogger, accountClaim, unclaimedAccount)
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

func (r *ReconcileAccountClaim) getClaimedAccount(accountLink string, namespace string) (*awsv1alpha1.Account, error) {
	account := &awsv1alpha1.Account{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: accountLink, Namespace: namespace}, account)
	if err != nil {
		return nil, err
	}
	return account, nil
}

func getUnclaimedAccount(reqLogger logr.Logger, accountList *awsv1alpha1.AccountList, accountClaim *awsv1alpha1.AccountClaim) (*awsv1alpha1.Account, error) {
	var unclaimedAccount awsv1alpha1.Account
	var reusedAccount awsv1alpha1.Account
	var unclaimedAccountFound = false
	var reusedAccountFound = false
	time.Sleep(1000 * time.Millisecond)

	// Range through accounts and select the first one that doesn't have a claim link
	for _, account := range accountList.Items {

		if !account.Status.Claimed && account.Spec.ClaimLink == "" && account.Status.State == "Ready" {
			// Check for a reused account with matching legalEntity
			if account.Status.Reused {
				if matchAccountForReuse(&account, accountClaim) {
					reusedAccountFound = true
					reusedAccount = account
					// if available we break the loop, reused account takes priority
					break
				}
			} else {
				// If account is not reused, and we didn't claim one yet, do it
				if !unclaimedAccountFound {
					unclaimedAccount = account
					unclaimedAccountFound = true
				}
			}
		}
	}

	// Give priority to reusing accounts instead of claiming
	if reusedAccountFound {
		reqLogger.Info(fmt.Sprintf("Reusing account: %s", reusedAccount.ObjectMeta.Name))
		return &reusedAccount, nil
	}
	// Go for unclaimed accounts
	if unclaimedAccountFound {
		reqLogger.Info(fmt.Sprintf("Claiming account: %s", unclaimedAccount.ObjectMeta.Name))
		return &unclaimedAccount, nil
	}

	// Neither unclaimed nor reused accounts found
	return nil, fmt.Errorf("can't find a ready account to claim")
}

func (r *ReconcileAccountClaim) createIAMSecret(reqLogger logr.Logger, accountClaim *awsv1alpha1.AccountClaim, unclaimedAccount *awsv1alpha1.Account) error {
	// Get secret created by Account controller and copy it to the name/namespace combo that OCM is expecting
	accountIAMUserSecret := &corev1.Secret{}
	objectKey := client.ObjectKey{Namespace: unclaimedAccount.Namespace, Name: unclaimedAccount.Spec.IAMUserSecret}

	err := r.client.Get(context.TODO(), objectKey, accountIAMUserSecret)
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

	err = r.client.Create(context.TODO(), OCMSecret)
	if err != nil {
		reqLogger.Error(err, "Unable to create secret for OCM")
		return err
	}

	reqLogger.Info(fmt.Sprintf("Secret %s created for claim %s", OCMSecret.Name, accountClaim.Name))
	return nil
}

func (r *ReconcileAccountClaim) checkIAMSecretExists(name string, namespace string) bool {
	// Need to check if the secret exists AND that it matches what we're expecting
	secret := corev1.Secret{}
	secretObjectKey := client.ObjectKey{Name: name, Namespace: namespace}
	err := r.client.Get(context.TODO(), secretObjectKey, &secret)
	if err != nil { //nolint; gosimple // Ignores false-positive S1008 gosimple notice
		return false
	}
	return true
}

func (r *ReconcileAccountClaim) statusUpdate(reqLogger logr.Logger, accountClaim *awsv1alpha1.AccountClaim) error {
	err := r.client.Status().Update(context.TODO(), accountClaim)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Status update for %s failed", accountClaim.Name))
	}
	return err
}

func (r *ReconcileAccountClaim) specUpdate(reqLogger logr.Logger, accountClaim *awsv1alpha1.AccountClaim) error {
	err := r.client.Update(context.TODO(), accountClaim)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Spec update for %s failed", accountClaim.Name))
	}
	return err
}

func (r *ReconcileAccountClaim) accountSpecUpdate(reqLogger logr.Logger, account *awsv1alpha1.Account) error {
	err := r.client.Update(context.TODO(), account)
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
