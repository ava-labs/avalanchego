# Subnet-EVM SAE Port Context (historical)

The detailed handover context that used to live here described the original
spike's design (persisted hook artifacts, `MarkSynchronous`, the pre-master
hook shapes) and no longer matches the shipped code.

Current sources of truth:

- [`README.md`](README.md) — design decisions, feature surface, deferred work.
- [`PORT_STATUS.md`](PORT_STATUS.md) — the port log: milestones, the hook
  reconciliation decisions (D1-D8, including the header-encoded ACP-224 gas
  config that replaced the spike's persisted artifacts), the duplication-audit
  record, and validation results.

Both PORT files are working documents for the port itself and are expected to
be folded into the README / PR description before merge.
