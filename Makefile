.PHONY: help dev stop status test test-backend test-agent test-frontend test-bootstrap build-frontend verify fmt

help:
	@echo "HyperCDR development commands"
	@echo "  make dev            Start the external-runtime development environment"
	@echo "  make stop           Stop development services"
	@echo "  make status         Show development service status"
	@echo "  make test           Run backend, agent, and frontend checks"
	@echo "  make verify         Run tests plus repository consistency checks"

dev:
	./scripts/dev/start-dev.sh

stop:
	./scripts/dev/stop-dev.sh

status:
	./scripts/dev/status-dev.sh

test: test-backend test-agent test-frontend test-bootstrap

test-backend:
	cd backend && go test ./...

test-agent:
	cd agent/comm-agent && go test ./...

test-frontend:
	cd frontend && npm run build

test-bootstrap:
	bash bootstrap/tests/registry-ca-flow.sh
	bash scripts/tests/registry-config.sh

build-frontend:
	cd frontend && npm run build

fmt:
	cd backend && gofmt -w $$(find . -name '*.go' -type f)
	cd agent/comm-agent && gofmt -w $$(find . -name '*.go' -type f)

verify: test
	git diff --check
	find scripts bootstrap -type f -name '*.sh' -print0 | xargs -0 -n1 bash -n
