# hcl-lockfile-updater

Pin a provider (source address, version, and checksum hashes) into one or more
`.terraform.lock.hcl` files, like `tofu providers lock` but driven by explicit
arguments instead of a configuration tree.

## Install

    go install github.com/nijave/hcl-lockfile-updater@latest

## Usage

    hcl-lockfile-updater [--block-file FILE | PROVIDER_SOURCE] [flags] LOCKFILE...

Lookup mode resolves hashes from the provider registry:

    hcl-lockfile-updater registry.opentofu.org/hashicorp/aws \
        --version 6.0.0 --platform linux_amd64 --platform darwin_arm64 \
        .terraform.lock.hcl envs/dev/.terraform.lock.hcl

Generate a verbatim block and save it:

    hcl-lockfile-updater registry.opentofu.org/hashicorp/aws \
        --version 6.0.0 --platform linux_amd64 --print-block > aws.lock.hcl

Verbatim mode applies a hand-written block:

    hcl-lockfile-updater --block-file aws.lock.hcl .terraform.lock.hcl

### Flags

| Flag | Default | Meaning |
|---|---|---|
| `--version` | latest non-prerelease | exact version to pin |
| `--platform` | runtime `GOOS_GOARCH` | target platform (repeatable) |
| `--constraints` | (unset) | set/replace `constraints`; preserved when unset |
| `--registry` | `registry.opentofu.org` | registry host to query |
| `--block-file` | | verbatim mode: file with one `provider {}` block |
| `--print-block` | false | print the resolved block and exit |
| `--format` | true | run the written provider block through the hcl formatter |
| `--reformat` | false | reformat the entire lock file (overrides `--format`) |
| `--skip-missing` | false | only update lock files that already pin the provider |

## Behavior

- The tool preserves attributes it does not set. A lookup run leaves an
  existing `constraints` untouched unless you pass `--constraints`.
- Formatting scopes to the provider block the tool writes: `--format` (the
  default) runs that block through the hcl formatter while the rest of the
  file keeps its bytes. `--format=false` skips the formatter entirely, and
  `--reformat` normalizes the whole file through the formatter.
- The OpenTofu registry supplies `h1:` and `zh:` hashes directly (one call). For
  registries without that extension (the HashiCorp registry), the tool emits
  `zh:` hashes from the signed SHASUMS document only.
- Registry and SHASUMS requests require HTTPS, including every redirect. Hashes
  are validated before any lock file is written.
- The tool queries the registry once per provider+version, even across many lock files.

## License

Apache-2.0.
