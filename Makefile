ARCH?=amd64
EXECUTABLES = go
EXEC_CHECK := $(foreach exec,$(EXECUTABLES), \
	$(if $(shell which $(exec)),some string,$(error "No $(exec) in PATH.")))

GOLANG_VERSION?=1.19
REPO_DIR:=$(shell pwd)
PREFIX=cloudability
CLDY_API_KEY=${CLOUDABILITY_API_KEY}
PLATFORM?=linux/amd64
PLATFORM_TAG?=amd64


# $(call TEST_KUBERNETES, image_tag, prefix, git_commit)
define TEST_KUBERNETES
	KUBERNETES_VERSION=$(1) IMAGE=$(2)/metrics-agent:$(3) TEMP_DIR=$(TEMP_DIR) $(REPO_DIR)/testdata/e2e/e2e.sh; \
		if [ $$? != 0 ]; then \
			exit 1; \
		fi;
endef

ifndef TEMP_DIR
TEMP_DIR:=$(shell mktemp -d /tmp/metrics-agent.XXXXXX)
endif

# This repo's root import path (under GOPATH).
PKG := github.com/cloudability/metrics-agent

# Application name
APPLICATION := metrics-agent

# This version-strategy uses git tags to set the version string
VERSION := $(shell git describe --tags --always --dirty)

RELEASE-VERSION := $(shell sed -nE 's/^var[[:space:]]VERSION[[:space:]]=[[:space:]]"([^"]+)".*/\1/p' version/version.go)

# If this session isn't interactive, then we don't want to allocate a
# TTY, which would fail, but if it is interactive, we do want to attach
# so that the user can send e.g. ^C through.
INTERACTIVE := $(shell [ -t 0 ] && echo 1 || echo 0)
TTY=
ifeq ($(INTERACTIVE), 1)
	TTY=-t
endif

default:
	@echo Specify a goal

build:
	GOARCH=$(ARCH) CGO_ENABLED=0 go build -o metrics-agent main.go

# Build a container image and push to DockerHub master with correct version tags
container-build-master:
	docker buildx build --platform linux/arm/v7,linux/arm64/v8,linux/amd64 \
	--build-arg golang_version=$(GOLANG_VERSION) \
	--build-arg package=$(PKG) \
	--build-arg application=$(APPLICATION) \
	-t $(PREFIX)/metrics-agent:$(RELEASE-VERSION) \
	-t $(PREFIX)/metrics-agent:latest -f deploy/docker/Dockerfile . --push

# Build a container image and push to DockerHub beta with correct version tags
container-build-beta:
	docker buildx build --platform linux/arm/v7,linux/arm64/v8,linux/amd64 \
	--build-arg golang_version=$(GOLANG_VERSION) \
	--build-arg package=$(PKG) \
	--build-arg application=$(APPLICATION) \
	-t $(PREFIX)/metrics-agent:$(RELEASE-VERSION)-beta \
	-t $(PREFIX)/metrics-agent:beta-latest -f deploy/docker/Dockerfile . --push

# Build a local container image with the linux AMD architecture
container-build-single-platform:
	docker build --platform $(PLATFORM) \
	--build-arg golang_version=$(GOLANG_VERSION) \
	--build-arg package=$(PKG) \
	--build-arg application=$(APPLICATION) \
	-t $(PREFIX)/metrics-agent:$(VERSION)-$(PLATFORM_TAG) -f deploy/docker/Dockerfile .

# Specify the repository you would like to send the single-architecture image to after building
container-build-single-repository:
	@read -p "Enter the repository name you want to send this image to: " REPOSITORY; \
	docker buildx build --platform $(PLATFORM) \
	--build-arg golang_version=$(GOLANG_VERSION) \
	--build-arg package=$(PKG) \
	--build-arg application=$(APPLICATION) \
	-t $$REPOSITORY/metrics-agent:$(VERSION) -f deploy/docker/Dockerfile . --push

# Specify the repository you would like to send the multi-architectural image to after building.
container-build-repository:
	@read -p "Enter the repository name you want to send this image to: " REPOSITORY; \
	docker buildx build --platform linux/arm/v7,linux/arm64/v8,linux/amd64 \
    --build-arg golang_version=$(GOLANG_VERSION) \
    --build-arg package=$(PKG) \
    --build-arg application=$(APPLICATION) \
    -t $$REPOSITORY/metrics-agent:$(VERSION) -f deploy/docker/Dockerfile . --push

helm-package:
	helm package deploy/charts/metrics-agent

deploy-local: container-build-single-platform
	kubectl config use-context docker-desktop
	cat ./deploy/kubernetes/cloudability-metrics-agent.yaml | \
	sed "s/latest/$(VERSION)/g; s/XXXXXXXXX/$(CLDY_API_KEY)/g; s/Always/Never/g; s/NNNNNNNNN/local-dev-$(shell hostname)/g" \
	./deploy/kubernetes/cloudability-metrics-agent.yaml |kubectl apply -f -

download-deps:
	@echo Download go.mod dependencies
	@go mod download

install-tools: download-deps
	@echo Installing tools from tools/tools.go
	@cat ./tools/tools.go | grep _ | awk -F'"' '{print $$2}' | xargs -tI % go install %

fmt:
	gofmt -w .

lint:
	golangci-lint run

install:
	go install ./...

test:
	go test ./...

check: fmt lint test

version:
	@echo $(VERSION)

release-version:
	@echo $(RELEASE-VERSION)

test-e2e-1.28: container-build-single-platform install-tools
	$(call TEST_KUBERNETES,v1.28.0,$(PREFIX),$(VERSION)-$(PLATFORM_TAG))

test-e2e-1.27: container-build-single-platform install-tools
	$(call TEST_KUBERNETES,v1.27.0,$(PREFIX),$(VERSION)-$(PLATFORM_TAG))

test-e2e-1.26: container-build-single-platform install-tools
	$(call TEST_KUBERNETES,v1.26.0,$(PREFIX),$(VERSION)-$(PLATFORM_TAG))

test-e2e-1.25: container-build-single-platform install-tools
	$(call TEST_KUBERNETES,v1.25.3,$(PREFIX),$(VERSION)-$(PLATFORM_TAG))

test-e2e-all: test-e2e-1.28 test-e2e-1.27 test-e2e-1.26 test-e2e-1.25

.PHONY: test version
