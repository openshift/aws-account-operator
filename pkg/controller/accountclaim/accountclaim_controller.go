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

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner AccountClaim
	// err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
	// 	IsController: true,
	// 	OwnerType:    &awsv1alpha1.AccountClaim{},
	// })
	// if err != nil {
	// 	return err
	// }

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
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
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
		err = r.client.Status().Update(context.TODO(), accountClaim)
		if err != nil {
			return reconcile.Result{}, err
		}
		err := r.client.Get(context.TODO(), request.NamespacedName, accountClaim)
		if err != nil {
			reqLogger.Error(err, "Unable to refresh claim")
		}
	}

	if accountClaim.Status.State == awsv1alpha1.ClaimStatusPending {
		if accountClaim.Spec.AccountLink != "" {
			reqLogger.Info(fmt.Sprintf("Updating claim %s state to true", accountClaim.Name))
			accountClaim.Status.State = awsv1alpha1.ClaimStatusReady
			err = r.client.Status().Update(context.TODO(), accountClaim)
			if err != nil {
				reqLogger.Error(err, "Unable to update claim state to true")
				return reconcile.Result{}, err
			}
			err := r.client.Get(context.TODO(), request.NamespacedName, accountClaim)
			if err != nil {
				reqLogger.Error(err, "Unable to get claim after state update")
			}
			// Stop here no need to continue for the claim
			return reconcile.Result{}, nil
		}

	}
	accountList := &awsv1alpha1.AccountList{}

	listOps := &client.ListOptions{Namespace: awsv1alpha1.AccountCrNamespace}
	if err = r.client.List(context.TODO(), listOps, accountList); err != nil {
		return reconcile.Result{}, err
	}

	// Get an unclaimed account from the pool
	unclaimedAccount, err := getUnclaimedAccount(accountList)
	if err != nil {
		reqLogger.Error(err, "Unable to select an unclaimed account from the pool")
		return reconcile.Result{}, err
	}

	// Set claim link on Account
	err = setAccountLinkToClaim(reqLogger, unclaimedAccount, accountClaim)
	if err != nil {
		// If we got an error log it and reqeue the request
		reqLogger.Error(err, "Unable to set account link on claim")
		return reconcile.Result{}, err
	}
	reqLogger.Info(fmt.Sprintf("Claim %s has been satisfied ignoring", accountClaim.ObjectMeta.Name))

	// Update the Spec on Account
	err = r.client.Update(context.TODO(), unclaimedAccount)
	if err != nil {
		return reconcile.Result{}, err
	}

	unclaimedAccountObjectKey, err := client.ObjectKeyFromObject(unclaimedAccount)
	if err != nil {
		reqLogger.Error(err, "Unable to get name and namespace of Acccount object")
	}

	// Get updated Account object
	err = r.client.Get(context.TODO(), unclaimedAccountObjectKey, unclaimedAccount)
	if err != nil {
		reqLogger.Error(err, "Unable to get updated Acccount object")
		return reconcile.Result{}, err
	}

	// Set Account status to claimed
	setAccountStatusClaimed(reqLogger, unclaimedAccount, accountClaim)

	// selectedAccountPrettyPrint, err := json.MarshalIndent(unclaimedAccount, "", "  ")
	// if err != nil {
	// 	fmt.Printf("Error unmarshalling json: %s", err)
	// }

	// Update the Status on Account
	err = r.client.Status().Update(context.TODO(), unclaimedAccount)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Unable to update the status of Account %s to claimed", unclaimedAccount.Name))
		return reconcile.Result{}, err
	}
	// Refrest Account
	err = r.client.Get(context.TODO(), unclaimedAccountObjectKey, unclaimedAccount)
	if err != nil {
		reqLogger.Error(err, "Unable to get updated Acccount object after status update")
		return reconcile.Result{}, err
	}

	claimObjectKey, err := client.ObjectKeyFromObject(accountClaim)
	if err != nil {
		reqLogger.Error(err, "Unable to get name and namespace of Acccount object")
	}
	// Refresh AccountClaim
	err = r.client.Get(context.TODO(), claimObjectKey, accountClaim)
	if err != nil {
		reqLogger.Error(err, "Unable to get updated AcccountClaim object after status update")
		return reconcile.Result{}, err
	}

	// Set account link on AccountClaim
	setAccountLink(reqLogger, unclaimedAccount, accountClaim)

	// Update the Spec on AccountClaim
	err = r.client.Update(context.TODO(), accountClaim)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Get updated AccountClaim object
	err = r.client.Get(context.TODO(), claimObjectKey, accountClaim)
	if err != nil {
		reqLogger.Error(err, "Unable to get updated AcccountClaim object")
		return reconcile.Result{}, err
	}

	// Set AccountClaim status
	setAccountClaimStatus(reqLogger, unclaimedAccount, accountClaim)

	// Update the Spec on AccountClaim
	err = r.client.Status().Update(context.TODO(), accountClaim)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Get secret created by Account controller and copy it to the name/namespace combo that UHC is expecting
	accountIAMUserSecret := &corev1.Secret{}
	objectKey := client.ObjectKey{Namespace: unclaimedAccount.Namespace, Name: unclaimedAccount.Spec.IAMUserSecret}

	err = r.client.Get(context.TODO(), objectKey, accountIAMUserSecret)
	if err != nil {
		return reconcile.Result{}, err
	}

	UHCSecretName := accountClaim.Spec.AwsCredentialSecret.Name
	UHCSecretNamespace := accountClaim.Spec.AwsCredentialSecret.Namespace
	awsAccessKeyID := accountIAMUserSecret.Data[awsCredsAccessKeyId]
	awsSecretAccessKey := accountIAMUserSecret.Data[awsCredsSecretAccessKey]
	if string(awsAccessKeyID) == "" || string(awsSecretAccessKey) == "" {
		reqLogger.Error(err, "Cannot get AWS Credentials from secret referenced from Account")
	}
	UHCSecret := newSecretforCR(UHCSecretName, UHCSecretNamespace, awsAccessKeyID, awsSecretAccessKey)

	err = r.client.Create(context.TODO(), UHCSecret)
	if err != nil {
		reqLogger.Error(err, "Unable to create secret for UHC")
		return reconcile.Result{}, err
	}

	accountClaimList := &awsv1alpha1.AccountClaimList{}

	listOps = &client.ListOptions{Namespace: accountClaim.Namespace}
	if err = r.client.List(context.TODO(), listOps, accountList); err != nil {
		return reconcile.Result{}, err
	}

	metrics.UpdateAccountClaimMetrics(accountClaimList)
	return reconcile.Result{}, nil
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

