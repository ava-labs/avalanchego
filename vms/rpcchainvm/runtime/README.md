# Virtual Machine Runtime Engine (VMRE)

The `VMRE` handles the lifecycle, compatibility and logging IO of a managed VM process.

## How it works

The `runtime.Initializer` interface could be implemented to manage local or remote VM processes.
This implementation is consumed by a gRPC server which serves the `Runtime`
service. The server interacts with the underlying process and allows for the VM
binary to communicate with AvalancheGo.

### Subprocess VM management

The `subprocess` is currently the only supported `Runtime` implementation.
It works by starting the VM's as a subprocess of AvalancheGo by `os.Exec`.

## Workflow

- `VMRegistry` calls the RPC Chain VM `Factory`.
- Factory Starts an instance of a `VMRE` server that consumes a `runtime.Initializer` interface implementation.
- The address of this server is passed as a ENV variable `AVALANCHE_VM_RUNTIME_ENGINE_ADDR` via `os.Exec` which starts the VM binary.
- The VM uses the address of the `VMRE` server to create a client.
- Client sends a `Initialize` RPC informing the server of the `Protocol Version` and future `Address` of the RPC Chain VM server allowing it to perform a validation `Handshake`.
- After the `Handshake` is complete the RPC Chain VM server is started which serves the `ChainVM` implementation.
- The connection details for the RPC Chain VM server are now used to create an RPC Chain VM client.
- `ChainManager` uses this VM client to bootstrap the chain powered by `Snowman` consensus.
- To shutdown the VM `runtime.Stop()` sends a `SIGTERM` signal to the VM process.

## Debugging

### Process Not Found

When runtime is `Bootstrapped` handshake success is observed during the `Initialize` RPC. Process not found means that the runtime Client in the VM binary could not communicate with the runtime Server on AvalancheGo. This could be the result of networking issues or other error in `Serve()`.

```bash
failed to register VM {"vmID": "tGas3T58KzdjcJ2iKSyiYsWiqYctRXaPTqBCA11BqEkNg8kPc", "error": "handshake failed: timeout"}
```

### Protocol Version Mismatch

To ensure RPC compatibility the protocol version of AvalancheGo must match the subnet VM. To correct this error update the subnet VM's dependencies to the latest version AvalancheGo.

```bash
failed to register VM {"vmID": "tGas3T58KzdjcJ2iKSyiYsWiqYctRXaPTqBCA11BqEkNg8kPc", "error": "handshake failed: protocol version mismatch avalanchego: 19 vm: 18"}
```
