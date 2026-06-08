# WebSocket RPC Connection Visibility

## Summary

We have effectively no visibility into how WebSocket RPC is being used.
An operator today cannot tell how many clients are connected, who they
are, what they have subscribed to, how much data is flowing, or why
connections drop. The server exposes only request-level metrics that
are shared across all transports and say nothing about the connections
themselves, and it emits no log on a successful connect or subscribe.

This document proposes closing that gap with connection- and
subscription-level **metrics** (active counts, throughput, lifetime,
per-subscription notification volume, categorized close reasons) plus
**INFO-level logs** on connect/subscribe, so that WebSocket usage is
observable both in aggregate (Prometheus) and per-event (logs).

Rather than extend the legacy go-metrics name-based registry (which has
no label support, so dimensions have to be baked into metric names), the
new metrics are **native Prometheus** vectors registered through a
`prometheus.Registerer` plumbed into the rpc package. While we are doing
that plumbing we also **migrate the existing `rpc/*` request metrics**
onto it so dimensions become real labels: `rpc_requests_total{status,transport}`
instead of separate `rpc/success`/`rpc/failure`, and
`rpc_request_duration_seconds{method,status,transport}` instead of the
method/status baked into the metric path. This is a **breaking change**
to those series, but one we expect to affect **very few queries** in
practice (the old per-method names are awkward to query and not widely
dashboarded; see Migrating the request metrics); it is accepted
deliberately to stop accreting the old pattern.

## Problem

The JSON-RPC server in `graft/evm/rpc` accepts WebSocket connections
via `WebsocketHandler`/`WebsocketHandlerWithDuration`
(`graft/evm/rpc/websocket.go:61`). Each accepted connection is wrapped
in a `websocketCodec` and tracked for its lifetime in the server's
`codecs` map (`graft/evm/rpc/server.go:62`, registered in
`trackCodec`/`untrackCodec` at `server.go:138-154`).

The only metrics currently registered (`graft/evm/rpc/metrics.go`) are
request-oriented and transport-agnostic:

| Metric                           | Type      | Meaning                                                 |
| -------------------------------- | --------- | ------------------------------------------------------- |
| `rpc/requests`                   | Gauge\*   | cumulative requests served                              |
| `rpc/success`                    | Gauge\*   | cumulative successful requests                          |
| `rpc/failure`                    | Gauge\*   | cumulative failed requests                              |
| `rpc/duration/all`               | Timer     | aggregate request serving time                          |
| `rpc/duration/{method}/{status}` | Histogram | per-method latency (`status` is `success` or `failure`) |

\* Registered as go-metrics gauges but only ever `Inc`'d
(`handler.go:619-623`), so they behave as monotonic counters, not
point-in-time gauges. Notably, there is **no** in-flight-concurrency
metric today.

None of these distinguish WebSocket traffic from HTTP, and none
describe the connection itself. As a result, operators cannot answer
basic questions:

- How many WebSocket connections are currently open?
- How many connections have been opened/closed over time, and why
  (clean close, read-limit exceeded, ping timeout, server shutdown)?
- How much data has been read from / written to WebSocket clients?
- How long do connections stay open?

This makes it hard to size limits, detect connection leaks, diagnose
clients that open and abandon connections, or attribute bandwidth.

The information needed for the most important metric (the active
connection count) is *already* maintained in `s.codecs`; it is simply
never published.

## Goals

- Expose the current number of active WebSocket connections.
- Count total connections opened and closed, with each close attributed
  to a categorized `reason`.
- Track bytes read from and written to WebSocket connections.
- Track connection lifetime (open-to-close duration).
- Track active subscriptions and notification volume, broken down by
  subscription kind (`eth_subscribe` type) via a real `subscription`
  label.
- Register all metrics as native Prometheus vectors through a plumbed
  `prometheus.Registerer`, so dimensions (status, method, reason,
  direction, subscription) are real labels rather than name segments.
- Migrate the existing `rpc/*` request metrics onto the same backend,
  collapsing `rpc/success`/`rpc/failure` into
  `rpc_requests_total{status,transport}` and the
  `rpc/duration/{method}/{status}` path into
  `rpc_request_duration_seconds{method,status,transport}`. The new
  `transport` label (ws/http/ipc) also makes WS-specific request rate a
  query rather than a separate metric. Breaking the old series is
  acceptable.
- Make per-client usage answerable (top IPs, repeat reconnectors, who
  subscribed to what), via INFO logs carrying client identity, so it
  can be aggregated offline rather than living in metrics.
- Count how often the existing CPU limiter throttles (delays) a WS
  request, so the impact of throttling is finally visible.
- Provide the request- and byte-rate signal that a *future* WS traffic
  throttle would need to set and tune a per-minute/hour cap (building or
  retuning a throttle is out of scope; see Non-Goals and Future Work).
- Keep the cost of forking upstream low. The `graft/evm/rpc` files are
  derived from `libevm`/go-ethereum, but that package is mature and
  nearly frozen (the files here last changed substantively in 2023–2024,
  with one geth backport since), so the migration runs almost no risk of
  fighting frequent re-syncs; we isolate the new code in a first-party
  file to keep the rare future merge cheap (see Metrics backend).
- Keep overhead negligible on the request hot path.

## Non-Goals

- Per-client / per-IP identity **in metrics**. Recording client
  identity as Prometheus labels would explode series cardinality. That
  is the lousy design this document explicitly avoids. (Per-client
  visibility itself *is* a goal; it is delivered through logs, not
  metrics. See Logging.)
