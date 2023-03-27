FROM quay.io/app-sre/boilerplate:image-v3.0.3 AS builder

ENV OPERATOR_PATH=/go/src/github.com/openshift/aws-account-operator
ENV GO111MODULE=on
ENV GOFLAGS=""
ARG FIPS_ENABLED=true

COPY . ${OPERATOR_PATH}
WORKDIR ${OPERATOR_PATH}

RUN make go-build FIPS_ENABLED=${FIPS_ENABLED}

FROM registry.access.redhat.com/ubi8/ubi-minimal:8.7-1085.1679482090
ENV OPERATOR_BIN=aws-account-operator

WORKDIR /root/
COPY --from=builder /go/src/github.com/openshift/aws-account-operator/build/_output/bin/${OPERATOR_BIN} /usr/local/bin/${OPERATOR_BIN}
LABEL io.openshift.managed.name="aws-account-operator" \
      io.openshift.managed.description="This operator will be responsible for creating and maintaining a pool of AWS accounts."
      
