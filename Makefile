GLIDE=$(which glide)
GO_EXECUTABLE ?= go
# For windows developer, use $(go list ./... | grep -v /vendor/)
PACKAGES=$(glide novendor)
ORGANIZATION=hyperpilot
IMAGE=workload-profiler
TAG=latest

glide-check:
	@if [ -z $GLIDE ]; then \
		echo "glide doesn't exist."; \
		curl https://glide.sh/get | sh ; \
	else \
		echo "glide installed"; \
	fi

init: glide-check
	glide install

test:
	${GO_EXECUTABLE} test ${PACKAGES}

build: init
	CGO_ENABLED=0 go build -a -installsuffix cgo

build-linux: init
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -a -installsuffix cgo

docker-build:
	docker build --no-cache . -t ${ORGANIZATION}/${IMAGE}:${TAG}

docker-push: docker-build
	docker push ${ORGANIZATION}/${IMAGE}:${TAG}

run:
	./workload-profiler --config ./documents/deployed.config -logtostderr=true -v=2

run-dev:
	./workload-profiler --config ./documents/deployed_dev.config -logtostderr=true -v=2

dev-test: build
	run
