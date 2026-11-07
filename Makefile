GO ?= go
ASSETS := internal/server/assets

.PHONY: test it scanner lists singbox build e2e

scanner:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "-s -w" -o $(ASSETS)/wv-scan-linux-amd64 ./cmd/wv-scan
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "-s -w" -o $(ASSETS)/wv-scan-linux-arm64 ./cmd/wv-scan

test: scanner
	$(GO) test ./...

it: scanner
	WAYVEIL_IT=1 $(GO) test -tags integration -count=1 -timeout 20m ./...

# Bundled rule-set copies (fallback when GitHub is unreachable). Refresh before releases.
lists:
	python3 -c 'import json;[print(s["tag"],s["url"]) for s in json.load(open("rules/sources.json"))]' | \
	while read tag url; do curl -fsSL "$$url" -o "rules/lists/$$tag.srs" || exit 1; done
