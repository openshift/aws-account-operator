package account

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	corev1 "k8s.io/api/core/v1"
	"github.com/aws/smithy-go"
	"github.com/go-logr/logr"
	apis "github.com/openshift/aws-account-operator/api"
	"github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/awsclient/mock"
	"github.com/openshift/aws-account-operator/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
)

func init() {
	// Initialize Testing Defaults
	defaultSleepDelay = 0 * time.Millisecond
	defaultDelay = 0 * time.Second
	testSleepModifier = 0
}

func TestIAMCreateSecret(t *testing.T) {

	err := apis.AddToScheme(scheme.Scheme)
	if err != nil {
		fmt.Printf("failed adding to scheme in iam_test.go")
	}

	secret := CreateSecret(
		"test",
		TestAccountNamespace,
		map[string][]byte{
			"one": []byte("hello"),
			"two": []byte("world"),
		},
	)
	mocks := setupDefaultMocks(t, []runtime.Object{})

	// This is necessary for the mocks to report failures like methods not being called an expected number of times.
	// after mocks is defined
	defer mocks.mockCtrl.Finish()

	r := AccountReconciler{
		Client: mocks.fakeKubeClient,
		Scheme: scheme.Scheme,
	}

	nullLogger := testutils.NewTestLogger().Logger()
	account := newTestAccountBuilder().acct
	err = r.CreateSecret(nullLogger, &account, secret)
	assert.Nil(t, err)

	err = r.CreateSecret(nullLogger, &account, secret)
	assert.Error(t, err, "")
}

