#!/usr/bin/env python3
"""Compare two actual helpers on immutable, dense fixtures. No GUI or user data."""
import argparse
import importlib.util
import json
from pathlib import Path
import os
import shutil
import tempfile
from types import SimpleNamespace

spec=importlib.util.spec_from_file_location('airlink_benchmark',Path(__file__).with_name('benchmark-transfer.py'))
benchmark=importlib.util.module_from_spec(spec);spec.loader.exec_module(benchmark)
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('baseline');parser.add_argument('candidate');parser.add_argument('--output',required=True)
parser.add_argument('--single-files-only', action='store_true')
args=parser.parse_args()
output=Path(args.output).resolve();output.mkdir(parents=True,exist_ok=True)
if shutil.disk_usage(output).free < 18*1024**3: raise SystemExit('Need fixture/receive space plus a 10 GiB reserve')
results=[]
with tempfile.TemporaryDirectory(prefix='fixtures-',dir=output) as directory:
    fixture=Path(directory)
    block=os.urandom(1024**2)
    for title,size in [('300MiB',300*1024**2),('4GiB',4*1024**3)]:
        source=fixture/(title+'.bin')
        with source.open('wb') as target:
            for _ in range(size//len(block)): target.write(block)
        # Alternate ordering to reduce cache/order bias: three runs per helper.
        for index,label in enumerate(('baseline','candidate','candidate','baseline','baseline','candidate')):
            report=output/(title+'-'+str(index+1)+'-'+label+'.json')
            print(json.dumps({'event':'start','case':title,'helper':label,'run':index+1}),flush=True)
            records=benchmark.run(SimpleNamespace(helper=getattr(args,label),source=str(source),mib=1,repeat=1,timeout=1800,report=str(report)))
            for row in records: row.update(case=title,role=label)
            results.extend(records)
            (output/'results.json').write_text(json.dumps(results,indent=2)+'\n')
        source.unlink()
    if not args.single_files_only:
        source=fixture/'10000-files';source.mkdir();(source/'Empty').mkdir();(source/'Leerer Ordner').mkdir()
        for i in range(10000):
            folder=source/('Grüße-'+str(i//100));folder.mkdir(exist_ok=True)
            (folder/('Datei-'+str(i)+'.bin')).write_bytes(block[i%100: i%100+8192])
        for label in ('baseline','candidate'):
            print(json.dumps({'event':'start','case':'10000-files','helper':label}),flush=True)
            records=benchmark.run(SimpleNamespace(helper=getattr(args,label),source=str(source),mib=1,repeat=1,timeout=3600,report=str(output/('10000-files-'+label+'.json'))))
            for row in records: row.update(case='10000-files',role=label)
            results.extend(records)
            (output/'results.json').write_text(json.dumps(results,indent=2)+'\n')

print(json.dumps({'event':'complete','runs':len(results),'physicalNetworkTested':False}),flush=True)
