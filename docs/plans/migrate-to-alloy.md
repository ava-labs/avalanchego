# Migrate tmpnet Collectors from Prometheus + Promtail to Alloy

## Goal

Replace the separate Prometheus (agent mode) and Promtail detached processes with a
single Grafana Alloy instance for collecting metrics and logs from tmpnet-managed
AvalancheGo nodes. All existing behavior is preserved — same env vars, same remote
backends, same check functions — just a single collector process instead of two.

This is a prerequisite for
[enable-network-tracing](enable-network-tracing.md), which adds an OTLP trace
pipeline to Alloy.

## Current Collector Architecture

Two separate detached processes collect telemetry from tmpnet nodes:

- **Prometheus** (agent mode) — scrapes `/ext/metrics` from each node, pushes to a
  remote Prometheus backend via `remote_write`. Configured with `PROMETHEUS_*` env vars.
  Started by `--start-metrics-collector`.
- **Promtail** — tails log files from each node's data dir, pushes to a remote Loki
  backend. Configured with `LOKI_*` env vars. Started by `--start-logs-collector`.

Both use file-based service discovery: each node writes a JSON config file to
`~/.tmpnet/{prometheus,promtail}/file_sd_configs/` on startup. Both are managed as
detached processes with PID files and readiness checks in
[monitor_processes.go](/tests/fixture/tmpnet/monitor_processes.go).

Collection verification uses separate check functions:

- `CheckMetricsExist` queries the remote Prometheus API
- `CheckLogsExist` queries the remote Loki API
- Both filter by `network_uuid`

Key files:

- [tests/fixture/tmpnet/monitor_processes.go](/tests/fixture/tmpnet/monitor_processes.go)
  — process management, config generation, service discovery
- [tests/fixture/tmpnet/check_monitoring.go](/tests/fixture/tmpnet/check_monitoring.go)
  — `CheckMetricsExist`, `CheckLogsExist`, backend querying
- [tests/fixture/tmpnet/flags/collector.go](/tests/fixture/tmpnet/flags/collector.go)
  — `CollectorVars` flag registration
- [tests/fixture/e2e/flags.go](/tests/fixture/e2e/flags.go)
  — e2e flag registration including check flags
- [tests/fixture/e2e/env.go](/tests/fixture/e2e/env.go)
  — `NewTestEnvironment` wiring: starts collectors, registers deferred checks
- [tests/fixture/tmpnet/process_runtime.go](/tests/fixture/tmpnet/process_runtime.go)
  — `writeMonitoringConfig` writes per-node service discovery files

## Why Alloy

Grafana Alloy replaces Prometheus agent + Promtail with a single process that handles
metrics scraping and log tailing. It is Grafana's recommended collector for their cloud
stack.

| Current                          | Alloy equivalent                                |
|----------------------------------|-------------------------------------------------|
| Prometheus agent mode            | `prometheus.scrape` → `prometheus.remote_write`  |
| Promtail                         | `loki.source.file` → `loki.write`               |

What stays the same:

- Env vars for remote backends (`PROMETHEUS_*`, `LOKI_*`)
- Check functions query the same remote APIs
- File-based service discovery (Alloy supports the same JSON format)
- Per-node config files written on startup/removed on shutdown
- Detached process with PID file and readiness check

The migration also establishes the infrastructure for trace collection: Alloy can
receive OTLP traces and forward them to Grafana Cloud Tempo. That pipeline is added
in the [tracing plan](enable-network-tracing.md), not here.

## Design

### Environment variables

All existing env vars are preserved with the same semantics:

| Env var               | Used by             | Purpose                |
|-----------------------|---------------------|------------------------|
| `PROMETHEUS_URL`      | `CheckMetricsExist` | Query endpoint         |
| `PROMETHEUS_PUSH_URL` | Alloy config        | Remote write endpoint  |
| `PROMETHEUS_USERNAME` | Both                | Basic auth username    |
| `PROMETHEUS_PASSWORD` | Both                | Basic auth password    |
| `LOKI_URL`            | `CheckLogsExist`    | Query endpoint         |
| `LOKI_PUSH_URL`       | Alloy config        | Push endpoint          |
| `LOKI_USERNAME`       | Both                | Basic auth username    |
| `LOKI_PASSWORD`       | Both                | Basic auth password    |

