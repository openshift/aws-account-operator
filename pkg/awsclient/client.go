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
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/account"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/servicequotas"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/aws-sdk-go-v2/service/support"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/openshift/aws-account-operator/pkg/localmetrics"
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
	//Account
	EnableRegion(context.Context, *account.EnableRegionInput) (*account.EnableRegionOutput, error)
	GetRegionOptStatus(context.Context, *account.GetRegionOptStatusInput) (*account.GetRegionOptStatusOutput, error)

	//EC2
	RunInstances(context.Context, *ec2.RunInstancesInput) (*ec2.RunInstancesOutput, error)
	DescribeInstanceStatus(context.Context, *ec2.DescribeInstanceStatusInput) (*ec2.DescribeInstanceStatusOutput, error)
	TerminateInstances(context.Context, *ec2.TerminateInstancesInput) (*ec2.TerminateInstancesOutput, error)
	DescribeVolumes(context.Context, *ec2.DescribeVolumesInput) (*ec2.DescribeVolumesOutput, error)
	DeleteVolume(context.Context, *ec2.DeleteVolumeInput) (*ec2.DeleteVolumeOutput, error)
	DescribeSnapshots(context.Context, *ec2.DescribeSnapshotsInput) (*ec2.DescribeSnapshotsOutput, error)
	DeleteSnapshot(context.Context, *ec2.DeleteSnapshotInput) (*ec2.DeleteSnapshotOutput, error)
	DescribeImages(context.Context, *ec2.DescribeImagesInput) (*ec2.DescribeImagesOutput, error)
	DescribeInstances(context.Context, *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error)
	DescribeInstanceTypes(context.Context, *ec2.DescribeInstanceTypesInput) (*ec2.DescribeInstanceTypesOutput, error)
	DescribeRegions(context.Context, *ec2.DescribeRegionsInput) (*ec2.DescribeRegionsOutput, error)
	DescribeVpcEndpointServiceConfigurations(context.Context, *ec2.DescribeVpcEndpointServiceConfigurationsInput) (*ec2.DescribeVpcEndpointServiceConfigurationsOutput, error)
	DeleteVpcEndpointServiceConfigurations(context.Context, *ec2.DeleteVpcEndpointServiceConfigurationsInput) (*ec2.DeleteVpcEndpointServiceConfigurationsOutput, error)
	DescribeVpcs(context.Context, *ec2.DescribeVpcsInput) (*ec2.DescribeVpcsOutput, error)
	CreateVpc(context.Context, *ec2.CreateVpcInput) (*ec2.CreateVpcOutput, error)
	DeleteVpc(context.Context, *ec2.DeleteVpcInput) (*ec2.DeleteVpcOutput, error)
	DescribeSubnets(context.Context, *ec2.DescribeSubnetsInput) (*ec2.DescribeSubnetsOutput, error)
	CreateSubnet(context.Context, *ec2.CreateSubnetInput) (*ec2.CreateSubnetOutput, error)
	DeleteSubnet(context.Context, *ec2.DeleteSubnetInput) (*ec2.DeleteSubnetOutput, error)

	//IAM
	CreateAccessKey(context.Context, *iam.CreateAccessKeyInput) (*iam.CreateAccessKeyOutput, error)
	CreateUser(context.Context, *iam.CreateUserInput) (*iam.CreateUserOutput, error)
	DeleteAccessKey(context.Context, *iam.DeleteAccessKeyInput) (*iam.DeleteAccessKeyOutput, error)
	DeleteUser(context.Context, *iam.DeleteUserInput) (*iam.DeleteUserOutput, error)
	DeleteUserPolicy(context.Context, *iam.DeleteUserPolicyInput) (*iam.DeleteUserPolicyOutput, error)
	GetUser(context.Context, *iam.GetUserInput) (*iam.GetUserOutput, error)
	ListUsers(context.Context, *iam.ListUsersInput) (*iam.ListUsersOutput, error)
	ListUsersPages(context.Context, *iam.ListUsersInput, func(*iam.ListUsersOutput, bool) bool) error
	ListUserTags(context.Context, *iam.ListUserTagsInput) (*iam.ListUserTagsOutput, error)
	ListAccessKeys(context.Context, *iam.ListAccessKeysInput) (*iam.ListAccessKeysOutput, error)
	ListUserPolicies(context.Context, *iam.ListUserPoliciesInput) (*iam.ListUserPoliciesOutput, error)
	PutUserPolicy(context.Context, *iam.PutUserPolicyInput) (*iam.PutUserPolicyOutput, error)
	AttachUserPolicy(context.Context, *iam.AttachUserPolicyInput) (*iam.AttachUserPolicyOutput, error)
	DetachUserPolicy(context.Context, *iam.DetachUserPolicyInput) (*iam.DetachUserPolicyOutput, error)
	ListPolicies(context.Context, *iam.ListPoliciesInput) (*iam.ListPoliciesOutput, error)
	ListAttachedUserPolicies(context.Context, *iam.ListAttachedUserPoliciesInput) (*iam.ListAttachedUserPoliciesOutput, error)
	CreatePolicy(context.Context, *iam.CreatePolicyInput) (*iam.CreatePolicyOutput, error)
	DeletePolicy(context.Context, *iam.DeletePolicyInput) (*iam.DeletePolicyOutput, error)
	DeletePolicyVersion(context.Context, *iam.DeletePolicyVersionInput) (*iam.DeletePolicyVersionOutput, error)
	GetPolicy(context.Context, *iam.GetPolicyInput) (*iam.GetPolicyOutput, error)
	GetPolicyVersion(context.Context, *iam.GetPolicyVersionInput) (*iam.GetPolicyVersionOutput, error)
	ListPolicyVersions(context.Context, *iam.ListPolicyVersionsInput) (*iam.ListPolicyVersionsOutput, error)
	AttachRolePolicy(context.Context, *iam.AttachRolePolicyInput) (*iam.AttachRolePolicyOutput, error)
	DetachRolePolicy(context.Context, *iam.DetachRolePolicyInput) (*iam.DetachRolePolicyOutput, error)
	ListAttachedRolePolicies(context.Context, *iam.ListAttachedRolePoliciesInput) (*iam.ListAttachedRolePoliciesOutput, error)
	ListRolePolicies(context.Context, *iam.ListRolePoliciesInput) (*iam.ListRolePoliciesOutput, error)
	DeleteRolePolicy(context.Context, *iam.DeleteRolePolicyInput) (*iam.DeleteRolePolicyOutput, error)
	CreateRole(context.Context, *iam.CreateRoleInput) (*iam.CreateRoleOutput, error)
	GetRole(context.Context, *iam.GetRoleInput) (*iam.GetRoleOutput, error)
	DeleteRole(context.Context, *iam.DeleteRoleInput) (*iam.DeleteRoleOutput, error)
	ListRoles(context.Context, *iam.ListRolesInput) (*iam.ListRolesOutput, error)
	PutRolePolicy(context.Context, *iam.PutRolePolicyInput) (*iam.PutRolePolicyOutput, error)

	//Organizations
	ListAccounts(context.Context, *organizations.ListAccountsInput) (*organizations.ListAccountsOutput, error)
	CreateAccount(context.Context, *organizations.CreateAccountInput) (*organizations.CreateAccountOutput, error)
	DescribeCreateAccountStatus(context.Context, *organizations.DescribeCreateAccountStatusInput) (*organizations.DescribeCreateAccountStatusOutput, error)
	ListCreateAccountStatus(context.Context, *organizations.ListCreateAccountStatusInput) (*organizations.ListCreateAccountStatusOutput, error)
	MoveAccount(context.Context, *organizations.MoveAccountInput) (*organizations.MoveAccountOutput, error)
	CreateOrganizationalUnit(context.Context, *organizations.CreateOrganizationalUnitInput) (*organizations.CreateOrganizationalUnitOutput, error)
	ListOrganizationalUnitsForParent(context.Context, *organizations.ListOrganizationalUnitsForParentInput) (*organizations.ListOrganizationalUnitsForParentOutput, error)
	ListChildren(context.Context, *organizations.ListChildrenInput) (*organizations.ListChildrenOutput, error)
	TagResource(context.Context, *organizations.TagResourceInput) (*organizations.TagResourceOutput, error)
	UntagResource(context.Context, *organizations.UntagResourceInput) (*organizations.UntagResourceOutput, error)
	ListParents(context.Context, *organizations.ListParentsInput) (*organizations.ListParentsOutput, error)
	ListTagsForResource(context.Context, *organizations.ListTagsForResourceInput) (*organizations.ListTagsForResourceOutput, error)

	//sts
	AssumeRole(context.Context, *sts.AssumeRoleInput) (*sts.AssumeRoleOutput, error)
	GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput) (*sts.GetCallerIdentityOutput, error)
	GetFederationToken(context.Context, *sts.GetFederationTokenInput) (*sts.GetFederationTokenOutput, error)

	//Support
	CreateCase(context.Context, *support.CreateCaseInput) (*support.CreateCaseOutput, error)
	DescribeCases(context.Context, *support.DescribeCasesInput) (*support.DescribeCasesOutput, error)

	// S3
	ListBuckets(context.Context, *s3.ListBucketsInput) (*s3.ListBucketsOutput, error)
	DeleteBucket(context.Context, *s3.DeleteBucketInput) (*s3.DeleteBucketOutput, error)
	BatchDeleteBucketObjects(context.Context, *string) error
	ListObjectsV2(context.Context, *s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error)

	// Route53
	ListHostedZones(context.Context, *route53.ListHostedZonesInput) (*route53.ListHostedZonesOutput, error)
	DeleteHostedZone(context.Context, *route53.DeleteHostedZoneInput) (*route53.DeleteHostedZoneOutput, error)
	ListResourceRecordSets(context.Context, *route53.ListResourceRecordSetsInput) (*route53.ListResourceRecordSetsOutput, error)
	ChangeResourceRecordSets(context.Context, *route53.ChangeResourceRecordSetsInput) (*route53.ChangeResourceRecordSetsOutput, error)

	// Service Quota
	GetServiceQuota(context.Context, *servicequotas.GetServiceQuotaInput) (*servicequotas.GetServiceQuotaOutput, error)
	RequestServiceQuotaIncrease(context.Context, *servicequotas.RequestServiceQuotaIncreaseInput) (*servicequotas.RequestServiceQuotaIncreaseOutput, error)
	ListRequestedServiceQuotaChangeHistory(context.Context, *servicequotas.ListRequestedServiceQuotaChangeHistoryInput) (*servicequotas.ListRequestedServiceQuotaChangeHistoryOutput, error)
	ListRequestedServiceQuotaChangeHistoryByQuota(context.Context, *servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaInput) (*servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaOutput, error)
}

