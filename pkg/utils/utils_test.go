package utils

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apis "github.com/openshift/aws-account-operator/api"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/testutils"
	"k8s.io/client-go/kubernetes/scheme"
)

func createRoleMock(statements []awsv1alpha1.StatementEntry) awsv1alpha1.AWSFederatedRole {
	return awsv1alpha1.AWSFederatedRole{
		Spec: awsv1alpha1.AWSFederatedRoleSpec{
			AWSCustomPolicy: awsv1alpha1.AWSCustomPolicy{
				Name:        "MyPolicy",
				Description: "A policy for Testing",
				Statements:  statements,
			},
		},
	}
}

func TestMarshallingIAMPolicy(t *testing.T) {
	expected := AwsStatement{
		Effect:   "Allow",
		Action:   []string{"ec2:DescribeInstances"},
		Resource: []string{"*"},
	}

	// Create AWSFederatedRole and pass that through the MarshalIAMPolicy fun to test for correctness
	statements := []awsv1alpha1.StatementEntry{
		{
			Effect: "Allow",
			Action: []string{
				"ec2:DescribeInstances",
			},
			Resource: []string{
				"*",
			},
		},
	}

	role := createRoleMock(statements)

	policyJSON, err := MarshalIAMPolicy(role)
	if err != nil {
		t.Errorf("There was an error marshalling the IAM Policy. %s", err)
	}

	// Convert the policy back to an object so we can run comparisons easier than
	// trying to do the same with a string.
	var policy AwsPolicy
	err = json.Unmarshal([]byte(policyJSON), &policy)
	if err != nil {
		t.Errorf("There was an error unmarshalling the IAM Policy. %s", err)
	}

	if len(policy.Statement) != 1 {
		t.Errorf("Unexpected Statement Length.  Expected 1.  Got %d", len(policy.Statement))
	}

	statement := policy.Statement[0]

	if statement.Effect != expected.Effect {
		t.Errorf("Unexpected Effect.  Got: \n%s\n\n Expected:\n%s\n", statement.Effect, expected.Effect)
	}

	if len(statement.Action) != len(expected.Action) {
		t.Errorf("Unexpected Action Length.  Got: \n%s\n\nExpected: \n%s\n", statement.Action, expected.Action)
	}
	if statement.Action[0] != expected.Action[0] {
		t.Errorf("Unexected Action. Got: \n%s\n\n Expected:\n%s\n", statement.Action, expected.Action)
	}

	if len(statement.Resource) != len(expected.Resource) {
		t.Errorf("Unexpected Resource Length.  Got: \n%s\n\nExpected: \n%s\n", statement.Resource, expected.Resource)
	}
	if statement.Resource[0] != expected.Resource[0] {
		t.Errorf("Unexpected Resource. Got: \n%s\n\nExpected:\n%s\n", statement.Resource, expected.Resource)
	}
}

func TestMarshalingMultipleStatements(t *testing.T) {
	expectedList := []AwsStatement{
		{
			Effect:   "Allow",
			Action:   []string{"ec2:DescribeInstances"},
			Resource: []string{"*"},
		},
		{
			Effect:   "Deny",
			Action:   []string{"iam:CreateRole"},
			Resource: []string{"*"},
		},
	}

	statements := []awsv1alpha1.StatementEntry{
		{
			Effect:   "Allow",
			Action:   []string{"ec2:DescribeInstances"},
			Resource: []string{"*"},
		},
		{
			Effect:   "Deny",
			Action:   []string{"iam:CreateRole"},
			Resource: []string{"*"},
		},
	}

	role := createRoleMock(statements)

	policyJSON, err := MarshalIAMPolicy(role)
	if err != nil {
		t.Errorf("There was an error marshalling the IAM Policy. %s", err)
	}

	// Convert the policy back to an object so we can run comparisons easier than
	// trying to do the same with a string.
	var policy AwsPolicy
	err = json.Unmarshal([]byte(policyJSON), &policy)
	if err != nil {
		t.Errorf("There was an error unmarshalling the IAM Policy. %s", err)
	}

	if len(policy.Statement) != len(expectedList) {
		t.Errorf("Unexpected Statement Length.  Expected %d.  Got %d", len(expectedList), len(policy.Statement))
	}
}

