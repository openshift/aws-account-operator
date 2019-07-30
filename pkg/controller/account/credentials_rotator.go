package account

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/credentialwatcher"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// RotateCredentials update existing secret with new STS tokens and Singin URL
func (r *ReconcileAccount) RotateCredentials(reqLogger logr.Logger, awsSetupClient awsclient.Client, account *awsv1alpha1.Account) error {
	STSCredentialsSecretName := account.Name + credentialwatcher.STSCredentialsSuffix
	STSCredentialsSecretNamespace := account.Namespace

	reqLogger.Info(fmt.Sprintf("Rotating credentials for account %s secret %s", account.Name, STSCredentialsSecretName))

	// Get STS user credentials
	STSCredentials, STSCredentialsErr := getStsCredentials(reqLogger, awsSetupClient, "", account.Spec.AwsAccountID)

	if STSCredentialsErr != nil {
		reqLogger.Info("RotateCredentials: Failed to get SRE admin STSCredentials from AWS api ", "Error", STSCredentialsErr.Error())
		return STSCredentialsErr
	}

	// Create new awsClient with SRE IAM credentials so we can generate STS and Federation tokens from it
	SREAWSClient, err := r.getAWSClient(newAwsClientInput{
		secretName: account.Name + "-" + strings.ToLower(iamUserNameSRE) + "-secret",
		nameSpace:  awsv1alpha1.AccountCrNamespace,
		awsRegion:  "us-east-1"})

	if err != nil {
		reqLogger.Error(err, "RotateCredentials: Unable to create AWS conn with IAM user creds")
	}

	STSUserName := account.Name + "-sts"

	IAMAdministratorPolicy := "arn:aws:iam::aws:policy/AdministratorAccess"

	IAMPolicy := sts.PolicyDescriptorType{Arn: &IAMAdministratorPolicy}

	IAMPolicyDescriptors := []*sts.PolicyDescriptorType{&IAMPolicy}

	SigninTokenDuration := int64(credentialwatcher.STSCredentialsDuration)

	// Set IAM policy for Web Console login, this policy cannot grant more permissions than the IAM user has which creates it

	SREConsoleLoginURL, err := RequestSigninToken(reqLogger, SREAWSClient, &SigninTokenDuration, &STSUserName, &IAMAdministratorPolicy, IAMPolicyDescriptors, STSCredentials)
	if err != nil {
		reqLogger.Error(err, "RotateCredentials: Unable to create AWS signin token")
		return err
	}

	STSSecret := &corev1.Secret{}

	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: STSCredentialsSecretName, Namespace: awsv1alpha1.AccountCrNamespace}, STSSecret)

	if err != nil {
		reqLogger.Error(err, "Error retriving secret %s", STSCredentialsSecretName)
		return err
	}

	err = r.Client.Delete(context.TODO(), STSSecret)

	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Error deleting secret %s", STSCredentialsSecretName))
		return err
	}

	STSSecretInput := SRESecretInput{
		SecretName:              account.Name,
		NameSpace:               STSCredentialsSecretNamespace,
		awsCredsSecretIDKey:     *STSCredentials.Credentials.AccessKeyId,
		awsCredsSecretAccessKey: *STSCredentials.Credentials.SecretAccessKey,
		awsCredsSessionToken:    *STSCredentials.Credentials.SessionToken,
		awsCredsConsoleLoginURL: SREConsoleLoginURL,
	}

	STSCredentialsSecret := SRESecretInput.newSTSSecret(STSSecretInput)

	err = r.Client.Create(context.TODO(), STSCredentialsSecret)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Unable to update secret %s", STSSecret.Name))
		return err
	}

	// Set `status.RotateCredentials` to false now that they ahve been updated
	account.Status.RotateCredentials = false

	err = r.Client.Status().Update(context.TODO(), account)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("RotateCredentials: Error updating account %s", account.Name))
		return err
	}

	reqLogger.Info(fmt.Sprintf("AWS STS and signin token rotated for account %s valid for %d", account.Name, credentialwatcher.STSCredentialsDuration-credentialwatcher.STSCredentialsThreshold))

	return nil
}
