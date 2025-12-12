# Bundle Hack Scripts

This directory contains scripts for generating and updating OLM (Operator Lifecycle Manager) bundle manifests.

## update_bundle.sh

Updates OLM bundle manifests with operator image digests and metadata.

### Purpose

- Updates CSV (ClusterServiceVersion) with actual operator image digests
- Adds multi-architecture support labels based on operator image inspection
- Updates timestamps and metadata annotations
- Generates bundle metadata annotations

### Usage

The script is designed to be called from the `Containerfile.bundle` during bundle image builds.

```bash
# Set the operator image (with digest)
export OPERATOR_IMAGE="quay.io/app-sre/aws-account-operator@sha256:abc123..."

# Run the script
./update_bundle.sh
```

### Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `OPERATOR_IMAGE` | Yes | Full operator image reference with digest (e.g., `quay.io/org/operator@sha256:...`) |

### What It Does

1. **Image Inspection**: Uses `skopeo` to inspect the operator image and determine supported architectures
2. **CSV Updates**:
   - Updates operator container image references to use the provided digest
   - Adds/updates `spec.relatedImages[]` with the operator image (required by Konflux)
   - Updates `metadata.annotations.containerImage` annotation
   - Updates `metadata.annotations.createdAt` timestamp
   - Adds architecture support labels (`operatorframework.io/arch.*`)
3. **Metadata Generation**: Creates `metadata/annotations.yaml` with bundle metadata

### Dependencies

- `bash` 4.0+
- `skopeo` - For inspecting container images
- `jq` - For JSON processing
- `python3` with `ruamel.yaml` - For YAML manipulation

### Local Testing

For local development and testing:

```bash
# Build operator image first
make docker-build

# Get the image digest
OPERATOR_IMAGE=$(podman inspect quay.io/app-sre/aws-account-operator:latest --format='{{.Id}}' | sed 's/sha256:/quay.io\/app-sre\/aws-account-operator@sha256:/')

# Prepare bundle directory
mkdir -p bundle/manifests bundle/metadata
cp deploy/crds/*.yaml bundle/manifests/
cp config/templates/csv-template.yaml bundle/manifests/

# Run the script
OPERATOR_IMAGE="${OPERATOR_IMAGE}" ./bundle-hack/update_bundle.sh

# Validate the bundle
operator-sdk bundle validate ./bundle
```

### Customization

Most operators won't need to customize this script. However, if your operator has specific requirements (e.g., multiple related images, custom annotations), you can modify the Python section that updates the CSV.

## Integration with Konflux

In Konflux pipelines:

1. The `bundle-builder` pipeline resolves the operator image tag to a digest
2. The digest is passed as a build arg `OPERATOR_IMAGE_DIGEST` to `Containerfile.bundle`
3. The Containerfile sets `OPERATOR_IMAGE` environment variable and calls this script
4. The script updates the CSV and generates metadata
5. The final bundle image contains the updated manifests

## Troubleshooting

### Script fails with "OPERATOR_IMAGE not set"

Ensure you're setting the environment variable before running the script:
```bash
export OPERATOR_IMAGE="quay.io/org/operator@sha256:..."
```

### Python ModuleNotFoundError for ruamel.yaml

Install the required Python package:
```bash
pip3 install ruamel.yaml
```

### Skopeo command not found

Install skopeo:
```bash
# Fedora/RHEL
sudo dnf install skopeo

# Ubuntu/Debian
sudo apt-get install skopeo
```

### CSV not found

Ensure the CSV template exists at the expected location:
- During build: `/manifests/csv-template.yaml`
- Locally: `config/templates/csv-template.yaml`
