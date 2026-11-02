#!/usr/bin/env python3
"""去重 wailsjs 绑定文件中的重复导出。

背景：`wails dev`/`wails build` 会自动重新生成 frontend/wailsjs/go/app/*，
而开发过程中手写的同名导出会被追加进去，导致重复声明（Rollup 报
"Identifier X has already been declared"）。本脚本保留每个函数首次出现，
其余删除；同时按 name 去重 .d.ts 中的导出声明。

用法：python3 scripts/dedupe_bindings.py
"""
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
GO_DIR = ROOT / "frontend" / "wailsjs" / "go"


def dedupe_js(path: Path) -> int:
    if not path.exists():
        return 0
    src = path.read_text()
    block = re.compile(r"export function (\w+)\([^)]*\) \{\n(?:  [^\n]*\n)+\}\n", re.M)
    seen, out, last = set(), [], 0
    removed = 0
    for m in block.finditer(src):
        name = m.group(1)
        out.append(src[last:m.start()])
        if name not in seen:
            seen.add(name)
            out.append(m.group(0))
        else:
            removed += 1
        last = m.end()
    out.append(src[last:])
    text = re.sub(r"\n{3,}", "\n\n", "".join(out))
    if text != src:
        path.write_text(text)
    return removed


def dedupe_dts(path: Path) -> int:
    if not path.exists():
        return 0
    src = path.read_text()
    seen, out, removed = set(), [], 0
    for line in src.split("\n"):
        m = re.match(r"export function (\w+)\(", line)
        if m:
            if m.group(1) in seen:
                removed += 1
                continue
            seen.add(m.group(1))
        out.append(line)
    text = re.sub(r"\n{3,}", "\n\n", "\n".join(out))
    if text != src:
        path.write_text(text)
    return removed


def main() -> int:
    if not GO_DIR.exists():
        print(f"skip: {GO_DIR} not found")
        return 0
    total = 0
    for js in GO_DIR.rglob("*.js"):
        n = dedupe_js(js)
        if n:
            print(f"deduped {n} in {js.relative_to(ROOT)}")
        total += n
    for dts in GO_DIR.rglob("*.d.ts"):
        n = dedupe_dts(dts)
        if n:
            print(f"deduped {n} in {dts.relative_to(ROOT)}")
        total += n
    print(f"done, {total} duplicate export(s) removed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
