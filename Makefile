GO ?= go
VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)
.DEFAULT_GOAL := build
.PHONY: build cli app run vet fmt-check verify e2e clean

assets/tray.png: assets/logo.svg build/icon/gen.go
	$(GO) run ./build/icon/gen.go tray assets/logo.svg assets/tray.png

build: assets/tray.png
	@mkdir -p bin
	$(GO) build -tags mygo_noinspector -trimpath -ldflags "$(LDFLAGS)" -o bin/aicat ./cmd/aicat

cli:
	@mkdir -p bin
	CGO_ENABLED=0 $(GO) build -tags nogui -trimpath -ldflags "$(LDFLAGS)" -o bin/aicat-cli ./cmd/aicat

# Unsigned local bundle; MyGo's default CLI packager adds ad-hoc signing.
# Keep local packaging unsigned, as required by the repository's build policy.
app: build
	@mkdir -p "build/native/aicat.app/Contents/MacOS" "build/native/aicat.app/Contents/Resources"
	cp bin/aicat "build/native/aicat.app/Contents/MacOS/aicat.next"
	mv -f "build/native/aicat.app/Contents/MacOS/aicat.next" "build/native/aicat.app/Contents/MacOS/aicat"
	cp build/darwin/aicat.icns "build/native/aicat.app/Contents/Resources/aicat.icns"
	sed 's/@VERSION@/0.1.0/g' build/darwin/Info.plist > "build/native/aicat.app/Contents/Info.plist"

run:
	$(GO) run ./cmd/aicat

vet:
	$(GO) vet ./...

fmt-check:
	@test -z "$$(gofmt -l $$(rg --files cmd internal ui assets -g '*.go'))" || { echo 'Run gofmt on Go sources'; exit 1; }

verify: frontend-check fmt-check vet build cli app
	$(GO) test ./...
	bin/aicat-cli --version
	bin/aicat-cli serve --help

e2e:
	@mkdir -p bin
	$(GO) build -race -tags e2e -o bin/aicat-e2e ./cmd/aicat
	$(GO) build -race -tags nogui,e2e -o bin/aicat-rules-e2e ./cmd/aicat
	python3 e2e/prototype.py --binary bin/aicat-e2e

clean:
	rm -rf bin .mygo

frontend-check:
	@for source in ui/*.js; do node --input-type=module --check < "$$source" || exit 1; done
	node --check e2e/desktop.cjs
	node --check e2e/design.cjs
