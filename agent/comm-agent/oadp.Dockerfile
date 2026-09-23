FROM scratch

COPY oadp-comm-agent /oadp-comm-agent
COPY ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

ENV HCDR_BACKUP_BACKEND=oadp
USER 65532:65532
ENTRYPOINT ["/oadp-comm-agent"]
