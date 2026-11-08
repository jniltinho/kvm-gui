BIN     := dist/kvm-gui
VERSION ?= $(shell git describe --tags --match 'v[0-9]*' --dirty 2>/dev/null || echo v0.0.0-dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

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

tidy: image
	$(RUN) go mod tidy

# OpenCore's validator for the config.plist tests; downloaded once, never committed.
internal/core/testdata/ocvalidate: | image
	$(RUN) scripts/fetch-ocvalidate.sh

# MKG_REQUIRE_TOOLS=1: a test that needs qemu-img or ocvalidate fails instead of skipping.
test: image internal/core/testdata/ocvalidate
	$(RUN) sh -c 'test -z "$$(gofmt -l .)" || { gofmt -l .; echo "run gofmt -w ."; exit 1; }; \
	  go vet ./... && MKG_REQUIRE_TOOLS=1 go test -race ./...'

openspec: image
	$(RUN) openspec validate --all --strict

build: image
	mkdir -p dist
	$(RUN) env CGO_ENABLED=0 go build -o $(BIN) .

# Stripped, reproducible-path binary with the version baked in (used by releases).
build-prod: image
	mkdir -p dist
	$(RUN) env CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) .

# Real QEMU/KVM boot of the embedded OpenCore in a temporary workspace (needs /dev/kvm).
test-integration: image
	$(RUN_KVM) go test -race -tags integration -run Real ./internal/core
RUN_KVM = $(subst --rm,--rm --device /dev/kvm --group-add $(shell stat -c %g /dev/kvm),$(RUN))

# The .deb and .rpm of VERSION.
package: build-prod
	$(RUN) scripts/package.sh $(VERSION) $(BIN)

# dist/: every artifact this milestone has, with SHA256SUMS. Run after each milestone.
dist: package
	cd dist && rm -f SHA256SUMS && sha256sum kvm-gui_*.deb kvm-gui-*.rpm > SHA256SUMS && sha256sum -c SHA256SUMS

clean:
	rm -rf dist

.PHONY: image tidy test openspec build build-prod test-integration package dist clean
