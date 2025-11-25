# Makefile for Media Processing Service

.PHONY: run-all
run-all: build-all docker-run
	@echo "API running at http://localhost:9000"
	@echo "Grafana at http://localhost:3000"

.PHONY: help
help:
	@echo "Media Processing Service"
	@echo ""
	@echo "Build:"
	@echo "  build-api      - Build Spring Boot API"
	@echo "  build-lambdas  - Build Lambda JAR"
	@echo "  build-all      - Build everything"
	@echo ""
	@echo "Docker:"
	@echo "  docker-run     - Start all containers"
	@echo "  docker-stop    - Stop all containers"
	@echo "  start-local    - Start LocalStack + Grafana only"
	@echo ""
	@echo "Dev:"
	@echo "  run-api        - Run API locally"
	@echo "  test-api       - Run API tests"
	@echo "  test-lambdas   - Run Lambda tests"
	@echo "  clean          - Clean build artifacts"

# Build
.PHONY: build-api
build-api:
	@echo "Building API..."
	@cd app/api && mvn clean package -DskipTests

.PHONY: build-lambdas
build-lambdas:
	@echo "Building Lambdas..."
	@cd app/lambdas && mvn clean package -DskipTests

.PHONY: build-all
build-all: build-api build-lambdas

# Docker
.PHONY: docker-run
docker-run:
	@docker compose up -d

.PHONY: docker-stop
docker-stop:
	@docker compose down

.PHONY: start-local
start-local:
	@docker compose up -d localstack grafana

# Dev
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
	@echo "Cleaned"
