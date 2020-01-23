package credentialwatcher

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
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
	// List accounts and check if they are ready
	accountList := &awsv1alpha1.AccountList{}

	listOps := &client.ListOptions{Namespace: awsv1alpha1.AccountCrNamespace}
	if err := s.client.List(context.TODO(), listOps, accountList); err != nil {
		log.Error(err, fmt.Sprintf("Unable to list accounts in namespace %s", awsv1alpha1.AccountCrNamespace))
		return err
	}
	for _, account := range accountList.Items {
		if account.Status.State == "Ready" {
			// Checks if the STSCredentials secret is expired
			expiredCLICreds, err := s.checkSecretExpired(fmt.Sprintf("%s%s", account.GetName(), STSCredentialsSuffix), STSCredentialsDuration)
			if err != nil {
				log.Error(err, fmt.Sprintf("unable to get secret %s in namespace %s", fmt.Sprintf("%s%s", account.GetName(), STSCredentialsSuffix), awsv1alpha1.AccountCrNamespace))
			}

			// Checks if the STSCredentialsConsole secret is expired
			expiredConsoleCreds, err := s.checkSecretExpired(fmt.Sprintf("%s%s", account.GetName(), STSCredentialsConsoleSuffix), STSConsoleCredentialsDuration)
			if err != nil {
				log.Error(err, fmt.Sprintf("unable to get secret %s in namespace %s", fmt.Sprintf("%s%s", account.GetName(), STSCredentialsConsoleSuffix), awsv1alpha1.AccountCrNamespace))
			}

			// if either secret need rotation update the account status
			if expiredCLICreds || expiredConsoleCreds {
				account.Status.RotateCredentials = expiredCLICreds
				account.Status.RotateConsoleCredentials = expiredConsoleCreds
				err := s.client.Status().Update(context.TODO(), &account)
				if err != nil {
					// If we continue to get the object has been modified errors here we may need to get a new account object.
					log.Error(err, fmt.Sprintf("SecretWatcher: Error updating account %s", account.GetName()))
				}
			}
		}
	}

	return nil
}

// Check credentials will check that the supplied secretName in the awsv1alpha1.AccountCrNamespace is not expired
// It checks that the creations time of the secret does not exeed the ExpectedDuration
func (s *secretWatcher) checkSecretExpired(secretName string, ExpectedDuration int) (bool, error) {

	secret := &corev1.Secret{}
	err := s.client.Get(context.TODO(), types.NamespacedName{
		Name:      secretName,
		Namespace: awsv1alpha1.AccountCrNamespace,
	}, secret)
	if err != nil {
		// if the error is not found return true so that they can be created
		if k8serr.IsNotFound(err) {
			return true, nil
		}
		// if there are other errors return it back up so that the error is logged
		return false, err
	}

	timeSinceCreation := s.timeSinceCreation(secret.ObjectMeta.CreationTimestamp)
	if ExpectedDuration-timeSinceCreation < s.timeToInt(SecretWatcher.watchInterval) {
		// if the credentials are older then the expected duration return true
		return true, nil
	}

	// returns false as default so no changes are made
	return false, nil
}
