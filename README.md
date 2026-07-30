[![Build status](https://badge.buildkite.com/34ad31fe4231b2953cd3f2d116364d21a39b2a4dbf1eea539a.svg)](https://buildkite.com/theopenlane/courier?branch=main)
[![Go Reference](https://pkg.go.dev/badge/github.com/theopenlane/courier.svg)](https://pkg.go.dev/github.com/theopenlane/courier)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache2.0-brightgreen.svg)](https://opensource.org/licenses/Apache-2.0)
[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=theopenlane_courier&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=theopenlane_courier)

> This repository is experimental; it's based on ideas or techniques not fully hardened or finalized, so please use caution. Best efforts have been made to ensure safety guards are in place.

# Courier

Courier `pull` exports your Openlane organization's controls, policies, and other objects into structured YAML and markdown files, and `apply` pushes your edits back through the Openlane API. The goals, broadly:

- Allow for bulk edits of control or policy language in a git-friendly fashion
- Create the basis for a git-flow signature with Openlane so that you can lean into git's branch protection, pull request flow, diff abilities
- Use traditional text, markdown editors for those friendly to the style
- Provide non-UI methods for performing cohesive data exports and/or re-imports

Non-goals:

- Providing 100% seamless capabilities with what the [Openlane Console](https://github.com/theopenlane/openlane-ui) provides for rich-text editing, comments / discussions, etc. (table coloring, font, etc., are not preserved and this repo doesn't attempt it)
- Bloating the scope to include every object in Openlane's graphql schema
- Turning `courier` into some terraform-esk frankenstein that haunts all our dreams
- Making `apply` destructive (e.g. making the non-presence of records, or the presence of empty fields, create destructive actions)

## Installation

Clone this repository and build the binary:

```bash
task build
```

This produces a `courier` binary in the repository root. You can also install with Go:

```bash
go install github.com/theopenlane/courier@latest
```

Or with Homebrew:

```bash
brew install theopenlane/tap/courier
```

Signed release archives are available on the [releases page](https://github.com/theopenlane/courier/releases).

## How it works

Run `courier pull` to export your organization's controls, control mappings, and policies into these files. Edit them, open a pull request, and run `courier apply` after merge to push the changes back. Git is the change record in both directions: pull rewrites the files from the server's current state, so `git diff` after a pull shows what changed in Openlane, and your pull request shows what you are changing.

Controls are matched by their Openlane ID, with a `refCode` fallback when an ID is not present. Policies are matched by ID alone, carried in the manifest or the document frontmatter, because policy names are not unique. Entries without a match are created: new control IDs arrive on the next `pull`, and a created policy's ID is written into its document frontmatter so a repeated apply updates the policy instead of duplicating it.

Both `pull` and `apply` accept `--controls` or `--policies` to operate on one object kind; with neither flag they operate on both.

### Controls

Each entry in `controls.yaml` represents one organization control, the specific thing you implemented to meet a standard's control:

```yaml
- id: CTL_01J...
  refCode: CC1.1.3
  title: ""
  description: New hires are required to complete an acknowledgment form upon hire.
  category: Control Environment
  subcategory: Integrity and Ethics
  mappedControls:
    SOC 2:
      - CC1.1
```

The `title`, `description`, `category`, and `subcategory` fields are rendered even when they hold no value, so completing a sparse control means filling in a stubbed field rather than adding one. An empty field is treated as unmanaged: courier leaves the corresponding value in Openlane alone rather than clearing it. Rich-text descriptions authored in the Openlane editor export as plain text.

The `mappedControls` block groups the reference codes of controls this record maps to by their framework, the same shape as the `satisfies` map in policy frontmatter. Reference codes resolve case-insensitively within their framework, and references to your own controls without a framework group under the `custom` key.

A refCode that matches nothing is skipped with a warning, so you can apply a control inventory before the referenced framework has been cloned. New entries create mappings in Openlane; entries removed from your files don't get removed in the API.

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
```

The document carries YAML frontmatter followed by the policy body:

```markdown
---
openlane_id: PLC_01J...
title: Application Security Policy
status: PUBLISHED
tags:
  - application
  - security
revision: v1.0.0
satisfies:
  SOC 2:
    - CC6.2
---

## Purpose and Scope
...
```

The `satisfies` map lists the framework controls the policy satisfies, grouped by framework short name. On `apply`, courier uploads the whole document and the Openlane server parses the frontmatter, so the file you review in a pull request is the source the platform ingests. On `pull`, bodies render from the server's stored content: markdown that courier uploaded comes back as written, and edits made in the Openlane rich-text editor arrive as converted markdown.

### Editor validation

The jsonscehmas for `controls.yaml` and `policies.yaml` live in [`schema/`](schema/), generated from the same types courier validates with at `apply` time. Point your editor's YAML language server at them to get autocomplete and inline validation while editing:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/theopenlane/courier/main/schema/controls.json
```

## Configuration

Settings merge from three sources, and later sources win: the config file, `COURIER_`-prefixed environment variables, and command-line flags.

The config file defaults to `config/.config.yaml` relative to the working directory, or pass `--config path`.

```yaml
host: https://api.theopenlane.io
token: tolp_...
organization-id: ""              # only needed for multi-organization tokens
dir: .                           # workspace directory
```

## Contributing

See the [contributing](.github/CONTRIBUTING.md) guide for more information.
