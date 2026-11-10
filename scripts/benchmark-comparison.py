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
parser.add_argument('--folder-only', action='store_true')
parser.add_argument('--small-file-count', type=int, default=10000)
parser.add_argument('--folder-repeat', type=int, default=1)
parser.add_argument('--folder-role', choices=('both','candidate'), default='both')
args=parser.parse_args()
if args.single_files_only and args.folder_only: parser.error('Choose single files or folders, not both')
if not 1 <= args.small_file_count <= 100000 or not 1 <= args.folder_repeat <= 10: parser.error('Use 1–100000 files and 1–10 folder repetitions')
output=Path(args.output).resolve();output.mkdir(parents=True,exist_ok=True)
reserve=(2 if args.folder_only else 18)*1024**3
if shutil.disk_usage(output).free < reserve: raise SystemExit('Insufficient fixture/receive reserve')
results=[]
with tempfile.TemporaryDirectory(prefix='fixtures-',dir=output) as directory:
    fixture=Path(directory)
    block=os.urandom(1024**2)
    for title,size in ([] if args.folder_only else [('300MiB',300*1024**2),('4GiB',4*1024**3)]):
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
        case=str(args.small_file_count)+'-files'
        source=fixture/case;source.mkdir();(source/'Empty').mkdir();(source/'Leerer Ordner').mkdir()
        for i in range(args.small_file_count):
            folder=source/('Grüße-'+str(i//100));folder.mkdir(exist_ok=True)
            (folder/('Datei-'+str(i)+'.bin')).write_bytes(block[i%100: i%100+8192])
        for repetition in range(args.folder_repeat):
            roles=('candidate',) if args.folder_role=='candidate' else (('baseline','candidate') if repetition%2==0 else ('candidate','baseline'))
            for label in roles:
                print(json.dumps({'event':'start','case':case,'helper':label,'repetition':repetition+1}),flush=True)
                suffix='' if repetition==0 else '-'+str(repetition+1)
                records=benchmark.run(SimpleNamespace(helper=getattr(args,label),source=str(source),mib=1,repeat=1,timeout=3600,report=str(output/(case+'-'+label+suffix+'.json'))))
                for row in records: row.update(case=case,role=label,comparisonRepetition=repetition+1)
                results.extend(records)
                (output/'results.json').write_text(json.dumps(results,indent=2)+'\n')

print(json.dumps({'event':'complete','runs':len(results),'physicalNetworkTested':False}),flush=True)
