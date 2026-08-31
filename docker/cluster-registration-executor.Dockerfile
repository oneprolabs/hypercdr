ARG DEBIAN_IMAGE=debian:bookworm-slim
FROM ${DEBIAN_IMAGE}
COPY kubectl /usr/local/bin/kubectl
COPY ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY cluster-registration-executor /usr/local/bin/cluster-registration-executor
COPY curl /usr/local/bin/curl
EXPOSE 18082
ENTRYPOINT ["/usr/local/bin/cluster-registration-executor"]
