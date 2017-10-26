FROM alpine:3.4

RUN mkdir /lib64 && ln -s /lib/libc.musl-x86_64.so.1 /lib64/ld-linux-x86-64.so.2
RUN apk --update upgrade && \
    apk add curl ca-certificates jq && \
    update-ca-certificates && \
    rm -rf /var/cache/apk/*

ENV GOPATH /opt/workload-profiler

COPY workload-profiler /opt/workload-profiler/workload-profiler
COPY hyperpilot_influx.sh /opt/workload-profiler/hyperpilot_influx.sh
COPY ./documents/deployed.config /etc/workload-profiler/config.json
COPY ./ui/ /opt/workload-profiler/src/github.com/hyperpilotio/workload-profiler/ui/

CMD ["/opt/workload-profiler/workload-profiler", "-v", "1", "-logtostderr"]