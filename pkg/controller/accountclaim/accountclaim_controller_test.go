package accountclaim

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/golang/mock/gomock"
	"github.com/openshift/aws-account-operator/pkg/apis"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/localmetrics"
	"github.com/openshift/aws-account-operator/test/fixtures"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("AccountClaim", func() {
	var (
		name         = "testAccountClaim"
		namespace    = "myAccountClaimNamespace"
		accountClaim *awsv1alpha1.AccountClaim
		r            *ReconcileAccountClaim
		ctrl         *gomock.Controller
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

		// Create the reconciler with a mocking AWS client IBuilder.
		r = &ReconcileAccountClaim{
			// Test cases need to set fakeClient.
			scheme: scheme.Scheme,
			awsClientBuilder: &awsclient.MockBuilder{
				MockController: ctrl,
			},
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("Reconcile", func() {
		It("should reconcile correctly", func() {
			// Objects to track in the fake client.
			objs := []runtime.Object{accountClaim}
			r.client = fake.NewFakeClient(objs...)

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      name,
					Namespace: namespace,
				},
			}

			_, err := r.Reconcile(req)

			Expect(err).NotTo(HaveOccurred())
			ac := awsv1alpha1.AccountClaim{}
			err = r.client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, &ac)
			Expect(err).NotTo(HaveOccurred())
			Expect(ac.Spec).To(Equal(accountClaim.Spec))
		})

		It("should retry on a conflict error", func() {
			accountClaim.DeletionTimestamp = &metav1.Time{Time: time.Now()}
			accountClaim.SetFinalizers(append(accountClaim.GetFinalizers(), "finalizer.aws.managed.openshift.io"))

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

			objs := []runtime.Object{accountClaim, account}
			r.client = &possiblyErroringFakeCtrlRuntimeClient{
				fake.NewFakeClient(objs...),
				true,
			}
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      name,
					Namespace: namespace,
				},
			}

			// TODO: As written, this is just triggering error paths for each of the cleanup
			//       funcs, proving that errors in those cleanups don't propagate up to Reconcile.
			//       Once that's fixed, these will need to be changed to do more realistic things.
			mockAWSClient := awsclient.GetMockClient(r.awsClientBuilder)
			// Use a bogus error, just so we can fail AWS calls.
			theErr := awserr.NewBatchError("foo", "bar", []error{})
			mockAWSClient.EXPECT().ListHostedZones(gomock.Any()).Return(nil, theErr)
			mockAWSClient.EXPECT().ListBuckets(gomock.Any()).Return(nil, theErr)
			mockAWSClient.EXPECT().DescribeSnapshots(gomock.Any()).Return(nil, theErr)
			mockAWSClient.EXPECT().DescribeVolumes(gomock.Any()).Return(nil, theErr)

			_, err := r.Reconcile(req)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("account CR modified during reset: Conflict"))
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
