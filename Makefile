.PHONY: dev gateway cli-test gateway-test contract-test test

dev:
	@echo "Start gateway: make gateway"

gateway:
	cd apps/gateway && go run ./cmd/gateway

cli-test:
	cd apps/cli && pnpm test

gateway-test:
	cd apps/gateway && go test ./...

contract-test:
	cd tests/contract && pnpm test

test: gateway-test cli-test contract-test
