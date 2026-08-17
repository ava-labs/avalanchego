# Firewood Label Taxonomy

Labels are managed as code in [`.github/labels.yml`](.github/labels.yml) and
synced by the [`Sync labels`](.github/workflows/sync-labels.yaml) workflow. Do
**not** create or rename labels in the GitHub UI — edit the manifest instead.

GitHub **issue types** (Bug, Feature, Task) answer *"what is this?"*. Labels are
orthogonal and grouped into four namespaces plus a few flags.

## `area/*` — where in the system

`storage`, `revisions`, `hashing`, `proofs`, `ffi`, `cli`, `c-chain`,
`benchmark`, `ci`, `docs`. Multiple allowed.

## `kind/*` — the concern

`correctness`, `performance`, `security`, `tech-debt`, `observability`,
`testing`, `dependencies`, `research`. Multiple allowed.

## `priority/*` — how urgent (one per item; absence = untriaged)

| Label | Meaning |
| :--- | :--- |
| `priority/P0` | Critical — data loss/corruption, broken `main`. Drop everything. |
| `priority/P1` | High — needed for next release / blocks users. |
| `priority/P2` | Medium — should do, not urgent. |
| `priority/P3` | Low — nice to have / someday. |

## `status/*` — triage/workflow state

`needs-triage` (auto at intake), `needs-info`, `blocked`, `ready`, `wontfix`,
`do-not-merge` (PR gate; may coexist with another status).

## Flags

`good first issue`, `help wanted`, `no-changelog`, `breaking-change`,
`3rd party contributor`.

## How labels get applied

- **Issues**: the intake form sets the issue type and `status/needs-triage`.
  A maintainer adds `priority/*` (which auto-swaps to `status/ready`), plus
  `area/*`/`kind/*` as useful.
- **PRs**: `area/*` is auto-applied from changed paths; `kind/*` is derived from
  the conventional-commit title. `area/c-chain` is applied manually at review.
