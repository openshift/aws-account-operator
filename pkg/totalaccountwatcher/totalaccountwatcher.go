package totalaccountwatcher

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/organizations"
	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/config"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/localmetrics"
	controllerutils "github.com/openshift/aws-account-operator/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// ErrAwsAccountLimitExceeded indicates the organization account limit has been reached.
var ErrAwsAccountLimitExceeded = errors.New("AccountLimitExceeded")

// TotalAccountWatcher global var for TotalAccountWatcher
var TotalAccountWatcher = &AccountWatcher{}

var log = logf.Log.WithName("aws-account-operator")

type AccountWatcherIface interface {
	GetAccountCount() int
	GetLimit() int
}

type AccountWatcher struct {
	watchInterval        time.Duration
	awsClient            awsclient.Client
	client               client.Client
	total                int
	accountsCanBeCreated bool
	limit                int
}

// initialize creates a global instance of the TotalAccountWatcher
func initialize(client client.Client, watchInterval time.Duration) *AccountWatcher {
	log.Info("Initializing the totalAccountWatcher")

	awsRegion := config.GetDefaultRegion()

	// NOTE(efried): This is a snowflake use of awsclient.IBuilder. Everyone else puts the
	// IBuilder in their struct and uses it to GetClient() dynamically as needed. This one grabs a
	// single client one time and stores it in a global.
	builder := &awsclient.Builder{}
	awsClient, err := builder.GetClient("", client, awsclient.NewAwsClientInput{
		SecretName: controllerutils.AwsSecretName,
		NameSpace:  awsv1alpha1.AccountCrNamespace,
		AwsRegion:  awsRegion,
	})

	if err != nil {
		log.Error(err, "Failed to get AwsClient")
		return TotalAccountWatcher
	}

	TotalAccountWatcher = newTotalAccountWatcher(client, awsClient, watchInterval)
	err = TotalAccountWatcher.UpdateTotalAccounts(log)
	if err != nil {
		log.Error(err, "failed updating total accounts count")
	}
	return TotalAccountWatcher
}

// newTotalAccountWatcher returns a new instance of the TotalAccountWatcher interface
func newTotalAccountWatcher(
	client client.Client,
	awsClient awsclient.Client,
	watchInterval time.Duration,
) *AccountWatcher {
	return &AccountWatcher{
		watchInterval: watchInterval,
		awsClient:     awsClient,
		client:        client,
		// Initialize this to be false by default
		accountsCanBeCreated: false,
	}
}

// TotalAccountWatcher will trigger AwsLimitUpdate every `scanInternal` and only stop if the operator is killed or a
// message is sent on the stopCh
func (s *AccountWatcher) Start(log logr.Logger, stopCh context.Context, client client.Client, watchInterval time.Duration) {
	log.Info("Starting the totalAccountWatcher")
	s = initialize(client, watchInterval)
	for {
		select {
		case <-time.After(s.watchInterval):
			err := s.UpdateTotalAccounts(log)
			if err != nil {
				log.Error(err, "totalAccountWatcher not started, awsLimit won't be updated")
			}
		case <-stopCh.Done():
			log.Info("Stopping the totalAccountWatcher")
			break
		}
	}
}

// UpdateTotalAccounts will update the TotalAccountWatcher's total field
func (s *AccountWatcher) UpdateTotalAccounts(log logr.Logger) error {

	accountTotal, err := s.getTotalAwsAccounts()
	if err != nil {
		log.Error(err, "Failed to get account list with error code")
		// Stop account creation while we can't talk to AWS
		s.accountsCanBeCreated = false
		return err
	}
	localmetrics.Collector.SetTotalAWSAccounts(accountTotal)

	if accountTotal != s.total {
		log.Info(fmt.Sprintf("Updating total from %d to %d", s.total, accountTotal))
		s.total = accountTotal
	}

	// AccountsCanBeCreated is a bool that returns the opposite of accountLimitReached.
	// If the account limit is reached, we do NOT want to create accounts.  However, if the
	// account limit has NOT been reached, then account creation can happen.
	limitReached, err := s.accountLimitReached(log, accountTotal)
	if err != nil {
		s.accountsCanBeCreated = false
		return err
	}
	s.accountsCanBeCreated = (!limitReached)
	return nil
}

// TotalAwsAccounts returns the total number of aws accounts in the aws org
func (s *AccountWatcher) getTotalAwsAccounts() (int, error) {
	var nextToken *string

	accountTotal := 0
	// Ensure we paginate through the account list
	for {
		awsAccountList, err := s.awsClient.ListAccounts(&organizations.ListAccountsInput{NextToken: nextToken})
		if err != nil {
			errMsg := "Error getting a list of accounts"
			if aerr, ok := err.(awserr.Error); ok {
				errMsg = aerr.Message()
			}
			return s.total, errors.New(errMsg)
		}
		accountTotal += len(awsAccountList.Accounts)

		if awsAccountList.NextToken != nil {
			nextToken = awsAccountList.NextToken
		} else {
			break
		}
	}

	return accountTotal, nil
}

// AccountsCanBeCreated returns whether we can create accounts or not
func (s *AccountWatcher) AccountsCanBeCreated() bool {
	return s.accountsCanBeCreated
}

// GetAccountCount returns the number of accounts that are currently recorded.
func (s *AccountWatcher) GetAccountCount() int {
	return s.total
}

// GetLimit returns the soft limit we have set in the configmap
func (s *AccountWatcher) GetLimit() int {
	return s.limit
}

// accountLimitReached returns True if our account limit is reached or False if the account limit is not reached and we can create accounts.
func (s *AccountWatcher) accountLimitReached(log logr.Logger, currentAccounts int) (bool, error) {
	limit, err := s.getAwsAccountLimit()
	if err != nil {
		log.Error(err, "There was an error getting the limits.  Using the default value.")
		return true, err
	}
	return currentAccounts >= limit, err
}

// getAwsAccountLimit gets the limit from the ConfigMap or on error returns a default value.
func (s *AccountWatcher) getAwsAccountLimit() (int, error) {
	configMap := &corev1.ConfigMap{}
	err := s.client.Get(context.TODO(), types.NamespacedName{Namespace: awsv1alpha1.AccountCrNamespace, Name: awsv1alpha1.DefaultConfigMap}, configMap)
	if err != nil {
		return -1, err
	}

	limitStr, ok := configMap.Data["account-limit"]
	if !ok {
		return -1, awsv1alpha1.ErrInvalidConfigMap
	}

	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		return -1, err
	}

	// persist the limit
	s.limit = limit
	return limit, nil
}
