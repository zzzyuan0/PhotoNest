GO_TOOL ?= ./scripts/dev/go-tool.sh

.PHONY: install gen-client format format-check gofmt gofmt-check go-tidy \
	up up-sim down health api worker web \
	dev dev-cos stop status acceptance-check

install:
	pnpm install

gen-client:
	pnpm gen:client

format:
	pnpm format

format-check:
	pnpm format:check

gofmt:
	find cmd internal -name "*.go" -print0 | xargs -0 $(GO_TOOL) gofmt -w

gofmt-check:
	find cmd internal -name "*.go" -print0 | xargs -0 $(GO_TOOL) gofmt -d

go-tidy:
	$(GO_TOOL) go mod tidy

up:
	docker compose up -d postgres redis

up-sim:
	docker compose --profile object-sim up -d postgres redis minio minio-init

down:
	docker compose down -v

health:
	./scripts/dev/healthcheck.sh

api:
	./scripts/dev/api.sh

worker:
	./scripts/dev/worker.sh

web:
	./scripts/dev/web.sh

dev:
	./scripts/dev/start-stack.sh sim

dev-cos:
	./scripts/dev/start-stack.sh cos

stop:
	./scripts/dev/stop-stack.sh

status:
	./scripts/dev/status-stack.sh

acceptance-check:
	./scripts/dev/check-cos-acceptance.sh