type awsClient struct {
	acctClient          *account.Client
	ec2Client           *ec2.Client
	iamClient           *iam.Client
	orgClient           *organizations.Client
	stsClient           *sts.Client
	supportClient       *support.Client
	s3Client            *s3.Client
	route53client       *route53.Client
	serviceQuotasClient *servicequotas.Client
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

func (c *awsClient) EnableRegion(ctx context.Context, input *account.EnableRegionInput) (*account.EnableRegionOutput, error) {
	return c.acctClient.EnableRegion(ctx, input)
}

func (c *awsClient) GetRegionOptStatus(ctx context.Context, input *account.GetRegionOptStatusInput) (*account.GetRegionOptStatusOutput, error) {
	return c.acctClient.GetRegionOptStatus(ctx, input)
}

func (c *awsClient) RunInstances(ctx context.Context, input *ec2.RunInstancesInput) (*ec2.RunInstancesOutput, error) {
	return c.ec2Client.RunInstances(ctx, input)
}

func (c *awsClient) DescribeImages(ctx context.Context, input *ec2.DescribeImagesInput) (*ec2.DescribeImagesOutput, error) {
	return c.ec2Client.DescribeImages(ctx, input)
}

func (c *awsClient) DescribeInstanceStatus(ctx context.Context, input *ec2.DescribeInstanceStatusInput) (*ec2.DescribeInstanceStatusOutput, error) {
	return c.ec2Client.DescribeInstanceStatus(ctx, input)
}

func (c *awsClient) TerminateInstances(ctx context.Context, input *ec2.TerminateInstancesInput) (*ec2.TerminateInstancesOutput, error) {
	return c.ec2Client.TerminateInstances(ctx, input)
}

func (c *awsClient) DescribeVolumes(ctx context.Context, input *ec2.DescribeVolumesInput) (*ec2.DescribeVolumesOutput, error) {
	return c.ec2Client.DescribeVolumes(ctx, input)
}

func (c *awsClient) DeleteVolume(ctx context.Context, input *ec2.DeleteVolumeInput) (*ec2.DeleteVolumeOutput, error) {
	return c.ec2Client.DeleteVolume(ctx, input)
}

func (c *awsClient) DescribeVpcEndpointServiceConfigurations(ctx context.Context, input *ec2.DescribeVpcEndpointServiceConfigurationsInput) (*ec2.DescribeVpcEndpointServiceConfigurationsOutput, error) {
	return c.ec2Client.DescribeVpcEndpointServiceConfigurations(ctx, input)
}

func (c *awsClient) DeleteVpcEndpointServiceConfigurations(ctx context.Context, input *ec2.DeleteVpcEndpointServiceConfigurationsInput) (*ec2.DeleteVpcEndpointServiceConfigurationsOutput, error) {
	return c.ec2Client.DeleteVpcEndpointServiceConfigurations(ctx, input)
}

func (c *awsClient) DescribeSnapshots(ctx context.Context, input *ec2.DescribeSnapshotsInput) (*ec2.DescribeSnapshotsOutput, error) {
	return c.ec2Client.DescribeSnapshots(ctx, input)
}

func (c *awsClient) DeleteSnapshot(ctx context.Context, input *ec2.DeleteSnapshotInput) (*ec2.DeleteSnapshotOutput, error) {
	return c.ec2Client.DeleteSnapshot(ctx, input)
}

func (c *awsClient) DescribeInstances(ctx context.Context, input *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
	return c.ec2Client.DescribeInstances(ctx, input)
}

func (c *awsClient) DescribeInstanceTypes(ctx context.Context, input *ec2.DescribeInstanceTypesInput) (*ec2.DescribeInstanceTypesOutput, error) {
	return c.ec2Client.DescribeInstanceTypes(ctx, input)
}

func (c *awsClient) DescribeRegions(ctx context.Context, input *ec2.DescribeRegionsInput) (*ec2.DescribeRegionsOutput, error) {
	return c.ec2Client.DescribeRegions(ctx, input)
}

func (c *awsClient) DescribeVpcs(ctx context.Context, input *ec2.DescribeVpcsInput) (*ec2.DescribeVpcsOutput, error) {
	return c.ec2Client.DescribeVpcs(ctx, input)
}

func (c *awsClient) CreateVpc(ctx context.Context, input *ec2.CreateVpcInput) (*ec2.CreateVpcOutput, error) {
	return c.ec2Client.CreateVpc(ctx, input)
}

func (c *awsClient) DeleteVpc(ctx context.Context, input *ec2.DeleteVpcInput) (*ec2.DeleteVpcOutput, error) {
	return c.ec2Client.DeleteVpc(ctx, input)
}

func (c *awsClient) DescribeSubnets(ctx context.Context, input *ec2.DescribeSubnetsInput) (*ec2.DescribeSubnetsOutput, error) {
	return c.ec2Client.DescribeSubnets(ctx, input)
}

func (c *awsClient) CreateSubnet(ctx context.Context, input *ec2.CreateSubnetInput) (*ec2.CreateSubnetOutput, error) {
	return c.ec2Client.CreateSubnet(ctx, input)
}

func (c *awsClient) DeleteSubnet(ctx context.Context, input *ec2.DeleteSubnetInput) (*ec2.DeleteSubnetOutput, error) {
	return c.ec2Client.DeleteSubnet(ctx, input)
}

func (c *awsClient) CreateAccessKey(ctx context.Context, input *iam.CreateAccessKeyInput) (*iam.CreateAccessKeyOutput, error) {
	return c.iamClient.CreateAccessKey(ctx, input)
}

func (c *awsClient) CreateUser(ctx context.Context, input *iam.CreateUserInput) (*iam.CreateUserOutput, error) {
	return c.iamClient.CreateUser(ctx, input)
}

func (c *awsClient) DeleteAccessKey(ctx context.Context, input *iam.DeleteAccessKeyInput) (*iam.DeleteAccessKeyOutput, error) {
	return c.iamClient.DeleteAccessKey(ctx, input)
}

func (c *awsClient) DeleteUser(ctx context.Context, input *iam.DeleteUserInput) (*iam.DeleteUserOutput, error) {
	return c.iamClient.DeleteUser(ctx, input)
}

func (c *awsClient) DeleteUserPolicy(ctx context.Context, input *iam.DeleteUserPolicyInput) (*iam.DeleteUserPolicyOutput, error) {
	return c.iamClient.DeleteUserPolicy(ctx, input)
}

func (c *awsClient) GetUser(ctx context.Context, input *iam.GetUserInput) (*iam.GetUserOutput, error) {
	return c.iamClient.GetUser(ctx, input)
}

func (c *awsClient) ListUsers(ctx context.Context, input *iam.ListUsersInput) (*iam.ListUsersOutput, error) {
	return c.iamClient.ListUsers(ctx, input)
}

func (c *awsClient) ListUsersPages(ctx context.Context, input *iam.ListUsersInput, fn func(*iam.ListUsersOutput, bool) bool) error {
	paginator := iam.NewListUsersPaginator(c.iamClient, input)
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return err
		}
		if !fn(output, !paginator.HasMorePages()) {
			break
		}
	}
	return nil
}

