#!/usr/bin/env python3
"""Real two-service simultaneous TLS transfers; temporary data, no desktop."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import threading
import time

spec = importlib.util.spec_from_file_location('acceptance', Path(__file__).with_name('acceptance-single-file.py'))
acceptance = importlib.util.module_from_spec(spec)
spec.loader.exec_module(acceptance)
MIB = 1024 ** 2


def run(args):
    executable = Path(args.helper).resolve()
    size = args.mib * MIB
    if size < 32 * MIB or args.timeout <= 0:
        raise ValueError('Use at least 32 MiB and a positive timeout')
    helpers, sources, hashes = {}, {}, {}
    peaks = {'left': 0, 'right': 0}
    stopped = threading.Event()
    monitor = None
    deadline = time.monotonic() + args.timeout

    def check():
        if time.monotonic() > deadline:
            raise RuntimeError('Bidirectional acceptance deadline exceeded')

    with tempfile.TemporaryDirectory(prefix='AirLink-bidirectional-') as directory:
        root = Path(directory)
        if shutil.disk_usage(root).free < 4 * size + 10 * 1024 ** 3:
            raise RuntimeError('Insufficient fixture space plus reserve')
        try:
            for role, fill in (('left', 0x51), ('right', 0xA3)):
                folder = root / role
                folder.mkdir()
                source = folder / (role + '.bin')
                block = bytes([fill]) * MIB
                with source.open('wb') as content:
                    for _ in range(args.mib):
                        content.write(block)
                with source.open('rb') as content:
                    hashes[role] = hashlib.file_digest(content, 'sha256').hexdigest()
                sources[role] = source
                helpers[role] = acceptance.Helper(executable, folder, role)
                if args.expect_version:
                    assert helpers[role].initial['version'] == args.expect_version
                helpers[role].rpc('settings.update', {'sendLimitBytesPerSecond': args.send_limit_mib * MIB})
            left, right = helpers['left'], helpers['right']
            attempt = left.rpc('peer.connect', {'url': right.initial['urls'][-1], 'fingerprint': right.initial['fingerprint']})
            right.rpc('pair.approveTrusted', {'id': attempt['requestId']})
            for role, other in (('left', 'right'), ('right', 'left')):
                while True:
                    check()
                    peer = next((p for p in helpers[role].rpc('status.summary')['devices']
                                 if p['id'] == helpers[other].initial['id'] and p['paired']), None)
                    if peer:
                        peer['autoAccept'] = True
                        helpers[role].rpc('device.update', peer)
                        break
                    time.sleep(.02)

            def sample():
                while not stopped.wait(.1):
                    for role, host in helpers.items():
                        try:
                            value = subprocess.check_output(['ps', '-o', 'rss=', '-p', str(host.process.pid)],
                                                            text=True, stderr=subprocess.DEVNULL, timeout=2).strip()
                            peaks[role] = max(peaks[role], int(value) * 1024)
                        except (OSError, ValueError, subprocess.SubprocessError):
                            pass

            monitor = threading.Thread(target=sample, daemon=True)
            monitor.start()
            outgoing = {}
            started = time.monotonic()
            for role, other in (('left', 'right'), ('right', 'left')):
                outgoing[role] = helpers[role].rpc('offer', {'deviceId': helpers[other].initial['id'],
                                                           'paths': [str(sources[role])]})['id']
            overlap = False
            while True:
                check()
                states = {role: host.rpc('status.summary')['transfers'] for role, host in helpers.items()}
                sent = {role: next(t for t in states[role] if t['id'] == identifier)
                        for role, identifier in outgoing.items()}
                incoming = {role: next((t for t in rows if t['direction'] == 'receive'), None)
                            for role, rows in states.items()}
                for rows in states.values():
                    for transfer in rows:
                        if transfer['state'] in ('failed', 'cancelled'):
                            raise RuntimeError('Transfer failed: ' + str(transfer))
                if all(t['state'] == 'running' for t in sent.values()) and all(
                        t and 0 < t['completedBytes'] < size for t in incoming.values()):
                    overlap = True
                if all(t['state'] == 'completed' for t in sent.values()) and all(
                        t and t['state'] == 'completed' for t in incoming.values()):
                    break
                time.sleep(.02)
            elapsed = time.monotonic() - started
            assert overlap, 'No measured overlap of both active send/receive streams'
            for role, other in (('left', 'right'), ('right', 'left')):
                result = helpers[role].rpc('transfer.received', {'id': incoming[role]['id']})
                received = Path(result['items'][0]['path'])
                assert received.stat().st_size == size
                with received.open('rb') as content:
                    assert hashlib.file_digest(content, 'sha256').hexdigest() == hashes[other]
                with sources[role].open('rb') as content:
                    assert hashlib.file_digest(content, 'sha256').hexdigest() == hashes[role]
            stopped.set()
            monitor.join(timeout=3)
            assert all(0 < value < args.max_rss_mib * MIB for value in peaks.values()), peaks
            report = {'version': left.initial['version'], 'helperSHA256': hashlib.sha256(executable.read_bytes()).hexdigest(),
                      'bytesPerDirection': size, 'seconds': round(elapsed, 6), 'simultaneousProgressObserved': overlap,
                      'sendLimitMiBPerSecondPerHost': args.send_limit_mib,
                      'sampledRSSBytes': peaks, 'sha256': hashes, 'independentHashesPassed': True,
                      'scope': 'same-host actual simultaneous two-service TLS transfer; no GUI or physical network'}
            if args.report:
                Path(args.report).write_text(json.dumps(report, indent=2) + '\n')
            print(json.dumps(report), flush=True)
        finally:
            stopped.set()
            if monitor:
                monitor.join(timeout=3)
            for host in helpers.values():
                host.stop()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('helper')
    parser.add_argument('--mib', type=int, default=300)
    parser.add_argument('--send-limit-mib', type=int, default=64, help='Keep both streams observable in the control snapshots')
    parser.add_argument('--timeout', type=int, default=180)
    parser.add_argument('--max-rss-mib', type=int, default=256)
    parser.add_argument('--expect-version')
    parser.add_argument('--report')
    run(parser.parse_args())
