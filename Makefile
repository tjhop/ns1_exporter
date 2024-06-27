GOCMD := go
GOFMT := ${GOCMD} fmt
GOMOD := ${GOCMD} mod
RELEASE_CONTAINER_NAME := "ns1_exporter"
GOLANGCILINT_CACHE := ${CURDIR}/.golangci-lint/build/cache

## help:			print this help message
.PHONY: help
help: Makefile
	# autogenerate help messages for comment lines with 2 `#`
	@sed -n 's/^##//p' $<

## tidy:			tidy modules
tidy:
	${GOMOD} tidy

## fmt:			apply go code style formatter
fmt:
	${GOFMT} -x ./...

## lint:			run linters
lint:
	mkdir -p ${GOLANGCILINT_CACHE} || true
	podman run --rm -v ${CURDIR}:/app -v ${GOLANGCILINT_CACHE}:/root/.cache -w /app docker.io/golangci/golangci-lint:latest golangci-lint run -v
	nilaway ./...

## binary:		build a binary
binary: fmt tidy lint
	goreleaser build --clean --single-target --snapshot --output .

## build:			alias for `binary`
build: binary

## container: 		build container image with binary
container: binary
	podman image build -t "${RELEASE_CONTAINER_NAME}:latest" .

## image:			alias for `container`
image: container

## podman:		alias for `container`
podman: container

## docker:		alias for `container`
docker: container
