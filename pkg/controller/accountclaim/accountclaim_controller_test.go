package accountclaim

import (
	"context"
	"time"

	"github.com/openshift/aws-account-operator/pkg/apis"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
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
		fakeClient   client.Client
	)

	apis.AddToScheme(scheme.Scheme)
	localmetrics.Collector = localmetrics.NewMetricsCollector(nil)

	BeforeEach(func() {
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
	})

	JustBeforeEach(func() {
		// Objects to track in the fake client.
		objs := []runtime.Object{accountClaim}
		fakeClient = fake.NewFakeClient(objs...)
	})

	Context("Reconcile", func() {
		It("should reconcile correctly", func() {
			r = &ReconcileAccountClaim{client: fakeClient, scheme: scheme.Scheme}
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      name,
					Namespace: namespace,
				},
			}

			_, err := r.Reconcile(req)

			Expect(err).NotTo(HaveOccurred())
			ac := awsv1alpha1.AccountClaim{}
			err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, &ac)
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
			fakeClient = fake.NewFakeClient(objs...)
			cl := &possiblyErroringFakeCtrlRuntimeClient{
				fakeClient,
				true,
			}
			r = &ReconcileAccountClaim{client: cl, scheme: scheme.Scheme}
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      name,
					Namespace: namespace,
				},
			}

			_, err := r.Reconcile(req)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("Account CR Modified during CR reset. Conflict"))
		})
	})
})

type possiblyErroringFakeCtrlRuntimeClient struct {
	client.Client
	shouldError bool
}

func (p *possiblyErroringFakeCtrlRuntimeClient) Update(
	ctx context.Context,
	acc runtime.Object) error {
	if p.shouldError {
		return fixtures.Conflict
	}
	return p.Client.Update(ctx, acc)
}
