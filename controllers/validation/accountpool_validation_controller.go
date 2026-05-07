package validation

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"time"

	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

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

const (
	validationControllerName = "accountpoolvalidation"
)

type AccountPoolValidationReconciler struct {
	Client           client.Client
	Scheme           *runtime.Scheme
	awsClientBuilder awsclient.IBuilder
}

func (r *AccountPoolValidationReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	reqLogger := logs.WithValues("Controller", validationControllerName, "Request.Namespace", request.Namespace, "Request.Name", request.Name)

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

	var isEnabled = false

	enabled, err := strconv.ParseBool(cm.Data["feature.accountpool_validation"])
	if err != nil {
		logs.Info("Could not retrieve feature flag 'feature.accountpool_validation' - accountpool validation is disabled")
	} else {
		isEnabled = enabled
	}
	logs.Info("Is accountpool_validation enabled?", "enabled", isEnabled)

	reqLogger.Info("Checking ConfigMap for ServiceQuotas")
	// check if accountpool has servicequota defined in configmap
	regionalServiceQuotas, globalServiceQuotas, err := utils.GetServiceQuotasFromAccountPool(ctx, reqLogger, currentAccountPool.Name, r.Client)
	if err != nil {
		return reconcile.Result{}, err
	}

	reqLogger.Info("Updating Account ServiceQuotas")
	if err = r.checkAccountServiceQuota(ctx, reqLogger, currentAccountPool.Name, regionalServiceQuotas, globalServiceQuotas, isEnabled); err != nil {
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

func (r *AccountPoolValidationReconciler) getAccountPoolAccounts(ctx context.Context, accountPoolName string) ([]awsv1alpha1.Account, error) {
	//Get the number of actual unclaimed AWS accounts in the pool
	accountList := &awsv1alpha1.AccountList{}
	listOpts := []client.ListOption{
		client.InNamespace(awsv1alpha1.AccountCrNamespace),
	}
	if err := r.Client.List(ctx, accountList, listOpts...); err != nil {
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

// checkAccountServiceQuota updates Account Spec service quotas to match what's in the ConfigMap,
// covering both per-region and account-global quotas.
func (r *AccountPoolValidationReconciler) checkAccountServiceQuota(ctx context.Context, reqLogger logr.Logger, accountPoolName string, parsedRegionalServiceQuotas awsv1alpha1.RegionalServiceQuotas, parsedGlobalServiceQuotas awsv1alpha1.AccountServiceQuota, isEnabled bool) error {
	accountList, err := r.getAccountPoolAccounts(ctx, accountPoolName)
	if err != nil {
		reqLogger.Error(err, "Failed to get AccountPool accounts")
		return err
	}
	var updatedAccountSpecs []awsv1alpha1.Account

	for _, account := range accountList {
		accountCopy := account
		// Skip accounts with pause reconciliation annotation
		if accountCopy.Annotations[PauseReconciliationAnnotation] == "true" {
			reqLogger.Info("Skipping account with pause reconciliation annotation", "account", accountCopy.Name)
			continue
		}

		regionalDrift := !reflect.DeepEqual(accountCopy.Spec.RegionalServiceQuotas, parsedRegionalServiceQuotas)
		globalDrift := !reflect.DeepEqual(accountCopy.Spec.GlobalServiceQuotas, parsedGlobalServiceQuotas)

		if (regionalDrift || globalDrift) && isEnabled {
			accountCopy.Spec.RegionalServiceQuotas = parsedRegionalServiceQuotas
			accountCopy.Spec.GlobalServiceQuotas = parsedGlobalServiceQuotas

			reqLogger.Info(fmt.Sprintf("Attempting to update the account Spec for: %v", accountCopy.Name))
			err = r.accountSpecUpdate(reqLogger, &accountCopy)
			if err != nil {
				logs.Error(err, "failed to update account spec", "account", accountCopy.Name)
				return err
			}
			reqLogger.Info(fmt.Sprintf("Successfully updated %v Spec", accountCopy.Name))
			updatedAccountSpecs = append(updatedAccountSpecs, accountCopy)
		} else if (regionalDrift || globalDrift) && !isEnabled {
			reqLogger.Info("Accountpool Validation is not enabled")
			reqLogger.Info(fmt.Sprintf("Expected Regional Servicequotas:%v", parsedRegionalServiceQuotas))
			reqLogger.Info(fmt.Sprintf("Account Regional Servicequotas:%v", accountCopy.Spec.RegionalServiceQuotas))
			reqLogger.Info(fmt.Sprintf("Expected Global Servicequotas:%v", parsedGlobalServiceQuotas))
			reqLogger.Info(fmt.Sprintf("Account Global Servicequotas:%v", accountCopy.Spec.GlobalServiceQuotas))
		}
	}

	time.Sleep(defaultSleepDelay) // the delay ensures the most recent accountCR version is used
	updatedAccountList, err := r.getAccountPoolAccounts(ctx, accountPoolName)
	if err != nil {
		reqLogger.Error(err, "Failed to get AccountPool updated accounts")
		return err
	}

	updatedAccountMap := make(map[string]bool)
	for _, item2 := range updatedAccountSpecs {
		updatedAccountMap[item2.Name] = true
	}

	for _, updatedAccount := range updatedAccountList {
		updatedAccountCopy := updatedAccount
		// Skip accounts with pause reconciliation annotation
		if updatedAccountCopy.Annotations[PauseReconciliationAnnotation] == "true" {
			reqLogger.Info("Skipping account with pause reconciliation annotation", "account", updatedAccountCopy.Name)
			continue
		}
		if exists := updatedAccountMap[updatedAccountCopy.Name]; exists {
			updatedAccountCopy.Status.RegionalServiceQuotas = make(awsv1alpha1.RegionalServiceQuotas)
			updatedAccountCopy.Status.GlobalServiceQuotas = make(awsv1alpha1.AccountServiceQuota)
			reqLogger.Info(fmt.Sprintf("Attempting to update the account status for: %v", updatedAccountCopy.Name))
			err = r.accountStatusUpdate(reqLogger, &updatedAccountCopy)
			if err != nil {
				logs.Error(err, "failed to update account status", "account", updatedAccountCopy.Name)
				return err
			}
			reqLogger.Info(fmt.Sprintf("Successfully updated %v Status", updatedAccountCopy.Name))
		}
	}

	return nil
}

func (r *AccountPoolValidationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.awsClientBuilder = &awsclient.Builder{}
	maxReconciles, err := utils.GetControllerMaxReconciles(validationControllerName)
	if err != nil {
		logs.Error(err, "missing max reconciles for controller", "controller", validationControllerName)
	}

	rwm := utils.NewReconcilerWithMetrics(r, validationControllerName)
	return ctrl.NewControllerManagedBy(mgr).
		For(&awsv1alpha1.AccountPool{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxReconciles,
		}).Complete(rwm)
}
