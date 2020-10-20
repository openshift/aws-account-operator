module github.com/openshift/aws-account-operator

go 1.13

require (
	github.com/avast/retry-go v2.6.1+incompatible
	github.com/aws/aws-sdk-go v1.34.14
	github.com/evanphx/json-patch v4.5.0+incompatible // indirect
	github.com/fsnotify/fsnotify v1.4.9 // indirect
	github.com/go-logr/logr v0.1.0
	github.com/golang/mock v1.3.1-0.20190508161146-9fa652df1129
	github.com/gregjones/httpcache v0.0.0-20190212212710-3befbb6ad0cc // indirect
	github.com/onsi/ginkgo v1.12.0
	github.com/onsi/gomega v1.9.0
	github.com/openshift/api v3.9.0+incompatible
	github.com/openshift/operator-custom-metrics v0.2.1
	github.com/operator-framework/operator-sdk v0.8.1
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v0.9.4
	github.com/rkt/rkt v1.30.0
	github.com/spf13/pflag v1.0.3
	github.com/stretchr/testify v1.5.1
	golang.org/x/xerrors v0.0.0-20191204190536-9bdfabe68543 // indirect
	gopkg.in/yaml.v2 v2.2.4
	k8s.io/api v0.0.0-20190612125737-db0771252981
	k8s.io/apimachinery v0.0.0-20190612125636-6a5db36e93ad
	k8s.io/client-go v11.0.0+incompatible
	sigs.k8s.io/controller-runtime v0.1.12
)

replace gopkg.in/fsnotify.v1 v1.4.9 => github.com/fsnotify/fsnotify v1.4.9

// Fixes missing bitbucket dependency; replaces with same fork k8s uses
replace bitbucket.org/ww/goautoneg => github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822

// Operator-SDK rules below
// Pinned to kubernetes-1.13.4
replace (
	k8s.io/api => k8s.io/api v0.0.0-20190222213804-5cb15d344471
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.0.0-20190228180357-d002e88f6236
	k8s.io/apimachinery => k8s.io/apimachinery v0.0.0-20190221213512-86fb29eff628
	k8s.io/client-go => k8s.io/client-go v0.0.0-20190228174230-b40b2a5939e4
)

replace (
	github.com/coreos/prometheus-operator => github.com/coreos/prometheus-operator v0.29.0
	// Pinned to v2.9.2 (kubernetes-1.13.1) so https://proxy.golang.org can
	// resolve it correctly.
	github.com/prometheus/prometheus => github.com/prometheus/prometheus v0.0.0-20190424153033-d3245f150225
	k8s.io/kube-state-metrics => k8s.io/kube-state-metrics v1.6.0
	sigs.k8s.io/controller-runtime => sigs.k8s.io/controller-runtime v0.1.12
	sigs.k8s.io/controller-tools => sigs.k8s.io/controller-tools v0.1.11-0.20190411181648-9d55346c2bde
)

replace github.com/operator-framework/operator-sdk => github.com/operator-framework/operator-sdk v0.10.0
