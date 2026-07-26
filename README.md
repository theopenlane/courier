[![Go Report Card](https://goreportcard.com/badge/github.com/theopenlane/courier)](https://goreportcard.com/report/github.com/theopenlane/courier)
[![Build status](https://badge.buildkite.com/34ad31fe4231b2953cd3f2d116364d21a39b2a4dbf1eea539a.svg)](https://buildkite.com/theopenlane/courier?branch=main)
[![Go Reference](https://pkg.go.dev/badge/github.com/theopenlane/courier.svg)](https://pkg.go.dev/github.com/theopenlane/courier)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache2.0-brightgreen.svg)](https://opensource.org/licenses/Apache-2.0)
[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=theopenlane_courier&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=theopenlane_courier)

# courier

`courier` exports organization-owned controls and internal policies out
of Openlane into structured files, and applies file changes back through the
API. It is built for a git flow: the files live in a repository, changes
arrive as pull requests, and CI reconciles the merged state with Openlane.

Only user-manageable records are exported: controls owned by the organization
whose source is not `FRAMEWORK`, and organization-owned policies. Standards
and framework-derived controls are never written to files and never modified.
Apply never deletes anything — records that exist in Openlane but not in the
workspace are reported as drift.

## Workspace layout

```
controls.yaml           # control inventory
policies.yaml           # policy manifest
policies/*.md           # one markdown document per policy
```

`controls.yaml`:

```yaml
- id: CTL_01J...            # Openlane ULID, written back by pull after create
  refCode: CC1.1.3
  description: New hires are required to complete an acknowledgment form upon hire.
  category: Control Environment
  subcategory: Integrity and Ethics
  mappedControls:
    - CC1.1
```

`policies.yaml`:

```yaml
- id: PLC_01J...
  name: Application Security Policy
  policyType: Security
  markdownPath: policies/application-security-policy.md
  tags:
    - application
    - security
  mappedControls:
    - CC6.2
```

Policy markdown documents carry YAML frontmatter (title, tags) followed by the
policy body. On create the whole document is uploaded and the server parses
the frontmatter.

Controls are matched to Openlane by `id`, then by `refCode`; policies by `id`,
then by `name`. Entries without a match are created; `pull` writes the
assigned IDs back into the files.

`mappedControls` lists the refCodes of controls this record maps to, typically
framework controls cloned into the organization (e.g. `CC1.1`). RefCodes are
resolved case-insensitively against org-owned controls; a refCode that matches
nothing is skipped with a warning, so control inventories can be applied
before the referenced framework has been cloned. Additions create mappings
(confidence 80, source `IMPORTED`); removals are reported as drift, never
executed.

## Commands

| Command | Purpose |
|---|---|
| `pull` | Export controls, mappings, and policies into the workspace |
| `fmt` | Rewrite `controls.yaml` and `policies.yaml` into canonical form; `--check` fails instead |
| `plan` | Diff the workspace against Openlane; `--json`, `--detailed-exitcode` (exit 2 on changes) |
| `apply` | Create and update records from the workspace |

## Configuration

Settings merge in ascending precedence: config file, environment, flags.

`.courier.yaml` (or `--config path`):

```yaml
host: https://api.theopenlane.io
token: tolp_...
organizationID: org ULID          # only needed for multi-org tokens
dir: .                            # workspace directory
```

Environment: `COURIER_HOST`, `COURIER_TOKEN`, `COURIER_ORGANIZATION_ID`.
Flags: `--host`, `--token`, `--organization-id`, `--dir`.

## CI git flow

Main is protected, nothing pushes to it directly. Three workflows:

**Pull request** — validate and show the plan:

```yaml
steps:
  - uses: actions/checkout@v4
  - uses: theopenlane/setup-openlane@v1
    with:
      token: ${{ secrets.OPENLANE_TOKEN }}
  - run: courier fmt --check
  - run: courier plan
```

**Merge to main** — apply, then propose the ID write-back as a PR:

```yaml
steps:
  - uses: actions/checkout@v4
  - uses: theopenlane/setup-openlane@v1
    with:
      token: ${{ secrets.OPENLANE_TOKEN }}
  - run: courier apply
  - run: courier pull
  - uses: peter-evans/create-pull-request@v6
    with:
      title: "chore: write back IDs from Openlane"
      branch: courier/write-back
```

**Nightly drift detection** — export and open a PR when Openlane changed:

```yaml
on:
  schedule:
    - cron: "0 6 * * *"
steps:
  - uses: actions/checkout@v4
  - uses: theopenlane/setup-openlane@v1
    with:
      token: ${{ secrets.OPENLANE_TOKEN }}
  - run: courier pull
  - uses: peter-evans/create-pull-request@v6
    with:
      title: "chore: drift detected in Openlane"
      branch: courier/drift
```

The write-back and drift PRs are no-ops from Openlane's perspective, merging
them only records the current state in git.
