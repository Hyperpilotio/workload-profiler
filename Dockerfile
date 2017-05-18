FROM alpine:3.4

COPY workload-profiler /opt/workload-profiler/workload-profiler
COPY documents/deployed.config /opt/workload-profiler/deployed.config

CMD ["/opt/workload-profiler/workload-profiler", "--config", "/opt/workload-profiler/deployed.config"]