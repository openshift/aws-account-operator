FROM registry.svc.ci.openshift.org/openshift/release:golang-1.10 AS builder
COPY . /go/src/github.com/openshift/aws-account-operator
WORKDIR /go/src/github.com/openshift/aws-account-operator
RUN make gobuild

FROM registry.access.redhat.com/ubi8/ubi-minimal:latest
ENV OPERATOR_BIN=aws-account-operator

WORKDIR /root/
COPY --from=builder /go/src/github.com/openshift/aws-account-operator/build/_output/bin/${OPERATOR_BIN} /usr/local/bin/${OPERATOR_BIN}
LABEL io.openshift.managed.name="aws-account-operator" \
      io.openshift.managed.description="This operator will be responsible for creating and maintaining a pool of AWS accounts."
      
