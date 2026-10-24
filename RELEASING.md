# Releasing

How a version of the provider is built, signed, published to the Terraform and OpenTofu registries, and tested against Plotwork's preview and production APIs.

## TL;DR

| Question | Answer |
|---|---|
| How is a release made? | Push a tag `v*`. The Release workflow tests, builds, signs and publishes a GitHub release; the registries pick it up. |
| Preview or production? | A tag with a pre-release suffix (`v0.2.0-beta.1`) builds a version whose default API is preview. A plain tag (`v0.2.0`) defaults to production. |
| How do people get a beta? | Only by naming it exactly: `version = "0.2.0-beta.1"`. A range such as `~> 0.2` never selects a pre-release. |
| What signs it? | A GPG key whose private half is the repository secret `GPG_PRIVATE_KEY`, and whose public half is on each registry. |
| How is it tested before stable? | The smoke test in [terraform-plotwork-examples](https://github.com/plotwork-official/terraform-plotwork-examples) runs every example against preview with the beta, then against production with the stable version. |

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

## 3. Release a beta for preview

```shell
git switch main && git pull
git tag v0.1.0-beta.1
git push origin v0.1.0-beta.1
gh run watch -R plotwork-official/terraform-provider-plotwork   # the Release workflow
```

The GitHub release is marked pre-release. This build talks to `https://api-preview.plotwork.dev` unless `api_url` says otherwise. Smoke test it against preview:

```shell
gh workflow run smoke.yml -R plotwork-official/terraform-plotwork-examples \
  -f environment=preview -f provider_version=0.1.0-beta.1
```

Fix and tag `-beta.2`, `-beta.3` until it is clean.

## 4. Release the stable version for production

Deploy the API the provider needs to production first. Then tag the same commit as the last good beta:

```shell
git tag v0.1.0 v0.1.0-beta.3^{commit}
git push origin v0.1.0
```

Smoke test it against production:

```shell
gh workflow run smoke.yml -R plotwork-official/terraform-plotwork-examples \
  -f environment=production -f provider_version=0.1.0
```

Update `CHANGELOG.md`, moving "unreleased" to the version and date.

## 5. What a release contains

| File | What it is |
|---|---|
| `terraform-provider-plotwork_<v>_<os>_<arch>.zip` | The binary for each platform |
| `..._SHA256SUMS` and `.sig` | Checksums, signed with the GPG key: what the registries verify |
| `..._manifest.json` | The plugin protocol version (6) |
| `*.cdx.json` | A CycloneDX SBOM for each archive |
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
