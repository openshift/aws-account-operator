package account

import (
	"fmt"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/go-logr/logr"
	"github.com/golang/mock/gomock"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/awsclient/mock"
	"github.com/openshift/aws-account-operator/pkg/controller/testutils"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
)

type testRunInstanceInputBuilder struct {
	instanceInput ec2.RunInstancesInput
}

func newTestRunInstanceInputBuilder() *testRunInstanceInputBuilder {
	commonTags := []*ec2.Tag{
		{
			Key:   aws.String("clusterAccountName"),
			Value: aws.String(""),
		},
		{
			Key:   aws.String("clusterNamespace"),
			Value: aws.String(""),
		},
		{
			Key:   aws.String("clusterClaimLink"),
			Value: aws.String(""),
		},
		{
			Key:   aws.String("clusterClaimLinkNamespace"),
			Value: aws.String(""),
		},
		{
			Key:   aws.String("Name"),
			Value: aws.String("red-hat-region-init"),
		},
	}
	input := ec2.RunInstancesInput{
		BlockDeviceMappings: []*ec2.BlockDeviceMapping{
			{
				DeviceName: aws.String("/dev/sda1"),
				Ebs: &ec2.EbsBlockDevice{
					DeleteOnTermination: aws.Bool(true),
					Encrypted:           aws.Bool(true),
					VolumeSize:          aws.Int64(10),
				},
			},
		},
		ImageId:      aws.String("fakeami"),
		InstanceType: aws.String("t2.micro"),
		MaxCount:     aws.Int64(1),
		MinCount:     aws.Int64(1),
		TagSpecifications: []*ec2.TagSpecification{
			{
				ResourceType: &awsv1alpha1.InstanceResourceType,
				Tags:         commonTags,
			},
			{
				ResourceType: aws.String("volume"),
				Tags:         commonTags,
			},
		},
	}
	return &testRunInstanceInputBuilder{
		instanceInput: input,
	}
}

func (inputbuilder *testRunInstanceInputBuilder) WithKmsKeyId(kmsKeyId string) *testRunInstanceInputBuilder {
	inputbuilder.instanceInput.BlockDeviceMappings[0].Ebs.KmsKeyId = &kmsKeyId
	return inputbuilder
}

