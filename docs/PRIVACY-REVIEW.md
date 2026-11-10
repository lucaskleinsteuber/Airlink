# Publication privacy review

AirLink's active repository is **public** following the owner's later, explicit approval for the 0.12 beta. The repository IDs and visibility were checked again on 2026-11-10. The earlier development-only private interval and old account name below are historical observations. The active repository is the independent repository created during migration; the earlier repository remains separate, **private and archived**. The migration separates publication inputs from the earlier repository identity that retained old provider objects; it does not claim to erase those objects or other copies.

The owner now explicitly permits **lucaskleinsteuber** and the copyright name **Lucas Kleinsteuber**. Repository-local Git configuration retains the ID-based GitHub Noreply address for new author/committer metadata. Private home paths, access credentials, runtime state and publishing keys remain excluded. Earlier sanitized history can retain the former public handle `lucasfd3`. Third-party license authors and copyright notices remain intact.

## Repository boundary

| Repository | Role and recorded state |
| --- | --- |
| `lucaskleinsteuber/Airlink` | Active public repository, REST repository ID `1433237767`; independently created rather than a fork. Only the sanitized branch/tag graph was imported. The owner approved the current public beta publication after the earlier private development interval. |
| `lucaskleinsteuber/Airlink-private-archive` | Earlier repository identity, REST repository ID `1431646266`, retained as private and archived. Earlier provider-retained objects remain an archive concern; they are not claimed to have been erased. |

Reusing the `Airlink` name and URL does not make these the same GitHub repository identity. The initial import used an isolated fast-export/import stream preserving the sanitized object IDs, with 20 commits and nine annotated version tags at that snapshot. The import object database was scanned for the reviewed original private identity/home-account values with zero matches. An independent fresh clone at that snapshot contained 705 received objects, 20 commits and nine tags. Its complete received object database was clean under the reviewed checks, with no unreachable objects. Follow-up documentation commits have their own later scope.

## Scope

- Reviewed tracked source and documentation, all 17 original commits and eight original annotated tags, reachable historical blobs, historical release descriptions and the eight earlier releases' download assets before migration.
- Reviewed home paths, owner identity/email, key/token patterns, generated debug information, compressed .NET bundle contents, archive ownership, macOS bundle metadata and ad hoc code-signing identity.
- Gitleaks 8.30.1's historical scan returned two independently reviewed false positives: a fixed hexadecimal request-ID fixture and ordinary English checklist prose. No actual credential was identified by the completed pattern/content review. This is not a proof that every possible unknown secret is absent.
- Personal runtime data, existing pairings, receive folders and the installed app were not reset or renamed. They are separate from publication inputs.

## Recorded checks and limits

| Surface | State and boundary |
| --- | --- |
| Current source/docs | First-party identity and examples sanitized; private owner observations removed. |
| New-repository import and fresh clone | Sanitized main/version refs were imported while the new repository was private, before the later public visibility decision. Independent fresh clone: all 705 received objects, 20 commits and nine tags checked clean, with zero unreachable objects at the recorded snapshot. |
| Earlier history cleanup | Before archiving, the earlier repository received an owner-approved rewrite. Its then-fresh clone had 19 commits, nine tags and 502 historical blobs with zero guard findings; an exact-value scan of its 700 reachable objects was also clean. These are historical graph checks, not a new-repository or provider-erasure claim. |
| Earlier downloads and hosted metadata | Before archiving, all 40 earlier 0.1.0–0.8.0 packages, eight accompanying checksum files and eight approved old hosted runs were withdrawn. Those actions concern the earlier repository; their metadata is not part of the Git export/import. |
| Earlier retained provider objects | Two known old commit IDs remained retrievable with earlier author metadata in the earlier repository after its rewrite. That repository is now private and archived. No provider-wide deletion claim is made. |
| New-route isolation checks | All 17 original commit IDs were absent from the new repository: commit API responses explicitly reported no commit found, and Git-object requests returned not-found. All eight original annotated-tag objects and ten original copyright blobs also returned not-found. The 52 negative probes had no redirects; positive head/tag/license controls returned the expected objects and exact SHA values. |
| Replacement downloads | At the migration snapshot, the five reviewed 0.8.1 packages passed the local privacy check and were published in the new active repository. All six files, including checksums, matched the reviewed local sizes/SHA-256 values. Only the replacement 0.8.1 release was imported at that point; these are historical publication checks rather than the current download inventory. |
| Visibility decision | The owner returned the active repository to private visibility during earlier development and subsequently approved public 0.12 betas. The 2026-11-10 API checks confirm the active repository is public and the archive remains private/archived. Beta 1 anonymous package/feed verification is recorded separately in TESTING.md. These checks do not establish provider erasure or runtime acceptance. |

The earlier rewrite preserved commit topology, dates and version tag names while changing affected object IDs. Only the intended branch and version tags enter publication refs; editor checkpoint refs and unreachable local snapshots are excluded. Existing external clones must not merge the old history back into the cleaned branch.

The new repository's identity and intended refs, an independent clone and its complete received object database/history, known old-object routes without archive redirects, and the reviewed release assets' published sizes/SHA-256 values have been verified. Completed observations are recorded in [TESTING.md](TESTING.md). These are publication-input checks, not a server-side erasure guarantee for the earlier archive or other copies.

GitHub documents that old objects can remain accessible through cached commit URLs, other clones or forks even after rewriting branch/tag history. Repository migration and the checks above establish a reviewed publication-input boundary; they do not promise anonymity or control all copies outside that boundary. See [GitHub's sensitive-data removal guidance](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/removing-sensitive-data-from-a-repository).

## Regression checks and signing

Run `python3 scripts/check-privacy.py --self-test` and `python3 scripts/check-privacy.py`; after importing, also run it with `--history`. The guard prints categories and locations, not matched private values. It is a project regression guard and supplements the secret scanner, exact-value review and separate package audit. Its maintainer-only metadata policy needs deliberate review before accepting commits from additional contributors.

Go publication binaries omit checkout VCS metadata. Native .NET release assemblies omit debug symbols/paths, Swift release paths are mapped and linker debug maps are removed before signing, and Linux archive ownership/timestamps are neutral. Do not publish local logs, PDBs, dSYMs, tool caches, runtime state or raw audit reports.

The Git author name, application publisher label and cryptographic signing identity are separate. Current macOS test packages remain ad hoc signed without a human Developer ID authority; Windows packages remain unsigned. A GitHub handle is not an Apple Developer ID or Authenticode certificate.

Version 0.9 adds signed manual update delivery using the application's pinned Ed25519 public key. Only the public configuration and signed feeds are tracked. The persistent private seed is stored outside every Git checkout in a separate owner-only publishing directory; it is never bundled in the apps, release assets, logs or command arguments. Private seed/key file extensions are ignored and rejected by the publication guards. Upstream Sparkle/NetSparkle/Chaos.NaCl licenses retain their reviewed third-party authorship; those notices are distinct from first-party maintainer identity. See [UPDATES.md](UPDATES.md) for publishing, public-access requirements and key backup boundaries.
