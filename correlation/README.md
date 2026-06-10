# correlation (M3)

The differentiator: join `GPU job → node → rack → power feed → cooling loop` using
NetBox as the topology source of truth, with the **Slurm job ID / k8s pod UID** as
the canonical correlation key (consumed from dcgm-exporter's `hpc_job` and pod labels).

Derived metrics: rack-level kW, power density, stranded power, cooling ΔT vs. power,
throttle-vs-thermal correlation, job→rack thermal attribution.

Not built yet - depends on M1 (exporter) and M2 (backbone). NetBox does not model
cooling loops natively, so this adds custom fields / a small plugin for the cooling side.
