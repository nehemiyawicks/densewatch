# deploy (M2)

One-command stack: `dcgm + densewatch-cdu + snmp_exporter → VictoriaMetrics → Grafana`,
wired against the M0 simulator so anyone can `make demo` with no hardware.

- `docker-compose.yml` — local demo stack
- `helm/` — Kubernetes chart (M4)

Not built yet.