func TestRetryIfAwsServiceFailureOrInvalidToken(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		expectedValue bool
	}{
		{
			name:          "TestServiceFailure",
			err:           &smithy.GenericAPIError{Code: "ServiceFailure", Message: ""},
			expectedValue: true,
		},
		{
			name:          "TestInvalidClientTokenId",
			err:           &smithy.GenericAPIError{Code: "InvalidClientTokenId", Message: ""},
			expectedValue: true,
		},
		{
			name:          "TestAccessDenied",
			err:           &smithy.GenericAPIError{Code: "AccessDenied", Message: ""},
			expectedValue: true,
		},
		{
			name:          "TestNotFound",
			err:           &smithy.GenericAPIError{Code: "NotFound", Message: ""},
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
	user := iamtypes.User{UserName: &username}

	expectedAccessKeyID := aws.String("hihi")

	mockAWSClient.EXPECT().ListAccessKeys(gomock.Any(), gomock.Any()).Return(
		&iam.ListAccessKeysOutput{
			AccessKeyMetadata: []iamtypes.AccessKeyMetadata{
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
	returnErr := &smithy.GenericAPIError{Code: "AccessDenied", Message: ""}

	// Should retry 5 times
	mockAWSClient.EXPECT().ListAccessKeys(gomock.Any(), gomock.Any()).Return(nil, returnErr).Times(5)

	returnValue, err = listAccessKeys(mockAWSClient, &user)
	assert.Nil(t, returnValue)
	assert.Error(t, err, returnErr)
}

func TestDeleteAccessKey(t *testing.T) {
	mocks := setupDefaultMocks(t, []runtime.Object{})

	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)

	mockAWSClient.EXPECT().DeleteAccessKey(gomock.Any(), gomock.Any()).Return(
		&iam.DeleteAccessKeyOutput{},
		nil, // no error
	)

	accessKeyID := "accessKeyID"
	username := "username"

	deleteAccessKeyOutput, err := deleteAccessKey(mockAWSClient, &accessKeyID, &username)
	assert.Equal(t, deleteAccessKeyOutput, &iam.DeleteAccessKeyOutput{})
	assert.Nil(t, err)

	mockAWSClient = mock.NewMockClient(mocks.mockCtrl)
	returnErr := &smithy.GenericAPIError{Code: "AccessDenied", Message: ""}

	// Should retry 5 times
	mockAWSClient.EXPECT().DeleteAccessKey(gomock.Any(), gomock.Any()).Return(&iam.DeleteAccessKeyOutput{}, returnErr).Times(5)

	deleteAccessKeyOutput, err = deleteAccessKey(mockAWSClient, &accessKeyID, &username)
	assert.Equal(t, deleteAccessKeyOutput, &iam.DeleteAccessKeyOutput{})
	assert.Error(t, err, returnErr)
}

func TestDeleteAllAccessKeys(t *testing.T) {
	mocks := setupDefaultMocks(t, []runtime.Object{})

	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)
	username := "AwesomeUser"
	user := iamtypes.User{UserName: &username}

	expectedAccessKeyID := aws.String("expectedAccessKeyID")

	mockAWSClient.EXPECT().ListAccessKeys(gomock.Any(), &iam.ListAccessKeysInput{UserName: &username}).Return(
		&iam.ListAccessKeysOutput{
			AccessKeyMetadata: []iamtypes.AccessKeyMetadata{
				{
					AccessKeyId: expectedAccessKeyID,
				},
			},
		},
		nil, // no error
	)
	mockAWSClient.EXPECT().DeleteAccessKey(
		gomock.Any(),
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

	userID := aws.String("123456789")
	usernameStr := "MyUsername"
	username := aws.String(usernameStr)
	nullLogger := testutils.NewTestLogger().Logger()

	tests := []struct {
		name                     string
		setupAWSMock             func(r *mock.MockClientMockRecorder)
		expectedCreateUserOutput *iam.CreateUserOutput
		expectedErr              error
	}{
		{
			name: "Success",
			setupAWSMock: func(mc *mock.MockClientMockRecorder) {
				gomock.InOrder(
					mc.CreateUser(gomock.Any(), &iam.CreateUserInput{
						UserName: username,
					}).Return(
						&iam.CreateUserOutput{
							User: &iamtypes.User{
								UserId:   userID,
								UserName: username,
							},
						},
						nil, // no error
					),
				)
			},
			expectedCreateUserOutput: &iam.CreateUserOutput{
				User: &iamtypes.User{
					UserId:   userID,
					UserName: username,
				},
			},
			expectedErr: nil,
		},
		{
			name: "InvalidClientTokenId",
			setupAWSMock: func(mc *mock.MockClientMockRecorder) {
				gomock.InOrder(
					mc.CreateUser(gomock.Any(), &iam.CreateUserInput{
						UserName: username,
					}).Return(nil, &smithy.GenericAPIError{Code: "InvalidClientTokenId", Message: ""}).Times(9),
				)
			},
			expectedCreateUserOutput: &iam.CreateUserOutput{},
			expectedErr:              &smithy.GenericAPIError{Code: "InvalidClientTokenId", Message: ""},
		},
		{
			name: "AccessDenied",
			setupAWSMock: func(mc *mock.MockClientMockRecorder) {
				gomock.InOrder(
					mc.CreateUser(gomock.Any(), &iam.CreateUserInput{
						UserName: username,
					}).Return(nil, &smithy.GenericAPIError{Code: "AccessDenied", Message: ""}).Times(9),
				)
			},
			expectedCreateUserOutput: &iam.CreateUserOutput{},
			expectedErr:              &smithy.GenericAPIError{Code: "AccessDenied", Message: ""},
		},
		{
			name: "EntityAlreadyExists",
			setupAWSMock: func(mc *mock.MockClientMockRecorder) {
				gomock.InOrder(
					mc.CreateUser(gomock.Any(), &iam.CreateUserInput{
						UserName: username,
					}).Return(nil, &iamtypes.EntityAlreadyExistsException{Message: aws.String("")}),
				)
			},
			expectedCreateUserOutput: &iam.CreateUserOutput{},
			expectedErr:              &iamtypes.EntityAlreadyExistsException{Message: aws.String("")},
		},
		{
			name: "OtherErr",
			setupAWSMock: func(mc *mock.MockClientMockRecorder) {
				gomock.InOrder(
					mc.CreateUser(gomock.Any(), &iam.CreateUserInput{
						UserName: username,
					}).Return(nil, &smithy.GenericAPIError{Code: "OtherErr", Message: ""}),
				)
			},
			expectedCreateUserOutput: &iam.CreateUserOutput{},
			expectedErr:              &smithy.GenericAPIError{Code: "OtherErr", Message: ""},
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				mocks := setupDefaultMocks(t, []runtime.Object{})
				test.setupAWSMock(mocks.mockAWSClient.EXPECT())
				// This is necessary for the mocks to report failures like methods not being called an expected number of times.
				// after mocks is defined
				defer mocks.mockCtrl.Finish()

				createUserOutput, err := CreateIAMUser(nullLogger, mocks.mockAWSClient, usernameStr)
				assert.Equal(t, test.expectedCreateUserOutput, createUserOutput)
				assert.Equal(t, test.expectedErr, err)
			},
		)
	}
}

func TestAttachAdminUserPolicy(t *testing.T) {
	mocks := setupDefaultMocks(t, []runtime.Object{})

	username := "AwesomeUser"
	user := iamtypes.User{UserName: &username, Arn: aws.String("arn:aws:iam::1234567890:user/AwesomeUser")}
	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)

	// Testing valid state, returns with no issue.
	mockAWSClient.EXPECT().AttachUserPolicy(gomock.Any(), gomock.Any()).Return(
		&iam.AttachUserPolicyOutput{},
		nil, // no error
	)

	attachAdminUserPolicy, err := AttachAdminUserPolicy(mockAWSClient, &user)
	assert.Equal(t, attachAdminUserPolicy, &iam.AttachUserPolicyOutput{})
	assert.Nil(t, err)

	// Testing invalid state, returns error, retries up to 100 times.
	expectedError := &smithy.GenericAPIError{Code: "AccessDenied", Message: ""}
	mockAWSClient.EXPECT().AttachUserPolicy(gomock.Any(), gomock.Any()).Return(
		&iam.AttachUserPolicyOutput{},
		expectedError, // no error
	).Times(100)

	attachAdminUserPolicy, err = AttachAdminUserPolicy(mockAWSClient, &user)
	assert.Equal(t, attachAdminUserPolicy, &iam.AttachUserPolicyOutput{})
	assert.Equal(t, err, expectedError)
}

func TestAttachAndEnsureRolePolicies(t *testing.T) {

	nullLogger := testutils.NewTestLogger().Logger()
	mocks := setupDefaultMocks(t, []runtime.Object{})
	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)
	defer mocks.mockCtrl.Finish()

	managedSupRoleWithID := "RoleName-aabbcc"
	policyArn := "MyPolicyARN"

	mockAWSClient.EXPECT().AttachRolePolicy(gomock.Any(), &iam.AttachRolePolicyInput{
		RoleName:  aws.String(managedSupRoleWithID),
		PolicyArn: aws.String(policyArn),
	}).Return(nil, nil)

	mockAWSClient.EXPECT().ListAttachedRolePolicies(gomock.Any(), gomock.Any()).Return(
		&iam.ListAttachedRolePoliciesOutput{
			AttachedPolicies: []iamtypes.AttachedPolicy{
				{
					PolicyArn:  aws.String(policyArn),
					PolicyName: aws.String("PolicyName"),
				},
			},
		},
		nil,
	)

	err := attachAndEnsureRolePolicies(nullLogger, mockAWSClient, managedSupRoleWithID, policyArn)
	assert.Nil(t, err)
}

