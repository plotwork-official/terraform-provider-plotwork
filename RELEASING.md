# Releasing

How a version of the provider is built, signed, published to the Terraform and OpenTofu registries, and tested against Plotwork's preview and production APIs.

## TL;DR

| Question | Answer |
|---|---|
| How is a release made? | By merging to `main`. The Release pipeline (`pipeline.yml`) works out the version from the Conventional Commits, releases a beta, smoke-tests it on preview, and only then releases the stable version and smoke-tests it on production. A tag `v*` pushed by hand still releases that tag alone. |
| Preview or production? | A tag with a pre-release suffix (`v0.2.0-beta.1`) builds a version whose default API is preview. A plain tag (`v0.2.0`) defaults to production. |
| How do people get a beta? | Only by naming it exactly: `version = "0.2.0-beta.1"`. A range such as `~> 0.2` never selects a pre-release. |
| What signs it? | A GPG key whose private half is the repository secret `GPG_PRIVATE_KEY`, and whose public half is on each registry. |
| How is it tested before stable? | The beta, from the registry, runs every example of [terraform-plotwork-examples](https://github.com/plotwork-official/terraform-plotwork-examples) on preview with an Enterprise key, and the Free plan refusals suite with a Free key. Stable is released only if both pass, then tested the same way on production. |

## 1. One-time setup: the signing key

The Terraform Registry accepts RSA and DSA keys only, not Ed25519. Make an RSA 4096 key for the organisation, not for a person:

```shell
gpg --quick-generate-key "Plotwork Releases <releases@plotwork.dev>" rsa4096 sign 2y
```

GPG asks for a passphrase. Keep it in your password manager. Then find the key's id:

```shell
gpg --list-secret-keys --keyid-format=long "Plotwork Releases"
# sec   rsa4096/ABCDEF0123456789 2026-10-24 [SC] [expires: 2028-10-24]
```

Put the private key and passphrase in the repository's secrets, piping so neither touches the disk:

```shell
KEY_ID=ABCDEF0123456789
REPO=plotwork-official/terraform-provider-plotwork

gpg --armor --export-secret-keys "$KEY_ID" | gh secret set GPG_PRIVATE_KEY -R "$REPO"
gh secret set PASSPHRASE -R "$REPO"     # paste the passphrase at the prompt
```

Export the public key for the registries:

```shell
gpg --armor --export "$KEY_ID" > plotwork-signing-key.asc
```

Keep a backup of the private key in the password manager too (`gpg --armor --export-secret-keys "$KEY_ID"`). GPG wrote a revocation certificate to `~/.gnupg/openpgp-revocs.d/`; keep that with it. The key expires in two years: extend it with `gpg --quick-set-expire "$KEY_FPR" 2y`, then export the public key again and update it on both registries.

## 2. One-time setup: the registries

### Terraform Registry

1. Sign in at [registry.terraform.io](https://registry.terraform.io) with GitHub. On GitHub's consent page, grant access to the `plotwork-official` organisation (Request or Grant next to it). If the organisation restricts OAuth apps, approve "Terraform Registry" under the organisation's Settings, Third-party access.
2. Your avatar, **User Settings**, **Signing Keys**. Choose the namespace `plotwork-official` and paste `plotwork-signing-key.asc`.
3. Make the first release (section 3), so the repository has a signed release.
4. **Publish**, **Provider**, choose `plotwork-official`, then `terraform-provider-plotwork`, accept the terms, **Publish**. The registry adds a webhook, so every later release appears on its own within minutes.

### OpenTofu Registry

OpenTofu's registry is a GitHub repository; submissions are issues.

1. Open [opentofu/registry issues](https://github.com/opentofu/registry/issues/new/choose), choose **Submit new Provider**, and give `plotwork-official/terraform-provider-plotwork`.
2. Choose **Submit new Provider Signing Key**, namespace `plotwork-official`, paste `plotwork-signing-key.asc`.

A bot opens the pull request; once merged, OpenTofu reads new GitHub releases by itself.

## 3. One-time setup: the pipeline's keys

The pipeline tests with system identities from the Plotwork admin console (System, New system identity), one pair for each environment, made in that environment's console:

| Identity | Plan | Skips | Then |
|---|---|---|---|
| Enterprise | Enterprise, kept until deleted | Daily calls, Hosted diagrams, Hosted bytes, Stable links | Its organisation is made with it. Mint organisation key with the scopes Diagrams and Brand, icons and patterns; then Mint personal key. |
| Free | Free, kept until deleted | None | Mint key, then Create organisation. |

Put them in this repository's environments (Settings, Environments, `preview` and `production`):

```shell
REPO=plotwork-official/terraform-provider-plotwork
gh secret set PLOTWORK_API_KEY --env preview -R "$REPO"            # the Enterprise organisation key
gh secret set PLOTWORK_PERSONAL_API_KEY --env preview -R "$REPO"   # the Enterprise identity's personal key
gh variable set PLOTWORK_ORG --env preview -R "$REPO" --body <its organisation's slug>
gh secret set PLOTWORK_FREE_API_KEY --env preview -R "$REPO"   # the Free identity's key
gh variable set PLOTWORK_FREE_ORG --env preview -R "$REPO" --body <the Free organisation's slug>
# and the same four for --env production, from production's console
```

The smoke run sets and then removes each organisation's brand, icons and patterns, so use these organisations for nothing else.

## 4. How a release goes out

Merge to `main` with Conventional Commit messages. The pipeline does the rest:

| Step | What happens |
|---|---|
| What to release | `scripts/next-version.sh` reads the commits since the last stable tag: a breaking change bumps the minor before 1.0 (the major after), `feat:` the minor, `fix:` or `perf:` the patch. With nothing to release the run ends here. |
| Beta | Tags `vX.Y.Z-beta.N` and releases it: signed, a pre-release, preview by default. |
| Preview | Waits for the Terraform Registry to serve the beta, then runs the smoke test (Enterprise key) and the refusals (Free key) on preview. A failure stops here: nothing stable is released. |
| Stable | Tags `vX.Y.Z` on the same commit and releases it. |
| Production | Waits for the registry, then runs both suites on production. |

The run's summary shows each suite's table. To start it by hand (to retry after fixing an environment's keys), run **Release pipeline** from the Actions tab; on a commit already released it picks up where it stopped.

A failure on production cannot take the release back, because the registries keep every version. Fix it and merge: the pipeline releases the next patch. Deploy a Plotwork API change to production before merging provider code that needs it, or the production smoke test fails.

To release by hand instead, push a tag (`git tag v0.3.0-beta.1 && git push origin v0.3.0-beta.1`): the Release workflow releases that tag alone, with no smoke test. To smoke-test any released version, run **Smoke** from the Actions tab with the environment and the version.

## 5. What a release contains

| File | What it is |
|---|---|
| `terraform-provider-plotwork_<v>_<os>_<arch>.zip` | The binary for each platform |
| `..._SHA256SUMS` and `.sig` | Checksums, signed with the GPG key: what the registries verify |
| `..._manifest.json` | The plugin protocol version (6) |
| `*.cdx.json` | A CycloneDX SBOM for each archive, attached after the signed release and kept out of `SHA256SUMS`, which the registries read as the list of archives |
| Attestations | SLSA build provenance. Check one with `gh attestation verify <zip> --repo plotwork-official/terraform-provider-plotwork` |

## 6. If something goes wrong

| Problem | Fix |
|---|---|
| The workflow fails at "Import GPG key" | `GPG_PRIVATE_KEY` or `PASSPHRASE` is missing or wrong. Set them again (section 1). |
| The registry says the signature is invalid | The public key on the registry is not the one that signed. Upload `plotwork-signing-key.asc` again. |
| A release is broken | Never move or delete a published tag: the registries cache it. Release the next patch version. |
| A beta reached people by mistake | It cannot: a range never selects a pre-release. |

## See also

| Page | Why |
|---|---|
| [HashiCorp: publishing providers](https://developer.hashicorp.com/terraform/registry/providers/publishing) | The registry's rules |
| [OpenTofu registry: adding a provider](https://github.com/opentofu/registry#adding-providers-or-modules) | OpenTofu's process |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Building and testing locally |