### Flags

Replace the two collector start flags with a single flag:

- `--start-collector` (env: `TMPNET_START_COLLECTOR`) — starts Alloy

Keep the flag name behavior-oriented rather than implementation-oriented:
`start-collector` matches the existing `start-*` / `stop-*` tmpnetctl commands,
while avoiding a future CLI rename if the collector implementation changes again.

Remove the old flags (`--start-metrics-collector`, `--start-logs-collector`) in the
same change and update all in-repo uses to `--start-collector`. Do not keep aliases:
the migration is mechanical, the old flags express an obsolete split-process model,
and retaining them would complicate flag plumbing and test coverage for little value.

Check flags are unchanged:

- `--check-metrics-collected` — queries remote Prometheus (unchanged)
- `--check-logs-collected` — queries remote Loki (unchanged)

### Alloy configuration

Alloy uses River syntax. The config is generated at startup from env vars, similar to
how Prometheus and Promtail configs are currently generated as YAML templates.

Metrics pipeline (replaces Prometheus agent mode):

```river
prometheus.scrape "avalanchego" {
  targets    = discovery.relabel.avalanchego_metrics.output
  forward_to = [prometheus.remote_write.grafanacloud.receiver]
}

discovery.file "avalanchego" {
  files = ["~/.tmpnet/alloy/file_sd_configs/*.json"]
}

discovery.relabel "avalanchego_metrics" {
  targets = discovery.file.avalanchego.targets

  rule {
    source_labels = ["signal"]
    regex         = "metrics"
    action        = "keep"
  }
}

prometheus.remote_write "grafanacloud" {
  endpoint {
    url = "<PROMETHEUS_PUSH_URL>"
    basic_auth {
      username = "<PROMETHEUS_USERNAME>"
      password = "<PROMETHEUS_PASSWORD>"
    }
  }
}
```


Logs pipeline (replaces Promtail):

```river
loki.source.file "avalanchego" {
  targets    = discovery.relabel.avalanchego_logs.output
  forward_to = [loki.write.grafanacloud.receiver]
}

discovery.relabel "avalanchego_logs" {
  targets = discovery.file.avalanchego.targets

  rule {
    source_labels = ["signal"]
    regex         = "logs"
    action        = "keep"
  }
}

loki.write "grafanacloud" {
  endpoint {
    url        = "<LOKI_PUSH_URL>"
    basic_auth {
      username = "<LOKI_USERNAME>"
      password = "<LOKI_PASSWORD>"
    }
  }
}
```

The metrics and logs pipelines are only included in the generated config when their
respective env vars are set. This allows running Alloy with only metrics, only logs,
or both.

Even though both pipelines read from the same Alloy service-discovery directory, the
per-node JSON payload still needs multiple target entries because tmpnet currently
writes different target shapes today:

- Metrics targets use the node's HTTP API address as the scrape target.
- Log targets use `localhost` plus a `__path__` label pointing at the node's log files.

Because those shapes are different, the Alloy migration should keep a single directory
and write one file per node containing both entries. Add a label such as
`signal=metrics` / `signal=logs`, then use `discovery.relabel` to split the shared
target set before wiring it into `prometheus.scrape` and `loki.source.file`. Trace
collection is different again: OTLP ingestion does not use file-based discovery, so it
does not add another per-node SD file format.

### Service discovery

Alloy uses the same Prometheus file_sd JSON format. The service discovery dir moves
from separate `~/.tmpnet/{prometheus,promtail}/file_sd_configs/` dirs to a single
`~/.tmpnet/alloy/file_sd_configs/` dir.

Each node writes one config file on startup into that shared directory. The file
contains two target entries:

- a metrics entry for the node's scrape address with `signal=metrics`
- a logs entry for the node's log glob with `__path__` and `signal=logs`

Both entries carry the same node-identifying labels (`network_uuid`, `node_id`,
`is_ephemeral_node`, `network_owner`, and GitHub CI labels).

### Process management

