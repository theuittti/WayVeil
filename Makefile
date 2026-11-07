GO ?= go
ASSETS := internal/server/assets

.PHONY: test it scanner lists singbox build e2e

scanner:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "-s -w" -o $(ASSETS)/wv-scan-linux-amd64 ./cmd/wv-scan
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "-s -w" -o $(ASSETS)/wv-scan-linux-arm64 ./cmd/wv-scan

test: scanner singbox
	$(GO) test ./...

it: scanner singbox
	WAYVEIL_IT=1 $(GO) test -tags integration -count=1 -timeout 20m ./...

# Bundled rule-set copies (fallback when GitHub is unreachable). Refresh before releases.
lists:
	mkdir -p rules/lists
	python3 -c 'import json;[print(s["tag"],s["url"]) for s in json.load(open("rules/sources.json"))]' | \
	while read tag url; do curl -fsSL "$$url" -o "rules/lists/$$tag.mrs" || exit 1; done

SINGBOX_VERSION := 1.14.4
# Pinned sing-box binary for tests and local runs (build/ is gitignored).
singbox: build/sing-box
build/sing-box:
	mkdir -p build
	curl -fsSL https://github.com/SagerNet/sing-box/releases/download/v$(SINGBOX_VERSION)/sing-box-$(SINGBOX_VERSION)-linux-amd64.tar.gz | tar -xz -C build --strip-components=1 sing-box-$(SINGBOX_VERSION)-linux-amd64/sing-box
	./build/sing-box version | head -1

build: singbox
	$(GO) build -o build/wayveil ./cmd/wayveil
	$(GO) build -o build/wayveil-dev ./cmd/wayveil-dev
