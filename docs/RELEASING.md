# Releasing

Releases are cut from the **`CHANGELOG.md`** — keep the `## [Unreleased]` section
current as work lands (each PR adds its entry there). Cutting a release then
promotes that section to a version and publishes.

## One-click release (preferred)

Actions → **Prepare Release** → *Run workflow* → enter the version (e.g. `v2.6.2`).

`prepare-release.yml` then:

1. Validates the version and refuses if the tag already exists or `## [Unreleased]` is empty.
2. Renames `## [Unreleased]` → `## [<version>] — <today>` in `CHANGELOG.md`.
3. Commits `docs: changelog <version>`, creates the annotated tag, and pushes both.
4. Dispatches the **Release** workflow on the new tag.

`release.yml` (GoReleaser) then builds the binaries/archives, signs the checksums
(cosign), publishes the **GitHub Release with that CHANGELOG section as the notes**
(see `scripts/changelog-section.sh`), and pushes the Homebrew cask to
`ericmann/homebrew-tap`.

> Why the explicit dispatch in step 4: a tag pushed with the default
> `GITHUB_TOKEN` does not auto-trigger other workflows, so `prepare-release`
> dispatches `release.yml` on the tag itself (`gh workflow run release.yml --ref <tag>`).

## Manual release (fallback)

The old flow still works and produces the same curated notes — `release.yml` runs
on any `v*` tag push and uses the matching CHANGELOG section:

```sh
# finalize CHANGELOG: rename "## [Unreleased]" -> "## [2.6.2] — <date>", then:
git commit -am "docs: changelog v2.6.2"
git tag -a v2.6.2 -m v2.6.2
git push origin main --follow-tags
```

## Notes

- **Release notes source:** the GitHub Release body is the CHANGELOG section for
  the tag, not an auto-generated commit list. If a tag has no matching section,
  GoReleaser falls back to its auto-generated notes.
- **Secrets:** `HOMEBREW_TAP_TOKEN` (push the cask). The default `GITHUB_TOKEN`
  covers the release upload and the workflow dispatch.
- This release tooling is **project-specific** and deliberately deterministic — it
  is not part of the reusable agentic-workflow template.
