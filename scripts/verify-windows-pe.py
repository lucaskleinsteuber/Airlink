#!/usr/bin/env python3
"""Inspect the published GUI executable without executing Windows code."""
import argparse
import json
import struct
from pathlib import Path


def inspect(path: Path, architecture: str) -> dict:
    data = path.read_bytes()
    def u16(at): return struct.unpack_from('<H', data, at)[0]
    def u32(at): return struct.unpack_from('<I', data, at)[0]
    if data[:2] != b'MZ': raise ValueError('Missing DOS executable header')
    pe = u32(0x3c)
    if data[pe:pe+4] != b'PE\0\0': raise ValueError('Missing PE signature')
    machine = u16(pe+4)
    expected = {'x64': 0x8664, 'arm64': 0xaa64}[architecture]
    if machine != expected: raise ValueError(f'Wrong PE machine: {machine:#x}, expected {expected:#x}')
    optional = pe+24
    if u16(optional) != 0x20b: raise ValueError('Expected a 64-bit PE32+ optional header')
    subsystem = u16(optional+68)
    if subsystem != 2: raise ValueError(f'Executable would open a console (subsystem {subsystem})')
    section_start = optional+u16(pe+20)
    sections = []
    for index in range(u16(pe+6)):
        start = section_start+index*40
        sections.append((u32(start+12), u32(start+8), u32(start+20), u32(start+16)))
    def offset(rva):
        for virtual, size, raw, raw_size in sections:
            if virtual <= rva < virtual+max(size, raw_size): return raw+rva-virtual
        raise ValueError(f'Unmapped PE RVA: {rva:#x}')
    resource_rva = u32(optional+112+8*2)
    if not resource_rva: raise ValueError('Missing native resources')
    root = offset(resource_rva)
    count = u16(root+12)+u16(root+14)
    resource_types = []
    for index in range(count):
        name = u32(root+16+index*8)
        if not name & 0x80000000: resource_types.append(name)
    # Windows chooses taskbar/Explorer icons from native resources, not managed WPF data.
    for required in (3, 14, 16, 24):
        if required not in resource_types: raise ValueError(f'Missing native resource type {required}')
    return {'path': str(path), 'architecture': architecture, 'machine': hex(machine),
            'subsystem': subsystem, 'resourceTypes': resource_types, 'bytes': len(data)}

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('executable', type=Path)
    parser.add_argument('--arch', required=True, choices=['x64', 'arm64'])
    args = parser.parse_args()
    print(json.dumps(inspect(args.executable, args.arch), indent=2))