func (c *awsClient) ListUserTags(ctx context.Context, input *iam.ListUserTagsInput) (*iam.ListUserTagsOutput, error) {
	return c.iamClient.ListUserTags(ctx, input)
}

func (c *awsClient) ListAccessKeys(ctx context.Context, input *iam.ListAccessKeysInput) (*iam.ListAccessKeysOutput, error) {
	return c.iamClient.ListAccessKeys(ctx, input)
}

func (c *awsClient) ListUserPolicies(ctx context.Context, input *iam.ListUserPoliciesInput) (*iam.ListUserPoliciesOutput, error) {
	return c.iamClient.ListUserPolicies(ctx, input)
}

func (c *awsClient) PutUserPolicy(ctx context.Context, input *iam.PutUserPolicyInput) (*iam.PutUserPolicyOutput, error) {
	return c.iamClient.PutUserPolicy(ctx, input)
}

func (c *awsClient) AttachUserPolicy(ctx context.Context, input *iam.AttachUserPolicyInput) (*iam.AttachUserPolicyOutput, error) {
	return c.iamClient.AttachUserPolicy(ctx, input)
}

func (c *awsClient) DetachUserPolicy(ctx context.Context, input *iam.DetachUserPolicyInput) (*iam.DetachUserPolicyOutput, error) {
	return c.iamClient.DetachUserPolicy(ctx, input)
}

