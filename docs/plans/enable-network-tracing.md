# Enable Network Tracing for tmpnet

## Goal

Configure tmpnet-managed AvalancheGo nodes to export traces to Grafana Cloud Tempo
via the local Alloy collector. Add `--enable-tracing` and `--check-traces-collected`
flags following the same pattern as metrics and logs collection.

## Prerequisites

This plan assumes the Alloy migration described in
[migrate-to-alloy](migrate-to-alloy.md) is complete. Alloy is running as the single
collector process, started by `--start-collector`, handling metrics and logs pipelines.

## Relevant Existing State

### AvalancheGo tracing support

AvalancheGo already has full OTLP trace export:

- `--tracing-exporter-type` — `disabled`, `grpc`, or `http` (default: `disabled`)
- `--tracing-endpoint` — OTLP endpoint (default: `localhost:4317` for gRPC)
- `--tracing-headers` — map of headers (for auth)
- `--tracing-insecure` — skip TLS (default: `true`)
- `--tracing-sample-rate` — sampling fraction (default: `0.1`)

Config keys in [config/keys.go](/config/keys.go). Exporter implementation in
[trace/exporter.go](/trace/exporter.go). Server-side API handlers are wrapped with
tracing in [api/server/server.go](/api/server/server.go) when tracing is enabled.

### Node configuration

`network.DefaultFlags` is a `FlagsMap` persisted to the network config file. Node
flags are composed in `Node.composeFlags()` which applies `network.DefaultFlags` as
defaults. Config set in `DefaultFlags` is inherited by all nodes and persists across
`--reuse-network`.

## Architecture

```
AvalancheGo nodes → OTLP gRPC (localhost:4317) → Alloy → Grafana Cloud Tempo
```

Nodes push traces to Alloy's local OTLP receiver. Alloy handles authentication and
forwarding to Grafana Cloud Tempo. Nodes do not need Tempo credentials.

## Design

### Environment variables

New env vars for Tempo, following the `PROMETHEUS_*` / `LOKI_*` pattern:

| Env var          | Used by            | Purpose                    |
|------------------|--------------------|----------------------------|
| `TEMPO_ENDPOINT` | Alloy config       | gRPC OTLP ingest host:port |
| `TEMPO_URL`      | `CheckTracesExist` | HTTP query base URL        |
| `TEMPO_USERNAME` | Both               | Basic auth username        |
| `TEMPO_PASSWORD` | Both               | Basic auth password        |

### Alloy trace pipeline

When `TEMPO_*` env vars are set, the generated Alloy config includes an OTLP
receiver and Tempo exporter in addition to the existing metrics and logs pipelines:

```river
otelcol.receiver.otlp "default" {
  grpc { endpoint = "127.0.0.1:4317" }
  output { traces = [otelcol.exporter.otlp.grafanacloud.input] }
}

otelcol.exporter.otlp "grafanacloud" {
  client {
    endpoint = "<TEMPO_ENDPOINT>"
    auth     = otelcol.auth.basic.grafanacloud.handler
  }
}

otelcol.auth.basic "grafanacloud" {
  username = "<TEMPO_USERNAME>"
  password = "<TEMPO_PASSWORD>"
}
```

The trace pipeline is only included when `TEMPO_*` env vars are set. Alloy config
generation (from the Alloy migration) already conditionally includes pipelines based
on which env vars are present.

### Flags

- `--enable-tracing` (env: `TMPNET_ENABLE_TRACING`, default: empty string, non-empty
  enables) — configures nodes for trace export to local Alloy
- `--check-traces-collected` (env: `TMPNET_CHECK_TRACES_COLLECTED`, default: empty
  string, non-empty enables) — verifies traces arrived at Tempo

When `--enable-tracing` is set:

- Alloy config includes the OTLP receiver and Tempo exporter pipelines
- `network.DefaultFlags` gets tracing flags pointing nodes at `localhost:4317`
- `TEMPO_*` env vars must be set (error if missing)

### Node configuration injection

When `--enable-tracing` is set, inject into `network.DefaultFlags`:

```go
network.DefaultFlags[config.TracingExporterTypeKey] = "grpc"
network.DefaultFlags[config.TracingEndpointKey] = "127.0.0.1:4317"
network.DefaultFlags[config.TracingInsecureKey] = "true"
network.DefaultFlags[config.TracingSampleRateKey] = "1.0"
```

No `--tracing-headers` needed — nodes push to local Alloy without auth. Alloy
handles Grafana Cloud authentication.

`--tracing-insecure=true` is correct here since the connection is to localhost.

The sample rate of `1.0` captures everything, appropriate for e2e tests.

Since `DefaultFlags` is persisted in the network config file, tracing configuration
survives `--reuse-network`.

### Resource attribute injection

Traces need `network_uuid` and `node_id` as resource attributes so they are queryable
in Tempo. Two approaches:

1. **`OTEL_RESOURCE_ATTRIBUTES` env var on node processes.** Set
   `OTEL_RESOURCE_ATTRIBUTES=network_uuid=<uuid>,node_id=<id>` in the node's process
   environment. The Go OTLP SDK picks these up automatically and attaches them to all
   exported spans. Handles per-node attributes naturally.

2. **Alloy `otelcol.processor.attributes`** adds attributes at the collector level.
   Good for network-wide attributes like `network_uuid` but cannot distinguish
   per-node `node_id` since Alloy is a shared process.