- Per-subscription-instance metrics (one series per live subscription
  ID). Subscriptions are aggregated by *kind*, not by instance.
- Preserving the existing `rpc/success`, `rpc/failure`, and
  `rpc/duration/{method}/{status}` series. These are intentionally
  replaced (see Migrating the request metrics); any dashboard or alert
  referencing them must be updated.
- New connection/byte/subscription metrics for the **HTTP and IPC**
  transports. The native-Prometheus plumbing makes this straightforward
  later, but this design instruments the WebSocket path. (The migrated
  *request* metrics do cover all transports, distinguished by the
  `transport` label, since they sit on the shared dispatch path.)
- Building, changing, or retuning a throttle: neither the existing
  per-connection CPU limiter nor a new per-minute/hour traffic cap. This
  document only delivers visibility; the throttling mechanisms are
  out of scope (see Existing throttling and Future Work). The one
  exception pulled in scope is a *count* of how often the existing
  limiter engages, because it is cheap and immediately useful.
- Richer throttle-enforcement metrics (delay distribution, hard-limit
  rejections). Deferred to ship alongside the throttle work that needs
  them (see Future Work).

## Design

WebSocket connections have a single, well-defined lifecycle entry and
exit point in `ServeCodec` (`graft/evm/rpc/server.go:120`):
`trackCodec` runs when serving begins and `untrackCodec` (deferred)
runs when the connection ends. This is the natural place to instrument
connection open/close and lifetime without touching the request path.

Byte accounting belongs lower down, at the `websocketCodec` read/write
boundary (`graft/evm/rpc/websocket.go:305-360`), where every frame
crosses in or out.

### Metrics backend: native Prometheus

The existing `rpc/*` metrics use the global `libevm/metrics` (go-metrics)
registry, gathered into Prometheus by
`vms/evm/metrics/prometheus/prometheus.go`. That bridge maps a name
verbatim (`/`→`_`), emits **one unlabeled series per name**, and even
downgrades histograms to summaries; it has no concept of labels. The
only way to add a dimension there is to bake it into the metric name.

Instead, this design registers metrics as **native Prometheus vectors**
(`prometheus.CounterVec`/`GaugeVec`/`HistogramVec`) through a
`prometheus.Registerer` that is **plumbed into the rpc package**. This is
the pattern already used in `api/server/metrics.go` (`numCalls
*prometheus.CounterVec` labeled `base`, registered via a `Registerer`),
and the EVM VM already owns suitable registries
(`vm.sdkMetrics = prometheus.NewRegistry()`, plus the
`ctx.Metrics.Register(prefix, …)` mechanism). Concretely:

- `rpc.NewServer` gains a `prometheus.Registerer` parameter; it builds an
  `*rpcMetrics` struct (all the vectors below) once and shares it with
  every `handler` it creates.
- coreth/subnet-evm pass a registerer when constructing the handler
  (`vm.go` ~1030/1068), registered under an `rpc` prefix.

