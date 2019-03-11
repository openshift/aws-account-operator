FROM alpine:3.8

ENV OPERATOR=/usr/local/bin/aws-account-operator \
    USER_UID=1001 \
    USER_NAME=aws-account-operator

# install operator binary
COPY build/_output/bin/aws-account-operator ${OPERATOR}

COPY build/bin /usr/local/bin
RUN  /usr/local/bin/user_setup

ENTRYPOINT ["/usr/local/bin/entrypoint"]

USER ${USER_UID}
