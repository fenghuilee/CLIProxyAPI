.DEFAULT_GOAL := help

# Build metadata
VERSION    ?= $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_DATE ?= $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
LDFLAGS    := -s -w -X 'main.Version=$(VERSION)' -X 'main.Commit=$(COMMIT)' -X 'main.BuildDate=$(BUILD_DATE)'

# Docker image configuration
REGISTRY   ?= ai360-cn-guangzhou.cr.volces.com/devops/cliproxyapi
TAG        ?= latest
IMAGE      := $(REGISTRY):$(TAG)

.PHONY: help bin docker-build docker-push docker-release plugins-build plugins-deploy

# Local binary
bin:
	go build -buildvcs=false -ldflags="$(LDFLAGS)" -o ./bin/cli-proxy-api ./cmd/server

# Docker workflows
docker-build:
	docker build \
		--network=host \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(IMAGE) .

docker-push:
	docker push $(IMAGE)

docker-release: docker-build
	$(MAKE) docker-push

# Plugin delegation
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

help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "Local Targets:"
	@echo "  bin             - Build local server binary (cli-proxy-api)"
	@echo ""
	@echo "Docker Targets:"
	@echo "  docker-build    - Build Docker image ($(IMAGE))"
	@echo "  docker-push     - Push Docker image ($(IMAGE))"
	@echo "  docker-release  - Build and push Docker image sequentially"
	@echo ""
	@echo "Plugin Targets:"
	@echo "  plugins-build   - Build all plugins in plugins/ directory"
	@echo "  plugins-deploy  - Deploy all plugins in plugins/ directory"
	@echo ""
	@echo "Variables (override as needed):"
	@echo "  REGISTRY        (default: $(REGISTRY))"
	@echo "  TAG             (default: $(TAG))"
	@echo "  VERSION         (default: $(VERSION))"
	@echo "  COMMIT          (default: $(COMMIT))"
	@echo "  BUILD_DATE      (default: $(BUILD_DATE))"
