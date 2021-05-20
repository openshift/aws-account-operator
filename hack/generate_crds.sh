#!/bin/bash

# This bash is the replacement for: ./boilerplate/_lib/container-make op-generate
#                               or:  make -n generate

# What you need to run this
# 1. operator-sdk v0.16.0
# 2. CONTROLLER_GEN_VERSION=v0.3.0
#    go get sigs.k8s.io/controller-tools/cmd/controller-gen@${CONTROLLER_GEN_VERSION}
# 3. OPENAPI_GEN_VERSION=v0.19.4
#    go get k8s.io/code-generator/cmd/openapi-gen@${OPENAPI_GEN_VERSION}
# 4. YQ_VERSION="3.4.1"
#    https://github.com/mikefarah/yq/releases/3.4.1

# Generate CRDs
(cd pkg/apis; controller-gen crd paths=./aws/v1alpha1 output:dir=../../deploy/crds)

# Rename CRD filenames to _crd.yaml
for CRD in $(find ./deploy/crds -name '*.yaml'); do
  if ! ls "$CRD" | grep '_crd.yaml'; then
    FILENAME=$(echo "$CRD" | awk -F '.yaml' '{ print $1 }')
    NEW="${FILENAME}_crd.yaml"
    mv "$CRD" "$NEW"
  fi
done

# Fix format to comply with operator-sdk generate crd v0.16.0
find ./deploy/crds -name '*_crd.yaml' | xargs -n1 -I{} yq d -i {} 'metadata.annotations'
find ./deploy/crds -name '*_crd.yaml' | xargs -n1 -I{} yq d -i {} 'metadata.creationTimestamp'
find ./deploy/crds -name '*_crd.yaml' | xargs -n1 -I{} yq d -i {} 'status'
find ./deploy/crds -name '*_crd.yaml' | xargs -n1 -I{} yq d -i {} 'spec.validation.openAPIV3Schema.properties.status.properties.conditions.x-kubernetes-list-map-keys'
find ./deploy/crds -name '*_crd.yaml' | xargs -n1 -I{} yq d -i {} 'spec.validation.openAPIV3Schema.properties.status.properties.conditions.x-kubernetes-list-type'
find ./deploy/crds -name '*_crd.yaml' | xargs -n1 -I{} yq d -i {} 'spec.validation.openAPIV3Schema.properties.spec.properties.awsManagedPolicies.x-kubernetes-list-type'

operator-sdk generate k8s

find deploy/ -name '*_crd.yaml' | xargs -n1 -I{} yq d -i {} spec.validation.openAPIV3Schema.type
# Don't forget to commit generated files
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 GOFLAGS= go generate github.com/openshift/aws-account-operator/cmd/manager github.com/openshift/aws-account-operator/config github.com/openshift/aws-account-operator/pkg/awsclient github.com/openshift/aws-account-operator/pkg/awsclient/mock github.com/openshift/aws-account-operator/pkg/controller github.com/openshift/aws-account-operator/pkg/controller/account github.com/openshift/aws-account-operator/pkg/controller/accountclaim github.com/openshift/aws-account-operator/pkg/controller/accountpool github.com/openshift/aws-account-operator/pkg/controller/awsfederatedaccountaccess github.com/openshift/aws-account-operator/pkg/controller/awsfederatedrole github.com/openshift/aws-account-operator/pkg/controller/testutils github.com/openshift/aws-account-operator/pkg/controller/utils github.com/openshift/aws-account-operator/pkg/localmetrics github.com/openshift/aws-account-operator/pkg/totalaccountwatcher github.com/openshift/aws-account-operator/test/fixtures github.com/openshift/aws-account-operator/test/integration github.com/openshift/aws-account-operator/version
# Don't forget to commit generated files
find ./pkg/apis/ -maxdepth 2 -mindepth 2 -type d | xargs -t -n1 -I% \
		openapi-gen --logtostderr=true \
			-i % \
			-o "" \
			-O zz_generated.openapi \
			-p % \
			-h /dev/null \
			-r "-"
