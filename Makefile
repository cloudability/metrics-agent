ARCH?=amd64
EXECUTABLES = go dep
EXEC_CHECK := $(foreach exec,$(EXECUTABLES), \
	$(if $(shell which $(exec)),some string,$(error "No $(exec) in PATH.")))

GOLANG_VERSION?=1.10
REPO_DIR:=$(shell pwd)
PREFIX=cloudability
CLDY_API_KEY=${CLOUDABILITY_API_KEY}

ifndef TEMP_DIR
TEMP_DIR:=$(shell mktemp -d /tmp/metrics-agent.XXXXXX)
endif

# This repo's root import path (under GOPATH).
PKG := github.com/cloudability/metrics-agent

# This version-strategy uses git tags to set the version string
VERSION := $(shell git describe --tags --always --dirty)

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

circleci-container:
	go get -u github.com/golang/dep/cmd/dep && dep ensure -v
	CGO_ENABLED=0 GOOS=linux go build -o metrics-agent main.go
	cp deploy/docker/Dockerfile ./metrics-agent $(TEMP_DIR)
	docker build -t $(PREFIX)/metrics-agent:$(VERSION) $(TEMP_DIR)

circleci-push:
	docker push $(PREFIX)/metrics-agent:$(VERSION)

container-local:
	docker run --rm -it $(TTY) -v $(TEMP_DIR):/build -v $(REPO_DIR):/go/src/$(PKG) -v ~/.ssh/id_rsa:/root/.ssh/id_rsa -v ~/.ssh/known_hosts:/root/.ssh/known_hosts -w /go/src/$(PKG) golang:$(GOLANG_VERSION) /bin/bash -c "\
		GOOS=linux CGO_ENABLED=0 go build -o /build/metrics-agent /go/src/$(PKG)/main.go"

	cp deploy/docker/Dockerfile $(TEMP_DIR)
	docker build --pull -t $(PREFIX)/metrics-agent:$(VERSION) $(TEMP_DIR)
	rm -rf $(TEMP_DIR)

deploy-local: container-local
	kubectl config use-context docker-for-desktop
	cat ./deploy/kubernetes/cloudability-metrics-agent.yaml | \
	sed "s/latest/$(VERSION)/g; s/XXXXXXXXX/$(CLDY_API_KEY)/g; s/Always/Never/g; s/NNNNNNNNN/local-dev-$(shell hostname)/g" \
	./deploy/kubernetes/cloudability-metrics-agent.yaml |kubectl apply -f - 


dockerhub-push:
	docker tag $(PREFIX)/metrics-agent:$(VERSION) cloudability/metrics-agent:latest
	docker tag $(PREFIX)/metrics-agent:$(VERSION) cloudability/metrics-agent:$(VERSION)
	docker push cloudability/metrics-agent:latest
	docker push cloudability/metrics-agent:$(VERSION)
	
fmt:
	goreturns -w .

lint:
	CGO_ENABLED=0 gometalinter.v2 --config gometalinter.json ./...

install:
	go install ./...

test:
	go test ./...

check: fmt lint test

version:
	@echo $(VERSION)

.PHONY: test version