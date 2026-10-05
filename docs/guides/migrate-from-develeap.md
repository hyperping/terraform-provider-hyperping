---
page_title: "Migrating from develeap/hyperping"
---

# Migrating from develeap/hyperping

This provider was first published as `develeap/hyperping` by [Develeap](https://develeap.com). It is now maintained by Hyperping and published as `hyperping/hyperping`. Resources, data sources and attributes are unchanged: only the provider address moves, and no resource is recreated.

## 1. Update the provider source

In every module that declares the provider:

```terraform
terraform {
  required_providers {
    hyperping = {
      source  = "hyperping/hyperping" # was "develeap/hyperping"
      version = "~> 1.0"
    }
  }
}
```

## 2. Point the existing state at the new address

Run once per state (and per workspace):

```shell
terraform state replace-provider \
  registry.terraform.io/develeap/hyperping \
  registry.terraform.io/hyperping/hyperping
```

## 3. Reinstall providers and check the plan

```shell
terraform init -upgrade
terraform plan
```

The plan should report `No changes.` If you commit `.terraform.lock.hcl`, commit the regenerated file too.
