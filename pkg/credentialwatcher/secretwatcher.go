package credentialwatcher

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// STSCredentialsSuffix is the suffix applied to account.Name to create STS Secret
	STSCredentialsSuffix = "-sre-cli-credentials"
	// STSCredentialsConsoleSuffix is the suffix applied to account.Name to create STS Secret
	STSCredentialsConsoleSuffix = "-sre-console-url"
	// STSConsoleCredentialsDuration Duration of STS token and Console signin URL
	STSConsoleCredentialsDuration = 900
	// STSCredentialsDuration Duration of STS token and Console signin URL
	STSCredentialsDuration = 3600
	// STSCredentialsThreshold Time before STS credentials are recreated
	STSCredentialsThreshold = 60
)

// SecretWatcher global var for SecretWatcher
var SecretWatcher *secretWatcher

type secretWatcher struct {
	watchInterval time.Duration
	client        client.Client
}

// Initialize creates a global instance of the SecretWatcher
func Initialize(client client.Client, watchInterval time.Duration) {
	SecretWatcher = NewSecretWatcher(client, watchInterval)
}

// NewSecretWatcher returns a new instance of the SecretWatcher interface
func NewSecretWatcher(client client.Client, watchInterval time.Duration) *secretWatcher {
	return &secretWatcher{
		watchInterval: watchInterval,
		client:        client,
	}
}

// SecretWatcher will trigger CredentialsRotator every `scanInternal` and only stop if the operator is killed or a
// message is sent on the stopCh
func (s *secretWatcher) Start(log logr.Logger, stopCh <-chan struct{}) {
	log.Info("Starting the secretWatcher")
	log.Info("Secretwatcher initial run")
	err := s.ScanSecrets(log)
	if err != nil {
		log.Error(err, "secretwatcher initial run failed ")
	}

	for {
		select {
		case <-time.After(s.watchInterval):
			log.Info("secretWatcher: scanning secrets")
			err := s.ScanSecrets(log)
			if err != nil {
				log.Error(err, "secretWatcher not started, credentials wont be rotated")
			}
		case <-stopCh:
			log.Info("Stopping the secretWatcher")
			break
		}
	}
}

// timeSinceCreation takes a creationTimestamp from a kubernetes object and returns the sime in seconds
// since creation
func (s *secretWatcher) timeSinceCreation(creationTimestamp metav1.Time) int {
	unixTime := time.Unix(creationTimestamp.Unix(), 0)
	return int(time.Since(unixTime).Seconds())
}

func (s *secretWatcher) timeToInt(time time.Duration) int {
	return int(time.Seconds())
}

// CredentialsRotator will list all secrets with the `STSCredentialsSuffix` and mark the account CR `status.rotateCredentials` true
// if the credentials CreationTimeStamp is within `STSCredentialsRefreshThreshold` of `STSCredentialsDuration`
func (s *secretWatcher) ScanSecrets(log logr.Logger) error {
	// List STS secrets and check their expiry
	secretList := &corev1.SecretList{}

	listOps := &client.ListOptions{Namespace: awsv1alpha1.AccountCrNamespace}
	if err := s.client.List(context.TODO(), listOps, secretList); err != nil {
		log.Error(err, fmt.Sprintf("Unable to list secrets in namespace %s", awsv1alpha1.AccountCrNamespace))
		return err
	}

	fuzzSeed := time.Now().UnixNano()
	STSCredsFuzzFactor := getCredentialsFuzzLength(fuzzSeed)
	ConsoleFuzzFactor := getConsoleURLFuzzLength(fuzzSeed)

	for _, secret := range secretList.Items {
		if strings.HasSuffix(secret.ObjectMeta.Name, STSCredentialsSuffix) {
			accountName := strings.TrimSuffix(secret.ObjectMeta.Name, STSCredentialsSuffix)
			timeSinceCreation := s.timeSinceCreation(secret.ObjectMeta.CreationTimestamp)

			if STSCredentialsDuration-timeSinceCreation-STSCredsFuzzFactor < s.timeToInt(SecretWatcher.watchInterval) {
				s.updateAccountRotateCredentialsStatus(log, accountName, "cli")
			}
		}

		if strings.HasSuffix(secret.ObjectMeta.Name, STSCredentialsConsoleSuffix) {
			accountName := strings.TrimSuffix(secret.ObjectMeta.Name, STSCredentialsConsoleSuffix)
			timeSinceCreation := s.timeSinceCreation(secret.ObjectMeta.CreationTimestamp)

			if STSConsoleCredentialsDuration-timeSinceCreation-ConsoleFuzzFactor < s.timeToInt(SecretWatcher.watchInterval) {
				s.updateAccountRotateCredentialsStatus(log, accountName, "console")
			}
		}
	}
	return nil
}

