package accountclaim

import (
	"github.com/openshift/aws-account-operator/pkg/testutils"
	"testing"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/organizations"
	"github.com/go-logr/logr"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/awsclient/mock"
)

var _ = Describe("Organizational Unit", func() {
	var (
		nullLogger    logr.Logger
		ctrl          *gomock.Controller
		mockAWSClient *mock.MockClient
		r             AccountClaimReconciler
		ouName        = "ouName"
		ouID          = "ouID"
		baseID        = "baseID"
		myID          = "MyID"
		parentID      = "parentID"
		awsAccountID  = "12345"
		accountClaim  = awsv1alpha1.AccountClaim{
			ObjectMeta: v1.ObjectMeta{
				Name:      "test-account-name",
				Namespace: "test-namespace",
			},
		}
		account = awsv1alpha1.Account{
			Spec: awsv1alpha1.AccountSpec{
				AwsAccountID: awsAccountID,
			},
		}
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockAWSClient = mock.NewMockClient(ctrl)
		nullLogger = testutils.NewTestLogger().Logger()
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	When("Moving an Account to an OU", func() {
		It("Should error when no ConfigMap can be found", func() {
			localObjects := []runtime.Object{}
			r = AccountClaimReconciler{
				Scheme: scheme.Scheme,
				Client: fake.NewClientBuilder().WithRuntimeObjects(localObjects...).Build(),
			}
			err := MoveAccountToOU(&r, nullLogger, mockAWSClient, &accountClaim, &account)
			Expect(err).To(HaveOccurred())
		})

		It("Should error when an invaid ConfigMap is found", func() {
			cm := corev1.ConfigMap{
				ObjectMeta: v1.ObjectMeta{
					Namespace: awsv1alpha1.AccountCrNamespace,
					Name:      awsv1alpha1.DefaultConfigMap,
				},
				Data: map[string]string{
					"base": "",
				},
			}
			localObjects := []runtime.Object{&cm}
			r = AccountClaimReconciler{
				Scheme: scheme.Scheme,
				Client: fake.NewFakeClient(localObjects...),
			}

			err := MoveAccountToOU(&r, nullLogger, mockAWSClient, &accountClaim, &account)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(awsv1alpha1.ErrInvalidConfigMap))
		})

		It("Should error when the AccountClaim has an empty LegalEntity ID", func() {
			cm := corev1.ConfigMap{
				ObjectMeta: v1.ObjectMeta{
					Namespace: awsv1alpha1.AccountCrNamespace,
					Name:      awsv1alpha1.DefaultConfigMap,
				},
				Data: map[string]string{
					"base": "",
					"root": "",
				},
			}
			localObjects := []runtime.Object{&cm}
			r = AccountClaimReconciler{
				Scheme: scheme.Scheme,
				Client: fake.NewClientBuilder().WithRuntimeObjects(localObjects...).Build(),
			}

			accountClaim.Spec = awsv1alpha1.AccountClaimSpec{
				LegalEntity: awsv1alpha1.LegalEntity{
					ID: "",
				},
			}
			err := MoveAccountToOU(&r, nullLogger, mockAWSClient, &accountClaim, &account)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(awsv1alpha1.ErrUnexpectedValue))
		})

		It("Should error when moving an Account to an OU it's already in", func() {
			cm := corev1.ConfigMap{
				ObjectMeta: v1.ObjectMeta{
					Namespace: awsv1alpha1.AccountCrNamespace,
					Name:      awsv1alpha1.DefaultConfigMap,
				},
				Data: map[string]string{
					"base": "base",
					"root": "root",
				},
			}
			accountClaim.Spec = awsv1alpha1.AccountClaimSpec{
				LegalEntity: awsv1alpha1.LegalEntity{
					ID: ouName,
				},
			}

			localObjects := []runtime.Object{&accountClaim, &cm}
			r = AccountClaimReconciler{
				Scheme: scheme.Scheme,
				Client: fake.NewFakeClient(localObjects...),
			}

			mockAWSClient.EXPECT().CreateOrganizationalUnit(gomock.Any()).Return(
				&organizations.CreateOrganizationalUnitOutput{
					OrganizationalUnit: &organizations.OrganizationalUnit{
						Id: &myID,
					},
				},
				nil,
			)

			// Needed for
			expectedErr := awserr.New("AccountNotFoundException", "Some AWS Error", nil)
			mockAWSClient.EXPECT().MoveAccount(gomock.Any()).Return(nil, expectedErr)
			mockAWSClient.EXPECT().ListChildren(gomock.Any()).Return(
				&organizations.ListChildrenOutput{
					Children: []*organizations.Child{
						{
							Id: &awsAccountID,
						},
					},
				},
				nil,
			)

			err := MoveAccountToOU(&r, nullLogger, mockAWSClient, &accountClaim, &account)
			Expect(err).ToNot(HaveOccurred())
			Expect(accountClaim.Spec.AccountOU).To(Equal(myID))
		})

		It("Should successfully move Account to OU", func() {
			cm := corev1.ConfigMap{
				ObjectMeta: v1.ObjectMeta{
					Namespace: awsv1alpha1.AccountCrNamespace,
					Name:      awsv1alpha1.DefaultConfigMap,
				},
				Data: map[string]string{
					"base": "base",
					"root": "root",
				},
			}
			accountClaim.Spec = awsv1alpha1.AccountClaimSpec{
				LegalEntity: awsv1alpha1.LegalEntity{
					ID: ouName,
				},
			}

			localObjects := []runtime.Object{&accountClaim, &cm}
			r = AccountClaimReconciler{
				Scheme: scheme.Scheme,
				Client: fake.NewFakeClient(localObjects...),
			}

			mockAWSClient.EXPECT().CreateOrganizationalUnit(gomock.Any()).Return(
				&organizations.CreateOrganizationalUnitOutput{
					OrganizationalUnit: &organizations.OrganizationalUnit{
						Id: &myID,
					},
				},
				nil,
			)
			mockAWSClient.EXPECT().MoveAccount(gomock.Any()).Return(nil, nil)

			err := MoveAccountToOU(&r, nullLogger, mockAWSClient, &accountClaim, &account)
			Expect(err).ToNot(HaveOccurred())
			Expect(accountClaim.Spec.AccountOU).To(Equal(myID))
		})
	})

	When("Creating or Finding an OU", func() {
		It("Should create new OU if it doesn't already exists", func() {
			mockAWSClient.EXPECT().CreateOrganizationalUnit(
				&organizations.CreateOrganizationalUnitInput{
					Name:     &ouName,
					ParentId: &baseID,
				},
			).Return(
				&organizations.CreateOrganizationalUnitOutput{
					OrganizationalUnit: &organizations.OrganizationalUnit{
						Id: &myID,
					},
				},
				nil,
			)
			output, err := CreateOrFindOU(nullLogger, mockAWSClient, ouName, baseID)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal(myID))
		})

		It("Should fail when provided with invalid input", func() {
			output, err := CreateOrFindOU(nullLogger, mockAWSClient, "", "")
			Expect(err).To(HaveOccurred())
			Expect(err).To(BeEquivalentTo(awsv1alpha1.ErrUnexpectedValue))
			Expect(output).To(BeEmpty())
		})

		It("Should return OU ID when OU already exists", func() {
			mockAWSClient.EXPECT().CreateOrganizationalUnit(gomock.Any()).Return(
				&organizations.CreateOrganizationalUnitOutput{
					OrganizationalUnit: &organizations.OrganizationalUnit{
						Id: &myID,
					},
				},
				awserr.New("DuplicateOrganizationalUnitException", "Some AWS Error", nil),
			)
			mockAWSClient.EXPECT().ListOrganizationalUnitsForParent(gomock.Any()).Return(
				&organizations.ListOrganizationalUnitsForParentOutput{
					OrganizationalUnits: []*organizations.OrganizationalUnit{
						{
							Id:   &myID,
							Name: &ouName,
						},
					},
				},
				nil,
			)
			output, err := CreateOrFindOU(nullLogger, mockAWSClient, ouName, baseID)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal(myID))
		})

		It("Should return unhandled aws errors when attempting to create OU", func() {
			expectedErr := awserr.New("defaultErr", "Some AWS Error", nil)
			mockAWSClient.EXPECT().CreateOrganizationalUnit(gomock.Any()).Return(
				&organizations.CreateOrganizationalUnitOutput{
					OrganizationalUnit: &organizations.OrganizationalUnit{
						Id: &myID,
					},
				},
				expectedErr,
			)
			output, err := CreateOrFindOU(nullLogger, mockAWSClient, ouName, baseID)
			Expect(err).To(HaveOccurred())
			Expect(output).To(BeEmpty())
			Expect(err).To(BeEquivalentTo(expectedErr))
		})
	})

	When("Moving Account", func() {
		It("Should move successfully", func() {
			mockAWSClient.EXPECT().MoveAccount(&organizations.MoveAccountInput{
				AccountId:           &awsAccountID,
				DestinationParentId: &ouID,
				SourceParentId:      &parentID,
			}).Return(nil, nil)
			err := MoveAccount(nullLogger, mockAWSClient, &account, ouID, parentID)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should error when Account already in correct OU", func() {
			expectedErr := awserr.New("AccountNotFoundException", "Some AWS Error", nil)
			mockAWSClient.EXPECT().MoveAccount(gomock.Any()).Return(nil, expectedErr)
			mockAWSClient.EXPECT().ListChildren(gomock.Any()).Return(
				&organizations.ListChildrenOutput{
					Children: []*organizations.Child{
						{
							Id: &awsAccountID,
						},
					},
				},
				nil,
			)
			err := MoveAccount(nullLogger, mockAWSClient, &account, ouID, parentID)
			Expect(err).To(HaveOccurred())
			Expect(err).To(Equal(awsv1alpha1.ErrAccAlreadyInOU))
		})

		It("Should throw an error when Account cannot be found", func() {
			mockAWSClient.EXPECT().MoveAccount(gomock.Any()).Return(
				nil,
				awserr.New("AccountNotFoundException", "Some AWS Error", nil),
			)
			mockAWSClient.EXPECT().ListChildren(gomock.Any()).Return(
				&organizations.ListChildrenOutput{
					Children:  []*organizations.Child{},
					NextToken: nil,
				},
				nil,
			)
			err := MoveAccount(nullLogger, mockAWSClient, &account, ouID, parentID)
			Expect(err).To(HaveOccurred())
			Expect(err).To(Equal(awsv1alpha1.ErrChildNotFound))
		})

		It("Should error when encountering a race condition when attempting to move account", func() {
			expectedErr := awserr.New("ConcurrentModificationException", "Some AWS Error", nil)
			mockAWSClient.EXPECT().MoveAccount(gomock.Any()).Return(nil, expectedErr)
			err := MoveAccount(nullLogger, mockAWSClient, &account, ouID, parentID)
			Expect(err).To(HaveOccurred())
			Expect(err).To(Equal(awsv1alpha1.ErrAccMoveRaceCondition))
		})

		It("Should return error directly when an unexpected error has occurred", func() {
			expectedErr := awserr.New("OtherErr", "Some AWS Error", nil)
			mockAWSClient.EXPECT().MoveAccount(gomock.Any()).Return(nil, expectedErr)
			err := MoveAccount(nullLogger, mockAWSClient, &account, ouID, parentID)
			Expect(err).To(HaveOccurred())
			Expect(err).To(Equal(expectedErr))
		})
	})
})

