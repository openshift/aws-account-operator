package awsfederatedaccountaccess

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/sts"

	"github.com/golang/mock/gomock"

	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient/mock"
	"github.com/openshift/aws-account-operator/pkg/controller/testutils"
	"github.com/openshift/aws-account-operator/pkg/controller/utils"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type testAwsCustomPolicyBuilder struct {
	awsCustomPol awsv1alpha1.AWSCustomPolicy
}

func newTestAwsCustomPolicyBuilder() *testAwsCustomPolicyBuilder {
	return &testAwsCustomPolicyBuilder{
		awsCustomPol: awsv1alpha1.AWSCustomPolicy{
			Name:        "randomPolicy",
			Description: "randomDescription",
			Statements: []awsv1alpha1.StatementEntry{
				{
					Effect:   "",
					Action:   []string{""},
					Resource: []string{""},
					Condition: &awsv1alpha1.Condition{
						StringEquals: map[string]string{},
					},
					Principal: &awsv1alpha1.Principal{
						AWS: []string{},
					},
				}},
		},
	}
}

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

func TestCreateIAMPolicy(t *testing.T) {

	awsOutputPolicy := &iam.CreatePolicyOutput{
		Policy: &iam.Policy{},
	}

	tests := []struct {
		name                  string
		uidLabel              map[string]string
		createIAMPolicyOutput *iam.Policy
		expectedErr           error
	}{
		{
			name:                  "Test for UID label present",
			uidLabel:              map[string]string{"uid": "abcd"},
			createIAMPolicyOutput: awsOutputPolicy.Policy,
			expectedErr:           nil,
		},
		{
			name:                  "Test for UID label missing",
			uidLabel:              map[string]string{},
			createIAMPolicyOutput: nil,
			expectedErr:           errors.New("Failed to get UID label"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			mocks := setupDefaultMocks(t, []runtime.Object{})

			mockAWSClient := mock.NewMockClient(mocks.mockCtrl)

			defer mocks.mockCtrl.Finish()

			if test.createIAMPolicyOutput != nil {
				mockAWSClient.EXPECT().CreatePolicy(gomock.Any()).Return(
					awsOutputPolicy,
					nil,
				)
			}

			r := ReconcileAWSFederatedAccountAccess{}

			afr := awsv1alpha1.AWSFederatedRole{
				Spec: awsv1alpha1.AWSFederatedRoleSpec{
					AWSCustomPolicy: newTestAwsCustomPolicyBuilder().awsCustomPol},
			}

			afaa := awsv1alpha1.AWSFederatedAccountAccess{
				ObjectMeta: v1.ObjectMeta{
					Labels: test.uidLabel,
				}}

			createPolicyOutput, err := r.createIAMPolicy(mockAWSClient, afr, afaa)
			assert.Equal(t, err, test.expectedErr)
			assert.Equal(t, createPolicyOutput, test.createIAMPolicyOutput)
		})
	}

}

func TestCreateIAMRole(t *testing.T) {

	awsOutputRole := &iam.CreateRoleOutput{
		Role: &iam.Role{},
	}

	tests := []struct {
		name                string
		uidLabel            map[string]string
		createIAMRoleOutput *iam.Role
		expectedErr         error
	}{
		{
			name:                "Test for UID label present",
			uidLabel:            map[string]string{"uid": "abcd"},
			createIAMRoleOutput: awsOutputRole.Role,
			expectedErr:         nil,
		},
		{
			name:                "Test for UID label missing",
			uidLabel:            map[string]string{},
			createIAMRoleOutput: nil,
			expectedErr:         errors.New("Failed to get UID label"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			mocks := setupDefaultMocks(t, []runtime.Object{})

			mockAWSClient := mock.NewMockClient(mocks.mockCtrl)

			defer mocks.mockCtrl.Finish()

			if test.createIAMRoleOutput != nil {
				mockAWSClient.EXPECT().CreateRole(gomock.Any()).Return(
					awsOutputRole,
					nil,
				)
			}

			r := ReconcileAWSFederatedAccountAccess{}

			afr := awsv1alpha1.AWSFederatedRole{
				ObjectMeta: v1.ObjectMeta{
					Name: "",
				},
				Spec: awsv1alpha1.AWSFederatedRoleSpec{
					AWSCustomPolicy: newTestAwsCustomPolicyBuilder().awsCustomPol},
			}

			afaa := awsv1alpha1.AWSFederatedAccountAccess{
				ObjectMeta: v1.ObjectMeta{
					Labels: test.uidLabel},
				Spec: awsv1alpha1.AWSFederatedAccountAccessSpec{
					ExternalCustomerAWSIAMARN: "",
				},
			}

			createRoleOutput, err := r.createIAMRole(mockAWSClient, afr, afaa)
			assert.Equal(t, err, test.expectedErr)
			assert.Equal(t, createRoleOutput, test.createIAMRoleOutput)
		})
	}
}

