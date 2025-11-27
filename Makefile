# Makefile for Media Processing Service

.PHONY: help
help:
	@echo "Media Processing Service"
	@echo ""
	@echo "Local Development (recommended):"
	@echo "  local-up       - Full setup: build all, start everything (API, Lambda, LocalStack, Grafana)"
	@echo "  local-down     - Stop all services and destroy resources"
	@echo ""
	@echo "Build:"
	@echo "  build-api      - Build Spring Boot API"
	@echo "  build-lambdas  - Build Lambda JAR"
	@echo "  build-all      - Build everything"
	@echo ""
	@echo "Docker:"
	@echo "  docker-run     - Start all containers"
	@echo "  docker-stop    - Stop all containers"
	@echo "  start-infra    - Start LocalStack + Grafana only"
	@echo ""
	@echo "Terraform (LocalStack):"
	@echo "  tf-init        - Initialize Terraform for LocalStack"
	@echo "  tf-plan        - Plan Terraform changes"
	@echo "  tf-apply       - Apply Terraform to LocalStack"
	@echo "  tf-destroy     - Destroy LocalStack resources"
	@echo ""
	@echo "Dev:"
	@echo "  run-api        - Run API locally (outside Docker)"
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
	@docker compose up -d api

.PHONY: local-down
local-down:
	@echo "Stopping services..."
	@docker compose down --remove-orphans
	@docker ps -a --filter "name=localstack-lambda" -q | xargs -r docker rm -f 2>/dev/null || true
	@echo "All services stopped"

# =============================================================================
# Build
# =============================================================================

.PHONY: build-api
build-api:
	@echo "Building API..."
	@cd app/api && mvn clean package -DskipTests -q

.PHONY: build-lambdas
build-lambdas:
	@echo "Building Lambdas..."
	@cd app/lambdas && mvn clean package -DskipTests -q

.PHONY: build-all
build-all: build-api build-lambdas

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
	@echo "Starting LocalStack and Grafana..."
	@docker compose up -d localstack grafana
	@echo "Waiting for LocalStack to be ready..."
	@sleep 5

# =============================================================================
# Terraform (LocalStack)
# =============================================================================

.PHONY: tf-init
tf-init:
	@cd terraform/local && tflocal init

.PHONY: tf-plan
tf-plan:
	@cd terraform/local && tflocal plan

.PHONY: tf-apply
tf-apply: tf-init
	@echo "Deploying to LocalStack with Terraform..."
	@cd terraform/local && tflocal apply -auto-approve

.PHONY: tf-destroy
tf-destroy:
	@cd terraform/local && tflocal destroy -auto-approve 2>/dev/null || true

.PHONY: tf-output
tf-output:
	@cd terraform/local && tflocal output

# =============================================================================
# Dev
# =============================================================================

.PHONY: run-api
run-api:
	@cd app/api && mvn spring-boot:run

.PHONY: test-api
test-api:
	@cd app/api && mvn test

.PHONY: test-lambdas
test-lambdas:
	@cd app/lambdas && mvn test

.PHONY: clean
clean:
	@cd app/api && mvn clean
	@cd app/lambdas && mvn clean
	@rm -rf terraform/local/.terraform
	@rm -f terraform/local/.terraform.lock.hcl
	@rm -f terraform/local/terraform.tfstate*
	@echo "Cleaned"

# =============================================================================
# AWS Deployment (Production)
# =============================================================================

.PHONY: aws-init
aws-init:
	@cd terraform && terraform init

.PHONY: aws-plan
aws-plan:
	@cd terraform && terraform plan

.PHONY: aws-apply
aws-apply:
	@cd terraform && terraform apply
