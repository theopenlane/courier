[![Go Report Card](https://goreportcard.com/badge/github.com/theopenlane/courier)](https://goreportcard.com/report/github.com/theopenlane/courier)
[![Build status](https://badge.buildkite.com/34ad31fe4231b2953cd3f2d116364d21a39b2a4dbf1eea539a.svg)](https://buildkite.com/theopenlane/courier?branch=main)
[![Go Reference](https://pkg.go.dev/badge/github.com/theopenlane/courier.svg)](https://pkg.go.dev/github.com/theopenlane/courier)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache2.0-brightgreen.svg)](https://opensource.org/licenses/Apache-2.0)
[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=theopenlane_courier&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=theopenlane_courier)

# Courier

**Courier** keeps your Openlane controls and internal policies in sync with a git repository. It exports your organization's records into structured YAML and markdown files, and pushes your edits back through the Openlane API, so you can review compliance changes the same way you review code: in pull requests.

Courier only manages records your organization owns and can edit. Standards and framework controls stay untouched, and applying changes never deletes anything. Records that exist in Openlane but not in your files are reported as drift so you decide what happens to them.

## Installation

Install with Homebrew:

```bash
brew install theopenlane/tap/courier
```

Or with Go:

```bash
go install github.com/theopenlane/courier@latest
```

Signed release archives are also available on the [releases page](https://github.com/theopenlane/courier/releases).

## How it works

Courier treats a directory in your repository as a workspace:

```
controls.yaml           # your control inventory
policies.yaml           # your policy manifest
policies/*.md           # one markdown document per policy
```

Run `courier pull` to export your organization's controls, control mappings, and policies into these files. Edit them, open a pull request, and run `courier apply` after merge to push the changes back. Records are matched by their Openlane ID when present, and by `refCode` (controls) or `name` (policies) otherwise. Entries without a match are created, and the next `pull` writes the assigned IDs back into your files.

Your data is written exactly as it exists in Openlane. Courier never reorders, trims, or rewrites values on export, and an empty field in your files is treated as unmanaged: it never clears the corresponding value in Openlane.

### Controls

Each entry in `controls.yaml` is one organization control:

```yaml
- id: CTL_01J...
  refCode: CC1.1.3
  description: New hires are required to complete an acknowledgment form upon hire.
  category: Control Environment
  subcategory: Integrity and Ethics
  mappedControls:
    - CC1.1
```

The `mappedControls` list holds the reference codes of controls this record maps to, typically framework controls cloned into your organization. Reference codes resolve case-insensitively against your organization's controls. A code that matches nothing is skipped with a warning, so you can apply a control inventory before the referenced framework has been cloned. New entries create mappings in Openlane; removed entries are reported as drift and never deleted.

### Policies

Each entry in `policies.yaml` points at a markdown document that holds the policy content:

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

Policy documents carry YAML frontmatter (title, status, revision, and tags) followed by the policy body. When a policy is created or its body changes, courier uploads the whole document and the Openlane server parses the frontmatter, so the same file you review in a pull request is the source the platform ingests.

### Editor validation

The JSON schemas for `controls.yaml` and `policies.yaml` live in [`schema/`](schema/), generated from the same types courier validates with at apply time. Point your editor's YAML language server at them to get autocomplete and inline validation while editing:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/theopenlane/courier/main/schema/controls.json
```

## Commands

### pull

Exports your organization's controls, mappings, and policies into the workspace. Files are rewritten in place and policy documents that no longer exist in Openlane are removed. You should see a summary of written and removed paths, or a note that the workspace is already up to date.

### fmt

Rewrites `controls.yaml` and `policies.yaml` into canonical YAML form and validates them against their schemas. Data values are never changed. Use `--check` in CI to fail instead of rewriting.

### plan

Shows what `apply` would change without touching Openlane: records to create, fields to update, mappings to add, and drift. Use `--json` for machine-readable output, and `--detailed-exitcode` to exit with code 2 when changes exist, which lets CI gate on a clean plan.

### apply

Creates and updates records from the workspace. Controls are applied first so new mappings can reference them, then mappings, then policies. A record that already exists is skipped with a warning rather than failing the run, so re-running `apply` is always safe. Nothing is ever deleted.

## Configuration

Settings merge from three sources, and later sources win: the config file, `COURIER_`-prefixed environment variables, and command-line flags.

The config file defaults to `.courier.yaml` in the working directory, or pass `--config path`:

```yaml
host: https://api.theopenlane.io
token: tolp_...
organization-id: ""              # only needed for multi-organization tokens
dir: .                           # workspace directory
```

The environment equivalents are `COURIER_HOST`, `COURIER_TOKEN`, `COURIER_ORGANIZATION_ID`, and `COURIER_DIR`. A commented example config and environment file live in [`config/`](config/), generated from the settings type, so they stay current as options change.

## Running in CI

We recommend a protected main branch with three workflows: validate on pull requests, apply on merge, and detect drift on a schedule. Nothing pushes to main directly; every change to your files, including the ones courier writes back, arrives as a pull request.

Validate and show the plan on every pull request:

```yaml
steps:
  - uses: actions/checkout@v4
  - run: go install github.com/theopenlane/courier@latest
  - run: courier fmt --check
  - run: courier plan
    env:
      COURIER_TOKEN: ${{ secrets.COURIER_TOKEN }}
```

Apply on merge to main, then propose the ID write-back as a pull request:

```yaml
steps:
  - uses: actions/checkout@v4
  - run: go install github.com/theopenlane/courier@latest
  - run: courier apply
    env:
      COURIER_TOKEN: ${{ secrets.COURIER_TOKEN }}
  - run: courier pull
    env:
      COURIER_TOKEN: ${{ secrets.COURIER_TOKEN }}
  - uses: peter-evans/create-pull-request@v6
    with:
      title: "chore: write back IDs from Openlane"
      branch: courier/write-back
```

Detect drift nightly by exporting and opening a pull request when anything changed in Openlane:

```yaml
on:
  schedule:
    - cron: "0 6 * * *"
steps:
  - uses: actions/checkout@v4
  - run: go install github.com/theopenlane/courier@latest
  - run: courier pull
    env:
      COURIER_TOKEN: ${{ secrets.COURIER_TOKEN }}
  - uses: peter-evans/create-pull-request@v6
    with:
      title: "chore: drift detected in Openlane"
      branch: courier/drift
```

The write-back and drift pull requests make no changes in Openlane. Merging them records the platform's current state in git, which keeps your repository and your organization aligned in both directions.

## Development

Common tasks are defined in the [`Taskfile`](Taskfile.yaml):

```bash
task build            # build the courier binary
task go:test          # run the test suite
task go:lint          # run golangci-lint
task config:generate  # regenerate config and document schemas
task test:smoke       # build and smoke test the binary
```

## Contributing

See [contributing](.github/CONTRIBUTING.md) for details on submitting patches and the contribution workflow.
