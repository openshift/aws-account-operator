#!/bin/bash

set -euo pipefail

# Simplified PROW test runner for Go integration tests
# This script uses PROW's pre-built operator image instead of rebuilding in-cluster

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# Configuration
OPERATOR_NAMESPACE="${OPERATOR_NAMESPACE:-aws-account-operator}"
OPERATOR_IMAGE="${IMAGE_FORMAT:-}" # PROW sets this
TIMEOUT="${TEST_TIMEOUT:-30m}"

cd "${REPO_ROOT}"

echo "========================================================================"
echo "= Go Integration Test Runner (PROW)"
echo "========================================================================"
echo "Operator Namespace: ${OPERATOR_NAMESPACE}"
echo "Operator Image: ${OPERATOR_IMAGE}"
echo "Test Timeout: ${TIMEOUT}"
echo ""

# Cleanup function
cleanup() {
    echo ""
    echo "========================================================================"
    echo "= Cleanup"
    echo "========================================================================"

    # Delete operator deployment
    oc delete deployment aws-account-operator -n ${OPERATOR_NAMESPACE} --ignore-not-found=true

    # Delete namespace
    oc delete namespace ${OPERATOR_NAMESPACE} --ignore-not-found=true --timeout=5m || true

    echo "✓ Cleanup complete"
}

# Register cleanup on exit
trap cleanup EXIT

echo "========================================================================"
echo "= Setup: Creating namespace and deploying operator"
echo "========================================================================"

# Create namespace
oc create namespace ${OPERATOR_NAMESPACE} || true

# Wait for namespace to be ready
echo "Waiting for namespace to be ready..."
for i in {1..10}; do
    if oc get namespace ${OPERATOR_NAMESPACE} -o jsonpath='{.status.phase}' 2>/dev/null | grep -q "Active"; then
        echo "✓ Namespace is ready"
        break
    fi
    echo "Waiting for namespace (attempt $i/10)..."
    sleep 2
done

# Deploy CRDs (cluster-scoped, no namespace needed)
echo "Deploying CRDs..."
ls deploy/crds/*.yaml | xargs -L1 oc apply -f
echo "✓ CRDs deployed"

# Deploy operator resources (service account, roles, etc.)
echo "Deploying operator resources..."
oc apply -f deploy/service_account.yaml -n ${OPERATOR_NAMESPACE}
oc apply -f deploy/cluster_role.yaml
oc apply -f deploy/cluster_role_binding.yaml
oc apply -f deploy/role.yaml -n ${OPERATOR_NAMESPACE}
oc apply -f deploy/role_binding.yaml -n ${OPERATOR_NAMESPACE}
echo "✓ Operator resources deployed"

# Deploy operator with PROW's pre-built image
echo "Deploying operator..."
# Use PROW's IMAGE_FORMAT to construct the image reference
# IMAGE_FORMAT is like: "registry.ci.openshift.org/ci-op-xxx/pipeline:${component}"
OPERATOR_IMAGE_REF="${IMAGE_FORMAT//\$\{component\}/pipeline:src}"

cat <<EOF | oc apply -n ${OPERATOR_NAMESPACE} -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: aws-account-operator
  namespace: ${OPERATOR_NAMESPACE}
spec:
  replicas: 1
  selector:
    matchLabels:
      name: aws-account-operator
  template:
    metadata:
      labels:
        name: aws-account-operator
    spec:
      serviceAccountName: aws-account-operator
      containers:
      - name: aws-account-operator
        image: ${OPERATOR_IMAGE_REF}
        command:
        - aws-account-operator
        env:
        - name: WATCH_NAMESPACE
          value: ""
        - name: POD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: OPERATOR_NAME
          value: aws-account-operator
        - name: FORCE_DEV_MODE
          value: cluster
        resources:
          limits:
            cpu: 200m
            memory: 2Gi
EOF

echo "✓ Operator deployed"

# Wait for operator to be ready
echo "Waiting for operator to be ready..."
oc rollout status deployment/aws-account-operator -n ${OPERATOR_NAMESPACE} --timeout=5m
echo "✓ Operator is ready"

echo ""
echo "========================================================================"
echo "= Running Go Integration Tests"
echo "========================================================================"

# Run Go tests
cd test/integration
go test -v ./tests -timeout ${TIMEOUT}

TEST_RESULT=$?

echo ""
if [ ${TEST_RESULT} -eq 0 ]; then
    echo "========================================================================"
    echo "= ✓ GO INTEGRATION TESTS PASSED"
    echo "========================================================================"
else
    echo "========================================================================"
    echo "= ✗ GO INTEGRATION TESTS FAILED"
    echo "========================================================================"
fi

exit ${TEST_RESULT}
