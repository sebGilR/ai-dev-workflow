.PHONY: build build-darwin-arm64 build-darwin-amd64 build-linux-amd64 build-linux-arm64 build-all test clean install

INSTALL_ROOT ?= $(HOME)/.claude/ai-dev-workflow

build:
	go build -o bin/aidw-$(shell go env GOOS)-$(shell go env GOARCH) ./cmd/aidw

build-darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build -o bin/aidw-darwin-arm64 ./cmd/aidw

build-darwin-amd64:
	GOOS=darwin GOARCH=amd64 go build -o bin/aidw-darwin-amd64 ./cmd/aidw

build-linux-amd64:
	GOOS=linux GOARCH=amd64 go build -o bin/aidw-linux-amd64 ./cmd/aidw

build-linux-arm64:
	GOOS=linux GOARCH=arm64 go build -o bin/aidw-linux-arm64 ./cmd/aidw

build-all: build-darwin-arm64 build-darwin-amd64 build-linux-amd64 build-linux-arm64

test:
	go test ./...

clean:
	rm -f bin/aidw-darwin-arm64 bin/aidw-darwin-amd64 bin/aidw-linux-amd64 bin/aidw-linux-arm64

install: build
	@mkdir -p "$(INSTALL_ROOT)/bin"
	cp "bin/aidw-$(shell go env GOOS)-$(shell go env GOARCH)" "$(INSTALL_ROOT)/bin/"
	@echo "Installed bin/aidw-$(shell go env GOOS)-$(shell go env GOARCH) → $(INSTALL_ROOT)/bin/"
