GO ?= go
VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)
DESKTOP_LDFLAGS := $(LDFLAGS) -X github.com/egoist/mygo.production=1
.DEFAULT_GOAL := build
.PHONY: build cli app run bindings frontend frontend-check frontend-e2e e2e-build vet fmt-check verify e2e clean

assets/tray.png: assets/logo.svg build/icon/gen.go
	$(GO) run ./build/icon/gen.go tray assets/logo.svg assets/tray.png

ui/node_modules/.aicat-install: ui/package.json ui/package-lock.json
	npm --prefix ui ci --ignore-scripts
	@touch $@

# Generation executes main, but never WhenReady or gateway/OS initialization.
# An ignored empty embed input breaks the first-build cycle before Vite runs.
bindings:
	@mkdir -p ui/dist
	@test -f ui/dist/index.html || touch ui/dist/.keep
	$(GO) tool mygo generate

frontend: ui/node_modules/.aicat-install bindings
	npm --prefix ui run build

frontend-e2e: ui/node_modules/.aicat-install bindings
	npm --prefix ui run check
	npm --prefix ui run build:e2e

build: assets/tray.png frontend
	@mkdir -p bin
	$(GO) build -tags mygo_noinspector -trimpath -ldflags "$(DESKTOP_LDFLAGS)" -o bin/aicat ./cmd/aicat

cli:
	@mkdir -p bin
	CGO_ENABLED=0 $(GO) build -tags nogui -trimpath -ldflags "$(LDFLAGS)" -o bin/aicat-cli ./cmd/aicat

# Manual embed is supported by MyGo. Local bundles remain unsigned;
# its CLI packager would add signing and distribution artifacts.
app: build
	@mkdir -p "build/desktop/aicat.app/Contents/MacOS" "build/desktop/aicat.app/Contents/Resources"
	cp bin/aicat "build/desktop/aicat.app/Contents/MacOS/aicat.next"
	mv -f "build/desktop/aicat.app/Contents/MacOS/aicat.next" "build/desktop/aicat.app/Contents/MacOS/aicat"
	cp build/darwin/aicat.icns "build/desktop/aicat.app/Contents/Resources/aicat.icns"
	sed 's/@VERSION@/0.1.0/g' build/darwin/Info.plist > "build/desktop/aicat.app/Contents/Info.plist"

run: frontend
	$(GO) run ./cmd/aicat

vet: bindings
	$(GO) vet ./...

fmt-check:
	@test -z "$$(gofmt -l $$(rg --files cmd internal ui assets -g '*.go' -g '!node_modules'))" || { echo 'Run gofmt on Go sources'; exit 1; }

frontend-check: ui/node_modules/.aicat-install bindings
	npm --prefix ui run check
	npm --prefix ui run format:check
	node --check e2e/desktop.cjs
	node --check e2e/design.cjs
	node --check e2e/icons.cjs

verify: frontend-check fmt-check vet build cli app
	$(GO) test ./...
	bin/aicat-cli --version
	bin/aicat-cli serve --help
	python3 e2e/build.py

e2e-build: frontend-e2e
	@mkdir -p bin
	$(GO) build -race -tags e2e -trimpath -ldflags "$(DESKTOP_LDFLAGS)" -o bin/aicat-e2e ./cmd/aicat
	$(GO) build -race -tags nogui,e2e -trimpath -ldflags "$(LDFLAGS)" -o bin/aicat-rules-e2e ./cmd/aicat

e2e: e2e-build
	python3 e2e/prototype.py --binary bin/aicat-e2e

clean:
	rm -rf bin .mygo ui/dist ui/dist-e2e
