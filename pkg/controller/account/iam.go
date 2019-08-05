package account

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/credentialwatcher"
)

// Type for JSON response from Federation end point
type awsSigninTokenResponse struct {
	SigninToken string
}

// RequestSigninToken makes a HTTP request to retrieve a Signin Token from the federation end point
func RequestSigninToken(reqLogger logr.Logger, awsclient awsclient.Client, DurationSeconds *int64, FederatedUserName *string, PolicyArns []*sts.PolicyDescriptorType, STSCredentials *sts.AssumeRoleOutput) (string, error) {

	// // URLs for building Federated Signin queries
	federationEndPointURL := "https://signin.aws.amazon.com/federation"
	awsConsoleURL := "https://console.aws.amazon.com/"
	issuer := "Red Hat SRE"

	GetFederationTokenInput := sts.GetFederationTokenInput{
		DurationSeconds: DurationSeconds,
		Name:            FederatedUserName,
		PolicyArns:      PolicyArns,
	}

	// Get Federated token credentials to build console URL
	GetFederationTokenOutput, err := awsclient.GetFederationToken(&GetFederationTokenInput)

	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok {
			// Get error details
			reqLogger.Error(err, fmt.Sprintf("Error: %s, %s", awsErr.Code(), awsErr.Message()))
			return "", err
		}

		return "", err
	}

	if GetFederationTokenOutput == nil {

		reqLogger.Error(ErrFederationTokenOutputNil, fmt.Sprintf("Federation Token Output: %+v", GetFederationTokenOutput))
		return "", ErrFederationTokenOutputNil

	}

	// Build JSON credentials for federation requets
	federationCredentials := map[string]string{
		"sessionId":    *GetFederationTokenOutput.Credentials.AccessKeyId,
		"sessionKey":   *GetFederationTokenOutput.Credentials.SecretAccessKey,
		"sessionToken": *GetFederationTokenOutput.Credentials.SessionToken,
	}

	jsonCredentials, err := json.Marshal(federationCredentials)

	// TODO better error here
	if err != nil {
		reqLogger.Error(err, "Error serializing json")
		return "", err
	}

	// Build URL to request Signin Token via Federation end point
	baseFederationURL, err := url.Parse(federationEndPointURL)

	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Malformed URL: %s", err.Error()))
		return "", err
	}

	federationParams := url.Values{}

	federationParams.Add("Action", "getSigninToken")
	federationParams.Add("SessionType", "json")
	federationParams.Add("Session", string(jsonCredentials))

	baseFederationURL.RawQuery = federationParams.Encode()

	// Make HTTP request to retrieve Federated Signin Token
	res, err := http.Get(baseFederationURL.String())

	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Error requesting Signin Token from: %s\n", baseFederationURL.String()))
		return "", err
	}

	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)

	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Unable to read response body: %s", baseFederationURL.String()))
		return "", err
	}

	var SigninResponse awsSigninTokenResponse

	// Unmarshal JSON response so we can extract the signin token
	err = json.Unmarshal(body, &SigninResponse)

	if err != nil {
		reqLogger.Error(err, "Error unmarshalling Federated Signin Response JSON")
		return "", err
	}

	signinFederationURL, err := url.Parse(federationEndPointURL)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Malformed URL: %s", err.Error()))
		return "", err
	}

	signinParams := url.Values{}

	signinParams.Add("Action", "login")
	signinParams.Add("Destination", awsConsoleURL)
	signinParams.Add("Issuer", issuer)
	signinParams.Add("SigninToken", SigninResponse.SigninToken)

	signinFederationURL.RawQuery = signinParams.Encode()

	// Return Signin Token
	return signinFederationURL.String(), nil

}

// BuildSTSUser takes all parameters required to create a user, user secret
func (r *ReconcileAccount) BuildSTSUser(reqLogger logr.Logger, awsSetupClient awsclient.Client, awsClient awsclient.Client, account *awsv1alpha1.Account, nameSpace string) (string, error) {
	reqLogger.Info("Creating IAM STS User")

	// If IAM user was just created we cannot immediately create STS credentials due to an issue
	// with eventual consisency on AWS' side
	time.Sleep(10 * time.Second)

	// Create STS user for SRE admins
	STSCredentials, STSCredentialsErr := getStsCredentials(reqLogger, awsClient, "", account.Spec.AwsAccountID)
	if STSCredentialsErr != nil {
		reqLogger.Info("Failed to get SRE admin STSCredentials from AWS api ", "Error", STSCredentialsErr.Error())
		return "", STSCredentialsErr
	}

	STSUserName := account.Name + "-STS"

	IAMAdministratorPolicy := "arn:aws:iam::aws:policy/AdministratorAccess"

	IAMPolicy := sts.PolicyDescriptorType{Arn: &IAMAdministratorPolicy}

	IAMPolicyDescriptors := []*sts.PolicyDescriptorType{&IAMPolicy}

	SigninTokenDuration := int64(credentialwatcher.STSCredentialsDuration)

	// Set IAM policy for Web Console login, this policy cannot grant more permissions than the IAM user has which creates it

	SREConsoleLoginURL, err := RequestSigninToken(reqLogger, awsSetupClient, &SigninTokenDuration, &STSUserName, IAMPolicyDescriptors, STSCredentials)
	if err != nil {
		reqLogger.Error(err, "Unable to create AWS signin token")
	}

	secretName := account.Name

	STSSecretInput := SRESecretInput{
		SecretName:              secretName,
		NameSpace:               nameSpace,
		awsCredsSecretIDKey:     *STSCredentials.Credentials.AccessKeyId,
		awsCredsSecretAccessKey: *STSCredentials.Credentials.SecretAccessKey,
		awsCredsSessionToken:    *STSCredentials.Credentials.SessionToken,
		awsCredsConsoleLoginURL: SREConsoleLoginURL,
	}
	userSecret := STSSecretInput.newSTSSecret()
	createErr := r.Client.Create(context.TODO(), userSecret)
	if createErr != nil {
		failedToCreateUserSecretMsg := fmt.Sprintf("Failed to create secret for STS user %s", secretName)
		SetAccountStatus(reqLogger, account, failedToCreateUserSecretMsg, awsv1alpha1.AccountFailed, "Failed")
		err := r.Client.Status().Update(context.TODO(), account)
		if err != nil {
			return "", err
		}
		reqLogger.Info(failedToCreateUserSecretMsg)
		return "", createErr
	}
	reqLogger.Info("Created IAM STS User")
	return userSecret.ObjectMeta.Name, nil
}
