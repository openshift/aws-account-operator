package v1alpha1

import (
	"errors"
)

// CoveredRegions map
var CoveredRegions = map[string]map[string]string{
	"us-east-1": {
		"initializationAMI": "ami-000db10762d0c4c05",
	},
	"us-east-2": {
		"initializationAMI": "ami-094720ddca649952f",
	},
	"us-west-1": {
		"initializationAMI": "ami-04642fc8fca1e8e67",
	},
	"us-west-2": {
		"initializationAMI": "ami-0a7e1ebfee7a4570e",
	},
	"ca-central-1": {
		"initializationAMI": "ami-06ca3c0058d0275b3",
	},
	"eu-central-1": {
		"initializationAMI": "ami-09de4a4c670389e4b",
	},
	"eu-west-1": {
		"initializationAMI": "ami-0202869bdd0fc8c75",
	},
	"eu-west-2": {
		"initializationAMI": "ami-0188c0c5eddd2d032",
	},
	"eu-west-3": {
		"initializationAMI": "ami-0c4224e392ec4e440",
	},
	"ap-northeast-1": {
		"initializationAMI": "ami-00b95502a4d51a07e",
	},
	"ap-northeast-2": {
		"initializationAMI": "ami-041b16ca28f036753",
	},
	"ap-south-1": {
		"initializationAMI": "ami-0963937a03c01ecd4",
	},
	"ap-southeast-1": {
		"initializationAMI": "ami-055c55112e25b1f1f",
	},
	"ap-southeast-2": {
		"initializationAMI": "ami-036b423b657376f5b",
	},
	"sa-east-1": {
		"initializationAMI": "ami-05c1c16cac05a7c0b",
	},
}

// Custom errors

// ErrAwsAccountLimitExceeded indicates the orgnization account limit has been reached.
var ErrAwsAccountLimitExceeded = errors.New("AccountLimitExceeded")

// ErrAccountWatcherNoTotal indicates the TotalAccountWatcher has not run successfully yet.
var ErrAccountWatcherNoTotal = errors.New("AccountWatcherHasNoTotal")

// ErrAwsInternalFailure indicates that there was an internal failure on the aws api
var ErrAwsInternalFailure = errors.New("InternalFailure")

// ErrAwsFailedCreateAccount indicates that an account creation failed
var ErrAwsFailedCreateAccount = errors.New("FailedCreateAccount")

// ErrAwsTooManyRequests indicates that to many requests were sent in a short period
var ErrAwsTooManyRequests = errors.New("TooManyRequestsException")

// ErrAwsCaseCreationLimitExceeded indicates that the support case limit for the account has been reached
var ErrAwsCaseCreationLimitExceeded = errors.New("SupportCaseLimitExceeded")

// ErrAwsFailedCreateSupportCase indicates that a support case creation failed
var ErrAwsFailedCreateSupportCase = errors.New("FailedCreateSupportCase")

// ErrAwsSupportCaseIDNotFound indicates that the support case ID was not found
var ErrAwsSupportCaseIDNotFound = errors.New("SupportCaseIdNotfound")

// ErrAwsFailedDescribeSupportCase indicates that the support case describe failed
var ErrAwsFailedDescribeSupportCase = errors.New("FailedDescribeSupportCase")

// ErrFederationTokenOutputNil indicates that getting a federation token from AWS failed
var ErrFederationTokenOutputNil = errors.New("FederationTokenOutputNil")

// ErrCreateEC2Instance indicates that the CreateEC2Instance function timed out
var ErrCreateEC2Instance = errors.New("EC2CreationTimeout")

// ErrFailedAWSTypecast indicates that there was a failure while typecasting to aws error
var ErrFailedAWSTypecast = errors.New("FailedToTypecastAWSError")

// ErrMissingDefaultConfigMap indicates that the expected default confimap was not found
var ErrMissingDefaultConfigMap = errors.New("MissingDefaultConfigMap")

// ErrInvalidConfigMap indicates that the ConfigMap has invalid fields
var ErrInvalidConfigMap = errors.New("ConfigMapInvalid")

// ErrNonexistentOU indicates that an OU does not exist
var ErrNonexistentOU = errors.New("OUWithNameNotFound")

// ErrAccAlreadyInOU indicates that an account is already in an OU
var ErrAccAlreadyInOU = errors.New("ErrAccAlreadyInOU")

// ErrAccMoveRaceCondition indicates a race condition while moving the account
var ErrAccMoveRaceCondition = errors.New("ErrAccMoveRaceCondition")

// ErrChildNotFound indicates that a child was not found inside an OU
var ErrChildNotFound = errors.New("ChildNotFoundInOU")

// ErrUnexpectedValue indicates that a given variable has an unespected nil value
var ErrUnexpectedValue = errors.New("UnexpectedValue")

// ErrInvalidToken indiacates an invalid token
var ErrInvalidToken = errors.New("InvalidClientTokenId")

// ErrAccessDenied indicates an AWS error from an API call
var ErrAccessDenied = errors.New("AuthorizationError")

// Shared variables

// UIDLabel is the string for the uid label on AWS Federated Account Access CRs
var UIDLabel = "uid"

// AccountIDLabel is the string for the AWS Account ID label on AWS Federated Account Access CRs
var AccountIDLabel = "awsAccountID"

// ClusterAccountNameTagKey is the AWS key name for cluster account name
var ClusterAccountNameTagKey = "clusterAccountName"

// ClusterNamespaceTagKey is the AWS key name for cluster namespace
var ClusterNamespaceTagKey = "clusterNamespace"

// ClusterClaimLinkTagKey is the AWS key name for cluster claim
var ClusterClaimLinkTagKey = "clusterClaimLink"

// ClusterClaimLinkNamespaceTagKey is the AWS key name for cluster claim namespace
var ClusterClaimLinkNamespaceTagKey = "clusterClaimLinkNamespace"

// IAMUserIDLabel label key for IAM user suffix
var IAMUserIDLabel = "iamUserId"

// EmailID is the ID used for prefixing Account CR names
var EmailID = "osd-creds-mgmt"

// InstanceResourceType is the resource type used when building Instance tags
var InstanceResourceType = "instance"

// DefaultConfigMap holds the expected name for the operator's ConfigMap
var DefaultConfigMap = "aws-account-operator-configmap"

// DefaultConfigMapAccountLimit holds the fallback limit of aws-accounts
var DefaultConfigMapAccountLimit = 100