func (c *awsClient) ListPolicies(ctx context.Context, input *iam.ListPoliciesInput) (*iam.ListPoliciesOutput, error) {
	return c.iamClient.ListPolicies(ctx, input)
}

func (c *awsClient) ListAttachedUserPolicies(ctx context.Context, input *iam.ListAttachedUserPoliciesInput) (*iam.ListAttachedUserPoliciesOutput, error) {
	return c.iamClient.ListAttachedUserPolicies(ctx, input)
}

func (c *awsClient) ListRolePolicies(ctx context.Context, input *iam.ListRolePoliciesInput) (*iam.ListRolePoliciesOutput, error) {
	return c.iamClient.ListRolePolicies(ctx, input)
}

func (c *awsClient) DeleteRolePolicy(ctx context.Context, input *iam.DeleteRolePolicyInput) (*iam.DeleteRolePolicyOutput, error) {
	return c.iamClient.DeleteRolePolicy(ctx, input)
}

func (c *awsClient) CreatePolicy(ctx context.Context, input *iam.CreatePolicyInput) (*iam.CreatePolicyOutput, error) {
	return c.iamClient.CreatePolicy(ctx, input)
}

func (c *awsClient) DeletePolicy(ctx context.Context, input *iam.DeletePolicyInput) (*iam.DeletePolicyOutput, error) {
	return c.iamClient.DeletePolicy(ctx, input)
}

