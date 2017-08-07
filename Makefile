GLIDE=$(which glide)
GO_EXECUTABLE ?= go
# For windows developer, use $(go list ./... | grep -v /vendor/)
PACKAGES=$(glide novendor)
ORGANIZATION=hyperpilot
IMAGE=workload-profiler
TAG=latest

init:
	glide install

test:
	${GO_EXECUTABLE} test ${PACKAGES}

build:
	CGO_ENABLED=0 go build -a -installsuffix cgo

build-linux:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -a -installsuffix cgo

docker-build:
	docker build . -t ${ORGANIZATION}/${IMAGE}:${TAG}

docker-push:
	docker push ${ORGANIZATION}/${IMAGE}:${TAG}

run:
	./workload-profiler --config ./documents/deployed.config -logtostderr=true -v=2

dev-test: build
	run
