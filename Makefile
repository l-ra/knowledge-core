# Makefile for Knowledge Core

.PHONY: test test-unit test-acceptance run tidy helm-lint docker-build ui-build ui-dev

tidy:
	go mod tidy

ui-build:
	cd web/ui && npm install && npm run build

ui-dev:
	cd web/ui && npm run dev

test-unit:
	go test ./internal/datatype/... ./internal/bootstrap/...

test-acceptance:
	@test -n "$$KC_DATABASE_URL" || (echo "Set KC_DATABASE_URL"; exit 1)
	go test ./internal/api/http/ -count=1 -v

test: test-unit
	go test ./...

run: ui-build
	go run ./cmd/knowledge-core

helm-lint:
	helm lint deploy/helm/knowledge-core

docker-build:
	docker build -f deploy/Dockerfile -t ghcr.io/l-ra/knowledge-core:dev .