Cost, stated plainly: the `graft/evm/rpc` package (a fork of
go-ethereum's `rpc`) gains a `client_golang/prometheus` dependency it
does not have upstream, widening graft divergence. We accept that to get
real labels and to stop growing the name-segment pattern.

**Upstream-merge churn (low, but real).** All 24 files in
`graft/evm/rpc` are derived from `libevm`/go-ethereum, and `metrics.go`
already carries an `If resolving merge conflicts` banner because it
diverges to share metrics with `libevm/rpc`. Editing these files (a
`NewServer` signature change, threading `*rpcMetrics` through
`initClient`/`newHandler`, dispatch/`awaitLimit`/codec hooks), and
especially rewriting `metrics.go`, raises the chance of a conflict the
next time the package is re-synced. That risk is small in practice:
upstream's `rpc` package is mature and nearly frozen. The files here
last saw a substantive change in 2023–2024 (`metrics.go` in 2023-12,
`handler.go` in 2024-02), and the whole package has taken a single geth
backport since early 2024. So a conflict is a roughly once-a-year event,
not a per-bump one. We still keep it bounded:

- Put all new code (the `rpcMetrics` struct, every `*Vec`, and the
  close-reason/`transport` classification helpers) in a **new
  first-party file** (e.g. `ws_metrics.go`) that is *not* upstream-derived
  and therefore never conflicts.
- Reduce edits to the derived files to **small mechanical hooks** that
  call into that file, so a re-sync conflicts on a few call sites rather
  than rewritten bodies.
- The request-metric migration in `metrics.go` is the unavoidable
  exception (it deletes upstream-shared registrations); keep it a single
  isolated hunk and update the merge-conflict banner to describe the new
  contract.

Naming follows [Prometheus
conventions](https://prometheus.io/docs/practices/naming/): counters end
in `_total`, durations are `_seconds`, sizes are `_bytes`, and dimensions
are **real labels**, not name segments.

**Label vs. separate metric.** A dimension becomes a label only when it
passes the guide's rule of thumb: *"either the `sum()` or the `avg()`
over all dimensions of a given metric should be meaningful; if not, split
into multiple metrics."* So `status`, `direction`, `reason`, and
`subscription` are labels (summing across their values yields a real
total: all requests, all bytes, all closes), but **opened vs. closed are
separate metrics, not a `state` label**: `opened + closed` is not a
meaningful total, and the quantity you actually want, `active`, is their
*difference*. The same test is why read/write fold into `direction` but
`opened_total`/`closed_total` do not fold into each other.

#### Connection metrics

| Metric                               | Type       | Labels      | Meaning                                  |
| ------------------------------------ | ---------- | ----------- | ---------------------------------------- |
| `rpc_ws_connections_active`          | Gauge      | —           | currently open WS connections            |
| `rpc_ws_connections_opened_total`    | Counter    | —           | cumulative connections accepted          |
| `rpc_ws_connections_closed_total`    | CounterVec | `reason`    | cumulative closes, bucketed by cause     |
| `rpc_ws_connection_duration_seconds` | Histogram  | —           | connection lifetime (seconds)            |
| `rpc_ws_bytes_total`                 | CounterVec | `direction` | bytes transferred (`read`/`write`)       |
| `rpc_ws_messages_total`              | CounterVec | `direction` | JSON frames transferred (`read`/`write`) |

Two consolidations the labels enable: the close counter carries `reason`
directly (so `sum(rpc_ws_connections_closed_total)` is the total and
`by (reason)` breaks it down, with no separate close-reason metric), and
read/write collapse into one `bytes`/`messages` family keyed by
`direction` instead of four metrics.

`connections_active` is redundant with `opened - closed` but is cheap,
avoids relying on counter resets, and is the single most useful
at-a-glance value.

**Why `rpc_ws_` and not `rpc_connections_active{transport="ws"}`.** The
`transport` label belongs on the *request* metrics because requests are
a cross-transport phenomenon you compare and sum across (`sum(...)` is
total RPC load). Connection-level metrics are not: HTTP RPC never holds
a tracked connection (it goes through `serveSingleRequest`, not
`ServeCodec`, so it has nothing in `s.codecs` to count), and a
`transport` label would leave the `http` series permanently zero,
implying a comparison that does not exist and adding churn without
information. By the same rule of thumb (summing across label values
should be meaningful), a dimension with one real value belongs in the
name. The same reasoning applies to bytes, messages, subscriptions, and
throttling, all measured only on the WS path. (IPC also flows through
`ServeCodec`, but it is a single low-churn admin socket; scoping by name
to `ws` and revisiting if IPC visibility is ever wanted is deliberate.)

#### Request and subscription volume

WS request *rate* needs no bespoke counter: the migrated
`rpc_requests_total` carries a `transport` label (below), so WS load is
`rpc_requests_total{transport="ws"}`. What that counter cannot express is
*per-connection distribution*, so add two per-connection histograms,
observed once at connection close:

| Metric                            | Type      | Labels | Meaning                                                |
| --------------------------------- | --------- | ------ | ------------------------------------------------------ |
| `rpc_ws_connection_requests`      | Histogram | —      | requests handled per connection, over its lifetime     |
| `rpc_ws_connection_subscriptions` | Histogram | —      | subscriptions opened per connection, over its lifetime |

These turn a counter into a distribution: they reveal whether load is
spread across many light connections or concentrated in a few heavy
ones, and whether clients hold one subscription or hundreds. Both are
accumulated on a small per-connection counter and observed in
`untrackCodec`, alongside `rpc_ws_connection_duration_seconds`.

#### Close-reason categorization

`rpc_ws_connections_closed_total` carries a `reason` label whose values
come from the bounded set below.

The close cause is already available, it is just discarded today. The
server's read loop (`Client.read`, `client.go:728`) gets the terminating
error from `readBatch()` and forwards it to `readErr` without recording
why (`client.go:730-737`). Threading that error to the close site lets
us bucket every disconnect into a bounded `reason` set:

| `reason`      | How detected                                                                                                              | Graceful?                                           |
| ------------- | ------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------- |
| `normal`      | `*websocket.CloseError` code 1000 (`CloseNormalClosure`) / 1001 (`CloseGoingAway`), or `io.EOF`                           | yes — client sent a close frame                     |
| `dropped`     | `websocket.IsUnexpectedCloseError`, e.g. code 1006 (`CloseAbnormalClosure`) — TCP reset, killed client, network partition | no — connection went away without a close handshake |
| `timeout`     | read deadline exceeded after an unanswered keepalive ping (net timeout error)                                             | no — client stopped responding                      |
| `read_limit`  | message exceeded `wsDefaultReadLimit` (32 MiB, `websocket.go:52`); gorilla close code 1009                                | no — protocol/abuse                                 |
| `server_stop` | `Server.Stop` closed the codec (`server.go:188-198`)                                                                      | n/a — server-initiated                              |
| `error`       | any other read/decode error                                                                                               | no                                                  |

**Can we tell whether the client closed gracefully?** Yes. A graceful
close means the client sent a WebSocket close frame (codes 1000/1001) or
a clean EOF; gorilla returns these as a `*websocket.CloseError` we can
inspect. An ungraceful drop (TCP reset, process kill, network partition)
surfaces as `CloseAbnormalClosure` (1006) or a pong timeout, both
distinguished from the graceful case by `websocket.IsUnexpectedCloseError`.

**Keepalive.** The server runs an *application-level* WebSocket
keepalive (not TCP `SO_KEEPALIVE`): `pingLoop` (`websocket.go:363`) sends
a ping frame every `wsPingInterval` (30s) while the connection is idle
and arms a `wsPongTimeout` (30s) read deadline (`websocket.go:49-51`,
`379-385`). If the pong does not arrive, the next read fails with a
timeout, surfaced above as `reason="timeout"`. This is what lets us
distinguish a silently dead client from one that explicitly disconnected.

### Per-subscription metrics

WebSocket clients open long-lived push subscriptions via
`*_subscribe` (`handler.go:634`). Each subscription is identified by a
`(namespace, name)` pair, e.g. `eth`/`newHeads`, `eth`/`logs`,
`eth`/`newPendingTransactions` (`handler.go:645-646`). This set is
**statically registered** in the service registry, so it is bounded and
safe to use as a metric label.

Add the following, labeled by `subscription` (the `<namespace>_<name>`
kind). Notification successes and failures share one counter via a
`status` label:

| Metric                                     | Type       | Labels                   | Meaning                             |
| ------------------------------------------ | ---------- | ------------------------ | ----------------------------------- |
| `rpc_ws_subscriptions_active`              | GaugeVec   | `subscription`           | live subscriptions of this kind     |
| `rpc_ws_subscriptions_opened_total`        | CounterVec | `subscription`           | subscriptions created of this kind  |
| `rpc_ws_subscriptions_closed_total`        | CounterVec | `subscription`           | subscriptions cancelled             |
| `rpc_ws_subscriptions_notifications_total` | CounterVec | `subscription`, `status` | notifications pushed (`ok`/`error`) |

`rpc_ws_subscriptions_notifications_total` is the per-subscription
analogue of the `write` series of `rpc_ws_messages_total` and answers
"which subscription type is driving push volume"; `status="error"`
isolates send failures.

**Labels are real now.** Because these are native Prometheus vectors,
`subscription`, `reason`, `direction`, and `status` are genuine labels.
For example, `rpc_ws_subscriptions_active{subscription="eth_newHeads"}` and
`rpc_ws_connections_closed_total{reason="timeout"}`. PromQL
aggregates with `by (subscription)` / `by (reason)` directly; no
name-segment matching. The label sets are bounded (subscription kinds
and close reasons are single digits; `direction`/`status` are two each),
so there is no cardinality risk. The `subscription` value is the
statically-registered `(namespace, name)` kind, never a per-instance ID.

### Migrating the request metrics

Since we are plumbing a `Registerer` anyway, the legacy `rpc/*` request
metrics (`metrics.go:46-69`) move onto it with their dimensions promoted
to labels. They still cover all transports, but now distinguish them via
a `transport` label instead of being undifferentiated:

| Old (go-metrics)                 | New (Prometheus)                                        | Change                                                                        |
| -------------------------------- | ------------------------------------------------------- | ----------------------------------------------------------------------------- |
| `rpc/success` + `rpc/failure`    | `rpc_requests_total{status,transport}`                  | two metrics → one counter; status `success`/`failure`; new `transport` label  |
| `rpc/duration/{method}/{status}` | `rpc_request_duration_seconds{method,status,transport}` | path segments → labels; ns → base-unit seconds, real histogram (not summary)  |
| `rpc/duration/all`               | `sum(rpc_request_duration_seconds_sum)`                 | dropped; recover from the histogram's `_sum` (with `_count` for a mean)       |
| `rpc/requests` (monotonic total) | `sum(rpc_requests_total)`                               | dropped; it equals success + failure, so the status counter already covers it |

The `transport` label (`ws`/`http`/`ipc`) comes from `PeerInfo.Transport`
(`server.go:223-226`), which the handler already has via its codec. It is
what lets `rpc_requests_total{transport="ws"}` answer "how much request
load is WebSocket," replacing what would otherwise be a separate
WS-only counter. Three values, so no cardinality concern; `method` was
already the dominant dimension and is unchanged.

This is a **breaking change**: dashboards or alerts on `rpc_success`,
`rpc_failure`, or the per-method `rpc_duration_*` summaries must be
repointed. In practice we expect this to touch **very few queries**:
these series are per-method and were only ever exposed as go-metrics
summaries with baked-in names, so they are awkward to query and not
widely dashboarded today; the high-value request signal most consumers
actually use is the aggregate, which `sum(rpc_requests_total)` and
`histogram_quantile(…, rpc_request_duration_seconds_bucket)` reproduce.
We will grep the known dashboard/alert repos before merging and list any
hits in the PR.

### Instrumentation points

| Concern            | Location                                                                                | Mechanism                                                                                                                                           |
| ------------------ | --------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| active / opened    | `trackCodec` (`server.go:138`)                                                          | inc `connections_active` + `connections_opened_total` on successful track                                                                           |
| closed / duration  | `untrackCodec` (`server.go:149`)                                                        | dec `connections_active`, inc `connections_closed_total{reason}`, observe duration; read the per-connection byte totals for the close log           |
| bytes / messages   | codec read & write paths (`websocket.go:346`)                                           | inc `rpc_ws_{bytes,messages}_total{direction}` after `ReadJSON` / write                                                                             |
| subscription open  | `addSubscriptions` (`handler.go:390`)                                                   | on non-nil `takeSubscription()` (actually registered), inc `subscriptions_active`+`subscriptions_opened_total{subscription}`                        |
| subscription close | `unsubscribe` (`handler.go:677`) **and** `cancelServerSubscriptions` (`handler.go:402`) | dec `subscriptions_active`, inc `subscriptions_closed_total{subscription}` on both explicit-unsubscribe and connection-teardown paths               |
| notifications      | `Notifier.Notify` (`subscription.go:143`)                                               | inc `subscriptions_notifications_total{subscription,status}`                                                                                        |
| requests           | request dispatch (`handler.go:619-623`)                                                 | inc `rpc_requests_total{status,transport}`, observe `rpc_request_duration_seconds{...}`; for WS codecs also bump the per-connection request counter |
| throttled          | `awaitLimit` (`handler.go:421`)                                                         | inc `rpc_ws_throttled_total` when the reservation `delay` is non-zero                                                                               |

Per-connection counts (requests handled, subscriptions opened, and
bytes read / written) are accumulated on a small per-connection counter
held alongside the codec, so the hot path only does cheap atomic
increments. In `untrackCodec` the request/subscription counts are
observed once into their histograms, and the byte totals are read back
out to populate the `read_bytes` / `write_bytes` fields of the
connection-closed log (see Logging). The byte increments are the same
ones that feed `rpc_ws_bytes_total{direction}`; holding a per-connection
copy is what lets the close log report each connection's totals (which
the aggregate counter cannot attribute to a single IP).

To measure lifetime, record a start timestamp when the codec is
tracked. Two options:

1. Store the start time in the `codecs` map (change
   `map[ServerCodec]struct{}` to `map[ServerCodec]time.Time`). Keeps
   all lifecycle state in the server; smallest blast radius.
2. Store the start time on the `websocketCodec` itself.

Option 1 is preferred: it keeps connection bookkeeping in the server
where `track`/`untrack` already live, and works uniformly for any
`ServerCodec`, not just WebSocket.

Byte counting requires visibility into the actual reader/writer. The
`websocketCodec` wraps a `*websocket.Conn` (`websocket.go:297`) and
uses `conn.WriteJSON` / `conn.ReadJSON` (`websocket.go:307-311`). The
cleanest approach is to wrap the encode/decode functions passed to
`NewFuncCodec` so the byte count is observed as JSON is marshalled to /
unmarshalled from the connection, rather than reaching into gorilla
internals.

### Gating connection metrics to WebSocket

`trackCodec`/`untrackCodec` operate on any `ServerCodec`. HTTP is
naturally excluded because it uses `serveSingleRequest` and never goes
through `ServeCodec` (`server.go:159`). **IPC is not** (it shares the
`ServeCodec` path), so the WS-scoped counters must guard on
`codec.peerInfo().Transport == "ws"` rather than firing for every tracked
codec; otherwise IPC connections would be counted under `rpc_ws_*`. (The
transport-agnostic *request* metrics need no such guard: they carry a
`transport` label and intentionally cover IPC too.)

### Logging

Metrics give aggregate trends; logs give per-event visibility for
incident response (which client connected when, what they subscribed
to). Today neither event is logged usefully:

| Event                          | Current state                                                                                                                                                                    | Desired                                                                 |
| ------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| WS connection opened (success) | **No log.** Only `log.Debug("WebSocket upgrade failed")` (`websocket.go:75`) on failure and `log.Warn("Rejected WebSocket connection")` (`websocket.go:120`) on origin rejection | `log.Info` with remote address, origin, user-agent                      |
| WS connection closed           | No log                                                                                                                                                                           | `log.Info` with remote address, connection duration, close `reason`, and per-connection bytes read / written |
| Subscription created           | **No dedicated log.** `eth_subscribe` only appears in the generic `log.Debug("Served "+msg.Method)` (`handler.go:582`), at `Info` only on error (`handler.go:580`)               | `log.Info` with subscription kind, ID, remote address                   |
| Subscription cancelled         | No log                                                                                                                                                                           | `log.Debug` with subscription kind and ID                               |

Changes:

- **Connection open:** add `log.Info("WebSocket connection opened", ...)`
  after a successful upgrade (`websocket.go:73-78`), carrying the
  `PeerInfo` already assembled in `newWebsocketCodec`
  (`websocket.go:315-323`): `RemoteAddr` (top-level) and `HTTP.Origin` /
  `HTTP.UserAgent` (the origin/user-agent fields live under the nested
  `PeerInfo.HTTP` struct, `server.go:231-239`).
- **Connection close:** add `log.Info("WebSocket connection closed", ...)`
  in `untrackCodec` (`server.go:149`), with remote address, the
  lifetime computed for `rpc_ws_connection_duration_seconds`, the
  close `reason` (see Close-reason categorization), and the
  per-connection bytes read from / written to this connection over its
  lifetime. The byte totals are emitted as discrete structured fields
  (`read_bytes` / `write_bytes`) rather than baked into the message
  string, alongside `RemoteAddr`, so an external consumer can parse and
  **aggregate them by client IP**: offline log tooling (Loki, ELK, `jq`)
  or a Grafana panel backed by Loki can `sum by (remote_addr)` over the
  parsed fields to attribute bandwidth per client, with no Prometheus
  cardinality cost. These are the same per-connection counts already
  accumulated for `rpc_ws_bytes_total` (see below); the close log simply
  also retains and reports the per-connection totals, which the
  aggregate counter cannot break down by IP.
- **Subscription created:** add a dedicated `log.Info("Subscription
  opened", "subscription", kind, "id", id, ...)` in `addSubscriptions`
  (`handler.go:390`), where `takeSubscription()` returns non-nil (i.e.
  the subscription was actually registered). Instrumenting at the
  `handleSubscribe` `Notifier` creation (`handler.go:660`) instead would
  overcount, since the callback can still error without creating a
  subscription. This is also preferred over promoting the generic
  "Served" line (`handler.go:582`) to Info, which would make *every*
  successful RPC call log at Info and flood the logs.
- **Subscription cancelled:** `log.Debug` on both teardown paths:
  explicit `unsubscribe` (`handler.go:677`) and connection-close
  `cancelServerSubscriptions` (`handler.go:402`). Unsubscribes are
  routine teardown, not worth an INFO line.

Connection open/close and subscription *creation* log at **INFO**;
subscription cancellation and the high-frequency per-notification path
stay at Debug/Trace and are covered by metrics instead, to avoid log
volume proportional to churn or push traffic.

### Existing throttling

The server already has one throttling mechanism, documented here so this
work is shaped to observe it, though **changing or retuning it is out of
scope** (see Future Work). It is a **per-connection, CPU-time**
token-bucket limiter (`handler.addLimiter`, `handler.go:193`), driven by
the `refillRate`/`maxStored` parameters threaded through
`WebsocketHandlerWithDuration` (`websocket.go:65`). Before each call
`awaitLimit` (`handler.go:415`) blocks until budget is available;
afterward `consumeLimit` (`handler.go:435`) refunds the unused slice. It
limits *processing time per connection per second*, not request or byte
volume, and per-connection rather than globally or per-client. It was
introduced in coreth in 2021 (commit `84a767ee7`, "[AV-956] WS rate
limit"; the `#476` in that message predates coreth's PR renumbering and
no longer resolves to this change).

It is configured through three EVM chain-config knobs (coreth and
subnet-evm), all **disabled by default** (`0`):

| Config key (JSON)    | Field             | Meaning                                                                      |
| -------------------- | ----------------- | ---------------------------------------------------------------------------- |
| `api-max-duration`   | `APIMaxDuration`  | per-call wall-clock cap; also the per-call cost reserved against the limiter |
| `ws-cpu-refill-rate` | `WSCPURefillRate` | CPU-time budget refilled per second per connection                           |
| `ws-cpu-max-stored`  | `WSCPUMaxStored`  | max CPU-time budget that can accumulate (burst)                              |

The limiter is installed only when `ws-cpu-refill-rate > 0`,
`api-max-duration > 0`, and `ws-cpu-max-stored >= api-max-duration`
(`handler.go:194`); otherwise it is a no-op, so out of the box there is
no WS CPU limiting at all.

**It is active in production.** Both the WebSocket-serving and RPC C-Chain
node configs run identical, non-default values:

```json
"api-max-duration":   "10s",
"ws-cpu-refill-rate": "75ms",
"ws-cpu-max-stored":  "10s"
```

These satisfy the install condition, so the limiter is live: each
connection sustains ~75ms of processing time per wall-clock second
(~7.5% of one core), may burst against up to 10s of accumulated budget,
and any single call is capped at 10s. This makes the visibility gap
concrete: the limiter *is* throttling real traffic today, yet nothing
reports how often, which is exactly what `rpc_ws_throttled_total` fixes.

#### What this limit does and does not protect

It is worth being clear-eyed about how weak a safeguard this is, because
it shapes why visibility (not just the limiter) matters.

*How many misbehaving clients would it take?* At 75ms/s, each connection
can sustain ~7.5% of a single core. So roughly **13 busy connections
saturate one core** (`1s ÷ 75ms ≈ 13.3`), and an *M*-core node needs
only about `13 × M` connections to peg all cores at the sustained rate
(e.g. ~107 on an 8-core box, ~213 on a 16-core box). During the initial
burst each connection can additionally spend up to 10s of accumulated
budget at once, so a smaller number can spike CPU hard for the first
several seconds. Critically, **the limiter is per-connection and there
is no per-IP or global cap**, so a single client can simply open many
connections; "clients" effectively equals "connections," and the
per-connection budget does not bound aggregate load at all.

*And CPU is unlikely to be the binding resource.* Three gaps:

- **It meters wall-clock processing time, not resource intensity.** A
  request that is cheap in time but heavy on disk (random-access state
  reads behind `eth_getLogs`, archive queries, `debug_trace*`), memory
  (large result sets), or lock contention consumes little budget yet can
  saturate disk I/O, thrash the page cache, drive GC, or block shared
  locks for every other caller.
- **Subscription push bypasses the limiter entirely.** `Notifier.send`
  writes directly via `conn.writeJSON` (`subscription.go`); only the
  *incoming-call* path goes through `awaitLimit`/`consumeLimit`
  (`handler.go:466`,`474`). A client subscribed to high-volume streams
  generates ongoing server-side fan-out that the CPU budget never sees.
- **Per-connection overhead scales with connection count regardless of
  budget**: file descriptors, goroutines, ping/pong timers, and read
  buffers all grow with the number of open connections, independent of
  how much CPU each is allowed.

The takeaway: **disk I/O, memory, file descriptors, and lock contention
will commonly distress a node well before this CPU budget binds.** The
limiter is a coarse backstop against one failure mode, not a
comprehensive safeguard. This is precisely why this design invests in
*seeing* connection counts, throttle events, and per-connection
request/subscription volume: when a node is distressed you need to tell
which resource is actually under pressure, and the CPU limiter alone
will neither tell you nor necessarily prevent it.

Today this limiter is completely invisible: nothing reports whether it
ever engages, so operators have no way to know if a configured limit is
biting or how often. This design adds one cheap, high-value counter
**in scope**:

| Metric                   | Type    | Meaning                                                    |
| ------------------------ | ------- | ---------------------------------------------------------- |
| `rpc_ws_throttled_total` | Counter | calls the CPU limiter delayed (a connection was throttled) |

It is incremented in `awaitLimit` whenever the reservation's computed
`delay` is non-zero (`handler.go:421`), i.e. the call actually had to
wait; it is a no-op when no limiter is configured. Finer enforcement
metrics (how *long* calls waited, hard rejections) are deferred to the
throttle work that would consume them (see Future Work).

**Per-client analysis via logs.** These INFO logs are deliberately the
per-client dimension that the metrics intentionally omit. Because each
connection-open record carries `RemoteAddr`/`Origin`/`UserAgent`, each
connection-close record carries `RemoteAddr` plus the per-connection
`read_bytes`/`write_bytes` totals, and each subscription record carries
the client address and subscription kind, offline log aggregation (Loki,
ELK, `jq` over rotated logs) can answer per-IP / per-client questions
(top connecting IPs, repeat reconnect offenders, **bytes consumed per
client IP**, who subscribed to a given stream) without adding unbounded
label cardinality to Prometheus. One INFO line per connect /
subscribe keeps the volume bounded by connection churn, not by request
or notification rate.

## Alternatives Considered

**Stay on go-metrics and encode dimensions as name segments** (e.g.
`rpc_ws_subscriptions_eth_newHeads_opened_total`,
`rpc_ws_closed_timeout_total`). This is what the existing
`rpc/duration/{method}/{status}` metric does and it keeps the change
contained to the rpc package with no new dependency. Rejected: it
produces metric *names* that vary by dimension (awkward to query,
impossible to `by (label)`), and it would mean shipping new metrics in a
pattern we already know is wrong. The whole point of plumbing a
`Registerer` is to escape this; doing the plumbing but keeping name
segments would be the worst of both. The cost we accept instead is the
`client_golang/prometheus` dependency in the graft (see Metrics
backend).

**Use `NewLabelGatherer` to add labels instead of native vectors.**
`api/metrics/label_gatherer.go` can append a label to a whole gatherer,
but only one constant value per gatherer. To get `reason` you would
register one gatherer per reason value and partition the counter across
them. Clumsy and can't `inc(reason)` on a single metric. Rejected in
favor of a plain `CounterVec`.

**Label subscriptions by remote address or subscription ID instead of
kind.** Either produces unbounded cardinality (one series per client or
per live subscription). Aggregating by the statically-registered
`(namespace, name)` kind keeps the series count in the single digits.
Rejected.

**Derive connection counts externally from access logs.** Log scraping
is lossy, high-latency, and not available in all deployments. A gauge
sourced from the in-memory `codecs` map is exact and free. Rejected.

**Expose `len(s.codecs)` on demand via an RPC method instead of a
metric.** Doesn't integrate with the existing Prometheus scrape
pipeline and gives no history. Rejected.

**Per-IP / per-origin labels.** Now technically easy (real labels), but
unbounded cardinality makes it a bad metric regardless of backend.
Deferred to logs; the `Origin`/`RemoteAddr` are already captured in
`PeerInfo` (`server.go:223`) for offline aggregation (see Logging).

## Implementation

Phase 0 lands the plumbing; later phases each add one metric group on
top and are independently shippable.

### Phase 0: Plumb the `Registerer` and metrics struct

1. Add a `prometheus.Registerer` parameter to `rpc.NewServer`; build an
   `*rpcMetrics` struct holding every vector and register them once.
   Share it with each `handler` (via `initClient`/`newHandler`). Define
   the struct and all vectors in a **new first-party file**
   (`ws_metrics.go`), not in the upstream-derived files, to minimize
   merge churn (see Metrics backend).
2. Pass a registerer from coreth and subnet-evm where the handler is
   constructed (`vm.go` ~1030/1068), under an `rpc` prefix on the VM's
   registry.
3. No metrics emitted yet: just the wiring, so the diff that adds the
   dependency is small and reviewable on its own.

### Phase 1: Migrate the request metrics

1. Define `rpc_requests_total{status,transport}` and
   `rpc_request_duration_seconds{method,status,transport}` on
   `rpcMetrics`; delete the go-metrics `rpc/success`, `rpc/failure`,
   `rpc/duration/*`, and `rpc/requests` registrations in `metrics.go`
   (`rpc/requests` was just success + failure, now `sum(rpc_requests_total)`).
2. Emit them where the gauges are incremented today
   (`handler.go:619-623`, `updateServeTimeHistogram`), reading
   `transport` from the codec's `PeerInfo.Transport`.
3. Grep dashboards/alerts for the old names; record hits in the PR.
4. Test status/method labelling for success and error responses.

### Phase 2: Connection lifecycle metrics

1. Add `rpc_ws_connections_active`, `_opened_total`,
   `_closed_total{reason}` (reason filled in Phase 5), and
   `rpc_ws_connection_duration_seconds`.
2. Change `codecs` to `map[ServerCodec]time.Time` and record the start
   time in `trackCodec`; emit the lifecycle metrics in
   `track`/`untrackCodec`.
3. Unit test open/close accounting via `ServeCodec` with a fake codec.

### Phase 3: Byte / message and request volume

1. Add `rpc_ws_bytes_total{direction}`,
   `rpc_ws_messages_total{direction}`, and the per-connection histograms
   `rpc_ws_connection_{requests,subscriptions}`. (WS request *rate* is
   already covered by `rpc_requests_total{transport="ws"}` from Phase 1;
   no separate counter.)
2. Wrap the encode/decode funcs in `newWebsocketCodec` to count bytes and
   messages; bump the per-connection request counter in
   `handler.handleCallMsg` (`handler.go:553`); accumulate per-connection
   request/subscription counts and observe their histograms in
   `untrackCodec`. (Per-connection byte *retention* and the
   connection-closed log move to Phase 7; the byte/message *counters* are
   global and land here.)
3. Test with a real in-process WebSocket connection (the pattern in
   `websocket_test.go`).

### Phase 4: Per-subscription metrics

1. Add `rpc_ws_subscriptions_active{subscription}`,
   `_opened_total{subscription}`, `_closed_total{subscription}`, and
   `rpc_ws_subscriptions_notifications_total{subscription,status}`.
2. Emit opened at `addSubscriptions` (`handler.go:390`) on non-nil
   `takeSubscription()`; closed at **both** `unsubscribe`
   (`handler.go:677`) and `cancelServerSubscriptions` (`handler.go:402`,
   connection teardown) so the active gauge does not leak; notifications
   at `Notifier.Notify` (`subscription.go:143`). All keyed by the
   `(namespace, name)` kind.
3. Test accounting for explicit unsubscribe **and** connection-close
   teardown, and that a failed subscribe attempt does not increment
   opened.

### Phase 5: Close-reason label

1. Thread the terminating error from `Client.read` (`client.go:730-737`)
   to the close site instead of discarding it.
2. Classify it into the bounded `reason` set (`normal`, `dropped`,
   `timeout`, `read_limit`, `server_stop`, `error`) using
   `websocket.IsCloseError` / `IsUnexpectedCloseError` and net-timeout
   checks.
3. Set the `reason` label on `rpc_ws_connections_closed_total`, and
   include `reason` in the connection-closed INFO log.

### Phase 6: Throttle counter

1. Add `rpc_ws_throttled_total`.
2. Increment it in `awaitLimit` (`handler.go:421`) when the reservation's
   `delay` is non-zero, before blocking; no-op when no limiter is set.
3. Test that a tightly-configured limiter increments the counter while an
   unconfigured one leaves it at zero.

### Phase 7: Connection and subscription logging

Logging is a separate concern from the metric phases (see the Logging
section), so it lands last, once all the metrics — including the Phase 3
byte counters and the Phase 5 close `reason` — exist.

1. Add INFO logs on connection open (after the WebSocket upgrade) and
   connection close (`untrackCodec`), and on subscription open
   (`addSubscriptions`); subscription cancel stays at Debug.
2. For the connection-close log, retain the per-connection `read_bytes` /
   `write_bytes` totals — accumulated on the same per-connection counters
   introduced in Phase 3 for `rpc_ws_bytes_total` — and emit them as
   structured fields alongside `RemoteAddr`, the connection duration, and
   the close `reason` (Phase 5), so bandwidth can be aggregated by client
   IP externally.
3. Test that open/close/subscribe each log once at INFO with the expected
   fields.

## Future Work

### WS traffic throttling (out of scope)

A future change may retune the existing per-connection CPU limiter (see
Existing throttling) or add a new cap on WebSocket traffic at a
configurable rate per minute/hour. Building or retuning a throttle is
**not** part of this design; it is recorded here so the visibility added
now is shaped to support it later.

How this design helps tune it: the in-scope `rpc_ws_throttled_total`
counter already shows how often the current limiter engages, and
`rpc_requests_total{transport="ws"}` plus `rpc_ws_bytes_total` are the
rate signal a volume cap would be sized against:
`rate(rpc_requests_total{transport="ws"}[1m])` and the byte equivalent
give the per-minute/hour traffic an operator would tune a cap from.

What a richer throttle would still add (deferred): finer enforcement
metrics beyond the simple `rpc_ws_throttled_total` count:

| Metric                          | Type      | Meaning                                         |
| ------------------------------- | --------- | ----------------------------------------------- |
| `rpc_ws_throttle_delay_seconds` | Histogram | time a request was blocked waiting on the limit |
| `rpc_ws_throttle_limited_total` | Counter   | requests rejected by a hard cap (vs. delayed)   |

These show not just *how often* but *how hard* throttling bites; they
should ship with the throttle work that consumes them, not before it.

## References

- `graft/evm/rpc/websocket.go` — WebSocket handler and codec
- `graft/evm/rpc/server.go` — `ServeCodec`, `trackCodec`, `untrackCodec`
- `graft/evm/rpc/subscription.go` — `Notifier`, `Subscription`
- `graft/evm/rpc/handler.go` — `handleSubscribe`, `unsubscribe`
- `graft/evm/rpc/metrics.go` — existing request metrics being migrated
- `api/server/metrics.go` — existing native-Prometheus `CounterVec`/`GaugeVec` precedent (labeled by `base`)
- `vms/evm/metrics/prometheus/prometheus.go` — the go-metrics→Prometheus bridge (unlabeled, name-based) this design moves off of
- `graft/coreth/plugin/evm/vm.go` — VM-owned registries (`sdkMetrics`, `ctx.Metrics.Register`) and handler construction
- [go-ethereum WebSocket RPC](https://github.com/ethereum/go-ethereum/blob/master/rpc/websocket.go) — upstream origin of this code
- [Prometheus metric naming conventions](https://prometheus.io/docs/practices/naming/)
