---
page_title: "Migrating from develeap/hyperping"
subcategory: ""
description: |-
  Move an existing Terraform state from the develeap/hyperping provider address to hyperping/hyperping.
---

# Migrating from develeap/hyperping

This provider was first published as `develeap/hyperping` by [Develeap](https://develeap.com). It is now maintained by Hyperping and published as `hyperping/hyperping`, starting with version 2.1.0.

Resources, data sources and attributes are the same as in `develeap/hyperping` 2.0.x: only the provider address changes. No resource is recreated, and a state written by `develeap/hyperping` 2.0.x is read as is.

## Requirements

- Terraform >= 1.11, as for `develeap/hyperping` 2.0.0 (write-only attributes).
- Coming from `develeap/hyperping` 1.x: upgrade to `develeap/hyperping` 2.0.x first and apply its [breaking changes](https://github.com/hyperping/terraform-provider-hyperping/blob/main/CHANGELOG.md#200---2026-07-21) (`terraform plan` clean on 2.0.x), then follow this guide.

## 1. Update the provider source

In every module that declares the provider:

```terraform
terraform {
  required_version = ">= 1.11"

  required_providers {
    hyperping = {
      source  = "hyperping/hyperping" # was "develeap/hyperping"
      version = "~> 2.1"
    }
  }
}
```

The `provider "hyperping" { ... }` block itself does not change.

## 2. Point the existing state at the new address

Run once per state, and once per workspace:

```shell
terraform state replace-provider \
  registry.terraform.io/develeap/hyperping \
  registry.terraform.io/hyperping/hyperping
```

Terraform lists the resources it is about to move and asks for confirmation (`-auto-approve` skips the prompt, for CI). With Terragrunt, run it through `terragrunt state replace-provider ...` in each unit.

## 3. Reinstall providers and check the plan

```shell
terraform init -upgrade
terraform plan
```

The plan should report:

```
No changes. Your infrastructure matches the configuration.
```

If you commit `.terraform.lock.hcl`, commit the regenerated file too: it now pins `registry.terraform.io/hyperping/hyperping` instead of `registry.terraform.io/develeap/hyperping`.

## Troubleshooting

- **`Failed to query available provider packages` for `develeap/hyperping`**: a module still declares `source = "develeap/hyperping"`, or a resource in the state still points at the old address. Search the configuration (including nested modules) for `develeap/hyperping`, then re-run step 2.
- **`Provider configuration not present`**: the state was not migrated in this workspace. Run step 2 in it.
- **A plan with changes after the switch**: run the same plan with `develeap/hyperping` 2.0.x first. If it is clean there and not with `hyperping/hyperping`, please [open an issue](https://github.com/hyperping/terraform-provider-hyperping/issues) with the plan output.
