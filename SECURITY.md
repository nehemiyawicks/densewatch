# Security Policy

## Supported versions

densewatch is pre-1.0. Fixes are released on the latest tag and on `main`.

| Version | Supported |
| --- | --- |
| latest release (`v0.1.x`) | :white_check_mark: |
| `main` | :white_check_mark: |
| older tags | :x: |

## Reporting a vulnerability

Please report vulnerabilities **privately** - do not open a public issue.

Use GitHub private vulnerability reporting: the repository **Security** tab ->
**Report a vulnerability**
([report directly](https://github.com/nehemiyawicks/densewatch/security/advisories/new)).

Expect an acknowledgement within **72 hours** and an initial assessment within
about a week. If the report is accepted we will work on a fix and agree a
disclosure timeline with you; if declined we will explain why.

## Security posture

densewatch is **read-only by design** - it scrapes CDU and GPU telemetry and never
controls or actuates hardware, which limits blast radius. It still talks to
management interfaces, so when you deploy it:

- Treat Redfish / BMC credentials as secrets. Pass them via environment or a
  secrets store; never commit them.
- Keep the exporters on a management network segment, not exposed to untrusted clients.
- The `/metrics` endpoints are unauthenticated by design (the standard Prometheus
  exporter model). Put them behind your own network controls.
