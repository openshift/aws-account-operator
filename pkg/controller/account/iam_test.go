package account

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/golang/mock/gomock"
	"github.com/openshift/aws-account-operator/pkg/apis"
	"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient/mock"
	"github.com/openshift/aws-account-operator/pkg/controller/testutils"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
)

func TestIAMCreateSecret(t *testing.T) {

	err := apis.AddToScheme(scheme.Scheme)
	if err != nil {
		fmt.Printf("failed adding to scheme in iam_test.go")
	}

	secret := CreateSecret(
		"test",
		"namespace",
		map[string][]byte{
			"one": []byte("hello"),
			"two": []byte("world"),
		},
	)
	mocks := setupDefaultMocks(t, []runtime.Object{})

	// This is necessary for the mocks to report failures like methods not being called an expected number of times.
	// after mocks is defined
	defer mocks.mockCtrl.Finish()

	r := ReconcileAccount{
		Client: mocks.fakeKubeClient,
		scheme: scheme.Scheme,
	}

	nullLogger := testutils.NullLogger{}
	account := newTestAccountBuilder().acct
	err = r.CreateSecret(nullLogger, &account, secret)
	assert.Nil(t, err)

	err = r.CreateSecret(nullLogger, &account, secret)
	assert.Error(t, err, "")
}

func TestGetSTSCredentials(t *testing.T) {

	mocks := setupDefaultMocks(t, []runtime.Object{})
	nullLogger := testutils.NullLogger{}
	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)

	AccessKeyId := aws.String("MyAccessKeyID")
	Expiration := aws.Time(time.Now().Add(time.Hour))
	SecretAccessKey := aws.String("MySecretAccessKey")
	SessionToken := aws.String("MySessionToken")

	mockAWSClient.EXPECT().AssumeRole(gomock.Any()).Return(
		&sts.AssumeRoleOutput{
			Credentials: &sts.Credentials{
				AccessKeyId:     AccessKeyId,
				Expiration:      Expiration,
				SecretAccessKey: SecretAccessKey,
				SessionToken:    SessionToken,
			},
		},
		nil, // no error
	)

	creds, err := getSTSCredentials(
		nullLogger,
		mockAWSClient,
		"",
		"",
		"",
	)

	assert.Equal(t, creds.Credentials.AccessKeyId, AccessKeyId)
	assert.Equal(t, creds.Credentials.Expiration, Expiration)
	assert.Equal(t, creds.Credentials.SecretAccessKey, SecretAccessKey)
	assert.Equal(t, creds.Credentials.SessionToken, SessionToken)
	assert.NoError(t, err)

	// TODO Test AWS Failure
}

func TestRetryIfAwsServiceFailureOrInvalidToken(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		expectedValue bool
	}{
		{
			name:          "TestServiceFailure",
			err:           awserr.New("ServiceFailure", "", nil),
			expectedValue: true,
		},
		{
			name:          "TestInvalidClientTokenId",
			err:           awserr.New("InvalidClientTokenId", "", nil),
			expectedValue: true,
		},
		{
			name:          "TestAccessDenied",
			err:           awserr.New("AccessDenied", "", nil),
			expectedValue: true,
		},
		{
			name:          "TestNotFound",
			err:           awserr.New("NotFound", "", nil),
			expectedValue: false,
		},
		{
			name:          "TestMyNewError",
			err:           errors.New("MyNewError"),
			expectedValue: false,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			value := retryIfAwsServiceFailureOrInvalidToken(tt.err)
			if value != tt.expectedValue {
				t.Errorf("[TestRetryIfAwsServiceFailureOrInvalidToken()] Got %v, wanted %v", value, tt.expectedValue)
			}
		})
	}
}