func (c *awsClient) DeletePolicyVersion(ctx context.Context, input *iam.DeletePolicyVersionInput) (*iam.DeletePolicyVersionOutput, error) {
	return c.iamClient.DeletePolicyVersion(ctx, input)
}

func (c *awsClient) GetPolicy(ctx context.Context, input *iam.GetPolicyInput) (*iam.GetPolicyOutput, error) {
	return c.iamClient.GetPolicy(ctx, input)
}

func (c *awsClient) GetPolicyVersion(ctx context.Context, input *iam.GetPolicyVersionInput) (*iam.GetPolicyVersionOutput, error) {
	return c.iamClient.GetPolicyVersion(ctx, input)
}

func (c *awsClient) ListPolicyVersions(ctx context.Context, input *iam.ListPolicyVersionsInput) (*iam.ListPolicyVersionsOutput, error) {
	return c.iamClient.ListPolicyVersions(ctx, input)
}

func (c *awsClient) AttachRolePolicy(ctx context.Context, input *iam.AttachRolePolicyInput) (*iam.AttachRolePolicyOutput, error) {
	return c.iamClient.AttachRolePolicy(ctx, input)
}

func (c *awsClient) DetachRolePolicy(ctx context.Context, input *iam.DetachRolePolicyInput) (*iam.DetachRolePolicyOutput, error) {
	return c.iamClient.DetachRolePolicy(ctx, input)
}