func TestAddingConditionsToStatements(t *testing.T) {
	condition := &awsv1alpha1.Condition{
		StringEquals: map[string]string{"ram:RequestedResourceType": "route53resolver:ResolverRule"},
	}
	expected := AwsStatement{
		Effect:    "Allow",
		Action:    []string{"ec2:DescribeInstances"},
		Resource:  []string{"*"},
		Condition: condition,
	}

	// Create AWSFederatedRole and pass that through the MarshalIAMPolicy fun to test for correctness
	statements := []awsv1alpha1.StatementEntry{
		{
			Effect: "Allow",
			Action: []string{
				"ec2:DescribeInstances",
			},
			Resource: []string{
				"*",
			},
			Condition: condition,
		},
	}

	role := createRoleMock(statements)

	policyJSON, err := MarshalIAMPolicy(role)
	if err != nil {
		t.Errorf("There was an error marshalling the IAM Policy. %s", err)
	}

	// Convert the policy back to an object so we can run comparisons easier than
	// trying to do the same with a string.
	var policy AwsPolicy
	err = json.Unmarshal([]byte(policyJSON), &policy)
	if err != nil {
		t.Errorf("There was an error unmarshalling the IAM Policy. %s", err)
	}

	statement := policy.Statement[0]

	if statement.Condition.StringEquals == nil {
		t.Errorf("Condition Operator StringEquals not found.  Got:\n%s\n\nExpected:\n%s\n", expected.Condition, statement.Condition)
	}

	for key, value := range statement.Condition.StringEquals {
		if statement.Condition.StringEquals[key] == "" {
			t.Errorf("Conditional is not found.  Looking for: %s in %s", key, statement.Condition.StringEquals)
		}

		if statement.Condition.StringEquals[key] != value {
			t.Errorf("Unexected Condition. Got: \n%s\n\n Expected:\n%s\n", statement.Condition.StringEquals, expected.Condition.StringEquals)
		}
	}
}

func TestContains(t *testing.T) {
	tables := []struct {
		list   []string
		find   string
		result bool
	}{
		{[]string{}, "hello", false},
		{[]string{"hello"}, "hello", true},
		{[]string{"hello"}, "world", false},
	}

	for _, table := range tables {
		contained := Contains(table.list, table.find)
		if contained != table.result {
			var expected string
			var opposite string
			if table.result {
				expected = "found"
				opposite = "not found"
			} else {
				expected = "not found"
				opposite = "found"
			}
			t.Errorf("Expected %s to be %s.  Was %s in %s.", table.find, expected, opposite, table.list)
		}
	}
}

func TestRemove(t *testing.T) {
	tables := []struct {
		list   []string
		value  string
		result []string
	}{
		{[]string{}, "hello", []string{}},
		{[]string{"hello"}, "world", []string{"hello"}},
		{[]string{"hello", "world"}, "hello", []string{"world"}},
	}

	for _, table := range tables {
		postRemoveList := Remove(table.list, table.value)
		if !reflect.DeepEqual(postRemoveList, table.result) {
			t.Errorf("Unexpected Result.  Expected %s got %s", table.result, postRemoveList)
		}
	}
}

func TestGetControllerMaxReconcilesFromCM(t *testing.T) {
	validObjectMeta := metav1.ObjectMeta{
		Namespace: awsv1alpha1.AccountCrNamespace,
		Name:      awsv1alpha1.DefaultConfigMap,
	}
	tables := []struct {
		name        string
		expectedErr error
		expectedVal int
		configMap   *corev1.ConfigMap
	}{
		{
			name:        "Tests Key not found",
			expectedErr: awsv1alpha1.ErrInvalidConfigMap,
			expectedVal: 0,
			configMap: &corev1.ConfigMap{
				ObjectMeta: validObjectMeta,
				Data:       map[string]string{},
			},
		},
		{
			name:        "Tests not valid str->int conversion",
			expectedErr: fmt.Errorf("strconv.Atoi: parsing \"forty-two\": invalid syntax"),
			expectedVal: 0,
			configMap: &corev1.ConfigMap{
				ObjectMeta: validObjectMeta,
				Data: map[string]string{
					"MaxConcurrentReconciles.test-controller": "forty-two",
				},
			},
		},
		{
			name:        "Tests valid value returned",
			expectedErr: nil,
			expectedVal: 3,
			configMap: &corev1.ConfigMap{
				ObjectMeta: validObjectMeta,
				Data: map[string]string{
					"MaxConcurrentReconciles.test-controller": "3",
				},
			},
		},
	}

	for _, test := range tables {
		t.Run(test.name, func(t *testing.T) {
			// Add fake CM to fakes
			val, err := getControllerMaxReconcilesFromCM(test.configMap, "test-controller")

			// Check for Errors
			if test.expectedErr == nil && err != nil {
				t.Errorf("Expected no error but got %s", err.Error())
			}

			if test.expectedErr != nil && test.expectedErr.Error() != err.Error() {
				t.Errorf("Expected %s error but got %s", test.expectedErr.Error(), err.Error())
			}

			// Check for Value
			if test.expectedVal != val {
				t.Errorf("Expected value %d but got %d", test.expectedVal, val)
			}
		})
	}
}

