# Roadmap

Derived from `oss-tool-spec-v0.2`. Time-funded, ~$0 capital. Read-only telemetry only in v0.1.

## M0 — repo + simulator  *(in progress)*
Zero-hardware simulator so the whole stack can be built and demoed without a DC.
- [x] Repo scaffold, Apache-2.0, Go module, Makefile, tests
- [x] Redfish `CoolingUnit`/`CoolingLoop` simulator (DSP2064-shaped tree, live values)
- [x] dcgm-exporter-style GPU metrics with the `hpc_job` correlation key
- [x] Shared workload signal → GPU power and CDU heat correlate; heat balance holds
- [x] Modbus-TCP CDU simulator (the fallback path — a CDU that does *not* speak Redfish; FC3/4, 13-register map)
- [ ] SNMP PDU simulator (rack power)

## M1 — `densewatch-cdu` exporter
The wedge. Ship standalone for the first public release.
- [ ] Redfish `CoolingUnit` collector (pin schema ≥ v1.2; design for quarterly drift)
- [ ] **Modbus/SNMP fallback** behind one unified metric schema *(first-class, not a stretch)*
- [ ] **Per-vendor conformance probe** + shipped vendor profiles (don't assume DSP2064 from a "Redfish" bullet)
- [ ] Tests, README, demo GIF — validate semantics against DMTF Redfish-Tacklebox

## M2 — backbone wiring
- [ ] docker-compose: dcgm + densewatch-cdu + snmp → VictoriaMetrics → Grafana, against the simulator

## M3 — correlation + dashboards
- [ ] NetBox topology join (+ cooling-loop custom fields); canonical key = Slurm job ID / k8s pod UID
- [ ] Derived metrics: rack kW, power density, stranded power, ΔT-vs-power, job→rack thermal attribution
- [ ] 4 Grafana dashboards: rack heatmap, capacity planner, job impact, cooling health

## M4 — polish for adoption
- [ ] Helm chart, docs site, "why integrated power+thermal matters" post
- [ ] Submit to awesome-prometheus / awesome-selfhosted / r/datacenter / HN

## Pre-launch validation (ship-resolvable, not blockers)
- [ ] Vendor-conformance census (do as the fallback adapter is built)
- [ ] Demand signal — watch stars/issues/inbound after the M1 post
