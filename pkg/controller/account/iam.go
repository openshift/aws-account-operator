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
	corev1 "k8s.io/api/core/v1"
)

// Type for JSON response from Federation end point
type awsSigninTokenResponse struct {
	SigninToken string
}

// RequestSigninToken makes a HTTP request to retrieve a Signin Token from the federation end point
func RequestSigninToken(reqLogger logr.Logger, awsclient awsclient.Client, DurationSeconds *int64, FederatedUserName *string, PolicyArns []*sts.PolicyDescriptorType, STSCredentials *sts.AssumeRoleOutput) (string, error) {
	// URL for building Federated Signin queries
	federationEndpointURL := "https://signin.aws.amazon.com/federation"

	// Get Federated token credentials to build console URL
	GetFederationTokenOutput, err := getFederationToken(reqLogger, awsclient, DurationSeconds, FederatedUserName, PolicyArns)

	if err != nil {
		return "", err
	}

	signinTokenResponse, err := getSigninToken(reqLogger, federationEndpointURL, GetFederationTokenOutput)

	if err != nil {
		return "", err
	}

	signedFederationURL, err := formatSigninURL(reqLogger, federationEndpointURL, signinTokenResponse.SigninToken)

	if err != nil {
		return "", err
	}

	// Return Signin Token
	return signedFederationURL.String(), nil

}

func getFederationToken(reqLogger logr.Logger, awsclient awsclient.Client, DurationSeconds *int64, FederatedUserName *string, PolicyArns []*sts.PolicyDescriptorType) (*sts.GetFederationTokenOutput, error) {

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
			return GetFederationTokenOutput, err
		}

		return GetFederationTokenOutput, err
	}

	if GetFederationTokenOutput == nil {

		reqLogger.Error(ErrFederationTokenOutputNil, fmt.Sprintf("Federation Token Output: %+v", GetFederationTokenOutput))
		return GetFederationTokenOutput, ErrFederationTokenOutputNil

	}

	return GetFederationTokenOutput, nil

}

// formatSigninURL build and format the signin URL to be used in the secret
func formatSigninURL(reqLogger logr.Logger, federationEndpointURL, signinToken string) (*url.URL, error) {
	// URLs for building Federated Signin queries
	awsConsoleURL := "https://console.aws.amazon.com/"
	issuer := "Red Hat SRE"

	signinFederationURL, err := url.Parse(federationEndpointURL)

	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Malformed URL: %s", err.Error()))
		return signinFederationURL, err
	}

	signinParams := url.Values{}

	signinParams.Add("Action", "login")
	signinParams.Add("Destination", awsConsoleURL)
	signinParams.Add("Issuer", issuer)
	signinParams.Add("SigninToken", signinToken)

	signinFederationURL.RawQuery = signinParams.Encode()

	return signinFederationURL, nil

}

// CreateSecret creates a secret
func (r *ReconcileAccount) CreateSecret(reqLogger logr.Logger, secretName string, account *awsv1alpha1.Account, secret *corev1.Secret) error {

	createErr := r.Client.Create(context.TODO(), secret)
	if createErr != nil {
		failedToCreateUserSecretMsg := fmt.Sprintf("Failed to create secret for STS user %s", secretName)
		SetAccountStatus(reqLogger, account, failedToCreateUserSecretMsg, awsv1alpha1.AccountFailed, "Failed")
		err := r.Client.Status().Update(context.TODO(), account)
		if err != nil {
			return err
		}
		reqLogger.Info(failedToCreateUserSecretMsg)
		return createErr
	}
	reqLogger.Info("Created IAM STS User")
	return nil
}

// BuildSTSUser takes all parameters required to create a user, user secret
func (r *ReconcileAccount) BuildSTSUser(reqLogger logr.Logger, awsSetupClient awsclient.Client, awsClient awsclient.Client, account *awsv1alpha1.Account, nameSpace string, iamRole string) (string, error) {
	reqLogger.Info("Creating IAM STS User")

	// If IAM user was just created we cannot immediately create STS credentials due to an issue
	// with eventual consisency on AWS' side
	time.Sleep(10 * time.Second)

	// Create STS user for SRE admins
	STSCredentials, STSCredentialsErr := getStsCredentials(reqLogger, awsClient, iamRole, account.Spec.AwsAccountID)
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

	STSConsoleInput := SREConsoleInput{
		SecretName:              fmt.Sprintf("%s-sre-console-url", secretName),
		NameSpace:               nameSpace,
		awsCredsConsoleLoginURL: SREConsoleLoginURL,
	}

	userConsoleSecret := STSConsoleInput.newConsoleSecret()

	err = r.CreateSecret(reqLogger, secretName, account, userConsoleSecret)
	if err != nil {
		return "", err
	}

	STSSecretInput := SRESecretInput{
		SecretName:              fmt.Sprintf("%s-sre-cli-credentials", secretName),
		NameSpace:               nameSpace,
		awsCredsSecretIDKey:     *STSCredentials.Credentials.AccessKeyId,
		awsCredsSecretAccessKey: *STSCredentials.Credentials.SecretAccessKey,
		awsCredsSessionToken:    *STSCredentials.Credentials.SessionToken,
	}
	userSecret := STSSecretInput.newSTSSecret()

	err = r.CreateSecret(reqLogger, secretName, account, userSecret)
	if err != nil {
		return "", err
	}

	return userSecret.ObjectMeta.Name, nil
}

