---
title: Verifying a release
sidebar_position: 4
description: Check that a downloaded admin-server binary is the one this repository's release workflow built, using cosign and gh attestation.
---

# Verifying a release

A GitHub release page is not a chain of custody. Anyone with write access —
or anyone who takes it — can replace an asset with a binary that runs just as
happily as the real one. Releases cut from the first signed tag onward
therefore publish two independent proofs of where their binaries came from,
and this page is how you consume them.

## What is published, and what is not

One binary is published: **`admin-server`**, the standalone fleet server from
[Deployment](./deployment.md). Six archives, one per operating system and
architecture.

Nothing else here is published as a binary. The panel you mount with
`orbit.Module(...)`, the agent you embed in your application, the wire
contract and the two Quark adapters are **libraries**: they ship no
executable of their own — your application links them and ships them inside
*your* binary. So there is no `orbit-agent` download and nothing is missing:
you get those with `go get`, and the Go checksum database is what proves the
source you received is the source that was published. The tree does carry
runnable example programs (`examples/minimal`, `agent/examples/fleet-app`),
but those are demos that CI compiles and no release publishes. The signatures
on this page are for the one artefact that is downloaded rather than
compiled.

A signed release carries:

| Asset | What it is |
| --- | --- |
| `orbit-admin-server_<version>_<os>_<arch>.tar.gz` (`.zip` on Windows) | The server binary, six archives in total. |
| `<archive>.spdx.json` | An SPDX software bill of materials, one per archive, listing what went into it. |
| `checksums.txt` | SHA-256 of every archive and every SBOM. |
| `checksums.txt.sig` / `checksums.txt.pem` | A keyless signature over `checksums.txt`, and the short-lived certificate that made it. |

There is no long-lived signing key to trust, and none is published. The
signature is made by the release workflow itself with a certificate minted for
that single run, and what you check is not "who holds the key" but **which
workflow, in which repository, at which tag** produced the release. A build
provenance attestation, stored by GitHub rather than on the release page,
records the same thing a second way.

Signing cannot be applied retroactively — a release is signed by the run that
builds it, with a certificate minted for that run — so releases cut before
binaries were published carry no assets at all. Look at the release page
before you start: a verifiable release lists `checksums.txt.sig`.

Only **root** releases (`vX.Y.Z`) publish binaries. The module tags cut
alongside them — `server/…`, `agent/…`, `proto/…`, `quarkbridge/…`,
`quarkdatasource/…` — are Go module versions, distributed by the module proxy;
their release pages have no assets and nothing to verify here.

## Before you start

Install [cosign](https://docs.sigstore.dev/cosign/system_config/installation/)
and the [GitHub CLI](https://cli.github.com/). Then set the release you are
verifying:

```bash
export TAG=vX.Y.Z             # the root release you downloaded
export VERSION=${TAG#v}
export PKG=orbit-admin-server_${VERSION}_linux_amd64.tar.gz
```

## 1. Verify the signature over the checksum file

Download the checksum file, its signature and its certificate, then check
them:

```bash
gh release download "$TAG" --repo jcsvwinston/orbit \
  --pattern checksums.txt \
  --pattern checksums.txt.sig \
  --pattern checksums.txt.pem

cosign verify-blob checksums.txt \
  --signature checksums.txt.sig \
  --certificate checksums.txt.pem \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity "https://github.com/jcsvwinston/orbit/.github/workflows/release.yml@refs/tags/${TAG}"
```

`Verified OK` means the checksum file was produced by
`.github/workflows/release.yml` in `jcsvwinston/orbit`, running at the tag you
named, and has not been altered since.

:::caution The identity ends at the tag, not at `main`
Nearly every cosign example on the internet ends the identity in
`@refs/heads/main`. That string verifies nothing here. The release workflow
runs at the **tag** ref, so the certificate it signs with names
`@refs/tags/<tag>` — substitute the tag you are verifying, exactly as the
command above does. An identity ending in `refs/heads/main` will fail with
`none of the expected identities matched`, and that failure is correct.
:::

If you verify releases from a script and would rather not rewrite the identity
for each tag, match the shape instead of the value:

```bash
cosign verify-blob checksums.txt \
  --signature checksums.txt.sig \
  --certificate checksums.txt.pem \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github\.com/jcsvwinston/orbit/\.github/workflows/release\.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+$'
```

Keep the anchors and keep `refs/tags/` in the pattern. A regexp loose enough
to match any ref accepts a signature made from any branch of the repository,
which is most of the guarantee gone.

## 2. Carry the proof across to your archive

The signature covers `checksums.txt`. The checksum file covers the archive, so
one more step moves the trust onto the file you are about to run:

```bash
gh release download "$TAG" --repo jcsvwinston/orbit --pattern "$PKG"
awk -v f="$PKG" '$2 == f' checksums.txt | sha256sum -c -
```

On macOS, where there is no `sha256sum`, the last command is
`awk -v f="$PKG" '$2 == f' checksums.txt | shasum -a 256 -c -`.

The bill of materials beside each archive is listed in the same signed
checksum file, so the same two commands verify it: append `.spdx.json` to
`PKG` and run them again.

## 3. Verify the build provenance

The second, independent proof does not use the release assets at all: it asks
GitHub which workflow run built the file in front of you.

```bash
gh attestation verify "$PKG" \
  --repo jcsvwinston/orbit \
  --signer-workflow jcsvwinston/orbit/.github/workflows/release.yml
```

This matches the archive by digest against the attestation the release job
wrote, and `--signer-workflow` is the part that matters: it refuses an
attestation signed by any other workflow, in any other repository. The command
reads public data and needs no credentials beyond a logged-in `gh`.

## 4. Confirm what you unpacked

```bash
tar -xzf "$PKG"
./admin-server --version      # prints: nucleus-admin-server vX.Y.Z
```

The version is stamped into the binary at build time by the release workflow,
so it names the tag the binary was cut at rather than whatever the file was
renamed to. A published archive whose binary prints `devel` did not come from
a release build.

## When verification fails

- **`no matching signatures` or `none of the expected identities matched`** —
  the identity string is the first thing to check, and the tag inside it the
  first part of that. See the note above.
- **The release publishes no `checksums.txt.sig`** — releases cut before
  binaries were published carry no assets at all. Install from source with
  `go install` instead, or upgrade to a release that publishes a signature.
- **`gh attestation verify` reports no attestation** — same reason, same
  answer.
- **The tag has no assets and is a module tag** — module tags never publish
  binaries. Verify the root release of the same train instead; the
  [compatibility matrix](../reference/module-matrix.md) says which root
  release a module version belongs to.

Report anything that verifies against an identity other than the one above, or
an archive whose digest is absent from a validly signed checksum file, through
the repository's security policy rather than a public issue.