func TestListAccessKeys(t *testing.T) {

	mocks := setupDefaultMocks(t, []runtime.Object{})

	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)
	username := "AwesomeUser"
	user := iam.User{UserName: &username}

	expectedAccessKeyID := aws.String("hihi")

	mockAWSClient.EXPECT().ListAccessKeys(gomock.Any()).Return(
		&iam.ListAccessKeysOutput{
			AccessKeyMetadata: []*iam.AccessKeyMetadata{
				{
					AccessKeyId: expectedAccessKeyID,
				},
			},
		},
		nil, // no error
	)

	returnValue, err := listAccessKeys(mockAWSClient, &user)
	assert.Nil(t, err)
	assert.Len(t, returnValue.AccessKeyMetadata, 1)
	assert.Equal(t, returnValue.AccessKeyMetadata[0].AccessKeyId, expectedAccessKeyID)

	mockAWSClient = mock.NewMockClient(mocks.mockCtrl)
	returnErr := awserr.New("AccessDenied", "", nil)

	// Should retry 5 times
	mockAWSClient.EXPECT().ListAccessKeys(gomock.Any()).Return(nil, returnErr)
	mockAWSClient.EXPECT().ListAccessKeys(gomock.Any()).Return(nil, returnErr)
	mockAWSClient.EXPECT().ListAccessKeys(gomock.Any()).Return(nil, returnErr)
	mockAWSClient.EXPECT().ListAccessKeys(gomock.Any()).Return(nil, returnErr)
	mockAWSClient.EXPECT().ListAccessKeys(gomock.Any()).Return(nil, returnErr)

	// retries took long, need to mock it out
	old := DefaultDelay
	DefaultDelay = 0 * time.Second

	returnValue, err = listAccessKeys(mockAWSClient, &user)
	assert.Nil(t, returnValue)
	assert.Error(t, err, returnErr)
	DefaultDelay = old
}

func TestDeleteAccessKey(t *testing.T) {
	mocks := setupDefaultMocks(t, []runtime.Object{})

	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)

	mockAWSClient.EXPECT().DeleteAccessKey(gomock.Any()).Return(
		&iam.DeleteAccessKeyOutput{},
		nil, // no error
	)

	accessKeyID := "accessKeyID"
	username := "username"

	deleteAccessKeyOutput, err := deleteAccessKey(mockAWSClient, &accessKeyID, &username)
	assert.Equal(t, deleteAccessKeyOutput, &iam.DeleteAccessKeyOutput{})
	assert.Nil(t, err)

	mockAWSClient = mock.NewMockClient(mocks.mockCtrl)
	returnErr := awserr.New("AccessDenied", "", nil)

	// Should retry 5 times
	mockAWSClient.EXPECT().DeleteAccessKey(gomock.Any()).Return(&iam.DeleteAccessKeyOutput{}, returnErr)
	mockAWSClient.EXPECT().DeleteAccessKey(gomock.Any()).Return(&iam.DeleteAccessKeyOutput{}, returnErr)
	mockAWSClient.EXPECT().DeleteAccessKey(gomock.Any()).Return(&iam.DeleteAccessKeyOutput{}, returnErr)
	mockAWSClient.EXPECT().DeleteAccessKey(gomock.Any()).Return(&iam.DeleteAccessKeyOutput{}, returnErr)
	mockAWSClient.EXPECT().DeleteAccessKey(gomock.Any()).Return(&iam.DeleteAccessKeyOutput{}, returnErr)

	// retries took long, need to mock it out
	old := DefaultDelay
	DefaultDelay = 0 * time.Second

	deleteAccessKeyOutput, err = deleteAccessKey(mockAWSClient, &accessKeyID, &username)
	assert.Equal(t, deleteAccessKeyOutput, &iam.DeleteAccessKeyOutput{})
	assert.Error(t, err, returnErr)
	DefaultDelay = old
}

func TestDeleteAllAccessKeys(t *testing.T) {
	mocks := setupDefaultMocks(t, []runtime.Object{})

	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)
	username := "AwesomeUser"
	user := iam.User{UserName: &username}

	expectedAccessKeyID := aws.String("expectedAccessKeyID")

	mockAWSClient.EXPECT().ListAccessKeys(&iam.ListAccessKeysInput{UserName: &username}).Return(
		&iam.ListAccessKeysOutput{
			AccessKeyMetadata: []*iam.AccessKeyMetadata{
				{
					AccessKeyId: expectedAccessKeyID,
				},
			},
		},
		nil, // no error
	)
	mockAWSClient.EXPECT().DeleteAccessKey(
		&iam.DeleteAccessKeyInput{
			AccessKeyId: expectedAccessKeyID,
			UserName:    &username,
		}).Return(
		&iam.DeleteAccessKeyOutput{},
		nil, // no error
	)

	err := deleteAllAccessKeys(mockAWSClient, &user)
	assert.Nil(t, err)
}

func TestCreateIAMUser(t *testing.T) {
	mocks := setupDefaultMocks(t, []runtime.Object{})

	// This is necessary for the mocks to report failures like methods not being called an expected number of times.
	// after mocks is defined
	defer mocks.mockCtrl.Finish()

	nullLogger := testutils.NullLogger{}
	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)

	userID := aws.String("123456789")
	usernameStr := "MyUsername"

	expectedCreateUserOutput := &iam.CreateUserOutput{
		User: &iam.User{
			UserId:   userID,
			UserName: aws.String(usernameStr),
		},
	}

	mockAWSClient.EXPECT().CreateUser(&iam.CreateUserInput{
		UserName: aws.String(usernameStr),
	}).Return(
		expectedCreateUserOutput,
		nil, // no error
	)

	createUserOutput, err := CreateIAMUser(nullLogger, mockAWSClient, usernameStr)
	assert.Equal(t, expectedCreateUserOutput, createUserOutput)
	assert.Nil(t, err)
}

