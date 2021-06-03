package account

import (
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/golang/mock/gomock"
	"github.com/openshift/aws-account-operator/pkg/apis"
	"github.com/openshift/aws-account-operator/pkg/awsclient/mock"
	"github.com/openshift/aws-account-operator/pkg/controller/testutils"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/kubectl/pkg/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type mocks struct {
	fakeKubeClient client.Client
	mockCtrl       *gomock.Controller
	mockAWSClient  *mock.MockClient
}

// setupDefaultMocks is an easy way to setup all of the default mocks
func setupDefaultMocks(t *testing.T, localObjects []runtime.Object) *mocks {
	mocks := &mocks{
		fakeKubeClient: fake.NewFakeClient(localObjects...),
		mockCtrl:       gomock.NewController(t),
	}

	return mocks
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

	r := ReconcileAccount{
		Client: mocks.fakeKubeClient,
		scheme: scheme.Scheme,
	}

	nullLogger := testutils.NullLogger{}
	account := newTestAccountBuilder().acct
	err = r.CreateSecret(nullLogger, &account, secret)
	assert.Nil(t, err)

	/* Can't run a second time ->

	err = r.CreateSecret(nullLogger, &account, secret)
	assert.Nil(t, err)


	Expected nil, but got: &runtime.notRegisteredErr{
		schemeName:"pkg/runtime/scheme.go:101",
		gvk:schema.GroupVersionKind{Group:"", Version:"", Kind:""},
		target:runtime.GroupVersioner(nil), t:(*reflect.rtype)(0x148ad20)}
	*/
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
	//err :=
	//value := retryIfAwsServiceFailureOrInvalidToken()
}

func TestListAccessKeys(t *testing.T) {

}

func TestDeleteAccessKey(t *testing.T) {

}

func TestDeleteAllAccessKeys(t *testing.T) {

}

func TestCreateIAMUser(t *testing.T) {

}

func TestAttachAdminUserPolicy(t *testing.T) {

}

func TestCreateUserAccessKey(t *testing.T) {

}

func TestBuildIAMUser(t *testing.T) {

}

func TestCleanUpIAM(t *testing.T) {

}

func TestDeleteIAMUsers(t *testing.T) {

}

func TestCleanIAMRoles(t *testing.T) {

}

func TestRotateIAMAccessKeys(t *testing.T) {

}

func TestDetachUserPolicies(t *testing.T) {

}

func TestDetachRolePolicies(t *testing.T) {
	// test all AccountConditionType values

	/*
		var a Account
		tests := []struct {
			name                 string
			accountConditionType string
			expectedResult       bool
		}{
			{
				name:                 "AccountCreating",
				accountConditionType: string(AccountCreating),
				expectedResult:       false,
			},
		}
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				a.Status.State = tt.accountConditionType
				if got := a.IsFailed(); got != tt.expectedResult {
					t.Errorf("[Account.IsFailed()] Got %v, want %v for state %s", got, tt.expectedResult, tt.accountConditionType)
				}
			})
		}
	*/
}

func TestCreateIAMUserSecret(t *testing.T) {

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

func TestIsIAMUserOsdManagedAdminSRE(t *testing.T) {
	value := "osdManagedAdminSRE-username"
	assert.True(t, isIAMUserOsdManagedAdminSRE(&value))

	value = "osdManagedAdminSRE"
	assert.True(t, isIAMUserOsdManagedAdminSRE(&value))

	value = "osdmanagedadminsre" // case sensitive
	assert.False(t, isIAMUserOsdManagedAdminSRE(&value))

	value = ""
	assert.False(t, isIAMUserOsdManagedAdminSRE(&value))
}

func TestCreateIAMUserSecretName(t *testing.T) {
	value := createIAMUserSecretName("ThisIsMyAwesomeAccount")
	assert.EqualValues(t, value, "thisismyawesomeaccount-secret")

	value = createIAMUserSecretName("的不的")
	assert.EqualValues(t, value, "的不的-secret")

	value = createIAMUserSecretName("")
	assert.EqualValues(t, value, "-secret")
}
