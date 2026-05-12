BINARY     := titlis-operator
IMAGE      ?= kailima/titlis-operator-go
TAG        ?= latest
BUILD_FLAGS := -buildvcs=false -ldflags="-s -w"

.PHONY: build build-all test test-unit test-integration lint fmt vet tidy generate docker-build docker-push clean

build:
	cd src && go build $(BUILD_FLAGS) -o ../bin/$(BINARY) ./cmd/operator

build-all:
	cd src && \
	  go build $(BUILD_FLAGS) -o ../bin/titlis-operator      ./cmd/operator && \
	  go build $(BUILD_FLAGS) -o ../bin/castai-monitor       ./cmd/castai-monitor && \
	  go build $(BUILD_FLAGS) -o ../bin/synthetic-monitor    ./cmd/synthetic-monitor

test: test-unit

test-unit:
	cd src && go test -buildvcs=false ./tests/unit/... ./internal/k8s/... -v -count=1

test-coverage:
	cd src && go test -buildvcs=false ./tests/unit/... ./internal/k8s/... \
	  -coverprofile=../coverage.out -covermode=atomic
	go tool cover -html=coverage.out -o coverage.html

test-integration:
	KUBEBUILDER_ASSETS="$$(setup-envtest use -p path)" \
	  cd src && go test -buildvcs=false -tags integration ./tests/integration/... -v

lint: vet
	@which staticcheck > /dev/null 2>&1 && (cd src && staticcheck ./...) || echo "staticcheck not installed, skipping"

fmt:
	cd src && gofmt -w -s .

vet:
	cd src && go vet -buildvcs=false ./...

tidy:
	cd src && go mod tidy

generate:
	cd src && controller-gen object paths="./api/..."
	cd src && controller-gen crd paths="./api/..." output:crd:artifacts:config=../charts/titlis-operator/crds
	cd src && controller-gen rbac:roleName=titlis-operator paths="./internal/controller/..."

docker-build:
	docker build -t $(IMAGE):$(TAG) .

docker-push:
	docker push $(IMAGE):$(TAG)

clean:
	rm -rf bin/ coverage.out coverage.html