func TestCreateUserAccessKey(t *testing.T) {

	mocks := setupDefaultMocks(t, []runtime.Object{})

	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)
	username := "AwesomeUser"
	user := iamtypes.User{UserName: &username}

	expectedAccessKeyID := aws.String("expectedAccessKeyID")

	mockAWSClient.EXPECT().CreateAccessKey(
		gomock.Any(),
		&iam.CreateAccessKeyInput{
			UserName: aws.String(username),
		},
	).Return(
		&iam.CreateAccessKeyOutput{
			AccessKey: &iamtypes.AccessKey{
				AccessKeyId: expectedAccessKeyID,
			},
		},
		nil, // no error
	)

	returnValue, err := CreateUserAccessKey(mockAWSClient, &user)
	assert.Equal(t, returnValue.AccessKey.AccessKeyId, expectedAccessKeyID)
	assert.Nil(t, err)

	mockAWSClient = mock.NewMockClient(mocks.mockCtrl)
	returnErr := &smithy.GenericAPIError{Code: "AccessDenied", Message: ""}

	// Should retry 5 times
	mockAWSClient.EXPECT().CreateAccessKey(gomock.Any(), gomock.Any()).Return(&iam.CreateAccessKeyOutput{}, returnErr).Times(5)

	returnValue, err = CreateUserAccessKey(mockAWSClient, &user)
	assert.Equal(t, returnValue, &iam.CreateAccessKeyOutput{})
	assert.Error(t, err, returnErr)
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
	mockAWSClient.EXPECT().GetUser(gomock.Any(), &iam.GetUserInput{
		UserName: aws.String(username),
	}).Return(&iam.GetUserOutput{
		User: &iamtypes.User{
			UserName: &username,
			Arn:      aws.String("arn:aws:iam::1234567890:user/AwesomeUser"),
		},
	}, nil)
	mockAWSClient.EXPECT().AttachUserPolicy(gomock.Any(), &iam.AttachUserPolicyInput{
		UserName:  &username,
		PolicyArn: aws.String(strings.Join([]string{standardAdminAccessArnPrefix, adminAccessArnSuffix}, "")),
	}).Return(&iam.AttachUserPolicyOutput{}, nil)

	r := AccountReconciler{
		Client: mocks.fakeKubeClient,
		Scheme: scheme.Scheme,
	}

	nullLogger := testutils.NewTestLogger().Logger()
	account := newTestAccountBuilder().acct
	account.Name = username
	iamUserSecretName, err := r.BuildIAMUser(nullLogger, mockAWSClient, &account, username, namespace)
	assert.Equal(t, *iamUserSecretName, expectedSecretName)
	assert.Nil(t, err)
}