// getStsCredentials returns sts credentials for the specified account ARN
func getStsCredentials(reqLogger logr.Logger, client awsclient.Client, iamRoleName string, awsAccountID string) (*sts.AssumeRoleOutput, error) {
	// Use the role session name to uniquely identify a session when the same role
	// is assumed by different principals or for different reasons.
	var roleSessionName = "awsAccountOperator"
	// Default duration in seconds of the session token 3600. We need to have the roles policy
	// changed if we want it to be longer than 3600 seconds
	var roleSessionDuration int64 = 3600
	// The role ARN made up of the account number and the role which is the default role name
	// created in child accounts
	var roleArn = fmt.Sprintf("arn:aws:iam::%s:role/%s", awsAccountID, iamRoleName)
	reqLogger.Info(fmt.Sprintf("Creating STS credentials for AWS ARN: %s", roleArn))
	// Build input for AssumeRole
	assumeRoleInput := sts.AssumeRoleInput{
		DurationSeconds: &roleSessionDuration,
		RoleArn:         &roleArn,
		RoleSessionName: &roleSessionName,
	}

	assumeRoleOutput := &sts.AssumeRoleOutput{}
	var err error
	for i := 0; i < 100; i++ {
		time.Sleep(500 * time.Millisecond)
		assumeRoleOutput, err = client.AssumeRole(&assumeRoleInput)
		if err == nil {
			break
		}
	}
	if err != nil {
		// Log AWS error
		if aerr, ok := err.(awserr.Error); ok {
			reqLogger.Error(aerr,
				fmt.Sprintf(`New AWS Error while getting STS credentials, 
					AWS Error Code: %s, 
					AWS Error Message: %s`,
					aerr.Code(),
					aerr.Message()))
		}
		return &sts.AssumeRoleOutput{}, err
	}

	return assumeRoleOutput, nil
}

// formatFederatedCredentails returns a JSON byte array containing federation credentials
func formatFederatedCredentials(reqLogger logr.Logger, federatedTokenCredentials *sts.GetFederationTokenOutput) ([]byte, error) {
	var jsonCredentials []byte

	// Build JSON credentials for federation requets
	federationCredentials := map[string]string{
		"sessionId":    *federatedTokenCredentials.Credentials.AccessKeyId,
		"sessionKey":   *federatedTokenCredentials.Credentials.SecretAccessKey,
		"sessionToken": *federatedTokenCredentials.Credentials.SessionToken,
	}

	jsonCredentials, err := json.Marshal(federationCredentials)

	if err != nil {
		reqLogger.Error(err, "Error serializing federated URL as JSON")
		return jsonCredentials, err
	}

	return jsonCredentials, nil

}

// formatSiginTokenURL take STS credentials and build a URL for signing
func formatSigninTokenURL(reqLogger logr.Logger, federationEndpointURL string, jsonFederatedCredentials []byte) (*url.URL, error) {
	// Build URL to request Signin Token via Federation end point
	baseFederationURL, err := url.Parse(federationEndpointURL)

	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Malformed URL: %s", err.Error()))
		return baseFederationURL, err
	}

	federationParams := url.Values{}

	federationParams.Add("Action", "getSigninToken")
	federationParams.Add("SessionType", "json")
	federationParams.Add("Session", string(jsonFederatedCredentials))

	baseFederationURL.RawQuery = federationParams.Encode()

	return baseFederationURL, nil

}

// requestSignedURL makes a HTTP call to the baseFederationURL to retrieve a signed federated URL for web console login
func requestSignedURL(reqLogger logr.Logger, baseFederationURL string) ([]byte, error) {
	// Make HTTP request to retrieve Federated Signin Token
	res, err := http.Get(baseFederationURL)

	if err != nil {
		getErrMsg := fmt.Sprintf("Error requesting Signin Token from: %s\n", baseFederationURL)
		reqLogger.Error(err, getErrMsg)
		return nil, err
	}

	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)

	if err != nil {
		bodyReadErrMsg := fmt.Sprintf("Unable to read response body: %s", baseFederationURL)
		reqLogger.Error(err, bodyReadErrMsg)
		return body, err
	}

	return body, nil
}

// getSigninToken makes a request to the federation endpoint to sign signin token
func getSigninToken(reqLogger logr.Logger, federationEndpointURL string, federatedTokenCredentials *sts.GetFederationTokenOutput) (awsSigninTokenResponse, error) {
	var signinResponse awsSigninTokenResponse

	jsonFederatedCredentials, err := formatFederatedCredentials(reqLogger, federatedTokenCredentials)

	if err != nil {
		return signinResponse, err
	}

	baseFederationURL, err := formatSigninTokenURL(reqLogger, federationEndpointURL, jsonFederatedCredentials)

	if err != nil {
		return signinResponse, err
	}

	signedFederatedURL, err := requestSignedURL(reqLogger, baseFederationURL.String())

	if err != nil {
		return signinResponse, err
	}

	// Unmarshal JSON response so we can extract the signin token
	err = json.Unmarshal(signedFederatedURL, &signinResponse)

	if err != nil {
		reqLogger.Error(err, "Error unmarshalling Federated Signin Response JSON")
		return signinResponse, err
	}

	return signinResponse, nil

}
