#!/usr/bin/env bash

# Bundle Update Script
# Updates OLM bundle manifests with operator image digests and metadata
# This is a generic script copied to operator repos via boilerplate update
#
# Environment Variables:
#   OPERATOR_IMAGE - Full operator image reference with digest (required)
#                    Example: quay.io/app-sre/operator@sha256:abc123...
#
# Usage:
#   OPERATOR_IMAGE="quay.io/app-sre/operator@sha256:..." ./update_bundle.sh

set -euo pipefail

# Check required environment variables
if [[ -z "${OPERATOR_IMAGE:-}" ]]; then
    echo "ERROR: OPERATOR_IMAGE environment variable is not set"
    echo "Usage: OPERATOR_IMAGE='quay.io/org/image@sha256:...' $0"
    exit 1
fi

echo "Updating bundle with operator image: ${OPERATOR_IMAGE}"

# Paths - configurable for both container and local execution
# In container: /manifests/, /metadata/
# Locally: bundle/manifests/, bundle/metadata/
CSV_FILE="${CSV_FILE:-/manifests/csv-template.yaml}"
METADATA_DIR="${METADATA_DIR:-/metadata}"
MANIFESTS_DIR="${MANIFESTS_DIR:-/manifests}"

# Ensure CSV file exists
if [[ ! -f "${CSV_FILE}" ]]; then
    echo "ERROR: CSV template not found at ${CSV_FILE}"
    exit 1
fi

# Use skopeo to inspect the operator image and get architecture info
# This is optional for local testing - if the image doesn't exist, default to amd64
echo "Inspecting operator image for multi-arch support..."
SUPPORTED_ARCHS="amd64"
if IMAGE_MANIFEST=$(skopeo inspect --raw "docker://${OPERATOR_IMAGE}" 2>/dev/null); then
    # Check if this is a manifest list (multi-arch) or single manifest
    IS_MANIFEST_LIST=$(echo "${IMAGE_MANIFEST}" | jq -r '.schemaVersion == 2 and .mediaType == "application/vnd.docker.distribution.manifest.list.v2+json"')

    if [[ "${IS_MANIFEST_LIST}" == "true" ]]; then
        echo "Image is multi-arch"
        SUPPORTED_ARCHS=$(echo "${IMAGE_MANIFEST}" | jq -r '.manifests[].platform.architecture' | sort -u | tr '\n' ',' | sed 's/,$//')
    else
        echo "Image is single-arch"
        # For single arch, inspect the image to get the architecture
        ARCH=$(skopeo inspect "docker://${OPERATOR_IMAGE}" 2>/dev/null | jq -r '.Architecture // "amd64"')
        SUPPORTED_ARCHS="${ARCH}"
    fi
else
    echo "Warning: Could not inspect image (may not exist in registry yet). Defaulting to amd64."
    echo "This is normal for local testing before pushing the operator image."
fi

echo "Supported architectures: ${SUPPORTED_ARCHS}"

# Export variables for Python script
export SUPPORTED_ARCHS
export CSV_FILE
export METADATA_DIR
export MANIFESTS_DIR

# Update CSV with Python script
python3 << 'PYTHON_SCRIPT'
import os
import sys
from ruamel.yaml import YAML
from datetime import datetime, timezone

yaml = YAML()
yaml.preserve_quotes = True
yaml.default_flow_style = False

# Read environment variables
operator_image = os.environ['OPERATOR_IMAGE']
supported_archs = os.environ['SUPPORTED_ARCHS']
csv_file = os.environ['CSV_FILE']

print(f"Reading CSV from {csv_file}")
with open(csv_file, 'r') as f:
    csv = yaml.load(f)

# Update operator image in deployment spec
if 'spec' in csv and 'install' in csv['spec'] and 'spec' in csv['spec']['install']:
    deployments = csv['spec']['install']['spec'].get('deployments', [])
    for deployment in deployments:
        if 'spec' in deployment and 'template' in deployment['spec']:
            containers = deployment['spec']['template']['spec'].get('containers', [])
            for container in containers:
                # Update the main operator container image
                if 'name' in container and 'operator' in container['name'].lower():
                    print(f"Updating container {container['name']} image to {operator_image}")
                    container['image'] = operator_image

