# Makefile for Media Processing Service

.PHONY: help
help:
	@echo "Media Processing Service"
	@echo ""
	@echo "Local Development (recommended):"
	@echo "  local-up       - Full setup: build all, start API, Lambda, LocalStack, Redis"
	@echo "  local-start    - Start services with persisted data (no rebuild, no Terraform)"
	@echo "  local-down     - Stop all services (data persists)"
	@echo "  local-clean    - Stop all services AND delete all data"
	@echo ""
	@echo "Build:"
	@echo "  build-common   - Build shared common module"
	@echo "  build-providers - Build generation providers module"
	@echo "  build-api      - Build Spring Boot API"
	@echo "  build-lambdas  - Build Lambda JAR"
	@echo "  build-all      - Build everything"
	@echo ""
	@echo "Docker:"
	@echo "  docker-run     - Start all containers"
	@echo "  docker-stop    - Stop all containers"
	@echo "  start-infra    - Start LocalStack + Redis only"
	@echo ""
	@echo "Redis:"
	@echo "  redis-cli      - Connect to Redis CLI"
	@echo "  redis-flush    - Clear all Redis data"
	@echo ""
	@echo "Terraform (LocalStack):"
	@echo "  tf-init        - Initialize Terraform for LocalStack"
	@echo "  tf-plan        - Plan Terraform changes"
	@echo "  tf-apply       - Apply Terraform to LocalStack"
	@echo "  tf-destroy     - Destroy LocalStack resources"
	@echo ""
	@echo "Dev:"
	@echo "  run-api        - Run API locally (outside Docker)"
	@echo "  run-web        - Run web app"
	@echo "  test-api       - Run API tests"
	@echo "  test-lambdas   - Run Lambda tests"
	@echo "  clean          - Clean build artifacts"
	@echo ""
	@echo "NotebookLM bridge:"
	@echo "  notebooklm-login   - One-time Google sign-in via Playwright Chromium"
	@echo "  notebooklm-import  - Import cookies from Chrome Default authuser=1"
	@echo "                       override with NOTEBOOKLM_CHROME_PROFILE / NOTEBOOKLM_AUTHUSER"
	@echo "  notebooklm-status  - Show session file path, size, and mtime"

# =============================================================================
# Local Development - Full Workflow
# =============================================================================

.PHONY: local-up
local-up: build-all start-infra tf-apply start-api
	@echo ""
	@echo "All services running!"
	@echo "  - API: http://localhost:9000"
	@echo "  - LocalStack: http://localhost:4566"
	@echo "  - Grafana: run 'make start-observability' for http://localhost:3000"

.PHONY: start-api
start-api:
	@mkdir -p "$(HOME)/.notebooklm"
	@echo "Starting API..."
	@docker compose up -d --build api

.PHONY: local-start
local-start:
	@mkdir -p "$(HOME)/.notebooklm"
	@echo "Starting services with persisted data..."
	@docker compose up -d
	@echo ""
	@echo "All services running (using persisted data)!"
	@echo "  - API: http://localhost:9000"
	@echo "  - LocalStack: http://localhost:4566"
	@echo "  - Grafana: run 'make start-observability' for http://localhost:3000"

.PHONY: local-down
local-down:
	@echo "Stopping services (data will persist)..."
	@docker compose down --remove-orphans
	@docker ps -a --filter "name=localstack-lambda" -q | xargs -r docker rm -f 2>/dev/null || true
	@echo "All services stopped. Run 'make local-start' to resume with existing data."

.PHONY: local-clean
local-clean:
	@echo "Stopping services and deleting ALL data..."
	@docker compose down --remove-orphans -v
	@docker ps -a --filter "name=localstack-lambda" -q | xargs -r docker rm -f 2>/dev/null || true
	@rm -rf ./volume
	@echo "All services stopped and data deleted."

# =============================================================================
# Build
# =============================================================================