func TestCreateEC2Instance(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAWSClient := mock.NewMockClient(ctrl)
	instanceInfo := awsv1alpha1.AmiSpec{
		Ami:          "fakeami",
		InstanceType: "t2.micro",
	}
	type args struct {
		reqLogger           logr.Logger
		account             *awsv1alpha1.Account
		client              awsclient.Client
		instanceInfo        awsv1alpha1.AmiSpec
		managedTags         []awsclient.AWSTag
		customerTags        []awsclient.AWSTag
		customerKmsKeyId    string
		instanceInput       *ec2.RunInstancesInput
		instanceOutput      *ec2.Reservation
		instanceOutputError error
	}
	tests := []struct {
		name     string
		args     args
		expected string
		wantErr  bool
	}{
		{"Start instance without customer supplied key", args{
			reqLogger:        testutils.NullLogger{},
			account:          &newTestAccountBuilder().acct,
			client:           mockAWSClient,
			instanceInfo:     instanceInfo,
			managedTags:      []awsclient.AWSTag{},
			customerTags:     []awsclient.AWSTag{},
			customerKmsKeyId: "",
			instanceInput:    &newTestRunInstanceInputBuilder().instanceInput,
			instanceOutput: &ec2.Reservation{
				Groups: []*ec2.GroupIdentifier{},
				Instances: []*ec2.Instance{
					{
						InstanceId: aws.String("1"),
					},
				},
				OwnerId:       aws.String("red-hat"),
				RequesterId:   aws.String("aao"),
				ReservationId: aws.String("1"),
			},
			instanceOutputError: nil,
		}, "1", false},
		{"Start instance with customer supplied key", args{
			reqLogger:        testutils.NullLogger{},
			account:          &newTestAccountBuilder().acct,
			client:           mockAWSClient,
			instanceInfo:     instanceInfo,
			managedTags:      []awsclient.AWSTag{},
			customerTags:     []awsclient.AWSTag{},
			customerKmsKeyId: "123456",
			instanceInput:    &newTestRunInstanceInputBuilder().WithKmsKeyId("123456").instanceInput,
			instanceOutput: &ec2.Reservation{
				Groups: []*ec2.GroupIdentifier{},
				Instances: []*ec2.Instance{
					{
						InstanceId: aws.String("1"),
					},
				},
				OwnerId:       aws.String("red-hat"),
				RequesterId:   aws.String("aao"),
				ReservationId: aws.String("1"),
			},
			instanceOutputError: nil,
		}, "1", false},
		{"Failing to start intances return error", args{
			reqLogger:        testutils.NullLogger{},
			account:          &newTestAccountBuilder().acct,
			client:           mockAWSClient,
			instanceInfo:     instanceInfo,
			managedTags:      []awsclient.AWSTag{},
			customerTags:     []awsclient.AWSTag{},
			customerKmsKeyId: "",
			instanceInput:    &newTestRunInstanceInputBuilder().instanceInput,
			instanceOutput: &ec2.Reservation{
				Groups:        []*ec2.GroupIdentifier{},
				Instances:     []*ec2.Instance{},
				OwnerId:       aws.String("red-hat"),
				RequesterId:   aws.String("aao"),
				ReservationId: aws.String("1"),
			},
			instanceOutputError: awserr.New("Test", "Test", fmt.Errorf("Test")),
		}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAWSClient.EXPECT().RunInstances(tt.args.instanceInput).MinTimes(1).MaxTimes(1).Return(tt.args.instanceOutput, tt.args.instanceOutputError)
			got, err := CreateEC2Instance(tt.args.reqLogger, tt.args.account, tt.args.client, tt.args.instanceInfo, tt.args.managedTags, tt.args.customerTags, tt.args.customerKmsKeyId)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateEC2Instance() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.expected {
				t.Errorf("CreateEC2Instance() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestReconcileAccount_InitializeSupportedRegions(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAWSBuilder := mock.NewMockIBuilder(ctrl)
	mockAWSClient := mock.NewMockClient(ctrl)
	mockAWSBuilder.EXPECT().GetClient(gomock.Any(), gomock.Any(), gomock.Any()).Return(mockAWSClient, nil)
	mockAWSClient.EXPECT().DescribeInstances(gomock.Any()).Return(&ec2.DescribeInstancesOutput{}, nil).MinTimes(2).MaxTimes(3)
	mockAWSClient.EXPECT().RunInstances(gomock.Any()).Return(&ec2.Reservation{
		Groups: []*ec2.GroupIdentifier{},
		Instances: []*ec2.Instance{
			{
				InstanceId: aws.String("1"),
			},
		},
		OwnerId:       aws.String("red-hat"),
		RequesterId:   aws.String("aao"),
		ReservationId: aws.String("1"),
	}, nil)
	mockAWSClient.EXPECT().DescribeInstanceStatus(gomock.Any()).Return(&ec2.DescribeInstanceStatusOutput{
		InstanceStatuses: []*ec2.InstanceStatus{
			{
				InstanceState: &ec2.InstanceState{
					Code: aws.Int64(16),
					Name: aws.String("Running"),
				},
			},
		},
	}, nil)
	mockAWSClient.EXPECT().TerminateInstances(gomock.Any()).Return(&ec2.TerminateInstancesOutput{}, nil)
	type fields struct {
		Client           client.Client
		scheme           *runtime.Scheme
		awsClientBuilder awsclient.IBuilder
		shardName        string
	}
	type args struct {
		reqLogger  logr.Logger
		account    *awsv1alpha1.Account
		regions    []awsv1alpha1.AwsRegions
		creds      *sts.AssumeRoleOutput
		regionAMIs map[string]awsv1alpha1.AmiSpec
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		{"Log failure to retrieve KMS Key from claim.",
			fields{
				Client:           fake.NewFakeClient(),
				scheme:           scheme.Scheme,
				awsClientBuilder: mockAWSBuilder,
				shardName:        "test",
			}, args{
				reqLogger: &testutils.TestLogger{},
				account:   &awsv1alpha1.Account{},
				regions: []awsv1alpha1.AwsRegions{
					{
						Name: "us-east-1",
					}},
				creds: &sts.AssumeRoleOutput{
					AssumedRoleUser: &sts.AssumedRoleUser{},
					Credentials: &sts.Credentials{
						AccessKeyId:     aws.String("123456"),
						Expiration:      &time.Time{},
						SecretAccessKey: aws.String("123456"),
						SessionToken:    aws.String("123456"),
					},
					PackedPolicySize: new(int64),
				},
				regionAMIs: map[string]awsv1alpha1.AmiSpec{},
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ReconcileAccount{
				Client:           tt.fields.Client,
				scheme:           tt.fields.scheme,
				awsClientBuilder: tt.fields.awsClientBuilder,
				shardName:        tt.fields.shardName,
			}
			r.InitializeSupportedRegions(tt.args.reqLogger, tt.args.account, tt.args.regions, tt.args.creds, tt.args.regionAMIs)
			assert.Contains(t, tt.args.reqLogger.(*testutils.TestLogger).Output, "Could not retrieve account claim for account. [account ]")
		})
	}
}
