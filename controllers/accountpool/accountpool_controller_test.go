package accountpool

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	awsaccountapis "github.com/openshift/aws-account-operator/api"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/localmetrics"
)

type mocks struct {
	fakeKubeClient client.Client
	mockCtrl       *gomock.Controller
}

type mockTAW struct {
	accounts int
	limit    int
}

func (s *mockTAW) GetAccountCount() int {
	return s.accounts
}
func (s *mockTAW) GetLimit() int {
	return s.limit
}

// setupDefaultMocks is an easy way to setup all of the default mocks
func setupDefaultMocks(t *testing.T, localObjects []runtime.Object) *mocks {
	mocks := &mocks{
		fakeKubeClient: fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(localObjects...).Build(),
		mockCtrl:       gomock.NewController(t),
	}

	return mocks
}

const (
	unclaimed = false
	claimed   = true
)

func createAccountMock(name string, state string, claimed bool) *awsv1alpha1.Account {
	leID := ""
	if claimed {
		leID = "12345"
	}
	return &awsv1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "aws-account-operator",
			OwnerReferences: []metav1.OwnerReference{
				metav1.OwnerReference{Kind: "AccountPool"},
			},
		},
		Spec: awsv1alpha1.AccountSpec{
			AwsAccountID:  "000000",
			IAMUserSecret: "secret",
			ClaimLink:     "claim",
			LegalEntity: awsv1alpha1.LegalEntity{
				ID:   leID,
				Name: "",
			},
		},
		Status: awsv1alpha1.AccountStatus{
			Claimed:           claimed,
			SupportCaseID:     "000000",
			Conditions:        []awsv1alpha1.AccountCondition{},
			State:             state,
			RotateCredentials: false,
		},
	}

}

func TestReconcileAccountPool(t *testing.T) {
	err := awsaccountapis.AddToScheme(scheme.Scheme)
	if err != nil {
		fmt.Printf("failed adding to scheme in accountpoot_controller_test.go")
	}

	localmetrics.Collector = localmetrics.NewMetricsCollector(nil)
	tests := []struct {
		name                  string
		localObjects          []runtime.Object
		expectedAccountPool   awsv1alpha1.AccountPool
		verifyAccountFunction func(client.Client, *awsv1alpha1.AccountPool) bool
		expectedAWSCount      int
		expectedLimit         int
	}{
		{
			name: "Account count >= Pool Size",
			localObjects: []runtime.Object{
				&awsv1alpha1.AccountPool{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "aws-account-operator",
					},
					Spec: awsv1alpha1.AccountPoolSpec{
						PoolSize: 1,
					},
					Status: awsv1alpha1.AccountPoolStatus{
						PoolSize:          1,
						UnclaimedAccounts: 2,
					},
				},
				createAccountMock("account1", "Ready", unclaimed),
				createAccountMock("account2", "Ready", unclaimed),
			},
			expectedAccountPool: awsv1alpha1.AccountPool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "aws-account-operator",
				},
				Spec: awsv1alpha1.AccountPoolSpec{
					PoolSize: 1,
				},
				Status: awsv1alpha1.AccountPoolStatus{
					PoolSize:          1,
					UnclaimedAccounts: 2,
					AvailableAccounts: 2,
				},
			},
			expectedAWSCount:      2,
			expectedLimit:         2,
			verifyAccountFunction: verifyAccountPool,
		},
		{
			name: "Account count < Pool Size",
			localObjects: []runtime.Object{
				&awsv1alpha1.AccountPool{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "aws-account-operator",
					},
					Spec: awsv1alpha1.AccountPoolSpec{
						PoolSize: 1,
					},
					Status: awsv1alpha1.AccountPoolStatus{
						PoolSize:          1,
						UnclaimedAccounts: 0,
					},
				},
			},
			expectedAccountPool: awsv1alpha1.AccountPool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "aws-account-operator",
				},
				Spec: awsv1alpha1.AccountPoolSpec{
					PoolSize: 1,
				},
				Status: awsv1alpha1.AccountPoolStatus{
					PoolSize:          1,
					UnclaimedAccounts: 1,
				},
			},
			expectedAWSCount:      1,
			expectedLimit:         1,
			verifyAccountFunction: verifyAccountCreated,
		},
		{
			name: "TestAccountStatusCounter",
			localObjects: []runtime.Object{
				&awsv1alpha1.AccountPool{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "aws-account-operator",
					},
					Spec: awsv1alpha1.AccountPoolSpec{
						PoolSize: 1,
					},
				},
				createAccountMock("account1", "Ready", unclaimed),
				createAccountMock("account2", "InitializingRegions", unclaimed),
				createAccountMock("account3", "PendingVerification", unclaimed),
				createAccountMock("account4", "Failed", unclaimed),
				createAccountMock("account5", "Ready", claimed),
			},
			expectedAccountPool: awsv1alpha1.AccountPool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "aws-account-operator",
				},
				Spec: awsv1alpha1.AccountPoolSpec{
					PoolSize: 1,
				},
				Status: awsv1alpha1.AccountPoolStatus{
					PoolSize:            1,
					UnclaimedAccounts:   3,
					ClaimedAccounts:     1,
					AvailableAccounts:   1,
					AccountsProgressing: 2,
					AWSLimitDelta:       1,
				},
			},
			expectedAWSCount:      5,
			expectedLimit:         6,
			verifyAccountFunction: verifyAccountPool,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Arrange
			mocks := setupDefaultMocks(t, test.localObjects)
			// This is necessary for the mocks to report failures like methods not being called an expected number of times.
			// after mocks is defined
			defer mocks.mockCtrl.Finish()

			rap := &AccountPoolReconciler{
				Client: mocks.fakeKubeClient,
				Scheme: scheme.Scheme,
				accountWatcher: &mockTAW{
					accounts: test.expectedAWSCount,
					limit:    test.expectedLimit,
				},
			}

			ap := awsv1alpha1.AccountPool{}
			err := mocks.fakeKubeClient.Get(
				context.TODO(),
				types.NamespacedName{
					Name:      "test",
					Namespace: "aws-account-operator",
				},
				&ap,
			)
			if err != nil {
				fmt.Printf("Failed returning mock accountPool in accountpool controller tests: %s\n", err)
			}

			_, err = rap.Reconcile(context.TODO(), reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test",
					Namespace: "aws-account-operator",
				},
			})

			assert.NoError(t, err, "Unexpected Error")
			assert.True(t, test.verifyAccountFunction(mocks.fakeKubeClient, &test.expectedAccountPool))
		})
	}
}