func TestAttachAdminUserPolicy(t *testing.T) {
	mocks := setupDefaultMocks(t, []runtime.Object{})

	username := "AwesomeUser"
	user := iam.User{UserName: &username}
	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)

	mockAWSClient.EXPECT().AttachUserPolicy(gomock.Any()).Return(
		&iam.AttachUserPolicyOutput{},
		nil, // no error
	)

	attachAdminUserPolicy, err := AttachAdminUserPolicy(mockAWSClient, &user)
	assert.Equal(t, attachAdminUserPolicy, &iam.AttachUserPolicyOutput{})
	assert.Nil(t, err)
}

func TestCreateUserAccessKey(t *testing.T) {

	mocks := setupDefaultMocks(t, []runtime.Object{})

	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)
	username := "AwesomeUser"
	user := iam.User{UserName: &username}

	expectedAccessKeyID := aws.String("expectedAccessKeyID")

	mockAWSClient.EXPECT().CreateAccessKey(
		&iam.CreateAccessKeyInput{
			UserName: aws.String(username),
		},
	).Return(
		&iam.CreateAccessKeyOutput{
			AccessKey: &iam.AccessKey{
				AccessKeyId: expectedAccessKeyID,
			},
		},
		nil, // no error
	)

	returnValue, err := CreateUserAccessKey(mockAWSClient, &user)
	assert.Equal(t, returnValue.AccessKey.AccessKeyId, expectedAccessKeyID)
	assert.Nil(t, err)

	mockAWSClient = mock.NewMockClient(mocks.mockCtrl)
	returnErr := awserr.New("AccessDenied", "", nil)

	// Should retry 5 times
	mockAWSClient.EXPECT().CreateAccessKey(gomock.Any()).Return(&iam.CreateAccessKeyOutput{}, returnErr)
	mockAWSClient.EXPECT().CreateAccessKey(gomock.Any()).Return(&iam.CreateAccessKeyOutput{}, returnErr)
	mockAWSClient.EXPECT().CreateAccessKey(gomock.Any()).Return(&iam.CreateAccessKeyOutput{}, returnErr)
	mockAWSClient.EXPECT().CreateAccessKey(gomock.Any()).Return(&iam.CreateAccessKeyOutput{}, returnErr)
	mockAWSClient.EXPECT().CreateAccessKey(gomock.Any()).Return(&iam.CreateAccessKeyOutput{}, returnErr)

	// retries took long, need to mock it out
	old := DefaultDelay
	DefaultDelay = 0 * time.Second

	returnValue, err = CreateUserAccessKey(mockAWSClient, &user)
	assert.Equal(t, returnValue, &iam.CreateAccessKeyOutput{})
	assert.Error(t, err, returnErr)
	DefaultDelay = old
}

func TestBuildIAMUser(t *testing.T) {

	username := "AwesomeUser"
	namespace := "AwesomeNamespace"
	expectedSecretName := "awesomeuser-secret"

	// User has a valid secret created
	localObjects := []runtime.Object{
		CreateSecret(
			expectedSecretName,
			namespace,
			map[string][]byte{
				"one": []byte("hello"),
				"two": []byte("world"),
			},
		),
	}
	mocks := setupDefaultMocks(t, localObjects)

	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)
	mockAWSClient.EXPECT().GetUser(&iam.GetUserInput{
		UserName: aws.String(username),
	}).Return(&iam.GetUserOutput{
		User: &iam.User{
			UserName: &username,
		},
	}, nil)
	mockAWSClient.EXPECT().AttachUserPolicy(&iam.AttachUserPolicyInput{
		UserName:  &username,
		PolicyArn: aws.String(adminAccessArn),
	}).Return(&iam.AttachUserPolicyOutput{}, nil)

	r := ReconcileAccount{
		Client: mocks.fakeKubeClient,
		scheme: scheme.Scheme,
	}

	nullLogger := testutils.NullLogger{}
	account := newTestAccountBuilder().acct
	account.Name = username
	iamUserSecretName, err := r.BuildIAMUser(nullLogger, mockAWSClient, &account, username, namespace)
	assert.Equal(t, *iamUserSecretName, expectedSecretName)
	assert.Nil(t, err)
}

