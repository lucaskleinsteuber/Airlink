# Publication privacy review

AirLink remains private. This review separates sanitized source/build inputs from the pending cleanup of historical GitHub objects and downloads. It does not declare the repository ready to make public.

The first-party public maintainer identity is **lucasfd3**. Repository-local Git configuration uses the ID-based GitHub Noreply address for new author/committer metadata. The license, app metadata, setup examples and test fixtures use the public identity or neutral examples. Third-party license authors and copyright notices remain intact.

## Scope

- Reviewed tracked source and documentation, all 17 original commits and eight annotated tags, reachable historical blobs, release descriptions and the eight releases' download assets.
- Reviewed home paths, owner identity/email, key/token patterns, generated debug information, compressed .NET bundle contents, archive ownership, macOS bundle metadata and ad hoc code-signing identity.
- Gitleaks 8.30.1's historical scan returned two independently reviewed false positives: a fixed hexadecimal request-ID fixture and ordinary English checklist prose. No actual credential was identified by the completed pattern/content review. This is not a proof that every possible unknown secret is absent.
- Personal runtime data, existing pairings, receive folders and the installed app were not reset or renamed. They are separate from publication inputs.

## Current cleanup state

| Surface | State |
| --- | --- |
| Current source/docs | First-party identity and examples sanitized; private owner observations removed. |
| New builds | Privacy fixes remove checkout/debug paths and local archive ownership. Final package checks are recorded in TESTING. |
| Historical Git authors/taggers and old file revisions | An isolated rewrite is prepared and must be independently verified before replacing remote references. |
| Old release downloads | Historical 0.1.0–0.8.0 packages contain old identity or build metadata. They must be withdrawn before publication; changing the current LICENSE does not repair them. |
| Hosted run metadata | Historical runs reference old commits and require cleanup separately from file changes. |
| Repository visibility | Private; changing visibility is a separate owner decision. |

The history rewrite preserves commit topology, dates and version tag names while changing the affected object IDs. Only the intended branch and version tags enter the isolated rewrite; editor checkpoint refs and unreachable local snapshots do not become publication refs. Existing external clones must not merge the old history back into the cleaned branch.

GitHub documents that old objects can remain accessible through cached commit URLs, other clones or forks even after rewriting branch/tag history. A fresh-clone check and remote metadata check are necessary; a successful force push alone is not a server-side erasure guarantee. See [GitHub's sensitive-data removal guidance](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/removing-sensitive-data-from-a-repository).

## Regression checks and signing

Run `python3 scripts/check-privacy.py --self-test` and `python3 scripts/check-privacy.py`; after rewriting, also run it with `--history`. The guard prints categories and locations, not matched private values. It is a project regression guard and supplements the secret scanner and separate package audit. Its maintainer-only metadata policy needs deliberate review before accepting commits from additional contributors.

Go publication binaries omit checkout VCS metadata. Native .NET release assemblies omit debug symbols/paths, Swift release paths are mapped and linker debug maps are removed before signing, and Linux archive ownership/timestamps are neutral. Do not publish local logs, PDBs, dSYMs, tool caches, runtime state or raw audit reports.

The Git author name, application publisher label and cryptographic signing identity are separate. Current macOS test packages remain ad hoc signed without a human Developer ID authority; Windows packages remain unsigned. A GitHub handle is not an Apple Developer ID or Authenticode certificate. Future update signatures can use an application key identified as AirLink/lucasfd3, but the signing secret must remain outside Git and authenticity must be verified against a trusted public key. This patch enables no updater and adds no private signing key.
