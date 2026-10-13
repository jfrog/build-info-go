#!/usr/bin/env bash
set -euo pipefail

# Release script for build-info-go.
# Ported from the (now EOL) JFrog Pipelines pipeline "release_build_info_go"
# defined in release/pipelines.yml.
#
# Runnable both as a GitHub Actions step (.github/workflows/release.yml) and
# directly on a developer machine, provided the following are exported:
#
#   NEXT_VERSION           - version to release (e.g. 1.2.3)
#   JFROG_CLI_BUILD_NAME    - build name used for build-info collection
#   JFROG_CLI_BUILD_NUMBER  - build number used for build-info collection
#   IL_AUTOMATION_TOKEN     - GitHub token used to push to origin
#                             (secrets.IL_AUTOMATION_TOKEN in CI)
#
# Prerequisites (handled by dedicated setup steps in CI):
#   - repo checked out on `main` with full history (fetch-depth: 0)
#   - Go toolchain installed (matching go.mod)
#   - `jf` (JFrog CLI) installed and configured against Artifactory
#     (e.g. via `jf c add`, or the jfrog/setup-jfrog-cli action in CI)

# Always remove the JFrog CLI config on exit, mirroring the `if: always()`
# cleanup step from the original workflow.
trap 'jf c rm --quiet' EXIT

test -n "$NEXT_VERSION" -a "$NEXT_VERSION" != "0.0.0"

git checkout main
git config user.name "jfrog-ecosystem-integration-env"
git config user.email "eco-system@jfrog.com"
git remote set-url origin "https://${IL_AUTOMATION_TOKEN}@github.com/jfrog/build-info-go.git"
git fetch origin dev
git merge origin/dev

jf goc --repo-resolve ecosys-go-remote

jf audit

release/build.sh "$NEXT_VERSION"

jf rt bag
jf rt bce
jf rt bp "$JFROG_CLI_BUILD_NAME" "$JFROG_CLI_BUILD_NUMBER"

./buildscripts/build.sh
chmod u+x bi
./bi go > build-info.json
git add build-info.json
git commit -m "Update build-info.json file"

git tag "v${NEXT_VERSION}"

jf ds rbc ecosystem-build-info-go "$NEXT_VERSION" --spec="release/specs/bi-rbc-spec.json" --spec-vars="VERSION=$NEXT_VERSION" --sign
jf ds rbd ecosystem-build-info-go "$NEXT_VERSION" --site="releases.jfrog.io" --sync

git clean -fd
git push
git push --tags
