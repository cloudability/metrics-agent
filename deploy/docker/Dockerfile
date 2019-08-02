ARG golang_version

FROM golang:${golang_version} as build-env
ARG package
ARG application


WORKDIR /go/src/${package}/

# Install dependencies
RUN curl https://raw.githubusercontent.com/golang/dep/master/install.sh | sh
COPY Gopkg.toml Gopkg.lock ./
RUN dep ensure --vendor-only

# Build source code
ENV CGO_ENABLED=0
ENV GOOS=linux
COPY . /go/src/${package}
RUN go build

FROM alpine:3.9
ARG package
ARG application


# Allow delve to run on Alpine based containers.
RUN apk --update upgrade && apk add ca-certificates

RUN addgroup -g 1000 agent && \
    adduser agent -S -u 1000 -s /bin/nologin -g metrics-agent -H -G agent 

WORKDIR /

COPY --from=build-env /go/src/${package}/${application} /${application}

USER agent

ENTRYPOINT ["/metrics-agent"]
