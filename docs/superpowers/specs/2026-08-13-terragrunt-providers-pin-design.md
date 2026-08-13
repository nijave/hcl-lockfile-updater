# terragrunt-providers-pin — Design

**Status:** Approved (2026-08-13) — pending spec review
**Module path:** `github.com/nijave/terragrunt-providers-pin`
**Language / toolchain:** Go 1.25+

## 1. Purpose

A single Go binary that updates `.terraform.lock.hcl` files, modeled on
`tofu providers lock` but driven by explicit command-line arguments rather than
a configuration tree. It pins a provider — source address, version, and checksum
hashes — into one or more lock files without requiring a Terraform/OpenTofu
configuration to be present.

Two input modes share one merge routine:

- **Lookup mode** — given a provider source address (and optional version /
  platforms), query the provider registry for hashes and merge them into each
  lock file's `provider` block. The tool can also *emit* the resolved block for
  reuse.
- **Verbatim mode** — given a complete `provider "…" { … }` block in a file
  (`--block-file`), merge that block's attributes into each lock file's matching
  `provider` block.

## 2. CLI surface

```
terragrunt-providers-pin [--block-file FILE | PROVIDER_SOURCE] [flags] LOCKFILE...
```

`--block-file` and a positional `PROVIDER_SOURCE` are mutually exclusive. Pass
at least one lock file, or pass `--print-block` instead.

### Flags

| Flag | Repeatable | Default | Meaning |
|---|---|---|---|
| `--version V` | no | latest non-prerelease | exact version to pin |
| `--platform OS_ARCH` | yes | runtime `GOOS_GOARCH` | target platform(s) to fetch hashes for |
| `--constraints STR` | no | (unset) | set or replace the `constraints` attribute; when you omit it, the tool preserves any existing `constraints` |
| `--registry HOST` | no | `registry.opentofu.org` | registry host to query (bare host or full URL) |
| `--block-file PATH` | no | — | verbatim mode: file containing exactly one `provider {}` block |
| `--print-block` | no | false | print the resolved provider block to stdout and exit without writing files |

### Examples

```bash
# lookup: pin aws 6.0.0 for two platforms across two lock files
terragrunt-providers-pin registry.opentofu.org/hashicorp/aws \
    --version 6.0.0 --platform linux_amd64 --platform darwin_arm64 \
    .terraform.lock.hcl envs/dev/.terraform.lock.hcl

# generate a verbatim block from lookup and save it
terragrunt-providers-pin registry.opentofu.org/hashicorp/aws \
    --version 6.0.0 --platform linux_amd64 --print-block > aws.lock.hcl

# verbatim: apply a hand-written block to all lock files
terragrunt-providers-pin --block-file aws.lock.hcl \
    .terraform.lock.hcl envs/dev/.terraform.lock.hcl

# query the HashiCorp registry for a bare address
terragrunt-providers-pin hashicorp/aws --registry registry.terraform.io \
    --version 5.0.0 .terraform.lock.hcl
```

### Flag library

Use `github.com/spf13/pflag` so flags can appear among positional arguments and
`--platform` can repeat. (The stdlib `flag` package stops parsing at the first
positional, forcing a brittle flag-first ordering.)

## 3. Effective registry host

Resolve the host for both the registry query and the block label by this
precedence:

1. `--registry` flag, if set
2. the host embedded in the address, if present (e.g. `registry.opentofu.org` in
   `registry.opentofu.org/hashicorp/aws`)
3. default `registry.opentofu.org`

If `--registry` and an embedded host differ, the flag wins (documented, not an
error). The block label written into the lock file always carries the effective
host, keeping the address and the resolved hashes consistent. Always take the
namespace and type from the last two path components of the address.

The flag accepts a bare host (`registry.terraform.io`) or a full URL
(`https://registry.terraform.io`); the tool strips any scheme and always talks
HTTPS at `/v1/providers/...`.

## 4. Merge / write semantics (core behavior)

