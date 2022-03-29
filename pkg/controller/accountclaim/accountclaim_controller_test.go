package accountclaim

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/golang/mock/gomock"
	"github.com/openshift/aws-account-operator/pkg/apis"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient/mock"
	"github.com/openshift/aws-account-operator/pkg/localmetrics"
	"github.com/openshift/aws-account-operator/test/fixtures"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("AccountClaim", func() {
	var (
		name         = "testAccountClaim"
		namespace    = "myAccountClaimNamespace"
		accountClaim *awsv1alpha1.AccountClaim
		r            *ReconcileAccountClaim
		ctrl         *gomock.Controller
		req          reconcile.Request
	)

	err := apis.AddToScheme(scheme.Scheme)
	if err != nil {
		fmt.Printf("failed adding apis to scheme in account controller tests")
	}
	localmetrics.Collector = localmetrics.NewMetricsCollector(nil)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		region := awsv1alpha1.AwsRegions{
			Name: "us-east-1",
		}
		accountClaim = &awsv1alpha1.AccountClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: awsv1alpha1.AccountClaimSpec{
				LegalEntity: awsv1alpha1.LegalEntity{
					Name: "LegalCorp. Inc.",
					ID:   "abcdefg123456",
				},
				AccountLink: "osd-creds-mgmt-aaabbb",
				Aws: awsv1alpha1.Aws{
					Regions: []awsv1alpha1.AwsRegions{region},
				},
			},
		}
		req = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			},
		}

		// Create the reconciler with a mocking AWS client IBuilder.
		r = &ReconcileAccountClaim{
			// Test cases need to set fakeClient.
			scheme: scheme.Scheme,
			awsClientBuilder: &mock.Builder{
				MockController: ctrl,
			},
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	When("Reconciling an AccountClaim", func() {
		It("should reconcile correctly", func() {
			// Objects to track in the fake client.
			objs := []runtime.Object{accountClaim}
			r.client = fake.NewFakeClient(objs...)

			_, err := r.Reconcile(req)

			Expect(err).NotTo(HaveOccurred())
			ac := awsv1alpha1.AccountClaim{}
			err = r.client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, &ac)
			Expect(err).NotTo(HaveOccurred())
			Expect(ac.Spec).To(Equal(accountClaim.Spec))
		})

		Context("AccountClaim is marked for Deletion", func() {

			var (
				objs []runtime.Object
			)

			BeforeEach(func() {
				accountClaim.DeletionTimestamp = &metav1.Time{Time: time.Now()}
				accountClaim.SetFinalizers(append(accountClaim.GetFinalizers(), accountClaimFinalizer))

				account := &awsv1alpha1.Account{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "osd-creds-mgmt-aaabbb",
						Namespace: "aws-account-operator",
					},
					Spec: awsv1alpha1.AccountSpec{
						LegalEntity: awsv1alpha1.LegalEntity{
							Name: "LegalCorp. Inc.",
							ID:   "abcdefg123456",
						},
					},
				}

				objs = []runtime.Object{accountClaim, account}
			})

			It("should delete AccountClaim", func() {
				r.client = fake.NewFakeClient(objs...)

				mockAWSClient := mock.GetMockClient(r.awsClientBuilder)
				// Create empty empy aws responses.
				lhzo := &route53.ListHostedZonesOutput{
					HostedZones: []*route53.HostedZone{},
					IsTruncated: aws.Bool(false),
				}
				lbo := &s3.ListBucketsOutput{
					Buckets: []*s3.Bucket{},
				}
				dvpcesco := &ec2.DescribeVpcEndpointServiceConfigurationsOutput{
					ServiceConfigurations: []*ec2.ServiceConfiguration{},
				}
				dso := &ec2.DescribeSnapshotsOutput{
					Snapshots: []*ec2.Snapshot{},
				}
				dvo := &ec2.DescribeVolumesOutput{
					Volumes: []*ec2.Volume{},
				}

				mockAWSClient.EXPECT().ListHostedZones(gomock.Any()).Return(lhzo, nil)
				mockAWSClient.EXPECT().ListBuckets(gomock.Any()).Return(lbo, nil)
				mockAWSClient.EXPECT().DescribeVpcEndpointServiceConfigurations(gomock.Any()).Return(dvpcesco, nil)
				mockAWSClient.EXPECT().DescribeSnapshots(gomock.Any()).Return(dso, nil)
				mockAWSClient.EXPECT().DescribeVolumes(gomock.Any()).Return(dvo, nil)

				_, err := r.Reconcile(req)
				Expect(err).ToNot(HaveOccurred())

				// Ensure we have removed the finalizer.
				ac := awsv1alpha1.AccountClaim{}
				err = r.client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, &ac)
				Expect(err).NotTo(HaveOccurred())
				Expect(ac.Finalizers).To(BeNil())

				// Ensure the non-ccs account has been reset as expected.
				acc := awsv1alpha1.Account{}
				err = r.client.Get(context.TODO(), types.NamespacedName{Name: ac.Spec.AccountLink, Namespace: awsv1alpha1.AccountCrNamespace}, &acc)
				Expect(err).NotTo(HaveOccurred())
				Expect(acc.Spec.ClaimLink).To(BeEmpty())
				Expect(acc.Spec.ClaimLinkNamespace).To(BeEmpty())
				Expect(acc.Status.State).To(Equal(string(awsv1alpha1.AccountReady)))
				Expect(acc.Status.Reused).To(BeTrue())
			})

			It("should retry on a conflict error", func() {
				r.client = &possiblyErroringFakeCtrlRuntimeClient{
					fake.NewFakeClient(objs...),
					true,
				}

				mockAWSClient := mock.GetMockClient(r.awsClientBuilder)
				// Create empty empy aws responses.
				lhzo := &route53.ListHostedZonesOutput{
					HostedZones: []*route53.HostedZone{},
					IsTruncated: aws.Bool(false),
				}
				lbo := &s3.ListBucketsOutput{
					Buckets: []*s3.Bucket{},
				}
				dvpcesco := &ec2.DescribeVpcEndpointServiceConfigurationsOutput{
					ServiceConfigurations: []*ec2.ServiceConfiguration{},
				}
				dso := &ec2.DescribeSnapshotsOutput{
					Snapshots: []*ec2.Snapshot{},
				}
				dvo := &ec2.DescribeVolumesOutput{
					Volumes: []*ec2.Volume{},
				}

				mockAWSClient.EXPECT().ListHostedZones(gomock.Any()).Return(lhzo, nil)
				mockAWSClient.EXPECT().ListBuckets(gomock.Any()).Return(lbo, nil)
				mockAWSClient.EXPECT().DescribeVpcEndpointServiceConfigurations(gomock.Any()).Return(dvpcesco, nil)
				mockAWSClient.EXPECT().DescribeSnapshots(gomock.Any()).Return(dso, nil)
				mockAWSClient.EXPECT().DescribeVolumes(gomock.Any()).Return(dvo, nil)

				_, err := r.Reconcile(req)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("account CR modified during reset: Conflict"))

				// Ensure we haven't removed the finalizer.
				ac := awsv1alpha1.AccountClaim{}
				err = r.client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, &ac)
				Expect(err).NotTo(HaveOccurred())
				Expect(ac.Finalizers).To(Equal(accountClaim.GetFinalizers()))
			})

			It("should handle aws cleanup errors", func() {
				r.client = fake.NewFakeClient(objs...)

				mockAWSClient := mock.GetMockClient(r.awsClientBuilder)
				// Use a bogus error, just so we can fail AWS calls.
				theErr := awserr.NewBatchError("foo", "bar", []error{})
				mockAWSClient.EXPECT().ListHostedZones(gomock.Any()).Return(nil, theErr)
				mockAWSClient.EXPECT().ListBuckets(gomock.Any()).Return(nil, theErr)
				mockAWSClient.EXPECT().DescribeVpcEndpointServiceConfigurations(gomock.Any()).Return(nil, theErr)
				mockAWSClient.EXPECT().DescribeSnapshots(gomock.Any()).Return(nil, theErr)
				mockAWSClient.EXPECT().DescribeVolumes(gomock.Any()).Return(nil, theErr)

				_, err := r.Reconcile(req)

				Expect(err).To(HaveOccurred())

				// Ensure we haven't removed the finalizer.
				ac := awsv1alpha1.AccountClaim{}
				err = r.client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, &ac)
				Expect(err).NotTo(HaveOccurred())
				Expect(ac.Finalizers).To(Equal(accountClaim.GetFinalizers()))
			})
		})

		When("Accountclaim is BYOC", func() {

			BeforeEach(func() {
				accountClaim.SetFinalizers(append(accountClaim.GetFinalizers(), accountClaimFinalizer))
				accountClaim.Spec.BYOC = true
				accountClaim.Spec.AccountLink = ""
			})

			It("should fail validation", func() {
				// fail validation if BYOC is not associated with an account
				accountClaim.Spec.BYOCAWSAccountID = ""

				r.client = fake.NewFakeClient(accountClaim)

				_, err := r.Reconcile(req)

				Expect(err).To(HaveOccurred())
				ac := awsv1alpha1.AccountClaim{}
				err = r.client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, &ac)
				Expect(err).NotTo(HaveOccurred())
				Expect(ac.Status.State).To(Equal(awsv1alpha1.ClaimStatusError))
			})

			It("Should create a BYOC Account", func() {
				dummySecretRef := awsv1alpha1.SecretRef{
					Name:      "name",
					Namespace: "namespace",
				}
				accountClaim.Spec.BYOCSecretRef = dummySecretRef
				accountClaim.Spec.AwsCredentialSecret = dummySecretRef
				accountClaim.Spec.BYOCAWSAccountID = "123456"

				r.client = fake.NewFakeClient(accountClaim)

				_, err := r.Reconcile(req)
				Expect(err).NotTo(HaveOccurred())

				ac := awsv1alpha1.AccountClaim{}
				err = r.client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, &ac)
				Expect(err).NotTo(HaveOccurred())

				account := awsv1alpha1.Account{}
				err = r.client.Get(context.TODO(), types.NamespacedName{Name: ac.Spec.AccountLink, Namespace: awsv1alpha1.AccountCrNamespace}, &account)

				Expect(err).NotTo(HaveOccurred())
				Expect(account.Spec.BYOC).To(BeTrue())
				Expect(account.Spec.LegalEntity.ID).To(Equal(accountClaim.Spec.LegalEntity.ID))
				Expect(account.Spec.AwsAccountID).To(Equal(accountClaim.Spec.BYOCAWSAccountID))
			})
		})
	})
})

type possiblyErroringFakeCtrlRuntimeClient struct {
	client.Client
	shouldError bool
}

func (p *possiblyErroringFakeCtrlRuntimeClient) Update(
	ctx context.Context,
	acc runtime.Object,
	opts ...client.UpdateOption) error {
	if p.shouldError {
		return fixtures.Conflict
	}
	return p.Client.Update(ctx, acc)
}
