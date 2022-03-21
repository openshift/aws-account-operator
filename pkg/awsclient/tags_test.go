package awsclient

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/iam"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("AWS Resource Tag Builder", func() {

	When("Building happy path AWS Tags", func() {
		var (
			account = awsv1alpha1.Account{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tagsTest",
					Namespace: "tagsTestNamespace",
				},
				Spec: awsv1alpha1.AccountSpec{
					ClaimLink:          "tagsTestClaimLink",
					ClaimLinkNamespace: "tagsTestClaimLinkNamespace",
				},
			}
			managedTags = []AWSTag{
				{
					Key:   "managedTagKey1",
					Value: "managedTagValue1",
				},
				{
					Key:   "managedTagKey2",
					Value: "managedTagValue2",
				},
			}
			customTags = []AWSTag{
				{
					Key:   "managedTagKey1",
					Value: "managedTagValue1",
				},
				{
					Key:   "managedTagKey2",
					Value: "managedTagValue2",
				},
			}
			tagBuilder = AWSTags.BuildTags(&account, managedTags, customTags)
		)

		When("creating IAM resource tags", func() {
			var tags []*iam.Tag = tagBuilder.GetIAMTags()
			var hardCodedTags = 4

			It("Should not add unexpected tags", func() {
				var expectedCount = len(managedTags) + len(customTags) + hardCodedTags
				Expect(len(tags)).To(Equal(expectedCount))
			})

			It("Should add account name tag", func() {
				Expect(tags).To(ContainElement(iamTag(awsv1alpha1.ClusterAccountNameTagKey, account.Name)))
			})

			It("Should add cluster namespace tag", func() {
				Expect(tags).To(ContainElement(iamTag(awsv1alpha1.ClusterNamespaceTagKey, account.Namespace)))
			})

			It("Should add cluster ClaimLink tag", func() {
				Expect(tags).To(ContainElement(iamTag(awsv1alpha1.ClusterClaimLinkTagKey, account.Spec.ClaimLink)))
			})

			It("Should add cluster ClaimLinkNamespace tag", func() {
				Expect(tags).To(ContainElement(iamTag(awsv1alpha1.ClusterClaimLinkNamespaceTagKey, account.Spec.ClaimLinkNamespace)))
			})

			It("Should add managed tags", func() {
				Expect(tags).To(ContainElements(iamTags(managedTags)))
			})

			It("Should add custom tags", func() {
				Expect(tags).To(ContainElements(iamTags(customTags)))
			})

			It("Should set ec2 instance name tag", func() {
				Expect(tags).NotTo(ContainElement(iamTag(awsv1alpha1.EC2InstanceNameTagKey, awsv1alpha1.EC2InstanceNameTagValue)))
			})
		})

		When("creating EC2 resource tags", func() {
			var tags []*ec2.Tag = tagBuilder.GetEC2Tags()
			var hardCodedTags = 5

			It("Should not add unexpected tags", func() {
				var expectedCount = len(managedTags) + len(customTags) + hardCodedTags
				Expect(len(tags)).To(Equal(expectedCount))
			})

			It("Should add account name tag", func() {
				Expect(tags).To(ContainElement(ec2Tag(awsv1alpha1.ClusterAccountNameTagKey, account.Name)))
			})

			It("Should add cluster namespace tag", func() {
				Expect(tags).To(ContainElement(ec2Tag(awsv1alpha1.ClusterNamespaceTagKey, account.Namespace)))
			})

			It("Should add cluster ClaimLink tag", func() {
				Expect(tags).To(ContainElement(ec2Tag(awsv1alpha1.ClusterClaimLinkTagKey, account.Spec.ClaimLink)))
			})

			It("Should add cluster ClaimLinkNamespace tag", func() {
				Expect(tags).To(ContainElement(ec2Tag(awsv1alpha1.ClusterClaimLinkNamespaceTagKey, account.Spec.ClaimLinkNamespace)))
			})

			It("Should add managed tags", func() {
				Expect(tags).To(ContainElements(ec2Tags(managedTags)))
			})

			It("Should add custom tags", func() {
				Expect(tags).To(ContainElements(ec2Tags(customTags)))
			})

			It("Should add hard coded machine name so we dont spook customers", func() {
				Expect(tags).To(ContainElement(ec2Tag(awsv1alpha1.EC2InstanceNameTagKey, awsv1alpha1.EC2InstanceNameTagValue)))
			})
		})
	})
})

func iamTag(key string, value string) *iam.Tag {
	return &iam.Tag{
		Key:   aws.String(key),
		Value: aws.String(value),
	}
}

func iamTags(tags []AWSTag) []*iam.Tag {
	var convertedTags []*iam.Tag
	for _, tag := range tags {
		convertedTags = append(convertedTags, iamTag(tag.Key, tag.Value))
	}
	return convertedTags
}

func ec2Tag(key string, value string) *ec2.Tag {
	return &ec2.Tag{
		Key:   aws.String(key),
		Value: aws.String(value),
	}
}

func ec2Tags(tags []AWSTag) []*ec2.Tag {
	var convertedTags []*ec2.Tag
	for _, tag := range tags {
		convertedTags = append(convertedTags, ec2Tag(tag.Key, tag.Value))
	}
	return convertedTags
}
