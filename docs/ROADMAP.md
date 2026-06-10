# Roadmap

Derived from `oss-tool-spec-v0.2`. Time-funded, ~$0 capital. Read-only telemetry only in v0.1.

## M0 - repo + simulator  *(in progress)*
Zero-hardware simulator so the whole stack can be built and demoed without a DC.
- [x] Repo scaffold, Apache-2.0, Go module, Makefile, tests
- [x] Redfish `CoolingUnit`/`CoolingLoop` simulator (DSP2064-shaped tree, live values)
- [x] dcgm-exporter-style GPU metrics with the `hpc_job` correlation key
- [x] Shared workload signal → GPU power and CDU heat correlate; heat balance holds
- [x] Modbus-TCP CDU simulator (the fallback path - a CDU that does *not* speak Redfish; FC3/4, 13-register map)
- [ ] SNMP PDU simulator (rack power)

## M1 - `densewatch-cdu` exporter  *(core working)*
The wedge. Ship standalone for the first public release.
- [x] Redfish `CoolingUnit` collector (follows @odata.id links; SensorExcerpt readings)
- [x] **Modbus** fallback behind one unified metric schema *(SNMP/BACnet adapters next)*
- [x] Unified `densewatch_cdu_*` schema + exposition + tests (Redfish + Modbus, end-to-end)
- [x] **Conformance probe** - `densewatch-cdu probe <url>` reports which DSP2064 props a unit actually serves
- [ ] External vendor-profile files (YAML/JSON) instead of the in-code sim profile
- [ ] Pin schema ≥ v1.2 / track quarterly drift; validate semantics vs DMTF Redfish-Tacklebox
- [ ] Demo GIF

## M2 - backbone wiring  *(stack authored)*
- [x] docker-compose: simulator + densewatch-cdu → VictoriaMetrics → Grafana, against the simulator
- [x] Provisioned Grafana datasource + "power × thermal" dashboard (GPU power vs CDU heat, coolant temps, per-job power)
- [x] Live-run verified end-to-end (VictoriaMetrics scraped exporter + dcgm; Grafana dashboard rendered live, screenshot captured)

## M3 - correlation + dashboards  *(engine working)*
- [x] Topology join-key metrics (`densewatch-correlate`): node → rack → cdu + rack power capacity; key = Hostname / `hpc_job` (Slurm job ID / k8s pod UID)
- [x] Derived attribution via PromQL: per-rack kW, power density (% of capacity), per-rack-per-job power, CDU load vs capacity
- [x] Correlation dashboard (per-rack power, density, job→rack, CDU attribution)
- [ ] NetBox backend for the topology (replace the JSON file) + cooling-loop custom fields
- [ ] Stranded-power / capacity-planner panels; rack heatmap; ΔT-vs-power

## M4 - polish for adoption
- [ ] Helm chart, docs site, "why integrated power+thermal matters" post
- [ ] Submit to awesome-prometheus / awesome-selfhosted / r/datacenter / HN

## Pre-launch validation (ship-resolvable, not blockers)
- [ ] Vendor-conformance census (do as the fallback adapter is built)
- [ ] Demand signal - watch stars/issues/inbound after the M1 post
