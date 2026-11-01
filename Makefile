# Needs GNU Make. Recipes stick to what Git Bash, cmd and sh all understand.
.DEFAULT_GOAL := help

.PHONY: help demo serve serve-search web stack down build test e2e web-test web-check

help:
	@echo make demo          run the attack demo once, then stop
	@echo make serve         keep PocketBase, the API and the gateway up until Ctrl+C
	@echo make serve-search  same as serve, with Meilisearch search on
	@echo make web           start the Next.js app on http://localhost:3000
	@echo make stack         run everything in Docker
	@echo make down          stop everything in Docker
	@echo make build         build the Go code
	@echo make test          run the Go tests
	@echo make e2e           run the Go tests against a real PocketBase
	@echo make web-test      run the web tests
	@echo make web-check     typecheck and lint the web app

demo:
	node scripts/run.mjs

serve:
	node scripts/run.mjs --serve

serve-search:
	node scripts/run.mjs --serve --search

web:
	cd web && npm run dev

stack:
	docker compose up --build

down:
	docker compose down

build:
	go build ./...

test:
	go test ./...

e2e:
	go test -tags e2e ./internal/e2e/

web-test:
	cd web && npm test

web-check:
	cd web && npx tsc --noEmit && npm run lint