func TestDeleteIAMUser(t *testing.T) {
	nullLogger := testutils.NewTestLogger().Logger()
	mocks := setupDefaultMocks(t, []runtime.Object{})
	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)
	defer mocks.mockCtrl.Finish()

	mockAWSClient.EXPECT().ListAttachedUserPolicies(gomock.Any(), gomock.Any()).Return(
		&iam.ListAttachedUserPoliciesOutput{
			AttachedPolicies: []iamtypes.AttachedPolicy{},
		}, nil,
	)
	mockAWSClient.EXPECT().ListAccessKeys(gomock.Any(), gomock.Any()).Return(
		&iam.ListAccessKeysOutput{
			AccessKeyMetadata: []iamtypes.AccessKeyMetadata{},
		}, nil,
	)
	mockAWSClient.EXPECT().DeleteUser(gomock.Any(), &iam.DeleteUserInput{UserName: aws.String("MyUserName")}).Return(
		nil, nil,
	)

	user := iamtypes.User{UserName: aws.String("MyUserName")}

	err := deleteIAMUser(nullLogger, mockAWSClient, &user)
	assert.Nil(t, err)
}

func TestDeleteIAMUsers(t *testing.T) {
	err := apis.AddToScheme(scheme.Scheme)
	if err != nil {
		fmt.Printf("failed adding to scheme in iam_test.go")
	}

	nullLogger := testutils.NewTestLogger().Logger()
	mocks := setupDefaultMocks(t, []runtime.Object{})

	// This is necessary for the mocks to report failures like methods not being called an expected number of times.
	// after mocks is defined
	defer mocks.mockCtrl.Finish()

	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)
	username := aws.String("MyUserName")
	account := newTestAccountBuilder().acct
	account.Name = *username
	account.Namespace = "MyNamespace"
	mockAWSClient.EXPECT().GetUser(gomock.Any(), gomock.Any()).Return(
		&iam.GetUserOutput{
			User: &iamtypes.User{
				UserName: username,
				Tags:     getValidTags(&account),
			},
		}, nil,
	)

	// Copied expectations from TestDeleteIAMUser
	mockAWSClient.EXPECT().ListAttachedUserPolicies(gomock.Any(), gomock.Any()).Return(
		&iam.ListAttachedUserPoliciesOutput{
			AttachedPolicies: []iamtypes.AttachedPolicy{},
		}, nil,
	)
	mockAWSClient.EXPECT().ListAccessKeys(gomock.Any(), gomock.Any()).Return(
		&iam.ListAccessKeysOutput{
			AccessKeyMetadata: []iamtypes.AccessKeyMetadata{},
		}, nil,
	)
	mockAWSClient.EXPECT().DeleteUser(gomock.Any(), &iam.DeleteUserInput{UserName: username}).Return(
		nil, nil,
	)

	// Need to Monkey Patch awsclient.ListIAMUsers to return a list of users we define.
	old := listIAMUsers
	listIAMUsers = func(reqLogger logr.Logger, client awsclient.Client) ([]iamtypes.User, error) {
		return []iamtypes.User{{UserName: username}}, nil
	}

	err = DeleteIAMUsers(nullLogger, mockAWSClient, &account)
	listIAMUsers = old
	assert.Nil(t, err)
}

