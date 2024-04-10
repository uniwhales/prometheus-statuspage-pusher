FROM golang:1.22-alpine3.19 as builder

WORKDIR /src
COPY . .

ENV CGO_ENABLED=0

RUN go build -o prometheus-statuspage-pusher

FROM alpine:3.19.1

COPY --from=builder /src/prometheus-statuspage-pusher /usr/bin/prometheus-statuspage-pusher
ENTRYPOINT [ "/usr/bin/prometheus-statuspage-pusher" ]
