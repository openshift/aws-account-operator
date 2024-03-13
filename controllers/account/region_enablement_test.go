package account

import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/account"
	"github.com/go-logr/logr"
	apis "github.com/openshift/aws-account-operator/api"
	"github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient/mock"
	"github.com/openshift/aws-account-operator/pkg/testutils"
	"go.uber.org/mock/gomock"
	"k8s.io/client-go/kubernetes/scheme"
	"testing"
)

func TestAccountReconciler_HandleOptInRegionRequests(t *testing.T) {

	err := apis.AddToScheme(scheme.Scheme)
	if err != nil {
		fmt.Printf("failed adding to scheme in service_quota_test.go")
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
				RegionCode: "af-south-1",
				Status:     v1alpha1.OptInRequestTodo,
			},
			currentAcctInstance: &v1alpha1.Account{
				Status: v1alpha1.AccountStatus{
					OptInRegions: v1alpha1.OptInRegions{
						"CapeTown": &v1alpha1.OptInRegionStatus{
							RegionCode: "af-south-1",
							Status:     v1alpha1.OptInRequestTodo,
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
			mockAWSClient.EXPECT().GetRegionOptStatus(gomock.Any()).Return(
				&account.GetRegionOptStatusOutput{
					RegionName:      aws.String("af-south-1"),
					RegionOptStatus: aws.String("DISABLED"),
				},
				nil,
			)

			mockAWSClient.EXPECT().GetRegionOptStatus(gomock.Any()).Return(
				&account.GetRegionOptStatusOutput{
					RegionName:      aws.String("af-south-1"),
					RegionOptStatus: aws.String("DISABLED"),
				},
				nil,
			)

			mockAWSClient.EXPECT().EnableRegion(gomock.Any()).Return(
				&account.EnableRegionOutput{},
				nil,
			)

			if err := HandleOptInRegionRequests(test.reqLogger, mockAWSClient, test.optInRegion, test.currentAcctInstance); (err != nil) != test.wantErr {
				t.Errorf("AccountReconciler.HandleOptInRegionRequests() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}
