# DezKVM daemon (dezkvmd) build file
#
#   make                 Build for this machine          -> build/dezkvmd
#   make release         Build every supported platform  -> build/dezkvmd-linux-<arch>
#   make release-arm64   Build a single platform (amd64 | arm64 | arm | 386)
#   make clean           Remove build output
#
# dezkvmd depends on cgo (go4vl wraps linux/videodev2.h), so building for a
# different CPU than the host needs a cross C compiler. Run
# "./install_dependencies.sh --cross" on the build machine to install them.
#
# NOTE: "make release" compiles four binaries and is heavy. Run it on a
# desktop/CI machine, not on a 1GB board such as the NanoPi Zero 2.

BINARY     := dezkvmd
BUILD_DIR  := build
GO         ?= go
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS    := -s -w
# Limit parallel compile jobs on low-memory machines, e.g. make GO_P=2
GO_P       ?= $(shell nproc 2>/dev/null || echo 1)

# Platforms built by "make release" (all linux, matching the embedded gotty binaries)
RELEASE_ARCHES := amd64 arm64 arm 386

# GOARCH -> cross C compiler used when the host architecture differs
CC_amd64 := x86_64-linux-gnu-gcc
CC_arm64 := aarch64-linux-gnu-gcc
CC_arm   := arm-linux-gnueabihf-gcc
CC_386   := i686-linux-gnu-gcc

HOST_ARCH := $(shell $(GO) env GOARCH)

.PHONY: all build release clean help $(addprefix release-,$(RELEASE_ARCHES))

all: build

help:
	@echo "Targets:"
	@echo "  make build            Build for this machine -> $(BUILD_DIR)/$(BINARY)"
	@echo "  make release          Build all: $(RELEASE_ARCHES) (linux) -> $(BUILD_DIR)/"
	@echo "  make release-<arch>   Build one of: $(RELEASE_ARCHES)"
	@echo "  make clean            Remove $(BUILD_DIR)/"

build:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 $(GO) build -p $(GO_P) -trimpath -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) .
	@echo "Built $(BUILD_DIR)/$(BINARY) ($(VERSION))"

release: $(addprefix release-,$(RELEASE_ARCHES))
	@echo "Release binaries:"
	@ls -lh $(BUILD_DIR)/$(BINARY)-linux-*

# release-<arch>: use the native compiler when <arch> is the host, else the cross compiler
$(addprefix release-,$(RELEASE_ARCHES)): release-%:
	@mkdir -p $(BUILD_DIR)
	@cc="$(if $(filter $*,$(HOST_ARCH)),gcc,$(CC_$*))"; \
	if ! command -v $$cc >/dev/null 2>&1; then \
		echo "Missing C compiler '$$cc' for linux/$*. Run ./install_dependencies.sh --cross"; \
		exit 1; \
	fi; \
	echo "==> linux/$* (CC=$$cc)"; \
	CGO_ENABLED=1 CC=$$cc GOOS=linux GOARCH=$* $(if $(filter $*,arm),GOARM=7) \
		$(GO) build -p $(GO_P) -trimpath -ldflags "$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY)-linux-$* .

clean:
	rm -rf $(BUILD_DIR)
