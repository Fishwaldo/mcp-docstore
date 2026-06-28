.PHONY: web build test

web:
	cd web && npm ci && npm run build

build: web
	go build ./...

test:
	go test ./...
	cd web && npm run test