func getValidTags(account *v1alpha1.Account) []iamtypes.Tag {
	return []iamtypes.Tag{
		// These tags are required to enter the deletion block
		{
			Key:   aws.String(v1alpha1.ClusterAccountNameTagKey),
			Value: aws.String(account.Name),
		},
		{
			Key:   aws.String(v1alpha1.ClusterNamespaceTagKey),
			Value: aws.String(account.Namespace),
		},
	}
}

func TestCleanIAMRoles(t *testing.T) {
	mocks := setupDefaultMocks(t, []runtime.Object{})

	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)

	account := newTestAccountBuilder().acct

	expectedUsername := "ExpectedName"
	account.Name = expectedUsername

	expectedRoleName := aws.String("MyAwesomeRole")
	expectedRole := &iamtypes.Role{
		RoleName: expectedRoleName,
		Arn:      aws.String("LookAtMyArnMyArnIsAmazing"),
		Tags:     getValidTags(&account),
	}

	mockAWSClient.EXPECT().ListRoles(gomock.Any(), gomock.Any()).Return(
		&iam.ListRolesOutput{
			Roles:       []iamtypes.Role{*expectedRole},
			IsTruncated: false,
		},
		nil,
	)
	mockAWSClient.EXPECT().GetRole(
		gomock.Any(),
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
	mockAWSClient.EXPECT().ListAttachedRolePolicies(
		gomock.Any(),
		&iam.ListAttachedRolePoliciesInput{
			RoleName: expectedRoleName,
		},
	).Return(
		&iam.ListAttachedRolePoliciesOutput{
			AttachedPolicies: []iamtypes.AttachedPolicy{
				{
					PolicyArn:  &expectedPolicyArn,
					PolicyName: aws.String("ExpectedPolicyName"),
				},
			},
		},
		nil,
	)

	mockAWSClient.EXPECT().DetachRolePolicy(
		gomock.Any(),
		&iam.DetachRolePolicyInput{
			PolicyArn: &expectedPolicyArn,
			RoleName:  expectedRoleName,
		},
	).Return(nil, nil)

	mockAWSClient.EXPECT().DeleteRole(
		gomock.Any(),
		&iam.DeleteRoleInput{
			RoleName: expectedRoleName,
		},
	).Return(nil, nil)

	nullLogger := testutils.NewTestLogger().Logger()

	err := cleanIAMRoles(nullLogger, mockAWSClient, &account)
	assert.Nil(t, err)
}

func TestRotateIAMAccessKeys(t *testing.T) {
	mocks := setupDefaultMocks(t, []runtime.Object{})
	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)

	expectedUsername := "ExpectedName"
	account := newTestAccountBuilder().acct
	account.Name = expectedUsername

	oldKeyID := "oldAccessKeyID"
	newKeyID := "MyAccessKeyID"

	r := AccountReconciler{
		Client: mocks.fakeKubeClient,
		Scheme: scheme.Scheme,
	}
	iamUser := iamtypes.User{
		UserName: &expectedUsername,
	}
	nullLogger := testutils.NewTestLogger().Logger()

	gomock.InOrder(
		// 1. Count check: one existing key → under limit, rotation proceeds.
		mockAWSClient.EXPECT().ListAccessKeys(
			gomock.Any(),
			&iam.ListAccessKeysInput{UserName: &expectedUsername},
		).Return(
			&iam.ListAccessKeysOutput{
				AccessKeyMetadata: []iamtypes.AccessKeyMetadata{{AccessKeyId: &oldKeyID}},
			},
			nil,
		),
		// 2. Create new key first (create-before-delete).
		mockAWSClient.EXPECT().CreateAccessKey(
			gomock.Any(),
			&iam.CreateAccessKeyInput{UserName: iamUser.UserName},
		).Return(
			&iam.CreateAccessKeyOutput{
				AccessKey: &iamtypes.AccessKey{
					AccessKeyId: aws.String(newKeyID),
					UserName:    &expectedUsername,
				},
			},
			nil,
		),
		// 3. List again to find old keys to delete.
		mockAWSClient.EXPECT().ListAccessKeys(
			gomock.Any(),
			&iam.ListAccessKeysInput{UserName: &expectedUsername},
		).Return(
			&iam.ListAccessKeysOutput{
				AccessKeyMetadata: []iamtypes.AccessKeyMetadata{
					{AccessKeyId: &oldKeyID},
					{AccessKeyId: aws.String(newKeyID)},
				},
			},
			nil,
		),
		// 4. Delete only the old key; new key is preserved.
		mockAWSClient.EXPECT().DeleteAccessKey(
			gomock.Any(),
			&iam.DeleteAccessKeyInput{
				AccessKeyId: &oldKeyID,
				UserName:    &expectedUsername,
			},
		).Return(&iam.DeleteAccessKeyOutput{}, nil),
	)

	output, err := r.RotateIAMAccessKeys(nullLogger, mockAWSClient, &account, &iamUser)
	assert.Equal(t, aws.ToString(output.AccessKey.AccessKeyId), newKeyID)
	assert.Nil(t, err)
}