func TestFindOUIDFromName(t *testing.T) {
	// OrganizationalUnit list for testing
	idZero := "00"
	nameZero := "zero"
	idOne := "01"
	nameOne := "one"
	idTwo := "02"
	nameTwo := "two"
	ouList := []*organizations.OrganizationalUnit{
		{
			Id:   &idZero,
			Name: &nameZero,
		},
		{
			Id:   &idOne,
			Name: &nameOne,
		},
		{
			Id:   &idTwo,
			Name: &nameTwo,
		},
	}
	// tests
	tests := []struct {
		name                 string
		listOUForParentOut   *organizations.ListOrganizationalUnitsForParentOutput
		listOUForParentErr   error
		parentID             string
		ouName               string
		expectedOUID         string
		expectedErr          error
		findOUIDFromNameFunc func(logr.Logger, awsclient.Client, string, string) (string, error)
	}{
		{
			name: "Existing OU ID",
			listOUForParentOut: &organizations.ListOrganizationalUnitsForParentOutput{
				OrganizationalUnits: ouList,
			},
			listOUForParentErr:   nil,
			parentID:             "000",
			ouName:               "one",
			expectedOUID:         "01",
			expectedErr:          nil,
			findOUIDFromNameFunc: findouIDFromName,
		},
		{
			name:                 "ListOrganizationalUnitsForParent Err encountered",
			listOUForParentOut:   &organizations.ListOrganizationalUnitsForParentOutput{},
			listOUForParentErr:   awserr.New("AccountNotFoundException", "Some AWS Error", nil),
			parentID:             "000",
			ouName:               "one",
			expectedOUID:         "",
			expectedErr:          awserr.New("AccountNotFoundException", "Some AWS Error", nil),
			findOUIDFromNameFunc: findouIDFromName,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// build mock
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			mocks := mock.NewMockClient(ctrl)
			mocks.EXPECT().ListOrganizationalUnitsForParent(&organizations.ListOrganizationalUnitsForParentInput{
				ParentId: &test.parentID,
			}).Return(test.listOUForParentOut, test.listOUForParentErr)
			reqLogger := log.WithValues()
			// Test
			ouID, err := test.findOUIDFromNameFunc(reqLogger, mocks, test.parentID, test.ouName)
			assert.EqualValues(t, test.expectedOUID, ouID)
			assert.EqualValues(t, test.expectedErr, err)
		})
	}
}

