package account

import (
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/servicequotas"
	"github.com/go-logr/logr"
	apis "github.com/openshift/aws-account-operator/api"
	"github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient/mock"
	"github.com/openshift/aws-account-operator/pkg/testutils"
	"go.uber.org/mock/gomock"
	"k8s.io/client-go/kubernetes/scheme"
)

func TestAccountReconciler_HandleServiceQuotaRequests(t *testing.T) {

	err := apis.AddToScheme(scheme.Scheme)
	if err != nil {
		fmt.Printf("failed adding to scheme in service_quota_test.go")
	}

	nullLogger := testutils.NewTestLogger().Logger()

	tests := []struct {
		name       string
		quotaCode  v1alpha1.SupportedServiceQuotas
		quotaValue v1alpha1.ServiceQuotaStatus
		reqLogger  logr.Logger
		wantErr    bool
	}{
		{
			name:      "Valid Service Quota Request",
			quotaCode: v1alpha1.RunningStandardInstances,
			quotaValue: v1alpha1.ServiceQuotaStatus{
				Value: 10,
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

			mockAWSClient.EXPECT().GetServiceQuota(gomock.Any()).Return(
				&servicequotas.GetServiceQuotaOutput{
					Quota: &servicequotas.ServiceQuota{
						Value: aws.Float64(5),
					},
				},
				nil,
			)

			mockAWSClient.EXPECT().ListRequestedServiceQuotaChangeHistoryByQuota(gomock.Any()).Return(
				&servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaOutput{
					RequestedQuotas: []*servicequotas.RequestedServiceQuotaChange{},
				},
				nil,
			)

			mockAWSClient.EXPECT().RequestServiceQuotaIncrease(&servicequotas.RequestServiceQuotaIncreaseInput{
				DesiredValue: aws.Float64(10),
				QuotaCode:    aws.String(string(v1alpha1.RunningStandardInstances)),
				ServiceCode:  aws.String(string(v1alpha1.EC2ServiceQuota)),
			}).Return(
				&servicequotas.RequestServiceQuotaIncreaseOutput{
					RequestedQuota: &servicequotas.RequestedServiceQuotaChange{
						CaseId: aws.String("MyAwesomeCaseID"),
					},
				},
				nil,
			)

			if err := HandleServiceQuotaRequests(test.reqLogger, mockAWSClient, test.quotaCode, &test.quotaValue); (err != nil) != test.wantErr {
				t.Errorf("AccountReconciler.HandleServiceQuotaRequests() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}

}