func verifyAccountPool(c client.Client, expected *awsv1alpha1.AccountPool) bool {

	ap := awsv1alpha1.AccountPool{}
	err := c.Get(context.TODO(), types.NamespacedName{Name: expected.Name, Namespace: expected.Namespace}, &ap)

	if err != nil {
		fmt.Printf("Error returning fakeclient accountPool: %s\n", err)
		return false
	}

	if !reflect.DeepEqual(ap.Status, expected.Status) {
		fmt.Printf("Error comparing accountPool Status objects.\n\tExpected: %+v\n\tGot: %+v", expected.Status, ap.Status)
		return false
	}

	return true
}

func verifyAccountCreated(c client.Client, expected *awsv1alpha1.AccountPool) bool {

	listOpts := []client.ListOption{
		client.InNamespace(expected.Namespace),
	}
	al := awsv1alpha1.AccountList{}

	err := c.List(context.TODO(), &al, listOpts...)

	if err != nil {
		return false
	}

	unclaimedAccountCount := 0
	for _, account := range al.Items {
		// We don't want to count reused accounts here, filter by LegalEntity.ID
		if account.Status.Claimed == false && account.Spec.LegalEntity.ID == "" {
			if account.Status.State != "Failed" {
				unclaimedAccountCount++
			}
		}
	}

	ap := awsv1alpha1.AccountPool{}
	err = c.Get(context.TODO(), types.NamespacedName{Name: expected.Name, Namespace: expected.Namespace}, &ap)

	if err != nil {
		return false
	}

	ap.Status.UnclaimedAccounts = unclaimedAccountCount

	return reflect.DeepEqual(ap.Status, expected.Status)
}

func TestUpdateAccountPoolStatus(t *testing.T) {

	testAccountPoolCR := &awsv1alpha1.AccountPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: awsv1alpha1.AccountPoolSpec{
			PoolSize: 3,
		},
		Status: awsv1alpha1.AccountPoolStatus{
			PoolSize:          2,
			UnclaimedAccounts: 1,
			ClaimedAccounts:   1,
		},
	}

	testAccountStatus := awsv1alpha1.AccountPoolStatus{
		PoolSize:          3,
		UnclaimedAccounts: 1,
		ClaimedAccounts:   1,
	}

	//Case where spec and status poolsize are not equal
	//Expect true
	if !shouldUpdateAccountPoolStatus(testAccountPoolCR, testAccountStatus) {
		t.Error("AccountPool size in spec and status don't match, but AccountPool not updated")
	}

	//Update the status so the first case is skipped
	testAccountPoolCR.Status.PoolSize = 3
	// Change the unclaimed accounts
	testAccountStatus.UnclaimedAccounts = 2

	//Case where AccountPool status unclaimed accounts and actual unclaimed accounts are different
	//Expect true
	if !(shouldUpdateAccountPoolStatus(testAccountPoolCR, testAccountStatus)) {
		t.Error("AccountPool status UnclaimedAccounts does not equal unclaimed accounts, but AccountPool not updated")
	}

	testAccountStatus.UnclaimedAccounts = 1
	testAccountStatus.ClaimedAccounts = 2

	//Case where AccountPool status claimed accounts and actual claimed accounts are different
	//Expect true
	if !(shouldUpdateAccountPoolStatus(testAccountPoolCR, testAccountStatus)) {
		t.Error("AccountPool status ClaimedAccounts does not equal claimed accounts, but AccountPool not updated")
	}

	testAccountStatus.ClaimedAccounts = 1

	//Case where AccountPool does not need to be updated
	//Expect false
	if shouldUpdateAccountPoolStatus(testAccountPoolCR, testAccountStatus) {
		t.Error("AccountPool status doesn't need updating, but function returns true")
	}
}
