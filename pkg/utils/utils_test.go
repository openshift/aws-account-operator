package utils

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
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
		configMap   *v1.ConfigMap
	}{
		{
			name:        "Tests Key not found",
			expectedErr: awsv1alpha1.ErrInvalidConfigMap,
			expectedVal: 0,
			configMap: &v1.ConfigMap{
				ObjectMeta: validObjectMeta,
				Data:       map[string]string{},
			},
		},
		{
			name:        "Tests not valid str->int conversion",
			expectedErr: fmt.Errorf("strconv.Atoi: parsing \"forty-two\": invalid syntax"),
			expectedVal: 0,
			configMap: &v1.ConfigMap{
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
			configMap: &v1.ConfigMap{
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

func TestIsCloseOnReleaseEnabled(t *testing.T) {
	tests := []struct {
		name      string
		configMap *v1.ConfigMap
		expected  bool
	}{
		{
			name:      "nil configmap returns false",
			configMap: nil,
			expected:  false,
		},
		{
			name: "feature not set returns false",
			configMap: &v1.ConfigMap{
				Data: map[string]string{},
			},
			expected: false,
		},
		{
			name: "feature set to false returns false",
			configMap: &v1.ConfigMap{
				Data: map[string]string{
					FeatureCloseOnRelease: "false",
				},
			},
			expected: false,
		},
		{
			name: "feature set to true returns true",
			configMap: &v1.ConfigMap{
				Data: map[string]string{
					FeatureCloseOnRelease: "true",
				},
			},
			expected: true,
		},
		{
			name: "feature set to invalid value returns false",
			configMap: &v1.ConfigMap{
				Data: map[string]string{
					FeatureCloseOnRelease: "invalid",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsCloseOnReleaseEnabled(tt.configMap)
			if result != tt.expected {
				t.Errorf("IsCloseOnReleaseEnabled() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestIsCloseAccountDryRun(t *testing.T) {
	tests := []struct {
		name      string
		configMap *v1.ConfigMap
		expected  bool
	}{
		{
			name:      "nil configmap returns true (safe default)",
			configMap: nil,
			expected:  true,
		},
		{
			name: "dry_run not set returns true (safe default)",
			configMap: &v1.ConfigMap{
				Data: map[string]string{},
			},
			expected: true,
		},
		{
			name: "dry_run set to true returns true",
			configMap: &v1.ConfigMap{
				Data: map[string]string{
					CloseAccountDryRun: "true",
				},
			},
			expected: true,
		},
		{
			name: "dry_run set to false returns false",
			configMap: &v1.ConfigMap{
				Data: map[string]string{
					CloseAccountDryRun: "false",
				},
			},
			expected: false,
		},
		{
			name: "dry_run set to invalid value returns true (safe default)",
			configMap: &v1.ConfigMap{
				Data: map[string]string{
					CloseAccountDryRun: "invalid",
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsCloseAccountDryRun(tt.configMap)
			if result != tt.expected {
				t.Errorf("IsCloseAccountDryRun() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestIsCloseAccountRateLimited(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		wantLimited bool
		description string
	}{
		{
			name:        "nil annotations returns not limited",
			annotations: nil,
			wantLimited: false,
			description: "nil map should not be rate limited",
		},
		{
			name:        "empty annotations returns not limited",
			annotations: map[string]string{},
			wantLimited: false,
			description: "empty map should not be rate limited",
		},
		{
			name: "missing annotation returns not limited",
			annotations: map[string]string{
				"other-annotation": "value",
			},
			wantLimited: false,
			description: "missing rate limit annotation should not be rate limited",
		},
		{
			name: "future timestamp returns limited",
			annotations: map[string]string{
				CloseAccountRateLimitAnnotation: time.Now().Add(1 * time.Hour).Format(time.RFC3339),
			},
			wantLimited: true,
			description: "future timestamp should be rate limited",
		},
		{
			name: "past timestamp returns not limited",
			annotations: map[string]string{
				CloseAccountRateLimitAnnotation: time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			},
			wantLimited: false,
			description: "past timestamp should not be rate limited",
		},
		{
			name: "invalid timestamp returns not limited",
			annotations: map[string]string{
				CloseAccountRateLimitAnnotation: "invalid-timestamp",
			},
			wantLimited: false,
			description: "invalid timestamp should not be rate limited",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isLimited, _ := IsCloseAccountRateLimited(tt.annotations)
			if isLimited != tt.wantLimited {
				t.Errorf("IsCloseAccountRateLimited() = %v, want %v: %s", isLimited, tt.wantLimited, tt.description)
			}
		})
	}
}

func TestCalculateCloseAccountBackoff(t *testing.T) {
	tests := []struct {
		name            string
		annotations     map[string]string
		expectedBackoff time.Duration
	}{
		{
			name:            "nil annotations returns initial backoff",
			annotations:     nil,
			expectedBackoff: CloseAccountInitialBackoff,
		},
		{
			name:            "empty annotations returns initial backoff",
			annotations:     map[string]string{},
			expectedBackoff: CloseAccountInitialBackoff,
		},
		{
			name: "first backoff is initial value",
			annotations: map[string]string{
				CloseAccountBackoffAnnotation: "0",
			},
			expectedBackoff: CloseAccountInitialBackoff,
		},
		{
			name: "second backoff doubles initial (1h -> 2h)",
			annotations: map[string]string{
				CloseAccountBackoffAnnotation: "3600", // 1 hour in seconds
			},
			expectedBackoff: 2 * time.Hour,
		},
		{
			name: "third backoff doubles again (2h -> 4h)",
			annotations: map[string]string{
				CloseAccountBackoffAnnotation: "7200", // 2 hours in seconds
			},
			expectedBackoff: 4 * time.Hour,
		},
		{
			name: "backoff caps at max (24h)",
			annotations: map[string]string{
				CloseAccountBackoffAnnotation: "86400", // 24 hours in seconds
			},
			expectedBackoff: CloseAccountMaxBackoff,
		},
		{
			name: "large backoff caps at max",
			annotations: map[string]string{
				CloseAccountBackoffAnnotation: "172800", // 48 hours in seconds
			},
			expectedBackoff: CloseAccountMaxBackoff,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backoff, _ := CalculateCloseAccountBackoff(tt.annotations)
			if backoff != tt.expectedBackoff {
				t.Errorf("CalculateCloseAccountBackoff() = %v, want %v", backoff, tt.expectedBackoff)
			}
		})
	}
}

func TestSetCloseAccountRateLimited(t *testing.T) {
	t.Run("sets annotations on nil map", func(t *testing.T) {
		result := SetCloseAccountRateLimited(nil)
		if result == nil {
			t.Error("SetCloseAccountRateLimited(nil) returned nil, want non-nil map")
		}
		if _, ok := result[CloseAccountRateLimitAnnotation]; !ok {
			t.Error("SetCloseAccountRateLimited() did not set rate limit annotation")
		}
		if _, ok := result[CloseAccountBackoffAnnotation]; !ok {
			t.Error("SetCloseAccountRateLimited() did not set backoff annotation")
		}
	})

	t.Run("sets annotations on empty map", func(t *testing.T) {
		result := SetCloseAccountRateLimited(map[string]string{})
		if _, ok := result[CloseAccountRateLimitAnnotation]; !ok {
			t.Error("SetCloseAccountRateLimited() did not set rate limit annotation")
		}
	})

	t.Run("preserves existing annotations", func(t *testing.T) {
		annotations := map[string]string{
			"existing-key": "existing-value",
		}
		result := SetCloseAccountRateLimited(annotations)
		if result["existing-key"] != "existing-value" {
			t.Error("SetCloseAccountRateLimited() did not preserve existing annotation")
		}
	})

	t.Run("doubles backoff on subsequent calls", func(t *testing.T) {
		// First call
		annotations := SetCloseAccountRateLimited(nil)
		backoff1, _ := CalculateCloseAccountBackoff(nil)

		// Second call with annotations from first
		annotations = SetCloseAccountRateLimited(annotations)
		backoff2, _ := CalculateCloseAccountBackoff(map[string]string{
			CloseAccountBackoffAnnotation: annotations[CloseAccountBackoffAnnotation],
		})

		// The stored backoff should have doubled
		if backoff2 <= backoff1 {
			t.Errorf("Backoff did not increase: first=%v, second=%v", backoff1, backoff2)
		}
	})
}

func TestClearCloseAccountRateLimited(t *testing.T) {
	t.Run("handles nil map", func(t *testing.T) {
		result := ClearCloseAccountRateLimited(nil)
		if result != nil {
			t.Error("ClearCloseAccountRateLimited(nil) should return nil")
		}
	})

	t.Run("removes rate limit annotations", func(t *testing.T) {
		annotations := map[string]string{
			CloseAccountRateLimitAnnotation: time.Now().Format(time.RFC3339),
			CloseAccountBackoffAnnotation:   "3600",
			"other-annotation":              "value",
		}
		result := ClearCloseAccountRateLimited(annotations)

		if _, ok := result[CloseAccountRateLimitAnnotation]; ok {
			t.Error("ClearCloseAccountRateLimited() did not remove rate limit annotation")
		}
		if _, ok := result[CloseAccountBackoffAnnotation]; ok {
			t.Error("ClearCloseAccountRateLimited() did not remove backoff annotation")
		}
		if result["other-annotation"] != "value" {
			t.Error("ClearCloseAccountRateLimited() removed unrelated annotation")
		}
	})
}

func TestGetCloseAccountRetryAfter(t *testing.T) {
	t.Run("returns zero when not limited", func(t *testing.T) {
		duration := GetCloseAccountRetryAfter(nil)
		if duration != 0 {
			t.Errorf("GetCloseAccountRetryAfter(nil) = %v, want 0", duration)
		}
	})

	t.Run("returns positive duration when limited", func(t *testing.T) {
		retryAt := time.Now().Add(1 * time.Hour)
		annotations := map[string]string{
			CloseAccountRateLimitAnnotation: retryAt.Format(time.RFC3339),
		}
		duration := GetCloseAccountRetryAfter(annotations)
		if duration <= 0 {
			t.Errorf("GetCloseAccountRetryAfter() = %v, want positive duration", duration)
		}
		// Should be approximately 1 hour (allow some tolerance for test execution)
		if duration < 59*time.Minute || duration > 61*time.Minute {
			t.Errorf("GetCloseAccountRetryAfter() = %v, want ~1 hour", duration)
		}
	})

	t.Run("returns zero when backoff expired", func(t *testing.T) {
		retryAt := time.Now().Add(-1 * time.Hour)
		annotations := map[string]string{
			CloseAccountRateLimitAnnotation: retryAt.Format(time.RFC3339),
		}
		duration := GetCloseAccountRetryAfter(annotations)
		if duration != 0 {
			t.Errorf("GetCloseAccountRetryAfter() with expired backoff = %v, want 0", duration)
		}
	})
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
