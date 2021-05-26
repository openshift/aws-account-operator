package v1alpha1

import (
	"errors"
)

type AmiSpec struct {
	Ami 			string
	InstanceType 	string
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

// VolumeResourceType is the resource type used when building Volume tags
var VolumeResourceType = "volume"

// DefaultConfigMap holds the expected name for the operator's ConfigMap
var DefaultConfigMap = "aws-account-operator-configmap"

// DefaultConfigMapAccountLimit holds the fallback limit of aws-accounts
var DefaultConfigMapAccountLimit = 100

// AwsUSEastOneRegion holds the key for the aws east one region
var AwsUSEastOneRegion = "us-east-1"

// ManagedTagsConfigMapKey defines the default key for the configmap to add the defined tags to AWS resources
var ManagedTagsConfigMapKey = "aws-managed-tags"