func TestCheckOUMapping(t *testing.T) {
	tests := []struct {
		name               string
		localObjects       corev1.ConfigMap
		expectedError      error
		checkOUMappingFunc func(*corev1.ConfigMap) (string, string, error)
	}{
		{
			name: "No missing fields",
			localObjects: corev1.ConfigMap{
				Data: map[string]string{
					"root": "test",
					"base": "claim-test",
				},
			},
			expectedError:      nil,
			checkOUMappingFunc: checkOUMapping,
		},
		{
			name: "Missing root field",
			localObjects: corev1.ConfigMap{
				Data: map[string]string{
					"base": "claim-test",
				},
			},
			expectedError:      awsv1alpha1.ErrInvalidConfigMap,
			checkOUMappingFunc: checkOUMapping,
		},
		{
			name: "Missing base field",
			localObjects: corev1.ConfigMap{
				Data: map[string]string{
					"root": "test",
				},
			},
			expectedError:      awsv1alpha1.ErrInvalidConfigMap,
			checkOUMappingFunc: checkOUMapping,
		},
		{
			name: "Missing root and base fields",
			localObjects: corev1.ConfigMap{
				Data: map[string]string{
					"root": "test",
				},
			},
			expectedError:      awsv1alpha1.ErrInvalidConfigMap,
			checkOUMappingFunc: checkOUMapping,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := test.checkOUMappingFunc(&test.localObjects)
			assert.EqualValues(t, test.expectedError, err)
		})
	}
}

