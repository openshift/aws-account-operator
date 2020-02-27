package account

import (
	"testing"

	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/go-logr/logr"
	"github.com/golang/mock/gomock"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/awsclient/mock"
	"github.com/stretchr/testify/assert"
)

func TestDeleteOtherAccessKeys(t *testing.T) {
	// mock
	idZero := "000"
	idOne := "001"
	idTwo := "002"
	accessKeyMeta := []*iam.AccessKeyMetadata{
		{
			AccessKeyId: &idZero,
		},
		{
			AccessKeyId: &idOne,
		},
		{
			AccessKeyId: &idTwo,
		},
	}

	// Define tests
	tests := []struct {
		name                      string
		accessKeyID               string
		listAccessKeyOutput       *iam.ListAccessKeysOutput
		deleteAccessKeyErr        error
		outputErr                 error
		DeleteOtherAccessKeysFunc func(reqLogger logr.Logger, client awsclient.Client, accessKeyID string) error
	}{
		{
			name:        "Correct key to hold",
			accessKeyID: "001",
			listAccessKeyOutput: &iam.ListAccessKeysOutput{
				AccessKeyMetadata: accessKeyMeta,
			},
			deleteAccessKeyErr:        nil,
			outputErr:                 nil,
			DeleteOtherAccessKeysFunc: DeleteOtherAccessKeys,
		},
		{
			name:        "Incorect key to hold",
			accessKeyID: "004",
			listAccessKeyOutput: &iam.ListAccessKeysOutput{
				AccessKeyMetadata: accessKeyMeta,
			},
			deleteAccessKeyErr:        nil,
			outputErr:                 nil,
			DeleteOtherAccessKeysFunc: DeleteOtherAccessKeys,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// build mock
			ctrl := gomock.NewController(t)
			mocks := mock.NewMockClient(ctrl)
			mocks.EXPECT().ListAccessKeys(&iam.ListAccessKeysInput{}).AnyTimes().Return(test.listAccessKeyOutput, nil)
			mocks.EXPECT().DeleteAccessKey(gomock.Any()).AnyTimes().Return(&iam.DeleteAccessKeyOutput{}, test.deleteAccessKeyErr)
			reqLogger := log.WithValues()
			// Test
			err := test.DeleteOtherAccessKeysFunc(reqLogger, mocks, test.accessKeyID)
			assert.EqualValues(t, test.outputErr, err)
		})
	}
}
