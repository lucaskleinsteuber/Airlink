#!/usr/bin/env python3
"""Read-only privacy gate for release assets; never executes packaged code.

Usage: check-package-privacy.py PATH [PATH ...] or --self-test.
Accepts Windows single-file EXEs/ZIPs, Linux TARs, app bundles, and macOS DMGs.
DMGs are mounted read-only without opening Finder and only audit-created mounts
are detached. Findings name categories, never matched personal values. This is a
regression guard for known metadata channels, not a guarantee of anonymity.
"""
import argparse
import io
import plistlib
import re
import struct
import subprocess
import sys
import tarfile
import unittest
import zipfile
import zlib
from pathlib import Path

MAINTAINER = 'lucasfd3'
COMMIT_EMAIL = '307191020+lucasfd3@users.noreply.github.com'
BUNDLE_MARKER = bytes.fromhex('8b1202b96a612038727b930214d7a03213f5b9e6efae3318ee3b2dce24b36aae')
EXAMPLE_USERS = {'test', 'example', 'user', 'username', 'shared', 'public'}
HOME = re.compile(r'(?:/Users/|/home/)([A-Za-z0-9_.-]+)|[A-Za-z]:\\+Users\\+([A-Za-z0-9_.-]+)', re.I)
EMAIL = re.compile(r'[A-Za-z0-9_.+%\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}')
TOKEN = re.compile(r'\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{40,}|AKIA[A-Z0-9]{16})\b')
KEY = re.compile(r'-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----\s+[A-Za-z0-9+/=\s]{64,}-----END (?:RSA |EC |OPENSSH )?PRIVATE KEY-----')
COPYRIGHT = re.compile(r'copyright\s*(?:\(c\)|©)?\s*\d{4}(?:\s*[-–]\s*\d{4})?\s+([^\r\n]+)', re.I)
PRIVATE_FILE = re.compile(r'(?:^|/)(?:\.env(?:\.[^/]+)?|identity-key\.pem|certificate\.pem|settings\.json|transfers\.sqlite(?:-[^/]+)?|.*\.(?:p12|pfx|mobileprovision|keychain-db))$', re.I)
LIMIT = 512 * 1024 * 1024


