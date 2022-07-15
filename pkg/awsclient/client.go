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
	"time"

	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/service/route53/route53iface"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/aws/aws-sdk-go/service/servicequotas"
	"github.com/aws/aws-sdk-go/service/servicequotas/servicequotasiface"
	"github.com/openshift/aws-account-operator/pkg/localmetrics"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/iam/iamiface"
	"github.com/aws/aws-sdk-go/service/organizations"
	"github.com/aws/aws-sdk-go/service/organizations/organizationsiface"
	"github.com/aws/aws-sdk-go/service/route53"
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
	awsCredsSecretIDKey     = "aws_access_key_id"     // #nosec G101 -- This is a false positive
	awsCredsSecretAccessKey = "aws_secret_access_key" // #nosec G101 -- This is a false positive
)

//go:generate mockgen -source=./client.go -destination=./mock/zz_generated.mock_client.go -package=mock

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
	DescribeInstances(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error)
	DescribeRegions(input *ec2.DescribeRegionsInput) (*ec2.DescribeRegionsOutput, error)
	DescribeVpcEndpointServiceConfigurations(input *ec2.DescribeVpcEndpointServiceConfigurationsInput) (*ec2.DescribeVpcEndpointServiceConfigurationsOutput, error)
	DeleteVpcEndpointServiceConfigurations(*ec2.DeleteVpcEndpointServiceConfigurationsInput) (*ec2.DeleteVpcEndpointServiceConfigurationsOutput, error)
	DescribeVpcs(*ec2.DescribeVpcsInput) (*ec2.DescribeVpcsOutput, error)
	CreateVpc(*ec2.CreateVpcInput) (*ec2.CreateVpcOutput, error)
	DeleteVpc(*ec2.DeleteVpcInput) (*ec2.DeleteVpcOutput, error)
	DescribeSubnets(*ec2.DescribeSubnetsInput) (*ec2.DescribeSubnetsOutput, error)
	CreateSubnet(*ec2.CreateSubnetInput) (*ec2.CreateSubnetOutput, error)
	DeleteSubnet(*ec2.DeleteSubnetInput) (*ec2.DeleteSubnetOutput, error)

	//IAM
	CreateAccessKey(*iam.CreateAccessKeyInput) (*iam.CreateAccessKeyOutput, error)
	CreateUser(*iam.CreateUserInput) (*iam.CreateUserOutput, error)
	DeleteAccessKey(*iam.DeleteAccessKeyInput) (*iam.DeleteAccessKeyOutput, error)
	DeleteUser(*iam.DeleteUserInput) (*iam.DeleteUserOutput, error)
	DeleteUserPolicy(*iam.DeleteUserPolicyInput) (*iam.DeleteUserPolicyOutput, error)
	GetUser(*iam.GetUserInput) (*iam.GetUserOutput, error)
	ListUsers(*iam.ListUsersInput) (*iam.ListUsersOutput, error)
	ListUsersPages(*iam.ListUsersInput, func(*iam.ListUsersOutput, bool) bool) error
	ListUserTags(*iam.ListUserTagsInput) (*iam.ListUserTagsOutput, error)
	ListAccessKeys(*iam.ListAccessKeysInput) (*iam.ListAccessKeysOutput, error)
	ListUserPolicies(*iam.ListUserPoliciesInput) (*iam.ListUserPoliciesOutput, error)
	PutUserPolicy(*iam.PutUserPolicyInput) (*iam.PutUserPolicyOutput, error)
	AttachUserPolicy(*iam.AttachUserPolicyInput) (*iam.AttachUserPolicyOutput, error)
	DetachUserPolicy(*iam.DetachUserPolicyInput) (*iam.DetachUserPolicyOutput, error)
	ListPolicies(*iam.ListPoliciesInput) (*iam.ListPoliciesOutput, error)
	ListAttachedUserPolicies(*iam.ListAttachedUserPoliciesInput) (*iam.ListAttachedUserPoliciesOutput, error)
	CreatePolicy(*iam.CreatePolicyInput) (*iam.CreatePolicyOutput, error)
	DeletePolicy(input *iam.DeletePolicyInput) (*iam.DeletePolicyOutput, error)
	AttachRolePolicy(*iam.AttachRolePolicyInput) (*iam.AttachRolePolicyOutput, error)
	DetachRolePolicy(*iam.DetachRolePolicyInput) (*iam.DetachRolePolicyOutput, error)
	ListAttachedRolePolicies(*iam.ListAttachedRolePoliciesInput) (*iam.ListAttachedRolePoliciesOutput, error)
	CreateRole(*iam.CreateRoleInput) (*iam.CreateRoleOutput, error)
	GetRole(*iam.GetRoleInput) (*iam.GetRoleOutput, error)
	DeleteRole(*iam.DeleteRoleInput) (*iam.DeleteRoleOutput, error)
	ListRoles(input *iam.ListRolesInput) (*iam.ListRolesOutput, error)

	//Organizations
	ListAccounts(*organizations.ListAccountsInput) (*organizations.ListAccountsOutput, error)
	CreateAccount(*organizations.CreateAccountInput) (*organizations.CreateAccountOutput, error)
	DescribeCreateAccountStatus(*organizations.DescribeCreateAccountStatusInput) (*organizations.DescribeCreateAccountStatusOutput, error)
	MoveAccount(*organizations.MoveAccountInput) (*organizations.MoveAccountOutput, error)
	CreateOrganizationalUnit(*organizations.CreateOrganizationalUnitInput) (*organizations.CreateOrganizationalUnitOutput, error)
	ListOrganizationalUnitsForParent(*organizations.ListOrganizationalUnitsForParentInput) (*organizations.ListOrganizationalUnitsForParentOutput, error)
	ListChildren(*organizations.ListChildrenInput) (*organizations.ListChildrenOutput, error)
	TagResource(*organizations.TagResourceInput) (*organizations.TagResourceOutput, error)
	ListParents(*organizations.ListParentsInput) (*organizations.ListParentsOutput, error)
	ListTagsForResource(input *organizations.ListTagsForResourceInput) (*organizations.ListTagsForResourceOutput, error)

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
	BatchDeleteBucketObjects(bucketName *string) error
	ListObjectsV2(*s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error)

	// Route53
	ListHostedZones(*route53.ListHostedZonesInput) (*route53.ListHostedZonesOutput, error)
	DeleteHostedZone(*route53.DeleteHostedZoneInput) (*route53.DeleteHostedZoneOutput, error)
	ListResourceRecordSets(*route53.ListResourceRecordSetsInput) (*route53.ListResourceRecordSetsOutput, error)
	ChangeResourceRecordSets(*route53.ChangeResourceRecordSetsInput) (*route53.ChangeResourceRecordSetsOutput, error)

	// Service Quota
	GetServiceQuota(*servicequotas.GetServiceQuotaInput) (*servicequotas.GetServiceQuotaOutput, error)
	RequestServiceQuotaIncrease(*servicequotas.RequestServiceQuotaIncreaseInput) (*servicequotas.RequestServiceQuotaIncreaseOutput, error)
	ListRequestedServiceQuotaChangeHistory(*servicequotas.ListRequestedServiceQuotaChangeHistoryInput) (*servicequotas.ListRequestedServiceQuotaChangeHistoryOutput, error)
	ListRequestedServiceQuotaChangeHistoryByQuota(*servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaInput) (*servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaOutput, error)
}

