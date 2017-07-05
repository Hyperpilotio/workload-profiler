FROM alpine:3.4

RUN mkdir /lib64 && ln -s /lib/libc.musl-x86_64.so.1 /lib64/ld-linux-x86-64.so.2

ENV GOPATH /opt/workload-profiler

COPY workload-profiler /opt/workload-profiler/workload-profiler
COPY documents/deployed.config /opt/workload-profiler/deployed.config
COPY ./ui/ /opt/workload-profiler/src/github.com/hyperpilotio/workload-profiler/ui/

CMD ["/opt/workload-profiler/workload-profiler", "--config", "/opt/workload-profiler/deployed.config"]