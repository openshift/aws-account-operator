package account

import (
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	ststypes "github.com/aws/aws-sdk-go-v2/service/sts/types"
	"github.com/aws/smithy-go"
	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/awsclient/mock"
	"github.com/openshift/aws-account-operator/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type testRunInstanceInputBuilder struct {
	instanceInput ec2.RunInstancesInput
}

func newTestRunInstanceInputBuilder() *testRunInstanceInputBuilder {
	commonTags := []ec2types.Tag{
		{
			Key:   aws.String("clusterAccountName"),
			Value: aws.String(TestAccountName),
		},
		{
			Key:   aws.String("clusterNamespace"),
			Value: aws.String(TestAccountNamespace),
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
		BlockDeviceMappings: []ec2types.BlockDeviceMapping{
			{
				DeviceName: aws.String("/dev/sda1"),
				Ebs: &ec2types.EbsBlockDevice{
					DeleteOnTermination: aws.Bool(true),
					Encrypted:           aws.Bool(true),
					VolumeSize:          aws.Int32(10),
				},
			},
		},
		ImageId:      aws.String("fakeami"),
		InstanceType: ec2types.InstanceTypeT2Micro,
		MaxCount:     aws.Int32(1),
		MinCount:     aws.Int32(1),
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeInstance,
				Tags:         commonTags,
			},
			{
				ResourceType: ec2types.ResourceTypeVolume,
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
		instanceOutput      *ec2.RunInstancesOutput
		instanceOutputError error
	}
	tests := []struct {
		name     string
		args     args
		expected string
		wantErr  bool
	}{
		{"Start instance without customer supplied key", args{
			reqLogger:        testutils.NewTestLogger().Logger(),
			account:          &newTestAccountBuilder().acct,
			client:           mockAWSClient,
			instanceInfo:     instanceInfo,
			managedTags:      []awsclient.AWSTag{},
			customerTags:     []awsclient.AWSTag{},
			customerKmsKeyId: "",
			instanceInput:    &newTestRunInstanceInputBuilder().instanceInput,
			instanceOutput: &ec2.RunInstancesOutput{
				Groups: []ec2types.GroupIdentifier{},
				Instances: []ec2types.Instance{
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
			reqLogger: testutils.NewTestLogger().Logger(),

			account:          &newTestAccountBuilder().acct,
			client:           mockAWSClient,
			instanceInfo:     instanceInfo,
			managedTags:      []awsclient.AWSTag{},
			customerTags:     []awsclient.AWSTag{},
			customerKmsKeyId: "123456",
			instanceInput:    &newTestRunInstanceInputBuilder().WithKmsKeyId("123456").instanceInput,
			instanceOutput: &ec2.RunInstancesOutput{
				Groups: []ec2types.GroupIdentifier{},
				Instances: []ec2types.Instance{
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
			reqLogger:        testutils.NewTestLogger().Logger(),
			account:          &newTestAccountBuilder().acct,
			client:           mockAWSClient,
			instanceInfo:     instanceInfo,
			managedTags:      []awsclient.AWSTag{},
			customerTags:     []awsclient.AWSTag{},
			customerKmsKeyId: "",
			instanceInput:    &newTestRunInstanceInputBuilder().instanceInput,
			instanceOutput: &ec2.RunInstancesOutput{
				Groups:        []ec2types.GroupIdentifier{},
				Instances:     []ec2types.Instance{},
				OwnerId:       aws.String("red-hat"),
				RequesterId:   aws.String("aao"),
				ReservationId: aws.String("1"),
			},
			instanceOutputError: &smithy.GenericAPIError{Code: "Test", Message: "Test"},
		}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAWSClient.EXPECT().RunInstances(gomock.Any(), tt.args.instanceInput).MinTimes(1).MaxTimes(1).Return(tt.args.instanceOutput, tt.args.instanceOutputError)
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
	mockAWSClient.EXPECT().DescribeInstances(gomock.Any(), gomock.Any()).Return(&ec2.DescribeInstancesOutput{}, nil).MinTimes(2).MaxTimes(3)
	mockAWSClient.EXPECT().RunInstances(gomock.Any(), gomock.Any()).Return(&ec2.RunInstancesOutput{
		Groups: []ec2types.GroupIdentifier{},
		Instances: []ec2types.Instance{
			{
				InstanceId: aws.String("1"),
			},
		},
		OwnerId:       aws.String("red-hat"),
		RequesterId:   aws.String("aao"),
		ReservationId: aws.String("1"),
	}, nil)
	mockAWSClient.EXPECT().DescribeInstanceTypes(gomock.Any(), gomock.Any()).Return(&ec2.DescribeInstanceTypesOutput{
		InstanceTypes: []ec2types.InstanceTypeInfo{{
			InstanceType: ec2types.InstanceTypeT3Micro,
		}}}, nil).Times(2)
	mockAWSClient.EXPECT().DescribeImages(gomock.Any(), gomock.Any()).Return(
		&ec2.DescribeImagesOutput{
			Images: []ec2types.Image{
				{
					Architecture: ec2types.ArchitectureValuesX8664,
					ImageId:      aws.String("ami-075ed2fafb0c1aa68"),
					Name:         aws.String("RHEL-8.1.0_HVM-20211007-x86_64-0-Hourly2-GP2"),
					OwnerId:      aws.String("12345"),
				},
			},
		}, nil)
	mockAWSClient.EXPECT().DescribeInstanceStatus(gomock.Any(), gomock.Any()).Return(&ec2.DescribeInstanceStatusOutput{
		InstanceStatuses: []ec2types.InstanceStatus{
			{
				InstanceState: &ec2types.InstanceState{
					Code: aws.Int32(16),
					Name: ec2types.InstanceStateNameRunning,
				},
			},
		},
	}, nil)
	mockAWSClient.EXPECT().TerminateInstances(gomock.Any(), gomock.Any()).Return(&ec2.TerminateInstancesOutput{}, nil)
	type fields struct {
		Client           client.Client
		scheme           *runtime.Scheme
		awsClientBuilder awsclient.IBuilder
		shardName        string
	}
	type args struct {
		reqLogger testutils.TestLogger
		account   *awsv1alpha1.Account
		regions   []awsv1alpha1.AwsRegions
		creds     *sts.AssumeRoleOutput
		amiOwner  string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		{"Log failure to retrieve KMS Key from claim.",
			fields{
				Client:           fake.NewClientBuilder().Build(),
				scheme:           scheme.Scheme,
				awsClientBuilder: mockAWSBuilder,
				shardName:        "test",
			}, args{
			reqLogger: testutils.NewTestLogger(),
			account: &awsv1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name:      TestAccountName,
					Namespace: TestAccountNamespace,
				},
			},
			regions: []awsv1alpha1.AwsRegions{
				{
					Name: "us-east-1",
				}},
			creds: &sts.AssumeRoleOutput{
				AssumedRoleUser: &ststypes.AssumedRoleUser{},
				Credentials: &ststypes.Credentials{
					AccessKeyId:     aws.String("123456"),
					Expiration:      &time.Time{},
					SecretAccessKey: aws.String("123456"),
					SessionToken:    aws.String("123456"),
				},
				PackedPolicySize: new(int32),
			},
			amiOwner: "",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &AccountReconciler{
				Client:           tt.fields.Client,
				Scheme:           tt.fields.scheme,
				awsClientBuilder: tt.fields.awsClientBuilder,
				shardName:        tt.fields.shardName,
			}
			r.InitializeSupportedRegions(tt.args.reqLogger.Logger(), tt.args.account, tt.args.regions, tt.args.creds, tt.args.amiOwner)
			assert.Contains(t, tt.args.reqLogger.Messages(), "Could not retrieve account claim for account.")
		})
	}
}

func TestRetrieveFreeInstanceType(t *testing.T) {
	logger := testutils.NewTestLogger().Logger()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	type args struct {
		logger    logr.Logger
		awsClient awsclient.Client
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{"retrieve a t2.micro instance", args{
			awsClient: func() awsclient.Client {
				mock := mock.NewMockClient(ctrl)
				mock.EXPECT().DescribeInstanceTypes(gomock.Any(), gomock.Any()).Return(nil, &smithy.GenericAPIError{Code: "InvalidInstanceType", Message: "Not found"})
				mock.EXPECT().DescribeInstanceTypes(gomock.Any(), gomock.Any()).Return(&ec2.DescribeInstanceTypesOutput{
					InstanceTypes: []ec2types.InstanceTypeInfo{{
						InstanceType: ec2types.InstanceTypeT2Micro,
					}}}, nil)
				return mock
			}(),
			logger: logger,
		}, "t2.micro", false},
		{"retrieve a t3 instance", args{
			awsClient: func() awsclient.Client {
				mock := mock.NewMockClient(ctrl)
				mock.EXPECT().DescribeInstanceTypes(gomock.Any(), gomock.Any()).Return(&ec2.DescribeInstanceTypesOutput{
					InstanceTypes: []ec2types.InstanceTypeInfo{{
						InstanceType: ec2types.InstanceTypeT3Micro,
					}}}, nil)
				return mock
			}(),
			logger: logger,
		}, "t3.micro", false},
		{"can not retrieve an instance - other error", args{
			awsClient: func() awsclient.Client {
				mock := mock.NewMockClient(ctrl)
				mock.EXPECT().DescribeInstanceTypes(gomock.Any(), gomock.Any()).Return(nil, errors.New("an error happened"))
				return mock
			}(),
			logger: logger,
		}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RetrieveAvailableMicroInstanceType(tt.args.logger, tt.args.awsClient)
			if (err != nil) != tt.wantErr {
				t.Errorf("RetrieveFreeInstanceType() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("RetrieveFreeInstanceType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRetrieveAmi(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	type args struct {
		awsClient awsclient.Client
		amiOwner  string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "Choose non-SAP image",
			args: args{
				awsClient: func() awsclient.Client {
					mock := mock.NewMockClient(ctrl)
					mock.EXPECT().DescribeImages(gomock.Any(), gomock.Any()).Return(
						&ec2.DescribeImagesOutput{
							Images: []ec2types.Image{
								{
									Architecture: ec2types.ArchitectureValuesX8664,
									ImageId:      aws.String("ami-075ed2fafb0c1aa69"),
									Name:         aws.String("RHEL-SAP-8.1.0_HVM-20211007-x86_64-0-Hourly2-GP2"),
									OwnerId:      aws.String("12345"),
								},
								{
									Architecture: ec2types.ArchitectureValuesX8664,
									ImageId:      aws.String("ami-075ed2fafb0c1aa68"),
									Name:         aws.String("RHEL-8.1.0_HVM-20211007-x86_64-0-Hourly2-GP2"),
									OwnerId:      aws.String("12345"),
								},
							},
						}, nil)
					return mock
				}(),
				amiOwner: "12345",
			},
			want:    "ami-075ed2fafb0c1aa68",
			wantErr: false,
		},
		{
			name: "Choose non-beta image",
			args: args{
				awsClient: func() awsclient.Client {
					mock := mock.NewMockClient(ctrl)
					mock.EXPECT().DescribeImages(gomock.Any(), gomock.Any()).Return(
						&ec2.DescribeImagesOutput{
							Images: []ec2types.Image{
								{
									Architecture: ec2types.ArchitectureValuesX8664,
									ImageId:      aws.String("ami-075ed2fafb0c1aa69"),
									Name:         aws.String("RHEL-BETA-8.1.0_HVM-20211007-x86_64-0-Hourly2-GP2"),
									OwnerId:      aws.String("12345"),
								},
								{
									Architecture: ec2types.ArchitectureValuesX8664,
									ImageId:      aws.String("ami-075ed2fafb0c1aa68"),
									Name:         aws.String("RHEL-8.1.0_HVM-20211007-x86_64-0-Hourly2-GP2"),
									OwnerId:      aws.String("12345"),
								},
							},
						}, nil)
					return mock
				}(),
				amiOwner: "12345",
			},
			want:    "ami-075ed2fafb0c1aa68",
			wantErr: false,
		},
		{
			name: "error if only SAP and Beta images are available",
			args: args{
				awsClient: func() awsclient.Client {
					mock := mock.NewMockClient(ctrl)
					mock.EXPECT().DescribeImages(gomock.Any(), gomock.Any()).Return(
						&ec2.DescribeImagesOutput{
							Images: []ec2types.Image{
								{
									Architecture: ec2types.ArchitectureValuesX8664,
									ImageId:      aws.String("ami-075ed2fafb0c1aa69"),
									Name:         aws.String("RHEL-BETA-8.1.0_HVM-20211007-x86_64-0-Hourly2-GP2"),
									OwnerId:      aws.String("12345"),
								},
								{
									Architecture: ec2types.ArchitectureValuesX8664,
									ImageId:      aws.String("ami-075ed2fafb0c1aa68"),
									Name:         aws.String("RHEL-SAP-8.1.0_HVM-20211007-x86_64-0-Hourly2-GP2"),
									OwnerId:      aws.String("12345"),
								},
							},
						}, nil)
					return mock
				}(),
				amiOwner: "12345",
			},
			want:    "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RetrieveAmi(tt.args.awsClient, tt.args.amiOwner)
			if (err != nil) != tt.wantErr {
				t.Errorf("RetrieveAmi() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("RetrieveAmi() = %v, want %v", got, tt.want)
			}
		})
	}
}
