# gPanel kalite/güvenlik kapıları. `make check` = CI'nın bloklayan kapıları (local).
# .RECIPEPREFIX ile TAB yerine '>' kullanılır (kopya/yapıştır dostu).
.RECIPEPREFIX := >
GO ?= go
BIN := $(shell $(GO) env GOPATH)/bin

.PHONY: check all fmt vet build test race vuln lint sec smoke probe tools help config-check cover secret-scan lint-go lint-fe migrate-test gate
all: check

help: ## Komutları listele
> @grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | sed 's/:.*##/\t/' | sort

check: fmt vet build race vuln ## Bloklayan kapılar (CI ile aynı)
> @echo "[check] tüm bloklayan kapılar geçti ✓"

fmt: ## gofmt kontrolü (listeler, yazmaz)
> @d=$$(gofmt -l $$(git ls-files '*.go')); if [ -n "$$d" ]; then echo "gofmt gerekli:"; echo "$$d"; exit 1; fi

vet: ## go vet
> $(GO) vet ./...

build: ## go build (tüm paketler)
> $(GO) build ./...

test: ## go test
> $(GO) test ./...

race: ## yarış dedektörü (concurrent map vb. — toolkit.go bug sınıfı)
> $(GO) test -race ./...

vuln: ## govulncheck (CVE — erişilebilir kod yolları)
> $(BIN)/govulncheck ./...

lint: ## staticcheck (uyarı; build kırmaz)
> -$(BIN)/staticcheck ./...

sec: ## gosec HIGH/HIGH (uyarı; build kırmaz)
> -$(BIN)/gosec -quiet -severity high -confidence high ./...

smoke: ## derleme + statik sanity (DB gerektirmez)
> ./scripts/smoke.sh

probe: ## CANLI panel sağlık yoklaması (deploy sonrası)
> ./scripts/probe.sh

tools: ## kalite araçlarını kur (govulncheck/staticcheck/gosec)
> $(GO) install golang.org/x/vuln/cmd/govulncheck@latest
> $(GO) install honnef.co/go/tools/cmd/staticcheck@latest
> $(GO) install github.com/securego/gosec/v2/cmd/gosec@latest

# ── Birleşik kalite kapıları (CI-gates; scripts/*.sh + diğer ajan araçları) ──
config-check: ## statik config doğrulama (YAML/JSON parse · migration sırası · go mod verify)
> ./scripts/config-check.sh

cover: ## test kapsamı — lisans-gizli hariç (COVER_MIN=NN ile eşik kapısı)
> ./scripts/cover.sh

secret-scan: ## gitleaks — sır/anahtar taraması (git geçmişi; .gitleaks.toml)
> @command -v gitleaks >/dev/null 2>&1 || { echo "🔴 gitleaks yok — kurun: https://github.com/gitleaks/gitleaks"; exit 1; }
> gitleaks git --no-banner --redact

lint-go: ## golangci-lint (lisans-gizli dizinler hariç)
> @command -v golangci-lint >/dev/null 2>&1 || { echo "🔴 golangci-lint yok — kurun: https://golangci-lint.run"; exit 1; }
> golangci-lint run --timeout=5m --exclude-dirs '(cmd/server|cmd/eklenti-kur|cmd/gosp-baslatici|cmd/gosp-paketle|internal/eklenti|internal/lisans)' ./...

lint-fe: ## frontend lint (cd frontend && npm run lint)
> @command -v npm >/dev/null 2>&1 || { echo "🔴 npm yok"; exit 1; }
> @grep -qE '"lint"[[:space:]]*:' frontend/package.json 2>/dev/null || { echo "🔴 frontend/package.json icinde 'lint' script yok — ekleyin (or. eslint)"; exit 1; }
> cd frontend && npm run lint

migrate-test: ## migrasyon uygula/geri-al testi (scripts/migrate-test.sh — migrasyon ajani uretir)
> @test -x scripts/migrate-test.sh || { echo "🔴 scripts/migrate-test.sh yok/calistirilabilir degil — migrasyon-test ajani uretir"; exit 1; }
> ./scripts/migrate-test.sh

gate: fmt vet build race vuln lint sec secret-scan config-check migrate-test cover ## TÜM kapılar (check + secret-scan·config-check·migrate-test·cover)
> @echo "[gate] ✓ tüm kapılar geçti"
