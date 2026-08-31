FROM scratch

COPY comm-agent /comm-agent
COPY ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

USER 65532:65532
ENTRYPOINT ["/comm-agent"]
