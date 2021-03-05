FROM alpine

COPY prometheus-statuspage-pusher /usr/bin/
ENTRYPOINT ["/usr/bin/prometheus-statuspage-pusher"]