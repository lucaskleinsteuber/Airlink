import { readFileSync } from 'node:fs';
import { Script, runInNewContext } from 'node:vm';
import { describe, expect, it } from 'vitest';
// Go raw string literals discard carriage returns. Match the actual embedded
// source even when a Windows checkout uses CRLF on disk.
const go = readFileSync(new URL('../../internal/server/control.go', import.meta.url), 'utf8').replace(/\r\n/g, '\n');
const html = go.slice(go.indexOf('const adminHTML = `') + 'const adminHTML = `'.length, go.lastIndexOf('`'));
const script = html.match(/<script nonce="NONCE">([\s\S]*?)<\/script>/)![1];
describe('offline console protocol helpers', () => {
  it('parses the inline module and resolves all referenced DOM ids', () => {
    expect(() => new Script(script)).not.toThrow();
    const ids = [...html.matchAll(/\bid="([^"]+)"/g)].map(match => match[1]); expect(new Set(ids).size).toBe(ids.length);
    for (const match of script.matchAll(/el\('([^']+)'\)/g)) expect(ids).toContain(match[1]);
  });
  it('preserves several source paths pasted with Windows or Unix line endings', () => {
    const pattern = script.match(/el\('share-paths'\)\.value\.split\((\/[^)]+\/)\)/)![1];
    const paths = runInNewContext(`value.split(${pattern}).map(p=>p.trim()).filter(Boolean)`, { value: 'C:\\Users\\example\\first.txt\r\nC:\\Users\\example\\second.txt\nC:\\Users\\example\\third.txt\n' });
    expect(paths).toEqual(['C:\\Users\\example\\first.txt', 'C:\\Users\\example\\second.txt', 'C:\\Users\\example\\third.txt']);
  });
  it('uses explicit clipboard controls and text nodes for user content', () => {
    expect(script).not.toContain('clipboardchange'); expect(script).not.toContain('innerHTML');
    expect(script).toContain("el('clipboard-paste').addEventListener('click'");
    expect(script).toContain("el('clipboard-form').addEventListener('submit'");
  });
  it('converts KiB/s limits without silently turning a positive subminimum limit into unlimited', () => {
    const definition = script.match(/function limitBytes\(value\)\{[\s\S]*?\}\n/)![0];
    const convert = (value: string) => runInNewContext(`${definition};limitBytes(value)`, { value, t: (key: string) => key });
    expect(convert('0')).toBe(0); expect(convert('1')).toBe(1024); expect(convert('1.5')).toBe(1536); expect(convert('1073741824')).toBe(1099511627776);
    for (const value of ['', '-1', '0.0001', '0.5', 'Infinity', '1073741825']) expect(() => convert(value)).toThrow('invalidLimit');
  });
});
