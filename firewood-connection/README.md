# firewood-connection

The firewood-connection library provides primitives to run firewood inside of an
async environment, for example a custom VM. firewood-connection provides a
series of channels that can be used to send `Actions` or closures from the async
environment to firewood in order to be executed. Firewood runs asynchronously on
its own thread, and messages are passed between the application and the firewood
library.

This solution is required because firewood currently depends on its own async
runtime in order to run its storage layer. Since nesting async runtimes is not
possible, firewood needs to run on its own dedicated thread. Ideally firewood
would support async directly, and this crate would not be necessary, and we are
working to add this support directly in the near future.

## Usage

Using the firewood-connection is straightforward. A call to `initialize()`
sets up the communication channel between async environments and returns a
`Manager`. Once a `Manager` is established, `Manger.call()` can pass
functions along the channel that will be executed on the database. Messages will
also be returned along the channel indicating the result of the database
operation.

## Example

See the [transfervm](https://github.com/ava-labs/transfervm-rs) for
an example of a custom VM that integrates with firewood-connection.
