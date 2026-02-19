package sts

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	ststypes "github.com/aws/aws-sdk-go-v2/service/sts/types"
	"github.com/aws/smithy-go"
	"github.com/openshift/aws-account-operator/pkg/awsclient/mock"
	"github.com/openshift/aws-account-operator/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestGetSTSCredentials(t *testing.T) {

	mockCtrl := gomock.NewController(t)
	nullLogger := testutils.NewTestLogger().Logger()
	mockAWSClient := mock.NewMockClient(mockCtrl)
	defer mockCtrl.Finish()

	AccessKeyId := aws.String("MyAccessKeyID")
	Expiration := aws.Time(time.Now().Add(time.Hour))
	SecretAccessKey := aws.String("MySecretAccessKey")
	SessionToken := aws.String("MySessionToken")

	mockAWSClient.EXPECT().AssumeRole(gomock.Any(), gomock.Any()).Return(
		&sts.AssumeRoleOutput{
			Credentials: &ststypes.Credentials{
				AccessKeyId:     AccessKeyId,
				Expiration:      Expiration,
				SecretAccessKey: SecretAccessKey,
				SessionToken:    SessionToken,
			},
		},
		nil, // no error
	)

	creds, err := GetSTSCredentials(
		nullLogger,
		mockAWSClient,
		"",
		"",
		"",
	)

	assert.Equal(t, creds.Credentials.AccessKeyId, AccessKeyId)
	assert.Equal(t, creds.Credentials.Expiration, Expiration)
	assert.Equal(t, creds.Credentials.SecretAccessKey, SecretAccessKey)
	assert.Equal(t, creds.Credentials.SessionToken, SessionToken)
	assert.NoError(t, err)

	// Test AWS Failure
	expectedErr := &smithy.GenericAPIError{Code: "AccessDenied", Message: ""}
	mockAWSClient.EXPECT().AssumeRole(gomock.Any(), gomock.Any()).Return(
		&sts.AssumeRoleOutput{
			Credentials: &ststypes.Credentials{
				AccessKeyId:     AccessKeyId,
				Expiration:      Expiration,
				SecretAccessKey: SecretAccessKey,
				SessionToken:    SessionToken,
			},
		},
		expectedErr,
	).Times(100)

	creds, err = GetSTSCredentials(
		nullLogger,
		mockAWSClient,
		"",
		"",
		"",
	)
	assert.Error(t, err, expectedErr)
	assert.Equal(t, creds, &sts.AssumeRoleOutput{})
}
