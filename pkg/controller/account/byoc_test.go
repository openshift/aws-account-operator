package account

import (
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/golang/mock/gomock"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient/mock"
	"github.com/openshift/aws-account-operator/pkg/controller/testutils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Byoc", func() {
	var (
		nullLogger    testutils.NullLogger
		mockAWSClient *mock.MockClient
		policyFake    *iam.AttachedPolicy
		userARN       string
	)

	BeforeEach(func() {
		nullLogger = testutils.NullLogger{}
		ctrl := gomock.NewController(GinkgoT())
		mockAWSClient = mock.NewMockClient(ctrl)
		policyFake = &iam.AttachedPolicy{
			PolicyArn:  aws.String("arn:aws:iam::123456789012:policy/ManagedPolicyName"),
			PolicyName: aws.String("ManagedPolicyName"),
		}
		userARN = "arn:aws:iam::123456789012:user/JohnDoe"
	})

	Context("Testing GetExistingRole", func() {
		It("Returns the role when role exists", func() {
			mockAWSClient.EXPECT().GetRole(gomock.Any()).Return(&iam.GetRoleOutput{Role: &iam.Role{RoleId: aws.String("AROA1234567890EXAMPLE")}}, nil).AnyTimes()
			_, err := GetExistingRole(nullLogger, "roleName", mockAWSClient)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Catches the error when role doesn't exist", func() {
			mockAWSClient.EXPECT().GetRole(gomock.Any()).Return(nil, awserr.New(iam.ErrCodeNoSuchEntityException, "Role does not exist", nil)).AnyTimes()
			role, err := GetExistingRole(nullLogger, "roleName", mockAWSClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(role).To(BeEquivalentTo(&iam.GetRoleOutput{}))
		})

		It("Throws error on AWS Service Failure", func() {
			mockAWSClient.EXPECT().GetRole(gomock.Any()).Return(nil, awserr.New(iam.ErrCodeServiceFailureException, "AWS Service Failure", nil)).AnyTimes()
			_, err := GetExistingRole(nullLogger, "roleName", mockAWSClient)
			Expect(err).To(HaveOccurred())
		})

		It("Throws error on Unexpected AWS Error", func() {
			mockAWSClient.EXPECT().GetRole(gomock.Any()).Return(nil, awserr.New("ErrorCodeThatDoesntExist", "No such thing", nil)).AnyTimes()
			_, err := GetExistingRole(nullLogger, "roleName", mockAWSClient)
			Expect(err).To(HaveOccurred())
		})

		It("Throws error on non-aws Error", func() {
			mockAWSClient.EXPECT().GetRole(gomock.Any()).Return(nil, errors.New("NonAWSError")).AnyTimes()
			_, err := GetExistingRole(nullLogger, "roleName", mockAWSClient)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("Testing GetAttachedPolicies", func() {
		It("Throws an error on any AWS error", func() {
			mockAWSClient.EXPECT().ListAttachedRolePolicies(gomock.Any()).Return(nil, awserr.New("AWSError", "Some AWS Error", nil)).AnyTimes()
			_, err := GetAttachedPolicies(nullLogger, "roleName", mockAWSClient)
			Expect(err).To(HaveOccurred())
		})

		It("Throws an error on any Non-AWS error", func() {
			mockAWSClient.EXPECT().ListAttachedRolePolicies(gomock.Any()).Return(nil, errors.New("NonAWSError")).AnyTimes()
			_, err := GetAttachedPolicies(nullLogger, "roleName", mockAWSClient)
			Expect(err).To(HaveOccurred())
		})

		It("Returns a list of Policies when no errors happen", func() {
			response := &iam.ListAttachedRolePoliciesOutput{
				AttachedPolicies: []*iam.AttachedPolicy{policyFake},
			}
			mockAWSClient.EXPECT().ListAttachedRolePolicies(gomock.Any()).Return(response, nil).AnyTimes()
			policyList, err := GetAttachedPolicies(nullLogger, "roleName", mockAWSClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(policyList.AttachedPolicies).To(HaveLen(1))
		})
	})

	Context("Testing DetachPolicyFromRole", func() {
		It("Works properly without error", func() {
			mockAWSClient.EXPECT().DetachRolePolicy(gomock.Any()).Return(&iam.DetachRolePolicyOutput{}, nil).AnyTimes()
			err := DetachPolicyFromRole(nullLogger, policyFake, "roleName", mockAWSClient)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Throws an error on any AWS error", func() {
			mockAWSClient.EXPECT().DetachRolePolicy(gomock.Any()).Return(nil, awserr.New("AWSError", "Some AWS Error", nil)).AnyTimes()
			err := DetachPolicyFromRole(nullLogger, policyFake, "roleName", mockAWSClient)
			Expect(err).To(HaveOccurred())
		})

		It("Throws an error on any Non-AWS error", func() {
			mockAWSClient.EXPECT().DetachRolePolicy(gomock.Any()).Return(nil, errors.New("NonAWSError")).AnyTimes()
			err := DetachPolicyFromRole(nullLogger, policyFake, "roleName", mockAWSClient)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("Testing DeleteRole", func() {
		It("Works properly without error", func() {
			mockAWSClient.EXPECT().DeleteRole(gomock.Any()).Return(&iam.DeleteRoleOutput{}, nil).AnyTimes()
			err := DeleteRole(nullLogger, "roleName", mockAWSClient)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Throws an error on any AWS error", func() {
			mockAWSClient.EXPECT().DeleteRole(gomock.Any()).Return(nil, awserr.New("AWSError", "Some AWS Error", nil)).AnyTimes()
			err := DeleteRole(nullLogger, "roleName", mockAWSClient)
			Expect(err).To(HaveOccurred())
		})

		It("Throws an error on any Non-AWS error", func() {
			mockAWSClient.EXPECT().DeleteRole(gomock.Any()).Return(nil, errors.New("NonAWSError")).AnyTimes()
			err := DeleteRole(nullLogger, "roleName", mockAWSClient)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("Testing CreateRole", func() {
		It("Works properly without error", func() {
			mockAWSClient.EXPECT().CreateRole(gomock.Any()).Return(&iam.CreateRoleOutput{Role: &iam.Role{RoleId: aws.String("AROA1234567890EXAMPLE")}}, nil).AnyTimes()
			roleID, err := CreateRole(nullLogger, "roleName", userARN, mockAWSClient, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(roleID).To(Equal("AROA1234567890EXAMPLE"))
		})

		It("Throws an error on any AWS error", func() {
			mockAWSClient.EXPECT().CreateRole(gomock.Any()).Return(nil, awserr.New("AWSError", "Some AWS Error", nil)).AnyTimes()
			_, err := CreateRole(nullLogger, "roleName", userARN, mockAWSClient, nil)
			Expect(err).To(HaveOccurred())
		})

		It("Throws an error on any Non-AWS error", func() {
			mockAWSClient.EXPECT().CreateRole(gomock.Any()).Return(nil, errors.New("NonAWSError")).AnyTimes()
			_, err := CreateRole(nullLogger, "roleName", userARN, mockAWSClient, nil)
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
