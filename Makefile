GLIDE=$(which glide)
GO_EXECUTABLE ?= go
# For windows developer, use $(go list ./... | grep -v /vendor/)
PACKAGES=$(glide novendor)

init:
	glide install
	 rm -rf "vendor/k8s.io/client-go/vendor/github.com/golang/glog"

test:
	${GO_EXECUTABLE} test ${PACKAGES}

build:
	go build .

build-docker: build
	docker build . -t hyperpilot/workload-profiler:william

push:
	docker push hyperpilot/workload-profiler:william

run:
	./workload-profiler --config ./documents/deployed.config -logtostderr=true -v=2

dev-test: build
	run
