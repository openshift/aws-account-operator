package accountclaim

import (
	"testing"

	"github.com/aws/aws-sdk-go/service/organizations"
	"github.com/go-logr/logr"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"

	awsaccountapis "github.com/openshift/aws-account-operator/pkg/apis"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/awsclient/mock"
)

func TestFindOUIDFromName(t *testing.T) {
	awsaccountapis.AddToScheme(scheme.Scheme)
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
			findOUIDFromNameFunc: findOUIDFromName,
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
	awsaccountapis.AddToScheme(scheme.Scheme)
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
