ROOT     := $(shell pwd)
GOWORK   := $(ROOT)/go.work
SERVICES := svc-aerial-iam svc-aerial-subscriber svc-aerial-esim svc-aerial-provision svc-aerial-ran-control svc-aerial-billing svc-aerial-messaging
LIB      := lib-aerial-go
K3D_NAME := aerial

# ═══════════════════════════════════════════════════════
# CONTROL-PLANE INFRASTRUCTURE (docker compose)
# ═══════════════════════════════════════════════════════

up:
	docker compose -f docker-compose.yml up -d --build

up-no-build:
	docker compose -f docker-compose.yml up -d

down:
	docker compose -f docker-compose.yml down

reset:
	docker compose -f docker-compose.yml down -v
	docker compose -f docker-compose.yml up -d --build

logs:
	docker compose -f docker-compose.yml logs -f

ps:
	docker compose -f docker-compose.yml ps

health:
	@for svc in iam:8081 subscriber:8082 esim:8083 provision:8084 ran:8085 billing:8086 messaging:8087; do \
		name=$${svc%%:*}; port=$${svc##*:}; \
		code=$$(curl -s -o /dev/null -w '%{http_code}' http://localhost:$$port/v1/health 2>/dev/null); \
		if [ "$$code" = "200" ]; then echo "OK $$name"; else echo "FAIL $$name ($$code)"; fi; \
	done

migrate:
	docker compose -f docker-compose.yml run --rm migrate

# ═══════════════════════════════════════════════════════
# RAN PLANE (k3d + helm)
# ═══════════════════════════════════════════════════════

k3d-up:
	k3d cluster create $(K3D_NAME) --config infra/k3d/cluster.yaml

k3d-down:
	k3d cluster delete $(K3D_NAME)

k3d-ctx:
	kubectl config use-context k3d-$(K3D_NAME)

# Add helm repos used by RAN plane
helm-repos:
	helm repo add gradiant https://gradiant.github.io/openverso-charts/
	helm repo add cnpg https://cloudnative-pg.github.io/charts
	helm repo add nats https://nats-io.github.io/k8s/helm/charts/
	helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
	helm repo add grafana https://grafana.github.io/helm-charts
	helm repo update

# Deploy core platform addons into k3d
core-up: helm-repos
	kubectl create namespace platform 2>/dev/null || true
	helm upgrade --install nats nats/nats -n platform -f infra/helm/nats/values.yaml
	helm upgrade --install cnpg cnpg/cloudnative-pg -n platform --create-namespace
	helm upgrade --install kube-prom prometheus-community/kube-prometheus-stack -n platform -f infra/helm/kube-prometheus-stack/values.yaml

# Deploy Open5GS + UERANSIM via towards5gs-helm style charts
ran-up:
	kubectl create namespace ran 2>/dev/null || true
	helm upgrade --install open5gs gradiant/open5gs -n ran -f infra/helm/open5gs/values.yaml
	helm upgrade --install ueransim gradiant/ueransim-gnb -n ran -f infra/helm/ueransim/gnb-values.yaml
	helm upgrade --install ueransim-ues gradiant/ueransim-ue -n ran -f infra/helm/ueransim/ue-values.yaml

ran-down:
	helm uninstall ueransim-ues -n ran || true
	helm uninstall ueransim -n ran || true
	helm uninstall open5gs -n ran || true
	kubectl delete namespace ran || true

ran-health:
	@echo "--- AMF ---"
	@kubectl -n ran logs -l app.kubernetes.io/name=open5gs-amf --tail=20 2>/dev/null | grep -E '(Registration|PDU|UE)' | tail -5 || true
	@echo "--- UE attach status ---"
	@kubectl -n ran exec deploy/ueransim-ues-ueransim-ue -- nr-cli imsi-999700000000001 -e "status" 2>/dev/null || echo "UE not ready yet"

# ═══════════════════════════════════════════════════════
# BUILD + TEST
# ═══════════════════════════════════════════════════════

build:
	@for svc in $(SERVICES); do \
		echo ">>> building $$svc"; \
		cd $(ROOT)/$$svc && GOWORK=$(GOWORK) go build ./... || exit 1; \
		cd $(ROOT); \
	done

test-unit:
	@echo "=== Go Unit Tests ==="
	@for svc in $(SERVICES) $(LIB); do \
		cd $(ROOT)/$$svc && \
		fail=$$(GOWORK=$(GOWORK) go test -race ./... 2>&1 | grep "^FAIL" || true); \
		if [ -z "$$fail" ]; then echo "OK  $$svc"; else echo "FAIL $$svc"; echo "$$fail"; fi; \
		cd $(ROOT); \
	done

test-integration:
	@echo "=== Integration Tests (testcontainers) ==="
	@for svc in $(SERVICES); do \
		cd $(ROOT)/$$svc && \
		fail=$$(GOWORK=$(GOWORK) go test -tags integration -race -count=1 ./internal/repository/... 2>&1 | grep "^FAIL" || true); \
		if [ -z "$$fail" ]; then echo "OK  $$svc"; else echo "FAIL $$svc"; echo "$$fail"; fi; \
		cd $(ROOT); \
	done

lint:
	@command -v golangci-lint >/dev/null || { echo "install golangci-lint: brew install golangci-lint"; exit 1; }
	@for svc in $(SERVICES) $(LIB); do \
		echo ">>> linting $$svc"; \
		cd $(ROOT)/$$svc && GOWORK=$(GOWORK) golangci-lint run ./... || true; \
		cd $(ROOT); \
	done

tidy:
	@for svc in $(SERVICES) $(LIB); do \
		echo ">>> go mod tidy $$svc"; \
		cd $(ROOT)/$$svc && GOWORK=off go mod tidy && cd $(ROOT); \
	done

# ═══════════════════════════════════════════════════════
# SECURITY (best-effort; tools optional)
# ═══════════════════════════════════════════════════════

security-secrets:
	@command -v gitleaks >/dev/null && gitleaks detect --source . --no-banner --no-color || echo "skip: install gitleaks"

security-deps:
	@command -v govulncheck >/dev/null || { echo "install: go install golang.org/x/vuln/cmd/govulncheck@latest"; exit 0; }
	@for svc in $(SERVICES) $(LIB); do \
		cd $(ROOT)/$$svc && \
		result=$$(GOWORK=$(GOWORK) govulncheck ./... 2>&1 | tail -1); \
		if echo "$$result" | grep -q "No vulnerabilities"; then echo "OK  $$svc"; else echo "WARN $$svc: $$result"; fi; \
		cd $(ROOT); \
	done

security-docker:
	@command -v hadolint >/dev/null || { echo "install: brew install hadolint"; exit 0; }
	@for df in infra/dockerfiles/Dockerfile.*; do \
		[ -f "$$df" ] || continue; \
		result=$$(hadolint "$$df" 2>&1); \
		name=$$(basename "$$df"); \
		if [ -z "$$result" ]; then echo "OK  $$name"; else echo "WARN $$name"; echo "$$result"; fi; \
	done

# ═══════════════════════════════════════════════════════
# QUICK COMMANDS
# ═══════════════════════════════════════════════════════

quick: test-unit
	@echo "Quick check done"

daily: up-no-build test-unit security-deps
	@echo ""
	@echo "═══════════════════════════════════════"
	@echo "  DAILY CHECK COMPLETE"
	@echo "═══════════════════════════════════════"

# ═══════════════════════════════════════════════════════
# UTILITY
# ═══════════════════════════════════════════════════════

env-check:
	@[ -f .env ] || { echo "missing .env — copy .env.example"; exit 1; }
	@echo "OK .env present"

print-services:
	@echo $(SERVICES)

.PHONY: up up-no-build down reset logs ps health migrate \
	k3d-up k3d-down k3d-ctx helm-repos core-up ran-up ran-down ran-health \
	build test-unit test-integration lint tidy \
	security-secrets security-deps security-docker \
	quick daily env-check print-services
