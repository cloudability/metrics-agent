ARCH?=amd64
EXECUTABLES = go
EXEC_CHECK := $(foreach exec,$(EXECUTABLES), \
	$(if $(shell which $(exec)),some string,$(error "No $(exec) in PATH.")))

GOLANG_VERSION?=1.14
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

container-build:
	docker build --build-arg golang_version=$(GOLANG_VERSION) \
	--build-arg package=$(PKG) \
	--build-arg application=$(APPLICATION) \
	-t $(PREFIX)/metrics-agent:$(VERSION) -f deploy/docker/Dockerfile .

helm-package:
	helm package deploy/charts/metrics-agent

deploy-local: container-build
	kubectl config use-context docker-for-desktop
	cat ./deploy/kubernetes/cloudability-metrics-agent.yaml | \
	sed "s/latest/$(VERSION)/g; s/XXXXXXXXX/$(CLDY_API_KEY)/g; s/Always/Never/g; s/NNNNNNNNN/local-dev-$(shell hostname)/g" \
	./deploy/kubernetes/cloudability-metrics-agent.yaml |kubectl apply -f -

dockerhub-push:
	docker push cloudability/metrics-agent:latest
	docker push cloudability/metrics-agent:$(RELEASE-VERSION)

dockerhub-push-beta:
    docker push cloudability/metrics-agent-beta:$(RELEASE-VERSION)

docker-tag:
	docker tag $(PREFIX)/metrics-agent:$(VERSION) cloudability/metrics-agent:latest
	docker tag $(PREFIX)/metrics-agent:$(VERSION) cloudability/metrics-agent:$(RELEASE-VERSION)

download-deps:
	@echo Download go.mod dependencies
	@go mod download

install-tools: download-deps
	@echo Installing tools from tools/tools.go
	@cat ./tools/tools.go | grep _ | awk -F'"' '{print $$2}' | xargs -tI % go install %

fmt:
	goreturns -w .

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


test-e2e-1.17: container-build install-tools
	$(call TEST_KUBERNETES,v1.17.0,$(PREFIX),$(VERSION))

test-e2e-1.16: container-build install-tools
	$(call TEST_KUBERNETES,v1.16.1,$(PREFIX),$(VERSION))

test-e2e-1.15: container-build install-tools
	$(call TEST_KUBERNETES,v1.15.0,$(PREFIX),$(VERSION))

test-e2e-all: test-e2e-1.17 test-e2e-1.16 test-e2e-1.15

.PHONY: test version
