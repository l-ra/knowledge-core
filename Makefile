# Makefile for Knowledge Core

.PHONY: test test-unit test-acceptance run tidy

tidy:
	go mod tidy

test-unit:
	go test ./internal/datatype/...

test-acceptance:
	@test -n "$$KC_DATABASE_URL" || (echo "Set KC_DATABASE_URL"; exit 1)
	go test ./internal/api/http/ -count=1 -v

test: test-unit
	go test ./...

run:
	go run ./cmd/knowledge-core
