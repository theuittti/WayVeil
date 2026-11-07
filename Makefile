GO ?= go
ASSETS := internal/server/assets

.PHONY: test it scanner

scanner:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "-s -w" -o $(ASSETS)/wv-scan-linux-amd64 ./cmd/wv-scan
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "-s -w" -o $(ASSETS)/wv-scan-linux-arm64 ./cmd/wv-scan

test:
	$(GO) test ./...

it:
	WAYVEIL_IT=1 $(GO) test -tags integration -count=1 -timeout 20m ./...