func TestCleanIAMRoles(t *testing.T) {
	mocks := setupDefaultMocks(t, []runtime.Object{})

	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)

	account := newTestAccountBuilder().acct

	expectedUsername := "ExpectedName"
	account.Name = expectedUsername

	expectedRoleName := aws.String("MyAwesomeRole")
	expectedRole := &iam.Role{
		RoleName: expectedRoleName,
		Arn:      aws.String("LookAtMyArnMyArnIsAmazing"),
		Tags: []*iam.Tag{
			// These tags are required to enter the deletion block
			{
				Key:   aws.String(v1alpha1.ClusterAccountNameTagKey),
				Value: aws.String(account.Name),
			},
			{
				Key:   aws.String(v1alpha1.ClusterNamespaceTagKey),
				Value: aws.String(account.Namespace),
			},
		},
	}

	mockAWSClient.EXPECT().ListRoles(gomock.Any()).Return(
		&iam.ListRolesOutput{
			Roles:       []*iam.Role{expectedRole},
			IsTruncated: aws.Bool(false),
		},
		nil,
	)
	mockAWSClient.EXPECT().GetRole(
		&iam.GetRoleInput{
			RoleName: expectedRoleName,
		},
	).Return(
		&iam.GetRoleOutput{
			Role: expectedRole,
		},
		nil,
	)

	expectedPolicyArn := "ExpectedPolicyArn"
	mockAWSClient.EXPECT().ListAttachedUserPolicies(
		&iam.ListAttachedUserPoliciesInput{UserName: &expectedUsername},
	).Return(
		&iam.ListAttachedUserPoliciesOutput{
			AttachedPolicies: []*iam.AttachedPolicy{
				{
					PolicyArn:  &expectedPolicyArn,
					PolicyName: aws.String("ExpectedPolicyName"),
				},
			},
		},
		nil,
	)
	mockAWSClient.EXPECT().DetachRolePolicy(
		&iam.DetachUserPolicyInput{
			UserName:  &expectedUsername,
			PolicyArn: &expectedPolicyArn,
		},
	).Return(nil, nil)

	mockAWSClient.EXPECT().ListAttachedRolePolicies(
		&iam.ListAttachedRolePoliciesInput{
			RoleName: expectedRoleName,
		},
	).Return(
		&iam.ListAttachedRolePoliciesOutput{
			AttachedPolicies: []*iam.AttachedPolicy{
				{
					PolicyArn:  &expectedPolicyArn,
					PolicyName: aws.String("ExpectedPolicyName"),
				},
			},
		},
		nil,
	)

	mockAWSClient.EXPECT().DetachRolePolicy(
		&iam.DetachRolePolicyInput{
			PolicyArn: &expectedPolicyArn,
			RoleName:  expectedRoleName,
		},
	).Return(nil, nil)

	mockAWSClient.EXPECT().DeleteRole(
		&iam.DeleteRoleInput{
			RoleName: expectedRoleName,
		},
	).Return(nil, nil)

	nullLogger := testutils.NullLogger{}

	err := cleanIAMRoles(nullLogger, mockAWSClient, &account)
	assert.Nil(t, err)
}

func TestRotateIAMAccessKeys(t *testing.T) {
	mocks := setupDefaultMocks(t, []runtime.Object{})
	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)

	expectedUsername := "ExpectedName"
	account := newTestAccountBuilder().acct
	account.Name = expectedUsername

	expectedAccessKeyId := "expectedAccessKeyID"

	r := ReconcileAccount{
		Client: mocks.fakeKubeClient,
		scheme: scheme.Scheme,
	}
	iamUser := iam.User{
		UserName: &expectedUsername,
	}
	nullLogger := testutils.NullLogger{}

	mockAWSClient.EXPECT().ListAccessKeys(
		&iam.ListAccessKeysInput{
			UserName: &expectedUsername,
		},
	).Return(
		&iam.ListAccessKeysOutput{
			AccessKeyMetadata: []*iam.AccessKeyMetadata{
				{
					AccessKeyId: &expectedAccessKeyId,
				},
			},
		},
		nil,
	)
	mockAWSClient.EXPECT().DeleteAccessKey(
		&iam.DeleteAccessKeyInput{
			AccessKeyId: &expectedAccessKeyId,
			UserName:    &expectedUsername,
		},
	).Return(
		&iam.DeleteAccessKeyOutput{},
		nil,
	)

	expectedAccessKeyOutput := &iam.CreateAccessKeyOutput{
		AccessKey: &iam.AccessKey{
			AccessKeyId: aws.String("MyAccessKeyID"),
		},
	}
	mockAWSClient.EXPECT().CreateAccessKey(
		&iam.CreateAccessKeyInput{
			UserName: iamUser.UserName,
		},
	).Return(
		expectedAccessKeyOutput,
		nil,
	)

	output, err := r.RotateIAMAccessKeys(nullLogger, mockAWSClient, &account, &iamUser)
	assert.Equal(t, output, expectedAccessKeyOutput)
	assert.Nil(t, err)
}

