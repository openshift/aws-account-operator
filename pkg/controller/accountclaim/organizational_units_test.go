package accountclaim

import (
	"testing"

	"github.com/aws/aws-sdk-go/service/organizations"
	"github.com/go-logr/logr"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"

	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/awsclient/mock"
)

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
