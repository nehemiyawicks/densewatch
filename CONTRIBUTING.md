# Contributing

Thanks for looking at densewatch - it's early, and contributions are very welcome.

## Develop

```sh
make test      # unit tests
make sim       # run the zero-hardware simulator
make exporter  # run densewatch-cdu against the simulator
make demo      # full stack (Docker): sim → exporter → VictoriaMetrics → Grafana
```

Pure Go, no dependencies. Go 1.25+.

## Good places to start

- **Run the conformance probe against a real CDU** - the highest-leverage thing you
  can do, one line and read-only:
  ```sh
  go run github.com/nehemiyawicks/densewatch/exporters/cdu@latest probe https://YOUR-CDU/redfish/v1
  ```
  Then [open a CDU conformance report](https://github.com/nehemiyawicks/densewatch/issues/new?template=cdu_conformance.yml) and paste the output. Real-hardware coverage data is gold.
- **Add a CDU vendor profile** - a register map for a CDU that speaks Modbus/SNMP
  (see `exporters/cdu/modbus.go`). Broad heterogeneous-CDU coverage is the whole point.
- **An SNMP / BACnet adapter** behind the same unified metric schema.

## Conventions

- Keep the exporter dependency-free where reasonable; single static binary.
- **Read-only telemetry only** for now (no control / actuation) - it keeps the trust
  bar low for operators adopting it.
- Run `make fmt vet test` before opening a PR.

By contributing you agree that your contributions are licensed under Apache-2.0.
