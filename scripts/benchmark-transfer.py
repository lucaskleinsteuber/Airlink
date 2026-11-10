#!/usr/bin/env python3
"""Headless real-daemon TLS transfer timing; temporary fixtures only.

Measures actual payload and independent destination hash. This is a same-host
comparison, not evidence of Wi-Fi, Windows storage, or physical-device speed.
"""
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

_spec = importlib.util.spec_from_file_location('airlink_acceptance', Path(__file__).with_name('acceptance-single-file.py'))
_acceptance = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_acceptance)
Helper = _acceptance.Helper
MIB = 1024 ** 2


def inventory(path):
    paths = [path] if path.is_file() else sorted(p for p in path.rglob('*') if p.is_file())
    files = {}
    for item in paths:
        if item.is_symlink(): raise ValueError('Benchmark source must not contain links')
        relative = '.' if path.is_file() else item.relative_to(path).as_posix()
        with item.open('rb') as content: digest = hashlib.file_digest(content, 'sha256').hexdigest()
        files[relative] = (item.stat().st_size, digest)
    directories = [] if path.is_file() else sorted(p.relative_to(path).as_posix() for p in path.rglob('*') if p.is_dir())
    return files, directories


def run(args):
    helper = Path(args.helper).resolve()
    helper_digest = hashlib.sha256(helper.read_bytes()).hexdigest()
    provided = Path(args.source).resolve() if args.source else None
    expected_files, expected_directories = inventory(provided) if provided else ({}, [])
    size = sum(info[0] for info in expected_files.values()) if provided else args.mib * MIB
    if size <= 0 or args.repeat <= 0 or args.timeout <= 0:
        raise ValueError('Positive fixture size, repeat count and timeout required')
    results = []
    for iteration in range(args.repeat):
        with tempfile.TemporaryDirectory(prefix='AirLink-benchmark-') as temporary:
            root = Path(temporary)
            if shutil.disk_usage(root).free < 2 * size + 1024 ** 3:
                raise RuntimeError('Not enough free space for both fixture copies plus 1 GiB reserve')
            source_file = provided or root / 'payload.bin'
            if not provided:
                block = bytes(range(256)) * 4096
                remaining = size
                with source_file.open('wb') as fixture:
                    while remaining:
                        piece = block[:min(remaining, len(block))]
                        fixture.write(piece)
                        remaining -= len(piece)
                expected_files, expected_directories = inventory(source_file)
            hosts = {}
            peaks = {'source': 0, 'receiver': 0}
            stop = threading.Event()
            try:
                for role in peaks:
                    directory = root / role
                    directory.mkdir()
                    hosts[role] = Helper(helper, directory, role)
                source, receiver = hosts['source'], hosts['receiver']
                for host in hosts.values():
                    host.rpc('settings.update', {'sendLimitBytesPerSecond': 0, 'receiveLimitBytesPerSecond': 0})
                requested = source.rpc('peer.connect', {'url': receiver.initial['urls'][-1], 'fingerprint': receiver.initial['fingerprint']})
                receiver.rpc('pair.approveTrusted', {'id': requested['requestId']})
                deadline = time.monotonic() + args.timeout
                while True:
                    peer = next((d for d in source.rpc('status')['devices'] if d['id'] == receiver.initial['id'] and d['paired']), None)
                    if peer:
                        break
                    if time.monotonic() > deadline:
                        raise RuntimeError('Pairing deadline exceeded')
                    time.sleep(.02)

                def monitor():
                    while not stop.wait(.1):
                        for role, host in hosts.items():
                            try:
                                rss = subprocess.check_output(['ps', '-o', 'rss=', '-p', str(host.process.pid)], text=True, stderr=subprocess.DEVNULL, timeout=2).strip()
                                peaks[role] = max(peaks[role], int(rss) * 1024)
                            except (OSError, ValueError, subprocess.SubprocessError):
                                pass

                monitor_thread = threading.Thread(target=monitor, daemon=True)
                monitor_thread.start()
                compact = source.initial['version'].startswith('0.12.')
                status_method = 'status.summary' if compact else 'status'
                started = time.monotonic()
                if compact:
                    job = source.rpc('preparation.start', {'deviceId': peer['id'], 'paths': [str(source_file)], 'requestId': __import__('uuid').uuid4().hex})
                    while job['state'] == 'preparing':
                        if time.monotonic() - started > args.timeout: raise RuntimeError('Preparation deadline exceeded')
                        time.sleep(.02)
                        job = source.rpc('preparation.status', {'id': job['id']})
                    if job['state'] != 'completed': raise RuntimeError('Preparation failed: ' + str(job))
                    offered = {'id': job['transferId']}
                else:
                    offered = source.rpc('offer', {'deviceId': peer['id'], 'paths': [str(source_file)]}, timeout=args.timeout)
                stages = {'preparationComplete': time.monotonic() - started}
                deadline = started + args.timeout
                last_received = 0
                samples = []
                incoming = None
                while True:
                    now = time.monotonic()
                    if now > deadline:
                        raise RuntimeError('Transfer deadline exceeded')
                    local = source.rpc(status_method)
                    remote = receiver.rpc(status_method)
                    sent = next(t for t in local['transfers'] if t['id'] == offered['id'])
                    incoming = next((t for t in remote['transfers'] if t['deviceId'] == source.initial['id']), None)
                    for transfer in (sent, incoming):
                        if transfer and transfer['state'] in ('failed', 'cancelled'):
                            raise RuntimeError('Transfer failed: ' + transfer.get('error', transfer['state']))
                    if sent['state'] == 'running':
                        stages.setdefault('sourceRunning', time.monotonic() - started)
                    if incoming:
                        stages.setdefault('incomingOffer', time.monotonic() - started)
                        count = incoming['completedBytes']
                        if count != last_received:
                            samples.append({'seconds': round(time.monotonic() - started, 6), 'confirmedBytes': count})
                            last_received = count
                        if count > 0:
                            stages.setdefault('firstConfirmedBlock', time.monotonic() - started)
                        if count == size:
                            stages.setdefault('allPayloadConfirmed', time.monotonic() - started)
                        if incoming['state'] == 'completed':
                            stages.setdefault('receiverComplete', time.monotonic() - started)
                    if sent['state'] == 'completed' and incoming and incoming['state'] == 'completed':
                        stages['bothComplete'] = time.monotonic() - started
                        break
                    time.sleep(.02)
                result = receiver.rpc('transfer.received', {'id': incoming['id']})
                destination = Path(result['items'][0]['path'])
                received_files, received_directories = inventory(destination)
                if received_files != expected_files or received_directories != expected_directories:
                    raise RuntimeError('Independent file/directory/size/SHA-256 comparison failed')
                if inventory(source_file) != (expected_files, expected_directories):
                    raise RuntimeError('Original benchmark source changed')
                independent = hashlib.sha256(json.dumps(expected_files, sort_keys=True).encode()).hexdigest()
                stop.set()
                monitor_thread.join(timeout=3)
                record = {'helperVersion': source.initial['version'], 'helperSHA256': helper_digest, 'iteration': iteration + 1,
                          'bytes': size, 'files': len(expected_files), 'directories': len(expected_directories), 'manifestSHA256': independent, 'timingsSeconds': {k: round(v, 6) for k, v in stages.items()},
                          'MiBPerSecondIncludingPreparation': round(size / MIB / stages['bothComplete'], 3), 'sampledRSSBytes': peaks,
                          'samples': samples, 'scope': 'same-host actual two-service TLS transfer, unlimited rates, non-sparse source, no GUI'}
                results.append(record)
                print(json.dumps({key: value for key, value in record.items() if key != 'samples'}), flush=True)
            finally:
                stop.set()
                for host in hosts.values():
                    host.stop()
    if args.report:
        Path(args.report).write_text(json.dumps(results, indent=2) + '\n')
    return results


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('helper')
    parser.add_argument('--source', help='Read-only identical source fixture for comparisons')
    parser.add_argument('--mib', type=int, default=300)
    parser.add_argument('--repeat', type=int, default=3)
    parser.add_argument('--timeout', type=int, default=180)
    parser.add_argument('--report')
    run(parser.parse_args())
