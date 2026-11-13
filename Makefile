GO ?= go
VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)
DESKTOP_LDFLAGS := $(LDFLAGS) -X github.com/egoist/mygo.production=1
.DEFAULT_GOAL := build
.PHONY: build cli app run bindings frontend frontend-check frontend-e2e e2e-build vet fmt-check verify e2e design-shots clean tray-icons channel-check

assets/tray-motion.json: assets/logo.svg build/icon/gen.go build/icon/tail.go
	$(GO) run ./build/icon/*.go tray-motion assets/logo.svg assets

tray-icons: assets/tray-motion.json
	@test -f assets/tray.png -a -f assets/tray-active.png -a -f assets/tray-active.svg -a -f assets/tray-motion.png || $(GO) run ./build/icon/*.go tray-motion assets/logo.svg assets

ui/node_modules/.aicat-install: ui/package.json ui/package-lock.json
	npm --prefix ui ci --ignore-scripts
	@touch $@

# Generation executes main, but never WhenReady or gateway/OS initialization.
# An ignored empty embed input breaks the first-build cycle before Vite runs.
bindings: tray-icons
	@mkdir -p ui/dist
	@test -f ui/dist/index.html || touch ui/dist/.keep
	$(GO) tool mygo generate

frontend: ui/node_modules/.aicat-install bindings
	npm --prefix ui run build

frontend-e2e: ui/node_modules/.aicat-install bindings
	npm --prefix ui run check
	npm --prefix ui run build:e2e

bin/channel-host/.ready: $(wildcard runtime/channel-host/wecom-local/*.ts) runtime/channel-host/host.mjs runtime/channel-host/validate.mjs runtime/channel-host/prepare.mjs runtime/channel-host/shim/sdk.mjs runtime/channel-host/shim/text.mjs runtime/channel-host/shim/package.json
	node runtime/channel-host/prepare.mjs

build: tray-icons frontend bin/channel-host/.ready
	@mkdir -p bin
	$(GO) build -tags mygo_noinspector -trimpath -ldflags "$(DESKTOP_LDFLAGS)" -o bin/aicat ./cmd/aicat

cli: bin/channel-host/.ready
	@mkdir -p bin
	CGO_ENABLED=0 $(GO) build -tags nogui -trimpath -ldflags "$(LDFLAGS)" -o bin/aicat-cli ./cmd/aicat

# Manual embed is supported by MyGo. Local bundles remain unsigned;
# its CLI packager would add signing and distribution artifacts.
app: build
	@mkdir -p "build/desktop/aicat.app/Contents/MacOS" "build/desktop/aicat.app/Contents/Resources"
	cp bin/aicat "build/desktop/aicat.app/Contents/MacOS/aicat.next"
	mv -f "build/desktop/aicat.app/Contents/MacOS/aicat.next" "build/desktop/aicat.app/Contents/MacOS/aicat"
	cp build/darwin/aicat.icns "build/desktop/aicat.app/Contents/Resources/aicat.icns"
	rm -rf "build/desktop/aicat.app/Contents/Resources/channel-host"
	cp -R bin/channel-host "build/desktop/aicat.app/Contents/Resources/channel-host"
	sed 's/@VERSION@/0.1.0/g' build/darwin/Info.plist > "build/desktop/aicat.app/Contents/Info.plist"

run: frontend bin/channel-host/.ready
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/aicat-dev ./cmd/aicat
	bin/aicat-dev

vet: bindings
	$(GO) vet ./...

fmt-check:
	@test -z "$$(gofmt -l $$(rg --files cmd internal ui assets -g '*.go' -g '!node_modules'))" || { echo 'Run gofmt on Go sources'; exit 1; }

frontend-check: ui/node_modules/.aicat-install bindings channel-check
	npm --prefix ui run check
	npm --prefix ui run format:check
	node --check e2e/desktop.cjs
	node --check e2e/design.cjs
	node --check e2e/icons.cjs
	node --check e2e/app-tree.cjs
	node --check e2e/list-selection.cjs
	node --check e2e/overview.cjs
	node --check e2e/traffic-default.cjs
	node --check e2e/system-proxy.cjs
	node --check e2e/tray.cjs
	node --check e2e/tray-assets.cjs
	node --check e2e/tunnel.cjs
	node --check e2e/codex.cjs
	node --check e2e/design-shots.cjs
	node --check e2e/wecom.cjs
	node --check e2e/wecom-live.cjs
	node --check e2e/wecom-local-ui.cjs
	python3 -m py_compile e2e/wecom-local.py e2e/wecom-local-live.py
	node --check e2e/channel-install-failure.cjs

verify: frontend-check fmt-check vet build cli app
	$(GO) test ./...
	bin/aicat-cli --version
	bin/aicat-cli serve --help
	python3 e2e/build.py

e2e-build: frontend-e2e bin/channel-host/.ready
	@mkdir -p bin
	$(GO) build -race -tags e2e -trimpath -ldflags "$(DESKTOP_LDFLAGS)" -o bin/aicat-e2e ./cmd/aicat
	$(GO) build -race -tags nogui,e2e -trimpath -ldflags "$(LDFLAGS)" -o bin/aicat-cli-e2e ./cmd/aicat

e2e: e2e-build
	python3 e2e/prototype.py --binary bin/aicat-e2e

design-shots:
	node e2e/design-shots.cjs build/design-shots $(SCENES)

clean:
	rm -rf bin .mygo ui/dist ui/dist-e2e

runtime/channel-host/node_modules/.aicat-install: runtime/channel-host/package.json runtime/channel-host/package-lock.json
	npm --prefix runtime/channel-host ci --ignore-scripts
	@touch $@

channel-check: runtime/channel-host/node_modules/.aicat-install
	npm --prefix runtime/channel-host run check