Replace `startPrometheus` and `startPromtail` with `startAlloy`:

- Working dir: `~/.tmpnet/alloy/`
- Config file: `~/.tmpnet/alloy/config.alloy`
- PID file: `~/.tmpnet/alloy/run.pid`
- Log file: `~/.tmpnet/alloy/alloy.log`
- Readiness: `http://127.0.0.1:12345/ready` (Alloy's default HTTP port)

The existing `startCollector` helper handles PID files, log files, and readiness checks.
It should work with Alloy with minimal changes — mainly the readiness URL and command
arguments.

### Nix flake

Add `grafana-alloy` to `flake.nix`. Remove `promtail` once Promtail is no longer used.
Remove `prometheus` as well if no other local tmpnet workflow shells out to it. Based
on the current tree, the standalone binaries are used for the tmpnet local collector
path, while backend querying uses Go client libraries rather than the `prometheus`
binary. Re-check for any non-tmpnet shell usage when making the flake change, but the
expected outcome is that both `promtail` and `prometheus` drop out of the dev shell and
are replaced by `grafana-alloy`.

## Implementation Steps

### Step 1. Add Alloy to the nix flake and verify it runs

Add `grafana-alloy` to `flake.nix`. Verify `alloy run config.alloy` starts and the
readiness endpoint responds.

Validation: `alloy --version` works in `nix develop`, a minimal config starts cleanly.

### Step 2. Replace StartPrometheus/StartPromtail with StartAlloy

Implement `StartAlloy` so it generates `config.alloy` from `PROMETHEUS_*` and
`LOKI_*` env vars as part of startup, writes it into the Alloy working directory, and
starts the Alloy process. Keep config rendering as a small helper so it can be unit
tested directly, but treat it as part of the `StartAlloy` implementation rather than a
separate migration step.

Add `StartAlloy` / `StopAlloy` to `monitor_processes.go`. Wire it into `tmpnetctl`
as `start-collector` / `stop-collector`. Update `NewTestEnvironment` to call
`StartAlloy` instead of the separate functions.

Validation:

- Unit test config rendering for metrics-only, logs-only, and metrics+logs cases.
- Run an e2e test with `--start-collector --check-metrics-collected
  --check-logs-collected` and verify both checks pass.

### Step 3. Update service discovery paths and payloads

Move service discovery writes from
`~/.tmpnet/{prometheus,promtail}/file_sd_configs/` to
`~/.tmpnet/alloy/file_sd_configs/`. Update `writeMonitoringConfig` in
`process_runtime.go` so each node writes one JSON file containing both the metrics and
logs target entries.

Validation: nodes write SD configs to the new path, Alloy picks them up, metrics
and logs flow.

### Step 4. Update flags and remove old collector code

Replace `--start-metrics-collector` / `--start-logs-collector` with `--start-collector`.
Remove `startPrometheus`, `startPromtail`, and related config generation. Update
`tmpnetctl` commands.

Validation: existing CI workflows updated and passing. Old flags are removed from the
CLI surface and from in-repo invocations.

### Step 5. Update NetworkShutdownDelay

The current delay (12s) is based on Prometheus scrape interval. Alloy's scrape interval
will be the same, so the delay logic should be preserved but tied to whether the
collector is running rather than specifically whether Prometheus is running.

Validation: metrics at end of test are captured before shutdown.

## Validation

- `--start-collector --check-metrics-collected` passes (metrics flow through Alloy)
- `--start-collector --check-logs-collected` passes (logs flow through Alloy)
- Both checks pass together
- `tmpnetctl start-collector` / `stop-collector` work
- Repeated start is idempotent
- Grafana dashboard links still work (they query the same remote backends)
- Existing remote backend env vars work unchanged

## Risks

- **Alloy config syntax**: River is less familiar than YAML. Config generation needs
  careful testing. Invalid config will prevent Alloy from starting, which is fail-fast
  but could block CI if not caught early.
- **Service discovery migration**: The directory structure stays simple, but the node
  runtime and cleanup paths must switch from one-file-per-backend to two-files-in-one-
  directory. That is straightforward, but it touches both write and cleanup logic.
