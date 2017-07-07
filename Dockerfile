FROM golang:1.8.1-alpine

COPY workload-profiler /opt/workload-profiler/workload-profiler
COPY documents/deployed.config /opt/workload-profiler/deployed.config

CMD ["/opt/workload-profiler/workload-profiler", "--config", "/opt/workload-profiler/deployed.config", "-v", "1", "-logtostderr"]