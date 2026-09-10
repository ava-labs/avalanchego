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
