.PHONY: dev build serve web

VERSION ?= dev

web:
	cd web && npm run build

build: web
	go build -ldflags "-X github.com/jorgenbs/fido/internal/version.Version=$(VERSION)" -o fido .

serve: build
	./fido serve &

dev: build
	@trap 'kill 0' EXIT; \
	./fido serve & \
	cd web && npm run dev
