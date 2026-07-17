ifneq (,$(wildcard ./.env))
    include .env
    export
endif

.PHONY: run
run:
	go run ./cmd/server

.PHONY: migrate-create
migrate-create:
	goose -s -dir migrations create $(NAME) sql

.PHONY: migrate-up
migrate-up:
	goose -dir migrations postgres "$(DB_URL)" up

.PHONY: migrate-down
migrate-down:
	goose -dir migrations postgres "$(DB_URL)" down

.PHONY: migrate-status
migrate-status:
	goose -dir migrations postgres "$(DB_URL)" status

.PHONY: psql
psql:
	psql "$(DB_URL)"

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: tidy
tidy:
	go mod tidy