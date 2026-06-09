# densewatch-cdu (M1)

The wedge: a Prometheus exporter for CDU / liquid-cooling telemetry that speaks
**Redfish `CoolingUnit` *and* Modbus/SNMP** behind one unified metric schema, with
a **per-vendor conformance probe** and shipped vendor profiles.

Not built yet — M0 (the simulator this exporter scrapes) comes first. Develop it
against `../../simulator` (`make sim`), then validate schema semantics against
[DMTF Redfish-Tacklebox](https://github.com/DMTF/Redfish-Tacklebox).

Planned layout:

```
cdu/
├── main.go
├── redfish/      # CoolingUnit/CoolingLoop client (pin ≥ v1.2)
├── adapters/     # modbus / snmp fallback — first-class, not a stretch
├── conformance/  # per-vendor DSP2064 probe
├── profiles/     # shipped vendor profiles (coolit, supermicro, stulz, …)
└── metrics.go    # one schema across all protocols
```
