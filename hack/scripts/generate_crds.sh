#!/bin/bash
set -eo pipefail

# This bash is the replacement for boilerplate's `op-generate` target.

# What you need to run this
# 1. CONTROLLER_GEN_VERSION=v0.3.0
#    go get sigs.k8s.io/controller-tools/cmd/controller-gen@${CONTROLLER_GEN_VERSION}
# 2. YQ_VERSION="3.4.1"
#    https://github.com/mikefarah/yq/releases/3.4.1

REPO_ROOT=$(git rev-parse --show-toplevel)

# Generate CRDs
echo "--> Generating CRDs..."
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
# TODO: Is this actually important?
echo "--> Fixing format to comply with operator-sdk ..."
find ./deploy/crds -name '*_crd.yaml' | while read crd; do
    yq d -i $crd 'metadata.annotations'
    yq d -i $crd 'metadata.creationTimestamp'
    yq d -i $crd 'status'
    yq d -i $crd 'spec.validation.openAPIV3Schema.properties.status.properties.conditions.x-kubernetes-list-map-keys'
    yq d -i $crd 'spec.validation.openAPIV3Schema.properties.status.properties.conditions.x-kubernetes-list-type'
    yq d -i $crd 'spec.validation.openAPIV3Schema.properties.spec.properties.awsManagedPolicies.x-kubernetes-list-type'

    # TODO: Scrap this once v3 is dead
    echo "--> Patching CRDs with openAPIV3Schema: $crd ..."
    yq d -i $crd spec.validation.openAPIV3Schema.type
done
