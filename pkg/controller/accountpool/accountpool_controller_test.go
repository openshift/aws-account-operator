package accountpool

import (
	"reflect"

	"github.com/golang/mock/gomock"
	awsaccountapis "github.com/openshift/aws-account-operator/pkg/apis"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/localmetrics"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"context"
	"testing"
)

type mocks struct {
	fakeKubeClient client.Client
	mockCtrl       *gomock.Controller
}

// create fake client to mock API calls
func newTestReconciler() *ReconcileAccountPool {
	return &ReconcileAccountPool{
		client: fake.NewFakeClient(),
		scheme: scheme.Scheme,
	}
}

// setupDefaultMocks is an easy way to setup all of the default mocks
func setupDefaultMocks(t *testing.T, localObjects []runtime.Object) *mocks {
	mocks := &mocks{
		fakeKubeClient: fake.NewFakeClient(localObjects...),
		mockCtrl:       gomock.NewController(t),
	}

	return mocks
}

func TestReconcileAccountPool(t *testing.T) {
	awsaccountapis.AddToScheme(scheme.Scheme)
	localmetrics.Collector = localmetrics.NewMetricsCollector(nil)
	tests := []struct {
		name                  string
		localObjects          []runtime.Object
		expectedAccountPool   awsv1alpha1.AccountPool
		verifyAccountFunction func(client.Client, *awsv1alpha1.AccountPool) bool
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
				&awsv1alpha1.Account{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "account1",
						Namespace: "aws-account-operator",
					},
					Spec: awsv1alpha1.AccountSpec{
						AwsAccountID:  "000000",
						IAMUserSecret: "secret",
						ClaimLink:     "claim",
						LegalEntity: awsv1alpha1.LegalEntity{
							ID:   "",
							Name: "",
						},
					},
					Status: awsv1alpha1.AccountStatus{
						Claimed:           false,
						SupportCaseID:     "000000",
						Conditions:        []awsv1alpha1.AccountCondition{},
						State:             "Ready",
						RotateCredentials: false,
					},
				},
				&awsv1alpha1.Account{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "account2",
						Namespace: "aws-account-operator",
					},
					Spec: awsv1alpha1.AccountSpec{
						AwsAccountID:  "000000",
						IAMUserSecret: "secret",
						ClaimLink:     "claim",
						LegalEntity: awsv1alpha1.LegalEntity{
							ID:   "",
							Name: "",
						},
					},
					Status: awsv1alpha1.AccountStatus{
						Claimed:           false,
						SupportCaseID:     "000000",
						Conditions:        []awsv1alpha1.AccountCondition{},
						State:             "Ready",
						RotateCredentials: false,
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
					UnclaimedAccounts: 2,
				},
			},
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
			verifyAccountFunction: verifyAccountCreated,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Arrange
			mocks := setupDefaultMocks(t, test.localObjects)
			// This is necessary for the mocks to report failures like methods not being called an expected number of times.
			// after mocks is defined
			defer mocks.mockCtrl.Finish()

			rap := &ReconcileAccountPool{
				client: mocks.fakeKubeClient,
				scheme: scheme.Scheme,
			}

			ap := awsv1alpha1.AccountPool{}
			err := mocks.fakeKubeClient.Get(context.TODO(), types.NamespacedName{Name: "test", Namespace: "test"}, &ap)

			_, err = rap.Reconcile(reconcile.Request{
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
		return false
	}

	if !reflect.DeepEqual(ap, *expected) {
		return false
	}

	return true
}

func verifyAccountCreated(c client.Client, expected *awsv1alpha1.AccountPool) bool {

	listOps := &client.ListOptions{Namespace: expected.Namespace}
	al := awsv1alpha1.AccountList{}

	err := c.List(context.TODO(), listOps, &al)

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

	if !reflect.DeepEqual(ap, *expected) {
		return false
	}

	return true
}

func TestUpdateAccountPoolStatus(t *testing.T) {

	testAccountPoolCR := awsv1alpha1.AccountPool{
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

	//Case where spec and status poolsize are not equal
	//Expect true
	if !updateAccountPoolStatus(&testAccountPoolCR, 1, 1) {
		t.Error("AccountPool size in spec and status don't match, but AccountPool not updated")
	}

	//Update the spec so the first case is skipped
	testAccountPoolCR.Spec.PoolSize = 2

	//Case where AccountPool status unclaimed accounts and actual unclaimed accounts are different
	//Expect true
	if !(updateAccountPoolStatus(&testAccountPoolCR, 2, 1)) {
		t.Error("AccountPool status UnclaimedAccounts does not equal unclaimed accounts, but AccountPool not updated")
	}

	//Case where AccountPool status claimed accounts and actual claimed accounts are different
	//Expect true
	if !(updateAccountPoolStatus(&testAccountPoolCR, 1, 2)) {
		t.Error("AccountPool status ClaimedAccounts does not equal claimed accounts, but AccountPool not updated")
	}

	//Case where AccountPool does not need to be updated
	//Expect false
	if updateAccountPoolStatus(&testAccountPoolCR, 1, 1) {
		t.Error("AccountPool status doesn't need updating, but function returns true")
	}
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}
