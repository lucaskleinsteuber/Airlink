#!/usr/bin/env python3
"""Opt-in real daemon acceptance. No app, browser, Finder or input automation.

The source is a sparse fixture with nonzero markers. Every logical byte must
still traverse AirLink's TLS/chunk protocol into a separate received file.
Only temporary fixture/state directories are removed at completion.
"""
import argparse
import hashlib
import json
import pathlib
import queue
import shutil
import subprocess
import sys
import tempfile
import threading
import time
import urllib.parse

MIB = 1024 ** 2
GIB = 1024 ** 3


class Helper:
    def __init__(self, executable, root, role, port=0):
        self.root, self.role, self.sequence = root, role, 0
        self.messages, self.errors = queue.Queue(), []
        self.process = subprocess.Popen(
            [str(executable), '--stdio', '--no-discovery', '--data-dir', str(root / 'state'),
             '--receive-dir', str(root / 'received'), '--port', str(port)],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            text=True, bufsize=1)

        def output():
            try:
                for line in self.process.stdout:
                    self.messages.put(json.loads(line))
            finally:
                self.messages.put(None)

        def diagnostics():
            for line in self.process.stderr:
                self.errors.append(line.rstrip())
                del self.errors[:-12]

        threading.Thread(target=output, daemon=True).start()
        threading.Thread(target=diagnostics, daemon=True).start()
        self.initial = self.rpc('status', timeout=180)
        self.port = int(urllib.parse.urlsplit(self.initial['urls'][-1]).port)

    def rpc(self, method, params=None, timeout=30):
        self.sequence += 1
        identifier = self.sequence
        self.process.stdin.write(json.dumps({'id': identifier, 'method': method, 'params': params}) + '\n')
        self.process.stdin.flush()
        try:
            answer = self.messages.get(timeout=timeout)
        except queue.Empty:
            raise RuntimeError(self.role + ' RPC timeout: ' + method)
        if answer is None:
            raise RuntimeError(self.role + ' exited: ' + '\n'.join(self.errors))
        if answer.get('id') != identifier or answer.get('error'):
            raise RuntimeError(self.role + ' RPC failed: ' + str(answer))
        return answer['result']

    def stop(self, abrupt=False):
        if self.process.poll() is None:
            if abrupt:
                self.process.kill()
            else:
                try:
                    self.rpc('shutdown', timeout=10)
                except (OSError, RuntimeError):
                    self.process.kill()
            try:
                self.process.wait(timeout=20)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=10)
        for stream in (self.process.stdin, self.process.stdout, self.process.stderr):
            try:
                stream.close()
            except OSError:
                pass


