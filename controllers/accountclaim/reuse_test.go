package accountclaim_test

import (
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/go-logr/logr"
	"github.com/golang/mock/gomock"
	"github.com/openshift/aws-account-operator/controllers/accountclaim"
	mock "github.com/openshift/aws-account-operator/controllers/accountclaim/mock"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	awsmock "github.com/openshift/aws-account-operator/pkg/awsclient/mock"
	"github.com/openshift/aws-account-operator/pkg/testutils"
	"k8s.io/client-go/kubernetes/scheme"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type cleanupfunc func(logr.Logger, awsclient.Client, chan string, chan string) error

func runCleanupFunc(functorun cleanupfunc, client awsclient.Client) (string, string, error) {

	wg := sync.WaitGroup{}
	notifications, errors := make(chan string), make(chan string)

	msg := ""
	errMsg := ""
	go func() {
		defer wg.Done()
		wg.Add(1)
		select {
		case msg = <-notifications:

		case errMsg = <-errors:

		}
	}()
	err := functorun(testutils.NewTestLogger().Logger(), client, notifications, errors)
	wg.Wait()

	return msg, errMsg, err
}

var _ = Describe("Account Reuse", func() {
	var (
		r                *accountclaim.AccountClaimReconciler
		ctrl             *gomock.Controller
		awsClientBuilder *awsmock.Builder
		mockAwsClient    *awsmock.MockClient
		kubeClient       kclient.Client
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		awsClientBuilder = &awsmock.Builder{MockController: ctrl}
		kubeClient = mock.NewMockClient(ctrl)

		// Create the reconciler with a mocking AWS client IBuilder.
		r = accountclaim.NewAccountClaimReconciler(
			kubeClient,
			scheme.Scheme,
			awsClientBuilder,
		)

		mockAwsClient = awsmock.NewMockClient(ctrl)
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Describe("CleanUpAwsAccountVpcEndpointServiceConfigurations", func() {
		var (
			describeOutput ec2.DescribeVpcEndpointServiceConfigurationsOutput
			deleteOutput   ec2.DeleteVpcEndpointServiceConfigurationsOutput
		)
		Context("When no VPC Endpoint Service Configuration exists", func() {
			BeforeEach(func() {
				mockAwsClient.EXPECT().DescribeVpcEndpointServiceConfigurations(gomock.Any()).Return(&describeOutput, nil)
			})

			It("Does nothing", func() {
				notifications, errors, _ := runCleanupFunc(r.CleanUpAwsAccountVpcEndpointServiceConfigurations, mockAwsClient)
				Expect(errors).To(Equal(""))
				Expect(notifications).To(Equal("VPC endpoint service configuration cleanup finished successfully (nothing to do)"))
			})
		})

		Context("When one VPC Endpoint Service Configuration exists", func() {
			var (
				serviceConfigId string
				deleteInput     *ec2.DeleteVpcEndpointServiceConfigurationsInput
			)
			BeforeEach(func() {
				serviceConfigId = "FakeID"
				describeOutput = ec2.DescribeVpcEndpointServiceConfigurationsOutput{
					ServiceConfigurations: []*ec2.ServiceConfiguration{
						{
							ServiceId: &serviceConfigId,
						},
					},
				}
				deleteOutput = ec2.DeleteVpcEndpointServiceConfigurationsOutput{}
				mockAwsClient.EXPECT().DescribeVpcEndpointServiceConfigurations(gomock.Any()).Return(&describeOutput, nil)
				mockAwsClient.EXPECT().DeleteVpcEndpointServiceConfigurations(gomock.Any()).Do(func(input *ec2.DeleteVpcEndpointServiceConfigurationsInput) {
					deleteInput = input
				}).Return(&deleteOutput, nil)
			})

			It("Deletes the VPC Endpoint Service Configuration", func() {
				notifications, errors, _ := runCleanupFunc(r.CleanUpAwsAccountVpcEndpointServiceConfigurations, mockAwsClient)

				Expect(len(deleteInput.ServiceIds)).To(Equal(1))
				Expect(*deleteInput.ServiceIds[0]).To(Equal(serviceConfigId))
				Expect(errors).To(Equal(""))
				Expect(notifications).To(Equal("VPC endpoint service configuration cleanup finished successfully"))
			})
		})

		Context("When two VPC Endpoint Service Configuration exist", func() {
			var (
				serviceConfigId1 string
				serviceConfigId2 string
				deleteInput      *ec2.DeleteVpcEndpointServiceConfigurationsInput
				deleteErr        error
			)

			JustBeforeEach(func() {
				serviceConfigId1 = "FakeID"
				serviceConfigId2 = "FakeID2"
				describeOutput = ec2.DescribeVpcEndpointServiceConfigurationsOutput{
					ServiceConfigurations: []*ec2.ServiceConfiguration{
						{
							ServiceId: &serviceConfigId1,
						},
						{
							ServiceId: &serviceConfigId2,
						},
					},
				}
				mockAwsClient.EXPECT().DescribeVpcEndpointServiceConfigurations(gomock.Any()).Return(&describeOutput, nil)
				mockAwsClient.EXPECT().DeleteVpcEndpointServiceConfigurations(gomock.Any()).Do(func(input *ec2.DeleteVpcEndpointServiceConfigurationsInput) {
					deleteInput = input
				}).Return(&deleteOutput, deleteErr)
			})

			Context("When all VPC Endpoint Service Configurations can be deleted", func() {
				BeforeEach(func() {
					deleteErr = nil
					deleteOutput = ec2.DeleteVpcEndpointServiceConfigurationsOutput{}
				})
				It("Deletes the VPC Endpoint Service Configuration and doesn't return an error", func() {
					notifications, errors, _ := runCleanupFunc(r.CleanUpAwsAccountVpcEndpointServiceConfigurations, mockAwsClient)

					Expect(len(deleteInput.ServiceIds)).To(Equal(2))
					Expect(*deleteInput.ServiceIds[0]).To(Equal(serviceConfigId1))
					Expect(*deleteInput.ServiceIds[1]).To(Equal(serviceConfigId2))
					Expect(errors).To(Equal(""))
					Expect(notifications).To(Equal("VPC endpoint service configuration cleanup finished successfully"))
				})
			})

			Context("When the first of the VPC Endpoint Service Configurations can't be deleted", func() {
				BeforeEach(func() {
					deleteErr = fmt.Errorf("nop nop nop")
					deleteOutput = ec2.DeleteVpcEndpointServiceConfigurationsOutput{
						Unsuccessful: []*ec2.UnsuccessfulItem{
							{
								ResourceId: &serviceConfigId1,
							},
						},
					}
				})
				It("Deletes the VPC Endpoint Service Configuration and returns the failing Service ID", func() {
					notifications, errors, _ := runCleanupFunc(r.CleanUpAwsAccountVpcEndpointServiceConfigurations, mockAwsClient)

					Expect(len(deleteInput.ServiceIds)).To(Equal(2))
					Expect(*deleteInput.ServiceIds[0]).To(Equal(serviceConfigId1))
					Expect(*deleteInput.ServiceIds[1]).To(Equal(serviceConfigId2))
					Expect(errors).To(Equal("Failed deleting VPC endpoint service configurations: " + serviceConfigId1))
					Expect(notifications).To(Equal(""))

				})
			})
			Context("When both of the VPC Endpoint Service Configurations can't be deleted", func() {
				BeforeEach(func() {
					deleteErr = fmt.Errorf("nop nop nop")
					deleteOutput = ec2.DeleteVpcEndpointServiceConfigurationsOutput{
						Unsuccessful: []*ec2.UnsuccessfulItem{
							{
								ResourceId: &serviceConfigId1,
							},
							{
								ResourceId: &serviceConfigId2,
							},
						},
					}
				})
				It("Deletes the VPC Endpoint Service Configuration and returns the failing Service ID", func() {
					notifications, errors, _ := runCleanupFunc(r.CleanUpAwsAccountVpcEndpointServiceConfigurations, mockAwsClient)

					Expect(len(deleteInput.ServiceIds)).To(Equal(2))
					Expect(*deleteInput.ServiceIds[0]).To(Equal(serviceConfigId1))
					Expect(*deleteInput.ServiceIds[1]).To(Equal(serviceConfigId2))
					Expect(errors).To(Equal("Failed deleting VPC endpoint service configurations: " + serviceConfigId1 + ", " + serviceConfigId2))
					Expect(notifications).To(Equal(""))

				})
			})
		})

		Context("When Describe API call returns nil", func() {
			BeforeEach(func() {
				mockAwsClient.EXPECT().DescribeVpcEndpointServiceConfigurations(gomock.Any()).Return(nil, nil)
			})

			It("Returns an error", func() {
				notifications, errors, _ := runCleanupFunc(r.CleanUpAwsAccountVpcEndpointServiceConfigurations, mockAwsClient)
				Expect(errors).To(Equal("Failed describing VPC endpoint service configurations"))
				Expect(notifications).To(Equal(""))
			})
		})
	})
})
