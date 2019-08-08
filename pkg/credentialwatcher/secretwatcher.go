package credentialwatcher

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// STSCredentialsSuffix is the suffix applied to account.Name to create STS Secret
	STSCredentialsSuffix = "-sre-credentials"
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

	for _, secret := range secretList.Items {

		accountName := strings.TrimSuffix(secret.ObjectMeta.Name, STSCredentialsSuffix)

		if strings.HasSuffix(secret.ObjectMeta.Name, STSCredentialsSuffix) {
			unixTime := time.Unix(secret.ObjectMeta.CreationTimestamp.Unix(), 0)
			timeSinceCreation := int(time.Since(unixTime).Seconds())

			if STSCredentialsDuration-timeSinceCreation < STSCredentialsThreshold {

				accountInstance, err := s.GetAccount(accountName)
				if err != nil {
					getAccountErrMsg := fmt.Sprintf("Unable to retrieve account CR %s", accountName)
					log.Error(err, getAccountErrMsg)
				}

				if accountInstance.Status.RotateCredentials != true {

					log.Info(fmt.Sprintf("AWS credentials secret %s was created %s ago requeing to be refreshed", secret.ObjectMeta.Name, time.Since(unixTime)))

					accountInstance.Status.RotateCredentials = true

					err = s.UpdateAccount(accountInstance)
					if err != nil {
						log.Error(err, fmt.Sprintf("Error updating account %s", accountName))
					}
				}
			}
		}
	}
	return nil
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
