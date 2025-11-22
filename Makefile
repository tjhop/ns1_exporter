GOCMD := go
GOFMT := ${GOCMD} fmt
GOMOD := ${GOCMD} mod
RELEASE_CONTAINER_NAME := "ns1_exporter"
GOLANGCILINT_CACHE := ${CURDIR}/.golangci-lint/build/cache

.PHONY: help
help: ## print this help message
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} /^[a-z0-9A-Z_-]+:.*?##/ { printf "  \033[36m%-30s\033[0m%s\n", $$1, $$2 }' $(MAKEFILE_LIST)

tidy: ## tidy modules
	${GOMOD} tidy

fmt: ## apply go code style formatter
	${GOFMT} -x ./...

lint: ## run linters
	golangci-lint run -v
	nilaway ./...

binary: fmt tidy lint ## build a binary
	goreleaser build --clean --single-target --snapshot --output .

build: binary ## alias for `binary`

test: fmt tidy lint ## run tests
	go test -race -v ./...

container: binary ## build container image with binary
	podman image build -t "${RELEASE_CONTAINER_NAME}:latest" .

image: container ## alias for `container`

podman: container ## alias for `container`

docker: container ## alias for `container`
