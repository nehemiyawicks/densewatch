# densewatch-cdu

A Prometheus exporter for CDU / liquid-cooling telemetry that scrapes CDUs over
**Redfish (DSP2064 `CoolingUnit`) _and_ Modbus-TCP** and normalizes both into one
unified `densewatch_cdu_*` metric schema. Read-only.

That single schema across heterogeneous protocols - Redfish for units that speak
it, Modbus/SNMP for the many that don't - is the differentiator. A value a given
unit doesn't expose renders as an *absent* metric, never a fake zero.

## Run

```sh
# against the local simulator (run `make sim` in the repo root first)
go run . -redfish http://localhost:5000/redfish/v1/ThermalEquipment/CDUs/1 \
         -modbus  localhost:5020
# → http://localhost:9839/metrics
```

`-redfish <url>` and `-modbus <host:port>` are repeatable (scrape many CDUs).
Other flags: `-listen` (default `127.0.0.1:9839`; bind a routable address only behind
your own controls), `-timeout` (default `5s`), `-auth-token` (require
`Authorization: Bearer <token>` on `/metrics`), `-modbus-profile <file>` (JSON
register map for a non-simulator CDU; see below), `-ca-cert` / `-insecure-skip-verify`
(Redfish TLS). Credentials: URL userinfo or `REDFISH_USERNAME` / `REDFISH_PASSWORD`.

### Conformance probe

```sh
go run . probe http://<bmc>/redfish/v1     # service root, ThermalEquipment, or CoolingUnit URL
```

Reports which DSP2064 `CoolingUnit` properties the unit actually serves, a coverage
score, and a verdict (GOOD / PARTIAL / SPARSE, or NO REDFISH → use a Modbus/SNMP profile).

### Modbus vendor profiles

CDUs that speak Modbus instead of Redfish expose telemetry as raw registers with a
vendor-specific map. Point densewatch at yours with a JSON profile - no code change:

```sh
go run . -modbus <host:port> -modbus-profile profiles/example.json
```

Each entry maps a register `addr` (read as a 16-bit input register) and a `scale` to
a schema `field`. Valid field names: `supply_temp_c`, `return_temp_c`, `delta_temp_c`,
`flow_lpm`, `heat_removed_kw`, `pump_pct`, `supply_kpa`, `return_kpa`, `reservoir_pct`,
`inlet_temp_c`, `humidity_pct`, `dew_point_c`, `leak` (non-zero = leak). Copy
[`profiles/example.json`](profiles/example.json) and edit the addresses/scales.
**Have a CDU? Contributing its profile is a one-file PR** - that is how coverage grows.

## Metrics

`densewatch_cdu_up`, `_info`, `_scrape_duration_seconds`, plus gauges
`coolant_{supply,return,delta}_temp_celsius`, `coolant_flow_lpm`, `heat_removed_kw`,
`coolant_{supply,return}_pressure_kpa`, `pump_speed_percent`,
`reservoir_level_percent`, `cooling_capacity_kw`, `inlet_temp_celsius`,
`humidity_percent`, `dew_point_celsius`, `leak_detected`, and `coolant_pair_reversed`
(derived: 1 when supply/return appear swapped). Labels: `cdu`, `protocol`.

## Compatibility

- **GPU side (the correlation join):** NVIDIA dcgm-exporter - densewatch joins on its
  `hpc_job` label. Both Kubernetes (PodMapper) and Slurm (via the Slinky integration's
  prolog/epilog job-mapping files) populate `hpc_job`, so either scheduler works.
  Tested against DCGM 4.5.x / dcgm-exporter 4.8.x. densewatch relies only on
  `DCGM_FI_DEV_POWER_USAGE` (stable across DCGM 3.x/4.x); it does **not** require the
  `DCGM_FI_PROF_PCIE_*` / `NVLINK_*` fields that exporter 4.x dropped from its defaults.
- **CDU side:** DMTF Redfish `CoolingUnit` (DSP2064; validate against the latest DSP8010
  schema bundle) for units that speak it, Modbus-TCP register-map profiles for those that
  don't. This Redfish-primary + Modbus/SNMP/BACnet-fallback split matches OCP's 2025
  "Third Party Integration, Telemetry & APIs" telemetry guidance.

## Design

`schema.go` defines the unified `Reading` (pointer fields → absent-when-unsupported)
and the Prometheus exposition. `redfish.go` follows `@odata.id` links
(collection → member) and reads `SensorExcerpt` `{"Reading": x}` values.
`modbus.go` is a minimal Modbus-TCP client (FC 0x04) plus a register-map **vendor
profile** - the in-miniature version of the per-vendor profiles that give broad
heterogeneous-CDU coverage.

## Status / next

- [x] Redfish `CoolingUnit` collector (link-following, SensorExcerpt readings)
- [x] Modbus-TCP collector via a register-map vendor profile
- [x] Unified schema + exposition + tests (Redfish + Modbus, end-to-end)
- [ ] SNMP / BACnet adapters (same profile pattern)
- [x] Conformance probe - `densewatch-cdu probe <url>` reports which DSP2064 properties a unit actually serves
- [x] External vendor-profile files (JSON) - `-modbus-profile <file>`; see `profiles/`
- [ ] Pin/track `CoolingUnit` schema versions; validate vs DMTF Redfish-Tacklebox
