package account

import (
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/account"
	accounttypes "github.com/aws/aws-sdk-go-v2/service/account/types"
	"github.com/go-logr/logr"
	apis "github.com/openshift/aws-account-operator/api"
	"github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient/mock"
	"github.com/openshift/aws-account-operator/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
	"k8s.io/client-go/kubernetes/scheme"
)

func TestParseDisabledRegions(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]bool
	}{
		{
			name:     "empty string returns empty map",
			input:    "",
			expected: map[string]bool{},
		},
		{
			name:  "single region",
			input: "me-south-1",
			expected: map[string]bool{
				"me-south-1": true,
			},
		},
		{
			name:  "multiple regions",
			input: "me-south-1,me-central-1",
			expected: map[string]bool{
				"me-south-1":   true,
				"me-central-1": true,
			},
		},
		{
			name:  "handles whitespace",
			input: " me-south-1 , me-central-1 ",
			expected: map[string]bool{
				"me-south-1":   true,
				"me-central-1": true,
			},
		},
		{
			name:     "trailing comma produces no spurious entry",
			input:    "me-south-1,",
			expected: map[string]bool{"me-south-1": true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseDisabledRegions(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAccountReconciler_HandleOptInRegionRequests(t *testing.T) {

	err := apis.AddToScheme(scheme.Scheme)
	if err != nil {
		fmt.Printf("failed adding to scheme in region_enablement_test.go")
	}

	nullLogger := testutils.NewTestLogger().Logger()

	tests := []struct {
		name                string
		optInRegion         *v1alpha1.OptInRegionStatus
		currentAcctInstance *v1alpha1.Account
		reqLogger           logr.Logger
		wantErr             bool
	}{
		{
			name: "Valid Region Enablement Request",
			optInRegion: &v1alpha1.OptInRegionStatus{
				Status: v1alpha1.OptInRequestTodo,
			},
			currentAcctInstance: &v1alpha1.Account{
				Status: v1alpha1.AccountStatus{
					OptInRegions: v1alpha1.OptInRegions{
						"af-south-1": &v1alpha1.OptInRegionStatus{
							Status: v1alpha1.OptInRequestTodo,
						},
					},
				},
			},
			reqLogger: nullLogger,
			wantErr:   false,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			mocks := setupDefaultMocks(t, nil)
			mockAWSClient := mock.NewMockClient(mocks.mockCtrl)

			// This is necessary for the mocks to report failures like methods not being called an expected number of times.
			// after mocks is defined
			defer mocks.mockCtrl.Finish()
			mockAWSClient.EXPECT().GetRegionOptStatus(gomock.Any(), gomock.Any()).Return(
				&account.GetRegionOptStatusOutput{
					RegionName:      aws.String("af-south-1"),
					RegionOptStatus: accounttypes.RegionOptStatusDisabled,
				},
				nil,
			)

			mockAWSClient.EXPECT().GetRegionOptStatus(gomock.Any(), gomock.Any()).Return(
				&account.GetRegionOptStatusOutput{
					RegionName:      aws.String("af-south-1"),
					RegionOptStatus: accounttypes.RegionOptStatusDisabled,
				},
				nil,
			)

			mockAWSClient.EXPECT().EnableRegion(gomock.Any(), gomock.Any()).Return(
				&account.EnableRegionOutput{},
				nil,
			)

			if err := HandleOptInRegionRequests(test.reqLogger, mockAWSClient, "af-south-1", test.optInRegion, test.currentAcctInstance); (err != nil) != test.wantErr {
				t.Errorf("AccountReconciler.HandleOptInRegionRequests() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}