func TestRotateIAMAccessKeys_AtKeyLimit(t *testing.T) {
	mocks := setupDefaultMocks(t, []runtime.Object{})
	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)

	expectedUsername := "ExpectedName"
	account := newTestAccountBuilder().acct
	key1, key2 := "key1", "key2"

	r := AccountReconciler{Client: mocks.fakeKubeClient, Scheme: scheme.Scheme}
	iamUser := iamtypes.User{UserName: &expectedUsername}
	nullLogger := testutils.NewTestLogger().Logger()

	// User already has 2 keys — rotation must fail without creating a new one.
	mockAWSClient.EXPECT().ListAccessKeys(
		gomock.Any(),
		&iam.ListAccessKeysInput{UserName: &expectedUsername},
	).Return(
		&iam.ListAccessKeysOutput{
			AccessKeyMetadata: []iamtypes.AccessKeyMetadata{
				{AccessKeyId: &key1},
				{AccessKeyId: &key2},
			},
		},
		nil,
	)

	_, err := r.RotateIAMAccessKeys(nullLogger, mockAWSClient, &account, &iamUser)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already has 2")
}

func TestDetachUserPolicies(t *testing.T) {
	mocks := setupDefaultMocks(t, []runtime.Object{})

	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)

	expectedUsername := "ExpectedName"
	iamUser := &iamtypes.User{
		UserName: &expectedUsername,
	}

	expectedPolicyArn := "ExpectedPolicyArn"
	mockAWSClient.EXPECT().ListAttachedUserPolicies(
		gomock.Any(),
		&iam.ListAttachedUserPoliciesInput{UserName: &expectedUsername},
	).Return(
		&iam.ListAttachedUserPoliciesOutput{
			AttachedPolicies: []iamtypes.AttachedPolicy{
				{
					PolicyArn:  &expectedPolicyArn,
					PolicyName: aws.String("ExpectedPolicyName"),
				},
			},
		},
		nil,
	)
	mockAWSClient.EXPECT().DetachUserPolicy(
		gomock.Any(),
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
		gomock.Any(),
		&iam.ListAttachedRolePoliciesInput{
			RoleName: expectedRoleName,
		},
	).Return(
		&iam.ListAttachedRolePoliciesOutput{
			AttachedPolicies: []iamtypes.AttachedPolicy{
				{
					PolicyArn:  &expectedPolicyArn,
					PolicyName: aws.String("ExpectedPolicyName"),
				},
			},
		},
		nil,
	)

	mockAWSClient.EXPECT().DetachRolePolicy(
		gomock.Any(),
		&iam.DetachRolePolicyInput{
			PolicyArn: &expectedPolicyArn,
			RoleName:  expectedRoleName,
		},
	).Return(nil, nil)

	err := detachRolePolicies(mockAWSClient, *expectedRoleName)
	assert.Nil(t, err)
}

