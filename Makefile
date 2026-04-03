.PHONY: dev build serve web

web:
	cd web && npm run build

build: web
	go build -o fido .

serve: build
	./fido serve &

dev: build
	@trap 'kill 0' EXIT; \
	./fido serve & \
	cd web && npm run dev
