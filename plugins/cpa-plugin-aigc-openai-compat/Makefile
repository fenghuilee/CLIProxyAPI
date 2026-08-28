.PHONY: all build test deploy clean

VERSION ?= $(shell date "+v%Y.%-m.%-d (build %H:%M:%S)")
PLUGIN_NAME := aigc-openai-compat.so
HEADER_NAME := aigc-openai-compat.h

LOCAL_DIR   ?= ../../bin/plugins/linux/amd64
REMOTE_USER ?= root
REMOTE_HOST ?= 192.168.18.249
REMOTE_DIR  ?= /data/compose/cliproxyapi/cpa/plugins/linux/amd64
TARGET_NAME := aigc-openai-compat.so

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
	rm -f $(PLUGIN_NAME) $(HEADER_NAME)
