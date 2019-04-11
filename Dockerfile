FROM registry.svc.ci.openshift.org/openshift/release:golang-1.10 AS builder
ENV OPERATOR_PATH=/go/src/github.com/openshift/aws-account-operator
COPY . ${OPERATOR_PATH}
WORKDIR ${OPERATOR_PATH}
RUN rm -rf build/_output/bin
RUN make gobuild

FROM registry.svc.ci.openshift.org/openshift/origin-v4.0:base
ENV OPERATOR_PATH=/go/src/github.com/openshift/aws-account-operator \
    OPERATOR_BIN=aws-account-operator

WORKDIR /root/
COPY --from=builder ${OPERATOR_PATH}/build/_output/bin/${OPERATOR_BIN} /usr/local/bin/${OPERATOR_BIN}
ENTRYPOINT ["/usr/local/bin/${OPERATOR_BIN}"]
      
