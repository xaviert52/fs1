# Configuración básica
BINARY_NAME=subscription-service
GO=go
DOCKER=docker
DOCKER_COMPOSE=docker compose

# Rutas
CMD_DIR=./cmd/subscription-service
DOCS_DIR=./docs

# Comandos principales
.PHONY: all build run test clean docker-up docker-down swagger help

help: ## Muestra esta ayuda
	@echo 'Uso: make [TARGET]'
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

all: tidy swagger build ## Instala dependencias, genera docs y compila

build: ## Compila el binario para tu OS actual
	@echo "🏗️  Compilando servicio..."
	$(GO) build -o bin/$(BINARY_NAME) $(CMD_DIR)/main.go
	@echo "✅ Compilación exitosa: bin/$(BINARY_NAME)"

build-linux: ## Compila el binario para Linux (AMD64)
	@echo "🐧 Compilando para Linux..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -o bin/$(BINARY_NAME)-linux $(CMD_DIR)/main.go
	@echo "✅ Compilación Linux exitosa"

run: build ## Compila y ejecuta localmente (lee .env)
	@echo "🚀 Ejecutando servicio..."
	./bin/$(BINARY_NAME)

dev: ## Ejecuta directamente con go run (hot reload si usas air, o simple run)
	$(GO) run $(CMD_DIR)/main.go

test: ## Ejecuta los tests unitarios
	@echo "🧪 Ejecutando tests..."
	$(GO) test -v ./...

test-integration: ## Ejecuta el test de integración de WebSockets
	@echo "🔌 Probando conexión WebSocket..."
	$(GO) run test_websocket.go

clean: ## Limpia binarios y archivos temporales
	@echo "🧹 Limpiando..."
	$(GO) clean
	rm -rf bin/
	rm -rf $(DOCS_DIR)

tidy: ## Descarga dependencias y limpia go.mod
	$(GO) mod tidy

swagger: ## Genera/Actualiza documentación Swagger
	@echo "📄 Generando documentación Swagger..."
	swag init -g $(CMD_DIR)/main.go --output $(DOCS_DIR)

# Docker
docker-up: swagger ## Levanta el entorno con Docker Compose
	$(DOCKER_COMPOSE) up --build -d

docker-down: ## Detiene y elimina contenedores
	$(DOCKER_COMPOSE) down -v --remove-orphans

docker-logs: ## Muestra logs de los contenedores
	$(DOCKER_COMPOSE) logs -f
