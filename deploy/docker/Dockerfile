FROM alpine:3.6

RUN apk --update upgrade && apk add ca-certificates

COPY ./metrics-agent /metrics-agent

ENTRYPOINT [ "/metrics-agent" ]