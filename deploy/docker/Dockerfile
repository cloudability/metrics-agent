ARG golang_version

FROM golang:${golang_version} as build-env
ARG package
ARG application


WORKDIR /go/src/${package}/

# Build source code
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ENV CGO_ENABLED=0
ENV GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH} \
    GOARM=${TARGETVARIANT}
COPY . /go/src/${package}
RUN export GOARM=$(echo "${GOARM}" | cut -c2-) && \
    go build

FROM alpine:3.13
ARG package
ARG application


# Allow delve to run on Alpine based containers.
RUN apk --update upgrade && apk add ca-certificates

RUN addgroup -g 1000 agent && \
    adduser agent -S -u 1000 -s /bin/nologin -g metrics-agent -H -G agent

WORKDIR /

COPY --from=build-env /go/src/${package}/${application} /${application}

USER 1000

ENTRYPOINT ["/metrics-agent"]