func TestCreateIAMUserSecret(t *testing.T) {
	err := apis.AddToScheme(scheme.Scheme)
	if err != nil {
		fmt.Printf("failed adding to scheme in iam_test.go")
	}

	nullLogger := testutils.NewTestLogger().Logger()
	mocks := setupDefaultMocks(t, []runtime.Object{})

	// This is necessary for the mocks to report failures like methods not being called an expected number of times.
	// after mocks is defined
	defer mocks.mockCtrl.Finish()

	r := AccountReconciler{
		Client: mocks.fakeKubeClient,
		Scheme: scheme.Scheme,
	}

	createAccessKeyOutput := iam.CreateAccessKeyOutput{
		AccessKey: &iamtypes.AccessKey{
			UserName:        aws.String("UserName"),
			AccessKeyId:     aws.String("AccessKeyId"),
			SecretAccessKey: aws.String("SecretAccessKey"),
		},
	}
	acct := newTestAccountBuilder().acct
	namespacedName := types.NamespacedName{
		Namespace: TestAccountNamespace,
		Name:      TestAccountName,
	}

	err = r.createIAMUserSecret(nullLogger, &acct, namespacedName, &createAccessKeyOutput)
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

	r := AccountReconciler{
		Client: mocks.fakeKubeClient,
		Scheme: scheme.Scheme,
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

func TestUpdateIAMUserSecret(t *testing.T) {
	err := apis.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Fatalf("failed adding to scheme: %v", err)
	}

	existingSecret := CreateSecret(
		"testaccount-secret",
		TestAccountNamespace,
		map[string][]byte{
			"aws_user_name":         []byte("old-user"),
			"aws_access_key_id":     []byte("OLD_KEY_ID"),
			"aws_secret_access_key": []byte("OLD_SECRET"),
		},
	)
	mocks := setupDefaultMocks(t, []runtime.Object{existingSecret})
	defer mocks.mockCtrl.Finish()

	r := AccountReconciler{
		Client: mocks.fakeKubeClient,
		Scheme: scheme.Scheme,
	}
	nullLogger := testutils.NewTestLogger().Logger()
	secretName := types.NamespacedName{Name: "testaccount-secret", Namespace: TestAccountNamespace}

	newKeyOutput := &iam.CreateAccessKeyOutput{
		AccessKey: &iamtypes.AccessKey{
			UserName:        aws.String("new-user"),
			AccessKeyId:     aws.String("NEW_KEY_ID"),
			SecretAccessKey: aws.String("NEW_SECRET"),
		},
	}

	err = r.updateIAMUserSecret(nullLogger, secretName, newKeyOutput)
	assert.Nil(t, err)

	// Verify the secret exists and contains the new credentials.
	exists, err := r.DoesSecretExist(secretName)
	assert.Nil(t, err)
	assert.True(t, exists)

	updatedSecret := &corev1.Secret{}
	getErr := r.Get(context.TODO(), secretName, updatedSecret)
	assert.Nil(t, getErr)
	assert.Equal(t, "new-user", string(updatedSecret.Data["aws_user_name"]))
	assert.Equal(t, "NEW_KEY_ID", string(updatedSecret.Data["aws_access_key_id"]))
	assert.Equal(t, "NEW_SECRET", string(updatedSecret.Data["aws_secret_access_key"]))
}

func TestUpdateIAMUserSecret_SecretNotFound(t *testing.T) {
	err := apis.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Fatalf("failed adding to scheme: %v", err)
	}

	mocks := setupDefaultMocks(t, []runtime.Object{})
	defer mocks.mockCtrl.Finish()

	r := AccountReconciler{
		Client: mocks.fakeKubeClient,
		Scheme: scheme.Scheme,
	}
	nullLogger := testutils.NewTestLogger().Logger()
	secretName := types.NamespacedName{Name: "nonexistent-secret", Namespace: TestAccountNamespace}

	newKeyOutput := &iam.CreateAccessKeyOutput{
		AccessKey: &iamtypes.AccessKey{
			UserName:        aws.String("user"),
			AccessKeyId:     aws.String("KEY_ID"),
			SecretAccessKey: aws.String("SECRET"),
		},
	}

	err = r.updateIAMUserSecret(nullLogger, secretName, newKeyOutput)
	assert.Error(t, err)
}
