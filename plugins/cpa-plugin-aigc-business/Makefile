.PHONY: all build test deploy clean

# 获取当前时间作为版本号 (格式: vyyyy.m.d (build hh:ii:ss)，例如 v2026.8.18 (build 09:14:19))
VERSION ?= $(shell date "+v%Y.%-m.%-d (build %H:%M:%S)")
PLUGIN_NAME := aigc-business.so
HEADER_NAME := aigc-business.h

# 部署目标配置
LOCAL_DIR   ?= ../../bin/plugins/linux/amd64
REMOTE_USER ?= root
REMOTE_HOST ?= 192.168.18.249
REMOTE_DIR  ?= /data/compose/cliproxyapi/cpa/plugins/linux/amd64
TARGET_NAME := aigc-business.so

LDFLAGS := -X 'main.pluginVersion=$(VERSION)'

all: build

## 编译 C-Shared 动态库并注入构建时间版本号
build:
	@echo "==> Building $(PLUGIN_NAME) with version: $(VERSION)"
	CGO_ENABLED=1 go build -buildmode=c-shared -ldflags "$(LDFLAGS)" -o $(PLUGIN_NAME) .
	@echo "==> Copying $(PLUGIN_NAME) to $(LOCAL_DIR)/$(TARGET_NAME)..."
	@mkdir -p $(LOCAL_DIR)
	@cp -fv $(PLUGIN_NAME) $(LOCAL_DIR)/$(TARGET_NAME)
	@echo "==> Build complete: $(PLUGIN_NAME)"

## 运行单元测试
test:
	@echo "==> Running tests..."
	go test -v ./...

## 部署到远程服务器 (原子替换)
deploy: build
	@echo "==> Deploying $(PLUGIN_NAME) to $(REMOTE_USER)@$(REMOTE_HOST):$(REMOTE_DIR)/$(TARGET_NAME)..."
	scp $(PLUGIN_NAME) $(REMOTE_USER)@$(REMOTE_HOST):$(REMOTE_DIR)/$(TARGET_NAME).tmp
	ssh $(REMOTE_USER)@$(REMOTE_HOST) "mv -f $(REMOTE_DIR)/$(TARGET_NAME).tmp $(REMOTE_DIR)/$(TARGET_NAME)"
	@echo "==> Deployment complete."

## 清理编译产物
clean:
	@echo "==> Cleaning build artifacts..."
	rm -f $(PLUGIN_NAME) $(HEADER_NAME)
