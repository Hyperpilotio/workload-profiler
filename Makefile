GLIDE=$(which glide)
GO_EXECUTABLE ?= go
# For windows developer, use $(go list ./... | grep -v /vendor/)
PACKAGES=$(glide novendor)

init:
	glide install

test:
	${GO_EXECUTABLE} test ${PACKAGES}

build:
	CGO_ENABLED=0 go build -a -installsuffix cgo

build-docker:
	sudo docker build . -t hyperpilot/workload-profiler

push:
	sudo docker push hyperpilot/workload-profiler:latest

run:
	./workload-profiler --config ./documents/deployed.config -logtostderr=true -v=2

dev-test: build
	run