// updateAccountRotateCredentialsStatus
func (s *secretWatcher) updateAccountRotateCredentialsStatus(log logr.Logger, accountName, credentialType string) {

	accountInstance, err := s.GetAccount(accountName)
	if err != nil {
		getAccountErrMsg := fmt.Sprintf("Unable to retrieve account CR %s", accountName)
		log.Error(err, getAccountErrMsg)
		return
	}

	// Only rotate STS credentials if the account CR is in a Ready state
	if accountInstance.Status.State != string(awsv1alpha1.AccountReady) {
		return
	}

	if (accountInstance.Status.RotateConsoleCredentials && credentialType == "console") || (accountInstance.Status.RotateCredentials && credentialType == "cli") {
		// We don't want to update the status since its already been updated
		return
	}

	if !accountInstance.Status.RotateConsoleCredentials && credentialType == "console" {
		accountInstance.Status.RotateConsoleCredentials = true
		log.Info(fmt.Sprintf("%s %s credentials secret to be refreshed", accountName, credentialType))
	}

	if !accountInstance.Status.RotateCredentials && credentialType == "cli" {
		accountInstance.Status.RotateCredentials = true
		log.Info(fmt.Sprintf("%s %s credentials secret to be refreshed", accountName, credentialType))
	}

	err = s.UpdateAccount(accountInstance)
	if err != nil {
		log.Error(err, fmt.Sprintf("Unable to update status to trigger account %s to rotate %s credentials", accountName, credentialType))
	}
}

// GetAccount retrieve account CR
func (s *secretWatcher) GetAccount(accountName string) (*awsv1alpha1.Account, error) {
	accountInstance := &awsv1alpha1.Account{}
	accountNamespacedName := types.NamespacedName{Name: accountName, Namespace: awsv1alpha1.AccountCrNamespace}

	err := s.client.Get(context.TODO(), accountNamespacedName, accountInstance)
	if err != nil {
		return nil, err
	}

	return accountInstance, nil
}

// UpdateAccount updates account CR
func (s *secretWatcher) UpdateAccount(account *awsv1alpha1.Account) error {
	err := s.client.Status().Update(context.TODO(), account)
	if err != nil {
		return err
	}

	return nil
}

func getConsoleURLFuzzLength(seed int64) int {
	// The lower limit is the minimum amount of "fuzz" time we want to add, in minutes.
	var requeueLowerLimit int64 = 0
	// The upper limit is the maximum amount of "fuzz" time we want to add, in minutes.
	var requeueUpperLimit int64 = 3

	return GetFuzzLength(seed, requeueLowerLimit, requeueUpperLimit)
}

func getCredentialsFuzzLength(seed int64) int {
	// The lower limit is the minimum amount of "fuzz" time we want to add, in minutes.
	var requeueLowerLimit int64 = 5
	// The upper limit is the maximum amount of "fuzz" time we want to add, in minutes.
	var requeueUpperLimit int64 = 15

	return GetFuzzLength(seed, requeueLowerLimit, requeueUpperLimit)
}

// Gets a random number between the lower limit and upper limit.  Fuzz time is a way to
// randomly distribute secret refresh time.
func GetFuzzLength(seed int64, requeueLowerLimit int64, requeueUpperLimit int64) int {
	rand.Seed(seed)
	requeueLength := rand.Int63n(requeueUpperLimit)

	for requeueLength <= requeueLowerLimit || requeueLength >= requeueUpperLimit {
		requeueLength = rand.Int63n(requeueUpperLimit)
	}

	// Convert to seconds and return an int
	return int(requeueLength * 60)
}