def run(args):
    executable = pathlib.Path(args.helper).resolve()
    size = args.mib * MIB if args.mib else args.gib * GIB
    if size < 64 * MIB or size % (8 * MIB):
        raise ValueError('Choose at least 64 MiB and a multiple of 8 MiB')
    started = time.monotonic()
    deadline = started + args.timeout
    monitor_thread = None
    helpers, peaks, monitor_errors = {}, {'source': 0, 'receiver': 0}, []
    stopped = threading.Event()
    events = []

    def event(name, **values):
        entry = {'event': name, 'seconds': round(time.monotonic() - started, 2), **values}
        events.append(entry)
        print(json.dumps(entry), flush=True)

    with tempfile.TemporaryDirectory(prefix='AirLink-single-file-') as temporary:
        base = pathlib.Path(temporary)
        if shutil.disk_usage(base).free < size + args.reserve_gib * GIB:
            raise RuntimeError('Not enough free space for received fixture plus reserve')
        source_file = base / 'single-file.bin'
        with source_file.open('wb') as fixture:
            fixture.truncate(size)
            marker = bytes(range(256)) * 4096
            for offset in (0, size // 2, size - len(marker)):
                fixture.seek(offset)
                fixture.write(marker)
        if shutil.disk_usage(base).free < size + args.reserve_gib * GIB:
            raise RuntimeError('Fixture allocation left insufficient destination space')

        def monitor():
            while not stopped.wait(0.5):
                if shutil.disk_usage(base).free < 16 * GIB:
                    monitor_errors.append('Free disk space dropped below the 16 GiB abort threshold')
                for role, helper in list(helpers.items()):
                    if helper.process.poll() is not None:
                        continue
                    try:
                        measured = subprocess.check_output(
                            ['ps', '-o', 'rss=', '-p', str(helper.process.pid)],
                            stderr=subprocess.DEVNULL, text=True, timeout=2).strip()
                        peaks[role] = max(peaks[role], int(measured) * 1024)
                    except (OSError, ValueError, subprocess.SubprocessError):
                        pass

        def check():
            if time.monotonic() > deadline:
                raise RuntimeError('Acceptance deadline exceeded')
            if monitor_errors:
                raise RuntimeError(monitor_errors[0])

        def restart(role):
            previous = helpers[role]
            identity = previous.initial
            previous.stop(abrupt=True)
            helpers[role] = Helper(executable, previous.root, role, previous.port)
            current = helpers[role].initial
            assert current['id'] == identity['id'] and current['fingerprint'] == identity['fingerprint'], 'identity changed on restart'

        def full_hash(path, label):
            last_hash_report = time.monotonic()
            result = hashlib.sha256()
            with path.open('rb') as content:
                while True:
                    check()
                    block = content.read(4 * MIB)
                    if not block:
                        return result.hexdigest()
                    result.update(block)
                    if time.monotonic()-last_hash_report>=10:
                        last_hash_report=time.monotonic()
                        event('independent-hash-progress', role=label, bytesRead=content.tell())

        try:
            for role in ('source', 'receiver'):
                (base / role).mkdir()
                helpers[role] = Helper(executable, base / role, role)
                if args.expect_version:
                    assert helpers[role].initial['version'] == args.expect_version
            monitor_thread = threading.Thread(target=monitor, daemon=True)
            monitor_thread.start()
            source, receiver = helpers['source'], helpers['receiver']
            requested = source.rpc('peer.connect', {'url': receiver.initial['urls'][-1], 'fingerprint': receiver.initial['fingerprint']})
            receiver.rpc('pair.approve', {'id': requested['requestId']})
            while True:
                check()
                local = source.rpc('status')
                peer = next((d for d in local['devices'] if d['id'] == receiver.initial['id']), None)
                if peer:
                    break
                time.sleep(0.1)
            remote = receiver.rpc('status')
            device = next(d for d in remote['devices'] if d['id'] == source.initial['id'])
            device['autoAccept'] = True
            receiver.rpc('device.update', device)
            # Small harness validation is intentionally slow enough to exercise
            # all interruption boundaries before its single file completes.
            if size <= GIB:
                source.rpc('settings.update', {'sendLimitBytesPerSecond': 8 * MIB})
            offered = source.rpc('offer', {'deviceId': peer['id'], 'paths': [str(source_file)]})
            event('offered', bytes=size, fixture='sparse source with nonzero markers; all bytes transferred')
            paused = receiver_restarted = source_restarted = False
            last_report = 0
            while True:
                check()
                local = helpers['source'].rpc('status')
                remote = helpers['receiver'].rpc('status')
                sent = next(t for t in local['transfers'] if t['id'] == offered['id'])
                incoming = next((t for t in remote['transfers'] if t['deviceId'] == source.initial['id']), None)
                for transfer in (sent, incoming):
                    if transfer and transfer['state'] in ('failed', 'cancelled'):
                        raise RuntimeError('Transfer failed: ' + str(transfer))
                confirmed = incoming['completedBytes'] if incoming else 0
                if not paused and sent['completedBytes'] >= min(16 * MIB, size // 8):
                    helpers['source'].rpc('transfer.pause', {'id': sent['id']})
                    state = helpers['source'].rpc('status')
                    assert next(t for t in state['transfers'] if t['id'] == offered['id'])['state'] == 'paused'
                    event('paused', confirmedBytes=confirmed)
                    helpers['source'].rpc('transfer.resume', {'id': sent['id']})
                    paused = True
                elif paused and not receiver_restarted and confirmed >= size // 3:
                    before = confirmed
                    previous = helpers['receiver']
                    previous.stop(abrupt=True)
                    reconnect_deadline = time.monotonic() + 15
                    while time.monotonic() < reconnect_deadline:
                        state = helpers['source'].rpc('status')
                        if next(t for t in state['transfers'] if t['id'] == offered['id'])['state'] == 'reconnecting':
                            break
                        time.sleep(0.1)
                    else:
                        raise RuntimeError('Source did not detect receiver interruption')
                    helpers['receiver'] = Helper(executable, previous.root, 'receiver', previous.port)
                    assert helpers['receiver'].initial['id'] == receiver.initial['id']
                    assert helpers['receiver'].initial['fingerprint'] == receiver.initial['fingerprint']
                    resumed = next(t for t in helpers['receiver'].initial['transfers'] if t['id'] == incoming['id'])
                    assert resumed['completedBytes'] >= before, 'receiver lost confirmed data'
                    receiver_restarted = True
                    event('receiver-crash-recovered', confirmedBytes=resumed['completedBytes'])
                elif receiver_restarted and not source_restarted and confirmed >= 2 * size // 3:
                    restart('source')
                    source_restarted = True
                    event('source-crash-recovered', confirmedBytes=confirmed)
                if sent['state'] == 'completed' and incoming and incoming['state'] == 'completed':
                    assert paused and receiver_restarted and source_restarted, 'fixture completed before interruption coverage'
                    assert sent['completedBytes'] == size and incoming['completedBytes'] == size
                    assert not sent.get('skippedBytes') and not incoming.get('skippedBytes')
                    break
                if time.monotonic() - last_report >= 10:
                    last_report = time.monotonic()
                    event('progress', confirmedBytes=confirmed, sourceState=sent['state'], receiverState=incoming['state'] if incoming else 'waiting', peakRSSBytes=dict(peaks))
                time.sleep(0.1 if size <= GIB else 0.5)
            transfer_seconds = time.monotonic() - started
            received_file = pathlib.Path(incoming['destination']) / incoming['files'][0].get('destinationPath', incoming['files'][0]['path'])
            assert received_file.stat().st_size == size
            event('transfer-completed', confirmedBytes=incoming['completedBytes'])
            expected_hash, received_hash = full_hash(source_file, 'source'), full_hash(received_file, 'receiver')
            assert expected_hash == received_hash == incoming['files'][0]['sha256'], 'independent final SHA-256 mismatch'
            if sys.platform in ('darwin', 'linux'):
                assert all(0 < value < args.max_rss_mib * MIB for value in peaks.values()), 'service RSS exceeded target or was not observed: ' + str(peaks)
            helper_digest=hashlib.sha256(executable.read_bytes()).hexdigest()
            report = {'helperSHA256': helper_digest, 'version': local['version'], 'bytes': size, 'singleFile': True, 'transport': 'native service to service, TLS 1.3, same-host loopback',
                      'transferAndRecoverySeconds': round(transfer_seconds, 2), 'totalSeconds': round(time.monotonic() - started, 2),
                      'sha256': received_hash, 'peakRSSBytes': peaks, 'rssSamplingSeconds': 0.5,
                      'sourcePayloadCounter': local['statistics']['trafficSentBytes'], 'receiverPayloadCounter': remote['statistics']['trafficReceivedBytes'],
                      'counterNote': 'Lifetime counters checkpoint periodically; abrupt process kills may lose recent counter samples, not confirmed file receipts.',
                      'events': events, 'physicalDevicesTested': False, 'guiAutomation': False}
            if args.report:
                pathlib.Path(args.report).write_text(json.dumps(report, indent=2) + '\n')
            event('PASS', bytes=size, sha256=received_hash, peakRSSBytes=peaks)
            return report
        finally:
            stopped.set()
            if monitor_thread is not None:
                monitor_thread.join(timeout=5)
            for helper in helpers.values():
                helper.stop()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('helper')
    parser.add_argument('--gib', type=int, default=100)
    parser.add_argument('--mib', type=int, default=0, help='small background harness validation, minimum 64')
    parser.add_argument('--timeout', type=int, default=3600)
    parser.add_argument('--reserve-gib', type=int, default=32)
    parser.add_argument('--max-rss-mib', type=int, default=256)
    parser.add_argument('--expect-version')
    parser.add_argument('--report')
    run(parser.parse_args())
