GO ?= go
# GitHub downloads fail now and then (504 etc.): retry instead of failing the build
CURL := curl -fsSL --retry 5 --retry-all-errors --retry-delay 3 --connect-timeout 15
# fallback through the GitHub API when direct release links fail (needs gh)
GHDL := gh release download --clobber
ASSETS := internal/server/assets

.PHONY: test it scanner lists mihomo xray build e2e

scanner:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "-s -w" -o $(ASSETS)/wv-scan-linux-amd64 ./cmd/wv-scan
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "-s -w" -o $(ASSETS)/wv-scan-linux-arm64 ./cmd/wv-scan

test: scanner mihomo xray
	$(GO) test ./...

it: scanner mihomo xray
	WAYVEIL_IT=1 $(GO) test -tags integration -count=1 -timeout 20m ./...

# Bundled rule-set copies (fallback when GitHub is unreachable). Refresh before releases.
lists:
	mkdir -p rules/lists
	python3 -c 'import json;[print(s["tag"],s["url"]) for s in json.load(open("rules/sources.json"))]' | \
	while read tag url; do $(CURL) "$$url" -o "rules/lists/$$tag.mrs" || exit 1; done

MIHOMO_VERSION := v1.19.33
MIHOMO_SHA256  := 132f6f3edf9a65e62f04853aed3358472e0b2b7666daa8419edfa2ddf5bb6323
# Pinned mihomo binary for tests and local runs (build/ is gitignored).
mihomo: build/mihomo
build/mihomo:
	mkdir -p build
	$(CURL) https://github.com/MetaCubeX/mihomo/releases/download/$(MIHOMO_VERSION)/mihomo-linux-amd64-$(MIHOMO_VERSION).gz -o build/mihomo.gz || \
	  $(GHDL) $(MIHOMO_VERSION) -R MetaCubeX/mihomo -p mihomo-linux-amd64-$(MIHOMO_VERSION).gz -O build/mihomo.gz
	echo "$(MIHOMO_SHA256)  build/mihomo.gz" | sha256sum -c --quiet - || { rm -f build/mihomo.gz; echo "mihomo download does not match the pinned SHA-256"; exit 1; }
	gunzip -c build/mihomo.gz > build/mihomo.tmp && rm build/mihomo.gz
	chmod +x build/mihomo.tmp && mv build/mihomo.tmp build/mihomo
	./build/mihomo -v | head -1

build: mihomo xray
	$(GO) build -o build/wayveil ./cmd/wayveil
	$(GO) build -o build/wayveil-dev ./cmd/wayveil-dev

XRAY_VERSION := v26.10.31
XRAY_SHA256  := e4bd9119f50ae7c41658b1abd0679ae06f98bd864a2907ff33f2f2cf7081b214
# Pinned xray client binary (same release as the server, internal/server.XrayVersion).
xray: build/xray
build/xray:
	mkdir -p build/xray.d
	$(CURL) https://github.com/XTLS/Xray-core/releases/download/$(XRAY_VERSION)/Xray-linux-64.zip -o build/xray.d/x.zip || \
	  $(GHDL) $(XRAY_VERSION) -R XTLS/Xray-core -p Xray-linux-64.zip -O build/xray.d/x.zip
	echo "$(XRAY_SHA256)  build/xray.d/x.zip" | sha256sum -c --quiet - || { rm -rf build/xray.d; echo "xray download does not match the official SHA-256"; exit 1; }
	cd build/xray.d && unzip -qo x.zip xray && mv xray ../xray && mv x.zip ../Xray-linux-64.zip && cd .. && rm -rf xray.d
	./build/xray version | head -1

# End-to-end on the disposable test VPS from ~/.config/wayveil-test/vps.env.
e2e: build
	$(GO) test -tags e2e -count=1 -timeout 30m -v -run TestE2EVPS ./internal/engine/