func TestValidateValue(t *testing.T) {
	emptyValue := ""
	filledValue := "value"
	tests := []struct {
		name          string
		value         *string
		expectedError error
		function      func(*string) error
	}{
		{
			name:          "Pass test",
			value:         &filledValue,
			expectedError: nil,
			function:      validateValue,
		},
		{
			name:          "Empty value",
			value:         &emptyValue,
			expectedError: awsv1alpha1.ErrUnexpectedValue,
			function:      validateValue,
		},
		{
			name:          "Nil value",
			value:         nil,
			expectedError: awsv1alpha1.ErrUnexpectedValue,
			function:      validateValue,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Test
			out := test.function(test.value)
			assert.EqualValues(t, test.expectedError, out)
		})
	}
}

func TestValidateOrganizationalUnitInput(t *testing.T) {
	// OrganizationalUnit list for testing
	name := "zerozero"
	parentID := "00"
	tests := []struct {
		name          string
		localObjects  organizations.CreateOrganizationalUnitInput
		expectedError error
		function      func(*organizations.CreateOrganizationalUnitInput) error
	}{
		{
			name: "Passing test",
			localObjects: organizations.CreateOrganizationalUnitInput{
				Name:     &name,
				ParentId: &parentID,
			},
			expectedError: nil,
			function:      validateOrganizationalUnitInput,
		},
		{
			name: "Two nil values",
			localObjects: organizations.CreateOrganizationalUnitInput{
				Name:     nil,
				ParentId: nil,
			},
			expectedError: awsv1alpha1.ErrUnexpectedValue,
			function:      validateOrganizationalUnitInput,
		},
		{
			name: "Name nil",
			localObjects: organizations.CreateOrganizationalUnitInput{
				Name:     nil,
				ParentId: &parentID,
			},
			expectedError: awsv1alpha1.ErrUnexpectedValue,
			function:      validateOrganizationalUnitInput,
		},
		{
			name: "ParentID nil",
			localObjects: organizations.CreateOrganizationalUnitInput{
				Name:     &name,
				ParentId: nil,
			},
			expectedError: awsv1alpha1.ErrUnexpectedValue,
			function:      validateOrganizationalUnitInput,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Test
			out := test.function(&test.localObjects)
			assert.EqualValues(t, test.expectedError, out)
		})
	}
}

