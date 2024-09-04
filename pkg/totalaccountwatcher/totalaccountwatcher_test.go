package totalaccountwatcher

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/organizations"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	mockAWS "github.com/openshift/aws-account-operator/pkg/awsclient/mock"
	"github.com/openshift/aws-account-operator/pkg/localmetrics"
	"github.com/openshift/aws-account-operator/pkg/testutils"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		fakeKubeClient: fakekubeclient.NewClientBuilder().WithRuntimeObjects(localObjects...).Build(),
		mockCtrl:       gomock.NewController(t),
	}

	mocks.mockAWSClient = mockAWS.NewMockClient(mocks.mockCtrl)
	return mocks
}

func TestAccountWatcherCreation(t *testing.T) {
	t.Run("Tests Account Watcher Creation", func(t *testing.T) {
		// Arrange
		mocks := setupDefaultMocks(t, []runtime.Object{})

		// This is necessary for the mocks to report failures like methods not being called an expected number of times.
		// after mocks is defined
		defer mocks.mockCtrl.Finish()

		totalAccountWatcher := newTotalAccountWatcher(mocks.fakeKubeClient, mocks.mockAWSClient, 10)
		totalAccountWatcher.awsClient = mocks.mockAWSClient

		if totalAccountWatcher.AccountsCanBeCreated() {
			t.Error("Account Should Not be able to be created by default")
		}
	})
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
							{Name: aws.String("test1"), Status: aws.String(organizations.AccountStatusActive)},
							{Name: aws.String("test2"), Status: aws.String(organizations.AccountStatusActive)},
						},
					},
					nil).Times(1)
				r.ListCreateAccountStatus(gomock.Any()).Return(
					&organizations.ListCreateAccountStatusOutput{},
					nil).Times(1)
			},
			validateTotal: func(t *testing.T, total int) {
				assert.Equal(t, 2, total)
			}}, {
			name:          "3 accounts total 2 are ACTIVE and 1 is SUSPENDED",
			errorExpected: false,
			setupAWSMock: func(r *mockAWS.MockClientMockRecorder) {
				r.ListAccounts(gomock.Any()).Return(
					&organizations.ListAccountsOutput{
						Accounts: []*organizations.Account{
							{Name: aws.String("test1"), Status: aws.String(organizations.AccountStatusActive)},
							{Name: aws.String("test2"), Status: aws.String(organizations.AccountStatusActive)},
							{Name: aws.String("test3"), Status: aws.String(organizations.AccountStatusSuspended)},
						},
					},
					nil).Times(1)
				r.ListCreateAccountStatus(gomock.Any()).Return(
					&organizations.ListCreateAccountStatusOutput{},
					nil).Times(1)
			},
			validateTotal: func(t *testing.T, total int) {
				assert.Equal(t, 2, total)
			}},
		{
			name:          "4 accounts total 2 ACTIVE and 2 CREATING-IN_PROGRESS",
			errorExpected: false,
			setupAWSMock: func(r *mockAWS.MockClientMockRecorder) {
				r.ListAccounts(gomock.Any()).Return(
					&organizations.ListAccountsOutput{
						Accounts: []*organizations.Account{
							{Name: aws.String("test1"), Status: aws.String(organizations.AccountStatusActive)},
							{Name: aws.String("test2"), Status: aws.String(organizations.AccountStatusActive)},
						},
					},
					nil).Times(1)
				r.ListCreateAccountStatus(gomock.Any()).Return(
					&organizations.ListCreateAccountStatusOutput{
						CreateAccountStatuses: []*organizations.CreateAccountStatus{
							{AccountName: aws.String("testA"), State: aws.String(organizations.CreateAccountStateInProgress)},
							{AccountName: aws.String("testB"), State: aws.String(organizations.CreateAccountStateInProgress)},
						},
					},
					nil).Times(1)
			},
			validateTotal: func(t *testing.T, total int) {
				assert.Equal(t, 4, total)
			}},
		{
			name:          "AccountList with NextToken, return 4 Active accounts",
			errorExpected: false,
			setupAWSMock: func(r *mockAWS.MockClientMockRecorder) {
				gomock.InOrder(
					r.ListAccounts(gomock.Any()).Return(
						&organizations.ListAccountsOutput{
							NextToken: aws.String("NextToken"),
							Accounts: []*organizations.Account{
								{Name: aws.String("test1"), Status: aws.String(organizations.AccountStatusActive)},
								{Name: aws.String("test2"), Status: aws.String(organizations.AccountStatusActive)},
							}},
						nil).Times(1),
					r.ListAccounts(gomock.Any()).Return(
						&organizations.ListAccountsOutput{
							Accounts: []*organizations.Account{
								{Name: aws.String("test2"), Status: aws.String(organizations.AccountStatusActive)},
								{Name: aws.String("test3"), Status: aws.String(organizations.AccountStatusActive)},
							}},
						nil).Times(1),
				)
				r.ListCreateAccountStatus(gomock.Any()).Return(
					&organizations.ListCreateAccountStatusOutput{},
					nil).Times(1)
			},
			validateTotal: func(t *testing.T, total int) {
				assert.Equal(t, 4, total)
			},
		},
		{
			name:          "AccountList with NextToken, return 6 accounts total 4 Active and 2 Suspend",
			errorExpected: false,
			setupAWSMock: func(r *mockAWS.MockClientMockRecorder) {
				gomock.InOrder(
					r.ListAccounts(gomock.Any()).Return(
						&organizations.ListAccountsOutput{
							NextToken: aws.String("NextToken"),
							Accounts: []*organizations.Account{
								{Name: aws.String("test1"), Status: aws.String(organizations.AccountStatusActive)},
								{Name: aws.String("test2"), Status: aws.String(organizations.AccountStatusSuspended)},
							}},
						nil).Times(1),
					r.ListAccounts(gomock.Any()).Return(
						&organizations.ListAccountsOutput{
							Accounts: []*organizations.Account{
								{Name: aws.String("test2"), Status: aws.String(organizations.AccountStatusActive)},
								{Name: aws.String("test3"), Status: aws.String(organizations.AccountStatusActive)},
								{Name: aws.String("test4"), Status: aws.String(organizations.AccountStatusSuspended)},
								{Name: aws.String("test5"), Status: aws.String(organizations.AccountStatusActive)},
							}},
						nil).Times(1),
				)
				r.ListCreateAccountStatus(gomock.Any()).Return(
					&organizations.ListCreateAccountStatusOutput{},
					nil).Times(1)
			},
			validateTotal: func(t *testing.T, total int) {
				assert.Equal(t, 4, total)
			},
		},
		{
			name:          "AccountCreatingList with NextToken, 2 ACTIVE and 4 CREATING-IN_PROGRESS",
			errorExpected: false,
			setupAWSMock: func(r *mockAWS.MockClientMockRecorder) {
				r.ListAccounts(gomock.Any()).Return(
					&organizations.ListAccountsOutput{
						Accounts: []*organizations.Account{
							{Name: aws.String("test1"), Status: aws.String(organizations.AccountStatusActive)},
							{Name: aws.String("test2"), Status: aws.String(organizations.AccountStatusActive)},
						},
					},
					nil).Times(1)
				gomock.InOrder(
					r.ListCreateAccountStatus(gomock.Any()).Return(
						&organizations.ListCreateAccountStatusOutput{
							NextToken: aws.String("NextToken"),
							CreateAccountStatuses: []*organizations.CreateAccountStatus{
								{AccountName: aws.String("testA"), State: aws.String(organizations.CreateAccountStateInProgress)},
								{AccountName: aws.String("testB"), State: aws.String(organizations.CreateAccountStateInProgress)},
							},
						},
						nil).Times(1),
					r.ListCreateAccountStatus(gomock.Any()).Return(
						&organizations.ListCreateAccountStatusOutput{
							CreateAccountStatuses: []*organizations.CreateAccountStatus{
								{AccountName: aws.String("testC"), State: aws.String(organizations.CreateAccountStateInProgress)},
								{AccountName: aws.String("testD"), State: aws.String(organizations.CreateAccountStateInProgress)},
							},
						},
						nil).Times(1),
				)
			},
			validateTotal: func(t *testing.T, total int) {
				assert.Equal(t, 6, total)
			}},
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
			TotalAccountWatcher = newTotalAccountWatcher(mocks.fakeKubeClient, mocks.mockAWSClient, 10)
			total, err := TotalAccountWatcher.getTotalAwsAccounts()

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

