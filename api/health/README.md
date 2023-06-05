# Health Checking

## Health Check Types

### Readiness

Readiness is a special type of health check. Readiness checks will only run until they pass for the first time. After a readiness check passes, it will never be run again. These checks are typically used to indicate that the startup of a component has finished.

### Health

Health checks typically indicate that a component is operating as expected. The health of a component may flip due to any arbitrary heuristic the component exposes.

### Liveness

Liveness checks are intended to indicate that a component has become unhealthy and has no way to recover.

## Naming and Tags

All registered checks must have a unique name which will be included in the health check results.

Additionally, checks can optionally specify an arbitrary number of tags which can be used to group health checks together.

### Special Tags

- "All" is a tag that is automatically added for every check that is registered.
- "Application" checks are checks that are globally applicable. This means that it is not possible to filter application-wide health checks from a response.

## Health Check Worker

Readiness, Health, and Liveness checks are all implemented by using their own health check worker.

A health check worker starts a goroutine that updates the health of all registered checks every `freq`. By default `freq` is set to `30s`.

When a health check is added it will always initially report as unhealthy.

Every health check runs in its own goroutine to maximize concurrency. It is guaranteed that no locks from the health checker are held during the execution of the health check.

When the health check worker is stopped, it will finish executing any currently running health checks and then terminate its primary goroutine. After the health check worker is stopped, the health checks will never run again.
