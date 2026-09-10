# How to trace AvalancheGo

AvalancheGo can export [OpenTelemetry](https://opentelemetry.io/) traces with spans covering message routing, consensus, and VM operations. Tracing is disabled by default. This guide visualizes traces locally with [Jaeger](https://www.jaegertracing.io/).

## 1. Start Jaeger

```sh
docker run --rm \
  --name jaeger \
  -p 16686:16686 \
  -p 4317:4317 \
  -p 4318:4318 \
  jaegertracing/jaeger:2.20.0@sha256:46a886260e04002d8f45e213fc39063fa11a50446048fdaa64786fc0840cb9f8
```

Jaeger stores traces in memory. Traces are lost when the container stops.

## 2. Run AvalancheGo with tracing enabled

```sh
./build/avalanchego --tracing-exporter-type=grpc --tracing-sample-rate=1
```

The default export endpoint is `localhost:4317`, which matches the Jaeger container above. The default sample rate is 0.1, which is too sparse for interactive debugging, so this samples everything.

## 3. View traces

Open <http://localhost:16686>, select the `avalanchego` service, and click Find Traces.

## OpenTelemetry environment variables

Tracing is enabled with the `--tracing-exporter-type` flag (or its
`AVAGO_TRACING_EXPORTER_TYPE` form). When the flag isn't explicitly set, the
standard `OTEL_TRACES_EXPORTER` variable is honored instead: `otlp` enables
tracing (with the transport from `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL` or
`OTEL_EXPORTER_OTLP_PROTOCOL`, defaulting to `http/protobuf`) and `none`
disables it. If neither the flag nor `OTEL_TRACES_EXPORTER` is set, tracing
stays disabled.

Once enabled, the standard
[OTel SDK](https://opentelemetry.io/docs/languages/sdk-configuration/) and
[OTLP exporter](https://opentelemetry.io/docs/languages/sdk-configuration/otlp-exporter/)
environment variables are honored, e.g. `OTEL_EXPORTER_OTLP_ENDPOINT`,
`OTEL_EXPORTER_OTLP_HEADERS`, `OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES`,
`OTEL_TRACES_SAMPLER` and `OTEL_BSP_*`.

Where a `--tracing-*` flag configures the same thing, the flag takes
precedence, with two exceptions: `OTEL_SERVICE_NAME`/`OTEL_RESOURCE_ATTRIBUTES`
override the default resource, and `OTEL_TRACES_SAMPLER` (with
`OTEL_TRACES_SAMPLER_ARG`) takes precedence over `--tracing-sample-rate`, since
setting either is an explicit opt-in.
