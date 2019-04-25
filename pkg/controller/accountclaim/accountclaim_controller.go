package accountclaim

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	controllerutils "github.com/openshift/aws-account-operator/pkg/controller/utils"
	"github.com/openshift/aws-account-operator/pkg/metrics"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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

const (
	AccountClaimed          = "AccountClaimed"
	AccountUnclaimed        = "AccountUnclaimed"
	awsCredsUserName        = "aws_user_name"
	awsCredsAccessKeyId     = "aws_access_key_id"
	awsCredsSecretAccessKey = "aws_secret_access_key"
)

var log = logf.Log.WithName("controller_accountclaim")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new AccountClaim Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileAccountClaim{client: mgr.GetClient(), scheme: mgr.GetScheme()}
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

	return nil
}

var _ reconcile.Reconciler = &ReconcileAccountClaim{}

// ReconcileAccountClaim reconciles a AccountClaim object
type ReconcileAccountClaim struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a AccountClaim object and makes changes based on the state read
// and what is in the AccountClaim.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileAccountClaim) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling AccountClaim")

	// Watch AccountCliaim
	accountClaim := &awsv1alpha1.AccountClaim{}
	err := r.client.Get(context.TODO(), request.NamespacedName, accountClaim)
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

	// Return if this claim has been satisfied
	if accountClaim.Spec.AccountLink != "" && accountClaim.Status.State == awsv1alpha1.ClaimStatusReady {
		reqLogger.Info(fmt.Sprintf("Claim %s has been satisfied ignoring", accountClaim.ObjectMeta.Name))
		return reconcile.Result{}, nil
	}

	if accountClaim.Status.State == "" {
		message := "Attempting to claim account"
		accountClaim.Status.State = awsv1alpha1.ClaimStatusPending

		accountClaim.Status.Conditions = controllerutils.SetAccountClaimCondition(
			accountClaim.Status.Conditions,
			awsv1alpha1.AccountUnclaimed,
			corev1.ConditionTrue,
			AccountClaimed,
			message,
			controllerutils.UpdateConditionNever)
		// Update the Spec on AccountClaim
		return reconcile.Result{}, r.statusUpdate(reqLogger, accountClaim)
	}

	accountList := &awsv1alpha1.AccountList{}

	listOps := &client.ListOptions{Namespace: awsv1alpha1.AccountCrNamespace}
	if err = r.client.List(context.TODO(), listOps, accountList); err != nil {
		return reconcile.Result{}, err
	}

	var unclaimedAccount *awsv1alpha1.Account

	// Get an unclaimed account from the pool
	if accountClaim.Spec.AccountLink == "" {
		unclaimedAccount, err = getUnclaimedAccount(accountList)
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
		setAccountClaimLinkOnAccount(reqLogger, unclaimedAccount, accountClaim)
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

	// Create secret for UHC to consume
	if !r.checkIAMSecretExists(accountClaim.Spec.AwsCredentialSecret.Name, accountClaim.Spec.AwsCredentialSecret.Namespace) {
		err = r.createIAMSecret(reqLogger, accountClaim, unclaimedAccount)
		if err != nil {
			return reconcile.Result{}, nil
		}
	}

	// Set metrics
	accountClaimList := &awsv1alpha1.AccountClaimList{}

	listOps = &client.ListOptions{Namespace: accountClaim.Namespace}
	if err = r.client.List(context.TODO(), listOps, accountList); err != nil {
		return reconcile.Result{}, err
	}

	metrics.UpdateAccountClaimMetrics(accountClaimList)

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

func getUnclaimedAccount(accountList *awsv1alpha1.AccountList) (*awsv1alpha1.Account, error) {
	var unclaimedAccount awsv1alpha1.Account
	var unclaimedAccountFound = false
	time.Sleep(1000 * time.Millisecond)
	// Range through accounts and select the first one that doesn't have a claim link
	for _, account := range accountList.Items {
		if account.Status.Claimed == false && account.Spec.ClaimLink == "" && account.Status.State == "Ready" {
			fmt.Printf("Claiming account: %s", account.ObjectMeta.Name)
			unclaimedAccount = account
			unclaimedAccountFound = true
			break
		}
	}

	if !unclaimedAccountFound {
		return &unclaimedAccount, fmt.Errorf("can't find a ready account to claim")
	}

	return &unclaimedAccount, nil
}

func (r *ReconcileAccountClaim) createIAMSecret(reqLogger logr.Logger, accountClaim *awsv1alpha1.AccountClaim, unclaimedAccount *awsv1alpha1.Account) error {
	// Get secret created by Account controller and copy it to the name/namespace combo that UHC is expecting
	accountIAMUserSecret := &corev1.Secret{}
	objectKey := client.ObjectKey{Namespace: unclaimedAccount.Namespace, Name: unclaimedAccount.Spec.IAMUserSecret}

	err := r.client.Get(context.TODO(), objectKey, accountIAMUserSecret)
	if err != nil {
		reqLogger.Error(err, "Unable to find AWS account STS secret")
		return err
	}

	UHCSecretName := accountClaim.Spec.AwsCredentialSecret.Name
	UHCSecretNamespace := accountClaim.Spec.AwsCredentialSecret.Namespace
	awsAccessKeyID := accountIAMUserSecret.Data[awsCredsAccessKeyId]
	awsSecretAccessKey := accountIAMUserSecret.Data[awsCredsSecretAccessKey]

	if string(awsAccessKeyID) == "" || string(awsSecretAccessKey) == "" {
		reqLogger.Error(err, fmt.Sprintf("Cannot get AWS Credentials from secret %s referenced from Account", unclaimedAccount.Spec.IAMUserSecret))
	}

	UHCSecret := newSecretforCR(UHCSecretName, UHCSecretNamespace, awsAccessKeyID, awsSecretAccessKey)

	err = r.client.Create(context.TODO(), UHCSecret)
	if err != nil {
		reqLogger.Error(err, "Unable to create secret for UHC")
		return err
	}

	reqLogger.Info(fmt.Sprintf("Secret %s created for claim %s", UHCSecret.Name, accountClaim.Name))
	return nil
}

func (r *ReconcileAccountClaim) checkIAMSecretExists(name string, namespace string) bool {
	// Need to check if the secret exists AND that it matches what we're expecting
	secret := corev1.Secret{}
	secretObjectKey := client.ObjectKey{Name: name, Namespace: namespace}
	err := r.client.Get(context.TODO(), secretObjectKey, &secret)
	if err != nil {
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

// setAccountClaimLinkOnAccount sets Account.Spec.ClaimLink to AccountClaim.ObjectMetadata.Name
func setAccountClaimLinkOnAccount(reqLogger logr.Logger, awsAccount *awsv1alpha1.Account, awsAccountClaim *awsv1alpha1.AccountClaim) {
	// Set link on Account
	awsAccount.Spec.ClaimLink = awsAccountClaim.ObjectMeta.Name
	reqLogger.Info(fmt.Sprintf("Account %s ClaimLink set to AccountClaim %s", awsAccount.Name, awsAccountClaim.Name))
}

func setAccountClaimStatus(reqLogger logr.Logger, awsAccount *awsv1alpha1.Account, awsAccountClaim *awsv1alpha1.AccountClaim) {
	message := fmt.Sprintf("Account claimed by %s", awsAccount.Name)
	awsAccountClaim.Status.Conditions = controllerutils.SetAccountClaimCondition(
		awsAccountClaim.Status.Conditions,
		awsv1alpha1.AccountClaimed,
		corev1.ConditionTrue,
		AccountClaimed,
		message,
		controllerutils.UpdateConditionNever)
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
