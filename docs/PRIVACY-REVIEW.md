# Publication privacy review

AirLink's active repository is now a newly created **private** repository. The earlier repository remains a separate **private, archived** repository. The migration separates the active publication inputs from the earlier repository identity that retained old provider objects; it does not claim to erase those objects or other copies.

The first-party public maintainer identity is **lucasfd3**. Repository-local Git configuration uses the ID-based GitHub Noreply address for new author/committer metadata. The license, app metadata, setup examples and test fixtures use the public identity or neutral examples. Third-party license authors and copyright notices remain intact.

## Repository boundary

| Repository | Role and recorded state |
| --- | --- |
| `lucasfd3/Airlink` | Newly created active private repository, REST repository ID `1433237767`; independently created rather than a fork. Only the sanitized branch/tag graph was imported. |
| `lucasfd3/Airlink-private-archive` | Earlier repository identity, REST repository ID `1431646266`, retained as private and archived. Earlier provider-retained objects remain an archive concern; they are not claimed to have been erased. |

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
| New-repository import and fresh clone | Sanitized main/version refs imported into the new private repository. Independent fresh clone: all 705 received objects, 20 commits and nine tags checked clean, with zero unreachable objects at the recorded snapshot. |
| Earlier history cleanup | Before archiving, the earlier repository received an owner-approved rewrite. Its then-fresh clone had 19 commits, nine tags and 502 historical blobs with zero guard findings; an exact-value scan of its 700 reachable objects was also clean. These are historical graph checks, not a new-repository or provider-erasure claim. |
| Earlier downloads and hosted metadata | Before archiving, all 40 earlier 0.1.0–0.8.0 packages, eight accompanying checksum files and eight approved old hosted runs were withdrawn. Those actions concern the earlier repository; their metadata is not part of the Git export/import. |
| Earlier retained provider objects | Two known old commit IDs remained retrievable with earlier author metadata in the earlier repository after its rewrite. That repository is now private and archived. No provider-wide deletion claim is made. |
| New-route isolation checks | All 17 original commit IDs were absent from the new repository: commit API responses explicitly reported no commit found, and Git-object requests returned not-found. All eight original annotated-tag objects and ten original copyright blobs also returned not-found. The 52 negative probes had no redirects; positive head/tag/license controls returned the expected objects and exact SHA values. |
| Replacement downloads | The five reviewed 0.8.1 packages passed the local privacy check and are published in the new active repository. All six new-repository files, including checksums, match the reviewed local sizes/SHA-256 values. Only the replacement 0.8.1 release was imported. |
| Visibility decision | Both repositories remain private. The reviewed source/history, known-object routes and replacement release files passed the completed checks. The owner decides visibility separately; the earlier archive should remain private. |

The earlier rewrite preserved commit topology, dates and version tag names while changing affected object IDs. Only the intended branch and version tags enter publication refs; editor checkpoint refs and unreachable local snapshots are excluded. Existing external clones must not merge the old history back into the cleaned branch.

The new repository's identity and intended refs, an independent clone and its complete received object database/history, known old-object routes without archive redirects, and the reviewed release assets' published sizes/SHA-256 values have been verified. Completed observations are recorded in [TESTING.md](TESTING.md). These are publication-input checks, not a server-side erasure guarantee for the earlier archive or other copies.

GitHub documents that old objects can remain accessible through cached commit URLs, other clones or forks even after rewriting branch/tag history. Repository migration and the checks above establish a reviewed publication-input boundary; they do not promise anonymity or control all copies outside that boundary. See [GitHub's sensitive-data removal guidance](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/removing-sensitive-data-from-a-repository).

## Regression checks and signing

Run `python3 scripts/check-privacy.py --self-test` and `python3 scripts/check-privacy.py`; after importing, also run it with `--history`. The guard prints categories and locations, not matched private values. It is a project regression guard and supplements the secret scanner, exact-value review and separate package audit. Its maintainer-only metadata policy needs deliberate review before accepting commits from additional contributors.

Go publication binaries omit checkout VCS metadata. Native .NET release assemblies omit debug symbols/paths, Swift release paths are mapped and linker debug maps are removed before signing, and Linux archive ownership/timestamps are neutral. Do not publish local logs, PDBs, dSYMs, tool caches, runtime state or raw audit reports.

The Git author name, application publisher label and cryptographic signing identity are separate. Current macOS test packages remain ad hoc signed without a human Developer ID authority; Windows packages remain unsigned. A GitHub handle is not an Apple Developer ID or Authenticode certificate. Future update signatures can use an application key identified as AirLink/lucasfd3, but the signing secret must remain outside Git and authenticity must be verified against a trusted public key. This patch enables no updater and adds no private signing key.
