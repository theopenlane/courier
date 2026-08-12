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

### Reports

By default `apply` prints one line per record, an operator's view of the run. `--report` renders the same run as a write-up instead: the totals, the records created, and for everything updated a table of every field that changed with the value before and after.

```bash
courier apply --report                # the report, in your terminal
courier apply --report -o results.md  # the same report, written as markdown
courier apply --dry-run               # the operator's view, as before
```

`--report` implies `--dry-run`: a report is something you read before deciding, so it never writes to Openlane. Pass `-o` on a real apply to keep the write-up of what a run actually did. The report renders before a failed record aborts the command, so a partial run still says what went through. Values render in full, apart from a policy body, which is a whole document and is cut short.

### Controls

Each entry in `controls.yaml` represents one organization control, the specific thing you implemented to meet a standard's control:

```yaml
- id: CTL_01J...
  refCode: CC1.1.3
  title: ""
  description: New hires are required to complete an acknowledgment form upon hire.
  category: Control Environment
  subcategory: Integrity and Ethics
  status: APPROVED
  categoryID: CC1.1
  controlOwner: Security Team
  delegate: Compliance Team
  referenceID: INT-014
  auditorReferenceID: AUD-014
  mappedControls:
    SOC 2:
      - CC1.1
```

The `title`, `description`, `category`, and `subcategory` fields are rendered even when they hold no value, so completing a sparse control means filling in a stubbed field rather than adding one. An empty field is treated as unmanaged: courier leaves the corresponding value in Openlane alone rather than clearing it. Rich-text descriptions authored in the Openlane editor export as plain text.

`controlOwner` names the group that owns the control and `delegate` the group it is delegated to. Openlane stores both as references to a group, so pull writes the group's display name and apply resolves it back, case-insensitively. Groups are never created: a name matching no group is skipped with a warning and the value in Openlane is left as it was. `referenceID` is your own internal tracking id for the control and `auditorReferenceID` is the reference your audit partner uses; both are free-form, as is `categoryID`.

`status` is one of `DRAFT`, `PREPARING`, `NEEDS_APPROVAL`, `CHANGES_REQUESTED`, `APPROVED`, `ARCHIVED`, `NOT_IMPLEMENTED`, or `NOT_APPLICABLE`, matched case-insensitively; a value naming none of them is skipped with a warning rather than written. A control created without a status takes the server's default, `NOT_IMPLEMENTED`; courier never approves a control on your behalf.

Editing a control's `description` moves it to `NEEDS_APPROVAL` on apply, whatever status the file carries, so changed control language is never left standing as approved. Editing a policy's body does the same to that policy. The dry run reports `status` alongside the edit that caused it. Pull afterwards so the files reflect the new status: a stale `status: APPROVED` left in a file will be pushed back on the next apply, since by then the text matches and nothing sends it for review again.

Pass `--keep-status` to turn the gate off and write the status the files carry, for a bulk edit that should not re-open approvals.

`externalUUID` is the stable external UUID used for deterministic OSCAL export. It is push-only: apply writes what the file carries, but no control query returns it, so pull cannot render it back and a change to it alone will not trigger an update.

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

### Importing a CSV

If your controls start life in a spreadsheet, `courier convert` turns a CSV into a store file, rendered exactly as `pull` would write it:

```console
courier convert controls.csv --map "control lead=controlOwner" --write
```

The header names the fields to fill, case insensitively and ignoring spaces, dashes, and underscores, so `refCode`, `ref_code`, and `Ref Code` are the same field. A `<field>.<key>` column fills one key of a map field, e.g. `mappedControls.SOC 2`, multiple values in one cell separate with a pipe, and a `parentRefCode` column nests a control under a control declared in an earlier row. Columns naming no field are an error rather than a silent drop: map them onto a field with `--map <column>=<field>`, drop one with `--map <column>=-`, or drop them all with `--skip-unknown`. The output goes to stdout unless you pass `--output path` or `--write`.

Conversion is driven by the document type, so `--kind policies` converts a policy manifest through the same rules, and validation against the schema happens before anything is written.

### Merging two files

`courier merge` combines two store files, which is how a converted spreadsheet meets what is already in Openlane: pull first, convert the spreadsheet second, then merge with the pulled file as the primary.

```console
courier merge data/controls.yaml updates.yaml --write --force
```

The primary file wins. A field it fills is kept; a field it leaves empty takes the value the secondary file holds for the same record. Map fields such as `mappedControls` merge key by key, so the secondary contributes only the frameworks the primary is missing, and subcontrols merge under their parent by the same rules. Records only the secondary file holds are appended, unless you pass `--only-existing` to fill what you already track and nothing more.

Records match the way `apply` matches them against Openlane: on the Openlane `id` when both records carry one, and on `refCode` for controls or `name` for policies otherwise. A converted CSV has no ids, so it always matches on refCode; a control renamed in Openlane after the spreadsheet was exported still merges with the record it came from, and the pulled refCode wins. Pass `--match-key-only` to ignore ids, or `--key-field` to match on something else entirely, e.g. `referenceID`.

Merging reports every field it filled and every record it added, as `refCode.field`, so a merge you did not expect shows up before you commit it.

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
