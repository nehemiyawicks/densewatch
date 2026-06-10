# deploy - full demo stack (M2)

One command stands up the whole pipeline with **no hardware**:

```
simulator ──Redfish + Modbus──▶ densewatch-cdu ──▶ VictoriaMetrics ──▶ Grafana
   └────────── dcgm GPU metrics (hpc_job) ──────────────▶ ┘
```

```sh
docker compose -f deploy/docker-compose.yml up --build
# or, from the repo root:  make demo
```

Then open **http://localhost:3000** → dashboard *"densewatch - AI-infra power × thermal"*
(anonymous admin, no login). It shows GPU power next to CDU heat removed (the
correlation), coolant temps, per-job GPU power, and flow/pump.

Tear down: `make demo-down` (or `docker compose -f deploy/docker-compose.yml down -v`).

| Service | Port | What |
|---|---|---|
| grafana | 3000 | dashboards (open this) |
| victoriametrics | 8428 | metrics store / query API |
| cdu-exporter | 9839 | unified `densewatch_cdu_*` metrics |
| sim | 9400 / 5000 | dcgm metrics / Redfish CoolingUnit |

Files: `Dockerfile` (builds either Go binary via `TARGET`), `docker-compose.yml`,
`victoriametrics/scrape.yml`, `grafana/provisioning/*`, `grafana/dashboards/densewatch.json`.
