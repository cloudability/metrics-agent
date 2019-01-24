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

FROM alpine:3.6
ARG package
ARG application


# Allow delve to run on Alpine based containers.
RUN apk --update upgrade && apk add ca-certificates

WORKDIR /

COPY --from=build-env /go/src/${package}/${application} /${application}

# Expland the environment variable into a startup script
RUN echo "/${application} \$@" > /run_application.sh

ENTRYPOINT ["/bin/sh", "/run_application.sh"]
