The Metrics API allows clients to get statistics about a node's health and performance.

<Callout title="Note">
This API set is for a specific node, it is unavailable on the [public server](https://build.avax.network/docs/tooling/rpc-providers).
</Callout>

## Endpoint

```
/ext/metrics
```

## Usage

To get the node metrics:

```sh
curl -X POST 127.0.0.1:9650/ext/metrics
```

## Format

This API produces Prometheus compatible metrics. See [here](https://prometheus.io/docs/instrumenting/exposition_formats) for information on Prometheus' formatting.

[Here](https://build.avax.network/docs/nodes/maintain/monitoring) is a tutorial that shows how to set up Prometheus and Grafana to monitor AvalancheGo node using the Metrics API.