// This tests our accountLimitReached function
func TestAccountLimitsReached(t *testing.T) {
	tests := []struct {
		name      string
		limit     string
		testCount int
		expected  bool
	}{
		{
			name:      "Test Limit 5 Current 1",
			limit:     "5",
			testCount: 1,
			expected:  false,
		},
		{
			name:      "Test Limit 5 Current 5",
			limit:     "5",
			testCount: 5,
			expected:  true,
		},
		{
			name:      "Test Limit 5 Current 6",
			limit:     "5",
			testCount: 6,
			expected:  true,
		},
	}

	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      awsv1alpha1.DefaultConfigMap,
						Namespace: awsv1alpha1.AccountCrNamespace,
					},
					Data: map[string]string{
						"account-limit": test.limit,
					},
				}
				objs := []runtime.Object{configMap}
				mocks := setupDefaultMocks(t, objs)
				nullLogger := testutils.NewTestLogger().Logger()
				taw := newTotalAccountWatcher(mocks.fakeKubeClient, mocks.mockAWSClient, 10)

				result, _ := taw.accountLimitReached(nullLogger, test.testCount)

				if result != test.expected {
					t.Error(
						"Expected", test.expected,
						"got:", result,
						"limit:", test.limit,
						"accountCount:", test.testCount,
					)
				}
			},
		)
	}
}

