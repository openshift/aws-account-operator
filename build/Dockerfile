FROM quay.io/redhat-services-prod/openshift/boilerplate:image-v8.3.6 AS builder

ENV OPERATOR_PATH=/go/src/github.com/openshift/aws-account-operator
ENV GO111MODULE=on
ENV GOFLAGS=""
ARG FIPS_ENABLED=true

COPY . ${OPERATOR_PATH}
WORKDIR ${OPERATOR_PATH}

RUN make go-build FIPS_ENABLED=${FIPS_ENABLED}

FROM registry.access.redhat.com/ubi9/ubi-minimal:9.7-1778072020@sha256:b9b10f42d7eba7ad4a6d5ef26b7d34fdc892b2ffe59b8d0372ec884008569eb6
ENV OPERATOR_BIN=aws-account-operator

WORKDIR /root/
COPY --from=builder /go/src/github.com/openshift/aws-account-operator/build/_output/bin/${OPERATOR_BIN} /usr/local/bin/${OPERATOR_BIN}
LABEL io.openshift.managed.name="aws-account-operator" \
      io.openshift.managed.description="This operator will be responsible for creating and maintaining a pool of AWS accounts."