func TestValidateListChildrenInput(t *testing.T) {
	// OrganizationalUnit list for testing
	childType := "zerozero"
	parentID := "00"
	tests := []struct {
		name          string
		localObjects  organizations.ListChildrenInput
		expectedError error
		function      func(*organizations.ListChildrenInput) error
	}{
		{
			name: "Passing test",
			localObjects: organizations.ListChildrenInput{
				ChildType: &childType,
				ParentId:  &parentID,
			},
			expectedError: nil,
			function:      validateListChildrenInput,
		},
		{
			name: "Two nil values",
			localObjects: organizations.ListChildrenInput{
				ChildType: nil,
				ParentId:  nil,
			},
			expectedError: awsv1alpha1.ErrUnexpectedValue,
			function:      validateListChildrenInput,
		},
		{
			name: "Name nil",
			localObjects: organizations.ListChildrenInput{
				ChildType: nil,
				ParentId:  &parentID,
			},
			expectedError: awsv1alpha1.ErrUnexpectedValue,
			function:      validateListChildrenInput,
		},
		{
			name: "ParentID nil",
			localObjects: organizations.ListChildrenInput{
				ChildType: &childType,
				ParentId:  nil,
			},
			expectedError: awsv1alpha1.ErrUnexpectedValue,
			function:      validateListChildrenInput,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Test
			out := test.function(&test.localObjects)
			assert.EqualValues(t, test.expectedError, out)
		})
	}
}

func TestValidateMoveAccount(t *testing.T) {
	// OrganizationalUnit list for testing
	accountID := "00"
	destinationParentID := "01"
	sourceParentID := "02"
	tests := []struct {
		name          string
		localObjects  organizations.MoveAccountInput
		expectedError error
		function      func(*organizations.MoveAccountInput) error
	}{
		{
			name: "Pass test",
			localObjects: organizations.MoveAccountInput{
				AccountId:           &accountID,
				DestinationParentId: &destinationParentID,
				SourceParentId:      &sourceParentID,
			},
			expectedError: nil,
			function:      validateMoveAccount,
		},
		{
			name: "Three nil values",
			localObjects: organizations.MoveAccountInput{
				AccountId:           nil,
				DestinationParentId: nil,
				SourceParentId:      nil,
			},
			expectedError: awsv1alpha1.ErrUnexpectedValue,
			function:      validateMoveAccount,
		},
		{
			name: "Destination and Source nil",
			localObjects: organizations.MoveAccountInput{
				AccountId:           &accountID,
				DestinationParentId: nil,
				SourceParentId:      nil,
			},
			expectedError: awsv1alpha1.ErrUnexpectedValue,
			function:      validateMoveAccount,
		},
		{
			name: "Account and Source nil",
			localObjects: organizations.MoveAccountInput{
				AccountId:           nil,
				DestinationParentId: &destinationParentID,
				SourceParentId:      nil,
			},
			expectedError: awsv1alpha1.ErrUnexpectedValue,
			function:      validateMoveAccount,
		},
		{
			name: "Account and Destination nil",
			localObjects: organizations.MoveAccountInput{
				AccountId:           nil,
				DestinationParentId: nil,
				SourceParentId:      &sourceParentID,
			},
			expectedError: awsv1alpha1.ErrUnexpectedValue,
			function:      validateMoveAccount,
		},
		{
			name: "Source nil",
			localObjects: organizations.MoveAccountInput{
				AccountId:           &accountID,
				DestinationParentId: &destinationParentID,
				SourceParentId:      nil,
			},
			expectedError: awsv1alpha1.ErrUnexpectedValue,
			function:      validateMoveAccount,
		},
		{
			name: "Account nil",
			localObjects: organizations.MoveAccountInput{
				AccountId:           nil,
				DestinationParentId: &destinationParentID,
				SourceParentId:      &sourceParentID,
			},
			expectedError: awsv1alpha1.ErrUnexpectedValue,
			function:      validateMoveAccount,
		},
		{
			name: "Destination nil",
			localObjects: organizations.MoveAccountInput{
				AccountId:           &accountID,
				DestinationParentId: nil,
				SourceParentId:      &sourceParentID,
			},
			expectedError: awsv1alpha1.ErrUnexpectedValue,
			function:      validateMoveAccount,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Test
			out := test.function(&test.localObjects)
			assert.EqualValues(t, test.expectedError, out)
		})
	}
}
