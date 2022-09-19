package awsclient

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/iam"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
)

// AWSTag is a representation of an AWS Tag
type AWSTag struct {
	Key   string
	Value string
}

// AWSAccountOperatorTags contains a list of tags to be applied to resources created by the aws-account-operator
type AWSAccountOperatorTags struct {
	Tags []AWSTag
}

// AWSTags implements AWSTagBuilder to return AWS Tags
var AWSTags *AWSAccountOperatorTags

// AWSTagBuilder provides a common interface to generate AWS Tags
type AWSTagBuilder interface {
	GetIAMTags() []*iam.Tag
	GetEC2Tags() []*ec2.Tag
}

// GetIAMTags returns IAM tags
func (t *AWSAccountOperatorTags) GetIAMTags() []*iam.Tag {
	var tags []*iam.Tag
	for _, tag := range t.Tags {
		tags = append(tags, &iam.Tag{Key: aws.String(tag.Key), Value: aws.String(tag.Value)})
	}
	return tags
}

// GetEC2Tags returns EC2 tags
func (t *AWSAccountOperatorTags) GetEC2Tags() []*ec2.Tag {
	var tags []*ec2.Tag
	for _, tag := range t.Tags {
		tags = append(tags, &ec2.Tag{Key: aws.String(tag.Key), Value: aws.String(tag.Value)})
	}

	//make sure the ec2 instance has a descriptive name to avoid customer confusion
	tags = append(tags, &ec2.Tag{Key: aws.String(awsv1alpha1.EC2InstanceNameTagKey), Value: aws.String(awsv1alpha1.EC2InstanceNameTagValue)})

	return tags
}

// BuildTags initializes AWSTags with required tags
func (t *AWSAccountOperatorTags) BuildTags(account *awsv1alpha1.Account, managedTags []AWSTag, customTags []AWSTag) AWSTagBuilder {
	tags := []AWSTag{}

	// Adds a tag for the cluster's Account Name
	tags = append(tags, AWSTag{
		Key:   awsv1alpha1.ClusterAccountNameTagKey,
		Value: account.Name,
	})
	// Add a tag with the cluster's Namespace
	tags = append(tags, AWSTag{
		Key:   awsv1alpha1.ClusterNamespaceTagKey,
		Value: account.Namespace,
	})

	// Add a tag for the cluster's ClaimLink
	tags = append(tags, AWSTag{
		Key:   awsv1alpha1.ClusterClaimLinkTagKey,
		Value: account.Spec.ClaimLink,
	})

	// Add a tag for the cluster's ClaimLink Namespace
	tags = append(tags, AWSTag{
		Key:   awsv1alpha1.ClusterClaimLinkNamespaceTagKey,
		Value: account.Spec.ClaimLinkNamespace,
	})

	// Adds all of the "managed tags" passed in (typically through the configmap)
	tags = append(tags, managedTags...)

	// Adds all Custom Tags passed in (through the accountclaim)
	tags = append(tags, customTags...)

	return &AWSAccountOperatorTags{
		Tags: tags,
	}
}