func TestCreateOrUpdateIAMPolicy(t *testing.T) {

	mocks := setupDefaultMocks(t, []runtime.Object{})

	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)

	defer mocks.mockCtrl.Finish()

	afr := awsv1alpha1.AWSFederatedRole{
		Spec: awsv1alpha1.AWSFederatedRoleSpec{
			AWSCustomPolicy: newTestAwsCustomPolicyBuilder().awsCustomPol,
		},
	}

	afaa := awsv1alpha1.AWSFederatedAccountAccess{
		ObjectMeta: v1.ObjectMeta{
			Labels: map[string]string{"uid": ""}},
	}

	uidLabel := afaa.Labels["uid"]
	policyName := afr.Spec.AWSCustomPolicy.Name + "-" + uidLabel

	jsonPolicyDoc, err := utils.MarshalIAMPolicy(afr)
	if err != nil {
		t.Error("failed to get json policy")
	}

	awsOutputGci := &sts.GetCallerIdentityOutput{
		Account: aws.String(""),
	}

	mockAWSClient.EXPECT().GetCallerIdentity(gomock.Any()).Return(
		awsOutputGci,
		nil,
	)

	customPolArns := createPolicyArns(*awsOutputGci.Account, []string{afr.Spec.AWSCustomPolicy.Name + "-" + uidLabel}, false)

	mockAWSClient.EXPECT().CreatePolicy(&iam.CreatePolicyInput{
		PolicyName:     aws.String(policyName),
		Description:    aws.String(afr.Spec.AWSCustomPolicy.Description),
		PolicyDocument: aws.String(string(jsonPolicyDoc)),
	}).Return(
		nil,
		awserr.New("EntityAlreadyExists", "", nil),
	)

	mockAWSClient.EXPECT().DeletePolicy(&iam.DeletePolicyInput{PolicyArn: aws.String(customPolArns[0])}).Return(
		&iam.DeletePolicyOutput{},
		nil,
	)

	mockAWSClient.EXPECT().CreatePolicy(&iam.CreatePolicyInput{
		PolicyName:     aws.String(policyName),
		Description:    aws.String(afr.Spec.AWSCustomPolicy.Description),
		PolicyDocument: aws.String(string(jsonPolicyDoc)),
	}).Return(
		&iam.CreatePolicyOutput{
			Policy: &iam.Policy{},
		},
		nil,
	)

	r := ReconcileAWSFederatedAccountAccess{}

	err = r.createOrUpdateIAMPolicy(mockAWSClient, afr, afaa)
	assert.Nil(t, err)
}