MAVEN_IMAGE := maven:3.9.6-eclipse-temurin-21
DOCKER_SOCKET := $(shell readlink /var/run/docker.sock 2>/dev/null || echo /var/run/docker.sock)
MAVEN_DOCKER := docker run --rm \
	-v "$(PWD)":/workspace \
	-v maven-repo:/root/.m2 \
	-v "$(DOCKER_SOCKET)":/var/run/docker.sock \
	-e DOCKER_HOST=unix:///var/run/docker.sock \
	-e TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock \
	-e TESTCONTAINERS_HOST_OVERRIDE=host.docker.internal \
	-w /workspace $(MAVEN_IMAGE)

# Maven + Java 21 + Python 3 + notebooklm-py (for the local NotebookLM bridge).
MAVEN_PYTHON_IMAGE := media-service-maven-python:21
MAVEN_PYTHON_DOCKER := docker run --rm -it \
	-v "$(PWD)":/workspace \
	-v maven-repo:/root/.m2 \
	-v "$(DOCKER_SOCKET)":/var/run/docker.sock \
	-v "$(HOME)/.notebooklm":/secrets/notebooklm:ro \
	-e DOCKER_HOST=unix:///var/run/docker.sock \
	-e TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock \
	-e TESTCONTAINERS_HOST_OVERRIDE=host.docker.internal \
	-e NOTEBOOKLM_STORAGE_STATE_PATH=/secrets/notebooklm/state.json \
	-e NOTEBOOKLM_SCRIPT_PATH=/workspace/scripts/notebooklm/overview.py \
	-e NOTEBOOKLM_AUTHUSER=$${NOTEBOOKLM_AUTHUSER:-1} \
	-e GENERATION_AUDIO_OVERVIEW_PROVIDER=$${GENERATION_AUDIO_OVERVIEW_PROVIDER:-simulated} \
	-e MEDIA_GENERATION_QUEUE_URL=http://host.docker.internal:4566/000000000000/generation-jobs \
	-e MEDIA_GENERATION_PAID_QUEUE_URL=http://host.docker.internal:4566/000000000000/generation-jobs-paid \
	-e MEDIA_GENERATION_LOCAL_STAGE_POLLER_ENABLED=true \
	-p 9000:9000 \
	-w /workspace $(MAVEN_PYTHON_IMAGE)

.PHONY: build-common
build-common:
	@echo "Building Common module (Java 21 via Docker)..."
	@$(MAVEN_DOCKER) mvn -f app/common/pom.xml clean install -DskipTests -q

.PHONY: build-api
build-api: build-providers
	@echo "Building API (Java 21 via Docker)..."
	@$(MAVEN_DOCKER) mvn -f app/api/pom.xml clean package -DskipTests -q

.PHONY: build-lambdas
build-lambdas: build-providers
	@echo "Building Lambdas (Java 21 via Docker)..."
	@$(MAVEN_DOCKER) mvn -f app/lambdas/pom.xml clean package -DskipTests -q

.PHONY: build-providers
build-providers: build-common
	@echo "Building Providers module (Java 21 via Docker)..."
	@$(MAVEN_DOCKER) mvn -f app/providers/pom.xml clean install -DskipTests -q

.PHONY: build-all
build-all: build-common build-providers build-api build-lambdas

# Build using local Maven
.PHONY: build-local
build-local:
	@echo "Building with local Maven (requires Java 21)..."
	@cd app/common && mvn clean install -DskipTests -q
	@cd app/providers && mvn clean install -DskipTests -q
	@cd app/api && mvn clean package -DskipTests -q
	@cd app/lambdas && mvn clean package -DskipTests -q

# =============================================================================
# Docker
# =============================================================================

.PHONY: docker-run
docker-run:
	@docker compose up -d

.PHONY: docker-stop
docker-stop:
	@docker compose down

