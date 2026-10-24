#!/usr/bin/env python3
"""Headless packaged-service smoke test. Never launches the macOS GUI."""
import hashlib
import json
import pathlib
import re
import ssl
import subprocess
import sys
import tempfile
import urllib.error
import urllib.request

with tempfile.TemporaryDirectory(prefix="AirLink-binary-smoke-") as directory:
    base = pathlib.Path(directory)
    process = subprocess.Popen(
        [str(pathlib.Path(sys.argv[1]).resolve()), "--stdio", "--no-discovery",
         "--data-dir", str(base / "state"), "--receive-dir", str(base / "received")],
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True,
    )
    try:
        def rpc(identifier, method):
            process.stdin.write(json.dumps({"id": identifier, "method": method}) + "\n")
            process.stdin.flush()
            response = json.loads(process.stdout.readline())
            assert response.get("id") == identifier and "error" not in response, response
            return response["result"]

        status = rpc(1, "status")
        assert status["version"] == pathlib.Path("VERSION").read_text().strip()
        certificate = pathlib.Path(status["certificatePath"]).read_text()
        assert hashlib.sha256(ssl.PEM_cert_to_DER_cert(certificate)).hexdigest() == status["fingerprint"]
        url = next(value for value in status["urls"] if "localhost:" in value)

        def get(path):
            # Apple's bundled Python may use LibreSSL without TLS 1.3. Node is
            # already a build dependency and verifies TLS 1.3 with the same CA.
            script = """
const https=require('node:https'),fs=require('node:fs');
const req=https.get(process.argv[2],{ca:fs.readFileSync(process.argv[1]),minVersion:'TLSv1.3'},res=>{
const parts=[];res.on('data',part=>parts.push(part));res.on('end',()=>process.stdout.write(JSON.stringify({status:res.statusCode,body:Buffer.concat(parts).toString('base64')})));});
req.setTimeout(15000,()=>req.destroy(Error('timeout')));req.on('error',e=>{process.stderr.write(e.message);process.exitCode=1});
"""
            response = json.loads(subprocess.check_output(["node", "-e", script, status["certificatePath"], url + path],text=True))
            if response["status"] >= 400:
                raise urllib.error.HTTPError(url + path, response["status"], "HTTP error", {}, None)
            import base64
            return base64.b64decode(response["body"])

        index = get("/").decode()
        resources = re.findall(r'(?:src|href)="(/assets/[^\"]+)"', index)
        assert len(resources) >= 2, "production HTML does not contain built application assets"
        for resource in resources:
            data = get(resource)
            assert len(data) > 100
        try:
            get("/api/v1/status")
            raise AssertionError("private status accessible without pairing")
        except urllib.error.HTTPError as error:
            assert error.code == 401
        setup_url = next(value for value in status["setupURLs"] if "localhost:" in value)
        with urllib.request.urlopen(setup_url, timeout=15) as response:
            assert status["fingerprint"].encode() in response.read()
        print("PASS: final binary serves embedded production assets over verified TLS; private status requires pairing; public bootstrap fingerprint matches")
        rpc(2, "shutdown")
        assert process.wait(timeout=10) == 0
    finally:
        if process.poll() is None:
            process.terminate()
            process.wait(timeout=10)
