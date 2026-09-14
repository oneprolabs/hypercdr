FROM python:3.12-alpine
COPY server.py /opt/release-center/server.py
# The data directory may be a host-mounted path; the operator should restrict
# access to the host and provide filesystem permissions appropriate to policy.
ENTRYPOINT ["python", "/opt/release-center/server.py"]
