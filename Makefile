GO_IMAGE ?= golang:1.24

.PHONY: install gen-client format format-check gofmt gofmt-check go-tidy up up-sim down health api worker

install:
	pnpm install

gen-client:
	pnpm gen:client

format:
	pnpm format

format-check:
	pnpm format:check

gofmt:
	docker run --rm -v "$(CURDIR):/workspace" -w /workspace $(GO_IMAGE) \
		sh -lc 'find cmd internal -name "*.go" -print0 | xargs -0 gofmt -w'

gofmt-check:
	docker run --rm -v "$(CURDIR):/workspace" -w /workspace $(GO_IMAGE) \
		sh -lc 'find cmd internal -name "*.go" -print0 | xargs -0 gofmt -d'

go-tidy:
	docker run --rm -v "$(CURDIR):/workspace" -w /workspace $(GO_IMAGE) go mod tidy

up:
	docker compose up -d postgres redis

up-sim:
	docker compose --profile object-sim up -d postgres redis minio

down:
	docker compose down -v

health:
	./scripts/dev/healthcheck.sh

api:
	./scripts/dev/api.sh

worker:
	./scripts/dev/worker.sh