Both modes produce a set of **attribute overrides** and apply them through one
routine. The defining rule: the tool leaves untouched any attribute it does not
explicitly set. An existing `constraints = "~> 5.0"` survives a lookup run unless
you pass `--constraints`.

### Override sets

- **Lookup mode:** `version` (always), `hashes` (always — they must correspond to
  the version), `constraints` (only when you set `--constraints`).
- **Verbatim mode:** every attribute present in the provided block (`version`,
  `hashes`, `constraints`, and any others).

### Write procedure (per lock file)

1. Read the file. An empty or missing file starts from a blank HCL file.
2. Parse with `hclwrite`.
3. Find the existing `provider` block whose label equals the effective source
   address. If absent, append a new `provider` block with that label.
4. For each override, set that attribute on the block body via
   `SetAttributeValue` (scalars) or `SetAttributeRaw` (the `hashes` list,
   rendered from a `cty` list through `hclwrite.TokensForValue`). Setting
   replaces the attribute in place if present, appends if absent, and preserves
   every other attribute and the surrounding comments.
5. Write back atomically: write to a temp file in the same directory, then call
   `os.Rename`. Ensure a trailing newline.

### Block rendering

A single renderer produces the bytes of one `provider {}` block from
`(address, version, constraints, hashes)`. The file writer wraps this into a
full lock file; `--print-block` emits it bare (a valid standalone snippet).
Verbatim mode decodes the provided block into the same `(address, …)` tuple (see
§8) and then reuses the renderer, so both modes share identical output
formatting.

## 5. Registry protocol and hash acquisition

Both registries support the `providers.v1` protocol. Endpoints (HTTPS, base path
`/v1/providers/`):

- **List versions:** `GET /v1/providers/{ns}/{type}/versions` →
  `{ "versions": [ { "version": "...", "protocols": ["..."], "platforms": [ {"os","arch"} ] } ], "warnings": null }`
- **Package metadata:** `GET /v1/providers/{ns}/{type}/{version}/download/{os}/{arch}`
  → `{ filename, download_url, shasums_url, shasums_signature_url, shasum, signing_keys }`.
  Resolve relative URLs in the response against the request URL.

### Procedure

1. **Resolve version.** Fetch the versions list. Parse every `version` with
   `go-version`; drop prereleases unless the requested `--version` is itself a
   prerelease. Pick the highest (or the exact `--version`, verifying it exists).
2. **Fetch package metadata** for one of the selected platforms. This yields the
   `shasums_url` and (for OpenTofu) the `packages` map.