type awsClient struct {
	ec2Client           ec2iface.EC2API
	iamClient           iamiface.IAMAPI
	orgClient           organizationsiface.OrganizationsAPI
	stsClient           stsiface.STSAPI
	supportClient       supportiface.SupportAPI
	s3Client            s3iface.S3API
	route53client       route53iface.Route53API
	serviceQuotasClient servicequotasiface.ServiceQuotasAPI
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

func (c *awsClient) DescribeVpcEndpointServiceConfigurations(input *ec2.DescribeVpcEndpointServiceConfigurationsInput) (*ec2.DescribeVpcEndpointServiceConfigurationsOutput, error) {
	return c.ec2Client.DescribeVpcEndpointServiceConfigurations(input)
}

func (c *awsClient) DeleteVpcEndpointServiceConfigurations(input *ec2.DeleteVpcEndpointServiceConfigurationsInput) (*ec2.DeleteVpcEndpointServiceConfigurationsOutput, error) {
	return c.ec2Client.DeleteVpcEndpointServiceConfigurations(input)
}

func (c *awsClient) DescribeSnapshots(input *ec2.DescribeSnapshotsInput) (*ec2.DescribeSnapshotsOutput, error) {
	return c.ec2Client.DescribeSnapshots(input)
}

func (c *awsClient) DeleteSnapshot(input *ec2.DeleteSnapshotInput) (*ec2.DeleteSnapshotOutput, error) {
	return c.ec2Client.DeleteSnapshot(input)
}

func (c *awsClient) DescribeInstances(input *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
	return c.ec2Client.DescribeInstances(input)
}

func (c *awsClient) DescribeRegions(input *ec2.DescribeRegionsInput) (*ec2.DescribeRegionsOutput, error) {
	return c.ec2Client.DescribeRegions(input)
}

func (c *awsClient) DescribeVpcs(input *ec2.DescribeVpcsInput) (*ec2.DescribeVpcsOutput, error) {
	return c.ec2Client.DescribeVpcs(input)
}

func (c *awsClient) CreateVpc(input *ec2.CreateVpcInput) (*ec2.CreateVpcOutput, error) {
	return c.ec2Client.CreateVpc(input)
}

func (c *awsClient) DeleteVpc(input *ec2.DeleteVpcInput) (*ec2.DeleteVpcOutput, error) {
	return c.ec2Client.DeleteVpc(input)
}

func (c *awsClient) DescribeSubnets(input *ec2.DescribeSubnetsInput) (*ec2.DescribeSubnetsOutput, error) {
	return c.ec2Client.DescribeSubnets(input)
}

func (c *awsClient) CreateSubnet(input *ec2.CreateSubnetInput) (*ec2.CreateSubnetOutput, error) {
	return c.ec2Client.CreateSubnet(input)
}

func (c *awsClient) DeleteSubnet(input *ec2.DeleteSubnetInput) (*ec2.DeleteSubnetOutput, error) {
	return c.ec2Client.DeleteSubnet(input)
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

func (c *awsClient) ListUsers(input *iam.ListUsersInput) (*iam.ListUsersOutput, error) {
	return c.iamClient.ListUsers(input)
}

func (c *awsClient) ListUsersPages(input *iam.ListUsersInput, fn func(*iam.ListUsersOutput, bool) bool) error {
	return c.iamClient.ListUsersPages(input, fn)
}

func (c *awsClient) ListUserTags(input *iam.ListUserTagsInput) (*iam.ListUserTagsOutput, error) {
	return c.iamClient.ListUserTags(input)
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

func (c *awsClient) DetachUserPolicy(input *iam.DetachUserPolicyInput) (*iam.DetachUserPolicyOutput, error) {
	return c.iamClient.DetachUserPolicy(input)
}

func (c *awsClient) ListPolicies(input *iam.ListPoliciesInput) (*iam.ListPoliciesOutput, error) {
	return c.iamClient.ListPolicies(input)
}

func (c *awsClient) ListAttachedUserPolicies(input *iam.ListAttachedUserPoliciesInput) (*iam.ListAttachedUserPoliciesOutput, error) {
	return c.iamClient.ListAttachedUserPolicies(input)
}

func (c *awsClient) CreatePolicy(input *iam.CreatePolicyInput) (*iam.CreatePolicyOutput, error) {
	return c.iamClient.CreatePolicy(input)
}

func (c *awsClient) DeletePolicy(input *iam.DeletePolicyInput) (*iam.DeletePolicyOutput, error) {
	return c.iamClient.DeletePolicy(input)
}

func (c *awsClient) AttachRolePolicy(input *iam.AttachRolePolicyInput) (*iam.AttachRolePolicyOutput, error) {
	return c.iamClient.AttachRolePolicy(input)
}

func (c *awsClient) DetachRolePolicy(input *iam.DetachRolePolicyInput) (*iam.DetachRolePolicyOutput, error) {
	return c.iamClient.DetachRolePolicy(input)
}

func (c *awsClient) ListAttachedRolePolicies(input *iam.ListAttachedRolePoliciesInput) (*iam.ListAttachedRolePoliciesOutput, error) {
	return c.iamClient.ListAttachedRolePolicies(input)
}

func (c *awsClient) CreateRole(input *iam.CreateRoleInput) (*iam.CreateRoleOutput, error) {
	return c.iamClient.CreateRole(input)
}

func (c *awsClient) GetRole(input *iam.GetRoleInput) (*iam.GetRoleOutput, error) {
	return c.iamClient.GetRole(input)
}

func (c *awsClient) DeleteRole(input *iam.DeleteRoleInput) (*iam.DeleteRoleOutput, error) {
	return c.iamClient.DeleteRole(input)
}

func (c *awsClient) ListRoles(input *iam.ListRolesInput) (*iam.ListRolesOutput, error) {
	return c.iamClient.ListRoles(input)
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

func (c *awsClient) MoveAccount(input *organizations.MoveAccountInput) (*organizations.MoveAccountOutput, error) {
	return c.orgClient.MoveAccount(input)
}

func (c *awsClient) CreateOrganizationalUnit(input *organizations.CreateOrganizationalUnitInput) (*organizations.CreateOrganizationalUnitOutput, error) {
	return c.orgClient.CreateOrganizationalUnit(input)
}

func (c *awsClient) ListOrganizationalUnitsForParent(input *organizations.ListOrganizationalUnitsForParentInput) (*organizations.ListOrganizationalUnitsForParentOutput, error) {
	return c.orgClient.ListOrganizationalUnitsForParent(input)
}

func (c *awsClient) ListChildren(input *organizations.ListChildrenInput) (*organizations.ListChildrenOutput, error) {
	return c.orgClient.ListChildren(input)
}

func (c *awsClient) TagResource(input *organizations.TagResourceInput) (*organizations.TagResourceOutput, error) {
	return c.orgClient.TagResource(input)
}

func (c *awsClient) ListParents(input *organizations.ListParentsInput) (*organizations.ListParentsOutput, error) {
	return c.orgClient.ListParents(input)
}

func (c *awsClient) ListTagsForResource(input *organizations.ListTagsForResourceInput) (*organizations.ListTagsForResourceOutput, error) {
	return c.orgClient.ListTagsForResource(input)
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
	GetFederationTokenOutput, err := c.stsClient.GetFederationToken(input)
	if GetFederationTokenOutput != nil {
		return GetFederationTokenOutput, err
	}
	return &sts.GetFederationTokenOutput{}, err
}

func (c *awsClient) ListBuckets(input *s3.ListBucketsInput) (*s3.ListBucketsOutput, error) {
	return c.s3Client.ListBuckets(input)
}

func (c *awsClient) DeleteBucket(input *s3.DeleteBucketInput) (*s3.DeleteBucketOutput, error) {
	return c.s3Client.DeleteBucket(input)
}

func (c *awsClient) ListObjectsV2(input *s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error) {
	return c.s3Client.ListObjectsV2(input)
}

func (c *awsClient) BatchDeleteBucketObjects(bucketName *string) error {
	// Setup BatchDeleteItrerator to iterate through a list of objects
	iter := s3manager.NewDeleteListIterator(c.s3Client, &s3.ListObjectsInput{
		Bucket: bucketName,
	})

	// Traverse iterator deleting each object
	return s3manager.NewBatchDeleteWithClient(c.s3Client).Delete(aws.BackgroundContext(), iter)
}

func (c *awsClient) ListHostedZones(input *route53.ListHostedZonesInput) (*route53.ListHostedZonesOutput, error) {
	return c.route53client.ListHostedZones(input)
}

func (c *awsClient) DeleteHostedZone(input *route53.DeleteHostedZoneInput) (*route53.DeleteHostedZoneOutput, error) {
	return c.route53client.DeleteHostedZone(input)
}

func (c *awsClient) ListResourceRecordSets(input *route53.ListResourceRecordSetsInput) (*route53.ListResourceRecordSetsOutput, error) {
	return c.route53client.ListResourceRecordSets(input)
}

func (c *awsClient) ChangeResourceRecordSets(input *route53.ChangeResourceRecordSetsInput) (*route53.ChangeResourceRecordSetsOutput, error) {
	return c.route53client.ChangeResourceRecordSets(input)
}

func (c *awsClient) GetServiceQuota(input *servicequotas.GetServiceQuotaInput) (*servicequotas.GetServiceQuotaOutput, error) {
	return c.serviceQuotasClient.GetServiceQuota(input)
}

func (c *awsClient) RequestServiceQuotaIncrease(input *servicequotas.RequestServiceQuotaIncreaseInput) (*servicequotas.RequestServiceQuotaIncreaseOutput, error) {
	return c.serviceQuotasClient.RequestServiceQuotaIncrease(input)
}

func (c *awsClient) ListRequestedServiceQuotaChangeHistory(input *servicequotas.ListRequestedServiceQuotaChangeHistoryInput) (*servicequotas.ListRequestedServiceQuotaChangeHistoryOutput, error) {
	return c.serviceQuotasClient.ListRequestedServiceQuotaChangeHistory(input)
}

func (c *awsClient) ListRequestedServiceQuotaChangeHistoryByQuota(input *servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaInput) (*servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaOutput, error) {
	return c.serviceQuotasClient.ListRequestedServiceQuotaChangeHistoryByQuota(input)
}

// NewClient creates our client wrapper object for the actual AWS clients we use.
// If controllerName is nonempty, metrics are collected timing and counting each AWS request.
func newClient(controllerName, awsAccessID, awsAccessSecret, token, region string) (Client, error) {
	var err error
	// Set region and retryer to prevent any potential rate limiting on the aws side
	awsConfig := &aws.Config{
		Region:      aws.String(region),
		Credentials: credentials.NewStaticCredentials(awsAccessID, awsAccessSecret, token),
		Retryer: client.DefaultRetryer{
			NumMaxRetries:    10,
			MinThrottleDelay: 2 * time.Second,
		},
	}

	s, err := session.NewSession(awsConfig)
	if err != nil {
		return nil, err
	}

	// Use a regional endpoint for ec2 calls in order to reach opt-in regions when necessary
	resolver := func(service, region string, optFns ...func(*endpoints.Options)) (endpoints.ResolvedEndpoint, error) {
		return endpoints.ResolvedEndpoint{
			PartitionID:   "aws",
			URL:           fmt.Sprintf("https://ec2.%s.amazonaws.com", region),
			SigningRegion: region,
		}, nil
	}
	ec2AwsConfig := &aws.Config{
		Region:           aws.String(region),
		Credentials:      credentials.NewStaticCredentials(awsAccessID, awsAccessSecret, token),
		EndpointResolver: endpoints.ResolverFunc(resolver),
		Retryer: client.DefaultRetryer{
			NumMaxRetries:    10,
			MinThrottleDelay: 2 * time.Second,
		},
	}
	ec2Sess, err := session.NewSession(ec2AwsConfig)
	if err != nil {
		return nil, err
	}

	// Time (and count) calls to AWS.
	// But only from controllers, signaled by a nonempty controllerName.
	if controllerName != "" {
		// The AWS SDK sets Request.Time to Now() when the request is initialized.
		// We time the whole call, from that point until as late as possible, by adding a handler
		// at the end of the `Complete` phase, which is the last available phase of the request.
		s.Handlers.Complete.PushBack(func(r *request.Request) {
			localmetrics.Collector.AddAPICall(controllerName, r.HTTPRequest, r.HTTPResponse, time.Since(r.Time).Seconds(), r.Error)
		})
		ec2Sess.Handlers.Complete.PushBack(func(r *request.Request) {
			localmetrics.Collector.AddAPICall(controllerName, r.HTTPRequest, r.HTTPResponse, time.Since(r.Time).Seconds(), r.Error)
		})
	}

	return &awsClient{
		iamClient:           iam.New(s),
		ec2Client:           ec2.New(ec2Sess),
		orgClient:           organizations.New(s),
		route53client:       route53.New(s),
		s3Client:            s3.New(s),
		stsClient:           sts.New(s),
		supportClient:       support.New(s),
		serviceQuotasClient: servicequotas.New(s, aws.NewConfig()),
	}, nil
}

// IBuilder implementations know how to produce a Client.
type IBuilder interface {
	GetClient(controllerName string, kubeClient kubeclientpkg.Client, input NewAwsClientInput) (Client, error)
}

// Builder is an IBuilder implementation that knows how to produce a real AWS Client (i.e. one
// that really talks to the AWS APIs).
type Builder struct{}

// GetClient generates a real awsclient
// function must include region
// Pass in token if sessions requires a token
// if it includes a secretName and nameSpace it will create credentials from that secret data
// If it includes awsCredsSecretIDKey and awsCredsSecretAccessKey it will build credentials from those
func (rp *Builder) GetClient(controllerName string, kubeClient kubeclientpkg.Client, input NewAwsClientInput) (Client, error) {

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

		awsClient, err := newClient(controllerName, string(accessKeyID), string(secretAccessKey), input.AwsToken, input.AwsRegion)
		if err != nil {
			return nil, err
		}
		return awsClient, nil
	}

	if input.AwsCredsSecretIDKey == "" && input.AwsCredsSecretAccessKey != "" {
		return nil, fmt.Errorf("getAWSClient: NoAwsCredentials or Secret %v", input)
	}

	awsClient, err := newClient(controllerName, input.AwsCredsSecretIDKey, input.AwsCredsSecretAccessKey, input.AwsToken, input.AwsRegion)
	if err != nil {
		return nil, err
	}
	return awsClient, nil
}
