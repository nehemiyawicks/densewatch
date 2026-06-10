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
Other flags: `-listen` (default `:9839`), `-timeout` (default `5s`).

### Conformance probe

```sh
go run . probe http://<bmc>/redfish/v1     # service root, ThermalEquipment, or CoolingUnit URL
```

Reports which DSP2064 `CoolingUnit` properties the unit actually serves, a coverage
score, and a verdict (GOOD / PARTIAL / SPARSE, or NO REDFISH → use a Modbus/SNMP profile).

## Metrics

`densewatch_cdu_up`, `_info`, `_scrape_duration_seconds`, plus gauges
`coolant_{supply,return,delta}_temp_celsius`, `coolant_flow_lpm`, `heat_removed_kw`,
`coolant_{supply,return}_pressure_kpa`, `pump_speed_percent`,
`reservoir_level_percent`, `cooling_capacity_kw`, `inlet_temp_celsius`,
`humidity_percent`, `dew_point_celsius`, `leak_detected`. Labels: `cdu`, `protocol`.

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
- [ ] External vendor-profile files (YAML/JSON) instead of the in-code sim profile
- [ ] Pin/track `CoolingUnit` schema versions; validate vs DMTF Redfish-Tacklebox
