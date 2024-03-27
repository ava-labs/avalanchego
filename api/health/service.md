---
tags: [AvalancheGo APIs]
description: This page is an overview of the Health API associated with AvalancheGo. This API can be used for measuring node health.
sidebar_label: Health API
pagination_label: Health API
---

# Health API

This API can be used for measuring node health.

:::info

This API set is for a specific node, it is unavailable on the [public server](/tooling/rpc-providers.md).

:::

## Filterable Health Checks

The health checks that are run by the node are filterable. You can specify which health checks
you want to see by using `tags` filters. Returned results will only include health checks that
match the specified tags and global
health checks like `network`, `database` etc.
When filtered, the returned results will not show the full node health,
but only a subset of filtered health checks.
This means the node can be still unhealthy in unfiltered checks, even if the returned results show that
the node is healthy.
AvalancheGo supports filtering tags by subnetIDs. For more information check Filtering sections below.

## GET Request

To get an HTTP status code response that indicates the node’s health, make a `GET` request to
`/ext/health`. If the node is healthy, it will return a `200` status code. If you want more in-depth
information about a node’s health, use the JSON RPC methods.

### Filtering

To filter GET health checks, add a `tag` query parameter to the request. The `tag` parameter is a
string.
To filter health results by subnetID, use the
`subnetID` tag. For example,
to filter health results by subnetID `29uVeLPJB1eQJkzRemU8g8wZDw5uJRqpab5U2mX9euieVwiEbL`,
use the following query:

```sh
curl --location --request GET 'http://localhost:9650/ext/health?tag=29uVeLPJB1eQJkzRemU8g8wZDw5uJRqpab5U2mX9euieVwiEbL' \
--header 'Content-Type: application/json' \
--data-raw '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"health.health",
}'
```

In this example returned results will contain global health checks and health checks that are
related to subnetID `29uVeLPJB1eQJkzRemU8g8wZDw5uJRqpab5U2mX9euieVwiEbL`.

**Note:** This filtering can show healthy results even if the node is unhealthy in other Chains/Subnets.

In order to filter results by multiple tags, use multiple `tag` query parameters. For example, to
filter health results by subnetID `29uVeLPJB1eQJkzRemU8g8wZDw5uJRqpab5U2mX9euieVwiEbL` and
`28nrH5T2BMvNrWecFcV3mfccjs6axM1TVyqe79MCv2Mhs8kxiY` use the following query:

```sh
curl --location --request GET 'http://localhost:9650/ext/health?tag=29uVeLPJB1eQJkzRemU8g8wZDw5uJRqpab5U2mX9euieVwiEbL&tag=28nrH5T2BMvNrWecFcV3mfccjs6axM1TVyqe79MCv2Mhs8kxiY' \
--header 'Content-Type: application/json' \
--data-raw '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"health.health",
}'
```

Returned results will contain checks for both subnetIDs and global health checks.

## JSON RPC Request

### Format

This API uses the `json 2.0` RPC format. For more information on making JSON RPC calls, see
[here](/reference/standards/guides/issuing-api-calls.md).

### Endpoint

```text
/ext/health
```

### Methods

#### `health.health`

The node runs a set of health checks every 30 seconds, including a health check for each chain. This
method returns the last set of health check results.

**Signature:**

```sh
health.health() -> {
    checks: []{
        checkName: {
            message: JSON,
            error: JSON,
            timestamp: string,
            duration: int,
            contiguousFailures: int,
            timeOfFirstFailure: int
        }
    },
    healthy: bool
}
```

`healthy` is true if the node if all health checks are passing.

`checks` is a list of health check responses.

- A check response may include a `message` with additional context.
- A check response may include an `error` describing why the check failed.
- `timestamp` is the timestamp of the last health check.
- `duration` is the execution duration of the last health check, in nanoseconds.
- `contiguousFailures` is the number of times in a row this check failed.
- `timeOfFirstFailure` is the time this check first failed.

More information on these measurements can be found in the documentation for the
[go-sundheit](https://github.com/AppsFlyer/go-sundheit) library.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"health.health"
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/health
```

**Example Response:**

In this example response, the C-Chain’s health check is failing.

```json
{
  "jsonrpc": "2.0",
  "result": {
    "checks": {
      "C": {
        "message": null,
        "error": {
          "message": "example error message"
        },
        "timestamp": "2020-10-14T14:04:20.57759662Z",
        "duration": 465253,
        "contiguousFailures": 50,
        "timeOfFirstFailure": "2020-10-14T13:16:10.576435413Z"
      },
      "P": {
        "message": {
          "percentConnected": 0.9967694992864075
        },
        "timestamp": "2020-10-14T14:04:08.668743851Z",
        "duration": 433363830,
        "contiguousFailures": 0,
        "timeOfFirstFailure": null
      },
      "X": {
        "timestamp": "2020-10-14T14:04:20.3962705Z",
        "duration": 1853,
        "contiguousFailures": 0,
        "timeOfFirstFailure": null
      },
      "chains.default.bootstrapped": {
        "timestamp": "2020-10-14T14:04:04.238623814Z",
        "duration": 8075,
        "contiguousFailures": 0,
        "timeOfFirstFailure": null
      },
      "network.validators.heartbeat": {
        "message": {
          "heartbeat": 1602684245
        },
        "timestamp": "2020-10-14T14:04:05.610007874Z",
        "duration": 6124,
        "contiguousFailures": 0,
        "timeOfFirstFailure": null
      }
    },
    "healthy": false
  },
  "id": 1
}
```

### Filtering

JSON RPC methods in Health API supports filtering by tags. In order to filter results use `tags`
params in the
request body. `tags` accepts a list of tags. Currently only `subnetID`s are supported as tags.
For example, to filter health results by subnetID `29uVeLPJB1eQJkzRemU8g8wZDw5uJRqpab5U2mX9euieVwiEbL`
use the following request:

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"health.health",
    "params":{
        "tags": ["29uVeLPJB1eQJkzRemU8g8wZDw5uJRqpab5U2mX9euieVwiEbL"]
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/health
```

Returned results will contain checks for subnetID `29uVeLPJB1eQJkzRemU8g8wZDw5uJRqpab5U2mX9euieVwiEbL`
and global health checks.
