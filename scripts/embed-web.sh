#!/bin/bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
test -f "$ROOT/web/dist/index.html"
python3 - "$ROOT" <<'PY'
import pathlib,shutil,sys
root=pathlib.Path(sys.argv[1]); target=root/'internal/server/assets'
for item in target.iterdir():
    if item.name=='README.txt': continue
    if item.is_dir(): shutil.rmtree(item)
    else: item.unlink()
shutil.copytree(root/'web/dist',target,dirs_exist_ok=True)
PY
