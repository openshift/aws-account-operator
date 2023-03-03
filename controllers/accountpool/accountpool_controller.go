package accountpool

import (
	"context"
	"fmt"
	"strconv"

	"gopkg.in/yaml.v2"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/config"
	"github.com/openshift/aws-account-operator/controllers/account"
	"github.com/openshift/aws-account-operator/pkg/totalaccountwatcher"
	"github.com/openshift/aws-account-operator/pkg/utils"
	"github.com/openshift/aws-account-operator/test/fixtures"
)

const (
	controllerName = "accountpool"
)

var log = logf.Log.WithName("controller_accountpool")

// AccountPoolReconciler reconciles a AccountPool object
type AccountPoolReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	accountWatcher totalaccountwatcher.AccountWatcherIface
}

//+kubebuilder:rbac:groups=aws.managed.openshift.io,resources=accountpools,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=aws.managed.openshift.io,resources=accountpools/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=aws.managed.openshift.io,resources=accountpools/finalizers,verbs=update

// Reconcile reads that state of the cluster for a AccountPool object and makes changes based on the state read
// and what is in the AccountPool.Spec
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *AccountPoolReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	reqLogger := log.WithValues("Controller", controllerName, "Request.Namespace", request.Namespace, "Request.Name", request.Name)

	// Fetch the AccountPool instance
	currentAccountPool := &awsv1alpha1.AccountPool{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: request.Name, Namespace: awsv1alpha1.AccountCrNamespace}, currentAccountPool)
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
	calculatedStatus, err := r.calculateAccountPoolStatus(reqLogger, currentAccountPool.Name)
	if err != nil {
		return reconcile.Result{}, err
	}
	// Update the pool size after we calculate all other values
	calculatedStatus.PoolSize = currentAccountPool.Spec.PoolSize

	if shouldUpdateAccountPoolStatus(currentAccountPool, calculatedStatus) {
		currentAccountPool.Status = calculatedStatus
		err = r.Client.Status().Update(context.TODO(), currentAccountPool)
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
	newAccount.Spec.AccountPool = currentAccountPool.Name
	utils.AddFinalizer(newAccount, awsv1alpha1.AccountFinalizer)

	// Set AccountPool instance as the owner and controller
	if err := controllerutil.SetControllerReference(currentAccountPool, newAccount, r.Scheme); err != nil {
		return reconcile.Result{}, err
	}

	if err = r.handleServiceQuotas(reqLogger, newAccount); err != nil {
		return reconcile.Result{}, err
	}

	reqLogger.Info(fmt.Sprintf("Creating account %s for accountpool. Unclaimed accounts: %d, poolsize%d", newAccount.Name, unclaimedAccountCount, poolSizeCount))
	err = r.Client.Create(context.TODO(), newAccount)
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *AccountPoolReconciler) handleServiceQuotas(reqLogger logr.Logger, account *awsv1alpha1.Account) error {
	reqLogger.Info("Loading Service Quotas")

	cm, err := utils.GetOperatorConfigMap(r.Client)
	if err != nil {
		reqLogger.Error(err, "failed retrieving configmap")
		return err
	}

	accountpoolString, found := cm.Data["accountpool"]
	if !found {
		reqLogger.Error(fixtures.NotFound, "failed getting accountpool data from configmap")
		return fixtures.NotFound
	}

	type Servicequotas map[string]string
	type AccountPool struct {
		IsDefault             bool                     `yaml:"default,omitempty"`
		RegionedServicequotas map[string]Servicequotas `yaml:"servicequotas,omitempty"`
	}

	data := make(map[string]AccountPool)
	err = yaml.Unmarshal([]byte(accountpoolString), &data)

	if err != nil {
		reqLogger.Error(err, "Failed to unmarshal yaml")
		return err
	}

	var parsedRegionalServiceQuotas = make(awsv1alpha1.RegionalServiceQuotas)
	// If the pool we've specified in the account doesn't exist, we need to error out
	if poolData, ok := data[account.Spec.AccountPool]; !ok {
		reqLogger.Error(fixtures.NotFound, "Accountpool not found")
		return fixtures.NotFound
	} else {
		// for each service quota in a given region, we'll need to parse and save to use in the account spec.
		for regionName, serviceQuota := range poolData.RegionedServicequotas {
			var parsedServiceQuotas = make(awsv1alpha1.AccountServiceQuota)
			for quotaCode, quotaValue := range serviceQuota {
				qv, _ := strconv.Atoi(quotaValue)
				parsedServiceQuotas[awsv1alpha1.SupportedServiceQuotas(quotaCode)] = &awsv1alpha1.ServiceQuotaStatus{
					Value: qv,
				}
			}
			parsedRegionalServiceQuotas[regionName] = parsedServiceQuotas
		}
	}
	reqLogger.Info("Loaded Service Quotas")

	account.Spec.RegionalServiceQuotas = parsedRegionalServiceQuotas
	account.Status.RegionalServiceQuotas = make(awsv1alpha1.RegionalServiceQuotas)

	return nil
}