func (c *awsClient) PutRolePolicy(ctx context.Context, input *iam.PutRolePolicyInput) (*iam.PutRolePolicyOutput, error) {
	return c.iamClient.PutRolePolicy(ctx, input)
}

func (c *awsClient) ListAttachedRolePolicies(ctx context.Context, input *iam.ListAttachedRolePoliciesInput) (*iam.ListAttachedRolePoliciesOutput, error) {
	return c.iamClient.ListAttachedRolePolicies(ctx, input)
}

func (c *awsClient) CreateRole(ctx context.Context, input *iam.CreateRoleInput) (*iam.CreateRoleOutput, error) {
	return c.iamClient.CreateRole(ctx, input)
}

func (c *awsClient) GetRole(ctx context.Context, input *iam.GetRoleInput) (*iam.GetRoleOutput, error) {
	return c.iamClient.GetRole(ctx, input)
}

func (c *awsClient) DeleteRole(ctx context.Context, input *iam.DeleteRoleInput) (*iam.DeleteRoleOutput, error) {
	return c.iamClient.DeleteRole(ctx, input)
}

func (c *awsClient) ListRoles(ctx context.Context, input *iam.ListRolesInput) (*iam.ListRolesOutput, error) {
	return c.iamClient.ListRoles(ctx, input)
}

func (c *awsClient) ListAccounts(ctx context.Context, input *organizations.ListAccountsInput) (*organizations.ListAccountsOutput, error) {
	return c.orgClient.ListAccounts(ctx, input)
}

func (c *awsClient) ListCreateAccountStatus(ctx context.Context, input *organizations.ListCreateAccountStatusInput) (*organizations.ListCreateAccountStatusOutput, error) {
	return c.orgClient.ListCreateAccountStatus(ctx, input)
}

func (c *awsClient) CreateAccount(ctx context.Context, input *organizations.CreateAccountInput) (*organizations.CreateAccountOutput, error) {
	return c.orgClient.CreateAccount(ctx, input)
}

func (c *awsClient) DescribeCreateAccountStatus(ctx context.Context, input *organizations.DescribeCreateAccountStatusInput) (*organizations.DescribeCreateAccountStatusOutput, error) {
	return c.orgClient.DescribeCreateAccountStatus(ctx, input)
}

func (c *awsClient) MoveAccount(ctx context.Context, input *organizations.MoveAccountInput) (*organizations.MoveAccountOutput, error) {
	return c.orgClient.MoveAccount(ctx, input)
}

func (c *awsClient) CreateOrganizationalUnit(ctx context.Context, input *organizations.CreateOrganizationalUnitInput) (*organizations.CreateOrganizationalUnitOutput, error) {
	return c.orgClient.CreateOrganizationalUnit(ctx, input)
}

func (c *awsClient) ListOrganizationalUnitsForParent(ctx context.Context, input *organizations.ListOrganizationalUnitsForParentInput) (*organizations.ListOrganizationalUnitsForParentOutput, error) {
	return c.orgClient.ListOrganizationalUnitsForParent(ctx, input)
}

func (c *awsClient) ListChildren(ctx context.Context, input *organizations.ListChildrenInput) (*organizations.ListChildrenOutput, error) {
	return c.orgClient.ListChildren(ctx, input)
}

func (c *awsClient) TagResource(ctx context.Context, input *organizations.TagResourceInput) (*organizations.TagResourceOutput, error) {
	return c.orgClient.TagResource(ctx, input)
}

func (c *awsClient) UntagResource(ctx context.Context, input *organizations.UntagResourceInput) (*organizations.UntagResourceOutput, error) {
	return c.orgClient.UntagResource(ctx, input)
}