// setAccountLinkToClaim sets Account.Spec.ClaimLink to AccountClaim.ObjectMetadata.Name
func setAccountLinkToClaim(reqLogger logr.Logger, awsAccount *awsv1alpha1.Account, awsAccountClaim *awsv1alpha1.AccountClaim) error {
	// Initially this will naively deal with concurrency
	if awsAccount.Status.Claimed == true || awsAccount.Spec.ClaimLink != "" {
		return fmt.Errorf("AWS Account already claimed by %s, attempting to select another account", awsAccount.Spec.ClaimLink)
	}

	// Set link on Account
	awsAccount.Spec.ClaimLink = awsAccountClaim.ObjectMeta.Name
	reqLogger.Info(fmt.Sprintf("Account %s ClaimLink set to AccountClaim %s", awsAccount.Name, awsAccountClaim.Name))
	return nil
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

func setAccountStatusClaimed(reqLogger logr.Logger, awsAccount *awsv1alpha1.Account, awsAccountClaim *awsv1alpha1.AccountClaim) {
	// Set Status on Account
	// This shouldn't error but lets log it just incase
	if awsAccount.Status.Claimed != false {
		fmt.Printf("Account Status.Claimed field is %v it should be false\n", awsAccount.Status.Claimed)
	}

	// Set Account status to claimed
	awsAccount.Status.Claimed = true
	reqLogger.Info(fmt.Sprintf("Account %s status updated", awsAccountClaim.Name))
}

// setAccountLink sets AccountClaim.Spec.AccountLink to Account.ObjectMetadata.Name
func setAccountLink(reqLogger logr.Logger, awsAccount *awsv1alpha1.Account, awsAccountClaim *awsv1alpha1.AccountClaim) {
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
