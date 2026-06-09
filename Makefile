GO ?= go

.PHONY: sim demo build test vet fmt tidy clean help

help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-8s %s\n", $$1, $$2}'

sim: ## Run the zero-hardware simulator (Redfish :5000 + dcgm :9400 + Modbus CDU :5020)
	$(GO) run ./simulator

demo: sim ## M0: demo == run the simulator (compose stack arrives in M2)

exporter: ## Run densewatch-cdu against the local simulator (run `make sim` first)
	$(GO) run ./exporters/cdu -redfish http://localhost:5000/redfish/v1/ThermalEquipment/CDUs/1 -modbus localhost:5020

build: ## Build binaries into bin/
	$(GO) build -o bin/densewatch-sim ./simulator
	$(GO) build -o bin/densewatch-cdu ./exporters/cdu

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