# Update or create relatedImages section (required by Konflux)
if 'relatedImages' not in csv['spec']:
    csv['spec']['relatedImages'] = []

# Add operator image to relatedImages if not already there
operator_related_image = {
    'name': 'operator',
    'image': operator_image
}
# Remove any existing operator entry
csv['spec']['relatedImages'] = [img for img in csv['spec']['relatedImages'] if img.get('name') != 'operator']
# Add the new one
csv['spec']['relatedImages'].append(operator_related_image)

# Update annotations
if 'metadata' not in csv:
    csv['metadata'] = {}
if 'annotations' not in csv['metadata']:
    csv['metadata']['annotations'] = {}

# Update containerImage annotation
csv['metadata']['annotations']['containerImage'] = operator_image

# Update createdAt timestamp
csv['metadata']['annotations']['createdAt'] = datetime.now(timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')

# Add multi-arch support labels
arch_list = supported_archs.split(',')
for arch in ['amd64', 'arm64', 'ppc64le', 's390x']:
    label_key = f'operatorframework.io/arch.{arch}'
    if arch in arch_list:
        csv['metadata']['labels'] = csv['metadata'].get('labels', {})
        csv['metadata']['labels'][label_key] = 'supported'

# Populate CRDs in customresourcedefinitions.owned
import glob
manifests_dir = os.environ['MANIFESTS_DIR']
crd_files = glob.glob(f"{manifests_dir}/*.yaml")
print(f"Found {len(crd_files)} manifest files")

crds_owned = []
for crd_file in crd_files:
    # Skip the CSV template itself
    if 'csv-template' in crd_file or crd_file == csv_file:
        continue

    try:
        with open(crd_file, 'r') as f:
            crd_content = yaml.load(f)

        # Only process CRD files
        if crd_content.get('kind') == 'CustomResourceDefinition':
            # Extract CRD info
            crd_name = crd_content['metadata']['name']
            spec = crd_content.get('spec', {})

            # Get the first version's schema for description
            versions = spec.get('versions', [])
            description = ''
            if versions and len(versions) > 0:
                description = versions[0].get('schema', {}).get('openAPIV3Schema', {}).get('description', '')

            # Build the owned CRD entry
            owned_crd = {
                'name': crd_name,
                'version': spec.get('versions', [{}])[0].get('name', 'v1alpha1'),
                'kind': spec.get('names', {}).get('kind', ''),
                'displayName': spec.get('names', {}).get('kind', ''),
                'description': description or f"{spec.get('names', {}).get('kind', '')} resource"
            }
            crds_owned.append(owned_crd)
            print(f"  Added CRD: {owned_crd['kind']}")
    except Exception as e:
        print(f"  Skipping {os.path.basename(crd_file)}: {e}")

# Update CSV with CRDs
if 'spec' not in csv:
    csv['spec'] = {}
if 'customresourcedefinitions' not in csv['spec']:
    csv['spec']['customresourcedefinitions'] = {}
csv['spec']['customresourcedefinitions']['owned'] = crds_owned

# Write updated CSV
output_csv = csv_file.replace('csv-template.yaml', f"{os.path.basename(csv_file).replace('csv-template', csv['metadata']['name'])}")
print(f"Writing updated CSV to {output_csv}")
with open(output_csv, 'w') as f:
    yaml.dump(csv, f)

# Remove the template file if it's different from output
if output_csv != csv_file and os.path.exists(csv_file):
    os.remove(csv_file)

print("CSV updated successfully")
PYTHON_SCRIPT

# Generate bundle metadata annotations
echo "Generating bundle metadata..."
mkdir -p "${METADATA_DIR}"

cat > "${METADATA_DIR}/annotations.yaml" << EOF
annotations:
  operators.operatorframework.io.bundle.channels.v1: stable
  operators.operatorframework.io.bundle.channel.default.v1: stable
  operators.operatorframework.io.bundle.manifests.v1: manifests/
  operators.operatorframework.io.bundle.metadata.v1: metadata/
  operators.operatorframework.io.bundle.package.v1: aws-account-operator
  operators.operatorframework.io.bundle.mediatype.v1: registry+v1
EOF

echo "Bundle update complete!"
echo "Manifests directory: ${MANIFESTS_DIR}"
echo "Metadata directory: ${METADATA_DIR}"
