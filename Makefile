# Makefile for Media Processing Service

.PHONY: help
help:
	@echo "Media Processing Service"
	@echo ""
	@echo "Local Development (recommended):"
	@echo "  local-up       - Full setup: build all, start everything (API, Lambda, LocalStack, Grafana, Redis)"
	@echo "  local-start    - Start services with persisted data (no rebuild, no Terraform)"
	@echo "  local-down     - Stop all services (data persists)"
	@echo "  local-clean    - Stop all services AND delete all data"
	@echo ""
	@echo "Build:"
	@echo "  build-common   - Build shared common module"
	@echo "  build-api      - Build Spring Boot API"
	@echo "  build-lambdas  - Build Lambda JAR"
	@echo "  build-all      - Build everything"
	@echo ""
	@echo "Docker:"
	@echo "  docker-run     - Start all containers"
	@echo "  docker-stop    - Stop all containers"
	@echo "  start-infra    - Start LocalStack + Grafana + Redis only"
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

# =============================================================================
# Local Development - Full Workflow
# =============================================================================

.PHONY: local-up
local-up: build-all start-infra tf-apply start-api
	@echo ""
	@echo "All services running!"
	@echo "  - API: http://localhost:9000"
	@echo "  - Grafana: http://localhost:3000"
	@echo "  - LocalStack: http://localhost:4566"

.PHONY: start-api
start-api:
	@echo "Starting API..."
	@docker compose up -d --build api

.PHONY: local-start
local-start:
	@echo "Starting services with persisted data..."
	@docker compose up -d
	@echo ""
	@echo "All services running (using persisted data)!"
	@echo "  - API: http://localhost:9000"
	@echo "  - Grafana: http://localhost:3000"
	@echo "  - LocalStack: http://localhost:4566"

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
MAVEN_DOCKER := docker run --rm -v "$(PWD)":/workspace -v maven-repo:/root/.m2 -w /workspace $(MAVEN_IMAGE)

.PHONY: build-common
build-common:
	@echo "Building Common module (Java 21 via Docker)..."
	@$(MAVEN_DOCKER) mvn -f app/common/pom.xml clean install -DskipTests -q

.PHONY: build-api
build-api: build-common
	@echo "Building API (Java 21 via Docker)..."
	@$(MAVEN_DOCKER) mvn -f app/api/pom.xml clean package -DskipTests -q

.PHONY: build-lambdas
build-lambdas: build-common
	@echo "Building Lambdas (Java 21 via Docker)..."
	@$(MAVEN_DOCKER) mvn -f app/lambdas/pom.xml clean package -DskipTests -q

.PHONY: build-all
build-all: build-common build-api build-lambdas

# Build using local Maven
.PHONY: build-local
build-local:
	@echo "Building with local Maven (requires Java 21)..."
	@cd app/common && mvn clean install -DskipTests -q
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
	@echo "Starting LocalStack, Grafana, and Redis..."
	@docker compose up -d localstack grafana redis
	@echo "Waiting for LocalStack to be ready..."
	@sleep 5

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

.PHONY: run-api
run-api:
	@echo "Running API locally (Java 21 via Docker)..."
	@$(MAVEN_DOCKER) mvn -f app/api/pom.xml spring-boot:run

.PHONY: run-web
run-web:
	@echo "Running web app..."
	@cd app/web && pnpm dev

.PHONY: test-api
test-api:
	@echo "Running API tests (Java 21 via Docker)..."
	@$(MAVEN_DOCKER) mvn -f app/api/pom.xml test

.PHONY: test-lambdas
test-lambdas:
	@echo "Running Lambda tests (Java 21 via Docker)..."
	@$(MAVEN_DOCKER) mvn -f app/lambdas/pom.xml test

.PHONY: clean
clean:
	@$(MAVEN_DOCKER) mvn -f app/common/pom.xml clean -q
	@$(MAVEN_DOCKER) mvn -f app/api/pom.xml clean -q
	@$(MAVEN_DOCKER) mvn -f app/lambdas/pom.xml clean -q
	@rm -rf terraform/.terraform
	@rm -f terraform/.terraform.lock.hcl
	@rm -f terraform/terraform.tfstate*
	@echo "Cleaned"

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
