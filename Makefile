ARCH?=amd64
EXECUTABLES = go
EXEC_CHECK := $(foreach exec,$(EXECUTABLES), \
	$(if $(shell which $(exec)),some string,$(error "No $(exec) in PATH.")))

GOLANG_VERSION?=1.16
REPO_DIR:=$(shell pwd)
PREFIX=cloudability
CLDY_API_KEY=${CLOUDABILITY_API_KEY}


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

# Build a container image and push to DockerHub
container-build:
	docker buildx build --platform linux/arm/v7,linux/arm64/v8,linux/amd64 \
	--build-arg golang_version=$(GOLANG_VERSION) \
	--build-arg package=$(PKG) \
	--build-arg application=$(APPLICATION) \
	-t $(PREFIX)/metrics-agent:$(VERSION) -f deploy/docker/Dockerfile .

# Build a local container image with the linux AMD architecture
container-build-amd:
	docker build --platform linux/amd64 \
	--build-arg golang_version=$(GOLANG_VERSION) \
	--build-arg package=$(PKG) \
	--build-arg application=$(APPLICATION) \
	-t $(PREFIX)/metrics-agent:$(VERSION)-amd64 -f deploy/docker/Dockerfile .

# Build a local container image with the specified architecture (can only build a single architecture image)
container-build-arm64:
	docker build --platform linux/arm64/v8 \
	--build-arg golang_version=$(GOLANG_VERSION) \
	--build-arg package=$(PKG) \
	--build-arg application=$(APPLICATION) \
	-t $(PREFIX)/metrics-agent:$(VERSION)-arm64 -f deploy/docker/Dockerfile .

# Build a local container image with the specified architecture (can only build a single architecture image)
container-build-arm:
	docker build --platform linux/arm/v7 \
	--build-arg golang_version=$(GOLANG_VERSION) \
	--build-arg package=$(PKG) \
	--build-arg application=$(APPLICATION) \
	-t $(PREFIX)/metrics-agent:$(VERSION)-arm -f deploy/docker/Dockerfile .

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

deploy-local: container-build
	kubectl config use-context docker-desktop
	cat ./deploy/kubernetes/cloudability-metrics-agent.yaml | \
	sed "s/latest/$(VERSION)/g; s/XXXXXXXXX/$(CLDY_API_KEY)/g; s/Always/Never/g; s/NNNNNNNNN/local-dev-$(shell hostname)/g" \
	./deploy/kubernetes/cloudability-metrics-agent.yaml |kubectl apply -f -

dockerhub-push:
	docker push cloudability/metrics-agent:latest
	docker push cloudability/metrics-agent:$(RELEASE-VERSION)

dockerhub-push-beta:
	docker push cloudability/metrics-agent:beta-latest
	docker push cloudability/metrics-agent:$(RELEASE-VERSION)-beta

docker-tag:
	docker tag $(PREFIX)/metrics-agent:$(VERSION) cloudability/metrics-agent:latest
	docker tag $(PREFIX)/metrics-agent:$(VERSION) cloudability/metrics-agent:$(RELEASE-VERSION)

docker-tag-beta:
	docker tag $(PREFIX)/metrics-agent:$(VERSION) cloudability/metrics-agent:beta-latest
	docker tag $(PREFIX)/metrics-agent:$(VERSION) cloudability/metrics-agent:$(RELEASE-VERSION)-beta

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

test-e2e-1.22-amd: container-build-amd install-tools
	$(call TEST_KUBERNETES,v1.22.0,$(PREFIX),$(VERSION))

test-e2e-1.21.1-amd: container-build-amd install-tools
	$(call TEST_KUBERNETES,v1.21.1,$(PREFIX),$(VERSION))

test-e2e-1.20-amd: container-build-amd install-tools
	$(call TEST_KUBERNETES,v1.20.0,$(PREFIX),$(VERSION))

test-e2e-1.19-amd: container-build-amd install-tools
	$(call TEST_KUBERNETES,v1.19.0,$(PREFIX),$(VERSION))

test-e2e-1.18-amd: container-build-amd install-tools
	$(call TEST_KUBERNETES,v1.18.0,$(PREFIX),$(VERSION))

test-e2e-all-amd: test-e2e-1.22-amd test-e2e-1.21.1-amd test-e2e-1.20-amd test-e2e-1.19-amd test-e2e-1.18-amd

test-e2e-1.22-arm64: container-build-arm64 install-tools
	$(call TEST_KUBERNETES,v1.22.0,$(PREFIX),$(VERSION))

test-e2e-1.21.1-arm64: container-build-arm64 install-tools
	$(call TEST_KUBERNETES,v1.21.1,$(PREFIX),$(VERSION))

test-e2e-1.20-arm64: container-build-arm64 install-tools
	$(call TEST_KUBERNETES,v1.20.0,$(PREFIX),$(VERSION))

test-e2e-1.19-arm64: container-build-arm64 install-tools
	$(call TEST_KUBERNETES,v1.19.0,$(PREFIX),$(VERSION))

test-e2e-1.18-arm64: container-build-arm64 install-tools
	$(call TEST_KUBERNETES,v1.18.0,$(PREFIX),$(VERSION))

test-e2e-all-arm64: test-e2e-1.22-arm64 test-e2e-1.21.1-arm64 test-e2e-1.20-arm64 test-e2e-1.19-arm64 test-e2e-1.18-arm64

test-e2e-1.22-arm: container-build-arm install-tools
	$(call TEST_KUBERNETES,v1.22.0,$(PREFIX),$(VERSION))

test-e2e-1.21.1-arm: container-build-arm install-tools
	$(call TEST_KUBERNETES,v1.21.1,$(PREFIX),$(VERSION))

test-e2e-1.20-arm: container-build-arm install-tools
	$(call TEST_KUBERNETES,v1.20.0,$(PREFIX),$(VERSION))

test-e2e-1.19-arm: container-build-arm install-tools
	$(call TEST_KUBERNETES,v1.19.0,$(PREFIX),$(VERSION))

test-e2e-1.18-arm: container-build-arm install-tools
	$(call TEST_KUBERNETES,v1.18.0,$(PREFIX),$(VERSION))

test-e2e-all-arm: test-e2e-1.22-arm test-e2e-1.21.1-arm test-e2e-1.20-arm test-e2e-1.19-arm test-e2e-1.18-arm

.PHONY: test version