Recommended: use `OTEL_RESOURCE_ATTRIBUTES` on node processes. It requires no Alloy
config changes and handles both network-wide and per-node attributes. Evaluate whether
the Go SDK reliably picks up the env var during implementation.

### Reuse-network compatibility

When loading an existing network with `--reuse-network` or `--restart-network`, check
whether the loaded network's `DefaultFlags` include tracing configuration:

| Run requests tracing | Reused network has tracing | Behavior                                 |
|----------------------|----------------------------|------------------------------------------|
| yes                  | yes                        | OK — proceed                             |
| yes                  | no                         | Error — nodes not configured for tracing |
| no                   | yes                        | Warn — traces emitted but not checked    |
| no                   | no                         | OK — proceed                             |

Check `config.TracingExporterTypeKey` in the loaded network's `DefaultFlags`.

### CheckTracesExist

Add `CheckTracesExist(ctx, log, networkUUID)` to `check_monitoring.go`, following the
same pattern as `CheckMetricsExist` and `CheckLogsExist`.

Query Tempo's search API filtered by `network_uuid` resource attribute:

```
GET <TEMPO_URL>/api/search?q={resource.network_uuid="<uuid>"}&start=<epoch>&end=<epoch>
```

Authenticate with `TEMPO_USERNAME` / `TEMPO_PASSWORD` via basic auth header (same
pattern as `queryLoki`).

Use `waitForCount` to poll until traces appear or timeout.

### Grafana link for traces

Emit trace query info at network start so the user can find traces in Grafana. Start
with a log message containing the Tempo query parameters. Refine to a clickable Grafana
Explore link once the workflow is validated.

### Wiring in NewTestEnvironment

In [tests/fixture/e2e/env.go](/tests/fixture/e2e/env.go) `NewTestEnvironment`, add
handling parallel to the existing metrics/logs pattern:

```go
// Before network start
if flagVars.EnableTracing() {
    applyTracingFlags(desiredNetwork)
}

// Register cleanup (before network start for LIFO ordering)
if flagVars.CheckTracesCollected() {
    tc.DeferCleanup(func() {
        if network == nil {
            tc.Log().Warn(
                "unable to check that traces were collected" +
                " from an uninitialized network",
            )
            return
        }
        ctx, cancel := context.WithTimeout(
            context.Background(), DefaultTimeout,
        )
        defer cancel()
        require.NoError(
            tmpnet.CheckTracesExist(ctx, tc.Log(), network.UUID),
        )
    })
}

// Reuse-network tracing compatibility check
if network != nil && flagVars.EnableTracing() {
    checkTracingCompatibility(network)
}
```

## Implementation Steps

### Step 1. Add OTLP receiver pipeline to Alloy config generation

When `TEMPO_*` env vars are set, include the OTLP receiver, Tempo exporter, and basic
auth blocks in the generated Alloy config.

Validation: generated config includes trace pipeline when TEMPO vars are set, omits it
when they are not.

### Step 2. Add --enable-tracing flag and node config injection

Add the flag. When set, inject tracing flags into `network.DefaultFlags` pointing at
`localhost:4317`. Set `OTEL_RESOURCE_ATTRIBUTES` in the node process environment with
`network_uuid` and `node_id`. Error if `TEMPO_*` env vars are missing.

Validation: start a network with `--enable-tracing`, verify node config files contain
tracing flags pointing at `127.0.0.1:4317`.

### Step 3. Add CheckTracesExist and --check-traces-collected

Implement the Tempo query function. Wire `--check-traces-collected` into
`NewTestEnvironment` as a deferred cleanup.

Validation: run an e2e test with `--start-collector --enable-tracing
--check-traces-collected` and verify traces arrive in Tempo and the check passes.

### Step 4. Add reuse-network compatibility check

Check the loaded network's `DefaultFlags` for tracing config and compare against the
current `--enable-tracing` flag.

Validation: test that correct error/warning is produced for each combination in the
compatibility matrix.

### Step 5. Emit trace link or query info

Log Tempo query info at network start. Optionally generate a Grafana Explore link.

Validation: manual — verify logged info leads to traces in Grafana.

## Validation

- `--start-collector --enable-tracing --check-traces-collected` passes
- Traces are visible in Grafana filtered by `network_uuid`
- Nodes push to localhost without needing Tempo credentials
- `--enable-tracing` without `--start-collector` produces a clear error
- `--enable-tracing` with `--reuse-network` on an untraced network errors
- Check flags default from env vars (non-empty string enables)

## Open Questions

### OTEL_RESOURCE_ATTRIBUTES propagation

Need to verify the Go OTLP SDK picks up `OTEL_RESOURCE_ATTRIBUTES` from the process
environment. It should per the OpenTelemetry spec, but needs confirmation with the
specific SDK version used by AvalancheGo.

### Shutdown flush

AvalancheGo's tracer has a 15-second shutdown timeout. The existing
`NetworkShutdownDelay` (12s, for scrape interval) should be sufficient since Alloy
is still running when nodes shut down and flush. Verify that Alloy's OTLP receiver
accepts the final batch before the node process exits.

## Risks

- **Span volume**: `sample-rate=1.0` may produce many spans in a full e2e run. Fine
  for PoC, may need tuning for regular CI.
- **Tempo search latency**: Traces may not be immediately queryable after export.
  `CheckTracesExist` uses polling (via `waitForCount`) which handles this, but the
  timeout may need tuning.
