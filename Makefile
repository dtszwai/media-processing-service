SHELL := /usr/bin/env bash

GO        ?= go
BUF       ?= buf
PNPM      ?= pnpm
PYTHON    ?= $(shell test -x $(HOME)/.notebooklm/venv/bin/python && echo $(HOME)/.notebooklm/venv/bin/python || echo python3)
TFLOCAL   ?= tflocal
COMPOSE   ?= docker compose -f deploy/compose/local.yaml
LOG_DIR   := .local/logs
LAMBDA_DIR := backend/dist/lambda
NOTEBOOKLM_STATE ?= $(HOME)/.notebooklm/state.json

LAMBDA_SOURCES := \
	workers/generation:generation-worker \
	workers/media:media-worker \
	workers/webhook:webhook-worker \
	workers/analytics:analytics-worker \
	workers/cleanup:cleanup-worker \
	workers/upload-events:upload-events-worker \
	workers/outbox-relay:outbox-relay \
	cron/analytics-rollup:analytics-rollup \
	cron/lease-reaper:lease-reaper

.PHONY: help
help:
	@echo "Daily inner loop:"
	@echo "  up             - regen idl + start compose stack, wait for /healthz"
	@echo "  down           - stop compose stack (keep data under .local/volume)"
	@echo "  clean          - stop compose + wipe .local/volume + backend/dist + terraform state"
	@echo "  web            - start the frontend dev server"
	@echo
	@echo "Terraform / LocalStack:"
	@echo "  tf-up          - cross-compile Lambda bootstrap zips + tflocal init + apply"
	@echo "  tf-down        - tflocal destroy"
	@echo
	@echo "Backend gate (CI parity):"
	@echo "  build          - go build ./... in backend/"
	@echo "  test           - go test ./... in backend/"
	@echo
	@echo "Provider auth (run on host; container reads via bind-mount):"
	@echo "  notebooklm-import - refresh ~/.notebooklm/state.json from Chrome cookies (quit Chrome first)"
	@echo
	@echo "Mental model: 'up' = minimum for healthy api. 'tf-up' = LocalStack-supported topology + Lambdas."

.PHONY: build
build:
	$(GO) -C backend build ./...

.PHONY: test
test:
	$(GO) -C backend test ./...

.PHONY: up
up: proto
	mkdir -p $(LOG_DIR) .local/volume/localstack
	$(COMPOSE) up -d --build
	@echo "Waiting for api :9000/healthz ..."
	@ok=0; for i in $$(seq 1 60); do \
		if curl -fsS http://localhost:9000/healthz >/dev/null 2>&1; then ok=1; break; fi; \
		sleep 2; \
	done; \
	if [ $$ok -eq 1 ]; then echo "api ready"; else echo "api did not become healthy"; $(COMPOSE) logs api; exit 1; fi

.PHONY: down
down:
	$(COMPOSE) down

.PHONY: clean
clean:
	$(COMPOSE) down -v
	rm -rf .local/volume backend/dist terraform/.terraform terraform/terraform.tfstate*

.PHONY: web
web:
	$(PNPM) -C frontend dev

.PHONY: proto
proto:
	rm -rf backend/pkg/contracts frontend/packages/api-client/src/gen
	cd idl && $(BUF) lint
	cd idl && $(BUF) generate

.PHONY: tf-up
tf-up:
	@for entry in $(LAMBDA_SOURCES); do \
		src=$${entry%%:*}; \
		name=$${entry##*:}; \
		echo "==> bootstrap $$name (from cmd/$$src)"; \
		mkdir -p $(LAMBDA_DIR)/$$name; \
		GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
			$(GO) -C backend build -tags lambda.norpc -o ../$(LAMBDA_DIR)/$$name/bootstrap ./cmd/$$src; \
	done
	cd terraform && $(TFLOCAL) init && $(TFLOCAL) apply -auto-approve

.PHONY: tf-down
tf-down:
	cd terraform && $(TFLOCAL) destroy -auto-approve

# Captures NotebookLM cookies into a storage_state.json
NOTEBOOKLM_CHROME_PROFILE ?= Default
NOTEBOOKLM_AUTHUSER       ?= 1
.PHONY: notebooklm-import
notebooklm-import:
	mkdir -p $(dir $(NOTEBOOKLM_STATE))
	$(PYTHON) scripts/notebooklm/import.py \
		--out $(NOTEBOOKLM_STATE) \
		--chrome-profile $(NOTEBOOKLM_CHROME_PROFILE) \
		--authuser $(NOTEBOOKLM_AUTHUSER) \