func TestJoinLabelMaps(t *testing.T) {
	tests := []struct {
		name string
		m1   map[string]string
		m2   map[string]string
		want map[string]string
	}{
		{
			name: "both maps nil",
			want: map[string]string{},
		},
		{
			name: "m1 is nil",
			m1:   nil,
			m2:   map[string]string{"foo": "bar"},
			want: map[string]string{"foo": "bar"},
		},
		{
			name: "m2 is nil",
			m1:   map[string]string{"foo": "bar"},
			m2:   nil,
			want: map[string]string{"foo": "bar"},
		},
		{
			name: "m1 and m2 populated with same entry",
			m1:   map[string]string{"foo": "bar"},
			m2:   map[string]string{"foo": "bar"},
			want: map[string]string{"foo": "bar"},
		},
		{
			name: "m1 and m2 populated with differententries ",
			m1:   map[string]string{"foo": "bar"},
			m2:   map[string]string{"boo": "far"},
			want: map[string]string{"foo": "bar", "boo": "far"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := JoinLabelMaps(tt.m1, tt.m2); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("JoinLabelMaps() = %v, want %v", got, tt.want)
			}
		})
	}
}

var _ = Describe("Utils", func() {
	var (
		nullTestLogger testutils.TestLogger
		nullLogger     logr.Logger
		ctrl           *gomock.Controller
		configMap      *v1.ConfigMap
	)
	err := apis.AddToScheme(scheme.Scheme)
	if err != nil {
		fmt.Printf("failed adding apis to scheme in utils test")
	}
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		nullTestLogger = testutils.NewTestLogger()
		nullLogger = nullTestLogger.Logger()
		configMap = &v1.ConfigMap{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Name:        awsv1alpha1.DefaultConfigMap,
				Namespace:   awsv1alpha1.AccountCrNamespace,
				Labels:      map[string]string{},
				Annotations: map[string]string{},
			},
			Data: map[string]string{
				"ami-owner": "12345",
			},
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("GetServiceQuotasFromAccountpool", func() {
		BeforeEach(func() {
			configMap = &v1.ConfigMap{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:        awsv1alpha1.DefaultConfigMap,
					Namespace:   awsv1alpha1.AccountCrNamespace,
					Labels:      map[string]string{},
					Annotations: map[string]string{},
				},
				Data: map[string]string{
					"ami-owner": "12345",
				},
			}
		})
		It("Should return an Empty map when accountpool not found in configmap", func() {
			configMap.Data["accountpool"] = `testpool:
  default: true
`
			client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects([]runtime.Object{configMap}...).Build()
			quotas, err := GetServiceQuotasFromAccountPool(nullLogger, "nonexisting", client)
			Expect(err).To(BeNil())
			Expect(quotas).To(BeEmpty())
		})
		It("Should return an Error when the aao configmap isn't found", func() {
			client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects().Build()
			quotas, err := GetServiceQuotasFromAccountPool(nullLogger, "nonexisting", client)
			Expect(err).ToNot(BeNil())
			Expect(quotas).To(BeEmpty())
		})
		It("Should return an Error when there is no accountpool key in the configmap", func() {
			client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects([]runtime.Object{configMap}...).Build()
			quotas, err := GetServiceQuotasFromAccountPool(nullLogger, "nonexisting", client)
			Expect(err).ToNot(BeNil())
			Expect(quotas).To(BeEmpty())
		})
		It("Should return an Error when the accoutpool data is malformed", func() {
			configMap.Data["accountpool"] = `invalid: true`
			client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects([]runtime.Object{configMap}...).Build()
			quotas, err := GetServiceQuotasFromAccountPool(nullLogger, "nonexisting", client)
			Expect(err).ToNot(BeNil())
			Expect(quotas).To(BeEmpty())
		})
		It("Should return an Error when the accoutpool data is malformed", func() {
			configMap.Data["accountpool"] = `invalid: true`
			client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects([]runtime.Object{configMap}...).Build()
			quotas, err := GetServiceQuotasFromAccountPool(nullLogger, "nonexisting", client)
			Expect(err).ToNot(BeNil())
			Expect(quotas).To(BeEmpty())
		})
		It("Should return the Regional Servicequotas defined in the cm", func() {
			configMap.Data["accountpool"] = `hives02ue1:
  default: true
fm-accountpool:
  servicequotas:
    default:
      L-1216C47A: '2500'
      L-0EA8095F: '200'
      L-69A177A2: '255'
`
			client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects([]runtime.Object{configMap}...).Build()
			quotas, err := GetServiceQuotasFromAccountPool(nullLogger, "fm-accountpool", client)
			Expect(err).To(BeNil())
			Expect(quotas).ToNot(BeEmpty())
      Expect(quotas).To(HaveKey("default"))
		})
	})

})
