# Makefile for Knowledge Core

GOPATH := $(shell go env GOPATH)
export PATH := $(GOPATH)/bin:$(PATH)
AIR_VERSION := v1.63.9
VERSION ?= $(shell sed -n 's/^appVersion: *"\(.*\)"/\1/p' deploy/helm/knowledge-core/Chart.yaml)

.PHONY: test test-unit test-acceptance run tidy helm-lint podman-build set-chart-version ui-build ui-dev api-dev dev clean-dev air

tidy:
	go mod tidy

ui-build:
	cd web/ui && npm install && npm run build

# Vite HMR — open http://127.0.0.1:5173/ui/ (API via proxy to :8080)
ui-dev:
	cd web/ui && npm install && npm run dev

air:
	go install github.com/air-verse/air@$(AIR_VERSION)

# Go live-reload (Air). Does not rebuild the SPA.
api-dev: air
	@mkdir -p .tmp web/ui/dist
	@printf '%s\n' '<!doctype html><title>Knowledge Core</title><p>Embedded UI is a stub. Use Vite: <a href="http://127.0.0.1:5173/ui/">http://127.0.0.1:5173/ui/</a></p>' > web/ui/dist/index.html
	KC_BOOTSTRAP_PASSWORD_FILE=$$(pwd)/.tmp/admin.password air -c .air.toml

# Concurrent: Air (Go) + Vite HMR. UI: http://127.0.0.1:5173/ui/
dev:
	./scripts/dev.sh

# Stop Vite/Air, free :5173/:8080, remove tmp/ and .tmp/
clean-dev:
	./scripts/clean-dev.sh

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

# Sync Chart.yaml version + appVersion (also used as default image tag).
# Example: make VERSION=1.2.3 set-chart-version
set-chart-version:
	./deploy/helm/scripts/set-chart-version.sh "$(VERSION)"

# Image tag matches Chart appVersion (override with VERSION=…).
podman-build:
	podman build -f deploy/Dockerfile -t ghcr.io/l-ra/knowledge-core:$(VERSION) .
