#!/usr/bin/env python3
"""校验 zh-CN / en-US 的 i18n key 集合是否一致。

用法：python3 scripts/check_i18n.py
退出码：0 = 一致；1 = 存在缺失（缺失 key 会打印出来）。

实现说明：
- 两个文件都是纯嵌套对象字面量（无数组/模板字符串），用缩进栈解析 dotted key。
- 跳过行注释、块注释与 `export default {`；值里含 `//`（URL）不受影响，
  因为只看每行开头的 `key:` 部分。
"""
from __future__ import annotations

import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
ZH = ROOT / "frontend/src/i18n/zh-CN.ts"
EN = ROOT / "frontend/src/i18n/en-US.ts"

KEY_RE = re.compile(r"^(?P<indent>\s*)(?P<key>[A-Za-z_][A-Za-z0-9_]*):\s*(?P<value>.*)$")


def parse_keys(path: Path) -> set[str]:
    keys: set[str] = set()
    stack: list[tuple[int, str]] = []  # (indent, key)
    in_block_comment = False
    for raw in path.read_text(encoding="utf-8").splitlines():
        line = raw.rstrip()
        stripped = line.strip()
        if in_block_comment:
            if "*/" in stripped:
                in_block_comment = False
            continue
        if stripped.startswith("/*"):
            if "*/" not in stripped:
                in_block_comment = True
            continue
        if not stripped or stripped.startswith("//"):
            continue
        if stripped.startswith("export default"):
            continue
        indent = len(line) - len(line.lstrip(" "))
        if stripped.startswith("}"):
            # 关闭当前缩进层级及更深的对象
            while stack and stack[-1][0] >= indent:
                stack.pop()
            continue
        m = KEY_RE.match(line)
        if not m:
            continue
        key = m.group("key")
        value = m.group("value").strip()
        if value.startswith("{"):
            stack.append((indent, key))
            continue
        keys.add(".".join([k for _, k in stack] + [key]))
    return keys


def main() -> int:
    zh = parse_keys(ZH)
    en = parse_keys(EN)
    missing_in_en = sorted(zh - en)
    missing_in_zh = sorted(en - zh)

    if missing_in_en:
        print(f"[i18n] en-US 缺少 {len(missing_in_en)} 个 key:")
        for key in missing_in_en:
            print("  - " + key)
    if missing_in_zh:
        print(f"[i18n] zh-CN 缺少 {len(missing_in_zh)} 个 key:")
        for key in missing_in_zh:
            print("  - " + key)
    if missing_in_en or missing_in_zh:
        return 1

    print(f"[i18n] OK: zh-CN 与 en-US 各 {len(zh)} 个 key")
    return 0


if __name__ == "__main__":
    sys.exit(main())
