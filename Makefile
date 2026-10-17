COMPOSE      := docker compose -f deploy/docker-compose.yml
TELEMETRYGEN := ghcr.io/open-telemetry/opentelemetry-collector-contrib/telemetrygen:v0.161.0
GEN          := docker run --rm --network vindice_default $(TELEMETRYGEN)
CH           := $(COMPOSE) exec -T clickhouse clickhouse-client --user vindice --password vindice -d vindice

.PHONY: build web web-dev test lint up down logs demo stats

build: web
	go build -o bin/vindice ./cmd/vindice

# Build the UI into web/dist, which the binary embeds.
web:
	cd web && npm ci --no-audit --no-fund && npm run build

# UI with hot reload on :5173, proxying /api to a vindice on :8080.
web-dev:
	cd web && npm run dev

test:
	go test -race ./...

lint:
	go vet ./...
	test -z "$$(gofmt -l .)"

up:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f vindice

# Send sample traces, logs and metrics through the running stack.
demo:
	$(GEN) traces  --otlp-endpoint vindice:4317 --otlp-insecure --rate 0 --traces 200 --child-spans 3 --service demo-api
	$(GEN) logs    --otlp-endpoint vindice:4317 --otlp-insecure --rate 0 --logs 1000 --service demo-api
	$(GEN) metrics --otlp-endpoint vindice:4317 --otlp-insecure --rate 0 --metrics 100 --service demo-api

# Row counts and on-disk size per table.
stats:
	$(CH) --query "SELECT table, sum(rows) AS rows, formatReadableSize(sum(data_compressed_bytes)) AS compressed, \
	  formatReadableSize(sum(data_uncompressed_bytes)) AS raw FROM system.parts \
	  WHERE active AND database = 'vindice' GROUP BY table ORDER BY table FORMAT PrettyCompact"