.PHONY: start-infra
start-infra:
	@echo "Starting LocalStack and Redis..."
	@docker compose up -d localstack redis
	@echo "Waiting for LocalStack to be ready..."
	@for i in $$(seq 1 30); do \
	  status=$$(docker inspect -f '{{.State.Health.Status}}' localstack 2>/dev/null || echo starting); \
	  if [ "$$status" = "healthy" ]; then exit 0; fi; \
	  sleep 2; \
	done; \
	docker logs --tail=80 localstack; \
	exit 1

.PHONY: start-observability
start-observability:
	@echo "Starting Grafana/OTel LGTM..."
	@docker compose --profile observability up -d grafana

# =============================================================================
# Redis
# =============================================================================

.PHONY: redis-cli
redis-cli:
	@docker exec -it media-service-redis redis-cli

.PHONY: redis-flush
redis-flush:
	@docker exec media-service-redis redis-cli FLUSHALL
	@echo "Redis data cleared"

# =============================================================================
# Terraform (LocalStack)
# =============================================================================

.PHONY: tf-init
tf-init:
	@cd terraform && tflocal init

.PHONY: tf-plan
tf-plan: tf-init
	@cd terraform && tflocal plan -var-file=local.tfvars

.PHONY: build-generation-worker-image
build-generation-worker-image: build-lambdas ## (AWS only) Build the generation-worker Lambda container image (Java + Python + notebooklm-py). LocalStack community does not support container Lambdas; the API-side stage poller handles NotebookLM in local mode.
	@echo "Building generation-worker Lambda container image..."
	@DOCKER_BUILDKIT=1 docker buildx build \
		--platform linux/amd64 \
		--file app/lambdas/Dockerfile \
		--build-context scripts=./scripts \
		--tag media-service-generation-worker:latest \
		--load \
		app/lambdas

.PHONY: tf-apply
tf-apply: tf-init
	@echo "Deploying to LocalStack with Terraform..."
	@cd terraform && tflocal apply -var-file=local.tfvars -auto-approve
	@$(MAKE) fix-gsi

