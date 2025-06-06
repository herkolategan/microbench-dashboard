# Docker image configuration
REGISTRY := us-central1-docker.pkg.dev/cockroach-testeng-infra/crl-roachperf
IMAGE_NAME := roachperf-server
GIT_SHA := $(shell git rev-parse --short HEAD)
VERSION ?= $(shell git describe --tags --always || echo "dev")

# Full image path
IMAGE := $(REGISTRY)/$(IMAGE_NAME)

# Platform configuration
PLATFORM := linux/amd64

.PHONY: all
all: build

.PHONY: build
build:
	GOOS=linux GOARCH=amd64 go build -o perf-server ./perf/

.PHONY: build-docker
build-docker:
	docker build --platform $(PLATFORM) \
		-t $(IMAGE):$(VERSION) \
		-t $(IMAGE):$(GIT_SHA) \
		-t $(IMAGE):latest .

.PHONY: push-docker
push-docker:
	docker push $(IMAGE):$(VERSION)
	docker push $(IMAGE):$(GIT_SHA)
	docker push $(IMAGE):latest

.PHONY: clean
clean:
	rm -f perf-server

.PHONY: version
version:
	@echo $(VERSION)

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build         - Build the binary locally"
	@echo "  build-docker  - Build Docker image"
	@echo "  push-docker   - Push Docker image to registry"
	@echo "  clean         - Remove built binary"
	@echo "  version       - Show current version"
	@echo ""
	@echo "To build and push a specific version:"
	@echo "  make build-docker push-docker VERSION=v1.0.0" 