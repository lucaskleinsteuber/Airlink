#!/usr/bin/env python3
"""Read-only privacy gate for release assets; never executes packaged code.

Usage: check-package-privacy.py PATH [PATH ...] or --self-test.
Accepts Windows single-file/NSIS EXEs/ZIPs, Linux TARs, app bundles, and macOS DMGs.
NSIS installers require 7zz/7z (or AIRLINK_7ZIP/--seven-zip). Installer code is
never executed; compressed control data and extracted payloads are inspected.
DMGs are mounted read-only without opening Finder and only audit-created mounts
are detached. Findings name categories, never matched personal values. This is a
regression guard for known metadata channels, not a guarantee of anonymity.
"""
import argparse
import gzip
import io
import hashlib
import io
import lzma
import os
import plistlib
import queue
import re
import shutil
import stat
import struct
import subprocess
import sys
import tarfile
import tempfile
import threading
import time
import unittest
import zipfile
import zlib
from pathlib import Path, PurePosixPath
from unittest import mock

MAINTAINER = 'lucaskleinsteuber'
COMMIT_EMAIL = '307191020+lucaskleinsteuber@users.noreply.github.com'
APPROVED_COPYRIGHT = ('lucasfd3', 'lucaskleinsteuber', 'Lucas Kleinsteuber')
APPROVED_EMAILS = {COMMIT_EMAIL.lower(), '307191020+lucasfd3@users.noreply.github.com'}
BUNDLE_MARKER = bytes.fromhex('8b1202b96a612038727b930214d7a03213f5b9e6efae3318ee3b2dce24b36aae')
EXAMPLE_USERS = {'test', 'example', 'user', 'username', 'shared', 'public'}
HOME = re.compile(r'(?:/Users/|/home/)([A-Za-z0-9_.-]+)|[A-Za-z]:\\+Users\\+([A-Za-z0-9_.-]+)', re.I)
EMAIL = re.compile(r'[A-Za-z0-9_.+%\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}')
TOKEN = re.compile(r'\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{40,}|AKIA[A-Z0-9]{16})\b')
KEY = re.compile(r'-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----\s+[A-Za-z0-9+/=\s]{64,}-----END (?:RSA |EC |OPENSSH )?PRIVATE KEY-----')
COPYRIGHT = re.compile(r'copyright\s*(?:\(c\)|©)?\s*\d{4}(?:\s*[-–]\s*\d{4})?\s+([^\r\n]+)', re.I)
PRIVATE_FILE = re.compile(r'(?:^|/)(?:\.env(?:\.[^/]+)?|identity-key\.pem|certificate\.pem|settings\.json|transfers\.sqlite(?:-[^/]+)?|.*\.(?:seed|key|priv|p8|p12|pfx|mobileprovision|keychain-db))$', re.I)
LIMIT = 512 * 1024 * 1024
NSIS_MAGIC = bytes.fromhex('efbeadde4e756c6c736f6674496e7374')
NSIS_ENTRY_LIMIT = 10000
NSIS_TOTAL_LIMIT = 1024 * 1024 * 1024
NSIS_LIST_LIMIT = 16 * 1024 * 1024
NSIS_DEPTH_LIMIT = 4
NSIS_PROCESS_SECONDS = 60


def is_nsis(data):
    return data[:2] == b'MZ' and NSIS_MAGIC in data


def nsis_stream(data):
    """Inspect our solid-LZMA NSIS profile, including non-file control strings.

    7-Zip exposes installed files, but not the compressed installer header (which
    contains e.g. LicenseData and registry strings). Unsupported NSIS profiles
    are rejected, rather than silently passing a scan of the outer PE stub.
    """
    marker = data.find(NSIS_MAGIC)
    if marker < 4: raise ValueError('invalid NSIS first header')
    start = marker - 4
    flags, signature, a, b, c, header_size, following = struct.unpack('<7I', checked_slice(data, start, 28))
    if header_size < 1 or header_size > 32 * 1024 * 1024 or following < 34:
        raise ValueError('invalid NSIS stream dimensions')
    raw = checked_slice(data, start + 28, following - 28)
    properties = raw[0]
    lc, remainder = properties % 9, properties // 9
    lp, pb = remainder % 5, remainder // 5
    dictionary = struct.unpack('<I', checked_slice(raw, 1, 4))[0]
    if properties >= 225 or lc + lp > 4 or not (4096 <= dictionary <= 128 * 1024 * 1024):
        raise ValueError('unsupported NSIS compression profile')
    decoder = lzma.LZMADecompressor(format=lzma.FORMAT_RAW, filters=[{
        'id': lzma.FILTER_LZMA1, 'dict_size': dictionary, 'lc': lc, 'lp': lp, 'pb': pb,
    }])
    decoded = decoder.decompress(raw[5:], max_length=LIMIT + 1)
    if len(decoded) > LIMIT or not decoder.eof or len(decoder.unused_data) not in (0, 4):
        raise ValueError('NSIS stream could not be inspected within its bound')
    if len(decoded) < header_size + 4 or struct.unpack('<I', checked_slice(decoded, 0, 4))[0] != header_size:
        raise ValueError('unsupported NSIS header framing')
    return checked_slice(decoded, 4, header_size), decoded