# Workaround: LocalStack sometimes fails to create all GSIs via Terraform.
# This ensures the auth-related GSIs exist on the DynamoDB table.
.PHONY: fix-gsi
fix-gsi:
	@for idx in \
	  'email-index|AttributeName=email,AttributeType=S|[{"Create":{"IndexName":"email-index","KeySchema":[{"AttributeName":"email","KeyType":"HASH"}],"Projection":{"ProjectionType":"ALL"}}}]' \
	  'tenantId-createdAt-index|AttributeName=tenantId,AttributeType=S AttributeName=createdAt,AttributeType=S|[{"Create":{"IndexName":"tenantId-createdAt-index","KeySchema":[{"AttributeName":"tenantId","KeyType":"HASH"},{"AttributeName":"createdAt","KeyType":"RANGE"}],"Projection":{"ProjectionType":"ALL"}}}]' \
	  'tenantId-index|AttributeName=tenantId,AttributeType=S|[{"Create":{"IndexName":"tenantId-index","KeySchema":[{"AttributeName":"tenantId","KeyType":"HASH"}],"Projection":{"ProjectionType":"ALL"}}}]'; \
	do \
	  name=$$(echo "$$idx" | cut -d'|' -f1); \
	  attrs=$$(echo "$$idx" | cut -d'|' -f2); \
	  updates=$$(echo "$$idx" | cut -d'|' -f3); \
	  exists=$$(aws --endpoint-url=http://localhost:4566 dynamodb describe-table --table-name media --region us-west-2 --query "Table.GlobalSecondaryIndexes[?IndexName=='$$name'].IndexName" --output text 2>/dev/null); \
	  if [ -z "$$exists" ]; then \
	    aws --endpoint-url=http://localhost:4566 dynamodb update-table --table-name media --region us-west-2 --attribute-definitions $$attrs --global-secondary-index-updates "$$updates" > /dev/null 2>&1 && echo "  GSI added: $$name"; \
	  fi; \
	done

.PHONY: tf-destroy
tf-destroy:
	@cd terraform && tflocal destroy -var-file=local.tfvars -auto-approve 2>/dev/null || true

.PHONY: tf-output
tf-output:
	@cd terraform && tflocal output

# =============================================================================
# Dev
# =============================================================================

.PHONY: build-maven-python
build-maven-python: ## Build the Maven+Python derived image used by run-api (cached locally).
	@if docker image inspect $(MAVEN_PYTHON_IMAGE) >/dev/null 2>&1; then \
		echo "Maven+Python image $(MAVEN_PYTHON_IMAGE) already present (delete it to rebuild)."; \
	else \
		echo "Building Maven+Python image $(MAVEN_PYTHON_IMAGE)..."; \
		mkdir -p "$(HOME)/.notebooklm"; \
		DOCKER_BUILDKIT=1 docker buildx build \
			--file infra/Dockerfile.maven-python \
			--build-context scripts=./scripts \
			--tag $(MAVEN_PYTHON_IMAGE) \
			--load \
			infra; \
	fi

.PHONY: run-api
run-api: build-maven-python ## Run API locally (Java + NotebookLM Python bridge). Forwards :9000.
	@mkdir -p "$(HOME)/.notebooklm"
	@echo "Running API locally (Java 21 + Python via Docker)..."
	@$(MAVEN_PYTHON_DOCKER) mvn -f app/api/pom.xml spring-boot:run

.PHONY: run-paid
run-paid: ## Run API locally with real provider (paid). Requires GENERATION_OPENAI_API_KEY env.
	@if [ -z "$$GENERATION_OPENAI_API_KEY" ]; then echo "GENERATION_OPENAI_API_KEY env required"; exit 1; fi
	@echo "Starting API with paid OpenAI provider..."
	cd app/api && GENERATION_PROVIDER=openai GENERATION_MODEL=$${GENERATION_MODEL:-gpt-image-1} \
		mvn -q spring-boot:run -Dspring-boot.run.profiles=local

.PHONY: run-web
run-web:
	@echo "Running web app..."
	@cd app/web && pnpm dev

.PHONY: test-api
test-api:
	@echo "Running API tests (Java 21 via Docker)..."
	@$(MAVEN_DOCKER) mvn -f app/api/pom.xml -Dapi.version=1.41 test

.PHONY: test-lambdas
test-lambdas:
	@echo "Running Lambda tests (Java 21 via Docker)..."
	@$(MAVEN_DOCKER) mvn -f app/lambdas/pom.xml test

.PHONY: clean
clean:
	@$(MAVEN_DOCKER) mvn -f app/common/pom.xml clean -q
	@$(MAVEN_DOCKER) mvn -f app/providers/pom.xml clean -q
	@$(MAVEN_DOCKER) mvn -f app/api/pom.xml clean -q
	@$(MAVEN_DOCKER) mvn -f app/lambdas/pom.xml clean -q
	@rm -rf terraform/.terraform
	@rm -f terraform/.terraform.lock.hcl
	@rm -f terraform/terraform.tfstate*
	@echo "Cleaned"

# =============================================================================
# NotebookLM bridge (host-side login)
# =============================================================================

NOTEBOOKLM_DIR := $(HOME)/.notebooklm
NOTEBOOKLM_VENV := $(NOTEBOOKLM_DIR)/venv
NOTEBOOKLM_STATE := $(NOTEBOOKLM_DIR)/state.json
NOTEBOOKLM_CHROME_PROFILE ?= Default
NOTEBOOKLM_AUTHUSER ?= 1
NOTEBOOKLM_EXPECTED_EMAIL ?= dtszwai@gmail.com
# rookiepy (transitive dep) builds against pyo3 0.20 which caps at Python 3.12.
# Prefer 3.12, fall back to 3.11. Refuse 3.13+.
NOTEBOOKLM_PY := $(shell command -v python3.12 || command -v python3.11)

.PHONY: notebooklm-venv
notebooklm-venv: ## Internal: create venv + install notebooklm-py (no chromium binary).
	@if [ -z "$(NOTEBOOKLM_PY)" ]; then \
		echo "Need python3.11 or python3.12 on PATH (rookiepy/pyo3 caps at 3.12)."; \
		echo "On macOS: brew install python@3.12"; \
		exit 1; \
	fi
	@mkdir -p "$(NOTEBOOKLM_DIR)"
	@if [ -d "$(NOTEBOOKLM_VENV)" ] && [ ! -x "$(NOTEBOOKLM_VENV)/bin/python3.12" ] && [ ! -x "$(NOTEBOOKLM_VENV)/bin/python3.11" ]; then \
		echo "Existing venv uses incompatible Python - recreating..."; \
		rm -rf "$(NOTEBOOKLM_VENV)"; \
	fi
	@if [ ! -d "$(NOTEBOOKLM_VENV)" ]; then \
		echo "Creating venv at $(NOTEBOOKLM_VENV) with $$($(NOTEBOOKLM_PY) --version)..."; \
		$(NOTEBOOKLM_PY) -m venv "$(NOTEBOOKLM_VENV)"; \
	fi
	@"$(NOTEBOOKLM_VENV)/bin/pip" install --quiet --upgrade pip
	@"$(NOTEBOOKLM_VENV)/bin/pip" install --quiet -r scripts/notebooklm/login-requirements.txt

.PHONY: notebooklm-login
notebooklm-login: notebooklm-venv ## One-time Google sign-in via Playwright Chromium. Writes ~/.notebooklm/state.json. Re-run when session expires.
	@"$(NOTEBOOKLM_VENV)/bin/playwright" install chromium
	@echo ""
	@echo "Launching Chromium. Sign in to Google, open NotebookLM, then press Enter in this terminal."
	@"$(NOTEBOOKLM_VENV)/bin/python3" scripts/notebooklm/login.py --out "$(NOTEBOOKLM_STATE)" --authuser "$(NOTEBOOKLM_AUTHUSER)"
	@echo ""
	@echo "Saved -> $(NOTEBOOKLM_STATE)"

.PHONY: notebooklm-import
notebooklm-import: notebooklm-venv ## Import NotebookLM session from Chrome Default authuser=1. Override NOTEBOOKLM_CHROME_PROFILE / NOTEBOOKLM_AUTHUSER / NOTEBOOKLM_EXPECTED_EMAIL.
	@echo "Importing NotebookLM cookies from Chrome profile $(NOTEBOOKLM_CHROME_PROFILE), authuser=$(NOTEBOOKLM_AUTHUSER)..."
	@echo "Expected account: $(NOTEBOOKLM_EXPECTED_EMAIL)"
	@"$(NOTEBOOKLM_VENV)/bin/python3" scripts/notebooklm/import.py \
		--out "$(NOTEBOOKLM_STATE)" \
		--browser chrome \
		--chrome-profile "$(NOTEBOOKLM_CHROME_PROFILE)" \
		--authuser "$(NOTEBOOKLM_AUTHUSER)" \
		--expected-email "$(NOTEBOOKLM_EXPECTED_EMAIL)"
	@echo "Saved -> $(NOTEBOOKLM_STATE)"

.PHONY: notebooklm-status
notebooklm-status: ## Show NotebookLM session file metadata.
	@if [ -f "$(NOTEBOOKLM_STATE)" ]; then \
		echo "state file : $(NOTEBOOKLM_STATE)"; \
		ls -lh "$(NOTEBOOKLM_STATE)" | awk '{print "size       :", $$5; print "modified   :", $$6, $$7, $$8}'; \
	else \
		echo "no state file at $(NOTEBOOKLM_STATE) - run 'make notebooklm-login'"; \
		exit 1; \
	fi

# =============================================================================
# AWS Deployment (Production)
# =============================================================================

.PHONY: aws-init
aws-init:
	@cd terraform && terraform init

.PHONY: aws-plan
aws-plan:
	@cd terraform && terraform plan -var-file=prod.tfvars

.PHONY: aws-apply
aws-apply:
	@cd terraform && terraform apply -var-file=prod.tfvars
