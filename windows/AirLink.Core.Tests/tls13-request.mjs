// Headless test fixture only: Node's TLS stack supplies TLS 1.3 on macOS,
// where the .NET HttpClient backend uses legacy SecureTransport. This is
// neither part of AirLink's production client nor a certificate bypass.
import { readFile } from 'node:fs/promises';
import https from 'node:https';

const maximumInput = 128 * 1024;
const maximumBody = 1024 * 1024;
const deadline = setTimeout(() => { process.stderr.write('TLS fixture deadline exceeded\n'); process.exit(1); }, 12000);
try {
  const input = [];
  let inputBytes = 0;
  for await (const chunk of process.stdin) {
    inputBytes += chunk.length;
    if (inputBytes > maximumInput) throw new Error('input bound');
    input.push(chunk);
  }
  const request = JSON.parse(Buffer.concat(input).toString('utf8'));
  const url = new URL(request.url);
  if (url.protocol !== 'https:' || !['POST', 'PUT'].includes(request.method) || url.username || url.password) throw new Error('invalid request');
  const body = Buffer.from(request.bodyBase64, 'base64');
  if (body.length > maximumInput) throw new Error('body bound');
  const ca = await readFile(request.caPath);
  const response = await new Promise((resolve, reject) => {
    const req = https.request(url, {
      method: request.method,
      ca,
      rejectUnauthorized: true,
      minVersion: 'TLSv1.3',
      headers: { ...request.headers, 'Content-Length': body.length },
      agent: false,
    }, res => {
      const output = [];
      let received = 0;
      res.on('data', chunk => {
        received += chunk.length;
        if (received > maximumBody) { res.destroy(); reject(new Error('response bound')); return; }
        output.push(chunk);
      });
      res.on('error', reject);
      res.on('end', () => resolve({
        status: res.statusCode,
        bodyBase64: Buffer.concat(output).toString('base64'),
        setCookies: res.headers['set-cookie'] ?? [],
        protocol: res.socket.getProtocol(),
      }));
    });
    req.setTimeout(8000, () => req.destroy(new Error('request deadline')));
    req.on('error', reject);
    req.end(body);
  });
  process.stdout.write(JSON.stringify(response));
} catch {
  // Request URL, cookies, pairing tokens and response content must stay private.
  process.stderr.write('Verified TLS 1.3 fixture request failed\n');
  process.exitCode = 1;
} finally {
  clearTimeout(deadline);
}
