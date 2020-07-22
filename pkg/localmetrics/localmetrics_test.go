package localmetrics

import (
	neturl "net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPathParse(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "core non-namespaced kind",
			path:     "/api/v1/pods",
			expected: "core/v1/pods",
		},
		{
			name:     "core non-namespaced named resource",
			path:     "/api/v1/nodes/nodename",
			expected: "core/v1/nodes/{NAME}",
		},
		{
			name:     "core namespaced named resource",
			path:     "/api/v1/namespaces/aws-account-operator/configmaps/foo-bar-baz",
			expected: "core/v1/namespaces/{NAMESPACE}/configmaps/{NAME}",
		},
		{
			name:     "core namespaced named resource with sub-resource",
			path:     "/api/v1/namespaces/aws-account-operator/secret/foo-bar-baz/status",
			expected: "core/v1/namespaces/{NAMESPACE}/secret/{NAME}/status",
		},
		{
			name:     "extension non-namespaced kind",
			path:     "/apis/batch/v1/jobs",
			expected: "batch/v1/jobs",
		},
		{
			name:     "extension namespaced kind",
			path:     "/apis/batch/v1/namespaces/aws-account-operator/jobs",
			expected: "batch/v1/namespaces/{NAMESPACE}/jobs",
		},
		{
			name:     "extension namespaced named resource",
			path:     "/apis/batch/v1/namespaces/aws-account-operator/jobs/foo-bar-baz",
			expected: "batch/v1/namespaces/{NAMESPACE}/jobs/{NAME}",
		},
		{
			name:     "extension namespaced named resource with sub-resource",
			path:     "/apis/aws.managed.openshift.io/v1alpha1/namespaces/aws-account-operator/accountpool/foo-bar-baz/status",
			expected: "aws.managed.openshift.io/v1alpha1/namespaces/{NAMESPACE}/accountpool/{NAME}/status",
		},
		{
			name:     "core root (discovery)",
			path:     "/api",
			expected: "core",
		},
		{
			name:     "core version (discovery)",
			path:     "/api/v1",
			expected: "core/v1",
		},
		{
			name:     "extension discovery",
			path:     "/apis/aws.managed.openshift.io/v1",
			expected: "aws.managed.openshift.io/v1",
		},
		{
			name:     "unknown root",
			path:     "/weird/path/to/resource",
			expected: "{OTHER}",
		},
		{
			name:     "empty to make Split fail",
			path:     "",
			expected: "{OTHER}",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := resourceFrom(&neturl.URL{Path: test.path})
			assert.Equal(t, test.expected, result)
		})
	}

}
