package account

import (
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/organizations"
	"github.com/golang/mock/gomock"
	"github.com/openshift/aws-account-operator/pkg/apis"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient/mock"
	"github.com/openshift/aws-account-operator/pkg/controller/testutils"
	"github.com/openshift/aws-account-operator/pkg/controller/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

type testAccountBuilder struct {
	acct awsv1alpha1.Account
}

type mocks struct {
	fakeKubeClient client.Client
	mockCtrl       *gomock.Controller
	mockAWSClient  *mock.MockClient
}

func setupDefaultMocks(t *testing.T, localObjects []runtime.Object) *mocks {
	mocks := &mocks{
		fakeKubeClient: fake.NewFakeClient(localObjects...),
		mockCtrl:       gomock.NewController(t),
	}

	mocks.mockAWSClient = mock.NewMockClient(mocks.mockCtrl)

	return mocks
}

func (t *testAccountBuilder) GetTestAccount() *awsv1alpha1.Account {
	return &t.acct
}

func newTestAccountBuilder() *testAccountBuilder {
	return &testAccountBuilder{
		acct: awsv1alpha1.Account{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Labels:     map[string]string{},
				Finalizers: []string{},
				CreationTimestamp: metav1.Time{
					Time: time.Now().Add(-(5 * time.Minute)), // default tests to 5 minute old acct
				},
			},
			Status: awsv1alpha1.AccountStatus{
				State:   string(awsv1alpha1.AccountReady),
				Claimed: false,
			},
			Spec: awsv1alpha1.AccountSpec{},
		},
	}
}

// Just set the whole TypeMeta all in one go
func (t *testAccountBuilder) WithTypetMeta(tm metav1.TypeMeta) *testAccountBuilder {
	t.acct.TypeMeta = tm
	return t
}

// Just set the whole ObjectMeta all in one go
func (t *testAccountBuilder) WithObjectMeta(objm metav1.ObjectMeta) *testAccountBuilder {
	t.acct.ObjectMeta = objm
	return t
}

// Just set the whole Status all in one go
func (t *testAccountBuilder) WithStatus(status awsv1alpha1.AccountStatus) *testAccountBuilder {
	t.acct.Status = status
	return t
}

// Just set the whole Spec all in one go
func (t *testAccountBuilder) WithSpec(spec awsv1alpha1.AccountSpec) *testAccountBuilder {
	t.acct.Spec = spec
	return t
}

// Set a creation timestamp
func (t *testAccountBuilder) WithCreationTimeStamp(timestamp time.Time) *testAccountBuilder {
	t.acct.ObjectMeta.CreationTimestamp.Time = timestamp
	return t
}

// Set a deletion timestamp
func (t *testAccountBuilder) WithDeletionTimeStamp(timestamp time.Time) *testAccountBuilder {
	t.acct.ObjectMeta.DeletionTimestamp = &metav1.Time{Time: timestamp}
	return t
}

// Add finalizers
func (t *testAccountBuilder) WithFinalizers(finalizers []string) *testAccountBuilder {
	t.acct.ObjectMeta.Finalizers = finalizers
	return t
}

// Add labels
func (t *testAccountBuilder) WithLabels(labels map[string]string) *testAccountBuilder {
	t.acct.ObjectMeta.Labels = labels
	return t
}

// Add a state string
func (t *testAccountBuilder) WithState(state awsv1alpha1.AccountConditionType) *testAccountBuilder {
	t.acct.Status.State = string(state)
	return t
}

// Delete state
func (t *testAccountBuilder) WithoutState() *testAccountBuilder {
	t.acct.Status.State = ""
	return t
}

// Set account claimed or not
func (t *testAccountBuilder) Claimed(claimed bool) *testAccountBuilder {
	t.acct.Status.Claimed = claimed
	return t
}

// Add supportCaseID
func (t *testAccountBuilder) WithSupportCaseID(id string) *testAccountBuilder {
	t.acct.Status.SupportCaseID = id
	return t
}

