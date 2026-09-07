.PHONY: run build test cover lint tidy clean demo

run: ## Arranca la API en :8080
	go run ./cmd/server

demo: ## Arranca la API con datos de ejemplo de Austin
	SEED_DEMO=1 go run ./cmd/server

build: ## Compila el binario en bin/server
	go build -o bin/server ./cmd/server

test: ## Ejecuta las pruebas con detector de carreras
	go test -race ./...

cover: ## Informe de cobertura
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

lint: ## Formato y análisis estático
	gofmt -l .
	go vet ./...

tidy: ## Ordena las dependencias
	go mod tidy

clean:
	rm -rf bin coverage.out
