SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c

APP_NAME       := config-service
CLUSTER_NAME   := config-service
IMAGE          := $(APP_NAME):latest
NAMESPACE      := config-service
TF_DIR         := infra/terraform
KUBE_CONTEXT   := kind-$(CLUSTER_NAME)

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

.PHONY: test
test: ## Run Go unit tests
	go test ./... -count=1 -race

.PHONY: lint
lint: ## Run gofmt + go vet
	test -z "$$(gofmt -l .)" || (echo "gofmt needs to be run on:" && gofmt -l . && exit 1)
	go vet ./...

.PHONY: build-image
build-image: ## Build the application container image
	docker build -t $(IMAGE) .

.PHONY: cluster-up
cluster-up: ## Create the local kind cluster (no-op if it already exists)
	@if kind get clusters | grep -qx "$(CLUSTER_NAME)"; then \
		echo "kind cluster '$(CLUSTER_NAME)' already exists"; \
	else \
		kind create cluster --name $(CLUSTER_NAME); \
	fi

.PHONY: cluster-down
cluster-down: ## Delete the local kind cluster
	kind delete cluster --name $(CLUSTER_NAME)

.PHONY: tf-init
tf-init: ## Initialise Terraform providers
	cd $(TF_DIR) && terraform init

.PHONY: tf-apply
tf-apply: tf-init ## Provision namespace, PostgreSQL and secrets via Terraform
	cd $(TF_DIR) && terraform apply -auto-approve \
		-var="kube_context=$(KUBE_CONTEXT)"

.PHONY: tf-destroy
tf-destroy: ## Tear down Terraform-managed resources
	cd $(TF_DIR) && terraform destroy -auto-approve \
		-var="kube_context=$(KUBE_CONTEXT)"

.PHONY: load-image
load-image: build-image ## Load the built image into the kind cluster
	kind load docker-image $(IMAGE) --name $(CLUSTER_NAME)

.PHONY: deploy
deploy: load-image ## Apply application manifests (configmap, deployment, service)
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/
	kubectl --context $(KUBE_CONTEXT) -n $(NAMESPACE) rollout status deploy/$(APP_NAME) --timeout=120s

.PHONY: up
up: cluster-up tf-apply deploy ## Full bootstrap: cluster + Terraform (DB) + app deployment
	@echo ""
	@echo "config-service is deployed. Run 'make smoke-test' to validate it end-to-end."

.PHONY: down
down: ## Tear down everything (Terraform resources, then the kind cluster)
	-$(MAKE) tf-destroy
	$(MAKE) cluster-down

.PHONY: port-forward
port-forward: ## Forward localhost:8080 to the in-cluster service (blocking)
	kubectl --context $(KUBE_CONTEXT) -n $(NAMESPACE) port-forward svc/$(APP_NAME) 8080:8080

.PHONY: smoke-test
smoke-test: ## Port-forward the service and run end-to-end validation
	@bash scripts/smoke-test.sh $(KUBE_CONTEXT) $(NAMESPACE) $(APP_NAME)

.PHONY: logs
logs: ## Tail application logs
	kubectl --context $(KUBE_CONTEXT) -n $(NAMESPACE) logs -l app=$(APP_NAME) -f --tail=100