// Set rotate credentials or not
func (t *testAccountBuilder) RotateCredentials(rotate bool) *testAccountBuilder {
	t.acct.Status.RotateCredentials = rotate
	return t
}

// Set rotate console credentials or not
func (t *testAccountBuilder) RotateConsoleCredentials(rotate bool) *testAccountBuilder {
	t.acct.Status.RotateConsoleCredentials = rotate
	return t
}

// Add a claimLink
func (t *testAccountBuilder) WithClaimLink(link string) *testAccountBuilder {
	t.acct.Spec.ClaimLink = link
	return t
}

// Set BYOC or not
func (t *testAccountBuilder) BYOC(byoc bool) *testAccountBuilder {
	t.acct.Spec.BYOC = byoc
	return t
}

// Add an awsAccountID
func (t *testAccountBuilder) WithAwsAccountID(id string) *testAccountBuilder {
	t.acct.Spec.AwsAccountID = id
	return t
}

func TestMatchSubstring(t *testing.T) {
	tests := []struct {
		name     string
		roleID   string
		role     string
		expected bool
	}{
		{
			name:     "Match substrings 0",
			roleID:   "AROA3SYAY5EP3KG4G2FIR",
			role:     "AROA3SYAY5EP3KG4G2FIR:awsAccountOperator",
			expected: true,
		},
		{
			name:     "Match substrings 1",
			roleID:   "AROA3SYABCEDRKG4G2FIR",
			role:     "AROA3SYABCEDRKG4G2FIR:awsAccountOperator",
			expected: true,
		},
		{
			name:     "Match substrings 2",
			roleID:   "AROABIGORGOHOME4G2FIR",
			role:     "AROABIGORGOHOME4G2FIR:awsAccountOperator",
			expected: true,
		},
		{
			name:     "Match substrings 3",
			roleID:   "IHEHRHSHY5EP3KG4G2FIR",
			role:     "AROA3SYAY5EP3KG4G2FIR:awsAccountOperator",
			expected: false,
		},
		{
			name:     "Match substrings 4",
			roleID:   "AROA3SYAEHAIRHHALKBCDERKG422FIR",
			role:     "AROA3SYABCEDRKG4G2FIR:awsAccountOperator",
			expected: false,
		},
		{
			name:     "Match substrings 5",
			roleID:   "A test string",
			role:     "AROA3SYAY5EP3KG4G2FIR:awsAccountOperator",
			expected: false,
		},
	}
	for _, pair := range tests {
		t.Run(
			pair.name,
			func(t *testing.T) {
				result, err := matchSubstring(pair.roleID, pair.role)
				if result != pair.expected {
					t.Error(
						"For", fmt.Sprintf("%s - %s", pair.roleID, pair.role),
						"expected", pair.expected,
						"got", result,
					)
				}
				if err != nil {
					t.Error(
						"For", fmt.Sprintf("%s - %s", pair.roleID, pair.role),
						"expected", nil,
						"got", err,
					)
				}
			},
		)
	}
}

