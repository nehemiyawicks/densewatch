# Architecture

densewatch is a few small, single-purpose Go binaries plus a standard metrics
backbone. Read-only throughout.

## Data flow

```
   CDUs ---(Redfish CoolingUnit, Modbus)---> densewatch-cdu ----.
                                                                | densewatch_cdu_*
   GPUs ---(dcgm-exporter; hpc_job label)----------------.      |
                                                          v      v
   topology (file / NetBox) --> densewatch-correlate --> VictoriaMetrics --> Grafana
                                densewatch_topology_info        |
                                densewatch_rack_power_capacity  | PromQL joins
                                                                v
                              per-rack power . density . job->rack . CDU load
```

## Components

| Binary | Role |
|---|---|
| `simulator/` (`densewatch-sim`) | Zero-hardware fake feeds: Redfish CoolingUnit, dcgm GPU metrics, Modbus CDU. Drives GPU power and CDU heat from one shared signal so they correlate. |
| `exporters/cdu/` (`densewatch-cdu`) | Scrapes CDUs over Redfish (DSP2064 CoolingUnit) and Modbus, normalizing both into one `densewatch_cdu_*` schema. Also `densewatch-cdu probe <url>` for conformance. |
| `correlation/` (`densewatch-correlate`) | Reads the datacenter topology (node -> rack -> cdu, rack power capacity) and emits join-key metrics. |

## The correlation

The exporter and dcgm metrics both carry a `Hostname` label; `densewatch-correlate`
emits `densewatch_topology_info{Hostname, rack, cdu}`. PromQL joins on `Hostname`
to attribute GPU power to racks and CDUs:

```promql
# per-rack GPU power (kW)
sum by (rack) (DCGM_FI_DEV_POWER_USAGE * on(Hostname) group_left(rack) densewatch_topology_info) / 1000

# rack power density (% of provisioned capacity)
(sum by (rack) (DCGM_FI_DEV_POWER_USAGE * on(Hostname) group_left(rack) densewatch_topology_info) / 1000)
  / on(rack) densewatch_rack_power_capacity_kw * 100
```

## Topology source of truth

Today the topology is a JSON file (`correlation/topology.json`). A NetBox-backed
loader slots in behind the same model - NetBox is the data source, not the
correlation logic.

## Design choices

- **Read-only.** No control or actuation; a lower trust and liability bar for operators.
- **Zero dependencies.** Single static Go binaries; hand-rolled Prometheus exposition.
- **Heterogeneous CDUs.** Redfish where available, Modbus/SNMP profiles for the rest, one schema out.
