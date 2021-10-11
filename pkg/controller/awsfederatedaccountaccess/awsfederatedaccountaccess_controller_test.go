package awsfederatedaccountaccess

import (
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"

	"github.com/golang/mock/gomock"

	"github.com/openshift/aws-account-operator/pkg/awsclient/mock"
	"github.com/openshift/aws-account-operator/pkg/controller/testutils"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
)

type mocks struct {
	mockCtrl *gomock.Controller
}

func setupDefaultMocks(t *testing.T, localObjects []runtime.Object) *mocks {
	mocks := &mocks{
		mockCtrl: gomock.NewController(t),
	}

	return mocks
}

func TestCheckAndDeletePolicy(t *testing.T) {

	tests := []struct {
		name      string
		awsOutput *iam.DeletePolicyOutput
		err       error
	}{
		{
			name:      "No error",
			awsOutput: &iam.DeletePolicyOutput{},
			err:       nil,
		},
		{
			name:      "TestNoSuchEntity",
			awsOutput: nil,
			err:       awserr.New(iam.ErrCodeNoSuchEntityException, "", nil),
		},
		{
			name:      "TestLimitExceeded",
			awsOutput: nil,
			err:       awserr.New(iam.ErrCodeLimitExceededException, "", nil),
		},
		{
			name:      "TestInvalidInput",
			awsOutput: nil,
			err:       awserr.New(iam.ErrCodeInvalidInputException, "", nil),
		},
		{
			name:      "TestDeleteConflict",
			awsOutput: nil,
			err:       awserr.New(iam.ErrCodeDeleteConflictException, "", nil),
		},
		{
			name:      "TestServiceFailure",
			awsOutput: nil,
			err:       awserr.New(iam.ErrCodeServiceFailureException, "", nil),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			policyArn := aws.String("randPolicyArn")
			uidLabel := "randLabel"
			crPolicyName := "randPolicyName-randLabel"
			policyName := "randPolicyName-randLabel"

			mocks := setupDefaultMocks(t, []runtime.Object{})

			mockAWSClient := mock.NewMockClient(mocks.mockCtrl)

			mockAWSClient.EXPECT().DeletePolicy(
				&iam.DeletePolicyInput{PolicyArn: policyArn}).Return(test.awsOutput, test.err)

			nullLogger := testutils.NullLogger{}
			err := checkAndDeletePolicy(nullLogger, mockAWSClient, uidLabel, crPolicyName, &policyName, policyArn)
			if test.err != nil {
				assert.Equal(t, test.err, err)
			} else {
				assert.Nil(t, err)
			}
		},
		)
	}

}

func TestGetPolicyNameWithUID(t *testing.T) {
	testData := []struct {
		name                 string
		crPolicyName         string
		expectedCrPolicyName string
	}{
		{
			name:                 "test for uid label present",
			crPolicyName:         "randPolicy-randLabel",
			expectedCrPolicyName: "randPolicy-randLabel",
		},
		{
			name:                 "test for uid label not present",
			crPolicyName:         "randPolicy",
			expectedCrPolicyName: "randPolicy-randLabel",
		},
	}

	awsCustomPolicyname := "randCustomPolicyName"
	uidLabel := "randLabel"

	for _, test := range testData {
		t.Run(test.name, func(t *testing.T) {
			returnVal := getPolicyNameWithUID(awsCustomPolicyname, test.crPolicyName, uidLabel)
			if returnVal != test.expectedCrPolicyName {
				t.Errorf("expected return value %s and got %s", test.expectedCrPolicyName, returnVal)
			}
		})
	}
}