def installer_name(name):
    name = name.replace('\\', '/')
    if not name or len(name) > 1024 or name.startswith('/') or ':' in name or '\0' in name:
        raise ValueError('unsafe installer entry name')
    if any(ord(c) < 32 or ord(c) == 127 for c in name): raise ValueError('control character in installer entry name')
    parts = name.split('/')
    if any(part in ('', '.', '..') or part.rstrip(' .') != part for part in parts):
        raise ValueError('unsafe installer entry component')
    for part in parts:
        stem = part.split('.', 1)[0].upper()
        if stem in {'CON', 'PRN', 'AUX', 'NUL'} or re.fullmatch(r'(?:COM|LPT)[1-9]', stem):
            raise ValueError('reserved installer entry component')
    return str(PurePosixPath(*parts))


def nsis_listing(text):
    entries, names = {}, set()
    for block in re.split(r'\r?\n\s*\r?\n', text.strip()):
        if not block.strip(): continue
        fields = {}
        for line in block.splitlines():
            if ' = ' not in line: raise ValueError('unrecognized installer listing line')
            key, value = line.split(' = ', 1)
            if key in fields: raise ValueError('ambiguous installer listing')
            fields[key] = value
        if 'Path' not in fields: raise ValueError('installer listing omitted a path')
        name = installer_name(fields['Path'])
        if name.casefold() in names: raise ValueError('duplicate installer entry')
        names.add(name.casefold())
        if any(key.lower() in {'symbolic link', 'hard link', 'reparse', 'link'} for key in fields):
            raise ValueError('installer link entry is not permitted')
        attributes = fields.get('Attributes', '')
        if 'L' in attributes.upper() or 'l' in fields.get('Mode', ''):
            raise ValueError('installer link attributes are not permitted')
        directory = fields.get('Folder') == '+' or 'D' in attributes.upper() or fields.get('Mode', '').startswith('d')
        raw_size = fields.get('Size', '')
        # NSIS reconstructs an uninstaller without declaring its size in -slt.
        # Its actual extracted size is still checked before content inspection.
        size = int(raw_size) if raw_size else None
        if size is not None and not 0 <= size <= LIMIT: raise ValueError('installer entry exceeds its size bound')
        entries[name] = (directory, size)
        if len(entries) > NSIS_ENTRY_LIMIT: raise ValueError('too many installer entries')
    if sum(size or 0 for directory, size in entries.values() if not directory) > NSIS_TOTAL_LIMIT:
        raise ValueError('installer aggregate size exceeds its bound')
    return entries


def extracted_entries(root, declared=None, complete=False):
    """Validate the owned extraction tree without following any links."""
    found, total = {}, 0
    for directory, directories, files in os.walk(root, followlinks=False):
        for child in directories + files:
            item = Path(directory) / child
            info = item.lstat()
            if stat.S_ISLNK(info.st_mode) or getattr(info, 'st_file_attributes', 0) & 0x400:
                raise ValueError('installer extracted a link or reparse point')
            relative = installer_name(item.relative_to(root).as_posix())
            if not stat.S_ISDIR(info.st_mode) and not stat.S_ISREG(info.st_mode):
                raise ValueError('installer extracted an unsupported entry type')
            if item.resolve().is_relative_to(root.resolve()) is False:
                raise ValueError('installer entry escaped the owned extraction directory')
            directory_entry = stat.S_ISDIR(info.st_mode)
            if declared is not None:
                if relative not in declared and not (directory_entry and any(name.startswith(relative + '/') for name in declared)):
                    raise ValueError('installer extracted an undeclared entry')
                if relative in declared:
                    expected_dir, size = declared[relative]
                    if directory_entry != expected_dir: raise ValueError('installer entry type differs from its listing')
                    if size is not None and info.st_size > size and not directory_entry:
                        raise ValueError('installer entry exceeds its declared size')
                    if complete and size is not None and not directory_entry and info.st_size != size:
                        raise ValueError('installer entry size differs from its listing')
            if not directory_entry:
                total += info.st_size
                if info.st_size > LIMIT: raise ValueError('extracted installer file exceeds its bound')
            found[relative] = (directory_entry, item)
            if len(found) > NSIS_ENTRY_LIMIT: raise ValueError('too many extracted installer entries')
            if total > NSIS_TOTAL_LIMIT: raise ValueError('extracted installer size exceeds its bound')
    if complete and declared is not None and not set(declared).issubset(found):
        raise ValueError('installer extraction omitted declared entries')
    return found