func TestCreateOrUpdateIAMRole(t *testing.T) {

	mocks := setupDefaultMocks(t, []runtime.Object{})

	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)

	defer mocks.mockCtrl.Finish()

	afr := awsv1alpha1.AWSFederatedRole{
		Spec: awsv1alpha1.AWSFederatedRoleSpec{
			AWSCustomPolicy: newTestAwsCustomPolicyBuilder().awsCustomPol},
	}

	afaa := awsv1alpha1.AWSFederatedAccountAccess{
		ObjectMeta: v1.ObjectMeta{
			Labels: map[string]string{"uid": ""}},
	}

	type awsStatement struct {
		Effect    string                 `json:"Effect"`
		Action    []string               `json:"Action"`
		Resource  []string               `json:"Resource,omitempty"`
		Principal *awsv1alpha1.Principal `json:"Principal,omitempty"`
	}

	assumeRolePolicyDoc := struct {
		Version   string
		Statement []awsStatement
	}{
		Version: "2012-10-17",
		Statement: []awsStatement{{
			Effect: "Allow",
			Action: []string{"sts:AssumeRole"},
			Principal: &awsv1alpha1.Principal{
				AWS: []string{afaa.Spec.ExternalCustomerAWSIAMARN},
			},
		}},
	}

	jsonAssumeRolePolicyDoc, err := json.Marshal(&assumeRolePolicyDoc)
	if err != nil {
		t.Error("failed to get jsonAssumeRolePolicyDoc")
	}

	uidLabel := afaa.Labels["uid"]
	roleName := afaa.Spec.AWSFederatedRole.Name + "-" + uidLabel

	createRoleOutput := &iam.CreateRoleOutput{
		Role: &iam.Role{},
	}

	mockAWSClient.EXPECT().CreateRole(&iam.CreateRoleInput{
		RoleName:                 aws.String(roleName),
		Description:              aws.String(afr.Spec.RoleDescription),
		AssumeRolePolicyDocument: aws.String(string(jsonAssumeRolePolicyDoc)),
	}).Return(
		nil,
		awserr.New("EntityAlreadyExists", "", nil),
	)

	mockAWSClient.EXPECT().DeleteRole(&iam.DeleteRoleInput{
		RoleName: aws.String(roleName)}).Return(
		&iam.DeleteRoleOutput{},
		nil,
	)

	mockAWSClient.EXPECT().CreateRole(&iam.CreateRoleInput{
		RoleName:                 aws.String(roleName),
		Description:              aws.String(afr.Spec.RoleDescription),
		AssumeRolePolicyDocument: aws.String(string(jsonAssumeRolePolicyDoc)),
	}).Return(
		createRoleOutput,
		nil,
	)

	r := ReconcileAWSFederatedAccountAccess{}
	nullLogger := testutils.NullLogger{}

	outputRole, err := r.createOrUpdateIAMRole(mockAWSClient, afr, afaa, nullLogger)
	assert.Equal(t, outputRole, createRoleOutput.Role)
	assert.Nil(t, err)
}

func TestAttachIAMPolicies(t *testing.T) {

	mocks := setupDefaultMocks(t, []runtime.Object{})

	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)

	defer mocks.mockCtrl.Finish()

	roleName := "abcd"
	policyArns := []string{"xyz"}

	awsOutputAttachPolicies := &iam.AttachRolePolicyOutput{}

	mockAWSClient.EXPECT().AttachRolePolicy(gomock.Any()).Return(
		awsOutputAttachPolicies,
		nil,
	)

	r := ReconcileAWSFederatedAccountAccess{}

	err := r.attachIAMPolices(mockAWSClient, roleName, policyArns)
	assert.Nil(t, err)
}

func TestCreatePolicyArns(t *testing.T) {

	tests := []struct {
		name               string
		awsManaged         bool
		policyNames        []string
		accountId          string
		expectedPolicyArns []string
	}{
		{
			name:               "Test for aws managed policy",
			awsManaged:         true,
			policyNames:        []string{"abcd"},
			accountId:          "",
			expectedPolicyArns: []string{"arn:aws:iam::aws:policy/abcd"},
		},
		{
			name:               "Test for non aws managed policy",
			awsManaged:         false,
			policyNames:        []string{"abcd"},
			accountId:          "111111111111",
			expectedPolicyArns: []string{"arn:aws:iam::111111111111:policy/abcd"},
		},
	}

	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := createPolicyArns(test.accountId, test.policyNames, test.awsManaged)
				for _, r := range result {
					for _, arn := range test.expectedPolicyArns {
						if r != arn {
							t.Errorf("expected %s got %s", arn, r)
						}
					}
				}
			},
		)
	}
}
