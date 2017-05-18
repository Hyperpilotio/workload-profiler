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
	sudo docker push hyperpilot/worload-profiler:latest

dev-test: build
	./workload-profiler --config ./documents/dev.config -logtostderr=true -v=2
