GO ?= go

.PHONY: sim demo build test vet fmt tidy clean help

help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-8s %s\n", $$1, $$2}'

sim: ## Run the zero-hardware simulator (Redfish CDU :5000 + dcgm metrics :9400)
	$(GO) run ./simulator

demo: sim ## M0: demo == run the simulator (compose stack arrives in M2)

build: ## Build binaries into bin/
	$(GO) build -o bin/densewatch-sim ./simulator

test: ## Run tests
	$(GO) test ./...

vet: ## go vet
	$(GO) vet ./...

fmt: ## gofmt
	$(GO) fmt ./...

tidy: ## go mod tidy
	$(GO) mod tidy

clean: ## Remove build output
	rm -rf bin
