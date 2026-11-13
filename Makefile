.PHONY: prepare update-fyne-patch run build test test-headless vet package

prepare:
	go run ./scripts/prepare-fyne.go

update-fyne-patch:
	go run ./scripts/prepare-fyne.go -refresh-patch

run: build
	./bin/loop

build: prepare
	go build -o bin/loop ./cmd/loop
	go build -o bin/loopd ./cmd/loopd
	go build -o bin/loopctl ./cmd/loopctl
	go build -o bin/loop-tun ./cmd/loop-tun

test: prepare
	go test -race -tags test fyne.io/fyne/v2/internal/painter
	go test -race ./...

test-headless: prepare
	CGO_ENABLED=0 go test ./internal/app ./internal/autostart ./internal/cli ./internal/control ./internal/statusbar ./internal/profile ./internal/proxy ./internal/tunhelper

vet: prepare
	go vet ./...

package:
	bash scripts/package-macos.sh
