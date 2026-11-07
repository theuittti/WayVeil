GO ?= go
ASSETS := internal/server/assets

.PHONY: test it scanner lists mihomo build e2e

scanner:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "-s -w" -o $(ASSETS)/wv-scan-linux-amd64 ./cmd/wv-scan
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "-s -w" -o $(ASSETS)/wv-scan-linux-arm64 ./cmd/wv-scan

test: scanner mihomo
	$(GO) test ./...

it: scanner mihomo
	WAYVEIL_IT=1 $(GO) test -tags integration -count=1 -timeout 20m ./...

# Bundled rule-set copies (fallback when GitHub is unreachable). Refresh before releases.
lists:
	mkdir -p rules/lists
	python3 -c 'import json;[print(s["tag"],s["url"]) for s in json.load(open("rules/sources.json"))]' | \
	while read tag url; do curl -fsSL "$$url" -o "rules/lists/$$tag.mrs" || exit 1; done

MIHOMO_VERSION := v1.19.33
# Pinned mihomo binary for tests and local runs (build/ is gitignored).
mihomo: build/mihomo
build/mihomo:
	mkdir -p build
	curl -fsSL https://github.com/MetaCubeX/mihomo/releases/download/$(MIHOMO_VERSION)/mihomo-linux-amd64-$(MIHOMO_VERSION).gz | gunzip > build/mihomo.tmp
	chmod +x build/mihomo.tmp && mv build/mihomo.tmp build/mihomo
	./build/mihomo -v | head -1

build: mihomo
	$(GO) build -o build/wayveil ./cmd/wayveil
	$(GO) build -o build/wayveil-dev ./cmd/wayveil-dev
