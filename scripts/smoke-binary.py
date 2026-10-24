#!/usr/bin/env python3
"""Headless packaged-service smoke test. Never launches the macOS GUI."""
import hashlib
import base64
import json
import pathlib
import re
import ssl
import subprocess
import sys
import tempfile
import urllib.error
import urllib.request
import urllib.parse

with tempfile.TemporaryDirectory(prefix="AirLink-binary-smoke-") as directory:
    base = pathlib.Path(directory)
    process = subprocess.Popen(
        [str(pathlib.Path(sys.argv[1]).resolve()), "--stdio", "--no-discovery",
         "--data-dir", str(base / "state"), "--receive-dir", str(base / "received")],
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True,
    )
    try:
        def rpc(identifier, method, params=None):
            process.stdin.write(json.dumps({"id": identifier, "method": method, "params": params}) + "\n")
            process.stdin.flush()
            response = json.loads(process.stdout.readline())
            assert response.get("id") == identifier and "error" not in response, response
            return response["result"]

        status = rpc(1, "status")
        assert status["version"] == pathlib.Path("VERSION").read_text().strip()
        certificate = pathlib.Path(status["certificatePath"]).read_text()
        assert hashlib.sha256(ssl.PEM_cert_to_DER_cert(certificate)).hexdigest() == status["fingerprint"]
        url = next(value for value in status["urls"] if "localhost:" in value)

        def exchange(path, method="GET", body=None, cookie="", headers=None):
            # Apple's bundled Python may use LibreSSL without TLS 1.3. Node is
            # already a build dependency and verifies TLS 1.3 with the same CA.
            script = """
const https=require('node:https'),fs=require('node:fs');
let input='';process.stdin.setEncoding('utf8');process.stdin.on('data',part=>input+=part);process.stdin.on('end',()=>{
const options=JSON.parse(input),headers=options.headers||{};if(options.cookie)headers.Cookie=options.cookie;
const body=options.body==null?null:JSON.stringify(options.body);if(body){headers['Content-Type']='application/json';headers['Content-Length']=Buffer.byteLength(body)}
const req=https.request(process.argv[2],{ca:fs.readFileSync(process.argv[1]),minVersion:'TLSv1.3',method:options.method,headers},res=>{
const parts=[];res.on('data',part=>parts.push(part));res.on('end',()=>process.stdout.write(JSON.stringify({status:res.statusCode,headers:res.headers,body:Buffer.concat(parts).toString('base64')})));});
req.setTimeout(15000,()=>req.destroy(Error('timeout')));req.on('error',e=>{process.stderr.write(e.message);process.exitCode=1});if(body)req.write(body);req.end();});
"""
            response = json.loads(subprocess.check_output(["node", "-e", script, status["certificatePath"], url + path],input=json.dumps({"method": method,"body": body,"cookie": cookie,"headers": headers or {}}),text=True))
            response["data"] = base64.b64decode(response["body"])
            return response

        def get(path):
            response = exchange(path)
            if response["status"] >= 400:
                raise urllib.error.HTTPError(url + path, response["status"], "HTTP error", {}, None)
            return response["data"]

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
        if status["version"] == "0.2.0":
            original = base / "shared-original.txt"
            content = b"AirLink private share smoke fixture.\n"
            original.write_bytes(content)
            share = rpc(10, "share.create", {"paths": [str(original)], "password": "smoke-only-password", "maxDownloads": 1})
            fragment = urllib.parse.urlsplit(share["url"]).fragment
            public = "/api/v1/public/shares/" + share["id"]
            assert b"AirLink" in get("/s/" + share["id"])
            assert exchange(public)["status"] == 401
            assert exchange(public + "/unlock", "POST", {"secret": fragment, "password": "wrong"})["status"] == 401
            unlocked = exchange(public + "/unlock", "POST", {"secret": fragment, "password": "smoke-only-password"})
            assert unlocked["status"] == 200
            cookie = unlocked["headers"]["set-cookie"][0].split(";", 1)[0]
            resource = public + "/files/" + share["files"][0]["id"]
            part = exchange(resource, cookie=cookie, headers={"Range": "bytes=2-7"})
            assert part["status"] == 206 and part["data"] == content[2:8]
            assert exchange(resource, cookie=cookie)["data"] == content
            summary = json.loads(exchange(public, cookie=cookie)["data"])
            assert summary["downloads"] == 1 and not summary.get("url")
            rpc(11, "share.revoke", {"id": share["id"]})
            assert exchange(resource, cookie=cookie)["status"] == 410
            assert original.read_bytes() == content
            empty = base / "empty.txt"
            empty.write_bytes(b"")
            empty_share = rpc(18, "share.create", {"paths": [str(empty)], "maxDownloads": 1})
            empty_public = "/api/v1/public/shares/" + empty_share["id"]
            empty_unlock = exchange(empty_public + "/unlock", "POST", {"secret": urllib.parse.urlsplit(empty_share["url"]).fragment})
            assert empty_unlock["status"] == 200
            empty_cookie = empty_unlock["headers"]["set-cookie"][0].split(";", 1)[0]
            empty_resource = empty_public + "/files/" + empty_share["files"][0]["id"]
            assert exchange(empty_resource, cookie=empty_cookie, headers={"Range": "bytes=10-20"})["status"] == 416
            assert json.loads(exchange(empty_public, cookie=empty_cookie)["data"])["downloads"] == 0
            assert exchange(empty_resource, cookie=empty_cookie)["status"] == 200
            assert json.loads(exchange(empty_public, cookie=empty_cookie)["data"])["downloads"] == 1
            pairing = json.loads(exchange("/api/v1/pair/request", "POST", {"name": "Smoke browser", "kind": "browser"})["data"])
            rpc(12, "pair.approve", {"id": pairing["requestId"]})
            paired = exchange("/api/v1/pair/poll", "POST", pairing)
            assert paired["status"] == 200
            browser_cookie = paired["headers"]["set-cookie"][0].split(";", 1)[0]
            assert exchange("/api/v1/clipboard", "POST", {"text": "blocked"}, browser_cookie)["status"] == 403
            local = rpc(13, "status")
            device = next(device for device in local["devices"] if device["name"] == "Smoke browser")
            device["canClipboard"] = True
            rpc(14, "device.update", device)
            assert exchange("/api/v1/clipboard", "POST", {"text": "Explicit browser text"}, browser_cookie)["status"] == 201
            rpc(15, "clipboard.send", {"deviceId": device["id"], "text": "Explicit host text"})
            guest = json.loads(exchange("/api/v1/status", cookie=browser_cookie)["data"])
            assert guest["clipboardAllowed"] and len(guest["clipboard"]) == 2
            rpc(16, "device.revoke", {"id": device["id"]})
            assert not rpc(17, "status")["clipboard"]
            print("PASS: 0.2 protected share unlock/range/quota/revocation and explicit clipboard permission/guest delivery work in the packaged service")
        rpc(2, "shutdown")
        assert process.wait(timeout=10) == 0
    finally:
        if process.poll() is None:
            process.terminate()
            process.wait(timeout=10)