3. **Collect hashes** — **response-driven, not name-driven:**
   - If the metadata response contains a `packages` field (the OpenTofu
     registry's extension; `map[os_arch]{ hashes: [zh:…, h1:…], package_size }`),
     take both `h1:` and `zh:` for the selected platforms from it. One call covers
     every platform.
   - Otherwise (HashiCorp registry, or any registry without the extension), fetch
     the signed document at `shasums_url` and parse each line
     `<hex>  <filename>` into `zh:<hex>`. Emit `zh:` only (no `h1:`).
4. **Dedup and sort** the hash list (by scheme then value) for stable, diffable
   output. Hash order carries no meaning to Terraform/OpenTofu.

### Hash scope

Include hashes for the **selected platforms only**, filtering by `os_arch`
(OpenTofu `packages`) or by matching the platform token in the SHASUMS filename
(HashiCorp `zh:`).

### Hash formats (reference)

- `h1:` — `h1:` + base64(SHA256 of a directory summary). This matches Go's
  `dirhash.Hash1`. The OpenTofu `packages` extension provides these directly; the
  tool does **not** download zips or compute `h1:` itself.
- `zh:` — `zh:` + lowercase hex of the SHA256 of the provider `.zip` bytes. The
  registry's signed SHASUMS document supplies them.

## 6. Version and platform selection

- `go-version` semver parsing and precedence sort. Do not trust the registry's
  array order.
- "Latest" = highest non-prerelease version.
- Strip a registry-served leading `v` prefix on version strings defensively.
- If a requested `--platform` is not published for the chosen version, error out
  and list the available platforms.

## 7. Caching

An in-memory map keyed by `(host, namespace, type, version)` maps to the resolved
hash set. The tool consults the registry **once** even when it updates many lock
files in a single invocation — the multi-file case the tool exists to serve. No
disk cache.

## 8. Verbatim block decoding

Decode the `--block-file` content with `gohcl.DecodeBody`
(`hashicorp/hcl/v2/gohcl`, resolved to the OpenTofu fork) into a struct:

```go
type providerBlock struct {
    Version     string   `hcl:"version"`
    Constraints string   `hcl:"constraints,optional"`
    Hashes      []string `hcl:"hashes,optional"`
}
```

Validation: the file must contain exactly one top-level `provider` block; it must
have `version` and `hashes`; `constraints` is optional. The block label is the
source address. Decode failures (malformed HCL, wrong structure) raise a hard
error that surfaces the HCL diagnostics. The same decoder reads an existing lock
file to inspect its attributes.

## 9. Project structure

```
terragrunt-providers-pin/
  go.mod  go.sum  README.md  main.go
  internal/
    cli/        # pflag parsing, arg validation, orchestration
    registry/   # ListVersions, PackageMeta, SHASUMS parse, in-memory cache
    lockfile/   # Parse / Render block / MergeProviderBlock (hclwrite + gohcl)
  testdata/     # *.lock.hcl fixtures + recorded registry JSON
```

Small, single-purpose packages with clear boundaries. `main.go` is a thin entry
point that delegates to `internal/cli`.

## 10. Dependencies (all FOSS; OpenTofu preferred)

| Module | License | Role |
|---|---|---|
| `github.com/hashicorp/hcl/v2` → `replace` → `github.com/opentofu/hcl/v2` | MPL-2.0 | parse/decode (`gohcl`) + round-trip (`hclwrite`) |
| `github.com/hashicorp/go-version` | MPL-2.0 | semver parse/sort/constraints |
| `github.com/spf13/pflag` | BSD-3-Clause | interspersed, repeatable flags |
| `github.com/zclconf/go-cty` (transitive via hcl) | MIT | value tokens for `hclwrite` |

All are Mozilla / BSD / MIT — no business or source-available licenses. A
`replace` directive resolves the HCL module to the OpenTofu fork (preferred)
while the code keeps the canonical import paths.

## 11. Error handling and edge cases

- Verbatim input must hold exactly one top-level `provider` block (none or more
  than one → error).
- Registry HTTP errors → message with status code and URL.
- Version-not-found / platform-unavailable → clear error; list available
  platforms when relevant.
- File parse errors → report path with HCL diagnostics; exit non-zero, fail loud
  on the first error.
- Missing lock file → create it.
- Conflict between `--registry` and an embedded host → the flag wins (§3).

## 12. Testing

- **Registry** (via `httptest.Server` + fixture JSON, no live network): version
  resolution (latest / explicit / prerelease exclusion), OpenTofu `packages`
  extraction, HashiCorp SHASUMS→`zh` parsing, platform filtering, and cache
  hit-count (registry handler invoked once across many lock files).
- **Lockfile:** round-trip merge — add a new block, replace an existing one,
  preserve an existing `constraints` and any custom attribute through a lookup
  run, preserve comments; `--print-block` output; verbatim decode success and
  failure.
- **End-to-end** (`httptest`): lookup → merge into a temp lock file → assert
  contents against a golden file.
- **Known-value spot check:** e.g. `hashicorp/random` 3.6.3 `zh:` ==
  `4b4c11ccfba7319e901df2dac836b1ae8f12185e37249e8d870ee10bb87a13fe`.

## 13. Out of scope (YAGNI)

GPG signature verification of the SHASUMS document (the tool trusts HTTPS to the
registry); zip download / local `h1:` computation for registries without the
`packages` extension; provider protocol-version filtering; a disk cache; more
than one provider in a single invocation; `-fs-mirror` / `-net-mirror` support.
