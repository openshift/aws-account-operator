package account

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/go-logr/logr"
	"github.com/golang/mock/gomock"
	apis "github.com/openshift/aws-account-operator/api"
	"github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/awsclient/mock"
	"github.com/openshift/aws-account-operator/pkg/testutils"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestGetSTSCredentials(t *testing.T) {

	mocks := setupDefaultMocks(t, []runtime.Object{})
	nullLogger := testutils.NewTestLogger().Logger()
	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)
	defer mocks.mockCtrl.Finish()

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

	// Test AWS Failure
	expectedErr := awserr.New("AccessDenied", "", nil)
	mockAWSClient.EXPECT().AssumeRole(gomock.Any()).Return(
		&sts.AssumeRoleOutput{
			Credentials: &sts.Credentials{
				AccessKeyId:     AccessKeyId,
				Expiration:      Expiration,
				SecretAccessKey: SecretAccessKey,
				SessionToken:    SessionToken,
			},
		},
		expectedErr,
	).Times(100)

	creds, err = getSTSCredentials(
		nullLogger,
		mockAWSClient,
		"",
		"",
		"",
	)
	assert.Error(t, err, expectedErr)
	assert.Equal(t, creds, &sts.AssumeRoleOutput{})
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
	mockAWSClient.EXPECT().ListAccessKeys(gomock.Any()).Return(nil, returnErr).Times(5)

	returnValue, err = listAccessKeys(mockAWSClient, &user)
	assert.Nil(t, returnValue)
	assert.Error(t, err, returnErr)
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
	mockAWSClient.EXPECT().DeleteAccessKey(gomock.Any()).Return(&iam.DeleteAccessKeyOutput{}, returnErr).Times(5)

	deleteAccessKeyOutput, err = deleteAccessKey(mockAWSClient, &accessKeyID, &username)
	assert.Equal(t, deleteAccessKeyOutput, &iam.DeleteAccessKeyOutput{})
	assert.Error(t, err, returnErr)
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
					mc.CreateUser(&iam.CreateUserInput{
						UserName: username,
					}).Return(
						&iam.CreateUserOutput{
							User: &iam.User{
								UserId:   userID,
								UserName: username,
							},
						},
						nil, // no error
					),
				)
			},
			expectedCreateUserOutput: &iam.CreateUserOutput{
				User: &iam.User{
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
					mc.CreateUser(&iam.CreateUserInput{
						UserName: username,
					}).Return(nil, awserr.New("InvalidClientTokenId", "", nil)).Times(9),
				)
			},
			expectedCreateUserOutput: &iam.CreateUserOutput{},
			expectedErr:              awserr.New("InvalidClientTokenId", "", nil),
		},
		{
			name: "AccessDenied",
			setupAWSMock: func(mc *mock.MockClientMockRecorder) {
				gomock.InOrder(
					mc.CreateUser(&iam.CreateUserInput{
						UserName: username,
					}).Return(nil, awserr.New("AccessDenied", "", nil)).Times(9),
				)
			},
			expectedCreateUserOutput: &iam.CreateUserOutput{},
			expectedErr:              awserr.New("AccessDenied", "", nil),
		},
		{
			name: "EntityAlreadyExists",
			setupAWSMock: func(mc *mock.MockClientMockRecorder) {
				gomock.InOrder(
					mc.CreateUser(&iam.CreateUserInput{
						UserName: username,
					}).Return(nil, awserr.New(iam.ErrCodeEntityAlreadyExistsException, "", nil)),
				)
			},
			expectedCreateUserOutput: &iam.CreateUserOutput{},
			expectedErr:              awserr.New(iam.ErrCodeEntityAlreadyExistsException, "", nil),
		},
		{
			name: "OtherErr",
			setupAWSMock: func(mc *mock.MockClientMockRecorder) {
				gomock.InOrder(
					mc.CreateUser(&iam.CreateUserInput{
						UserName: username,
					}).Return(nil, awserr.New("OtherErr", "", nil)),
				)
			},
			expectedCreateUserOutput: &iam.CreateUserOutput{},
			expectedErr:              awserr.New("OtherErr", "", nil),
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
	user := iam.User{UserName: &username, Arn: aws.String("arn:aws:iam::1234567890:user/AwesomeUser")}
	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)

	// Testing valid state, returns with no issue.
	mockAWSClient.EXPECT().AttachUserPolicy(gomock.Any()).Return(
		&iam.AttachUserPolicyOutput{},
		nil, // no error
	)

	attachAdminUserPolicy, err := AttachAdminUserPolicy(mockAWSClient, &user)
	assert.Equal(t, attachAdminUserPolicy, &iam.AttachUserPolicyOutput{})
	assert.Nil(t, err)

	// Testing invalid state, returns error, retries up to 100 times.
	expectedError := awserr.New("AccessDenied", "", nil)
	mockAWSClient.EXPECT().AttachUserPolicy(gomock.Any()).Return(
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

	mockAWSClient.EXPECT().AttachRolePolicy(&iam.AttachRolePolicyInput{
		RoleName:  aws.String(managedSupRoleWithID),
		PolicyArn: aws.String(policyArn),
	}).Return(nil, nil)

	mockAWSClient.EXPECT().ListAttachedRolePolicies(gomock.Any()).Return(
		&iam.ListAttachedRolePoliciesOutput{
			AttachedPolicies: []*iam.AttachedPolicy{
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
	mockAWSClient.EXPECT().CreateAccessKey(gomock.Any()).Return(&iam.CreateAccessKeyOutput{}, returnErr).Times(5)

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
	mockAWSClient.EXPECT().GetUser(&iam.GetUserInput{
		UserName: aws.String(username),
	}).Return(&iam.GetUserOutput{
		User: &iam.User{
			UserName: &username,
			Arn:      aws.String("arn:aws:iam::1234567890:user/AwesomeUser"),
		},
	}, nil)
	mockAWSClient.EXPECT().AttachUserPolicy(&iam.AttachUserPolicyInput{
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

	mockAWSClient.EXPECT().ListAttachedUserPolicies(gomock.Any()).Return(
		&iam.ListAttachedUserPoliciesOutput{
			AttachedPolicies: []*iam.AttachedPolicy{},
		}, nil,
	)
	mockAWSClient.EXPECT().ListAccessKeys(gomock.Any()).Return(
		&iam.ListAccessKeysOutput{
			AccessKeyMetadata: []*iam.AccessKeyMetadata{},
		}, nil,
	)
	mockAWSClient.EXPECT().DeleteUser(&iam.DeleteUserInput{UserName: aws.String("MyUserName")}).Return(
		nil, nil,
	)

	user := iam.User{UserName: aws.String("MyUserName")}

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
	mockAWSClient.EXPECT().GetUser(gomock.Any()).Return(
		&iam.GetUserOutput{
			User: &iam.User{
				UserName: username,
				Tags:     getValidTags(&account),
			},
		}, nil,
	)

	// Copied expectations from TestDeleteIAMUser
	mockAWSClient.EXPECT().ListAttachedUserPolicies(gomock.Any()).Return(
		&iam.ListAttachedUserPoliciesOutput{
			AttachedPolicies: []*iam.AttachedPolicy{},
		}, nil,
	)
	mockAWSClient.EXPECT().ListAccessKeys(gomock.Any()).Return(
		&iam.ListAccessKeysOutput{
			AccessKeyMetadata: []*iam.AccessKeyMetadata{},
		}, nil,
	)
	mockAWSClient.EXPECT().DeleteUser(&iam.DeleteUserInput{UserName: username}).Return(
		nil, nil,
	)

	// Need to Monkey Patch awsclient.ListIAMUsers to return a list of users we define.
	old := listIAMUsers
	listIAMUsers = func(reqLogger logr.Logger, client awsclient.Client) ([]*iam.User, error) {
		return []*iam.User{{UserName: username}}, nil
	}

	err = deleteIAMUsers(nullLogger, mockAWSClient, &account)
	listIAMUsers = old
	assert.Nil(t, err)
}

func getValidTags(account *v1alpha1.Account) []*iam.Tag {
	return []*iam.Tag{
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
	expectedRole := &iam.Role{
		RoleName: expectedRoleName,
		Arn:      aws.String("LookAtMyArnMyArnIsAmazing"),
		Tags:     getValidTags(&account),
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

	expectedAccessKeyId := "expectedAccessKeyID"

	r := AccountReconciler{
		Client: mocks.fakeKubeClient,
		Scheme: scheme.Scheme,
	}
	iamUser := iam.User{
		UserName: &expectedUsername,
	}
	nullLogger := testutils.NewTestLogger().Logger()

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
		AccessKey: &iam.AccessKey{
			UserName:        aws.String("UserName"),
			AccessKeyId:     aws.String("AccessKeyId"),
			SecretAccessKey: aws.String("SecretAccessKey"),
		},
	}
	acct := newTestAccountBuilder().acct
	namespacedName := types.NamespacedName{
		Namespace: "namespace",
		Name:      "test",
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

func TestValidateIAMSecret(t *testing.T) {

	username := "AwesomeUser"
	namespace := "AwesomeNamespace"
	expectedSecretName := "awesomeuser-secret"
	expectedAccessKeyID := "expectedAccessKey"
	iamUser := iam.User{
		UserName: &username,
	}

	err := apis.AddToScheme(scheme.Scheme)
	if err != nil {
		fmt.Printf("failed adding to scheme in iam_test.go")
	}

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
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      expectedSecretName,
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
	mockAWSClient.EXPECT().ListAccessKeys(
		&iam.ListAccessKeysInput{
			UserName: &username,
		},
	).Return(
		&iam.ListAccessKeysOutput{
			AccessKeyMetadata: []*iam.AccessKeyMetadata{
				{
					AccessKeyId: &expectedAccessKeyID,
				},
			},
		},
		nil,
	)
	mockAWSClient.EXPECT().DeleteAccessKey(
		&iam.DeleteAccessKeyInput{
			AccessKeyId: &expectedAccessKeyID,
			UserName:    &username,
		},
	).Return(
		&iam.DeleteAccessKeyOutput{},
		nil,
	)

	expectedAccessKeyOutput := &iam.CreateAccessKeyOutput{
		AccessKey: &iam.AccessKey{
			UserName:        &username,
			AccessKeyId:     aws.String("NewAccessKeyID"),
			SecretAccessKey: aws.String("NewSecret"),
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
	mockAWSClient.EXPECT().AttachUserPolicy(&iam.AttachUserPolicyInput{
		UserName:  &username,
		PolicyArn: aws.String("adminAccessArn"),
	}).Return(&iam.AttachUserPolicyOutput{}, nil)

	r := AccountReconciler{
		Client: mocks.fakeKubeClient,
		Scheme: scheme.Scheme,
	}

	nullLogger := testutils.NewTestLogger().Logger()
	account := newTestAccountBuilder().acct
	account.Name = username
	err = r.updateIAMUserSecret(nullLogger, &account, namespacedName, expectedAccessKeyOutput)
	assert.Nil(t, err)
	err = r.ValidateIAMSecret(nullLogger, mockAWSClient, &account, username, namespacedName)
	assert.Nil(t, err)
}

func TestIsKubeSecretValid(t *testing.T) {

	//username := "AwesomeUser"
	namespace := "AwesomeNamespace"
	expectedSecretName := "awesomeuser-secret"
	err := apis.AddToScheme(scheme.Scheme)
	if err != nil {
		fmt.Printf("failed adding to scheme in iam_test.go")
	}

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

	account := v1alpha1.Account{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Labels:     map[string]string{},
			Finalizers: []string{},
			CreationTimestamp: metav1.Time{
				Time: time.Now().Add(-(5 * time.Minute)), // default tests to 5 minute old acct
			},
			Namespace: namespace,
		},
		Status: v1alpha1.AccountStatus{
			State:   string(v1alpha1.AccountReady),
			Claimed: false,
		},
		Spec: v1alpha1.AccountSpec{
			IAMUserSecret: expectedSecretName,
		},
	}

	mocks := setupDefaultMocks(t, localObjects)

	mockIBuilder := mock.NewMockIBuilder(mocks.mockCtrl)

	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)

	r := AccountReconciler{
		Client:           mocks.fakeKubeClient,
		Scheme:           scheme.Scheme,
		awsClientBuilder: mockIBuilder,
	}

	mockIBuilder.EXPECT().GetClient(controllerName,
		r.Client,
		awsclient.NewAwsClientInput{
			SecretName: account.Spec.IAMUserSecret,
			NameSpace:  account.Namespace,
			AwsRegion:  "us-east-1",
		}).Return(mockAWSClient, nil)

	mockAWSClient.EXPECT().GetCallerIdentity(gomock.Any()).Return(&sts.GetCallerIdentityOutput{}, nil)

	nullLogger := testutils.NewTestLogger().Logger()
	val, err := r.IsKubeSecretValid(nullLogger, &account)
	assert.Equal(t, val, true)
	assert.Nil(t, err)
}
