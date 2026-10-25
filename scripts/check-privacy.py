#!/usr/bin/env python3
"""Check tracked publication inputs without printing matched private values.

This is a regression guard, not a guarantee of anonymity or a secret scanner.
Historical release binaries and GitHub metadata need a separate audit.
"""
import argparse
import re
import subprocess
import sys
import unittest
from pathlib import Path

MAINTAINER = "lucasfd3"
COMMIT_EMAIL = "307191020+lucasfd3@users.noreply.github.com"
EXAMPLE_USERS = {"test", "example", "user", "username", "shared", "public"}
EXAMPLE_DOMAINS = {"example.com", "example.org", "example.net", "localhost", "test.invalid"}
EMAIL = re.compile(r"[A-Za-z0-9_.+%\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}")
HOME = re.compile(r"(?:/Users/|/home/)([A-Za-z0-9_.\-]+)|[A-Za-z]:\\+Users\\+([A-Za-z0-9_.\-]+)", re.I)
TOKENS = re.compile(r"\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{40,}|AKIA[A-Z0-9]{16})\b")
PRIVATE_KEY = re.compile(r"-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----\s+([A-Za-z0-9+/=\s]{64,})-----END (?:RSA |EC |OPENSSH )?PRIVATE KEY-----")
SENSITIVE_PATH = re.compile(r"(?:^|/)(?:\.env(?:\.[^/]+)?|identity-key\.pem|certificate\.pem|settings\.json|transfers\.sqlite(?:-[^/]+)?|.*\.(?:p12|pfx|mobileprovision|keychain-db))$", re.I)


def run(*args):
    return subprocess.check_output(["git", *args])


def findings(data):
    text = data.decode("utf-8", errors="replace")
    found = []
    for line, value in enumerate(text.splitlines(), 1):
        if any((match.group(1) or match.group(2)).lower() not in EXAMPLE_USERS for match in HOME.finditer(value)):
            found.append((line, "personal home path"))
        for match in EMAIL.finditer(value):
            address = match.group().lower()
            if address != COMMIT_EMAIL.lower() and address.split("@", 1)[1] not in EXAMPLE_DOMAINS:
                found.append((line, "unreviewed email address"))
        if TOKENS.search(value):
            found.append((line, "credential-shaped token"))
        if re.search(r"copyright.*\b2026\b", value, re.I) and "lucasfd3" not in value.lower():
            found.append((line, "first-party copyright identity requires review"))
    for match in PRIVATE_KEY.finditer(text):
        found.append((text[:match.start()].count("\n") + 1, "private key material"))
    return sorted(set(found))


def check_file(label, data, path=None):
    results = findings(data)
    if path and SENSITIVE_PATH.search(path) and not path.endswith(".env.example"):
        results.append((0, "private runtime/signing filename"))
    for line, rule in results:
        print(f"{label}:{line}: {rule}", file=sys.stderr)
    return len(results)


def check_current():
    errors = 0
    files = [p for p in run("ls-files", "-z").split(b"\0") if p]
    for raw_path in files:
        path = raw_path.decode()
        errors += check_file(path, Path(path).read_bytes(), path)
    print(f"Checked {len(files)} tracked publication inputs; {errors} finding(s).")
    return errors


def check_history():
    errors, blobs = 0, 0
    # Only branches/tags are publication inputs; local editor checkpoint refs are not pushed.
    rows = run("rev-list", "--objects", "--branches", "--tags").splitlines()
    for row in rows:
        fields = row.split(b" ", 1)
        oid = fields[0].decode()
        if run("cat-file", "-t", oid).strip() != b"blob":
            continue
        path = fields[1].decode() if len(fields) > 1 else ""
        errors += check_file(f"blob {oid[:12]} {path}", run("cat-file", "blob", oid), path)
        blobs += 1
    commits = run("rev-list", "--branches", "--tags").decode().splitlines()
    for oid in commits:
        values = run("show", "-s", "--format=%an%x00%ae%x00%cn%x00%ce%x00%B", oid).decode().split("\0", 4)
        if values[:4] != [MAINTAINER, COMMIT_EMAIL, MAINTAINER, COMMIT_EMAIL]:
            print(f"commit {oid[:12]}: unreviewed author/committer identity", file=sys.stderr)
            errors += 1
        errors += check_file(f"commit {oid[:12]} message", values[4].encode())
    tags = run("for-each-ref", "--format=%(refname)", "refs/tags").decode().splitlines()
    for tag in tags:
        if run("cat-file", "-t", tag).strip() != b"tag":
            continue
        body = run("cat-file", "tag", tag).decode()
        identity = next((line for line in body.splitlines() if line.startswith("tagger ")), "")
        if not identity.startswith(f"tagger {MAINTAINER} <{COMMIT_EMAIL}> "):
            print(f"{tag}: unreviewed tagger identity", file=sys.stderr)
            errors += 1
        errors += check_file(f"{tag} message", body.split("\n\n", 1)[-1].encode())
    print(f"Checked {len(commits)} commits, {len(tags)} tags and {blobs} historical blobs; {errors} finding(s).")
    return errors


class GuardTests(unittest.TestCase):
    def test_personal_paths_are_reported_without_values(self):
        for path in ("/Users/" + "private-person/Build", "/home/" + "private-person/source", "C:" + chr(92) + "Users" + chr(92) + "private-person" + chr(92) + "source"):
            self.assertIn((1, "personal home path"), findings(path.encode()))

    def test_neutral_fixtures_remain_valid(self):
        for path in ("/Users/test/Downloads", "/Users/example/Downloads", "/home/user/source", r"C:\Users\example\Downloads"):
            self.assertEqual(findings(path.encode()), [])

    def test_email_policy(self):
        self.assertEqual(findings(COMMIT_EMAIL.encode()), [])
        self.assertEqual(findings(b"fixture@example.com"), [])
        self.assertEqual(findings(("private-person" + "@" + "private.invalid").encode()), [(1, "unreviewed email address")])

    def test_synthetic_diagnostic_is_not_a_real_key(self):
        self.assertEqual(findings(b"-----BEGIN PRIVATE KEY-----\nPRIVATE_KEY_CONTENT\n-----END PRIVATE KEY-----"), [])
        key = b"-----BEGIN PRIVATE KEY-----\n" + b"A" * 80 + b"\n-----END PRIVATE KEY-----"
        self.assertIn((1, "private key material"), findings(key))

    def test_credential_and_runtime_files(self):
        self.assertIn((1, "credential-shaped token"), findings(("ghp_" + "a" * 36).encode()))
        self.assertIsNotNone(SENSITIVE_PATH.search("state/identity-key.pem"))
        self.assertIsNone(SENSITIVE_PATH.search("internal/server/identity.go"))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--history", action="store_true", help="Also inspect published branches/tags and reachable blobs")
    parser.add_argument("--self-test", action="store_true", help="Run guard logic tests without accessing repository data")
    args = parser.parse_args()
    if args.self_test:
        suite = unittest.defaultTestLoader.loadTestsFromTestCase(GuardTests)
        return 0 if unittest.TextTestRunner().run(suite).wasSuccessful() else 1
    errors = check_current()
    if args.history:
        errors += check_history()
    return 1 if errors else 0


if __name__ == "__main__":
    sys.exit(main())