def texts(data):
    # UTF-16LE strings in PE version/debug data need a separate pass.
    yield data.decode('utf-8', 'replace')
    if b'\0' in data:
        yield data[:len(data) // 2 * 2].decode('utf-16le', 'replace')
        yield data[1:1 + (len(data) - 1) // 2 * 2].decode('utf-16le', 'replace')


def content_findings(data, first_party=False):
    found = set()
    for text in texts(data):
        if any((m.group(1) or m.group(2)).lower() not in EXAMPLE_USERS for m in HOME.finditer(text)):
            found.add('personal home path')
        if TOKEN.search(text): found.add('credential-shaped token')
        if KEY.search(text): found.add('private key material')
        if first_party:
            for m in COPYRIGHT.finditer(text):
                if not re.match(re.escape(MAINTAINER) + r'(?:\b|$)', m.group(1), re.I):
                    found.add('first-party copyright identity requires review')
            for m in EMAIL.finditer(text):
                address = m.group().lower()
                if address != COMMIT_EMAIL.lower() and address.split('@')[1] not in {'example.com', 'example.org', 'example.net', 'localhost', 'test.invalid'}:
                    found.add('first-party email address requires review')
    return found


def safe_label(label):
    value = HOME.sub('/[home]/[redacted]', str(label))
    return EMAIL.sub('[email]', value)


def first_party_document(name):
    name = name.replace('\\', '/').rsplit('/', 1)[-1].lower()
    return name in {'readme', 'readme.md', 'readme.txt', 'license', 'license.md', 'license.txt'}


def checked_slice(data, offset, size):
    if offset < 0 or size < 0 or offset + size > len(data):
        raise ValueError('invalid binary range')
    return data[offset:offset + size]


def dotnet_string(data, offset):
    size, shift = 0, 0
    for _ in range(5):
        b = checked_slice(data, offset, 1)[0]; offset += 1
        size |= (b & 127) << shift
        if b < 128:
            return checked_slice(data, offset, size).decode('utf-8'), offset + size
        shift += 7
    raise ValueError('invalid bundle string')


def bundle_entries(data):
    marker = data.find(BUNDLE_MARKER)
    if marker < 0: return
    if marker < 8: raise ValueError('invalid bundle marker')
    start = struct.unpack('<q', checked_slice(data, marker - 8, 8))[0]
    major, minor, count = struct.unpack('<IIi', checked_slice(data, start, 12))
    if major != 6 or minor != 0 or count < 1 or count > 10000:
        raise ValueError('unsupported .NET single-file bundle header')
    _, at = dotnet_string(data, start + 12)
    at += 40
    for _ in range(count):
        offset, size, compressed, kind = struct.unpack('<qqqB', checked_slice(data, at, 25)); at += 25
        name, at = dotnet_string(data, at)
        if size < 0 or compressed < 0 or size > LIMIT: raise ValueError('invalid bundle entry size')
        raw = checked_slice(data, offset, compressed or size)
        if compressed:
            decoder = zlib.decompressobj(-15)
            payload = decoder.decompress(raw, size + 1)
            if len(payload) != size or not decoder.eof or decoder.unconsumed_tail:
                raise ValueError('invalid compressed bundle entry')
        else: payload = raw
        yield name, kind, payload


class PE:
    def __init__(self, data):
        self.data = data
        if data[:2] != b'MZ': raise ValueError('invalid PE DOS header')
        pe = struct.unpack('<I', checked_slice(data, 0x3c, 4))[0]
        if checked_slice(data, pe, 4) != b'PE\0\0': raise ValueError('invalid PE signature')
        nsect, optsize = struct.unpack_from('<H', data, pe + 6)[0], struct.unpack_from('<H', data, pe + 20)[0]
        optional = pe + 24
        magic = struct.unpack('<H', checked_slice(data, optional, 2))[0]
        if magic not in (0x10b, 0x20b): raise ValueError('unsupported PE optional header')
        directories = optional + (112 if magic == 0x20b else 96)
        self.directories = [struct.unpack('<II', checked_slice(data, directories + 8 * i, 8)) for i in range(16)]
        self.sections = []
        for i in range(nsect):
            at = optional + optsize + i * 40
            virtual_size, rva, raw_size, raw = struct.unpack('<IIII', checked_slice(data, at + 8, 16))
            self.sections.append((rva, max(virtual_size, raw_size), raw))

    def offset(self, rva):
        for base, size, raw in self.sections:
            if base <= rva < base + size: return raw + rva - base
        raise ValueError('unmapped PE address')

    def debug_records(self):
        rva, size = self.directories[6]
        if not rva: return
        at = self.offset(rva)
        if size % 28: raise ValueError('invalid PE debug directory')
        for i in range(size // 28):
            kind, length, _, pointer = struct.unpack('<IIII', checked_slice(self.data, at + i * 28 + 12, 16))
            yield kind, checked_slice(self.data, pointer, length)

    def resources(self):
        # ECMA-335 metadata tables let us distinguish our MIT license from the
        # separately embedded third-party runtime notices. No assemblies load.
        cli, _ = self.directories[14]
        if not cli: return
        cor = self.offset(cli)
        metadata_rva, metadata_size = struct.unpack('<II', checked_slice(self.data, cor + 8, 8))
        resource_rva, resource_size = struct.unpack('<II', checked_slice(self.data, cor + 24, 8))
        if not resource_rva: return
        resources = self.offset(resource_rva)
        md = checked_slice(self.data, self.offset(metadata_rva), metadata_size)
        if md[:4] != b'BSJB': raise ValueError('invalid managed metadata')
        version_size = struct.unpack_from('<I', md, 12)[0]
        at = 16 + version_size
        _, nstreams = struct.unpack('<HH', checked_slice(md, at, 4)); at += 4
        streams = {}
        for _ in range(nstreams):
            offset, size = struct.unpack('<II', checked_slice(md, at, 8)); at += 8
            end = md.find(b'\0', at)
            if end < 0: raise ValueError('invalid metadata stream')
            name = md[at:end].decode('ascii'); at += (end - at + 4) // 4 * 4
            streams[name] = checked_slice(md, offset, size)
        table, strings = streams.get('#~', streams.get('#-')), streams.get('#Strings')
        if table is None or strings is None: raise ValueError('missing metadata tables')
        heap = table[6]; valid = struct.unpack_from('<Q', table, 8)[0]; at = 24
        rows = {}
        for i in range(64):
            if valid & (1 << i): rows[i] = struct.unpack_from('<I', table, at)[0]; at += 4
        def index(t): return 4 if rows.get(t, 0) >= 65536 else 2
        def coded(bits, ids): return 4 if max((rows.get(t, 0) for t in ids), default=0) >= (1 << (16 - bits)) else 2
        s, g, b = (4 if heap & bit else 2 for bit in (1, 2, 4))
        td = coded(2, [2, 1, 27]); resolution = coded(2, [0, 26, 35, 1])
        constant = coded(2, [4, 8, 23]); semantics = coded(1, [20, 23])
        member = coded(3, [2, 1, 26, 6, 27]); fieldmethod = coded(1, [4, 6])
        implementation = coded(2, [38, 35, 39])
        sizes = {
            0:2+s+g*3,1:resolution+s*2,2:4+s*2+td+index(4)+index(6),3:index(4),4:2+s+b,5:index(6),
            6:8+s+b+index(8),7:index(8),8:4+s,9:index(2)+td,10:member+s+b,11:2+constant+b,
            12:coded(5,[6,4,1,2,8,9,10,0,14,23,20,17,26,27,32,35,38,39,40,42,44,43])+coded(3,[6,10])+b,
            13:coded(1,[4,8])+b,14:2+coded(2,[2,6,32])+b,15:6+index(2),16:4+index(4),17:b,
            18:index(2)+index(20),19:index(20),20:2+s+td,21:index(2)+index(23),22:index(23),23:2+s+b,
            24:2+index(6)+semantics,25:index(2)+fieldmethod*2,26:s,27:b,28:2+coded(1,[4,6])+s+index(26),
            29:4+index(4),30:8,31:4,32:16+b+s*2,33:4,34:12,35:12+b*2+s*2,
            36:4+index(35),37:12+index(35),38:4+s+b,39:8+s*2+implementation,40:8+s+implementation,
        }
        for table_id in range(40): at += rows.get(table_id, 0) * sizes[table_id]
        for i in range(rows.get(40, 0)):
            row = at + i * sizes[40]
            offset = struct.unpack_from('<I', table, row)[0]
            name_index = int.from_bytes(checked_slice(table, row + 8, s), 'little')
            impl = int.from_bytes(checked_slice(table, row + 8 + s, implementation), 'little')
            if impl: continue
            end = strings.find(b'\0', name_index)
            name = strings[name_index:end].decode('utf-8')
            length = struct.unpack('<I', checked_slice(self.data, resources + offset, 4))[0]
            if offset + 4 + length > resource_size: raise ValueError('invalid managed resource range')
            yield name, checked_slice(self.data, resources + offset + 4, length)


class Audit:
    def __init__(self): self.issues = set(); self.files = self.bundled = self.resources = 0
    def issue(self, label, category): self.issues.add((safe_label(label), category))
    def payload(self, label, data, first_party=False, inspect_bundle=True):
        self.files += 1
        for finding in content_findings(data, first_party): self.issue(label, finding)
        if PRIVATE_FILE.search(label) and not label.endswith('.env.example'):
            self.issue(label, 'private runtime/signing filename')
        if label.lower().endswith(('.pdb', '.dsym')) or '/.dsym/' in label.lower():
            self.issue(label, 'debug artifact included in package')
        if data[:2] == b'MZ':
            image = PE(data)
            for kind, record in image.debug_records():
                for finding in content_findings(record): self.issue(label + ' [PE debug record]', finding)
                if kind == 17: self.issue(label, 'embedded portable debug information')
            for name, resource in image.resources():
                self.resources += 1
                # All resource bytes are already checked above for paths/keys.
                # Only the first-party license gets the ownership/email policy.
                if name == 'AirLink.Windows.License':
                    for finding in content_findings(resource, True): self.issue(label + ' [' + name + ']', finding)
            if inspect_bundle:
                for name, kind, payload in bundle_entries(data):
                    self.bundled += 1
                    self.payload(label + '!/' + name, payload, first_party_document(name), False)

    def file(self, path, label=None):
        label = label or path.name
        if path.stat().st_size > LIMIT: raise ValueError('input exceeds bounded audit size')
        data = path.read_bytes()
        self.payload(label, data, first_party_document(path.name))
        if path.name == 'Info.plist':
            info = plistlib.loads(data)
            for key in ('NSHumanReadableCopyright', 'CFBundleGetInfoString'):
                if isinstance(info.get(key), str):
                    for finding in content_findings(info[key].encode(), True): self.issue(label + ' [' + key + ']', finding)

    def path(self, path):
        if path.is_dir():
            if path.suffix.lower() == '.app': self.app_signature(path, path.name)
            for item in sorted(path.rglob('*')):
                if item.is_file() and not item.is_symlink(): self.file(item, path.name + '/' + str(item.relative_to(path)))
        elif path.suffix.lower() == '.dmg': self.dmg(path)
        elif zipfile.is_zipfile(path):
            with zipfile.ZipFile(path) as archive:
                for item in archive.infolist():
                    if item.is_dir(): continue
                    if item.file_size > LIMIT: raise ValueError('archive entry exceeds bounded audit size')
                    label = path.name + '!/' + item.filename
                    self.zip_owner(label, item.extra)
                    self.payload(label, archive.read(item), first_party_document(item.filename))
        elif tarfile.is_tarfile(path):
            with tarfile.open(path) as archive:
                for item in archive:
                    label = path.name + '!/' + item.name
                    if item.uid != 0 or item.gid != 0 or item.uname not in ('', 'root') or item.gname not in ('', 'root'):
                        self.issue(label, 'non-neutral archive owner metadata')
                    if item.isfile():
                        if item.size > LIMIT: raise ValueError('archive entry exceeds bounded audit size')
                        self.payload(label, archive.extractfile(item).read(), first_party_document(item.name))
        else: self.file(path)

    def zip_owner(self, label, extra):
        at = 0
        while at + 4 <= len(extra):
            kind, size = struct.unpack_from('<HH', extra, at); at += 4
            body = checked_slice(extra, at, size); at += size
            if kind == 0x7875 and len(body) >= 3:
                uid_size = body[1]; uid = int.from_bytes(checked_slice(body, 2, uid_size), 'little')
                gid_at = 2 + uid_size; gid_size = checked_slice(body, gid_at, 1)[0]
                gid = int.from_bytes(checked_slice(body, gid_at + 1, gid_size), 'little')
                if uid or gid: self.issue(label, 'non-neutral archive owner metadata')
            elif kind in (0x000d, 0x5855) and len(body) >= 12:
                uid, gid = struct.unpack_from('<HH', body, 8)
                if uid or gid: self.issue(label, 'non-neutral archive owner metadata')
            elif kind == 0x7855 and len(body) >= 4:
                if any(struct.unpack_from('<HH', body)): self.issue(label, 'non-neutral archive owner metadata')

    def app_signature(self, path, label):
        if sys.platform != 'darwin': return
        result = subprocess.run(['codesign', '-dv', '--verbose=4', str(path)], capture_output=True)
        if result.returncode: raise ValueError('app signature metadata could not be inspected')
        for line in result.stderr.decode('utf-8', 'replace').splitlines():
            if line.startswith('Authority='):
                self.issue(label, 'non-ad-hoc signer identity requires review')

    def dmg(self, path):
        if sys.platform != 'darwin': raise ValueError('DMG auditing requires macOS read-only mounting')
        def hdi(*args): return plistlib.loads(subprocess.check_output(['hdiutil', *args], stderr=subprocess.DEVNULL))
        resolved = str(path.resolve()); mount, device, created = None, None, False
        for image in hdi('info', '-plist').get('images', []):
            if str(Path(image.get('image-path', '')).resolve()) == resolved:
                mount = next((e.get('mount-point') for e in image.get('system-entities', []) if e.get('mount-point')), None)
                break
        try:
            if not mount:
                info = hdi('attach', '-readonly', '-nobrowse', '-noautoopen', '-plist', resolved)
                entities = info.get('system-entities', [])
                device = next((e.get('dev-entry') for e in entities if e.get('dev-entry')), None)
                created = True
                mount = next((e.get('mount-point') for e in entities if e.get('mount-point')), None)
            if not mount: raise ValueError('DMG has no readable filesystem')
            for app in Path(mount).glob('*.app'): self.app_signature(app, path.name + '!/' + app.name)
            for item in sorted(Path(mount).rglob('*')):
                if item.is_file() and not item.is_symlink():
                    self.file(item, path.name + '!/' + str(item.relative_to(mount)))
        finally:
            if created and device:
                subprocess.run(['hdiutil', 'detach', device], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


class GuardTests(unittest.TestCase):
    def test_binary_paths_and_neutral_text(self):
        personal = '/Users/' + 'private-person/build/Core.pdb'
        self.assertIn('personal home path', content_findings(personal.encode()))
        self.assertIn('personal home path', content_findings(personal.encode('utf-16le')))
        self.assertEqual(content_findings(b'/Users/test/file ' + MAINTAINER.encode()), set())

    def test_ownership_is_scoped_to_first_party_documents(self):
        third_party = b'Copyright (c) 2020 Example Runtime Authors\ncontact' + b'@' + b'vendor.invalid'
        self.assertEqual(content_findings(third_party), set())
        self.assertIn('first-party copyright identity requires review', content_findings(third_party, True))
        year = 2026
        self.assertEqual(content_findings(('Copyright (c) ' + str(year) + ' ' + MAINTAINER + '\n' + COMMIT_EMAIL).encode(), True), set())

    def test_credentials_and_archive_owner(self):
        self.assertIn('credential-shaped token', content_findings(('ghp_' + 'a' * 36).encode()))
        self.assertIn('private key material', content_findings(b'-----BEGIN PRIVATE KEY-----\n' + b'A' * 80 + b'\n-----END PRIVATE KEY-----'))
        a = Audit(); a.zip_owner('fixture.zip', struct.pack('<HH', 0x7875, 5) + bytes([1, 1, 0, 1, 0])); self.assertFalse(a.issues)
        a.zip_owner('fixture.zip', struct.pack('<HH', 0x7875, 5) + bytes([1, 1, 42, 1, 20])); self.assertTrue(a.issues)

    def test_compressed_bundle_payload_is_actually_inspected(self):
        payload = ('opaque prefix /Users/' + 'private-person/obj/Core.pdb').encode()
        compressor = zlib.compressobj(wbits=-15); compressed = compressor.compress(payload) + compressor.flush()
        prefix = bytearray(64); offset = len(prefix); prefix.extend(compressed); header = len(prefix)
        name = b'AirLink.Core.dll'
        prefix.extend(struct.pack('<IIi', 6, 0, 1) + b'\x01x' + bytes(40))
        prefix.extend(struct.pack('<qqqB', offset, len(payload), len(compressed), 1) + bytes([len(name)]) + name)
        prefix[0:8] = struct.pack('<q', header); prefix[8:40] = BUNDLE_MARKER
        # The enclosing synthetic host need not be PE for parser-level coverage.
        entries = list(bundle_entries(bytes(prefix)))
        self.assertEqual(entries, [('AirLink.Core.dll', 1, payload)])
        self.assertNotIn(b'/Users/', bytes(prefix))
        audit = Audit()
        for entry, kind, data in entries: audit.payload('fixture.exe!/' + entry, data)
        self.assertIn(('fixture.exe!/AirLink.Core.dll', 'personal home path'), audit.issues)

    def test_malformed_compression_is_rejected(self):
        data = bytearray(80); data[:8] = struct.pack('<q', 80); data[8:40] = BUNDLE_MARKER
        data.extend(struct.pack('<IIi', 6, 0, 1) + b'\x01x' + bytes(40))
        data.extend(struct.pack('<qqqB', 64, 8, 2, 1) + b'\x01x')
        with self.assertRaises((ValueError, zlib.error)): list(bundle_entries(bytes(data)))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('paths', nargs='*', type=Path)
    parser.add_argument('--self-test', action='store_true')
    args = parser.parse_args()
    if args.self_test:
        return 0 if unittest.TextTestRunner().run(unittest.defaultTestLoader.loadTestsFromTestCase(GuardTests)).wasSuccessful() else 1
    if not args.paths: parser.error('at least one release package or app path is required')
    audit = Audit()
    for path in args.paths:
        try: audit.path(path)
        except (OSError, ValueError, struct.error, zlib.error, subprocess.SubprocessError, tarfile.TarError, zipfile.BadZipFile, plistlib.InvalidFileException):
            audit.issue(path.name, 'package could not be completely inspected')
    for label, category in sorted(audit.issues): print(f'{label}: {category}', file=sys.stderr)
    print(f'Checked {len(args.paths)} input(s), {audit.files} payload(s), {audit.bundled} bundle entry/entries, {audit.resources} managed resource(s); {len(audit.issues)} finding(s).')
    return 1 if audit.issues else 0


if __name__ == '__main__': sys.exit(main())
