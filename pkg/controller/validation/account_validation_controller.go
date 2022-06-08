package validation

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/organizations"
	"github.com/openshift/aws-account-operator/config"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/controller/utils"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_accountvalidation")

var account_move_enabled = false

const (
	controllerName = "accountvalidation"
	moveWaitTime   = 5 * time.Minute
)

type ValidateAccount struct {
	Client           client.Client
	scheme           *runtime.Scheme
	awsClientBuilder awsclient.IBuilder
}

type ValidationError int64

const (
	InvalidAccount ValidationError = iota
	AccountMoveFailed
)

type AccountValidationError struct {
	Type ValidationError
	Err  error
}

func (ave *AccountValidationError) Error() string {
	return ave.Err.Error()
}

func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

func add(mgr manager.Manager, r reconcile.Reconciler) error {
	c, err := utils.NewControllerWithMaxReconciles(log, controllerName, mgr, r)
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &awsv1alpha1.Account{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	reconciler := &ValidateAccount{
		Client:           utils.NewClientWithMetricsOrDie(log, mgr, controllerName),
		scheme:           mgr.GetScheme(),
		awsClientBuilder: &awsclient.Builder{},
	}

	return utils.NewReconcilerWithMetrics(reconciler, controllerName)
}

// Retrieve all parents of the given awsId until the predicate returns true.
func ParentsTillPredicate(awsId string, client awsclient.Client, p func(s string) bool, parents *[]string) error {
	listParentsInput := organizations.ListParentsInput{
		ChildId: aws.String(awsId),
	}
	listParentsOutput, err := client.ListParents(&listParentsInput)
	if err != nil {
		return err
	}
	if len(listParentsOutput.Parents) == 0 {
		log.Info("Exhausted search looking for root OU - root OU and account OU likely in separate subtrees.", "path", parents)
		return nil
	} else {
		id := *listParentsOutput.Parents[0].Id
		parents = append(parents, id)
		if p(id) {
			return nil
		}
		return ParentsTillPredicate(id, client, p, parents)
	}
}

// Verify if the account is already in the root OU
// The predicate indicates if the parent considered the desired root was found.
func IsAccountInPoolOU(account awsv1alpha1.Account, client awsclient.Client, isPoolOU func(s string) bool) bool {
	if account.Spec.AwsAccountID == "" {
		return false, errors.New("AwsAccountID is empty.")
	}
	parentList := []string{}
	err := ParentsTillPredicate(account.Spec.AwsAccountID, client, isPoolOU, &parentList)
	if err != nil {
		return false, err
	}
	if len(parentList) == 1 {
		return true
	}
	return false
}

func MoveAccount(account awsv1alpha1.Account, client awsclient.Client, targetOU string, dryRun bool) error {
	awsAccountId := account.Spec.AwsAccountID

	listParentsInput := organizations.ListParentsInput{
		ChildId: aws.String(awsAccountId),
	}
	listParentsOutput, err := client.ListParents(&listParentsInput)
	if err != nil {
		log.Error(err, "Can not find parent for AWS account", "aws-account", awsAccountId)
		return err
	}
	oldOu := listParentsOutput.Parents[0].Id
	moveAccountInput := organizations.MoveAccountInput{
		AccountId:           aws.String(awsAccountId),
		DestinationParentId: aws.String(targetOU),
		SourceParentId:      oldOu,
	}
	if !dryRun {
		log.Info("Moving aws account from old ou to new ou", "aws-account", awsAccountId, "old-ou", *oldOu, "new-ou", targetOU)
		_, err = client.MoveAccount(&moveAccountInput)
		if err != nil {
			log.Error(err, "Could not move aws account to new ou", "aws-account", awsAccountId, "ou", targetOU)
			return err
		}
	} else {
		log.Info("Not moving aws account from old ou to new ou (dry run)", "aws-account", awsAccountId, "old-ou", *oldOu, "new-ou", targetOU)
	}
	return nil
}

func (r *ValidateAccount) ValidateAccountOU(awsClient awsclient.Client, account awsv1alpha1.Account, configMap *v1.ConfigMap) error {
	poolOU := configMap.Data["root"]
	// Perform basic short-circuit checks
	if account.IsBYOC() {
		log.Info("Will not validate a CCS account", "account", account)
		return &AccountValidationError{
			Type: InvalidAccount,
			Err:  errors.New("Account is a CCS account"),
		}
	}
	if account.IsOwnedByAccountPool() {
		log.Info("Will not validate account owned by account pool", account)
		return &AccountValidationError{
			Type: InvalidAccount,
			Err:  errors.New("Account is in an account pool"),
		}
	}

	// Perform all checks on the account we want.
	inPool := IsAccountInPoolOU(account, awsClient, func(s string) bool {
		return s == poolOU
	})
	if inPool {
		log.Info("Account is already in the root OU.", "account", account)
	} else {
		log.Info("Account is not in the root OU - it will be moved.", "account", account)
		err := MoveAccount(account, awsClient, poolOU, account_move_enabled)
		if err != nil {
			log.Error(err, "Could not move account", "account", account)
			return &AccountValidationError{
				Type: AccountMoveFailed,
				Err:  err,
			}
		}
	}
	return nil
}

func (r *ValidateAccount) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	log.WithValues("Controller", controllerName, "Request.Namespace", request.Namespace, "Request.Name", request.Name)

	// Setup: retrieve account and awsClient
	var account awsv1alpha1.Account
	err := r.Client.Get(context.TODO(), request.NamespacedName, &account)
	if err != nil {
		log.Error(err, "Could not retrieve account to validate", "account-request", request.NamespacedName)
		return utils.DoNotRequeue()
	}

	cm, err := utils.GetOperatorConfigMap(r.Client)
	if err != nil {
		log.Error(err, "Could not retrieve the operator configmap")
		return utils.RequeueAfter(5 * time.Minute)
	}

	enabled, err := strconv.ParseBool(cm.Data["feature.validation_move_account"])
	if err != nil {
		log.Info("Could not retrieve feature flag 'feature.validation_move_account' - account moving is disabled")
	} else {
		account_move_enabled = enabled
	}
	log.Info("Is moving accounts enabled?", "enabled", account_move_enabled)

	awsClientInput := awsclient.NewAwsClientInput{
		AwsRegion:  config.GetDefaultRegion(),
		SecretName: utils.AwsSecretName,
		NameSpace:  awsv1alpha1.AccountCrNamespace,
	}
	awsClient, err := r.awsClientBuilder.GetClient(controllerName, r.Client, awsClientInput)
	if err != nil {
		log.Error(err, "Could not retrieve AWS client.")
	}

	// Perform any checks we want
	err = r.ValidateAccountOU(awsClient, account, cm)
	if err != nil {
		// Decide who we will requeue now
		validationError, ok := err.(*AccountValidationError)
		if ok {
			if validationError.Type == InvalidAccount {
				return utils.DoNotRequeue()
			}
			if validationError.Type == AccountMoveFailed {
				return utils.RequeueAfter(moveWaitTime)
			}
		}
	}

	return utils.DoNotRequeue()
}
