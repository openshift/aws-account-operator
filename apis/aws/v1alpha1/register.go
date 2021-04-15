// NOTE: Boilerplate only.  Ignore this file.

// Package v1alpha1 contains API Schema definitions for the aws v1alpha1 API group
// +k8s:deepcopy-gen=package,register
// +groupName=aws.managed.openshift.io
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"github.com/openshift/aws-account-operator/apis/scheme"
)

var (
	// SchemeGroupVersion is group version used to register these objects
	SchemeGroupVersion = schema.GroupVersion{Group: "aws.managed.openshift.io", Version: "v1alpha1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme
	SchemeBuilder = &scheme.Builder{GroupVersion: SchemeGroupVersion}

	// AddToScheme is a shortcut for SchemeBuilder.AddToScheme
	AddToScheme = SchemeBuilder.AddToScheme
)

// Resource takes an unqualified resource and returns a Group qualified GroupResource
func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}
