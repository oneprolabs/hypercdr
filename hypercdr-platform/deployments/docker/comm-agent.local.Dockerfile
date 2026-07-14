FROM scratch

COPY comm-agent /comm-agent

USER 65532:65532
ENTRYPOINT ["/comm-agent"]
