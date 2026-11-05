package accountclaim

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	route53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	awsmock "github.com/openshift/aws-account-operator/pkg/awsclient/mock"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCleanUpAwsAccountS3UsesBucketRegions(t *testing.T) {
	ctrl := gomock.NewController(t)
	base := awsmock.NewMockClient(ctrl)
	east := awsmock.NewMockClient(ctrl)
	west := awsmock.NewMockClient(ctrl)

	base.EXPECT().ListBuckets(gomock.Any(), gomock.Any()).Return(&s3.ListBucketsOutput{
		Buckets: []s3types.Bucket{{Name: aws.String("east-bucket")}, {Name: aws.String("west-bucket")}},
	}, nil)
	gomock.InOrder(
		base.EXPECT().S3ClientForBucket(gomock.Any(), "east-bucket").Return(east, nil),
		east.EXPECT().BatchDeleteBucketObjects(gomock.Any(), aws.String("east-bucket")).Return(nil),
		east.EXPECT().DeleteBucket(gomock.Any(), &s3.DeleteBucketInput{Bucket: aws.String("east-bucket")}).Return(&s3.DeleteBucketOutput{}, nil),
		base.EXPECT().S3ClientForBucket(gomock.Any(), "west-bucket").Return(west, nil),
		west.EXPECT().BatchDeleteBucketObjects(gomock.Any(), aws.String("west-bucket")).Return(nil),
		west.EXPECT().DeleteBucket(gomock.Any(), &s3.DeleteBucketInput{Bucket: aws.String("west-bucket")}).Return(&s3.DeleteBucketOutput{}, nil),
	)

	notifications, errors := make(chan string, 1), make(chan string, 1)
	r := &AccountClaimReconciler{}
	if err := r.cleanUpAwsAccountS3(context.Background(), logr.Discard(), base, notifications, errors); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errors:
		t.Fatalf("cleanup failed: %s", err)
	default:
	}
	if msg := <-notifications; msg != "S3 cleanup finished successfully" {
		t.Fatalf("unexpected cleanup notification: %s", msg)
	}
}

func TestCleanUpAwsAccountS3SkipsMissingBucket(t *testing.T) {
	ctrl := gomock.NewController(t)
	base := awsmock.NewMockClient(ctrl)
	regional := awsmock.NewMockClient(ctrl)
	missing := &smithyhttp.ResponseError{
		Response: &smithyhttp.Response{Response: &http.Response{StatusCode: http.StatusNotFound, Header: make(http.Header)}},
		Err:      errors.New("not found"),
	}
	base.EXPECT().ListBuckets(gomock.Any(), gomock.Any()).Return(&s3.ListBucketsOutput{
		Buckets: []s3types.Bucket{{Name: aws.String("gone")}, {Name: aws.String("remaining")}},
	}, nil)
	gomock.InOrder(
		base.EXPECT().S3ClientForBucket(gomock.Any(), "gone").Return(nil, missing),
		base.EXPECT().S3ClientForBucket(gomock.Any(), "remaining").Return(regional, nil),
		regional.EXPECT().BatchDeleteBucketObjects(gomock.Any(), aws.String("remaining")).Return(nil),
		regional.EXPECT().DeleteBucket(gomock.Any(), &s3.DeleteBucketInput{Bucket: aws.String("remaining")}).Return(&s3.DeleteBucketOutput{}, nil),
	)

	notifications, awsErrors := make(chan string, 1), make(chan string, 1)
	r := &AccountClaimReconciler{}
	if err := r.cleanUpAwsAccountS3(context.Background(), logr.Discard(), base, notifications, awsErrors); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-awsErrors:
		t.Fatalf("cleanup failed: %s", err)
	default:
	}
	if msg := <-notifications; msg != "S3 cleanup finished successfully" {
		t.Fatalf("unexpected cleanup notification: %s", msg)
	}
}

func TestCleanUpAwsAccountSkipsS3WithAnnotation(t *testing.T) {
	ctrl := gomock.NewController(t)
	base := awsmock.NewMockClient(ctrl)

	// Create an account claim with the skip-s3-cleanup annotation
	accountClaim := &awsv1alpha1.AccountClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-claim",
			Namespace: "test-namespace",
			Annotations: map[string]string{
				"aws.managed.openshift.io/skip-s3-cleanup": "true",
			},
		},
		Spec: awsv1alpha1.AccountClaimSpec{
			LegalEntity: awsv1alpha1.LegalEntity{
				Name: "TestCorp",
				ID:   "test123",
			},
			Aws: awsv1alpha1.Aws{
				Regions: []awsv1alpha1.AwsRegions{
					{Name: "us-east-1"},
				},
			},
		},
	}

	// Mock expectations for cleanup functions that should run
	// (snapshots, EBS volumes, VPC endpoints, Route53)
	// S3 cleanup should NOT be called
	base.EXPECT().DescribeSnapshots(gomock.Any(), gomock.Any()).Return(&ec2.DescribeSnapshotsOutput{
		Snapshots: []ec2types.Snapshot{},
	}, nil)
	base.EXPECT().DescribeVolumes(gomock.Any(), gomock.Any()).Return(&ec2.DescribeVolumesOutput{
		Volumes: []ec2types.Volume{},
	}, nil)
	base.EXPECT().DescribeVpcEndpointServiceConfigurations(gomock.Any(), gomock.Any()).Return(&ec2.DescribeVpcEndpointServiceConfigurationsOutput{
		ServiceConfigurations: []ec2types.ServiceConfiguration{},
	}, nil)
	base.EXPECT().ListHostedZones(gomock.Any(), gomock.Any()).Return(&route53.ListHostedZonesOutput{
		HostedZones: []route53types.HostedZone{},
		IsTruncated: false,
	}, nil)

	// ListBuckets should NOT be called if S3 cleanup is skipped
	// No S3-related mocks here means if S3 cleanup runs, test will fail

	r := &AccountClaimReconciler{}
	err := r.cleanUpAwsAccount(context.Background(), logr.Discard(), base, accountClaim)
	if err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}
}
