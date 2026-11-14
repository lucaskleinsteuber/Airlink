#!/usr/bin/env python3
"""Real two-computer AirLink transfer benchmark (development tool).

Runs one headless helper on this computer and one on a remote Linux/macOS
computer reached over SSH. Both helpers use temporary state and receive
directories; the remote helper's private stdio control channel runs through the
SSH pipe. The script pairs both helpers with the comparison-code flow, sends
generated fixtures in both directions and verifies every received file with an
independent SHA-256 inventory. Installed AirLink apps and their data are never
touched.

Example:
  scripts/bench-remote.py --local-helper build/bench/airlink-darwin-arm64 \
      --remote-helper build/bench/airlink-linux-amd64 --remote lucas@192.168.178.22 \
      --ssh-key ~/.ssh/cachyos_claude --cases large:512,small:1000x8,mixed:200 \
      --report docs/evidence/bench.json
"""
import argparse
import hashlib
import json
import os
import pathlib
import queue
import random
import shutil
import subprocess
import sys
import tempfile
import threading
import time
import uuid

MIB = 1024 ** 2

# Executed with the interpreter on either computer: fixture generation and an
# independent inventory (relative path -> size and SHA-256).
AGENT = r'''
import hashlib, json, os, pathlib, random, shutil, sys

def generate(kind, root, spec):
    root = pathlib.Path(root)
    if root.exists():
        shutil.rmtree(root) if root.is_dir() else root.unlink()
    rng = random.Random(spec)
    def write(path, size):
        path.parent.mkdir(parents=True, exist_ok=True)
        with open(path, 'wb') as out:
            remaining = size
            while remaining:
                piece = min(remaining, 4 << 20)
                out.write(os.urandom(piece))
                remaining -= piece
    if kind == 'large':
        write(root, int(spec) << 20)
    elif kind == 'small':
        count, kib = (int(v) for v in spec.split('x'))
        for i in range(count):
            write(root / ('dir%02d' % (i % 20)) / ('file%05d.bin' % i), kib * 1024)
    elif kind == 'mixed':
        total, budget, i = 0, int(spec) << 20, 0
        while total < budget:
            size = min(int(rng.lognormvariate(11.5, 2.0)), 64 << 20, budget - total) or 1
            write(root / ('album%02d' % (i % 7)) / ('item%04d.dat' % i), size)
            total += size
            i += 1
        (root / 'empty-folder').mkdir()
    else:
        raise SystemExit('unknown fixture kind ' + kind)

def inventory(root):
    root = pathlib.Path(root)
    paths = [root] if root.is_file() else sorted(p for p in root.rglob('*') if p.is_file())
    files = {}
    for item in paths:
        with open(item, 'rb') as content:
            digest = hashlib.file_digest(content, 'sha256').hexdigest()
        files['.' if root.is_file() else item.relative_to(root).as_posix()] = [item.stat().st_size, digest]
    directories = [] if root.is_file() else sorted(p.relative_to(root).as_posix() for p in root.rglob('*') if p.is_dir())
    return {'files': files, 'directories': directories}

command = sys.argv[1]
if command == 'generate':
    generate(sys.argv[2], sys.argv[3], sys.argv[4])
    print(json.dumps(inventory(sys.argv[3])))
elif command == 'inventory':
    print(json.dumps(inventory(sys.argv[2])))
elif command == 'clear':
    target = pathlib.Path(sys.argv[2])
    for child in target.iterdir() if target.exists() else []:
        if child.name == '.airlink-partials':
            continue
        shutil.rmtree(child) if child.is_dir() and not child.is_symlink() else child.unlink()
    print('{}')
'''


