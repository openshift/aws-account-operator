#!/bin/bash

set -exv

BRANCH_CHANNEL="$1"
QUAY_IMAGE="$2"

GIT_HASH=$(git rev-parse --short=7 HEAD)
GIT_COMMIT_COUNT=$(git rev-list $(git rev-list --max-parents=0 HEAD)..HEAD --count)

# clone bundle repo
SAAS_OPERATOR_DIR="saas-aws-account-operator-bundle"
BUNDLE_DIR="$SAAS_OPERATOR_DIR/aws-account-operator/"

rm -rf "$SAAS_OPERATOR_DIR"

git clone \
    --branch "$BRANCH_CHANNEL" \
    https://app:"${APP_SRE_BOT_PUSH_TOKEN}"@gitlab.cee.redhat.com/service/saas-aws-account-operator-bundle.git \
    "$SAAS_OPERATOR_DIR"

# remove any versions more recent than deployed hash
REMOVED_VERSIONS=""
if [[ "$REMOVE_UNDEPLOYED" == true ]]; then
    DEPLOYED_HASH=$(
        curl -s 'https://gitlab.cee.redhat.com/service/saas-osd-operators/raw/master/aws-account-operator-services/aws-account-operator.yaml' | \
            docker run --rm -i evns/yq -r '.services[]|select(.name="aws-account-operator").hash'
    )

    delete=false
    # Sort based on commit number
    for version in $(ls $BUNDLE_DIR | sort -t . -k 3 -g); do
        # skip if not directory
        [ -d "$BUNDLE_DIR/$version" ] || continue

        if [[ "$delete" == false ]]; then
            short_hash=$(echo "$version" | cut -d- -f2)

            if [[ "$DEPLOYED_HASH" == "${short_hash}"* ]]; then
                delete=true
            fi
        else
            rm -rf "${BUNDLE_DIR:?BUNDLE_DIR var not set}/$version"
            REMOVED_VERSIONS="$version $REMOVED_VERSIONS"
        fi
    done
fi

# generate bundle
PREV_VERSION=$(ls "$BUNDLE_DIR" | sort -t . -k 3 -g | tail -n 1)

./hack/generate-operator-bundle.py \
    "$BUNDLE_DIR" \
    "$PREV_VERSION" \
    "$GIT_COMMIT_COUNT" \
    "$GIT_HASH" \
    "$QUAY_IMAGE:$GIT_HASH"

NEW_VERSION=$(ls "$BUNDLE_DIR" | sort -t . -k 3 -g | tail -n 1)

if [ "$NEW_VERSION" = "$PREV_VERSION" ]; then
    # stopping script as that version was already built, so no need to rebuild it
    exit 0
fi

# create package yaml
cat <<EOF > $BUNDLE_DIR/aws-account-operator.package.yaml
packageName: aws-account-operator
channels:
- name: $BRANCH_CHANNEL
  currentCSV: aws-account-operator.v${NEW_VERSION}
EOF

# add, commit & push
pushd $SAAS_OPERATOR_DIR

git add .

MESSAGE="add version $GIT_COMMIT_COUNT-$GIT_HASH

replaces $PREV_VERSION
removed versions: $REMOVED_VERSIONS"

git commit -m "$MESSAGE"
git push origin "$BRANCH_CHANNEL"

popd

# build the registry image
REGISTRY_IMG="quay.io/app-sre/aws-account-operator-registry"
DOCKERFILE_REGISTRY="Dockerfile.olm-registry"

cat <<EOF > $DOCKERFILE_REGISTRY
FROM quay.io/openshift/origin-operator-registry:latest

COPY $SAAS_OPERATOR_DIR manifests
RUN initializer --permissive

CMD ["registry-server", "-t", "/tmp/terminate.log"]
EOF

docker build -f $DOCKERFILE_REGISTRY --tag "${REGISTRY_IMG}:${BRANCH_CHANNEL}-latest" .

# push image
skopeo copy --dest-creds "${QUAY_USER}:${QUAY_TOKEN}" \
    "docker-daemon:${REGISTRY_IMG}:${BRANCH_CHANNEL}-latest" \
    "docker://${REGISTRY_IMG}:${BRANCH_CHANNEL}-latest"

skopeo copy --dest-creds "${QUAY_USER}:${QUAY_TOKEN}" \
    "docker-daemon:${REGISTRY_IMG}:${BRANCH_CHANNEL}-latest" \
    "docker://${REGISTRY_IMG}:${BRANCH_CHANNEL}-${GIT_HASH}"
