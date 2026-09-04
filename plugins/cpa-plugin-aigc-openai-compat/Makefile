.PHONY: all build test deploy clean

VERSION ?= $(shell date "+v%Y.%-m.%-d (build %H:%M:%S)")
# 目标操作系统与架构检测
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

ifeq ($(GOOS),darwin)
    EXT := .dylib
else ifeq ($(GOOS),windows)
    EXT := .dll
else
    EXT := .so
endif

PLUGIN_BASE := aigc-openai-compat
PLUGIN_NAME := $(PLUGIN_BASE)$(EXT)
HEADER_NAME := $(PLUGIN_BASE).h

LOCAL_DIR   ?= ../../bin/plugins/$(GOOS)/$(GOARCH)
REMOTE_USER ?= root
REMOTE_HOST ?= 192.168.18.249
REMOTE_DIR  ?= /data/compose/cliproxyapi/cpa/plugins/linux/amd64
TARGET_NAME := $(PLUGIN_NAME)

LDFLAGS := -X 'main.pluginVersion=$(VERSION)'

all: build

build:
	@echo "==> Building $(PLUGIN_NAME) with version: $(VERSION)"
	CGO_ENABLED=1 go build -buildmode=c-shared -ldflags "$(LDFLAGS)" -o $(PLUGIN_NAME) .
	@echo "==> Copying $(PLUGIN_NAME) to $(LOCAL_DIR)/$(TARGET_NAME)..."
	@mkdir -p $(LOCAL_DIR)
	@cp -fv $(PLUGIN_NAME) $(LOCAL_DIR)/$(TARGET_NAME)
	@echo "==> Build complete: $(PLUGIN_NAME)"

test:
	@echo "==> Running tests..."
	go test -v ./...

deploy: build
	@echo "==> Deploying $(PLUGIN_NAME) to $(REMOTE_USER)@$(REMOTE_HOST):$(REMOTE_DIR)/$(TARGET_NAME)..."
	scp $(PLUGIN_NAME) $(REMOTE_USER)@$(REMOTE_HOST):$(REMOTE_DIR)/$(TARGET_NAME).tmp
	ssh $(REMOTE_USER)@$(REMOTE_HOST) "mv -f $(REMOTE_DIR)/$(TARGET_NAME).tmp $(REMOTE_DIR)/$(TARGET_NAME)"
	@echo "==> Deployment complete."

clean:
	@echo "==> Cleaning build artifacts..."
	rm -f $(PLUGIN_BASE).so $(PLUGIN_BASE).dylib $(PLUGIN_BASE).dll $(PLUGIN_BASE).h
