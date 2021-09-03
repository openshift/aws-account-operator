package account

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/golang/mock/gomock"
	"github.com/openshift/aws-account-operator/pkg/apis"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient/mock"
	"github.com/openshift/aws-account-operator/pkg/controller/testutils"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Byoc", func() {
	var (
		nullLogger    testutils.NullLogger
		mockAWSClient *mock.MockClient
		policyFake    *iam.AttachedPolicy
		userARN       string
		ctrl          *gomock.Controller
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockAWSClient = mock.NewMockClient(ctrl)
		policyFake = &iam.AttachedPolicy{
			PolicyArn:  aws.String("arn:aws:iam::123456789012:policy/ManagedPolicyName"),
			PolicyName: aws.String("ManagedPolicyName"),
		}
		userARN = "arn:aws:iam::123456789012:user/JohnDoe"
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("Testing GetExistingRole", func() {
		It("Returns the role when role exists", func() {
			mockAWSClient.EXPECT().GetRole(gomock.Any()).Return(&iam.GetRoleOutput{Role: &iam.Role{RoleId: aws.String("AROA1234567890EXAMPLE")}}, nil)
			_, err := GetExistingRole(nullLogger, "roleName", mockAWSClient)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Catches the error when role doesn't exist", func() {
			mockAWSClient.EXPECT().GetRole(gomock.Any()).Return(nil, awserr.New(iam.ErrCodeNoSuchEntityException, "Role does not exist", nil))
			role, err := GetExistingRole(nullLogger, "roleName", mockAWSClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(role).To(BeEquivalentTo(&iam.GetRoleOutput{}))
		})

		It("Throws error on AWS Service Failure", func() {
			mockAWSClient.EXPECT().GetRole(gomock.Any()).Return(nil, awserr.New(iam.ErrCodeServiceFailureException, "AWS Service Failure", nil))
			_, err := GetExistingRole(nullLogger, "roleName", mockAWSClient)
			Expect(err).To(HaveOccurred())
		})

		It("Throws error on Unexpected AWS Error", func() {
			mockAWSClient.EXPECT().GetRole(gomock.Any()).Return(nil, awserr.New("ErrorCodeThatDoesntExist", "No such thing", nil))
			_, err := GetExistingRole(nullLogger, "roleName", mockAWSClient)
			Expect(err).To(HaveOccurred())
		})

		It("Throws error on non-aws Error", func() {
			mockAWSClient.EXPECT().GetRole(gomock.Any()).Return(nil, errors.New("NonAWSError"))
			_, err := GetExistingRole(nullLogger, "roleName", mockAWSClient)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("Testing GetAttachedPolicies", func() {
		It("Throws an error on any AWS error", func() {
			mockAWSClient.EXPECT().ListAttachedRolePolicies(gomock.Any()).Return(nil, awserr.New("AWSError", "Some AWS Error", nil))
			_, err := GetAttachedPolicies(nullLogger, "roleName", mockAWSClient)
			Expect(err).To(HaveOccurred())
		})

		It("Throws an error on any Non-AWS error", func() {
			mockAWSClient.EXPECT().ListAttachedRolePolicies(gomock.Any()).Return(nil, errors.New("NonAWSError"))
			_, err := GetAttachedPolicies(nullLogger, "roleName", mockAWSClient)
			Expect(err).To(HaveOccurred())
		})

		It("Returns a list of Policies when no errors happen", func() {
			response := &iam.ListAttachedRolePoliciesOutput{
				AttachedPolicies: []*iam.AttachedPolicy{policyFake},
			}
			mockAWSClient.EXPECT().ListAttachedRolePolicies(gomock.Any()).Return(response, nil)
			policyList, err := GetAttachedPolicies(nullLogger, "roleName", mockAWSClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(policyList.AttachedPolicies).To(HaveLen(1))
		})
	})

	Context("Testing DetachPolicyFromRole", func() {
		It("Works properly without error", func() {
			mockAWSClient.EXPECT().DetachRolePolicy(gomock.Any()).Return(&iam.DetachRolePolicyOutput{}, nil)
			err := DetachPolicyFromRole(nullLogger, policyFake, "roleName", mockAWSClient)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Throws an error on any AWS error", func() {
			mockAWSClient.EXPECT().DetachRolePolicy(gomock.Any()).Return(nil, awserr.New("AWSError", "Some AWS Error", nil))
			err := DetachPolicyFromRole(nullLogger, policyFake, "roleName", mockAWSClient)
			Expect(err).To(HaveOccurred())
		})

		It("Throws an error on any Non-AWS error", func() {
			mockAWSClient.EXPECT().DetachRolePolicy(gomock.Any()).Return(nil, errors.New("NonAWSError"))
			err := DetachPolicyFromRole(nullLogger, policyFake, "roleName", mockAWSClient)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("Testing DeleteRole", func() {
		It("Works properly without error", func() {
			mockAWSClient.EXPECT().DeleteRole(gomock.Any()).Return(&iam.DeleteRoleOutput{}, nil)
			err := DeleteRole(nullLogger, "roleName", mockAWSClient)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Throws an error on any AWS error", func() {
			mockAWSClient.EXPECT().DeleteRole(gomock.Any()).Return(nil, awserr.New("AWSError", "Some AWS Error", nil))
			err := DeleteRole(nullLogger, "roleName", mockAWSClient)
			Expect(err).To(HaveOccurred())
		})

		It("Throws an error on any Non-AWS error", func() {
			mockAWSClient.EXPECT().DeleteRole(gomock.Any()).Return(nil, errors.New("NonAWSError"))
			err := DeleteRole(nullLogger, "roleName", mockAWSClient)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("Testing DeleteBYOCAdminAccessRole", func() {
		It("Doesn't have RolePolicy attached - Works properly without error", func() {
			mockAWSClient.EXPECT().ListAttachedRolePolicies(gomock.Any()).Return(&iam.ListAttachedRolePoliciesOutput{}, nil)
			mockAWSClient.EXPECT().DeleteRole(gomock.Any()).Return(&iam.DeleteRoleOutput{}, nil)
			err := DeleteBYOCAdminAccessRole(nullLogger, mockAWSClient, "roleName")
			Expect(err).NotTo(HaveOccurred())
		})

		It("Doesn't has RolePolicy attached - Works properly without error", func() {
			mockAWSClient.EXPECT().ListAttachedRolePolicies(gomock.Any()).Return(
				&iam.ListAttachedRolePoliciesOutput{
					AttachedPolicies: []*iam.AttachedPolicy{
						{
							PolicyArn:  aws.String("PolicyArn"),
							PolicyName: aws.String("PolicyName"),
						},
					},
				},
				nil,
			)
			mockAWSClient.EXPECT().DetachRolePolicy(gomock.Any()).Return(&iam.DetachRolePolicyOutput{}, nil)
			mockAWSClient.EXPECT().DeleteRole(gomock.Any()).Return(&iam.DeleteRoleOutput{}, nil)
			err := DeleteBYOCAdminAccessRole(nullLogger, mockAWSClient, "roleName")
			Expect(err).NotTo(HaveOccurred())
		})

		It("Throws an error on any AWS error on GetAttachedPolicies", func() {
			mockAWSClient.EXPECT().ListAttachedRolePolicies(gomock.Any()).Return(nil, awserr.New("AWSError", "Some AWS Error", nil))
			err := DeleteBYOCAdminAccessRole(nullLogger, mockAWSClient, "roleName")
			Expect(err).To(HaveOccurred())
		})

		It("Throws an error on any AWS error on DetachRolePolicy", func() {
			mockAWSClient.EXPECT().ListAttachedRolePolicies(gomock.Any()).Return(
				&iam.ListAttachedRolePoliciesOutput{
					AttachedPolicies: []*iam.AttachedPolicy{
						{
							PolicyArn:  aws.String("PolicyArn"),
							PolicyName: aws.String("PolicyName"),
						},
					},
				},
				nil,
			)
			mockAWSClient.EXPECT().DetachRolePolicy(gomock.Any()).Return(nil, awserr.New("AWSError", "Some AWS Error", nil))
			err := DeleteBYOCAdminAccessRole(nullLogger, mockAWSClient, "roleName")
			Expect(err).To(HaveOccurred())
		})
	})

	Context("Testing CreateRole", func() {
		It("Works properly without error", func() {
			mockAWSClient.EXPECT().CreateRole(gomock.Any()).Return(&iam.CreateRoleOutput{Role: &iam.Role{RoleId: aws.String("AROA1234567890EXAMPLE")}}, nil)
			roleID, err := CreateRole(nullLogger, "roleName", []string{userARN, "arn2"}, mockAWSClient, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(roleID).To(Equal("AROA1234567890EXAMPLE"))
		})

		It("Throws an error on any AWS error", func() {
			mockAWSClient.EXPECT().CreateRole(gomock.Any()).Return(nil, awserr.New("AWSError", "Some AWS Error", nil))
			_, err := CreateRole(nullLogger, "roleName", []string{userARN, "arn2"}, mockAWSClient, nil)
			Expect(err).To(HaveOccurred())
		})

		It("Throws an error on any Non-AWS error", func() {
			mockAWSClient.EXPECT().CreateRole(gomock.Any()).Return(nil, errors.New("NonAWSError"))
			_, err := CreateRole(nullLogger, "roleName", []string{userARN}, mockAWSClient, nil)
			Expect(err).To(HaveOccurred())
		})
	})
})

// These AccountStatus should all be evaluated as new
var testNewBYOCAccountInstances = []*awsv1alpha1.Account{
	{
		Spec: awsv1alpha1.AccountSpec{
			BYOC: true,
		},
		Status: awsv1alpha1.AccountStatus{
			Claimed: true,
			State:   "",
		},
	},
	{
		Spec: awsv1alpha1.AccountSpec{
			BYOC: true,
		},
		Status: awsv1alpha1.AccountStatus{
			Claimed: false,
			State:   "",
		},
	},
	{
		Spec: awsv1alpha1.AccountSpec{
			BYOC: true,
		},
		Status: awsv1alpha1.AccountStatus{
			Claimed: false,
			State:   "test state",
		},
	},
}

// This AccountStatus should be evaluated as NOT new
var testNotNewBYOCAccountInstances = []*awsv1alpha1.Account{
	{
		Spec: awsv1alpha1.AccountSpec{
			BYOC: true,
		},
		Status: awsv1alpha1.AccountStatus{
			Claimed: true,
			State:   "test state",
		},
	},
	{
		Spec: awsv1alpha1.AccountSpec{
			BYOC: false,
		},
		Status: awsv1alpha1.AccountStatus{
			Claimed: true,
			State:   "",
		},
	},
	{
		Spec: awsv1alpha1.AccountSpec{
			BYOC: false,
		},
		Status: awsv1alpha1.AccountStatus{
			Claimed: false,
			State:   "",
		},
	},
	{
		Spec: awsv1alpha1.AccountSpec{
			BYOC: false,
		},
		Status: awsv1alpha1.AccountStatus{
			Claimed: false,
			State:   "test state",
		},
	},
}

func TestNewBYOCAccount(t *testing.T) {
	for index, acct := range testNewBYOCAccountInstances {
		new := newBYOCAccount(acct)
		expected := true
		if new != expected {
			t.Error(
				"for account index:", index,
				"expected:", expected,
				"got:", new,
			)
		}
	}
}

func TestNotNewBYOCAccount(t *testing.T) {
	for index, acct := range testNotNewBYOCAccountInstances {
		new := newBYOCAccount(acct)
		expected := false
		if new != expected {
			t.Error(
				"for account index:", index,
				"expected:", expected,
				"got:", new,
			)
		}
	}
}

func TestClaimBYOCAccount(t *testing.T) {
	nullLogger := testutils.NullLogger{}
	tests := []struct {
		name           string
		acct           *awsv1alpha1.Account
		expectedResult error
	}{
		{
			name: "Account Already Claimed",
			acct: &awsv1alpha1.Account{
				Status: awsv1alpha1.AccountStatus{
					Claimed: true,
				},
			},
			expectedResult: nil,
		},
		{
			name: "Account unclaimed - Claiming",
			acct: &awsv1alpha1.Account{
				Status: awsv1alpha1.AccountStatus{
					Claimed: false,
				},
			},
			expectedResult: nil,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {

				err := apis.AddToScheme(scheme.Scheme)
				if err != nil {
					fmt.Printf("failed adding to scheme in byoc_test.go")
				}

				mocks := setupDefaultMocks(t, []runtime.Object{
					test.acct,
				})
				defer mocks.mockCtrl.Finish()

				r := ReconcileAccount{
					Client: mocks.fakeKubeClient,
					scheme: scheme.Scheme,
				}

				result := claimBYOCAccount(&r, nullLogger, test.acct)
				assert.Equal(t, test.expectedResult, result)
			},
		)
	}
}

func TestInitializeNewCCSAccount(t *testing.T) {

	nullLogger := testutils.NullLogger{}
	tests := []struct {
		name           string
		acct           *awsv1alpha1.Account
		localObjects   []runtime.Object
		errExpected    bool
		expectedResult error
	}{
		{
			name: "Could not find AccountClaim",
			acct: &awsv1alpha1.Account{
				Status: awsv1alpha1.AccountStatus{
					Claimed: true,
				},
			},
			localObjects:   []runtime.Object{},
			errExpected:    true,
			expectedResult: &k8serr.StatusError{},
		},

		{
			name: "accountClaim validation fails",
			acct: &awsv1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "AccName",
					Namespace: awsv1alpha1.AccountCrNamespace,
				},
				Spec: awsv1alpha1.AccountSpec{
					BYOC: true,
				},
				Status: awsv1alpha1.AccountStatus{
					Claimed: false,
				},
			},
			localObjects: []runtime.Object{
				&awsv1alpha1.AccountClaim{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: awsv1alpha1.AccountCrNamespace,
					},
					Spec: awsv1alpha1.AccountClaimSpec{
						BYOC:                true,
						BYOCAWSAccountID:    "1234",
						BYOCSecretRef:       awsv1alpha1.SecretRef{},
						AwsCredentialSecret: awsv1alpha1.SecretRef{},
					},
				},
			},
			errExpected:    true,
			expectedResult: awsv1alpha1.ErrBYOCSecretRefMissing,
		},
		{
			name: "claimBYOCAccount returned error",
			acct: &awsv1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "AccName",
					Namespace: awsv1alpha1.AccountCrNamespace,
				},
				Spec: awsv1alpha1.AccountSpec{
					BYOC: true,
				},
				Status: awsv1alpha1.AccountStatus{
					Claimed: false,
				},
			},
			localObjects: []runtime.Object{
				&awsv1alpha1.AccountClaim{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: awsv1alpha1.AccountCrNamespace,
					},
					Spec: awsv1alpha1.AccountClaimSpec{
						BYOC:             true,
						BYOCAWSAccountID: "1234",
						BYOCSecretRef: awsv1alpha1.SecretRef{
							Name:      "SecretName",
							Namespace: "SecretNamespace",
						},
						AwsCredentialSecret: awsv1alpha1.SecretRef{
							Name:      "SecretName",
							Namespace: "SecretNamespace",
						},
					},
				},
			},
			errExpected:    true,
			expectedResult: &k8serr.StatusError{},
		},
		{
			name: "CCSAccount initialized successfully",
			acct: &awsv1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "AccName",
					Namespace: awsv1alpha1.AccountCrNamespace,
				},
				Spec: awsv1alpha1.AccountSpec{
					BYOC: true,
				},
				Status: awsv1alpha1.AccountStatus{
					Claimed: false,
				},
			},
			localObjects: []runtime.Object{
				&awsv1alpha1.Account{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "AccName",
						Namespace: awsv1alpha1.AccountCrNamespace,
					},
					Spec: awsv1alpha1.AccountSpec{
						BYOC: true,
					},
					Status: awsv1alpha1.AccountStatus{
						Claimed: false,
					},
				},
				&awsv1alpha1.AccountClaim{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: awsv1alpha1.AccountCrNamespace,
					},
					Spec: awsv1alpha1.AccountClaimSpec{
						BYOC:             true,
						BYOCAWSAccountID: "1234",
						BYOCSecretRef: awsv1alpha1.SecretRef{
							Name:      "SecretName",
							Namespace: "SecretNamespace",
						},
						AwsCredentialSecret: awsv1alpha1.SecretRef{
							Name:      "SecretName",
							Namespace: "SecretNamespace",
						},
					},
				},
			},
			errExpected:    false,
			expectedResult: nil,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {

				err := apis.AddToScheme(scheme.Scheme)
				if err != nil {
					fmt.Printf("failed adding to scheme in byoc_test.go")
				}

				mocks := setupDefaultMocks(t, test.localObjects)
				defer mocks.mockCtrl.Finish()

				r := ReconcileAccount{
					Client: mocks.fakeKubeClient,
					scheme: scheme.Scheme,
				}
				_, err = r.initializeNewCCSAccount(nullLogger, test.acct)
				if test.errExpected {
					assert.Error(t, err)
					assert.IsType(t, test.expectedResult, err)
				} else {
					assert.Nil(t, err)
				}
			},
		)
	}

}

func TestGetSREAccessARN(t *testing.T) {
	expectedARN := "MyExpectedARN"
	tests := []struct {
		name           string
		expectedErr    bool
		configMap      corev1.ConfigMap
		expectedArnVal string
	}{
		{
			name:        "Valid ConfigMap, Works",
			expectedErr: false,
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      awsv1alpha1.DefaultConfigMap,
					Namespace: awsv1alpha1.AccountCrNamespace,
				},
				Data: map[string]string{
					"CCS-Access-Arn": expectedARN,
				},
			},
			expectedArnVal: expectedARN,
		},
		{
			name:           "Can't find ConfigMap, Throws Error",
			expectedErr:    true,
			configMap:      corev1.ConfigMap{},
			expectedArnVal: "",
		},
		{
			name:        "Valid ConfigMap, No CCS-Access-Arn, Throws Error",
			expectedErr: true,
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      awsv1alpha1.DefaultConfigMap,
					Namespace: awsv1alpha1.AccountCrNamespace,
				},
				Data: map[string]string{},
			},
			expectedArnVal: "",
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {

				objs := []runtime.Object{&test.configMap}
				mocks := setupDefaultMocks(t, objs)
				nullLogger := testutils.NullLogger{}
				defer mocks.mockCtrl.Finish()

				r := ReconcileAccount{
					Client: mocks.fakeKubeClient,
					scheme: scheme.Scheme,
				}

				retVal, err := r.GetSREAccessARN(nullLogger)
				assert.Equal(t, test.expectedArnVal, retVal)
				if test.expectedErr {
					assert.Error(t, err)
				}
			},
		)
	}
}
