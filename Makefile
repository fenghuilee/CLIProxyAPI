REGISTRY   ?= ai360-cn-guangzhou.cr.volces.com/devops/cliproxyapi
TAG        ?= latest
VERSION    ?= $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_DATE ?= $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
IMAGE      := $(REGISTRY):$(TAG)

.PHONY: all build push release plugins-build plugins-deploy help

all: build

plugins-build:
	@for dir in $(wildcard plugins/*); do \
		if [ -f "$$dir/Makefile" ]; then \
			echo "==> Building plugin in $$dir..."; \
			$(MAKE) -C "$$dir" build || exit 1; \
		fi \
	done

plugins-deploy:
	@for dir in $(wildcard plugins/*); do \
		if [ -f "$$dir/Makefile" ]; then \
			echo "==> Deploying plugin in $$dir..."; \
			$(MAKE) -C "$$dir" deploy || exit 1; \
		fi \
	done

build:
	docker build \
		--network=host \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(IMAGE) .

push:
	docker push $(IMAGE)

release: build push

help:
	@echo "Usage:"
	@echo "  make build          - Build Docker image ($(IMAGE))"
	@echo "  make push           - Push Docker image ($(IMAGE))"
	@echo "  make release        - Build and push Docker image"
	@echo "  make plugins-build  - Build all plugins in plugins/ directory"
	@echo "  make plugins-deploy - Deploy all plugins in plugins/ directory"
	@echo ""
	@echo "Variables (override as needed):"
	@echo "  REGISTRY   (default: $(REGISTRY))"
	@echo "  TAG        (default: $(TAG))"
	@echo "  VERSION    (default: $(VERSION))"