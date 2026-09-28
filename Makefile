ARCH?=amd64
EXECUTABLES = go
EXEC_CHECK := $(foreach exec,$(EXECUTABLES), \
	$(if $(shell which $(exec)),some string,$(error "No $(exec) in PATH.")))

GOLANG_VERSION?=1.25
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

# Specify the repository you would like to send the single-architecture image to after building
container-build-single-repository-podman:
	@read -p "Enter the repository name you want to send this image to: " REPOSITORY; \
	podman buildx build --platform $(PLATFORM) \
	--build-arg golang_version=$(GOLANG_VERSION) \
	--build-arg package=$(PKG) \
	--build-arg application=$(APPLICATION) \
	-t $$REPOSITORY/metrics-agent:$(VERSION) -f deploy/docker/Dockerfile .; \
	podman image push $$REPOSITORY/metrics-agent:$(VERSION)

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

install-tools: download-deps install-hooks
	@echo Installing tools from tools/tools.go
	@cat ./tools/tools.go | grep _ | awk -F'"' '{print $$2}' | xargs -tI % go install %

# Install the pre-commit framework and register the Git hooks defined in .pre-commit-config.yaml.
# Run once after cloning: make install-hooks
install-hooks:
	@which pre-commit > /dev/null 2>&1 || pip3 install pre-commit
	pre-commit install

# Propagate RELEASE-VERSION from version/version.go into Chart.yaml and values.yaml.
# Before propagating, compare the local version against the latest published GitHub release.
# If local <= published, bump version/version.go to published + 1 (patch).
# Called automatically by the pre-commit hook; can also be run manually.
bump-release-version:
	@LOCAL_VER="$(RELEASE-VERSION)"; \
	echo "Local version:    $$LOCAL_VER"; \
	PUBLISHED_TAG=$$(gh release view --repo cloudability/metrics-agent --json tagName --jq '.tagName' 2>/dev/null || true); \
	PUBLISHED_VER=$$(echo "$$PUBLISHED_TAG" | sed -E 's/^[^0-9]*//'); \
	if [ -z "$$PUBLISHED_VER" ]; then \
		echo "Warning: could not retrieve latest published release; keeping local version $$LOCAL_VER"; \
	else \
		echo "Published version: $$PUBLISHED_VER"; \
		NEED_BUMP=$$(awk -v local="$$LOCAL_VER" -v pub="$$PUBLISHED_VER" 'BEGIN { \
			n = split(local, la, "."); split(pub, pa, "."); \
			for (i = 1; i <= n; i++) { \
				if (la[i]+0 > pa[i]+0) { print 0; exit } \
				if (la[i]+0 < pa[i]+0) { print 1; exit } \
			} \
			print 1 \
		}'); \
		if [ "$$NEED_BUMP" = "1" ]; then \
			PATCH=$$(echo "$$PUBLISHED_VER" | awk -F. '{print $$3+1}'); \
			MAJOR_MINOR=$$(echo "$$PUBLISHED_VER" | awk -F. '{print $$1"."$$2}'); \
			NEW_VER="$$MAJOR_MINOR.$$PATCH"; \
			echo "Local version $$LOCAL_VER <= published $$PUBLISHED_VER — bumping version/version.go to $$NEW_VER"; \
			sed -i.bak -E "s/^var[[:space:]]VERSION[[:space:]]=[[:space:]]\"[^\"]+\"/var VERSION = \"$$NEW_VER\"/" version/version.go; \
			rm -f version/version.go.bak; \
			LOCAL_VER="$$NEW_VER"; \
		else \
			echo "Local version $$LOCAL_VER > published $$PUBLISHED_VER — no version bump needed"; \
		fi; \
	fi; \
	echo "Bumping chart files to match release version $$LOCAL_VER"; \
	sed -i.bak -E "s/^(version:[[:space:]]+).*/\1$$LOCAL_VER/"         charts/metrics-agent/Chart.yaml; \
	sed -i.bak -E "s/^(appVersion:[[:space:]]+).*/\1$$LOCAL_VER/"      charts/metrics-agent/Chart.yaml; \
	sed -i.bak -E "s/^([[:space:]]+tag:[[:space:]]+).*/\1$$LOCAL_VER/" charts/metrics-agent/values.yaml; \
	rm -f charts/metrics-agent/Chart.yaml.bak charts/metrics-agent/values.yaml.bak; \
	echo "Done. Files updated:"; \
	grep -E '^(version|appVersion):' charts/metrics-agent/Chart.yaml; \
	grep 'tag:' charts/metrics-agent/values.yaml

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

test-e2e-1.35: container-build-single-platform install-tools
	$(call TEST_KUBERNETES,v1.35.0,$(PREFIX),$(VERSION)-$(PLATFORM_TAG))

test-e2e-1.34: container-build-single-platform install-tools
	$(call TEST_KUBERNETES,v1.34.0,$(PREFIX),$(VERSION)-$(PLATFORM_TAG))

test-e2e-1.33: container-build-single-platform install-tools
	$(call TEST_KUBERNETES,v1.33.2,$(PREFIX),$(VERSION)-$(PLATFORM_TAG))

test-e2e-1.32: container-build-single-platform install-tools
	$(call TEST_KUBERNETES,v1.32.0,$(PREFIX),$(VERSION)-$(PLATFORM_TAG))

# E2E test the latest 4 versions (can remove the older tests)
test-e2e-all: test-e2e-1.35 test-e2e-1.34 test-e2e-1.33 test-e2e-1.32

.PHONY: test version bump-version install-hooks
