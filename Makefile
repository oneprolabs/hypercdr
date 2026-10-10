.PHONY: help dev update-dev stop status test test-backend test-agent test-frontend build-frontend verify fmt lint

help:
	@echo "HyperCDR development commands"
	@echo "  make dev            Start the external-runtime development environment"
	@echo "  make stop           Stop development services"
	@echo "  make update-dev     Rebuild running development services with provenance"
	@echo "  make status         Show development service status"
	@echo "  make test           Run backend, agent, and frontend checks"
	@echo "  make verify         Run tests plus repository consistency checks"

dev:
	./scripts/dev/start-dev.sh

stop:
	./scripts/dev/stop-dev.sh

update-dev:
	./scripts/dev/update-dev.sh

status:
	./scripts/dev/status-dev.sh

test: test-backend test-agent test-frontend

test-backend:
	./scripts/test-backend.sh

test-agent:
	cd agent/comm-agent && go test ./...

test-frontend:
	./scripts/build-frontend.sh

build-frontend:
	./scripts/build-frontend.sh

fmt:
	cd backend && gofmt -w $$(find . -name '*.go' -type f)
	cd agent/comm-agent && gofmt -w $$(find . -name '*.go' -type f)

lint:
	cd backend && go vet ./...
	cd agent/comm-agent && go vet ./...
	test -z "$$(gofmt -l $$(find backend agent/comm-agent -name '*.go' -type f))"

verify: lint test
	python3 -B scripts/tests/deployment-inputs.py
	python3 -B scripts/tests/dev-provenance.py
	python3 -B scripts/tests/repository-hygiene.py
	bash scripts/tests/repository-hygiene.sh
	bash scripts/tests/blue-green-deploy.sh
	bash scripts/tests/blue-green-compose.sh
	bash scripts/tests/release-workflow.sh
	bash scripts/tests/oadp-mirror.sh
	bash scripts/tests/pr-review-notification.sh
	git diff --check -- . ':!third_party/velero'
	test "$$(cat third_party/velero/UPSTREAM_BASELINE)" = "c253c7fe37d78c9b7e55c68544f7c5b2608712d8"
	bash -n third_party/velero/deployments/build-velero-image.sh third_party/velero/hack/build-restic.sh
	find scripts -type f -name '*.sh' -print0 | xargs -0 -n1 bash -n
