# densewatch

**Open-source observability that correlates GPU workload ↔ rack power ↔ liquid-cooling thermals for high-density AI infrastructure** — the integrated power+thermal view that today exists only in proprietary DCIM, across heterogeneous CDUs (Redfish *and* Modbus/SNMP).

> **Status: M0 — scaffold + zero-hardware simulator.** Redfish `CoolingUnit` + dcgm + Modbus-TCP CDU feeds working; SNMP PDU next. See [docs/ROADMAP.md](docs/ROADMAP.md).

## Why

`dcgm-exporter` and commercial GPU SaaS (e.g. Datadog) stop at the GPU device. The generic Redfish exporters stop at the server chassis. **Nobody open joins GPU jobs to rack power and CDU cooling.** That join — plus coverage of CDUs that speak Modbus/SNMP rather than Redfish — is densewatch.

## Quickstart (no hardware needed)

```sh
make sim
#   redfish  CoolingUnit sim  →  http://localhost:5000/redfish/v1/ThermalEquipment/CDUs/1
#   dcgm     metrics sim      →  http://localhost:9400/metrics
#   modbus   CDU sim (FC3/4)  →  modbus-tcp://localhost:5020  (13 input registers)
```

In another shell:

```sh
# live CDU coolant telemetry (DSP2064 CoolingUnit schema)
curl -s localhost:5000/redfish/v1/ThermalEquipment/CDUs/1/SecondaryCoolantConnectors/1 | python3 -m json.tool

# GPU power with the hpc_job correlation key
curl -s localhost:9400/metrics | grep DCGM_FI_DEV_POWER_USAGE | head
```

The simulator drives GPU power **and** CDU heat load from one shared workload signal, so the two telemetry streams genuinely correlate — exactly what the correlation engine (M3) will exploit. Heat balance holds: `HeatRemovedkW ≈ FlowLitersPerMinute × ΔT × 0.0698`.

## Layout

| Path | What | Milestone |
|---|---|---|
| `simulator/` | Zero-hardware feeds: Redfish CDU + dcgm + Modbus-TCP CDU (SNMP PDU next) | **M0** |
| `exporters/cdu/` | `densewatch-cdu`: Redfish CoolingUnit + Modbus/SNMP fallback + conformance probe | M1 |
| `correlation/` | GPU job → node → rack → power feed → cooling loop (NetBox topology) | M3 |
| `dashboards/` | Opinionated Grafana JSON | M3 |
| `deploy/` | docker-compose + Helm | M2 |

## How we're different

- **vs `dcgm-exporter` / Datadog GPU Monitoring** — they stop at the GPU device; densewatch adds the facility half (rack power + CDU cooling) and the join.
- **vs commercial DCIM-for-AI** (ProphetStor, Vertiv, Schneider, Sunbird, Nlyte) — closed and control-coupled; densewatch is open, read-only, operator-led.
- **vs DMTF Redfish-Tacklebox** — a CLI that reads the schema; densewatch is the productized exporter + correlation, with a Modbus/SNMP fallback for the many CDUs that don't speak Redfish.

## Develop

```sh
make test   # unit tests (telemetry physics + job mapping)
make vet    # go vet
make build  # → bin/densewatch-sim
```

## License

Apache-2.0 © 2026 ZNTRAQ.