// Test accountHasState
func TestAccountHasState(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account has State",
			acct:     newTestAccountBuilder(),
			expected: true,
		},
		{
			name:     "Account does not have State",
			acct:     newTestAccountBuilder().WithoutState(),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.HasState()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test the account has SupportCaseID
func TestAccountHasSupportCaseID(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account has Support CaseID",
			acct:     newTestAccountBuilder().WithSupportCaseID("fakeSupportCaseID"),
			expected: true,
		},
		{
			name:     "Account does not have Support CaseID",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.HasSupportCaseID()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountIsPendingVerification
func TestAccountIsPendingVerification(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account is pending verification",
			acct:     newTestAccountBuilder().WithState(awsv1alpha1.AccountPendingVerification),
			expected: true,
		},
		{
			name:     "Account is not pending verificatio",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsPendingVerification()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountIsReady
func TestAccountIsReady(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account is ready",
			acct:     newTestAccountBuilder(),
			expected: true,
		},
		{
			name:     "Account is not ready",
			acct:     newTestAccountBuilder().WithState(awsv1alpha1.AccountPending),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsReady()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountIsFailed
func TestAccountIsFailed(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account is ready",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
		{
			name:     "Account is failed",
			acct:     newTestAccountBuilder().WithState(awsv1alpha1.AccountFailed),
			expected: true,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsFailed()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountIsCreating
func TestAccountIsCreating(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account is creating",
			acct:     newTestAccountBuilder().WithState(awsv1alpha1.AccountCreating),
			expected: true,
		},
		{
			name:     "Account is not creating",
			acct:     newTestAccountBuilder().WithState(awsv1alpha1.AccountPending),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsCreating()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountIsClaimed
func TestAccountIsClaimed(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account is claimed",
			acct:     newTestAccountBuilder().Claimed(true),
			expected: true,
		},
		{
			name:     "Account is not claimed",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsClaimed()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountHasClaimlink
func TestAccountHasClaimLink(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account has claimLink",
			acct:     newTestAccountBuilder().WithClaimLink("fakeClaimLink"),
			expected: true,
		},
		{
			name:     "Account does not have claimLink",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.HasClaimLink()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountCreatingTooLong
func TestAccountCreatingToolong(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name: "Account creating too long",
			acct: newTestAccountBuilder().WithStatus(awsv1alpha1.AccountStatus{
				State: string(awsv1alpha1.AccountCreating),
				Conditions: []awsv1alpha1.AccountCondition{
					{
						Type:          awsv1alpha1.AccountCreating,
						LastProbeTime: metav1.Time{Time: time.Now().Add(-(createPendTime + time.Minute))},
					},
				},
			}), // 1 minute longer than the allowed timeout
			expected: true,
		},
		{
			name: "Account outside timeout threshold, but not creating",
			acct: newTestAccountBuilder().WithStatus(awsv1alpha1.AccountStatus{
				State: string(awsv1alpha1.AccountReady),
				Conditions: []awsv1alpha1.AccountCondition{
					{
						Type:          awsv1alpha1.AccountCreating,
						LastProbeTime: metav1.Time{Time: time.Now().Add(-(createPendTime + time.Minute))},
					},
				},
			}), // 1 minute longer than the allowed timeout
			expected: false,
		},
		{
			name: "Account creating within timout threshold",
			acct: newTestAccountBuilder().WithStatus(awsv1alpha1.AccountStatus{
				State: string(awsv1alpha1.AccountCreating),
				Conditions: []awsv1alpha1.AccountCondition{
					{
						Type:          awsv1alpha1.AccountCreating,
						LastProbeTime: metav1.Time{Time: time.Now()},
					},
				},
			}),
			expected: false,
		},
		{
			name:     "Account not creating and within timout threshold",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsCreating() && utils.CreationConditionOlderThan(test.acct.acct, createPendTime)
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountIsPendingDeletion
func TestAccountIsPendingDeletion(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account has Deletion Timestamp",
			acct:     newTestAccountBuilder().WithDeletionTimeStamp(time.Now()),
			expected: true,
		},
		{
			name:     "Account does not have Deletion Timestamp",
			acct:     newTestAccountBuilder().BYOC(false),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsPendingDeletion()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountIsBYOC
func TestAccountIsBYOC(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account BYOC spec is unset",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
		{
			name:     "Account BYOC spec is false",
			acct:     newTestAccountBuilder().BYOC(false),
			expected: false,
		},
		{
			name:     "Account BYOC spec is true",
			acct:     newTestAccountBuilder().BYOC(true),
			expected: true,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsBYOC()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountHasAwsv1alpha1Finalizer
func TestAccountHasAwsv1alpha1Finalizer(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account has v1alpha1 Finalizer",
			acct:     newTestAccountBuilder().WithFinalizers([]string{awsv1alpha1.AccountFinalizer, "fakeFinalizer0", "fakeFinalizer1"}),
			expected: true,
		},
		{
			name:     "Account has only v1alpha1 Finalizer",
			acct:     newTestAccountBuilder().WithFinalizers([]string{awsv1alpha1.AccountFinalizer}),
			expected: true,
		},
		{
			name:     "Account does not have awsv1alpha1 Finalizer",
			acct:     newTestAccountBuilder().WithFinalizers([]string{"fakeFinalizer0", "fakeFinalizer1"}),
			expected: false,
		},
		{
			name:     "Account has no finalizers",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.HasAwsv1alpha1Finalizer()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test the accountHasAwsAccountID
func TestAccountHasAwsAccountID(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account has AWS Account ID",
			acct:     newTestAccountBuilder().WithAwsAccountID("fakeAwsAccountID"),
			expected: true,
		},
		{
			name:     "Account does not have AWS Account ID",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.HasAwsAccountID()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test tagAccount
func TestTagAccount(t *testing.T) {
	mocks := setupDefaultMocks(t, []runtime.Object{})

	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)
	accountID := "111111111111"
	hivename := "hivename"

	awsOutputTag := &organizations.TagResourceOutput{}

	mockAWSClient.EXPECT().TagResource(&organizations.TagResourceInput{
		ResourceId: &accountID,
		Tags: []*organizations.Tag{
			{
				Key:   aws.String("owner"),
				Value: aws.String(hivename)}},
	}).Return(
		awsOutputTag,
		nil,
	)

	r := &ReconcileAccount{shardName: "hivename"}
	nullLogger := testutils.NullLogger{}
	err := r.tagAccount(nullLogger, mockAWSClient, accountID)
	if err != nil {
		t.Errorf("failed to tag account")
	}
}

// Test accountIsReadyUnclaimedAndHasClaimLink
func TestAccountIsReadyUnclaimedAndHasClaimLink(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account is ready, unclaimed, and has a claimLink",
			acct:     newTestAccountBuilder().WithClaimLink("fakeClaimLink"),
			expected: true,
		},
		{
			name:     "Account is not ready, unclaimed, and has a claimLink",
			acct:     newTestAccountBuilder().WithState(awsv1alpha1.AccountPending).WithClaimLink("fakeClaimLink"),
			expected: false,
		},
		{
			name:     "Account is ready, claimed, and has a claimLink",
			acct:     newTestAccountBuilder().Claimed(true).WithClaimLink("fakeClaimLink"),
			expected: false,
		},
		{
			name:     "Account is ready, unclaimed, and does not a claimLink",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsReadyUnclaimedAndHasClaimLink()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountISBYOCPendingDeletionWithFinalizer
func TestAccountIsBYOCPendingDeletionWithFinalizer(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account is a BYOC Account, Pending Deletion and has the awsv1alpha1 Finalizer",
			acct:     newTestAccountBuilder().BYOC(true).WithDeletionTimeStamp(time.Now()).WithFinalizers([]string{awsv1alpha1.AccountFinalizer}),
			expected: true,
		},
		{
			name:     "Account is not a BYOC Account, is Pending Deletion and has the awsv1alpha1 Finalizer",
			acct:     newTestAccountBuilder().WithDeletionTimeStamp(time.Now()).WithFinalizers([]string{awsv1alpha1.AccountFinalizer}),
			expected: false,
		},
		{
			name:     "Account is a BYOC Account, is not Pending Deletion and has the awsv1alpha1 Finalizer",
			acct:     newTestAccountBuilder().BYOC(true).WithFinalizers([]string{awsv1alpha1.AccountFinalizer}),
			expected: false,
		},
		{
			name:     "Account is a BYOC Account, is Pending Deletion but does not have the awsv1alpha1 Finalizer",
			acct:     newTestAccountBuilder().BYOC(true).WithDeletionTimeStamp(time.Now()),
			expected: false,
		},
		{
			name:     "Account is not BYOC, is not Pending Deletion and does not have the awsv1alpha1 Finalizer",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsBYOCPendingDeletionWithFinalizer()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountIsBYOCAndNotReady
func TestAccountIsBYOCAndNotReady(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account is BYOC and not ready",
			acct:     newTestAccountBuilder().BYOC(true).WithState(awsv1alpha1.AccountCreating),
			expected: true,
		},
		{
			name:     "Account not BYOC or ready",
			acct:     newTestAccountBuilder().WithState(awsv1alpha1.AccountCreating),
			expected: false,
		},
		{
			name:     "Account is BYOC and ready",
			acct:     newTestAccountBuilder().BYOC(true),
			expected: false,
		},
		{
			name:     "Account is not BYOC but is ready",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsBYOCAndNotReady()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountReadyForInitialization
func TestAccountReadyForInitialization(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account is BYOC and not ready",
			acct:     newTestAccountBuilder().BYOC(true).WithoutState(),
			expected: true,
		},
		{
			name:     "Account is unclaimed and creating",
			acct:     newTestAccountBuilder().WithState(awsv1alpha1.AccountCreating),
			expected: true,
		},
		{
			name:     "Account is BYOC and ready",
			acct:     newTestAccountBuilder().BYOC(true),
			expected: false,
		},
		{
			name:     "Account is not BYOC but is ready",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
		{
			name:     "Account is claimed and creating",
			acct:     newTestAccountBuilder().Claimed(true).WithState(awsv1alpha1.AccountCreating),
			expected: false,
		},
		{
			name:     "Account unclaimed and not creating",
			acct:     newTestAccountBuilder().WithState(awsv1alpha1.AccountReady),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.ReadyForInitialization()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountIsUnclaimedAndHasNoState
func TestAccountIsUnclaimedAndHasNoState(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account is unclaimed and has no state",
			acct:     newTestAccountBuilder().WithoutState(),
			expected: true,
		},
		{
			name:     "Account is unclaimed and has state",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
		{
			name:     "Account is claimed and has no state",
			acct:     newTestAccountBuilder().Claimed(true).WithoutState(),
			expected: false,
		},
		{
			name:     "Account is claimed and has state",
			acct:     newTestAccountBuilder().Claimed(true),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsUnclaimedAndHasNoState()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountIsUnclaimedAndIsCreating
func TestAccountIsUnclaimedAndCreating(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account is unclaimed and Creating",
			acct:     newTestAccountBuilder().WithState(awsv1alpha1.AccountCreating),
			expected: true,
		},
		{
			name:     "Account is unclaimed and not creating",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
		{
			name:     "Account is claimed and Creating",
			acct:     newTestAccountBuilder().Claimed(true).WithState(awsv1alpha1.AccountCreating),
			expected: false,
		},
		{
			name:     "Account is claimed and not creating",
			acct:     newTestAccountBuilder().Claimed(true),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsUnclaimedAndIsCreating()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

func TestGetAssumeRole(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		acct     *testAccountBuilder
	}{
		{
			name:     "Get role for BYOC Account",
			acct:     newTestAccountBuilder().BYOC(true).WithLabels(map[string]string{awsv1alpha1.IAMUserIDLabel: "xxxxx"}),
			expected: fmt.Sprintf("%s-%s", awsv1alpha1.ManagedOpenShiftSupportRole, "xxxxx"),
		},
		{
			name:     "Get role for Non-BYOC Account",
			acct:     newTestAccountBuilder(),
			expected: awsv1alpha1.AccountOperatorIAMRole,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := getAssumeRole(&test.acct.acct)
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test finalizeAccount
func TestFinalizeAccount(t *testing.T) {

	err := apis.AddToScheme(scheme.Scheme)
	if err != nil {
		fmt.Printf("failed adding to scheme in account_controller_test.go")
	}

	nullLogger := testutils.NullLogger{}

	tests := []struct {
		name string
		acct *testAccountBuilder
	}{
		{
			name: "Account has STS Mode enabled",
			acct: newTestAccountBuilder().WithSpec(awsv1alpha1.AccountSpec{ManualSTSMode: true}),
		},
		{
			name: "Account is BYOC without iamUserId Labels",
			acct: newTestAccountBuilder().BYOC(true),
		},
		{
			name: "Account is non-BYOC, non-STS",
			acct: newTestAccountBuilder(),
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			//t.Parallel()

			localObjects := []runtime.Object{
				&test.acct.acct,
			}

			mocks := setupDefaultMocks(t, localObjects)
			mockAWSClient := mock.NewMockClient(mocks.mockCtrl)

			// This is necessary for the mocks to report failures like methods not being called an expected number of times.
			// after mocks is defined
			defer mocks.mockCtrl.Finish()

			r := ReconcileAccount{
				Client: mocks.fakeKubeClient,
				scheme: scheme.Scheme,
			}

			r.finalizeAccount(nullLogger, mockAWSClient, &test.acct.acct)
		})
	}
}

func TestFinalizeAccount_LabelledBYOCAccount(t *testing.T) {
	err := apis.AddToScheme(scheme.Scheme)
	if err != nil {
		fmt.Printf("failed adding to scheme in account_controller_test.go")
	}
	nullLogger := testutils.NullLogger{}

	account := newTestAccountBuilder().BYOC(true).WithLabels(
		map[string]string{
			"iamUserId": "iam1234",
		},
	).acct

	localObjects := []runtime.Object{&account}
	mocks := setupDefaultMocks(t, localObjects)
	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)
	mockAWSClient.EXPECT().ListUsersPages(gomock.Any(), gomock.Any())
	mockAWSClient.EXPECT().ListRoles(gomock.Any()).Return(
		&iam.ListRolesOutput{
			Roles:       []*iam.Role{},
			IsTruncated: aws.Bool(false),
		},
		nil,
	)

	// This is necessary for the mocks to report failures like methods not being called an expected number of times.
	// after mocks is defined
	defer mocks.mockCtrl.Finish()

	r := ReconcileAccount{
		Client: mocks.fakeKubeClient,
		scheme: scheme.Scheme,
	}
	r.finalizeAccount(nullLogger, mockAWSClient, &account)
}

var _ = Describe("Account Controller", func() {
	var (
		nullLogger    testutils.NullLogger
		mockAWSClient *mock.MockClient
		accountName   string
		accountEmail  string
		ctrl          *gomock.Controller
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockAWSClient = mock.NewMockClient(ctrl)
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("Testing CreateAccount", func() {

		It("AWS returns ErrCodeConstraintViolationException from CreateAccount", func() {
			// ErrCodeConstraintViolationException is mapped to awsv1alpha1.ErrAwsAccountLimitExceeded in CreateAccount
			mockAWSClient.EXPECT().CreateAccount(gomock.Any()).Return(nil, awserr.New(organizations.ErrCodeConstraintViolationException, "Error String", nil))
			createAccountOutput, err := CreateAccount(nullLogger, mockAWSClient, accountName, accountEmail)
			Expect(err).To(HaveOccurred())
			Expect(createAccountOutput).To(Equal(&organizations.DescribeCreateAccountStatusOutput{}))
			Expect(awsv1alpha1.ErrAwsAccountLimitExceeded).To(Equal(err))
		})

		It("AWS returns ErrCodeServiceException from CreateAccount", func() {
			// ErrCodeServiceException is mapped to awsv1alpha1.ErrAwsInternalFailure in CreateAccount
			mockAWSClient.EXPECT().CreateAccount(gomock.Any()).Return(nil, awserr.New(organizations.ErrCodeServiceException, "Error String", nil))
			createAccountOutput, err := CreateAccount(nullLogger, mockAWSClient, accountName, accountEmail)
			Expect(err).To(HaveOccurred())
			Expect(createAccountOutput).To(Equal(&organizations.DescribeCreateAccountStatusOutput{}))
			Expect(awsv1alpha1.ErrAwsInternalFailure).To(Equal(err))
		})

		It("AWS returns ErrCodeTooManyRequestsException from CreateAccount", func() {
			// ErrCodeTooManyRequestsException is mapped to awsv1alpha1.ErrAwsTooManyRequests in CreateAccount
			mockAWSClient.EXPECT().CreateAccount(gomock.Any()).Return(nil, awserr.New(organizations.ErrCodeTooManyRequestsException, "Error String", nil))
			createAccountOutput, err := CreateAccount(nullLogger, mockAWSClient, accountName, accountEmail)
			Expect(err).To(HaveOccurred())
			Expect(createAccountOutput).To(Equal(&organizations.DescribeCreateAccountStatusOutput{}))
			Expect(awsv1alpha1.ErrAwsTooManyRequests).To(Equal(err))
		})

		It("AWS returns error from CreateAccount", func() {
			// Unhandled AWS exceptions get mapped awsv1alpha1.ErrAwsFailedCreateAccount in CreateAccount
			mockAWSClient.EXPECT().CreateAccount(gomock.Any()).Return(nil, awserr.New(organizations.ErrCodeDuplicateAccountException, "Error String", nil))
			createAccountOutput, err := CreateAccount(nullLogger, mockAWSClient, accountName, accountEmail)
			Expect(err).To(HaveOccurred())
			Expect(createAccountOutput).To(Equal(&organizations.DescribeCreateAccountStatusOutput{}))
			Expect(awsv1alpha1.ErrAwsFailedCreateAccount).To(Equal(err))
		})

		It("AWS returns an error from DescribeCreateAccountStatus", func() {
			mockAWSClient.EXPECT().CreateAccount(gomock.Any()).Return(
				&organizations.CreateAccountOutput{
					CreateAccountStatus: &organizations.CreateAccountStatus{
						Id: aws.String("ID"),
					},
				},
				nil,
			)

			expectedErr := awserr.New(organizations.ErrCodeServiceException, "Error String", nil)
			mockAWSClient.EXPECT().DescribeCreateAccountStatus(gomock.Any()).Return(nil, expectedErr) //errors.New("MyError")) //)
			createAccountOutput, err := CreateAccount(nullLogger, mockAWSClient, accountName, accountEmail)
			Expect(err).To(HaveOccurred())
			Expect(createAccountOutput).To(Equal(&organizations.DescribeCreateAccountStatusOutput{}))
			Expect(expectedErr).To(Equal(err))
		})

		It("DescribeCreateAccountStatus returns a FAILED state", func() {
			mockAWSClient.EXPECT().CreateAccount(gomock.Any()).Return(
				&organizations.CreateAccountOutput{
					CreateAccountStatus: &organizations.CreateAccountStatus{
						Id: aws.String("ID"),
					},
				},
				nil,
			)
			describeCreateAccountStatusOutput := &organizations.DescribeCreateAccountStatusOutput{
				CreateAccountStatus: &organizations.CreateAccountStatus{
					State:         aws.String("FAILED"),
					FailureReason: aws.String("ACCOUNT_LIMIT_EXCEEDED"),
				},
			}
			mockAWSClient.EXPECT().DescribeCreateAccountStatus(gomock.Any()).Return(describeCreateAccountStatusOutput, nil)
			createAccountOutput, err := CreateAccount(nullLogger, mockAWSClient, accountName, accountEmail)
			Expect(err).To(HaveOccurred())

			Expect(createAccountOutput).To(Equal(&organizations.DescribeCreateAccountStatusOutput{}))
			Expect(awsv1alpha1.ErrAwsAccountLimitExceeded).To(Equal(err))
		})
		It("CreateAccount creates account", func() {
			mockAWSClient.EXPECT().CreateAccount(gomock.Any()).Return(
				&organizations.CreateAccountOutput{
					CreateAccountStatus: &organizations.CreateAccountStatus{
						Id: aws.String("ID"),
					},
				},
				nil,
			)
			describeCreateAccountStatusOutput := &organizations.DescribeCreateAccountStatusOutput{
				CreateAccountStatus: &organizations.CreateAccountStatus{
					State: aws.String("SUCCEEDED"),
				},
			}
			mockAWSClient.EXPECT().DescribeCreateAccountStatus(gomock.Any()).Return(describeCreateAccountStatusOutput, nil)
			createAccountOutput, err := CreateAccount(nullLogger, mockAWSClient, accountName, accountEmail)
			Expect(err).To(Succeed())
			Expect(createAccountOutput).To(Equal(describeCreateAccountStatusOutput))
			Expect(err).Should(BeNil())
		})
	})
})
