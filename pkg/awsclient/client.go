/*
Copyright 2018 The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package awsclient

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/iam/iamiface"
	"github.com/aws/aws-sdk-go/service/organizations"
	"github.com/aws/aws-sdk-go/service/organizations/organizationsiface"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/aws/aws-sdk-go/service/sts/stsiface"
	"github.com/aws/aws-sdk-go/service/support"
	"github.com/aws/aws-sdk-go/service/support/supportiface"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	kubeclientpkg "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	awsCredsSecretIDKey     = "aws_access_key_id"
	awsCredsSecretAccessKey = "aws_secret_access_key"
)

//go:generate mockgen -source=./client.go -destination=./mock/client_generated.go -package=mock

// Client is a wrapper object for actual AWS SDK clients to allow for easier testing.
type Client interface {
	//EC2
	RunInstances(*ec2.RunInstancesInput) (*ec2.Reservation, error)
	DescribeInstanceStatus(*ec2.DescribeInstanceStatusInput) (*ec2.DescribeInstanceStatusOutput, error)
	TerminateInstances(*ec2.TerminateInstancesInput) (*ec2.TerminateInstancesOutput, error)
	DescribeVolumes(*ec2.DescribeVolumesInput) (*ec2.DescribeVolumesOutput, error)
	DeleteVolume(*ec2.DeleteVolumeInput) (*ec2.DeleteVolumeOutput, error)
	DescribeSnapshots(*ec2.DescribeSnapshotsInput) (*ec2.DescribeSnapshotsOutput, error)
	DeleteSnapshot(*ec2.DeleteSnapshotInput) (*ec2.DeleteSnapshotOutput, error)

	//IAM
	CreateAccessKey(*iam.CreateAccessKeyInput) (*iam.CreateAccessKeyOutput, error)
	CreateUser(*iam.CreateUserInput) (*iam.CreateUserOutput, error)
	DeleteAccessKey(*iam.DeleteAccessKeyInput) (*iam.DeleteAccessKeyOutput, error)
	DeleteUser(*iam.DeleteUserInput) (*iam.DeleteUserOutput, error)
	DeleteUserPolicy(*iam.DeleteUserPolicyInput) (*iam.DeleteUserPolicyOutput, error)
	GetUser(*iam.GetUserInput) (*iam.GetUserOutput, error)
	ListAccessKeys(*iam.ListAccessKeysInput) (*iam.ListAccessKeysOutput, error)
	ListUserPolicies(*iam.ListUserPoliciesInput) (*iam.ListUserPoliciesOutput, error)
	PutUserPolicy(*iam.PutUserPolicyInput) (*iam.PutUserPolicyOutput, error)
	AttachUserPolicy(*iam.AttachUserPolicyInput) (*iam.AttachUserPolicyOutput, error)

	//Organizations
	ListAccounts(*organizations.ListAccountsInput) (*organizations.ListAccountsOutput, error)
	CreateAccount(*organizations.CreateAccountInput) (*organizations.CreateAccountOutput, error)
	DescribeCreateAccountStatus(*organizations.DescribeCreateAccountStatusInput) (*organizations.DescribeCreateAccountStatusOutput, error)

	//sts
	AssumeRole(*sts.AssumeRoleInput) (*sts.AssumeRoleOutput, error)
	GetCallerIdentity(*sts.GetCallerIdentityInput) (*sts.GetCallerIdentityOutput, error)
	GetFederationToken(*sts.GetFederationTokenInput) (*sts.GetFederationTokenOutput, error)

	//Support
	CreateCase(*support.CreateCaseInput) (*support.CreateCaseOutput, error)
	DescribeCases(*support.DescribeCasesInput) (*support.DescribeCasesOutput, error)

	// S3
	ListBuckets(*s3.ListBucketsInput) (*s3.ListBucketsOutput, error)
	DeleteBucket(*s3.DeleteBucketInput) (*s3.DeleteBucketOutput, error)
}

type awsClient struct {
	ec2Client     ec2iface.EC2API
	iamClient     iamiface.IAMAPI
	orgClient     organizationsiface.OrganizationsAPI
	stsClient     stsiface.STSAPI
	supportClient supportiface.SupportAPI
	s3Client      s3iface.S3API
}

// NewAwsClientInput input for new aws client
type NewAwsClientInput struct {
	AwsCredsSecretIDKey     string
	AwsCredsSecretAccessKey string
	AwsToken                string
	AwsRegion               string
	SecretName              string
	NameSpace               string
}

func (c *awsClient) RunInstances(input *ec2.RunInstancesInput) (*ec2.Reservation, error) {
	return c.ec2Client.RunInstances(input)
}

func (c *awsClient) DescribeInstanceStatus(input *ec2.DescribeInstanceStatusInput) (*ec2.DescribeInstanceStatusOutput, error) {
	return c.ec2Client.DescribeInstanceStatus(input)
}

func (c *awsClient) TerminateInstances(input *ec2.TerminateInstancesInput) (*ec2.TerminateInstancesOutput, error) {
	return c.ec2Client.TerminateInstances(input)
}

func (c *awsClient) DescribeVolumes(input *ec2.DescribeVolumesInput) (*ec2.DescribeVolumesOutput, error) {
	return c.ec2Client.DescribeVolumes(input)
}

func (c *awsClient) DeleteVolume(input *ec2.DeleteVolumeInput) (*ec2.DeleteVolumeOutput, error) {
	return c.ec2Client.DeleteVolume(input)
}

func (c *awsClient) DescribeSnapshots(input *ec2.DescribeSnapshotsInput) (*ec2.DescribeSnapshotsOutput, error) {
	return c.ec2Client.DescribeSnapshots(input)
}

func (c *awsClient) DeleteSnapshot(input *ec2.DeleteSnapshotInput) (*ec2.DeleteSnapshotOutput, error) {
	return c.ec2Client.DeleteSnapshot(input)
}

func (c *awsClient) CreateAccessKey(input *iam.CreateAccessKeyInput) (*iam.CreateAccessKeyOutput, error) {
	return c.iamClient.CreateAccessKey(input)
}

func (c *awsClient) CreateUser(input *iam.CreateUserInput) (*iam.CreateUserOutput, error) {
	return c.iamClient.CreateUser(input)
}

func (c *awsClient) DeleteAccessKey(input *iam.DeleteAccessKeyInput) (*iam.DeleteAccessKeyOutput, error) {
	return c.iamClient.DeleteAccessKey(input)
}

func (c *awsClient) DeleteUser(input *iam.DeleteUserInput) (*iam.DeleteUserOutput, error) {
	return c.iamClient.DeleteUser(input)
}

func (c *awsClient) DeleteUserPolicy(input *iam.DeleteUserPolicyInput) (*iam.DeleteUserPolicyOutput, error) {
	return c.iamClient.DeleteUserPolicy(input)
}
func (c *awsClient) GetUser(input *iam.GetUserInput) (*iam.GetUserOutput, error) {
	return c.iamClient.GetUser(input)
}

func (c *awsClient) ListAccessKeys(input *iam.ListAccessKeysInput) (*iam.ListAccessKeysOutput, error) {
	return c.iamClient.ListAccessKeys(input)
}

func (c *awsClient) ListUserPolicies(input *iam.ListUserPoliciesInput) (*iam.ListUserPoliciesOutput, error) {
	return c.iamClient.ListUserPolicies(input)
}

func (c *awsClient) PutUserPolicy(input *iam.PutUserPolicyInput) (*iam.PutUserPolicyOutput, error) {
	return c.iamClient.PutUserPolicy(input)
}

func (c *awsClient) AttachUserPolicy(input *iam.AttachUserPolicyInput) (*iam.AttachUserPolicyOutput, error) {
	return c.iamClient.AttachUserPolicy(input)
}

func (c *awsClient) ListAccounts(input *organizations.ListAccountsInput) (*organizations.ListAccountsOutput, error) {
	return c.orgClient.ListAccounts(input)
}

func (c *awsClient) CreateAccount(input *organizations.CreateAccountInput) (*organizations.CreateAccountOutput, error) {
	return c.orgClient.CreateAccount(input)
}

func (c *awsClient) DescribeCreateAccountStatus(input *organizations.DescribeCreateAccountStatusInput) (*organizations.DescribeCreateAccountStatusOutput, error) {
	return c.orgClient.DescribeCreateAccountStatus(input)
}

func (c *awsClient) AssumeRole(input *sts.AssumeRoleInput) (*sts.AssumeRoleOutput, error) {
	return c.stsClient.AssumeRole(input)
}

func (c *awsClient) CreateCase(input *support.CreateCaseInput) (*support.CreateCaseOutput, error) {
	return c.supportClient.CreateCase(input)
}

func (c *awsClient) DescribeCases(input *support.DescribeCasesInput) (*support.DescribeCasesOutput, error) {
	return c.supportClient.DescribeCases(input)
}

func (c *awsClient) GetCallerIdentity(input *sts.GetCallerIdentityInput) (*sts.GetCallerIdentityOutput, error) {
	return c.stsClient.GetCallerIdentity(input)
}

func (c *awsClient) GetFederationToken(input *sts.GetFederationTokenInput) (*sts.GetFederationTokenOutput, error) {
	return c.stsClient.GetFederationToken(input)
}

func (c *awsClient) ListBuckets(input *s3.ListBucketsInput) (*s3.ListBucketsOutput, error) {
	return c.s3Client.ListBuckets(input)
}

func (c *awsClient) DeleteBucket(input *s3.DeleteBucketInput) (*s3.DeleteBucketOutput, error) {
	return c.s3Client.DeleteBucket(input)
}

// NewClient creates our client wrapper object for the actual AWS clients we use.
func NewClient(kubeClient kubeclientpkg.Client, awsAccessID, awsAccessSecret, token, region string) (Client, error) {
	awsConfig := &aws.Config{Region: aws.String(region)}
	awsConfig.Credentials = credentials.NewStaticCredentials(
		awsAccessID, awsAccessSecret, token)

	s, err := session.NewSession(awsConfig)
	if err != nil {
		return nil, err
	}

	return &awsClient{
		ec2Client:     ec2.New(s),
		iamClient:     iam.New(s),
		orgClient:     organizations.New(s),
		stsClient:     sts.New(s),
		supportClient: support.New(s),
		s3Client:      s3.New(s),
	}, nil
}

// GetAWSClient generates an awsclient
// function must include region
// Pass in token if sessions requires a token
// if it includes a secretName and nameSpace it will create credentials from that secret data
// If it includes awsCredsSecretIDKey and awsCredsSecretAccessKey it will build credentials from those
func GetAWSClient(kubeClient kubeclientpkg.Client, input NewAwsClientInput) (Client, error) {

	// error if region is not included
	if input.AwsRegion == "" {
		return nil, fmt.Errorf("getAWSClient:NoRegion: %v", input.AwsRegion)
	}

	if input.SecretName != "" && input.NameSpace != "" {
		secret := &corev1.Secret{}
		err := kubeClient.Get(context.TODO(),
			types.NamespacedName{
				Name:      input.SecretName,
				Namespace: input.NameSpace,
			},
			secret)
		if err != nil {
			return nil, err
		}
		accessKeyID, ok := secret.Data[awsCredsSecretIDKey]
		if !ok {
			return nil, fmt.Errorf("AWS credentials secret %v did not contain key %v",
				input.SecretName, awsCredsSecretIDKey)
		}
		secretAccessKey, ok := secret.Data[awsCredsSecretAccessKey]
		if !ok {
			return nil, fmt.Errorf("AWS credentials secret %v did not contain key %v",
				input.SecretName, awsCredsSecretAccessKey)
		}

		awsClient, err := NewClient(kubeClient, string(accessKeyID), string(secretAccessKey), input.AwsToken, input.AwsRegion)
		if err != nil {
			return nil, err
		}
		return awsClient, nil
	}

	if input.AwsCredsSecretIDKey == "" && input.AwsCredsSecretAccessKey != "" {
		return nil, fmt.Errorf("getAWSClient: NoAwsCredentials or Secret %v", input)
	}

	awsClient, err := NewClient(kubeClient, input.AwsCredsSecretIDKey, input.AwsCredsSecretAccessKey, input.AwsToken, input.AwsRegion)
	if err != nil {
		return nil, err
	}
	return awsClient, nil
}
