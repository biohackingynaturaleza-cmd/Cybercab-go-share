.PHONY: run build test cover lint tidy clean demo

run: ## Arranca la API en :8080
	go run ./cmd/server

demo: ## Arranca la API con datos de ejemplo de Austin
	SEED_DEMO=1 go run ./cmd/server

simulacion: ## Mide cuánta más demanda sirve la misma flota al compartir
	go run ./cmd/simulacion

build: ## Compila el binario en bin/server
	go build -o bin/server ./cmd/server

test: ## Ejecuta las pruebas con detector de carreras
	go test -race ./...

test-pg: ## Pruebas incluyendo las de Postgres (necesita TEST_DATABASE_URL)
	TEST_DATABASE_URL=$${TEST_DATABASE_URL:-postgres://cybercab:cybercab@127.0.0.1:5432/cybercab_test} \
		go test -race ./...

cover: ## Informe de cobertura
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

lint: ## Formato y análisis estático
	gofmt -l .
	go vet ./...

tidy: ## Ordena las dependencias
	go mod tidy

docker: ## Construye la imagen
	docker build -t cybercab-go-share .

arriba: ## Levanta app + Postgres con PostGIS
	docker compose up --build

clean:
	rm -rf bin coverage.out