func TestDetachUserPolicies(t *testing.T) {
	mocks := setupDefaultMocks(t, []runtime.Object{})

	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)

	expectedUsername := "ExpectedName"
	iamUser := &iam.User{
		UserName: &expectedUsername,
	}

	expectedPolicyArn := "ExpectedPolicyArn"
	mockAWSClient.EXPECT().ListAttachedUserPolicies(
		&iam.ListAttachedUserPoliciesInput{UserName: &expectedUsername},
	).Return(
		&iam.ListAttachedUserPoliciesOutput{
			AttachedPolicies: []*iam.AttachedPolicy{
				{
					PolicyArn:  &expectedPolicyArn,
					PolicyName: aws.String("ExpectedPolicyName"),
				},
			},
		},
		nil,
	)
	mockAWSClient.EXPECT().DetachUserPolicy(
		&iam.DetachUserPolicyInput{
			UserName:  &expectedUsername,
			PolicyArn: &expectedPolicyArn,
		},
	).Return(
		nil, nil,
	)

	err := detachUserPolicies(mockAWSClient, iamUser)
	assert.Nil(t, err)
}

func TestDetachRolePolicies(t *testing.T) {

	mocks := setupDefaultMocks(t, []runtime.Object{})
	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)

	expectedRoleName := aws.String("MyAwesomeRole")
	expectedPolicyArn := "ExpectedPolicyArn"

	mockAWSClient.EXPECT().ListAttachedRolePolicies(
		&iam.ListAttachedRolePoliciesInput{
			RoleName: expectedRoleName,
		},
	).Return(
		&iam.ListAttachedRolePoliciesOutput{
			AttachedPolicies: []*iam.AttachedPolicy{
				{
					PolicyArn:  &expectedPolicyArn,
					PolicyName: aws.String("ExpectedPolicyName"),
				},
			},
		},
		nil,
	)

	mockAWSClient.EXPECT().DetachRolePolicy(
		&iam.DetachRolePolicyInput{
			PolicyArn: &expectedPolicyArn,
			RoleName:  expectedRoleName,
		},
	).Return(nil, nil)

	mockAWSClient.EXPECT().DeleteRole(
		&iam.DeleteRoleInput{
			RoleName: expectedRoleName,
		},
	).Return(nil, nil)

	err := detachRolePolicies(mockAWSClient, *expectedRoleName)
	assert.Nil(t, err)
}

func TestDoesSecretExist(t *testing.T) {
	localObjects := []runtime.Object{
		CreateSecret(
			"test",
			"namespace",
			map[string][]byte{
				"one": []byte("hello"),
				"two": []byte("world"),
			},
		),
	}
	mocks := setupDefaultMocks(t, localObjects)

	r := ReconcileAccount{
		Client: mocks.fakeKubeClient,
		scheme: scheme.Scheme,
	}

	namespace := types.NamespacedName{
		Namespace: "namespace",
		Name:      "test",
	}

	// Secret Found
	value, err := r.DoesSecretExist(namespace)
	assert.True(t, value)
	assert.Nil(t, err)

	// Secret not Found
	namespace.Name = "invalid"
	namespace.Namespace = "invalid"
	value, err = r.DoesSecretExist(namespace)
	assert.False(t, value)
	assert.Nil(t, err)
}

func TestCreateIAMUserSecretName(t *testing.T) {

	tests := []struct {
		name          string
		paramVal      string
		expectedValue string
	}{
		{
			name:          "English",
			paramVal:      "ThisIsMyAwesomeAccount",
			expectedValue: "thisismyawesomeaccount-secret",
		},
		{
			name:          "Non-English",
			paramVal:      "的不的",
			expectedValue: "的不的-secret",
		},
		{
			name:          "Empty",
			paramVal:      "",
			expectedValue: "-secret",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			value := createIAMUserSecretName(tt.paramVal)
			if value != tt.expectedValue {
				t.Errorf("[TestCreateIAMUserSecretName()] Got %v, wanted %v", value, tt.expectedValue)
			}
		})
	}
}