func (c *awsClient) ListParents(ctx context.Context, input *organizations.ListParentsInput) (*organizations.ListParentsOutput, error) {
	return c.orgClient.ListParents(ctx, input)
}

func (c *awsClient) ListTagsForResource(ctx context.Context, input *organizations.ListTagsForResourceInput) (*organizations.ListTagsForResourceOutput, error) {
	return c.orgClient.ListTagsForResource(ctx, input)
}

func (c *awsClient) AssumeRole(ctx context.Context, input *sts.AssumeRoleInput) (*sts.AssumeRoleOutput, error) {
	return c.stsClient.AssumeRole(ctx, input)
}

func (c *awsClient) CreateCase(ctx context.Context, input *support.CreateCaseInput) (*support.CreateCaseOutput, error) {
	return c.supportClient.CreateCase(ctx, input)
}

func (c *awsClient) DescribeCases(ctx context.Context, input *support.DescribeCasesInput) (*support.DescribeCasesOutput, error) {
	return c.supportClient.DescribeCases(ctx, input)
}

func (c *awsClient) GetCallerIdentity(ctx context.Context, input *sts.GetCallerIdentityInput) (*sts.GetCallerIdentityOutput, error) {
	return c.stsClient.GetCallerIdentity(ctx, input)
}

func (c *awsClient) GetFederationToken(ctx context.Context, input *sts.GetFederationTokenInput) (*sts.GetFederationTokenOutput, error) {
	GetFederationTokenOutput, err := c.stsClient.GetFederationToken(ctx, input)
	if GetFederationTokenOutput != nil {
		return GetFederationTokenOutput, err
	}
	return &sts.GetFederationTokenOutput{}, err
}

func (c *awsClient) ListBuckets(ctx context.Context, input *s3.ListBucketsInput) (*s3.ListBucketsOutput, error) {
	return c.s3Client.ListBuckets(ctx, input)
}

func (c *awsClient) DeleteBucket(ctx context.Context, input *s3.DeleteBucketInput) (*s3.DeleteBucketOutput, error) {
	return c.s3Client.DeleteBucket(ctx, input)
}

func (c *awsClient) ListObjectsV2(ctx context.Context, input *s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error) {
	return c.s3Client.ListObjectsV2(ctx, input)
}

func (c *awsClient) BatchDeleteBucketObjects(ctx context.Context, bucketName *string) error {
	// List all objects in the bucket
	paginator := s3.NewListObjectsV2Paginator(c.s3Client, &s3.ListObjectsV2Input{
		Bucket: bucketName,
	})

	var objectsToDelete []s3types.ObjectIdentifier
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, obj := range page.Contents {
			objectsToDelete = append(objectsToDelete, s3types.ObjectIdentifier{
				Key: obj.Key,
			})
		}
	}

	// Delete objects in batches of 1000 (AWS limit)
	batchSize := 1000
	for i := 0; i < len(objectsToDelete); i += batchSize {
		end := i + batchSize
		if end > len(objectsToDelete) {
			end = len(objectsToDelete)
		}

		batch := objectsToDelete[i:end]
		_, err := c.s3Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: bucketName,
			Delete: &s3types.Delete{
				Objects: batch,
			},
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *awsClient) ListHostedZones(ctx context.Context, input *route53.ListHostedZonesInput) (*route53.ListHostedZonesOutput, error) {
	return c.route53client.ListHostedZones(ctx, input)
}

func (c *awsClient) DeleteHostedZone(ctx context.Context, input *route53.DeleteHostedZoneInput) (*route53.DeleteHostedZoneOutput, error) {
	return c.route53client.DeleteHostedZone(ctx, input)
}

func (c *awsClient) ListResourceRecordSets(ctx context.Context, input *route53.ListResourceRecordSetsInput) (*route53.ListResourceRecordSetsOutput, error) {
	return c.route53client.ListResourceRecordSets(ctx, input)
}

func (c *awsClient) ChangeResourceRecordSets(ctx context.Context, input *route53.ChangeResourceRecordSetsInput) (*route53.ChangeResourceRecordSetsOutput, error) {
	return c.route53client.ChangeResourceRecordSets(ctx, input)
}

