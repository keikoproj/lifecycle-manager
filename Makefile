.PHONY: build build-alpine clean test help default

BIN_NAME=lifecycle-manager

VERSION := $(shell grep "const Version " pkg/version/version.go | sed -E 's/.*"(.+)"$$/\1/')
GIT_COMMIT=$(shell git rev-parse HEAD)
GIT_DIRTY=$(shell test -n "`git status --porcelain`" && echo "+CHANGES" || true)
BUILD_DATE=$(shell date '+%Y-%m-%d-%H:%M:%S')
IMAGE_NAME ?= keikoproj/lifecycle-manager:latest
TARGETOS ?= linux
TARGETARCH ?= amd64
LDFLAGS=-ldflags "-X github.com/keikoproj/lifecycle-manager/version.GitCommit=${GIT_COMMIT}${GIT_DIRTY} -X github.com/keikoproj/lifecycle-manager/version.BuildDate=${BUILD_DATE}"

default: test

help:
	@echo 'Management commands for lifecycle-manager:'
	@echo
	@echo 'Usage:'
	@echo '    make build           Compile the project.'
	@echo '    make get-deps        runs dep ensure, mostly used for ci.'
	@echo '    make docker         Build final docker image with just the go binary inside'
	@echo '    make tag             Tag image created by package with latest, git commit and version'
	@echo '    make test            Run tests on a compiled project.'
	@echo '    make vtest            Run tests on a compiled project.'
	@echo '    make clean           Clean the directory tree.'
	@echo

build:
	@echo "building ${BIN_NAME} ${VERSION}"
	@echo "GOPATH=${GOPATH}"
	CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build ${LDFLAGS} -o bin/${BIN_NAME} github.com/keikoproj/lifecycle-manager

get-deps:
	dep ensure

docker-build:
	@echo "building image ${BIN_NAME} ${VERSION} $(GIT_COMMIT)"
	docker build --build-arg VERSION=${VERSION} --build-arg GIT_COMMIT=$(GIT_COMMIT) -t $(IMAGE_NAME) .

docker-push:
	docker push ${IMAGE_NAME}

clean:
	@test ! -e bin/${BIN_NAME} || rm bin/${BIN_NAME}

vtest:
	go test ./... -timeout 30s -v -coverprofile ./coverage.txt
	go tool cover -html=./coverage.txt -o cover.html

test:
	go test ./... -timeout 30s -coverprofile ./coverage.txt
	go tool cover -html=./coverage.txt -o cover.html

