GO_EXECUTABLE ?= go
# For windows developer, use $(go list ./... | grep -v /vendor/)
PACKAGES=$(glide novendor)
ORGANIZATION=hyperpilot
IMAGE=workload-profiler
TAG=latest
CLUSTER_TYPE=gcp

glide-check:
	@if [ -z `which glide` ]; then \
		echo "glide doesn't exist."; \
		curl https://glide.sh/get | sh ; \
	else \
		echo "glide installed"; \
	fi

init: glide-check
	glide install

test:
	${GO_EXECUTABLE} test ${PACKAGES}

build: 
	CGO_ENABLED=0 go build -a -installsuffix cgo

build-linux: init
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -a -installsuffix cgo

docker-build: 
	docker build --no-cache . -t ${ORGANIZATION}/${IMAGE}:${TAG}

docker-build-influx_aws: 
	docker build --no-cache . -t ${ORGANIZATION}/${IMAGE}:${CLUSTER_TYPE}_influx --file Dockerfile.influx_aws	

docker-push: 
	docker push ${ORGANIZATION}/${IMAGE}:${TAG}

docker-push-influx_aws: 
	docker push ${ORGANIZATION}/${IMAGE}:${CLUSTER_TYPE}_influx

run:
	./workload-profiler --config ./documents/deployed.config -logtostderr=true -v=2

dev-test: build
	run
