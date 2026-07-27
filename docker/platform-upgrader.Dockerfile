ARG DOCKER_CLI_IMAGE=docker:27-cli
FROM ${DOCKER_CLI_IMAGE}
COPY platform-upgrader /usr/local/bin/platform-upgrader
ENTRYPOINT ["/usr/local/bin/platform-upgrader"]