class Host:
    """One helper process and its private newline-delimited JSON channel."""

    def __init__(self, role, command, workdir, run_agent):
        self.role, self.workdir, self.run_agent = role, workdir, run_agent
        self.sequence, self.messages, self.errors = 0, queue.Queue(), []
        self.lock = threading.Lock()
        self.process = subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, bufsize=1)
        threading.Thread(target=self._output, daemon=True).start()
        threading.Thread(target=self._diagnostics, daemon=True).start()
        self.status = self.rpc('status.summary', timeout=120)

    def _output(self):
        try:
            for line in self.process.stdout:
                self.messages.put(json.loads(line))
        finally:
            self.messages.put(None)

    def _diagnostics(self):
        for line in self.process.stderr:
            self.errors.append(line.rstrip())
            del self.errors[:-20]

    def rpc(self, method, params=None, timeout=60):
        with self.lock:
            self.sequence += 1
            identifier = self.sequence
            self.process.stdin.write(json.dumps({'id': identifier, 'method': method, 'params': params or {}}) + '\n')
            self.process.stdin.flush()
            deadline = time.monotonic() + timeout
            while True:
                try:
                    answer = self.messages.get(timeout=max(0.01, deadline - time.monotonic()))
                except queue.Empty:
                    raise RuntimeError('%s RPC timeout: %s' % (self.role, method))
                if answer is None:
                    raise RuntimeError('%s helper exited: %s' % (self.role, '\n'.join(self.errors)))
                if answer.get('id') == identifier:
                    break
            if answer.get('error'):
                raise RuntimeError('%s %s: %s' % (self.role, method, answer['error']))
            return answer.get('result')

    def agent(self, *args):
        return json.loads(self.run_agent(*args))

    def stop(self):
        try:
            self.process.stdin.close()
            self.process.wait(timeout=20)
        except Exception:
            self.process.kill()


