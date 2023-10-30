package validation

import (
	"context"
	"fmt"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"gopkg.in/yaml.v2"
	"reflect"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	"github.com/openshift/aws-account-operator/pkg/utils"
	"github.com/openshift/aws-account-operator/test/fixtures"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	defaultSleepDelay = 500 * time.Millisecond
	logs              = logf.Log.WithName("controller_accountpoolvalidation")
)

const (
	ControllerName = "accountpoolvalidation"
)

type AccountPoolValidationReconciler struct {
	Client           client.Client
	Scheme           *runtime.Scheme
	awsClientBuilder awsclient.IBuilder
}

func (r *AccountPoolValidationReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	reqLogger := logs.WithValues("Controller", ControllerName, "Request.Namespace", request.Namespace, "Request.Name", request.Name)

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

	var accountPoolValidationEnabled bool = false

	enabled, err := strconv.ParseBool(cm.Data["feature.accountpool_validation"])
	if err != nil {
		logs.Info("Could not retrieve feature flag 'feature.accountpool_validation' - accountpool validation is disabled")
	} else {
		accountPoolValidationEnabled = enabled
	}
	logs.Info("Is accountpool_validation enabled?", "enabled", accountPoolValidationEnabled)

	reqLogger.Info("Checking ConfigMap for ServiceQuotas")
	// check if accountpool has servicequota defined in configmap
	reginalServiceQuotas, err := r.getAccountPoolRegionalServiceQuota(reqLogger, currentAccountPool.Name)
	if err != nil {
		return reconcile.Result{}, err
	}

	reqLogger.Info("Updating Account ServiceQuotas")
	_, err = r.checkAccountServiceQuota(reqLogger, currentAccountPool.Name, reginalServiceQuotas, accountPoolValidationEnabled)
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

// Gets ServiceQuota from ConfigMap
func (r *AccountPoolValidationReconciler) getAccountPoolRegionalServiceQuota(reqLogger logr.Logger, accountPoolName string) (awsv1alpha1.RegionalServiceQuotas, error) {
	reqLogger.Info("Loading Service Quotas")

	cm, err := utils.GetOperatorConfigMap(r.Client)
	if err != nil {
		reqLogger.Error(err, "failed retrieving configmap")
		return nil, err
	}

	accountpoolString, found := cm.Data["accountpool"]
	if !found {
		reqLogger.Error(fixtures.NotFound, "failed getting accountpool data from configmap")
		return nil, fixtures.NotFound
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
		return nil, err
	}

	var parsedRegionalServiceQuotas = make(awsv1alpha1.RegionalServiceQuotas)

	if poolData, ok := data[accountPoolName]; !ok {
		reqLogger.Error(fixtures.NotFound, "Accountpool not found")
		return nil, fixtures.NotFound
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

	return parsedRegionalServiceQuotas, nil
}

// Updates Account Spec ServiceQuotas to match what's in the ConfigMap
func (r *AccountPoolValidationReconciler) checkAccountServiceQuota(reqLogger logr.Logger, accountPoolName string, parsedRegionalServiceQuotas awsv1alpha1.RegionalServiceQuotas, accountPoolValidationEnabled bool) (ctrl.Result, error) {
	accountList, err := r.getAccountPoolAccounts(accountPoolName)
	if err != nil {
		reqLogger.Error(err, "Failed to get AccountPool accounts")
		return reconcile.Result{}, err
	}
	var (
		updatedAccountSpecs []awsv1alpha1.Account
		accountPtr          *awsv1alpha1.Account
	)

	for _, account := range accountList {
		accountPtr = &account
		if !reflect.DeepEqual(accountPtr.Spec.RegionalServiceQuotas, parsedRegionalServiceQuotas) {
			accountPtr.Spec.RegionalServiceQuotas = parsedRegionalServiceQuotas
			if !accountPoolValidationEnabled {
				reqLogger.Info("Accountpool Validation is not enabled")
				reqLogger.Info(fmt.Sprintf("Expected Servicequotas:%v", parsedRegionalServiceQuotas))
				reqLogger.Info(fmt.Sprintf("Account Servicequotas:%v", accountPtr.Spec.RegionalServiceQuotas))
				return reconcile.Result{}, nil
			}

			reqLogger.Info(fmt.Sprintf("Attempting to update the account Spec for: %v", accountPtr))
			err = r.accountSpecUpdate(reqLogger, accountPtr)
			if err != nil {
				logs.Error(err, "failed to update account spec", "account", accountPtr)
				return reconcile.Result{}, err
			}
			reqLogger.Info(fmt.Sprintf("Successfully updated %v Spec", accountPtr))
			updatedAccountSpecs = append(updatedAccountSpecs, *accountPtr)
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
	var updatedAccountPtr *awsv1alpha1.Account

	for _, updatedAccount := range updatedAccountList {
		updatedAccountPtr = &updatedAccount
		if exists := updatedAccountMap[updatedAccountPtr.ObjectMeta.Name]; exists {
			updatedAccountPtr.Status.RegionalServiceQuotas = make(awsv1alpha1.RegionalServiceQuotas)
			reqLogger.Info(fmt.Sprintf("Attempting to update the account status for: %v", updatedAccountPtr))
			err = r.accountStatusUpdate(reqLogger, updatedAccountPtr)
			if err != nil {
				logs.Error(err, "failed to update account status", "account", updatedAccountPtr)
				return reconcile.Result{}, err
			}
			reqLogger.Info(fmt.Sprintf("Successfully updated %v Status", updatedAccountPtr))
		}
	}

	return reconcile.Result{}, nil
}

func (r *AccountPoolValidationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.awsClientBuilder = &awsclient.Builder{}
	maxReconciles, err := utils.GetControllerMaxReconciles(controllerName)
	if err != nil {
		logs.Error(err, "missing max reconciles for controller", "controller", controllerName)
	}

	rwm := utils.NewReconcilerWithMetrics(r, "accountpoolvalidation")
	return ctrl.NewControllerManagedBy(mgr).
		For(&awsv1alpha1.AccountPool{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxReconciles,
		}).Complete(rwm)
}