func TestTotalAccountsUpdate(t *testing.T) {
	tests := []struct {
		name         string
		expected     bool
		configMap    corev1.ConfigMap
		expectErr    bool
		setupAWSMock func(r *mockAWS.MockClientMockRecorder)
	}{
		{
			name:      "Test Cannot get ConfigMap",
			expected:  false,
			expectErr: true,
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "TheWrongConfigMap",
					Namespace: awsv1alpha1.AccountCrNamespace,
				},
			},
			setupAWSMock: func(r *mockAWS.MockClientMockRecorder) {
				r.ListAccounts(gomock.Any()).Return(
					&organizations.ListAccountsOutput{
						Accounts: []*organizations.Account{
							{Name: aws.String("test1"), Status: aws.String(organizations.AccountStatusActive)},
						}},
					nil)
				r.ListCreateAccountStatus(gomock.Any()).Return(
					&organizations.ListCreateAccountStatusOutput{},
					nil).Times(1)
			},
		},
		{
			name:      "Test fail to convert string to int",
			expected:  false,
			expectErr: true,
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      awsv1alpha1.DefaultConfigMap,
					Namespace: awsv1alpha1.AccountCrNamespace,
				},
				Data: map[string]string{
					"account-limit": "alskdjf",
				},
			},
			setupAWSMock: func(r *mockAWS.MockClientMockRecorder) {
				r.ListAccounts(gomock.Any()).Return(
					&organizations.ListAccountsOutput{
						Accounts: []*organizations.Account{
							{Name: aws.String("test1"), Status: aws.String(organizations.AccountStatusActive)},
						}},
					nil)
				r.ListCreateAccountStatus(gomock.Any()).Return(
					&organizations.ListCreateAccountStatusOutput{},
					nil).Times(1)
			},
		},
		{
			name:      "Fail to get account-limit key",
			expected:  false,
			expectErr: true,
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      awsv1alpha1.DefaultConfigMap,
					Namespace: awsv1alpha1.AccountCrNamespace,
				},
				Data: map[string]string{
					"randomKey": "alskdjf",
				},
			},
			setupAWSMock: func(r *mockAWS.MockClientMockRecorder) {
				r.ListAccounts(gomock.Any()).Return(
					&organizations.ListAccountsOutput{
						Accounts: []*organizations.Account{
							{Name: aws.String("test1"), Status: aws.String(organizations.AccountStatusActive)},
						}},
					nil)
				r.ListCreateAccountStatus(gomock.Any()).Return(
					&organizations.ListCreateAccountStatusOutput{},
					nil).Times(1)
			},
		},
		{
			name:      "Fail AWS Error",
			expected:  false,
			expectErr: true,
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      awsv1alpha1.DefaultConfigMap,
					Namespace: awsv1alpha1.AccountCrNamespace,
				},
				Data: map[string]string{
					"account-limit": "4950",
				},
			},
			setupAWSMock: func(r *mockAWS.MockClientMockRecorder) {
				r.ListAccounts(gomock.Any()).Return(
					&organizations.ListAccountsOutput{
						Accounts: []*organizations.Account{
							{Name: aws.String("test1")},
						}},
					errors.New("FakeError")).Times(1)
			},
		},
		{
			name:      "Returns lower Limit than Current Accounts",
			expected:  false,
			expectErr: false,
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      awsv1alpha1.DefaultConfigMap,
					Namespace: awsv1alpha1.AccountCrNamespace,
				},
				Data: map[string]string{
					"account-limit": "1",
				},
			},
			setupAWSMock: func(r *mockAWS.MockClientMockRecorder) {
				r.ListAccounts(gomock.Any()).Return(
					&organizations.ListAccountsOutput{
						Accounts: []*organizations.Account{
							{Name: aws.String("test1"), Status: aws.String(organizations.AccountStatusActive)},
							{Name: aws.String("test2"), Status: aws.String(organizations.AccountStatusActive)},
						}},
					nil)
				r.ListCreateAccountStatus(gomock.Any()).Return(
					&organizations.ListCreateAccountStatusOutput{},
					nil).Times(1)
			},
		},
		{
			name:      "Returns higher Limit than Current Accounts",
			expected:  true,
			expectErr: false,
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      awsv1alpha1.DefaultConfigMap,
					Namespace: awsv1alpha1.AccountCrNamespace,
				},
				Data: map[string]string{
					"account-limit": "5",
				},
			},
			setupAWSMock: func(r *mockAWS.MockClientMockRecorder) {
				r.ListAccounts(gomock.Any()).Return(
					&organizations.ListAccountsOutput{
						Accounts: []*organizations.Account{
							{Name: aws.String("test1"), Status: aws.String(organizations.AccountStatusActive)},
						}},
					nil)
				r.ListCreateAccountStatus(gomock.Any()).Return(
					&organizations.ListCreateAccountStatusOutput{},
					nil).Times(1)
			},
		},
		{
			name:      "Returns Limit above Current Accounts but below Current+Creating Accounts",
			expected:  false,
			expectErr: false,
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      awsv1alpha1.DefaultConfigMap,
					Namespace: awsv1alpha1.AccountCrNamespace,
				},
				Data: map[string]string{
					"account-limit": "3",
				},
			},
			setupAWSMock: func(r *mockAWS.MockClientMockRecorder) {
				r.ListAccounts(gomock.Any()).Return(
					&organizations.ListAccountsOutput{
						Accounts: []*organizations.Account{
							{Name: aws.String("test1"), Status: aws.String(organizations.AccountStatusActive)},
							{Name: aws.String("test2"), Status: aws.String(organizations.AccountStatusActive)},
						}},
					nil)
				r.ListCreateAccountStatus(gomock.Any()).Return(
					&organizations.ListCreateAccountStatusOutput{
						CreateAccountStatuses: []*organizations.CreateAccountStatus{
							{AccountName: aws.String("testA"), State: aws.String(organizations.CreateAccountStateInProgress)},
							{AccountName: aws.String("testB"), State: aws.String(organizations.CreateAccountStateInProgress)},
						},
					},
					nil).Times(1)
			},
		},
		{
			name:      "Returns Limit above Current+Creating Accounts",
			expected:  true,
			expectErr: false,
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      awsv1alpha1.DefaultConfigMap,
					Namespace: awsv1alpha1.AccountCrNamespace,
				},
				Data: map[string]string{
					"account-limit": "10",
				},
			},
			setupAWSMock: func(r *mockAWS.MockClientMockRecorder) {
				r.ListAccounts(gomock.Any()).Return(
					&organizations.ListAccountsOutput{
						Accounts: []*organizations.Account{
							{Name: aws.String("test1"), Status: aws.String(organizations.AccountStatusActive)},
							{Name: aws.String("test2"), Status: aws.String(organizations.AccountStatusActive)},
						}},
					nil)
				r.ListCreateAccountStatus(gomock.Any()).Return(
					&organizations.ListCreateAccountStatusOutput{
						CreateAccountStatuses: []*organizations.CreateAccountStatus{
							{AccountName: aws.String("testA"), State: aws.String(organizations.CreateAccountStateInProgress)},
						},
					},
					nil).Times(1)
			},
		},
	}

	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				localmetrics.Collector = localmetrics.NewMetricsCollector(nil)

				objs := []runtime.Object{&test.configMap} // #nosec G601
				mocks := setupDefaultMocks(t, objs)
				test.setupAWSMock(mocks.mockAWSClient.EXPECT())
				nullLogger := testutils.NewTestLogger().Logger()
				defer mocks.mockCtrl.Finish()

				TotalAccountWatcher = newTotalAccountWatcher(mocks.fakeKubeClient, mocks.mockAWSClient, 10)
				TotalAccountWatcher.awsClient = mocks.mockAWSClient
				err := TotalAccountWatcher.UpdateTotalAccounts(nullLogger)

				if test.expectErr && err == nil {
					t.Error(
						"Expected an error",
					)
				}

				if TotalAccountWatcher.AccountsCanBeCreated() != test.expected {
					t.Error(
						"got:", TotalAccountWatcher.AccountsCanBeCreated(),
						"expected:", test.expected,
					)
				}
			},
		)
	}
}