def seven_zip(command, output_limit, extraction=None, declared=None):
    """Run only the trusted extractor, with output/time/disk bounds."""
    process = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    chunks, stopping = queue.Queue(maxsize=8), threading.Event()
    def read_output():
        try:
            while not stopping.is_set():
                block = process.stdout.read(65536)
                while not stopping.is_set():
                    try: chunks.put(block, timeout=.1); break
                    except queue.Full: continue
                if not block: return
        except (OSError, ValueError):
            while not stopping.is_set():
                try: chunks.put(b'', timeout=.1); break
                except queue.Full: continue
    reader = threading.Thread(target=read_output, daemon=True); reader.start()
    result, deadline, ended = bytearray(), time.monotonic() + NSIS_PROCESS_SECONDS, False
    try:
        while not ended or process.poll() is None:
            if time.monotonic() >= deadline: raise ValueError('installer extractor exceeded its deadline')
            if extraction is not None: extracted_entries(extraction, declared)
            try: block = chunks.get(timeout=.1)
            except queue.Empty: continue
            if not block: ended = True
            else:
                result.extend(block)
                if len(result) > output_limit: raise ValueError('installer extractor output exceeded its bound')
        if process.wait(timeout=2) != 0: raise ValueError('installer extractor did not complete successfully')
        return result.decode('utf-8', 'strict')
    finally:
        stopping.set()
        if process.poll() is None:
            process.kill()
            try: process.wait(timeout=2)
            except subprocess.TimeoutExpired: pass
        process.stdout.close(); reader.join(timeout=2)


def find_seven_zip(explicit=None):
    selected = explicit or os.environ.get('AIRLINK_7ZIP')
    if selected:
        command = shutil.which(selected)
        if not command: raise ValueError('configured installer extractor is not available')
        return command
    command = shutil.which('7zz') or shutil.which('7z')
    if not command: raise ValueError('NSIS inspection requires a trusted 7zz or 7z extractor')
    return command


def expected_installer_payload(path, architecture=None):
    if not path.is_file() or path.stat().st_size > LIMIT:
        raise ValueError('expected installer payload is unavailable or exceeds its bound')
    digest, size = hashlib.sha256(), 0
    with path.open('rb') as source:
        while block := source.read(1024 * 1024):
            size += len(block)
            if size > LIMIT: raise ValueError('expected installer payload exceeds its bound')
            digest.update(block)
    return size, digest.digest(), {'x64': 0x8664, 'arm64': 0xaa64}.get(architecture)


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
                if not any(re.match(re.escape(name) + r'(?:\b|$)', m.group(1), re.I) for name in APPROVED_COPYRIGHT):
                    found.add('first-party copyright identity requires review')
            for m in EMAIL.finditer(text):
                address = m.group().lower()
                if address not in APPROVED_EMAILS and address.split('@')[1] not in {'example.com', 'example.org', 'example.net', 'localhost', 'test.invalid'}:
                    found.add('first-party email address requires review')
    return found


def safe_label(label):
    value = HOME.sub('/[home]/[redacted]', str(label))
    return EMAIL.sub('[email]', value)


def third_party_sparkle(name):
    # Sparkle's original copyright and license must remain intact. Scope this
    # ownership exception to its bundled framework, including nested XPC apps;
    # path, credential, key and debug-artifact checks still apply to every byte.
    return '/sparkle.framework/' in ('/' + name.replace('\\', '/').lower())


def first_party_document(name):
    if third_party_sparkle(name): return False
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
        self.machine = struct.unpack('<H', checked_slice(data, pe + 4, 2))[0]
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


