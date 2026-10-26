package accountclaim

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/go-logr/logr"
	awsmock "github.com/openshift/aws-account-operator/pkg/awsclient/mock"
	"go.uber.org/mock/gomock"
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
		east.EXPECT().ListObjectsV2(gomock.Any(), &s3.ListObjectsV2Input{Bucket: aws.String("east-bucket")}).Return(&s3.ListObjectsV2Output{}, nil),
		east.EXPECT().DeleteBucket(gomock.Any(), &s3.DeleteBucketInput{Bucket: aws.String("east-bucket")}).Return(&s3.DeleteBucketOutput{}, nil),
		base.EXPECT().S3ClientForBucket(gomock.Any(), "west-bucket").Return(west, nil),
		west.EXPECT().ListObjectsV2(gomock.Any(), &s3.ListObjectsV2Input{Bucket: aws.String("west-bucket")}).Return(&s3.ListObjectsV2Output{
			Contents: []s3types.Object{{Key: aws.String("object")}},
		}, nil),
		west.EXPECT().BatchDeleteBucketObjects(gomock.Any(), aws.String("west-bucket")).Return(nil),
		west.EXPECT().DeleteBucket(gomock.Any(), &s3.DeleteBucketInput{Bucket: aws.String("west-bucket")}).Return(&s3.DeleteBucketOutput{}, nil),
	)

	notifications, errors := make(chan string, 1), make(chan string, 1)
	r := &AccountClaimReconciler{}
	if err := r.cleanUpAwsAccountS3(logr.Discard(), base, notifications, errors); err != nil {
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