func (c *awsClient) GetServiceQuota(ctx context.Context, input *servicequotas.GetServiceQuotaInput) (*servicequotas.GetServiceQuotaOutput, error) {
	return c.serviceQuotasClient.GetServiceQuota(ctx, input)
}

func (c *awsClient) RequestServiceQuotaIncrease(ctx context.Context, input *servicequotas.RequestServiceQuotaIncreaseInput) (*servicequotas.RequestServiceQuotaIncreaseOutput, error) {
	return c.serviceQuotasClient.RequestServiceQuotaIncrease(ctx, input)
}

func (c *awsClient) ListRequestedServiceQuotaChangeHistory(ctx context.Context, input *servicequotas.ListRequestedServiceQuotaChangeHistoryInput) (*servicequotas.ListRequestedServiceQuotaChangeHistoryOutput, error) {
	return c.serviceQuotasClient.ListRequestedServiceQuotaChangeHistory(ctx, input)
}

func (c *awsClient) ListRequestedServiceQuotaChangeHistoryByQuota(ctx context.Context, input *servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaInput) (*servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaOutput, error) {
	return c.serviceQuotasClient.ListRequestedServiceQuotaChangeHistoryByQuota(ctx, input)
}

var awsApiTimeout time.Duration = 30 * time.Second
var awsApiMaxRetries int = 10

// NewClient creates our client wrapper object for the actual AWS clients we use.
// If controllerName is nonempty, metrics are collected timing and counting each AWS request.
func newClient(controllerName, awsAccessID, awsAccessSecret, token, region string) (Client, error) {
	// Create HTTP client with timeout
	httpClient := &http.Client{
		Timeout: awsApiTimeout,
	}

	// Create AWS credentials provider
	credsProvider := credentials.NewStaticCredentialsProvider(awsAccessID, awsAccessSecret, token)

	// Create base AWS config
	awsConfig := aws.Config{
		Region:      region,
		Credentials: credsProvider,
		HTTPClient:  httpClient,
		Retryer: func() aws.Retryer {
			return retry.NewStandard(func(opts *retry.StandardOptions) {
				opts.MaxAttempts = awsApiMaxRetries
				opts.MaxBackoff = 2 * time.Second
			})
		},
	}

	// Add metrics middleware if controller name is provided
	if controllerName != "" {
		awsConfig.APIOptions = append(awsConfig.APIOptions, func(stack *middleware.Stack) error {
			return stack.Deserialize.Add(middleware.DeserializeMiddlewareFunc(
				"MetricsMiddleware",
				func(ctx context.Context, in middleware.DeserializeInput, next middleware.DeserializeHandler) (middleware.DeserializeOutput, middleware.Metadata, error) {
					startTime := time.Now()
					out, metadata, err := next.HandleDeserialize(ctx, in)

					// Extract HTTP request and response for metrics
					if smithyReq, ok := in.Request.(*smithyhttp.Request); ok {
						httpReq := smithyReq.Request
						var httpResp *http.Response
						if smithyResp, ok := out.RawResponse.(*smithyhttp.Response); ok {
							httpResp = smithyResp.Response
						}
						localmetrics.Collector.AddAPICall(controllerName, httpReq, httpResp, time.Since(startTime).Seconds(), err)
					}

					return out, metadata, err
				},
			), middleware.After)
		})
	}

	// Create EC2 config with regional endpoint
	ec2Config := awsConfig.Copy()
	ec2Config.EndpointResolverWithOptions = aws.EndpointResolverWithOptionsFunc(
		func(service, region string, options ...interface{}) (aws.Endpoint, error) {
			return aws.Endpoint{
				PartitionID:   "aws",
				URL:           fmt.Sprintf("https://ec2.%s.amazonaws.com", region),
				SigningRegion: region,
			}, nil
		},
	)

	return &awsClient{
		acctClient:          account.NewFromConfig(awsConfig),
		iamClient:           iam.NewFromConfig(awsConfig),
		ec2Client:           ec2.NewFromConfig(ec2Config),
		orgClient:           organizations.NewFromConfig(awsConfig),
		route53client:       route53.NewFromConfig(awsConfig),
		s3Client:            s3.NewFromConfig(awsConfig),
		stsClient:           sts.NewFromConfig(awsConfig),
		supportClient:       support.NewFromConfig(awsConfig),
		serviceQuotasClient: servicequotas.NewFromConfig(awsConfig),
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