def bounded_uncompress(data):
    if data.startswith(b'\x1f\x8b'): reader = gzip.GzipFile(fileobj=io.BytesIO(data))
    elif data.startswith(b'\xfd7zXZ\x00'): reader = lzma.LZMAFile(io.BytesIO(data))
    elif data.startswith(b'\x28\xb5\x2f\xfd'):
        with tempfile.TemporaryFile() as source:
            source.write(data); source.seek(0)
            process = subprocess.Popen(['zstd', '-q', '-d', '-c'], stdin=source, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
            try:
                result = process.stdout.read(NSIS_TOTAL_LIMIT + 1)
                if len(result) > NSIS_TOTAL_LIMIT: raise ValueError('native payload exceeds its bound')
                if process.wait(timeout=30): raise ValueError('native zstd payload could not be decoded')
                return result
            finally:
                if process.poll() is None: process.kill(); process.wait()
    else: return data
    with reader:
        result = reader.read(NSIS_TOTAL_LIMIT + 1)
    if len(result) > NSIS_TOTAL_LIMIT: raise ValueError('native payload exceeds its bound')
    return result


def rpm_payload(data):
    if len(data) < 112 or data[:4] != b'\xed\xab\xee\xdb': raise ValueError('invalid RPM lead')
    def header(at):
        raw = checked_slice(data, at, 16)
        if raw[:3] != b'\x8e\xad\xe8': raise ValueError('invalid RPM header')
        count, size = struct.unpack_from('>II', raw, 8)
        if count > 100000: raise ValueError('oversized RPM header')
        end = at + 16 + count * 16 + size
        checked_slice(data, at, end-at)
        return end
    end = header(96)
    end = header((end + 7) & ~7)
    return bounded_uncompress(data[end:])


class Audit:
    def __init__(self, seven_zip_path=None, expected_payload=None):
        self.issues = set(); self.files = self.bundled = self.resources = self.installers = 0
        self.seven_zip_path, self.expected_payload = seven_zip_path, expected_payload
        self.expected_checks = self.installer_entries = self.installer_bytes = 0
    def issue(self, label, category): self.issues.add((safe_label(label), category))
    def payload(self, label, data, first_party=False, inspect_bundle=True, nsis_depth=0):
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
                    self.payload(label + '!/' + name, payload, first_party_document(name), False, nsis_depth)
            if is_nsis(data): self.nsis(label, data, nsis_depth)

    def nsis(self, label, data, depth=0):
        if depth >= NSIS_DEPTH_LIMIT: raise ValueError('nested installer depth exceeds its bound')
        extractor = find_seven_zip(self.seven_zip_path)
        header, stream = nsis_stream(data)
        self.installers += 1
        for finding in content_findings(header, True): self.issue(label + ' [NSIS control header]', finding)
        for finding in content_findings(stream): self.issue(label + ' [NSIS decoded stream]', finding)
        del header, stream
        # The owned directory is discarded even if a listing, extraction, nested
        # PE, or .NET resource fails inspection. The installer is never run.
        with tempfile.TemporaryDirectory(prefix='airlink-package-audit-') as temporary:
            owned = Path(temporary)
            archive, output = owned / 'installer.exe', owned / 'payload'
            archive.write_bytes(data); output.mkdir()
            listing = seven_zip([extractor, 'l', '-slt', '-ba', '-sccUTF-8', '--', str(archive)], NSIS_LIST_LIMIT)
            declared = nsis_listing(listing)
            # Validate the complete manifest before letting the extractor write.
            seven_zip([extractor, 'x', '-y', '-bd', '-bb0', '-sccUTF-8', '-o' + str(output), '--', str(archive)],
                      2 * 1024 * 1024, output, declared)
            entries = extracted_entries(output, declared, complete=True)
            self.installer_entries += len(entries)
            if self.installer_entries > NSIS_ENTRY_LIMIT: raise ValueError('nested installer entry count exceeds its bound')
            expected_matches = []
            for name, (directory, path) in sorted(entries.items()):
                if directory: continue
                payload = path.read_bytes()
                self.installer_bytes += len(payload)
                if self.installer_bytes > NSIS_TOTAL_LIMIT: raise ValueError('nested installer size exceeds its bound')
                if depth == 0 and self.expected_payload is not None:
                    size, digest, machine = self.expected_payload
                    if len(payload) == size and hashlib.sha256(payload).digest() == digest:
                        expected_matches.append(name)
                        if machine is not None and PE(payload).machine != machine:
                            self.issue(label, 'installer native payload architecture does not match')
                self.payload(label + '!/' + name, payload, first_party_document(name), True, depth + 1)
            if depth == 0 and self.expected_payload is not None:
                self.expected_checks += 1
                if len(expected_matches) != 1:
                    self.issue(label, 'installer must contain exactly one byte-identical expected native payload')

    def file(self, path, label=None):
        label = label or path.name
        if path.stat().st_size > LIMIT: raise ValueError('input exceeds bounded audit size')
        data = path.read_bytes()
        self.payload(label, data, first_party_document(label))
        if path.name == 'Info.plist':
            info = plistlib.loads(data)
            for key in ('NSHumanReadableCopyright', 'CFBundleGetInfoString'):
                if isinstance(info.get(key), str):
                    for finding in content_findings(info[key].encode(), not third_party_sparkle(label)): self.issue(label + ' [' + key + ']', finding)

    def path(self, path):
        if path.is_dir():
            if path.suffix.lower() == '.app': self.app_signature(path, path.name)
            for item in sorted(path.rglob('*')):
                if item.is_file() and not item.is_symlink(): self.file(item, path.name + '/' + str(item.relative_to(path)))
        elif path.suffix.lower() == '.dmg': self.dmg(path)
        elif path.suffix == '.AppImage': self.native_appimage(path.read_bytes(), path.name)
        elif path.suffix == '.deb': self.native_deb(path.read_bytes(), path.name)
        elif path.suffix == '.rpm': self.native_rpm(path.read_bytes(), path.name)
        elif path.name.endswith('.pkg.tar.zst'): self.native_tar(path.read_bytes(), path.name)
        elif zipfile.is_zipfile(path):
            with zipfile.ZipFile(path) as archive:
                for item in archive.infolist():
                    if item.is_dir(): continue
                    if item.file_size > LIMIT: raise ValueError('archive entry exceeds bounded audit size')
                    label = path.name + '!/' + item.filename
                    self.zip_owner(label, item.extra)
                    self.payload(label, archive.read(item), first_party_document(label))
        elif tarfile.is_tarfile(path):
            with tarfile.open(path) as archive:
                for item in archive:
                    label = path.name + '!/' + item.name
                    if item.uid != 0 or item.gid != 0 or item.uname not in ('', 'root') or item.gname not in ('', 'root'):
                        self.issue(label, 'non-neutral archive owner metadata')
                    if item.isfile():
                        if item.size > LIMIT: raise ValueError('archive entry exceeds bounded audit size')
                        self.payload(label, archive.extractfile(item).read(), first_party_document(label))
        else: self.file(path)

    def native_tar(self, data, label):
        data = bounded_uncompress(data)
        total = 0
        with tarfile.open(fileobj=io.BytesIO(data), mode='r:') as archive:
            for index, item in enumerate(archive):
                if index >= NSIS_ENTRY_LIMIT: raise ValueError('too many native package entries')
                name = item.name
                while name.startswith('./'): name = name[2:]
                if name in ('', '.'): continue
                name = installer_name(name.rstrip('/'))
                if item.uid != 0 or item.gid != 0 or item.uname not in ('', 'root') or item.gname not in ('', 'root'): self.issue(label+'!/'+name, 'non-neutral archive owner metadata')
                if item.isdir(): continue
                if not item.isfile(): raise ValueError('unsupported native package entry type')
                total += item.size
                if item.size > LIMIT or total > NSIS_TOTAL_LIMIT: raise ValueError('oversized native archive')
                self.payload(label+'!/'+name, archive.extractfile(item).read(), first_party_document(name))

    def native_rpm(self, data, label):
        payload = rpm_payload(data); at = 0; count = 0; total = 0
        while at < len(payload):
            raw = checked_slice(payload, at, 110)
            if raw[:6] not in (b'070701', b'070702'): raise ValueError('unsupported RPM cpio payload')
            fields = [int(raw[6+i*8:14+i*8],16) for i in range(13)]
            mode, uid, gid, links, size, namesize = fields[1], fields[2], fields[3], fields[4], fields[6], fields[11]
            if not 1 <= namesize <= 4096: raise ValueError('invalid cpio pathname')
            name = checked_slice(payload,at+110,namesize)
            if not name.endswith(b'\0'): raise ValueError('unterminated cpio pathname')
            name = name[:-1].decode('utf-8'); at = (at+110+namesize+3)&~3
            if name == 'TRAILER!!!': return
            while name.startswith('./'): name=name[2:]
            name=installer_name(name.rstrip('/')); count+=1; total+=size
            if count>NSIS_ENTRY_LIMIT or size>LIMIT or total>NSIS_TOTAL_LIMIT: raise ValueError('oversized RPM payload')
            content=checked_slice(payload,at,size);at=(at+size+3)&~3
            if uid or gid: self.issue(label+'!/'+name, 'non-neutral archive owner metadata')
            if stat.S_ISDIR(mode): continue
            if not stat.S_ISREG(mode) or links != 1: raise ValueError('RPM links or special files are not permitted')
            self.payload(label+'!/'+name,content,first_party_document(name))
        raise ValueError('missing cpio trailer')

    def native_deb(self, data, label):
        if data[:8] != b'!<arch>\n': raise ValueError('invalid DEB archive')
        at=8; seen=set()
        while at<len(data):
            header=checked_slice(data,at,60)
            if header[-2:] != b'`\n': raise ValueError('invalid ar header')
            name=header[:16].decode('ascii').strip().rstrip('/');size=int(header[48:58]);at+=60
            content=checked_slice(data,at,size);at+=size+(size%2)
            if name in seen: raise ValueError('duplicate DEB member')
            seen.add(name)
            if name=='debian-binary':
                if content!=b'2.0\n': raise ValueError('unsupported DEB version')
            elif name.startswith(('control.tar.','data.tar.')): self.native_tar(content,label+'!/'+name)
            else: raise ValueError('unexpected DEB member')
        if len(seen)!=3 or 'debian-binary' not in seen: raise ValueError('incomplete DEB')

    def native_appimage(self, data, label):
        if data[:4]!=b'\x7fELF' or data[8:11]!=b'AI\x02': raise ValueError('invalid AppImage')
        candidates=[];at=0
        while True:
            at=data.find(b'hsqs',at)
            if at<0: break
            if at+96<=len(data):
                block=struct.unpack_from('<I',data,at+12)[0]
                major=struct.unpack_from('<H',data,at+28)[0]
                used=struct.unpack_from('<Q',data,at+40)[0]
                if major==4 and 4096<=block<=1048576 and block&(block-1)==0 and 96<=used<=len(data)-at: candidates.append(at)
            at+=4
        if len(candidates)!=1: raise ValueError('ambiguous AppImage filesystem')
        with tempfile.TemporaryDirectory(prefix='airlink-appimage-audit-') as temporary:
            owned=Path(temporary); archive=owned/'payload.squashfs'; output=owned/'payload'
            archive.write_bytes(data[candidates[0]:]); output.mkdir()
            extractor=find_seven_zip(self.seven_zip_path)
            listing=seven_zip([extractor,'l','-slt','-ba','-sccUTF-8','--',str(archive)],NSIS_LIST_LIMIT)
            declared=nsis_listing(listing)
            seven_zip([extractor,'x','-y','-bd','-bb0','-sccUTF-8','-o'+str(output),'--',str(archive)],2*1024*1024,output,declared)
            for name,(directory,path) in extracted_entries(output,declared,complete=True).items():
                if not directory: self.file(path,label+'!/'+name)

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
    @staticmethod
    def native_fixture(machine=0x8664):
        image = bytearray(512)
        image[:2] = b'MZ'; image[0x3c:0x40] = struct.pack('<I', 0x80)
        image[0x80:0x84] = b'PE\0\0'
        struct.pack_into('<H', image, 0x84, machine)
        struct.pack_into('<H', image, 0x94, 224)
        struct.pack_into('<H', image, 0x98, 0x10b)
        return bytes(image)

    @classmethod
    def nsis_fixture(cls, header=b'neutral control strings', payload=b''):
        decoded = struct.pack('<I', len(header)) + header + payload
        raw = b'\x5d' + struct.pack('<I', 8 * 1024 * 1024) + lzma.compress(decoded, format=lzma.FORMAT_RAW,
            filters=[{'id': lzma.FILTER_LZMA1, 'dict_size': 8 * 1024 * 1024, 'lc': 3, 'lp': 0, 'pb': 2}]) + bytes(4)
        return cls.native_fixture() + bytes(4) + NSIS_MAGIC + struct.pack('<II', len(header), 28 + len(raw)) + raw

    def mocked_installer_audit(self, image, files, expected=None):
        def extract(command, limit, extraction=None, declared=None):
            if extraction is None:
                return '\n\n'.join('Path = ' + name + '\nSize = ' + str(len(data)) for name, data in files.items())
            for name, data in files.items():
                output = extraction / name; output.parent.mkdir(parents=True, exist_ok=True); output.write_bytes(data)
            return ''
        audit = Audit(expected_payload=expected)
        with mock.patch(__name__ + '.find_seven_zip', return_value='trusted-test-extractor'), \
             mock.patch(__name__ + '.seven_zip', side_effect=extract):
            audit.payload('fixture.exe', image)
        return audit

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

    def test_sparkle_third_party_license_keeps_attribution_and_general_checks(self):
        self.assertFalse(first_party_document('AirLink.app/Contents/Frameworks/Sparkle.framework/Versions/B/Resources/LICENSE'))
        self.assertTrue(first_party_document('AirLink.app/Contents/Resources/LICENSE'))
        self.assertTrue(first_party_document('AirLink.app/Contents/Resources/Sparkle.framework-fake/LICENSE'))
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / 'Info.plist'
            path.write_bytes(plistlib.dumps({'NSHumanReadableCopyright': 'Copyright (c) 2025 Sparkle Project'}))
            audit = Audit(); audit.file(path, 'AirLink.app/Contents/Frameworks/Sparkle.framework/Resources/Info.plist')
            self.assertFalse(audit.issues)
            path.write_bytes(plistlib.dumps({'NSHumanReadableCopyright': '/Users/' + 'private-person/project'}))
            audit.file(path, 'AirLink.app/Contents/Frameworks/Sparkle.framework/Resources/Info.plist')
            self.assertTrue(any(category == 'personal home path' for label, category in audit.issues))

    def test_private_signing_seed_filenames_are_rejected(self):
        for extension in ('seed', 'key', 'priv', 'p8', 'p12', 'pfx'):
            audit = Audit(); audit.payload('keys/signing.' + extension, b'opaque sensitive bytes')
            self.assertIn(('keys/signing.' + extension, 'private runtime/signing filename'), audit.issues)
        audit = Audit(); audit.payload('updates/public-keys.json', b'{"algorithm":"Ed25519"}')
        self.assertFalse(audit.issues)

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

    def test_nsis_compressed_control_and_file_bytes_are_inspected(self):
        year = 2026
        header = ('Copyright (c) ' + str(year) + ' Example Private Owner\n' + '/Users/' + 'private-person/source').encode('utf-16le')
        payload = ('unlisted compressed /home/' + 'private-person/build').encode()
        image = self.nsis_fixture(header, payload)
        self.assertNotIn(b'/Users/', image); self.assertNotIn(header, image)
        audit = self.mocked_installer_audit(image, {'$PLUGINSDIR/payload.txt': b'neutral'})
        self.assertIn(('fixture.exe [NSIS control header]', 'first-party copyright identity requires review'), audit.issues)
        self.assertIn(('fixture.exe [NSIS control header]', 'personal home path'), audit.issues)
        self.assertIn(('fixture.exe [NSIS decoded stream]', 'personal home path'), audit.issues)
        self.assertEqual(audit.installers, 1)

    def test_nsis_missing_extractor_fails_closed(self):
        with mock.patch.dict(os.environ, {'AIRLINK_7ZIP': ''}), mock.patch('shutil.which', return_value=None):
            with self.assertRaisesRegex(ValueError, 'requires a trusted'): Audit().payload('fixture.exe', self.nsis_fixture())

    def test_nsis_stream_bounds_and_unsupported_compression(self):
        image = self.nsis_fixture()
        self.assertEqual(nsis_stream(image)[0], b'neutral control strings')
        broken = bytearray(image); start = broken.find(NSIS_MAGIC) - 4
        for offset, value in [(start + 20, 33 * 1024 * 1024), (start + 24, len(image) * 2), (start + 29, 256 * 1024 * 1024)]:
            invalid = bytearray(broken); struct.pack_into('<I', invalid, offset, value)
            with self.assertRaises((ValueError, lzma.LZMAError)): nsis_stream(bytes(invalid))
        invalid = bytearray(image); invalid[start + 28] = 255
        with self.assertRaises(ValueError): nsis_stream(bytes(invalid))
        with mock.patch(__name__ + '.LIMIT', 8):
            with self.assertRaises(ValueError): nsis_stream(image)

    def test_nsis_listing_rejects_unsafe_names_links_duplicates_and_sizes(self):
        for name in ('../escape', '/absolute', 'C:\\absolute', 'safe/../../escape', 'file:stream', '\\\\server\\share',
                     './dot', 'a//b', 'NUL.txt', 'folder/trailing.', 'line\nPath = injected'):
            with self.subTest(name=name), self.assertRaises(ValueError): nsis_listing('Path = ' + name + '\nSize = 1')
        for listing in ('Path = file\nSize = 1\nSymbolic Link = target',
                        'Path = file\nSize = 1\nAttributes = L',
                        'Path = a\nSize = 1\n\nPath = A\nSize = 1',
                        'Path = file\nSize = -1', 'Path = file\nSize = ' + str(LIMIT + 1)):
            with self.subTest(listing=listing), self.assertRaises(ValueError): nsis_listing(listing)
        self.assertEqual(nsis_listing('Path = $PLUGINSDIR/System.dll\nSize = 3'), {'$PLUGINSDIR/System.dll': (False, 3)})
        with mock.patch(__name__ + '.NSIS_ENTRY_LIMIT', 1):
            with self.assertRaises(ValueError): nsis_listing('Path = a\nSize = 1\n\nPath = b\nSize = 1')
        with mock.patch(__name__ + '.NSIS_TOTAL_LIMIT', 1):
            with self.assertRaises(ValueError): nsis_listing('Path = a\nSize = 2')

    def test_nsis_extraction_tree_rejects_undeclared_links_and_size_mismatch(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / 'known').write_bytes(b'abc')
            self.assertEqual(set(extracted_entries(root, {'known': (False, 3)}, True)), {'known'})
            with self.assertRaises(ValueError): extracted_entries(root, {'known': (False, 2)}, True)
            with self.assertRaises(ValueError): extracted_entries(root, {'known': (False, 4)}, True)
            with self.assertRaises(ValueError): extracted_entries(root, {}, True)
            try: (root / 'link').symlink_to(root / 'known')
            except (OSError, NotImplementedError): self.skipTest('host does not permit creating a synthetic symlink')
            with self.assertRaises(ValueError): extracted_entries(root)

    def test_nsis_nested_installer_depth_is_bounded(self):
        image = self.nsis_fixture()
        with self.assertRaisesRegex(ValueError, 'nested installer depth'):
            self.mocked_installer_audit(image, {'nested.exe': image})

    def test_extractor_output_and_deadline_are_bounded(self):
        with self.assertRaisesRegex(ValueError, 'output exceeded'):
            seven_zip([sys.executable, '-c', 'import sys; sys.stdout.buffer.write(b"x" * 1024)'], 32)
        with mock.patch(__name__ + '.NSIS_PROCESS_SECONDS', .1):
            with self.assertRaisesRegex(ValueError, 'deadline'):
                seven_zip([sys.executable, '-c', 'import time; time.sleep(10)'], 1024)

    def test_nsis_expected_payload_is_matched_by_bytes_not_temporary_filename(self):
        native = self.native_fixture()
        expected = len(native), hashlib.sha256(native).digest(), 0x8664
        audit = self.mocked_installer_audit(self.nsis_fixture(), {'$_13_': native}, expected)
        self.assertEqual(audit.expected_checks, 1); self.assertFalse(audit.issues)
        wrong = self.mocked_installer_audit(self.nsis_fixture(), {'$_13_': native + b'changed'}, expected)
        self.assertIn(('fixture.exe', 'installer must contain exactly one byte-identical expected native payload'), wrong.issues)
        duplicate = self.mocked_installer_audit(self.nsis_fixture(), {'one': native, 'two': native}, expected)
        self.assertTrue(duplicate.issues)
        architecture = self.mocked_installer_audit(self.nsis_fixture(), {'$_13_': native}, (expected[0], expected[1], 0xaa64))
        self.assertIn(('fixture.exe', 'installer native payload architecture does not match'), architecture.issues)

    def test_nsis_extracted_nested_dotnet_bundle_is_inspected(self):
        payload = ('/Users/' + 'private-person/obj/managed.pdb').encode()
        compressor = zlib.compressobj(wbits=-15); compressed = compressor.compress(payload) + compressor.flush()
        native = bytearray(self.native_fixture()); offset = len(native); native.extend(compressed); header = len(native)
        name = b'AirLink.Core.dll'
        native.extend(struct.pack('<IIi', 6, 0, 1) + b'\x01x' + bytes(40))
        native.extend(struct.pack('<qqqB', offset, len(payload), len(compressed), 1) + bytes([len(name)]) + name)
        native[0x40:0x48] = struct.pack('<q', header); native[0x48:0x68] = BUNDLE_MARKER
        audit = self.mocked_installer_audit(self.nsis_fixture(), {'$_13_': bytes(native)})
        self.assertIn(('fixture.exe!/$_13_!/AirLink.Core.dll', 'personal home path'), audit.issues)
        self.assertEqual(audit.bundled, 1)

    @unittest.skipUnless(shutil.which('makensis') and (shutil.which('7zz') or shutil.which('7z')), 'NSIS and 7-Zip CLI are optional integration-test tools')
    def test_real_nsis_compressed_license_and_generated_uninstaller(self):
        with tempfile.TemporaryDirectory(prefix='airlink-nsis-self-test-') as temporary:
            root = Path(temporary)
            year = 2026
            (root / 'payload.txt').write_text('neutral installed file')
            (root / 'license.txt').write_text('Copyright (c) ' + str(year) + ' Example Private Owner\n' + '/Users/' + 'private-person/build')
            (root / 'fixture.nsi').write_text('Unicode true\nName "Privacy fixture"\nOutFile "fixture.exe"\n'
                'RequestExecutionLevel user\nSetCompressor /SOLID lzma\nPage license\nLicenseData "license.txt"\n'
                'Section\nSetOutPath "$INSTDIR"\nFile "payload.txt"\nWriteUninstaller "$INSTDIR\\Uninstall.exe"\nSectionEnd\n'
                'Section "Uninstall"\nDelete "$INSTDIR\\payload.txt"\nSectionEnd\n')
            result = subprocess.run([shutil.which('makensis'), '-V1', 'fixture.nsi'], cwd=root, capture_output=True, timeout=30)
            self.assertEqual(result.returncode, 0, 'synthetic installer must compile before audit')
            audit = Audit(); audit.path(root / 'fixture.exe')
            self.assertEqual(audit.installers, 2, 'generated uninstaller must also be deeply inspected')
            self.assertIn(('fixture.exe [NSIS control header]', 'personal home path'), audit.issues)
            self.assertIn(('fixture.exe [NSIS control header]', 'first-party copyright identity requires review'), audit.issues)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('paths', nargs='*', type=Path)
    parser.add_argument('--self-test', action='store_true')
    parser.add_argument('--seven-zip', help='trusted 7zz/7z executable; defaults to AIRLINK_7ZIP or PATH')
    parser.add_argument('--expected-installer-payload', type=Path, help='require exactly one byte-identical native payload in this installer')
    parser.add_argument('--expected-installer-arch', choices=('x64', 'arm64'), help='require the expected native PE architecture')
    args = parser.parse_args()
    if args.self_test:
        return 0 if unittest.TextTestRunner().run(unittest.defaultTestLoader.loadTestsFromTestCase(GuardTests)).wasSuccessful() else 1
    if not args.paths: parser.error('at least one release package or app path is required')
    if args.expected_installer_arch and not args.expected_installer_payload:
        parser.error('--expected-installer-arch requires --expected-installer-payload')
    if args.expected_installer_payload and len(args.paths) != 1:
        parser.error('--expected-installer-payload requires exactly one installer input')
    audit = Audit(seven_zip_path=args.seven_zip)
    if args.expected_installer_payload:
        try: audit.expected_payload = expected_installer_payload(args.expected_installer_payload, args.expected_installer_arch)
        except (OSError, ValueError): audit.issue('expected native payload', 'expected installer payload could not be inspected')
    for path in args.paths:
        try: audit.path(path)
        except (OSError, ValueError, struct.error, zlib.error, lzma.LZMAError, UnicodeError, subprocess.SubprocessError, tarfile.TarError, zipfile.BadZipFile, plistlib.InvalidFileException):
            audit.issue(path.name, 'package could not be completely inspected')
    if args.expected_installer_payload and audit.expected_checks != 1:
        audit.issue(args.paths[0].name, 'expected native payload was not verified in exactly one installer')
    for label, category in sorted(audit.issues): print(f'{label}: {category}', file=sys.stderr)
    print(f'Checked {len(args.paths)} input(s), {audit.files} payload(s), {audit.bundled} bundle entry/entries, {audit.resources} managed resource(s), {audit.installers} NSIS installer(s); {len(audit.issues)} finding(s).')
    return 1 if audit.issues else 0


if __name__ == '__main__': sys.exit(main())
