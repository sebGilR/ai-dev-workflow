.PHONY: build build-darwin-arm64 build-darwin-amd64 build-linux-amd64 build-linux-arm64 build-all test clean install mirrors

INSTALL_ROOT ?= $(HOME)/.claude/ai-dev-workflow

build:
	go build -tags sqlite_ext -o bin/aidw-$(shell go env GOOS)-$(shell go env GOARCH) ./cmd/aidw

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

# Regenerate the .github/skills/ and .github/agents/ mirrors from their
# claude/skills/ and claude/agents/ sources in this checkout. Run this
# after editing any SKILL.md or agent .md file, and before committing —
# the mirrors_test.go drift test fails CI otherwise. Uses `go run` (no
# rebuild needed) with --src pointed at the checkout, not the embedded FS,
# so it always reflects uncommitted local edits.
#
# --prune is passed ONLY here: it deletes files under .github/{skills,agents}
# that no longer exist in claude/, which is what keeps the mirrors orphan-free.
# The flag defaults to false everywhere else so `generate-github-*` pointed at
# an arbitrary --dest can never delete a user's own files.
mirrors:
	go run ./cmd/aidw generate-github-skills --src claude/skills --dest .github/skills --prune
	go run ./cmd/aidw generate-github-agents --src claude/agents --dest .github/agents --prune

install: build
	@mkdir -p "$(INSTALL_ROOT)/bin"
	cp "bin/aidw-$(shell go env GOOS)-$(shell go env GOARCH)" "$(INSTALL_ROOT)/bin/"
	@echo "Installed bin/aidw-$(shell go env GOOS)-$(shell go env GOARCH) → $(INSTALL_ROOT)/bin/"
