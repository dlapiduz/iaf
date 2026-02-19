# IAF - Intelligent Application Fabric

CONTROLLER_GEN ?= go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.17.2
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

.PHONY: all
all: build

##@ Build

.PHONY: build
build: build-apiserver build-mcpserver build-controller

.PHONY: build-apiserver
build-apiserver:
	go build -o bin/apiserver ./cmd/apiserver

.PHONY: build-mcpserver
build-mcpserver:
	go build -o bin/mcpserver ./cmd/mcpserver

.PHONY: build-controller
build-controller:
	go build -o bin/controller ./cmd/controller

##@ Run

.PHONY: run-apiserver
run-apiserver:
	go run ./cmd/apiserver

.PHONY: run-mcpserver
run-mcpserver:
	go run ./cmd/mcpserver

.PHONY: run-controller
run-controller:
	go run ./cmd/controller

##@ Generate

.PHONY: generate
generate: manifests generate-deepcopy

.PHONY: manifests
manifests:
	$(CONTROLLER_GEN) crd paths="./api/..." output:crd:artifacts:config=config/crd/bases
	$(CONTROLLER_GEN) rbac:roleName=iaf-controller-role paths="./internal/controller/..." output:rbac:artifacts:config=config/rbac

.PHONY: generate-deepcopy
generate-deepcopy:
	$(CONTROLLER_GEN) object paths="./api/..."

##@ Install

.PHONY: install-crds
install-crds: manifests
	kubectl apply -f config/crd/bases/

.PHONY: uninstall-crds
uninstall-crds:
	kubectl delete -f config/crd/bases/

##@ Setup

.PHONY: setup-local
setup-local:
	bash scripts/setup-local.sh

.PHONY: setup-kpack
setup-kpack:
	bash scripts/setup-kpack.sh

##@ Deploy

.PHONY: deploy-local
deploy-local: manifests
	nerdctl build --namespace k8s.io -t iaf-platform:latest .
	kubectl apply -f config/crd/bases/
	kubectl apply -f config/rbac/
	kubectl apply -f config/deploy/platform.yaml
	kubectl rollout restart deployment/iaf-controller -n iaf-system
	kubectl rollout restart deployment/iaf-apiserver -n iaf-system

##@ Misc

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: clean
clean:
	rm -rf bin/

.PHONY: test
test:
	go test ./... -v
