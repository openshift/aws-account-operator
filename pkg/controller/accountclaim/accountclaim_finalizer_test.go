package accountclaim

import (
	"fmt"

	"github.com/golang/mock/gomock"
	"github.com/openshift/aws-account-operator/pkg/apis"
	"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient/mock"
	"github.com/openshift/aws-account-operator/pkg/controller/testutils"
	"github.com/openshift/aws-account-operator/pkg/localmetrics"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("AccountClaim", func() {

	var (
		nullLogger   testutils.NullLogger
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
					Regions: []awsv1alpha1.AwsRegions{
						{
							Name: "us-east-1",
						},
					},
				},
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

	Context("Finalizers", func() {
		It("should add finalizer correctly", func() {
			// Objects to track in the fake client.
			objs := []runtime.Object{accountClaim}
			r.client = fake.NewFakeClient(objs...)

			err := r.addFinalizer(nullLogger, accountClaim)
			Expect(err).NotTo(HaveOccurred())
		})
		It("should not add finalizer as account claim doesn't exist", func() {
			// Objects to track in the fake client.
			objs := []runtime.Object{}
			r.client = fake.NewFakeClient(objs...)

			err := r.addFinalizer(nullLogger, accountClaim)
			Expect(err).To(HaveOccurred())
		})

		It("should remove finalizer from account claim", func() {
			// Objects to track in the fake client.
			objs := []runtime.Object{accountClaim}
			r.client = fake.NewFakeClient(objs...)

			err := r.removeFinalizer(nullLogger, accountClaim, accountClaimFinalizer)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should not remove finalizer as account claim doesn't exist", func() {
			// Objects to track in the fake client.
			objs := []runtime.Object{}
			r.client = fake.NewFakeClient(objs...)

			err := r.removeFinalizer(nullLogger, accountClaim, accountClaimFinalizer)
			Expect(err).To(HaveOccurred())
		})

		It("should add byoc secret finalizer", func() {
			// Objects to track in the fake client.
			accountClaim.Spec.BYOCSecretRef = v1alpha1.SecretRef{
				Name:      name,
				Namespace: namespace,
			}
			byocSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
			}
			objs := []runtime.Object{accountClaim, byocSecret}
			r.client = fake.NewFakeClient(objs...)

			err := r.addBYOCSecretFinalizer(accountClaim)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should not find byoc secret", func() {
			// Objects to track in the fake client.
			accountClaim.Spec.BYOCSecretRef = v1alpha1.SecretRef{
				Name:      name,
				Namespace: namespace,
			}

			objs := []runtime.Object{accountClaim}
			r.client = fake.NewFakeClient(objs...)

			err := r.addBYOCSecretFinalizer(accountClaim)
			Expect(err).To(HaveOccurred())
		})

		It("should remove byoc secret finalizer", func() {
			// Objects to track in the fake client.
			accountClaim.Spec.BYOCSecretRef = v1alpha1.SecretRef{
				Name:      name,
				Namespace: namespace,
			}
			byocSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
			}
			objs := []runtime.Object{accountClaim, byocSecret}
			r.client = fake.NewFakeClient(objs...)

			err := r.removeBYOCSecretFinalizer(accountClaim)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should not remove byoc secret finalizer as secret doesn't exist", func() {
			// Objects to track in the fake client.
			accountClaim.Spec.BYOCSecretRef = v1alpha1.SecretRef{
				Name:      name,
				Namespace: namespace,
			}
			objs := []runtime.Object{accountClaim}
			r.client = fake.NewFakeClient(objs...)

			err := r.removeBYOCSecretFinalizer(accountClaim)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
