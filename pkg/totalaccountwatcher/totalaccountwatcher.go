package totalaccountwatcher

import (
	"errors"
	"fmt"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/organizations"
	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	controllerutils "github.com/openshift/aws-account-operator/pkg/controller/utils"
	"github.com/openshift/aws-account-operator/pkg/localmetrics"
)

// ErrAwsAccountLimitExceeded indicates the organization account limit has been reached.
var ErrAwsAccountLimitExceeded = errors.New("AccountLimitExceeded")

// TotalAccountWatcher global var for TotalAccountWatcher
var TotalAccountWatcher *totalAccountWatcher

var log = logf.Log.WithName("aws-account-operator")

type totalAccountWatcher struct {
	watchInterval time.Duration
	AwsClient     awsclient.Client
	client        client.Client
	Total         int
}

// Initialize creates a global instance of the TotalAccountWatcher
func Initialize(client client.Client, watchInterval time.Duration) {
	log.Info("Initializing the totalAccountWatcher")

	// NOTE(efried): This is a snowflake use of awsclient.IBuilder. Everyone else puts the
	// IBuilder in their struct and uses it to GetClient() dynamically as needed. This one grabs a
	// single client one time and stores it in a global.
	builder := &awsclient.Builder{}
	AwsClient, err := builder.GetClient("", client, awsclient.NewAwsClientInput{
		SecretName: controllerutils.AwsSecretName,
		NameSpace:  awsv1alpha1.AccountCrNamespace,
		AwsRegion:  "us-east-1",
	})

	if err != nil {
		log.Error(err, "Failed to get AwsClient")
		return
	}

	TotalAccountWatcher = NewTotalAccountWatcher(client, AwsClient, watchInterval)
	TotalAccountWatcher.UpdateTotalAccounts(log)
}

// NewTotalAccountWatcher returns a new instance of the TotalAccountWatcher interface
func NewTotalAccountWatcher(
	client client.Client,
	AwsClient awsclient.Client,
	watchInterval time.Duration,
) *totalAccountWatcher {
	return &totalAccountWatcher{
		watchInterval: watchInterval,
		AwsClient:     AwsClient,
		client:        client,
	}
}

// TotalAccountWatcher will trigger AwsLimitUpdate every `scanInternal` and only stop if the operator is killed or a
// message is sent on the stopCh
func (s *totalAccountWatcher) Start(log logr.Logger, stopCh <-chan struct{}) {
	log.Info("Starting the totalAccountWatcher")
	for {
		select {
		case <-time.After(s.watchInterval):
			err := s.UpdateTotalAccounts(log)
			if err != nil {
				log.Error(err, "totalAccountWatcher not started, awsLimit won't be updated")
			}
		case <-stopCh:
			log.Info("Stopping the totalAccountWatcher")
			break
		}
	}
}

// UpdateTotalAccounts will update the TotalAccountWatcher's total field
func (s *totalAccountWatcher) UpdateTotalAccounts(log logr.Logger) error {

	accountTotal, err := TotalAwsAccounts()
	if err != nil {
		log.Error(err, "Failed to get account list with error code")
		return nil
	}
	localmetrics.Collector.SetTotalAWSAccounts(accountTotal)

	if accountTotal != TotalAccountWatcher.Total {
		log.Info(fmt.Sprintf("Updating total from %d to %d", TotalAccountWatcher.Total, accountTotal))
		TotalAccountWatcher.Total = accountTotal
	}

	return nil
}

// TotalAwsAccounts returns the total number of aws accounts in the aws org
func TotalAwsAccounts() (int, error) {
	var nextToken *string

	accountTotal := 0
	// Ensure we paginate through the account list
	for {
		awsAccountList, err := TotalAccountWatcher.AwsClient.ListAccounts(&organizations.ListAccountsInput{NextToken: nextToken})
		if err != nil {
			errMsg := "Error getting a list of accounts"
			if aerr, ok := err.(awserr.Error); ok {
				errMsg = aerr.Message()
			}
			return TotalAccountWatcher.Total, errors.New(errMsg)
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
