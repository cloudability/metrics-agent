ARG golang_version

FROM golang:${golang_version} as build-env
ARG package
ARG application


WORKDIR /go/src/${package}/

# Build source code
ENV CGO_ENABLED=0
ENV GOOS=linux
COPY . /go/src/${package}
RUN go build

FROM alpine:3
ARG package
ARG application


# Allow delve to run on Alpine based containers.
RUN apk --update upgrade && apk add ca-certificates && apk add curl

# Remove unnecessary netcat tool
RUN apk del netcat-openbsd && apk del netcat-openbsd-doc && rm /var/cache/apk/* && rm /usr/bin/nc

RUN addgroup -g 10000 agent && \
    adduser agent -S -u 10000 -s /bin/nologin -g metrics-agent -H -G agent

WORKDIR /

COPY --from=build-env /go/src/${package}/${application} /${application}

USER 10000

ENTRYPOINT ["/metrics-agent"]
