The Admin API can be used for measuring node health and debugging.

<Callout title="Note">
The Admin API is disabled by default for security reasons. To run a node with the Admin API enabled, use [`config flag --api-admin-enabled=true`](https://build.avax.network/docs/nodes/configure/configs-flags#--api-admin-enabled-boolean).

This API set is for a specific node, it is unavailable on the [public server](https://build.avax.network/docs/tooling/rpc-providers).
</Callout>

## Format

This API uses the `json 2.0` RPC format. For details, see [here](https://build.avax.network/docs/api-reference/guides/issuing-api-calls).

## Endpoint

```
/ext/admin
```

## Methods

### `admin.alias`

Assign an API endpoint an alias, a different endpoint for the API. The original endpoint will still work. This change only affects this node; other nodes will not know about this alias.

**Signature**:

```
admin.alias({endpoint:string, alias:string}) -> {}
```

- `endpoint` is the original endpoint of the API. `endpoint` should only include the part of the endpoint after `/ext/`.
- The API being aliased can now be called at `ext/alias`.
- `alias` can be at most 512 characters.

**Example Call**:

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"admin.alias",
    "params": {
        "alias":"myAlias",
        "endpoint":"bc/X"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/admin
```

**Example Response**:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {}
}
```

Now, calls to the X-Chain can be made to either `/ext/bc/X` or, equivalently, to `/ext/myAlias`.

### `admin.aliasChain`

Give a blockchain an alias, a different name that can be used any place the blockchain's ID is used.

<Callout title="Note">
Aliasing a chain can also be done via the [Node API](https://build.avax.network/docs/nodes/configure/configs-flags#--chain-aliases-file-string).

Note that the alias is set for each chain on each node individually. In a multi-node Avalanche L1, the same alias should be configured on each node to use an alias across an Avalanche L1 successfully. Setting an alias for a chain on one node does not register that alias with other nodes automatically.
</Callout>

**Signature**:

```
admin.aliasChain(
    {
        chain:string,
        alias:string
    }
) -> {}
```

- `chain` is the blockchain's ID.
- `alias` can now be used in place of the blockchain's ID (in API endpoints, for example.)

**Example Call**:

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"admin.aliasChain",
    "params": {
        "chain":"sV6o671RtkGBcno1FiaDbVcFv2sG5aVXMZYzKdP4VQAWmJQnM",
        "alias":"myBlockchainAlias"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/admin
```

**Example Response**:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {}
}
```

Now, instead of interacting with the blockchain whose ID is `sV6o671RtkGBcno1FiaDbVcFv2sG5aVXMZYzKdP4VQAWmJQnM` by making API calls to `/ext/bc/sV6o671RtkGBcno1FiaDbVcFv2sG5aVXMZYzKdP4VQAWmJQnM`, one can also make calls to `ext/bc/myBlockchainAlias`.

### `admin.getChainAliases`

Returns the aliases of the chain

**Signature**:

```
admin.getChainAliases(
  {
    chain:string
  }
) -> {aliases:string[]}
```

- `chain` is the blockchain's ID.

**Example Call**:

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"admin.getChainAliases",
    "params": {
        "chain":"sV6o671RtkGBcno1FiaDbVcFv2sG5aVXMZYzKdP4VQAWmJQnM"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/admin
```

**Example Response**:

```json
{
  "jsonrpc": "2.0",
  "result": {
    "aliases": [
      "X",
      "avm",
      "2eNy1mUFdmaxXNj1eQHUe7Np4gju9sJsEtWQ4MX3ToiNKuADed"
    ]
  },
  "id": 1
}
```

### `admin.getLoggerLevel`

Returns log and display levels of loggers.

**Signature**:

```
admin.getLoggerLevel(
  {
    loggerName:string // optional
  }
) -> {
        loggerLevels: {
          loggerName: {
            logLevel: string,
            displayLevel: string
          }
        }
    }
```

- `loggerName` is the name of the logger to be returned. This is an optional argument. If not specified, it returns all possible loggers.

**Example Call**:

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"admin.getLoggerLevel",
    "params": {
        "loggerName": "C"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/admin
```

**Example Response**:

```json
{
  "jsonrpc": "2.0",
  "result": {
    "loggerLevels": {
      "C": {
        "logLevel": "DEBUG",
        "displayLevel": "INFO"
      }
    }
  },
  "id": 1
}
```

### `admin.loadVMs`

Dynamically loads any virtual machines installed on the node as plugins. See [here](https://build.avax.network/docs/virtual-machines#installing-a-vm) for more information on how to install a virtual machine on a node.

**Signature**:

```
admin.loadVMs() -> {
  newVMs: map[string][]string
  failedVMs: map[string]string,
}
```

- `failedVMs` is only included in the response if at least one virtual machine fails to be loaded.

**Example Call**:

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"admin.loadVMs",
    "params" :{}
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/admin
```

**Example Response**:

```json
{
  "jsonrpc": "2.0",
  "result": {
    "newVMs": {
      "tGas3T58KzdjLHhBDMnH2TvrddhqTji5iZAMZ3RXs2NLpSnhH": ["foovm"]
    },
    "failedVMs": {
      "rXJsCSEYXg2TehWxCEEGj6JU2PWKTkd6cBdNLjoe2SpsKD9cy": "error message"
    }
  },
  "id": 1
}
```

### `admin.lockProfile`

Writes a profile of mutex statistics to `lock.profile`.

**Signature**:

```
admin.lockProfile() -> {}
```

**Example Call**:

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"admin.lockProfile",
    "params" :{}
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/admin
```

**Example Response**:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {}
}
```

### `admin.memoryProfile`

Writes a memory profile of the to `mem.profile`.

**Signature**:

```
admin.memoryProfile() -> {}
```

**Example Call**:

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"admin.memoryProfile",
    "params" :{}
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/admin
```

**Example Response**:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {}
}
```

### `admin.setLoggerLevel`

Sets log and display levels of loggers.

**Signature**:

```
admin.setLoggerLevel(
  {
    loggerName: string, // optional
    logLevel: string, // optional
    displayLevel: string, // optional
  }
) -> {}
```

- `loggerName` is the logger's name to be changed. This is an optional parameter. If not specified, it changes all possible loggers.
- `logLevel` is the log level of written logs, can be omitted.
- `displayLevel` is the log level of displayed logs, can be omitted.

`logLevel` and `displayLevel` cannot be omitted at the same time.

**Example Call**:

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"admin.setLoggerLevel",
    "params": {
        "loggerName": "C",
        "logLevel": "DEBUG",
        "displayLevel": "INFO"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/admin
```

**Example Response**:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {}
}
```

### `admin.startCPUProfiler`

Start profiling the CPU utilization of the node. To stop, call `admin.stopCPUProfiler`. On stop, writes the profile to `cpu.profile`.

**Signature**:

```
admin.startCPUProfiler() -> {}
```

**Example Call**:

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"admin.startCPUProfiler",
    "params" :{}
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/admin
```

**Example Response**:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {}
}
```

### `admin.stopCPUProfiler`

Stop the CPU profile that was previously started.

**Signature**:

```
admin.stopCPUProfiler() -> {}
```

**Example Call**:

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"admin.stopCPUProfiler"
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/admin
```

**Example Response**:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {}
}
```
