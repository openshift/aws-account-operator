package totalaccountwatcher

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/aws/aws-sdk-go/service/organizations"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/golang/mock/gomock"
	mockAWS "github.com/openshift/aws-account-operator/pkg/awsclient/mock"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fakekubeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type mocks struct {
	fakeKubeClient client.Client
	mockCtrl       *gomock.Controller
	mockAWSClient  *mockAWS.MockClient
}

// setupDefaultMocks is an easy way to setup all of the default mocks
func setupDefaultMocks(t *testing.T, localObjects []runtime.Object) *mocks {
	mocks := &mocks{
		fakeKubeClient: fakekubeclient.NewFakeClient(localObjects...),
		mockCtrl:       gomock.NewController(t),
	}

	mocks.mockAWSClient = mockAWS.NewMockClient(mocks.mockCtrl)
	return mocks
}

func TestTotalAwsAccounts(t *testing.T) {
	tests := []struct {
		name string
		//localObjects []runtime.Object
		setupAWSMock   func(r *mockAWS.MockClientMockRecorder)
		errorExpected  bool
		validateErrors func(*testing.T, int, error)
		validateTotal  func(*testing.T, int)
	}{
		{
			name:          "Error Path",
			errorExpected: true,
			setupAWSMock: func(r *mockAWS.MockClientMockRecorder) {
				r.ListAccounts(gomock.Any()).Return(
					&organizations.ListAccountsOutput{
						Accounts: []*organizations.Account{
							{Name: aws.String("test1")},
						}},
					errors.New("FakeError")).Times(1)
			},
			validateErrors: func(t *testing.T, total int, err error) {
				assert.Equal(t, 0, total)
				assert.Equal(t, err, errors.New("Error getting a list of accounts"))
			}},
		{
			name:          "2 accounts returned",
			errorExpected: false,
			setupAWSMock: func(r *mockAWS.MockClientMockRecorder) {
				r.ListAccounts(gomock.Any()).Return(
					&organizations.ListAccountsOutput{
						Accounts: []*organizations.Account{
							{Name: aws.String("test1")},
							{Name: aws.String("test2")},
						},
					},
					nil).Times(1)
			},
			validateTotal: func(t *testing.T, total int) {
				assert.Equal(t, 2, total)
			}},
		{
			name:          "AccountList with NextToken, return 4 accounts",
			errorExpected: false,
			setupAWSMock: func(r *mockAWS.MockClientMockRecorder) {
				gomock.InOrder(
					r.ListAccounts(gomock.Any()).Return(
						&organizations.ListAccountsOutput{
							NextToken: aws.String("NextToken"),
							Accounts: []*organizations.Account{
								{Name: aws.String("test1")},
								{Name: aws.String("test2")},
							}},
						nil).Times(1),
					r.ListAccounts(gomock.Any()).Return(
						&organizations.ListAccountsOutput{
							Accounts: []*organizations.Account{
								{Name: aws.String("test2")},
								{Name: aws.String("test3")},
							}},
						nil).Times(1),
				)
			},
			validateTotal: func(t *testing.T, total int) {
				assert.Equal(t, 4, total)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Arrange
			mocks := setupDefaultMocks(t, []runtime.Object{})
			test.setupAWSMock(mocks.mockAWSClient.EXPECT())

			// This is necessary for the mocks to report failures like methods not being called an expected number of times.
			// after mocks is defined
			defer mocks.mockCtrl.Finish()

			// Act
			TotalAccountWatcher = NewTotalAccountWatcher(mocks.fakeKubeClient, mocks.mockAWSClient, 10)
			TotalAccountWatcher.AwsClient = mocks.mockAWSClient
			total, err := TotalAwsAccounts()

			// Assert
			if test.errorExpected {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			// validate
			if test.validateErrors != nil {
				test.validateErrors(t, total, err)
			}

			if test.validateTotal != nil {
				test.validateTotal(t, total)
			}
		})
	}
}