def ssh_base(args):
    base = ['ssh', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=10']
    if args.ssh_key:
        base += ['-i', os.path.expanduser(args.ssh_key), '-o', 'IdentitiesOnly=yes']
    return base + [args.remote]


def lan_url(status, prefix):
    for url in status['urls']:
        if url.startswith('https://' + prefix):
            return url
    raise RuntimeError('No address in %s for %s' % (prefix, status['name']))


def pair(initiator, responder, responder_url, timeout):
    try:
        responder.rpc('pairing.listen')  # 0.13+: accept requests only while "Add device" is open
    except RuntimeError:
        pass
    session = initiator.rpc('pairing.start', {'url': responder_url, 'fingerprint': responder.status['fingerprint']})
    deadline = time.monotonic() + timeout
    code, remote_session = None, None
    while time.monotonic() < deadline:
        session = initiator.rpc('pairing.status', {'id': session['id']})
        if session['state'] in ('denied', 'cancelled', 'expired'):
            raise RuntimeError('Pairing failed: %s' % session)
        if session['state'] == 'comparing':
            code = session['comparisonCode']
            remote_session = next((p for p in responder.rpc('status.summary').get('pairingSessions') or [] if p.get('comparisonCode') == code), None)
            if remote_session:
                break
        time.sleep(0.05)
    if not remote_session:
        raise RuntimeError('Pairing did not reach the comparison step')
    initiator.rpc('pairing.confirm', {'id': session['id'], 'automaticReceive': True})
    responder.rpc('pairing.confirm', {'id': remote_session['id'], 'automaticReceive': True})
    while time.monotonic() < deadline:
        a = any(d['id'] == responder.status['id'] and d.get('paired') for d in initiator.rpc('status.summary')['devices'])
        b = any(d['id'] == initiator.status['id'] and d.get('paired') for d in responder.rpc('status.summary')['devices'])
        if a and b:
            return code
        time.sleep(0.05)
    raise RuntimeError('Pairing did not complete')


def send(source, receiver, source_path, total_bytes, timeout, poll):
    started = time.monotonic()
    stages = {}
    job = source.rpc('preparation.start', {'deviceId': receiver.status['id'], 'paths': [source_path], 'requestId': uuid.uuid4().hex})
    while job['state'] == 'preparing':
        if time.monotonic() - started > timeout:
            raise RuntimeError('Preparation deadline exceeded')
        time.sleep(poll)
        job = source.rpc('preparation.status', {'id': job['id']})
    if job['state'] != 'completed':
        raise RuntimeError('Preparation failed: %s' % job)
    stages['prepared'] = time.monotonic() - started
    transfer_id = job['transferId']
    samples = []
    while True:
        now = time.monotonic() - started
        if now > timeout:
            raise RuntimeError('Transfer deadline exceeded')
        sent = source.rpc('transfer.get', {'id': transfer_id})
        if sent['state'] in ('failed', 'cancelled'):
            raise RuntimeError('Transfer %s: %s' % (sent['state'], sent.get('error')))
        if sent['completedBytes'] > 0:
            stages.setdefault('firstConfirmedBytes', now)
        if not samples or samples[-1][1] != sent['completedBytes']:
            samples.append((round(now, 3), sent['completedBytes']))
        if sent['state'] == 'completed':
            stages['sourceCompleted'] = now
            break
        time.sleep(poll)
    while True:
        incoming = next((t for t in receiver.rpc('status.summary')['transfers'] if t['deviceId'] == source.status['id'] and t['state'] in ('completed', 'failed', 'cancelled')), None)
        if incoming:
            if incoming['state'] != 'completed':
                raise RuntimeError('Receiver %s: %s' % (incoming['state'], incoming.get('error')))
            stages['receiverCompleted'] = time.monotonic() - started
            break
        if time.monotonic() - started > timeout:
            raise RuntimeError('Receiver completion deadline exceeded')
        time.sleep(poll)
    return stages, samples


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--local-helper', required=True)
    parser.add_argument('--remote-helper', required=True, help='Helper binary for the remote computer (copied over SSH)')
    parser.add_argument('--remote', required=True, help='user@host of the remote computer')
    parser.add_argument('--ssh-key')
    parser.add_argument('--remote-port', type=int, default=43871, help='Must be allowed by the remote firewall')
    parser.add_argument('--local-port', type=int, default=43881)
    parser.add_argument('--subnet', default='192.168.178.', help='Address prefix of the shared LAN')
    parser.add_argument('--cases', default='large:512,small:1000x8,mixed:200')
    parser.add_argument('--directions', default='local-to-remote,remote-to-local')
    parser.add_argument('--repeat', type=int, default=1)
    parser.add_argument('--timeout', type=int, default=3600)
    parser.add_argument('--poll', type=float, default=0.1)
    parser.add_argument('--label', default='')
    parser.add_argument('--initiator', choices=['local', 'remote'], default='local',
                        help='Computer that starts pairing; use remote when the remote computer cannot accept connections')
    parser.add_argument('--report')
    args = parser.parse_args()

    ssh = ssh_base(args)
    local_helper = pathlib.Path(args.local_helper).resolve()
    remote_binary = pathlib.Path(args.remote_helper).resolve()
    digest = hashlib.sha256(remote_binary.read_bytes()).hexdigest()
    remote_root = 'airlink-dev/run-%s' % uuid.uuid4().hex[:8]
    remote_helper = 'airlink-dev/helper-%s' % digest[:12]
    subprocess.run(ssh + ['mkdir -p %s/state %s/received %s/fixtures' % (remote_root, remote_root, remote_root)], check=True)
    subprocess.run(['scp', '-q'] + ssh[1:-1] + [str(remote_binary), '%s:%s' % (args.remote, remote_helper)], check=True)
    subprocess.run(ssh + ['chmod +x ' + remote_helper], check=True)
    remote_home = subprocess.run(ssh + ['echo $HOME'], check=True, capture_output=True, text=True).stdout.strip()
    remote_root_abs = remote_home + '/' + remote_root

    def remote_agent(*agent_args):
        return subprocess.run(ssh + ['python3 - ' + ' '.join(agent_args)], input=AGENT, check=True, capture_output=True, text=True).stdout

    local_dir = pathlib.Path(tempfile.mkdtemp(prefix='AirLink-bench-'))
    for name in ('state', 'received', 'fixtures'):
        (local_dir / name).mkdir()

    def local_agent(*agent_args):
        return subprocess.run([sys.executable, '-I', '-c', AGENT] + list(agent_args), check=True, capture_output=True, text=True).stdout

    hosts = []
    results = []
    try:
        local = Host('local', [str(local_helper), '--stdio', '--no-discovery', '--data-dir', str(local_dir / 'state'), '--receive-dir', str(local_dir / 'received'), '--port', str(args.local_port)], str(local_dir), local_agent)
        hosts.append(local)
        remote = Host('remote', ssh + ['exec %s/%s --stdio --no-discovery --data-dir %s/state --receive-dir %s/received --port %d' % (remote_home, remote_helper, remote_root_abs, remote_root_abs, args.remote_port)], remote_root_abs, remote_agent)
        hosts.append(remote)
        for host in hosts:
            host.rpc('settings.update', {'sendLimitBytesPerSecond': 0, 'receiveLimitBytesPerSecond': 0})
        started = time.monotonic()
        if args.initiator == 'remote':
            code = pair(remote, local, lan_url(local.status, args.subnet), 120)
        else:
            code = pair(local, remote, lan_url(remote.status, args.subnet), 120)
        pairing_seconds = time.monotonic() - started
        print('paired (%s) in %.2fs: %s %s <-> %s %s' % (code, pairing_seconds, local.status['name'], local.status['version'], remote.status['name'], remote.status['version']), flush=True)
        for case in args.cases.split(','):
            kind, spec = case.split(':')
            for direction in args.directions.split(','):
                source, receiver = (local, remote) if direction == 'local-to-remote' else (remote, local)
                for iteration in range(args.repeat):
                    name = '%s-%s' % (kind, spec.replace('x', 'x'))
                    fixture = source.workdir + '/fixtures/' + name
                    expected = source.agent('generate', kind, fixture, spec)
                    receiver.agent('clear', receiver.workdir + '/received')
                    total = sum(size for size, _ in expected['files'].values())
                    stages, samples = send(source, receiver, fixture, total, args.timeout, args.poll)
                    received = receiver.agent('inventory', receiver.workdir + '/received/' + name)
                    verified = received == expected
                    seconds = stages['receiverCompleted']
                    record = {'label': args.label, 'case': case, 'direction': direction, 'iteration': iteration + 1,
                              'sourceVersion': source.status['version'], 'receiverVersion': receiver.status['version'],
                              'bytes': total, 'files': len(expected['files']), 'directories': len(expected['directories']),
                              'seconds': round(seconds, 3), 'MiBPerSecond': round(total / MIB / seconds, 3),
                              'stagesSeconds': {k: round(v, 3) for k, v in stages.items()}, 'verified': verified,
                              'pairingSeconds': round(pairing_seconds, 3), 'samples': samples}
                    results.append(record)
                    print('%-16s %-16s %8.1f MiB %6d files %8.2fs %7.2f MiB/s first-bytes %.2fs verified=%s' % (case, direction, total / MIB, len(expected['files']), seconds, record['MiBPerSecond'], stages.get('firstConfirmedBytes', -1), verified), flush=True)
                    if not verified:
                        raise RuntimeError('Independent SHA-256 inventory mismatch for ' + case)
                    receiver.agent('clear', receiver.workdir + '/received')
    finally:
        for host in hosts:
            host.stop()
        shutil.rmtree(local_dir, ignore_errors=True)
        subprocess.run(ssh + ['rm -rf ' + remote_root], check=False)
        if args.report:
            pathlib.Path(args.report).write_text(json.dumps({'remote': args.remote.split('@')[-1], 'results': results}, indent=2) + '\n')


if __name__ == '__main__':
    main()
