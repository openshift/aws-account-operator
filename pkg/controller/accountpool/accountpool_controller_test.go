package accountpool

import (
	//"fmt"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes/scheme"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"testing"
)

// create fake client to mock API calls
func newTestReconciler() *ReconcileAccountPool {
	return &ReconcileAccountPool{
		client: fake.NewFakeClient(),
		scheme: scheme.Scheme,
	}
}

func TestReconcileAccountPool(t *testing.T) {

}

func TestUpdateAccountPoolStatus(t *testing.T) {

	testAccountPoolCR := awsv1alpha1.AccountPool{
		ObjectMeta: metav1.ObjectMeta{
			//The string is the expeted output of rand given the seed
			Name:      "test'",
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

func TestNewAccountForCR(t *testing.T) {

	//Seed with a specific number to ensure output is predictable
	rand.Seed(1)

	//Generate CR with namespace "test"
	testAccountCR := newAccountForCR("test")

	//Create a CR with the expected output
	expectedAccountCR := awsv1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			//The string is the expeted output of rand given the seed
			Name:      emailID + "-xn8fgg",
			Namespace: "test",
		},
		Spec: awsv1alpha1.AccountSpec{
			AwsAccountID:  "",
			IAMUserSecret: "",
			ClaimLink:     "",
		},
	}

	//Ensure the two are equal
	if !reflect.DeepEqual(*testAccountCR, expectedAccountCR) {
		t.Errorf("Generated Account CR does not match the expected output\n")
		t.Errorf("\nExpected: %+v\n Actual: %+v\n", expectedAccountCR, testAccountCR)
	}
}

func TestAddFinalizers(t *testing.T) {

	//Create a CR with the expected output
	testAccountCR := awsv1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			//The string is the expeted output of rand given the seed
			Name:      emailID,
			Namespace: "test",
		},
		Spec: awsv1alpha1.AccountSpec{
			AwsAccountID:  "",
			IAMUserSecret: "",
			ClaimLink:     "",
		},
	}

	addFinalizer(&testAccountCR, "finalizer.aws.managed.openshift.io")
	finalizers := testAccountCR.GetFinalizers()

	if !stringInSlice("finalizer.aws.managed.openshift.io", finalizers) {
		t.Error()
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
