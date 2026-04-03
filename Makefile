.PHONY: dev build serve

build:
	go build -o fido .

serve: build
	./fido serve &

dev: build
	@trap 'kill 0' EXIT; \
	./fido serve & \
	cd web && npm run dev
