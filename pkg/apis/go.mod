module github.com/openshift/aws-account-operator/pkg/apis

go 1.13

require (
	github.com/go-openapi/spec v0.19.4
	k8s.io/api v0.15.7
	k8s.io/apimachinery v0.15.7
	k8s.io/kube-openapi v0.0.0-20190918143330-0270cf2f1c1d
)

// Pinned to Kubernetes-1.16.2
replace (
	k8s.io/api => k8s.io/api v0.0.0-20191016110408-35e52d86657a
	k8s.io/apimachinery => k8s.io/apimachinery v0.0.0-20191004115801-a2eda9f80ab8
)
