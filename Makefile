BIN     := dist/kvm-gui
VERSION ?= $(shell git describe --tags --match 'v[0-9]*' --dirty 2>/dev/null || echo v0.0.0-dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
# production: Wails never swaps the embedded page for a dev server named in the environment.
GO_TAGS := -tags production

# Everything runs in the build image (build/linux.Dockerfile): the host needs only Docker and make.
# Go's build and module caches persist in ~/.cache/kvm-gui-build between runs.
IMAGE := kvm-gui-build:24.04
CACHE := $(HOME)/.cache/kvm-gui-build
RUN   := docker run --rm --user $(shell id -u):$(shell id -g) -e HOME=/tmp \
         -e GOCACHE=/cache/go-build -e GOMODCACHE=/cache/gomod -e GOFLAGS=-buildvcs=false \
         -v $(CURDIR):/src -v $(CACHE):/cache -w /src $(IMAGE)

image:
	mkdir -p $(CACHE)
	docker build -t $(IMAGE) -f build/linux.Dockerfile build

tidy: image embed-stub
	$(RUN) go mod tidy

# The binary embeds internal/gui/dist (//go:embed): a fresh checkout has no SPA build yet, so the
# compile-only targets drop a placeholder page there (git-ignored; `make frontend` replaces it).
embed-stub:
	@test -e internal/gui/dist/index.html || { mkdir -p internal/gui/dist && echo '<!doctype html><title>kvm-gui</title>' > internal/gui/dist/index.html; }

# The Vue / Vite Plus / Tailwind page into internal/gui/dist.
frontend: image
	$(RUN) sh -c 'cd frontend && npm ci --no-audit --no-fund && npm run build'

# Frontend lint and type checks (Vite Plus).
frontend-check: image
	$(RUN) sh -c 'cd frontend && npm ci --no-audit --no-fund && npx vp check --no-fmt'

# Layering: only internal/gui imports Wails; every skin defines the same tokens.
layers: image embed-stub
	$(RUN) go test ./internal/archtest

# The GUI in a container: Xvfb, a session bus, and WebKit's own sandbox (bubblewrap), which needs
# user namespaces and an unmasked /proc inside the container. The session bus needs this user's
# passwd entry (CI runs as uid 1001, unknown to the image) and a runtime folder of its own.
RUN_GUI := $(subst --rm,--rm --init --security-opt seccomp=unconfined --security-opt apparmor=unconfined \
           --security-opt systempaths=unconfined -v /etc/passwd:/etc/passwd:ro -v /etc/group:/etc/group:ro,$(RUN))

# The Manager opens, loads the VM list and exits 0.
self-test: build
	$(RUN_GUI) sh -c 'export XDG_RUNTIME_DIR=$$(mktemp -d); Xvfb :57 -screen 0 1280x800x24 >/dev/null 2>&1 & sleep 1; \
	  DISPLAY=:57 dbus-run-session timeout 120 ./$(BIN) --self-test >/tmp/self-test.log 2>&1 || { rc=$$?; echo "self-test exit $$rc"; \
	  grep -m20 -nE "^(SIG|fatal error|panic|goroutine [0-9]+ \[running)|Gtk-|Gdk-|WebKit|EGL|MESA|bwrap|error" /tmp/self-test.log; head -30 /tmp/self-test.log; exit 1; }'

# OpenCore's validator for the config.plist tests; downloaded once, never committed.
internal/core/testdata/ocvalidate: | image
	$(RUN) scripts/fetch-ocvalidate.sh

# MKG_REQUIRE_TOOLS=1: a test that needs qemu-img or ocvalidate fails instead of skipping.
test: image embed-stub internal/core/testdata/ocvalidate
	$(RUN) sh -c 'test -z "$$(gofmt -l .)" || { gofmt -l .; echo "run gofmt -w ."; exit 1; }; \
	  go vet ./... && MKG_REQUIRE_TOOLS=1 go test -race ./...'

openspec: image
	$(RUN) openspec validate --all --strict

build: image frontend
	mkdir -p dist
	$(RUN) go build $(GO_TAGS) -o $(BIN) .

# Stripped, reproducible-path binary with the version baked in (used by releases).
build-prod: image frontend
	mkdir -p dist
	$(RUN) go build $(GO_TAGS) -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) .

# Real QEMU/KVM boot of the embedded OpenCore in a temporary workspace (needs /dev/kvm).
test-integration: image
	$(RUN_KVM) go test -race -tags integration -run Real ./internal/core
RUN_KVM = $(subst --rm,--rm --device /dev/kvm --group-add $(shell stat -c %g /dev/kvm),$(RUN))

# The .deb and .rpm of VERSION.
package: build-prod
	$(RUN) scripts/package.sh $(VERSION) $(BIN)

# The AppImage: the binary, its desktop entry, icon and notices; GTK 4 and WebKitGTK 6.0 come from
# the system. Built as .new then renamed, so a running copy is replaced too.
appimage: build-prod
	rm -f dist/kvm-gui-*-x86_64.AppImage dist/kvm-gui-*-linux-amd64
	$(RUN) sh -c 'set -e; app=/tmp/AppDir; rm -rf $$app; \
	  install -D -m 0755 $(BIN) $$app/usr/bin/kvm-gui; \
	  install -D -m 0644 LICENSE $$app/usr/share/doc/kvm-gui/LICENSE; \
	  install -D -m 0644 THIRD_PARTY_NOTICES.md $$app/usr/share/doc/kvm-gui/THIRD_PARTY_NOTICES.md; \
	  install -D -m 0644 internal/assets/logo/logo.svg $$app/usr/share/icons/hicolor/scalable/apps/kvm-gui.svg; \
	  cp internal/assets/logo/logo.svg $$app/kvm-gui.svg; \
	  install -m 0755 build/linux/AppRun $$app/AppRun; cp build/linux/kvm-gui.desktop $$app/; \
	  desktop-file-validate $$app/kvm-gui.desktop; \
	  ARCH=x86_64 appimagetool --no-appstream --runtime-file /usr/local/share/appimage-runtime-x86_64 \
	    $$app dist/kvm-gui-$(VERSION)-x86_64.AppImage.new >/dev/null; \
	  mv -f dist/kvm-gui-$(VERSION)-x86_64.AppImage.new dist/kvm-gui-$(VERSION)-x86_64.AppImage'

# dist/: every artifact, with SHA256SUMS. Run after each milestone.
dist: package appimage
	cp $(BIN) dist/kvm-gui-$(VERSION)-linux-amd64
	cd dist && rm -f SHA256SUMS && sha256sum kvm-gui_*.deb kvm-gui-*.rpm kvm-gui-*-x86_64.AppImage kvm-gui-*-linux-amd64 > SHA256SUMS && sha256sum -c SHA256SUMS

clean:
	rm -rf dist

.PHONY: self-test image tidy embed-stub frontend frontend-check layers test openspec build build-prod test-integration package appimage dist clean
