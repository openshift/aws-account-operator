package accountclaim

import (
	"context"
	"reflect"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	awsaccountapis "github.com/openshift/aws-account-operator/pkg/apis"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type mocks struct {
	fakeKubeClient client.Client
	mockCtrl       *gomock.Controller
}

// create fake client to mock API calls
func newTestReconciler() *ReconcileAccountClaim {
	return &ReconcileAccountClaim{
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

func TestReconcileAccountClaim(t *testing.T) {
	awsaccountapis.AddToScheme(scheme.Scheme)
	tests := []struct {
		name                  string
		localObjects          []runtime.Object
		expectedAccountClaim  awsv1alpha1.AccountClaim
		verifyAccountFunction func(client.Client, *awsv1alpha1.AccountClaim) bool
	}{
		{
			name: "Placeholder",
			localObjects: []runtime.Object{
				&awsv1alpha1.AccountClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "claim-test",
					},
				},
			},
			expectedAccountClaim: awsv1alpha1.AccountClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "claim-test",
				},
			},
			verifyAccountFunction: verifyAccountClaim,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Arrange
			mocks := setupDefaultMocks(t, test.localObjects)
			// This is necessary for the mocks to report failures like methods not being called an expected number of times.
			// after mocks is defined
			defer mocks.mockCtrl.Finish()

			rap := &ReconcileAccountClaim{
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
			assert.True(t, test.verifyAccountFunction(mocks.fakeKubeClient, &test.expectedAccountClaim))
		})
	}
}

func verifyAccountClaim(c client.Client, expected *awsv1alpha1.AccountClaim) bool {

	ap := awsv1alpha1.AccountClaim{}
	err := c.Get(context.TODO(), types.NamespacedName{Name: expected.Name, Namespace: expected.Namespace}, &ap)

	if err != nil {
		return false
	}

	if !reflect.DeepEqual(ap, *expected) {
		return false
	}

	return true
}
