package accountpool

import (
	"context"
	"fmt"

	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/controller/account"
	"github.com/openshift/aws-account-operator/pkg/controller/utils"
	"github.com/openshift/aws-account-operator/pkg/totalaccountwatcher"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	controllerName = "accountpool"
)

var log = logf.Log.WithName("controller_accountpool")

// Add creates a new AccountPool Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	reconciler := &ReconcileAccountPool{
		client:         utils.NewClientWithMetricsOrDie(log, mgr, controllerName),
		scheme:         mgr.GetScheme(),
		accountWatcher: totalaccountwatcher.TotalAccountWatcher,
	}
	return utils.NewReconcilerWithMetrics(reconciler, controllerName)
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("accountpool-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource AccountPool
	err = c.Watch(&source.Kind{Type: &awsv1alpha1.AccountPool{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &awsv1alpha1.Account{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &awsv1alpha1.AccountPool{},
	})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileAccountPool{}

// ReconcileAccountPool reconciles a AccountPool object
type ReconcileAccountPool struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client         client.Client
	scheme         *runtime.Scheme
	accountWatcher totalaccountwatcher.AccountWatcherIface
}

// Reconcile reads that state of the cluster for a AccountPool object and makes changes based on the state read
// and what is in the AccountPool.Spec
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileAccountPool) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Controller", controllerName, "Request.Namespace", request.Namespace, "Request.Name", request.Name)

	// Fetch the AccountPool instance
	currentAccountPool := &awsv1alpha1.AccountPool{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: request.Name, Namespace: awsv1alpha1.AccountCrNamespace}, currentAccountPool)
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

	// Calculate unclaimed accounts vs claimed accounts
	calculatedStatus, err := r.calculateAccountPoolStatus()
	if err != nil {
		return reconcile.Result{}, err
	}
	// Update the pool size after we calculate all other values
	calculatedStatus.PoolSize = currentAccountPool.Spec.PoolSize

	if shouldUpdateAccountPoolStatus(currentAccountPool, calculatedStatus) {
		currentAccountPool.Status = calculatedStatus
		err = r.client.Status().Update(context.TODO(), currentAccountPool)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	// Get the number of desired unclaimed AWS accounts in the pool
	poolSizeCount := currentAccountPool.Spec.PoolSize
	unclaimedAccountCount := calculatedStatus.UnclaimedAccounts

	reqLogger.Info(fmt.Sprintf("AccountPool Calculations Completed: %+v", calculatedStatus))

	if unclaimedAccountCount >= poolSizeCount {
		reqLogger.Info(fmt.Sprintf("unclaimed account pool satisfied, unclaimedAccounts %d >= poolSize %d", unclaimedAccountCount, poolSizeCount))
		return reconcile.Result{}, nil
	}

	// Create Account CR
	newAccount := account.GenerateAccountCR(awsv1alpha1.AccountCrNamespace)
	utils.AddFinalizer(newAccount, awsv1alpha1.AccountFinalizer)

	// Set AccountPool instance as the owner and controller
	if err := controllerutil.SetControllerReference(currentAccountPool, newAccount, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	reqLogger.Info(fmt.Sprintf("Creating account %s for accountpool. Unclaimed accounts: %d, poolsize%d", newAccount.Name, unclaimedAccountCount, poolSizeCount))
	err = r.client.Create(context.TODO(), newAccount)
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// Calculates the unclaimedAccountCount and Claimed Account Counts
func (r *ReconcileAccountPool) calculateAccountPoolStatus() (awsv1alpha1.AccountPoolStatus, error) {
	unclaimedAccountCount := 0
	claimedAccountCount := 0
	availableAccounts := 0
	accountsProgressing := 0

	//Get the number of actual unclaimed AWS accounts in the pool
	accountList := &awsv1alpha1.AccountList{}

	listOpts := []client.ListOption{
		client.InNamespace(awsv1alpha1.AccountCrNamespace),
	}
	if err := r.client.List(context.TODO(), accountList, listOpts...); err != nil {
		return awsv1alpha1.AccountPoolStatus{}, err
	}

	for _, account := range accountList.Items {
		// if the account is not owned by the accountpool, skip it
		if !account.IsOwnedByAccountPool() {
			continue
		}

		// count unclaimed accounts
		if account.HasNeverBeenClaimed() {
			if !account.IsFailed() {
				unclaimedAccountCount++
			}
		}

		// count claimed accounts
		if account.HasBeenClaimedAtLeastOnce() {
			claimedAccountCount++
		}

		// count available accounts
		if account.HasNeverBeenClaimed() && account.IsReady() {
			availableAccounts++
		}

		// count accounts progressing towards ready by looking at the state
		if account.IsProgressing() {
			accountsProgressing++
		}
	}

	accountDelta := r.calculateAccountDelta()

	return awsv1alpha1.AccountPoolStatus{
		UnclaimedAccounts:   unclaimedAccountCount,
		ClaimedAccounts:     claimedAccountCount,
		AvailableAccounts:   availableAccounts,
		AccountsProgressing: accountsProgressing,
		AWSLimitDelta:       accountDelta,
	}, nil
}

func (r *ReconcileAccountPool) calculateAccountDelta() int {
	accounts := r.accountWatcher.GetAccountCount()
	limit := r.accountWatcher.GetLimit()

	return limit - accounts
}

// We only want to update the account pool status if something in the status has changed
func shouldUpdateAccountPoolStatus(currentAccountPool *awsv1alpha1.AccountPool, calculatedStatus awsv1alpha1.AccountPoolStatus) bool {
	return currentAccountPool.Status != calculatedStatus
}
