# terragrunt-providers-pin

Pin a provider (source address, version, and checksum hashes) into one or more
`.terraform.lock.hcl` files, like `tofu providers lock` but driven by explicit
arguments instead of a configuration tree.

## Install

    go install github.com/nijave/terragrunt-providers-pin@latest

## Usage

    terragrunt-providers-pin [--block-file FILE | PROVIDER_SOURCE] [flags] LOCKFILE...

Lookup mode resolves hashes from the provider registry:

    terragrunt-providers-pin registry.opentofu.org/hashicorp/aws \
        --version 6.0.0 --platform linux_amd64 --platform darwin_arm64 \
        .terraform.lock.hcl envs/dev/.terraform.lock.hcl

Generate a verbatim block and save it:

    terragrunt-providers-pin registry.opentofu.org/hashicorp/aws \
        --version 6.0.0 --platform linux_amd64 --print-block > aws.lock.hcl

Verbatim mode applies a hand-written block:

    terragrunt-providers-pin --block-file aws.lock.hcl .terraform.lock.hcl

### Flags

| Flag | Default | Meaning |
|---|---|---|
| `--version` | latest non-prerelease | exact version to pin |
| `--platform` | runtime `GOOS_GOARCH` | target platform (repeatable) |
| `--constraints` | (unset) | set/replace `constraints`; preserved when unset |
| `--registry` | `registry.opentofu.org` | registry host to query |
| `--block-file` | | verbatim mode: file with one `provider {}` block |
| `--print-block` | false | print the resolved block and exit |

## Behavior

- The tool preserves attributes it does not set. A lookup run leaves an
  existing `constraints` untouched unless you pass `--constraints`.
- The OpenTofu registry supplies `h1:` and `zh:` hashes directly (one call). For
  registries without that extension (the HashiCorp registry), the tool emits
  `zh:` hashes from the signed SHASUMS document only.
- The tool queries the registry once per provider+version, even across many lock files.

## License

MPL-2.0.