// Calculates the unclaimedAccountCount and Claimed Account Counts
func (r *AccountPoolReconciler) calculateAccountPoolStatus(reqLogger logr.Logger, poolName string) (awsv1alpha1.AccountPoolStatus, error) {
	unclaimedAccountCount := 0
	claimedAccountCount := 0
	availableAccounts := 0
	accountsProgressing := 0

	//Get the number of actual unclaimed AWS accounts in the pool
	accountList := &awsv1alpha1.AccountList{}

	listOpts := []client.ListOption{
		client.InNamespace(awsv1alpha1.AccountCrNamespace),
	}
	if err := r.Client.List(context.TODO(), accountList, listOpts...); err != nil {
		return awsv1alpha1.AccountPoolStatus{}, err
	}

	for _, account := range accountList.Items {
		// if the account is not owned by the accountpool, skip it
		if !account.IsOwnedByAccountPool() {
			continue
		}

		// Special intermediary case until all account crs have had their account.Spec.AccountPool set appropriately.
		// If account.Spec.AccountPool is empty, we count it as if it's from the default accountpool.
		if account.Spec.AccountPool == "" {
			defaultPoolName, err := config.GetDefaultAccountPoolName(reqLogger, r.Client)

			if err != nil {
				reqLogger.Error(err, "error getting default accountpool name")
				return awsv1alpha1.AccountPoolStatus{}, err
			}

			if poolName != defaultPoolName {
				continue
			}
		} else {
			// If an accountpool name is specified, we want to count ONLY that pool
			if account.Spec.AccountPool != poolName {
				continue
			}
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

func (r *AccountPoolReconciler) calculateAccountDelta() int {
	accounts := r.accountWatcher.GetAccountCount()
	limit := r.accountWatcher.GetLimit()

	return limit - accounts
}

// We only want to update the account pool status if something in the status has changed
func shouldUpdateAccountPoolStatus(currentAccountPool *awsv1alpha1.AccountPool, calculatedStatus awsv1alpha1.AccountPoolStatus) bool {
	return currentAccountPool.Status != calculatedStatus
}

// SetupWithManager sets up the controller with the Manager.
func (r *AccountPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.accountWatcher = totalaccountwatcher.TotalAccountWatcher
	maxReconciles, err := utils.GetControllerMaxReconciles(controllerName)
	if err != nil {
		log.Error(err, "missing max reconciles for controller", "controller", controllerName)
	}

	rwm := utils.NewReconcilerWithMetrics(r, controllerName)
	return ctrl.NewControllerManagedBy(mgr).
		For(&awsv1alpha1.AccountPool{}).
		Owns(&awsv1alpha1.Account{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxReconciles,
		}).Complete(rwm)
}
