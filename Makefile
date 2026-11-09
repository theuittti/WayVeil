GO ?= go
# GitHub downloads fail now and then (504 etc.): retry instead of failing the build
CURL := curl -fsSL --retry 5 --retry-all-errors --retry-delay 3 --connect-timeout 15
# fallback through the GitHub API when direct release links fail (needs gh)
GHDL := gh release download --clobber
ASSETS := internal/server/assets

.PHONY: test it scanner lists mihomo xray build e2e frontend app app-smoke ui gui-builder

scanner:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "-s -w" -o $(ASSETS)/wv-scan-linux-amd64 ./cmd/wv-scan
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "-s -w" -o $(ASSETS)/wv-scan-linux-arm64 ./cmd/wv-scan

test: scanner mihomo xray
	$(GO) test ./...

it: scanner mihomo xray
	WAYVEIL_IT=1 $(GO) test -tags integration -count=1 -timeout 60m ./...

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
XRAY_ARM64_SHA256 := 1811c8f1da0c3b6dc707ebaf1c8390e987ba39859a8f0a7aadd8526157f2ec8d
XRAY_SHA256  := e4bd9119f50ae7c41658b1abd0679ae06f98bd864a2907ff33f2f2cf7081b214
# Pinned xray: the verified zip (also uploaded to servers by tests/e2e) and
# the client binary extracted from it. Same release as internal/server.XrayVersion.
xray: build/xray
build/Xray-linux-64.zip:
	mkdir -p build
	$(CURL) https://github.com/XTLS/Xray-core/releases/download/$(XRAY_VERSION)/Xray-linux-64.zip -o $@.tmp || \
	  $(GHDL) $(XRAY_VERSION) -R XTLS/Xray-core -p Xray-linux-64.zip -O $@.tmp
	echo "$(XRAY_SHA256)  $@.tmp" | sha256sum -c --quiet - || { rm -f $@.tmp; echo "xray download does not match the official SHA-256"; exit 1; }
	mv $@.tmp $@
build/xray: build/Xray-linux-64.zip
	rm -rf build/xray.d && mkdir -p build/xray.d
	unzip -qo build/Xray-linux-64.zip xray -d build/xray.d && mv build/xray.d/xray build/xray && rm -rf build/xray.d
	touch build/xray
	./build/xray version | head -1

build/Xray-linux-arm64-v8a.zip:
	mkdir -p build
	$(CURL) https://github.com/XTLS/Xray-core/releases/download/$(XRAY_VERSION)/Xray-linux-arm64-v8a.zip -o $@.tmp || \
	  $(GHDL) $(XRAY_VERSION) -R XTLS/Xray-core -p Xray-linux-arm64-v8a.zip -O $@.tmp
	echo "$(XRAY_ARM64_SHA256)  $@.tmp" | sha256sum -c --quiet - || { rm -f $@.tmp; echo "xray arm64 download does not match the official SHA-256"; exit 1; }
	mv $@.tmp $@

# ---- Desktop app (Wails v2 + Svelte). Builds natively when WebKitGTK headers and npm exist,
# otherwise inside the tools/gui-builder docker image.
frontend:
	cd frontend && npm ci && npm run build

GUI_NATIVE := $(shell pkg-config --exists webkit2gtk-4.1 2>/dev/null && command -v npm >/dev/null 2>&1 && echo yes)
GUI_BUILD := cd frontend && npm ci && npm run build && cd .. && \
	rm -rf cmd/wayveil-gui/frontend_dist && cp -r frontend/dist cmd/wayveil-gui/frontend_dist && \
	CGO_ENABLED=1 $(GO) build -tags desktop,production,webkit2_41 -trimpath -ldflags "-s -w" -o build/wayveil-gui ./cmd/wayveil-gui
GUI_DOCKER := docker run --rm --user $$(id -u):$$(id -g) -e HOME=/tmp -e GOCACHE=/tmp/gocache \
	-e GOMODCACHE=/gomod -v "$$(pwd)":/src -v "$$($(GO) env GOMODCACHE)":/gomod -w /src

gui-builder:
	docker build -t wayveil-gui-builder tools/gui-builder

app: build build/Xray-linux-arm64-v8a.zip
ifeq ($(GUI_NATIVE),yes)
	$(GUI_BUILD)
else
	$(MAKE) gui-builder
	$(GUI_DOCKER) wayveil-gui-builder sh -c '$(GUI_BUILD)'
endif

# Runs the built binary under Xvfb for 5 s; fails if it exits early.
app-smoke:
	$(MAKE) gui-builder
	$(GUI_DOCKER) wayveil-gui-builder sh -c 'test -x build/wayveil-gui && xvfb-run -a sh -c "build/wayveil-gui & pid=\$$!; sleep 5; kill -0 \$$pid && kill \$$pid && echo app-smoke OK"'

# Vite dev server (a later task adds a mock backend).
ui:
	cd frontend && npm run dev

# End-to-end on the disposable test VPS from ~/.config/wayveil-test/vps.env.
e2e: build
	$(GO) test -tags e2e -count=1 -timeout 30m -v -run TestE2EVPS ./internal/engine/
