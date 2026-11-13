.PHONY: run build test test-headless vet package

run:
	go run ./cmd/miniproxy

build:
	go build -o bin/miniproxy ./cmd/miniproxy
	go build -o bin/miniproxy-tun ./cmd/miniproxy-tun

test:
	go test -race ./...

test-headless:
	CGO_ENABLED=0 go test ./internal/app ./internal/autostart ./internal/cli ./internal/profile ./internal/proxy ./internal/tunhelper

vet:
	go vet ./...

package:
	bash scripts/package-macos.sh
