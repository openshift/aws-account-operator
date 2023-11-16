package validation

import (
	"context"
	"fmt"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"reflect"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	"github.com/openshift/aws-account-operator/pkg/utils"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	defaultSleepDelay = 10 * time.Second
	logs              = logf.Log.WithName("controller_accountpoolvalidation")
)

type AccountPoolValidationReconciler struct {
	Client           client.Client
	Scheme           *runtime.Scheme
	awsClientBuilder awsclient.IBuilder
}

func (r *AccountPoolValidationReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	reqLogger := logs.WithValues("Controller", "accountpoolvalidation", "Request.Namespace", request.Namespace, "Request.Name", request.Name)

	// Fetch the AccountPool instance
	reqLogger.Info("Fetching accountpool")

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
	cm, err := utils.GetOperatorConfigMap(r.Client)
	if err != nil {
		logs.Error(err, "Could not retrieve the operator configmap")
		return utils.RequeueAfter(5 * time.Minute)
	}

	var isEnabled bool = false

	enabled, err := strconv.ParseBool(cm.Data["feature.accountpool_validation"])
	if err != nil {
		logs.Info("Could not retrieve feature flag 'feature.accountpool_validation' - accountpool validation is disabled")
	} else {
		isEnabled = enabled
	}
	logs.Info("Is accountpool_validation enabled?", "enabled", isEnabled)

	reqLogger.Info("Checking ConfigMap for ServiceQuotas")
	// check if accountpool has servicequota defined in configmap
	reginalServiceQuotas, err := utils.GetServiceQuotasFromAccountPool(reqLogger, currentAccountPool.Name, r.Client)
	if err != nil {
		return reconcile.Result{}, err
	}

	reqLogger.Info("Updating Account ServiceQuotas")
	_, err = r.checkAccountServiceQuota(reqLogger, currentAccountPool.Name, reginalServiceQuotas, isEnabled)
	if err != nil {
		return reconcile.Result{}, err
	}

	return utils.DoNotRequeue()
}

func (r *AccountPoolValidationReconciler) accountSpecUpdate(reqLogger logr.Logger, account *awsv1alpha1.Account) error {
	err := r.Client.Update(context.TODO(), account)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Account spec update for %s failed", account.Name))
	}
	return err
}

func (r *AccountPoolValidationReconciler) accountStatusUpdate(reqLogger logr.Logger, account *awsv1alpha1.Account) error {
	err := r.Client.Status().Update(context.TODO(), account)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Account status update for %s failed", account.Name))
	}
	return err
}

func (r *AccountPoolValidationReconciler) getAccountPoolAccounts(accountPoolName string) ([]awsv1alpha1.Account, error) {
	//Get the number of actual unclaimed AWS accounts in the pool
	accountList := &awsv1alpha1.AccountList{}
	listOpts := []client.ListOption{
		client.InNamespace(awsv1alpha1.AccountCrNamespace),
	}
	if err := r.Client.List(context.TODO(), accountList, listOpts...); err != nil {
		return nil, err
	}
	var accounts []awsv1alpha1.Account
	for _, account := range accountList.Items {
		// gets accounts belonging to poo
		if account.IsOwnedByAccountPool() && account.Spec.AccountPool == accountPoolName {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil

}

// Updates Account Spec ServiceQuotas to match what's in the ConfigMap
func (r *AccountPoolValidationReconciler) checkAccountServiceQuota(reqLogger logr.Logger, accountPoolName string, parsedRegionalServiceQuotas awsv1alpha1.RegionalServiceQuotas, isEnabled bool) (ctrl.Result, error) {
	accountList, err := r.getAccountPoolAccounts(accountPoolName)
	if err != nil {
		reqLogger.Error(err, "Failed to get AccountPool accounts")
		return reconcile.Result{}, err
	}
	var updatedAccountSpecs []awsv1alpha1.Account

	for _, account := range accountList {
		accountCopy := account
		if !reflect.DeepEqual(accountCopy.Spec.RegionalServiceQuotas, parsedRegionalServiceQuotas) && isEnabled {
			accountCopy.Spec.RegionalServiceQuotas = parsedRegionalServiceQuotas

			reqLogger.Info(fmt.Sprintf("Attempting to update the account Spec for: %v", accountCopy.Name))
			err = r.accountSpecUpdate(reqLogger, &accountCopy)
			if err != nil {
				logs.Error(err, "failed to update account spec", "account", accountCopy.Name)
				return reconcile.Result{}, err
			}
			reqLogger.Info(fmt.Sprintf("Successfully updated %v Spec", accountCopy.Name))
			updatedAccountSpecs = append(updatedAccountSpecs, accountCopy)

		} else if !reflect.DeepEqual(accountCopy.Spec.RegionalServiceQuotas, parsedRegionalServiceQuotas) && !isEnabled {
			reqLogger.Info("Accountpool Validation is not enabled")
			reqLogger.Info(fmt.Sprintf("Expected Servicequotas:%v", parsedRegionalServiceQuotas))
			reqLogger.Info(fmt.Sprintf("Account Servicequotas:%v", accountCopy.Spec.RegionalServiceQuotas))
		}
	}

	time.Sleep(defaultSleepDelay) // the delay ensures the most recent accountCR version is used
	updatedAccountList, err := r.getAccountPoolAccounts(accountPoolName)
	if err != nil {
		reqLogger.Error(err, "Failed to get AccountPool updated accounts")
		return reconcile.Result{}, err
	}

	updatedAccountMap := make(map[string]bool)
	for _, item2 := range updatedAccountSpecs {
		updatedAccountMap[item2.Name] = true
	}

	for _, updatedAccount := range updatedAccountList {
		updatedAccountCopy := updatedAccount
		if exists := updatedAccountMap[updatedAccountCopy.ObjectMeta.Name]; exists {
			updatedAccountCopy.Status.RegionalServiceQuotas = make(awsv1alpha1.RegionalServiceQuotas)
			reqLogger.Info(fmt.Sprintf("Attempting to update the account status for: %v", updatedAccountCopy.Name))
			err = r.accountStatusUpdate(reqLogger, &updatedAccountCopy)
			if err != nil {
				logs.Error(err, "failed to update account status", "account", updatedAccountCopy.Name)
				return reconcile.Result{}, err
			}
			reqLogger.Info(fmt.Sprintf("Successfully updated %v Status", updatedAccountCopy.Name))
		}
	}

	return reconcile.Result{}, nil
}

func (r *AccountPoolValidationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.awsClientBuilder = &awsclient.Builder{}
	maxReconciles, err := utils.GetControllerMaxReconciles("accountpoolvalidation")
	if err != nil {
		logs.Error(err, "missing max reconciles for controller", "controller", "accountpoolvalidation")
	}

	rwm := utils.NewReconcilerWithMetrics(r, "accountpoolvalidation")
	return ctrl.NewControllerManagedBy(mgr).
		For(&awsv1alpha1.AccountPool{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxReconciles,
		}).Complete(rwm)
}
