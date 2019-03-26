package accountclaim

import (
	"context"
	"fmt"

	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
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

	accountList := &awsv1alpha1.AccountList{}

	listOps := &client.ListOptions{Namespace: accountClaim.Namespace}
	if err = r.client.List(context.TODO(), listOps, accountList); err != nil {
		return reconcile.Result{}, err
	}

	selectedAccount, err := selectAccount(accountList)
	if err != nil {
		reqLogger.Error(err, "Error selecting account account")
	}

	// Set claim link on Account
	err = setClaimLink(accountClaim, selectedAccount)
	if err != nil {
		// If we got an error log it and reqeue the request
		return reconcile.Result{}, err
	}

	// Update the Spec on Account
	err = r.client.Update(context.TODO(), selectedAccount)
	if err != nil {
		return reconcile.Result{}, err
	}
	// Update the Status on Account
	err = r.client.Status().Update(context.TODO(), selectedAccount)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Set account link on AccountClaim
	setAccountLink(selectedAccount, accountClaim)

	// Update the Spec on AccountClaim
	err = r.client.Update(context.TODO(), accountClaim)
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func selectAccount(accountList *awsv1alpha1.AccountList) (*awsv1alpha1.Account, error) {
	var selectedAccount awsv1alpha1.Account

	// Range through accounts and select the first one that doesn't have a claim link
	for _, account := range accountList.Items {
		if account.Status.Claimed == false && account.Spec.ClaimLink == "" {
			selectedAccount = account
		}
	}

	return &selectedAccount, nil
}

// setClaimLink sets Account.Spec.ClaimLink to AccountClaim.ObjectMetadata.Name
func setClaimLink(awsAccountClaim *awsv1alpha1.AccountClaim, awsAccount *awsv1alpha1.Account) error {
	// Set Account Spec.ClaimLink to name of the claim, fail if its not empty
	// so we can select another account
	// Initially this will naively deal with concurrency
	if awsAccount.Status.Claimed == true || awsAccount.Spec.ClaimLink != "" {
		return fmt.Errorf("AWS Account already claimed by %s, attempting to select another account", awsAccount.Spec.ClaimLink)
	}

	// Set link on Account
	awsAccount.Spec.ClaimLink = awsAccountClaim.ObjectMeta.Name

	// Set Status on Account
	// This shouldn't error but lets log it just incase
	if awsAccount.Status.Claimed != false {
		fmt.Printf("Account Status.Claimed field is %v it should be false\n", awsAccount.Status.Claimed)
	}
	awsAccount.Status.Claimed = true

	return nil
}

// setAccountLink sets AccountClaim.Spec.AccountLink to Account.ObjectMetadata.Name
func setAccountLink(awsAccount *awsv1alpha1.Account, awsAccountClaim *awsv1alpha1.AccountClaim) {
	// This shouldn't error but lets log it just incase
	if awsAccountClaim.Spec.AccountLink != "" {
		fmt.Printf("AccountLink field is already populated for claim: %s, AWS account link is: %s\n", awsAccountClaim.ObjectMeta.Name, awsAccountClaim.Spec.AccountLink)
	}
	// Set link on AccountClaim
	awsAccountClaim.Spec.AccountLink = awsAccount.ObjectMeta.Name
}
