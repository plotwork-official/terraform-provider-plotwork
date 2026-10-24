#!/usr/bin/env bash
# Works out what the release pipeline releases for the commit checked out, from
# Conventional Commits since the last stable tag (vX.Y.Z with no suffix):
#
#   a breaking change (`feat!:` or a `BREAKING CHANGE:` footer)   major, or minor before 1.0
#   feat:                                                          minor
#   fix: or perf:                                                  patch
#   anything else (docs:, chore:, ci:, test:, refactor:)           nothing to release
#
# It prints key=value lines for $GITHUB_OUTPUT:
#
#   release=true|false   whether there is something to release
#   version=X.Y.Z        the next stable version
#   beta_tag=vX.Y.Z-beta.N   the pre-release to make, or the one this commit already has
#   beta_version=X.Y.Z-beta.N the same, as the registries name versions
#   beta_exists=true|false   true when this commit already carries that pre-release tag
#   stable_exists=true|false true when this commit already carries vX.Y.Z
#
# Run it with the full history checked out (actions/checkout fetch-depth: 0).
set -euo pipefail

last_stable="$(git tag --list 'v[0-9]*' | grep -Ev -- '-' | sort -V | tail -n 1 || true)"
range="${last_stable:+$last_stable..}HEAD"

breaking=false
feature=false
fix=false
while IFS= read -r -d $'\x1e' commit; do
  # Each record after the first starts with the newline that ended the one before.
  commit="${commit#$'\n'}"
  subject="${commit%%$'\n'*}"
  if [[ "$subject" =~ ^[a-z]+(\([^\)]*\))?!: ]] || grep -q '^BREAKING[ -]CHANGE:' <<<"$commit"; then breaking=true; fi
  if [[ "$subject" =~ ^feat(\([^\)]*\))?!?: ]]; then feature=true; fi
  if [[ "$subject" =~ ^(fix|perf)(\([^\)]*\))?!?: ]]; then fix=true; fi
done < <(git log --format='%B%x1e' "$range")

IFS=. read -r major minor patch <<<"${last_stable#v}"
major="${major:-0}" minor="${minor:-0}" patch="${patch:-0}"
if [[ "$breaking" == true ]]; then
  if ((major == 0)); then minor=$((minor + 1)) patch=0; else major=$((major + 1)) minor=0 patch=0; fi
elif [[ "$feature" == true ]]; then
  minor=$((minor + 1)) patch=0
elif [[ "$fix" == true ]]; then
  patch=$((patch + 1))
else
  echo "release=false"
  exit 0
fi
version="$major.$minor.$patch"

# This commit may already be released: a rerun picks up where the last run stopped.
here="$(git tag --points-at HEAD)"
stable_exists=false
grep -qx "v$version" <<<"$here" && stable_exists=true
beta_here="$(grep -E "^v$version-beta\.[0-9]+$" <<<"$here" | sort -V | tail -n 1 || true)"
if [[ -n "$beta_here" ]]; then
  beta_tag="$beta_here" beta_exists=true
else
  last_beta="$(git tag --list "v$version-beta.*" | sed "s/^v$version-beta\.//" | sort -n | tail -n 1 || true)"
  beta_tag="v$version-beta.$((${last_beta:-0} + 1))" beta_exists=false
fi

echo "release=true"
echo "version=$version"
echo "beta_tag=$beta_tag"
echo "beta_version=${beta_tag#v}"
echo "beta_exists=$beta_exists"
echo "stable_exists=$stable_exists"
