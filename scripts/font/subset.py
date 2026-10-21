#!/usr/bin/env python3
"""Goxml 嵌入字体的构建期子集化脚本（TASK-FONT-001-B / B2）。

用途：把官方 Noto Sans SC 静态 OTF 子集化为 GB2312 覆盖、改名、并生成
`pkg/runtime/theme/embedded/` 下的全部产物（字体 / 字符集清单 / 许可证 / 元数据）。

设计约束（B 卡 §2）：

  * **只在构建期运行**，运行时绝不执行子集化；
  * 输入文件必须与冻结的 SHA-256 完全一致，否则拒绝执行——防止"看起来
    子集成功了，其实换了一份字体"；
  * 字符集与工具版本都固定，产物 SHA-256 写进 `METADATA.json`，可逐项复核；
  * 许可证全文随附（OFL 1.1 要求），同样校验 SHA-256。

用法：

    python scripts/font/subset.py \
        --source  <NotoSansSC-Regular.otf> \
        --license <官方仓库 LICENSE>

    # 只校验已落地的产物是否与 METADATA.json 一致（不重新生成）
    python scripts/font/subset.py --verify-only

上游来源（冻结）：

    仓库   https://github.com/notofonts/noto-cjk
    tag    Sans2.004
    commit 523d033d6cb47f4a80c58a35753646f5c3608a78
    路径   Sans/SubsetOTF/SC/NotoSansSC-Regular.otf
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import subprocess
import sys

# ---------------------------------------------------------------- 冻结常量

UPSTREAM = {
    "repository": "https://github.com/notofonts/noto-cjk",
    "tag": "Sans2.004",
    "commit": "523d033d6cb47f4a80c58a35753646f5c3608a78",
    "path": "Sans/SubsetOTF/SC/NotoSansSC-Regular.otf",
    "license": "SIL Open Font License 1.1",
}

SOURCE_SHA256 = "faa6c9df652116dde789d351359f3d7e5d2285a2b2a1f04a2d7244df706d5ea9"
SOURCE_BYTES = 8331336
SOURCE_GIT_BLOB_SHA1 = "fc0fda9394367c16c1af7555a2ae4d5e2e6a6f02"

LICENSE_SHA256 = "6a73f9541c2de74158c0e7cf6b0a58ef774f5a780bf191f2d7ec9cc53efe2bf2"
LICENSE_BYTES = 4301

FONT_TOOL_VERSION = "4.67.0"

# OFL 衍生品改名（B 卡 §1.2，维护者已确认）。
FAMILY_NAME = "Goxml Sans SC"
POSTSCRIPT_NAME = "GoxmlSansSC-Regular"
FONT_VERSION = "Version 1.000"

FONT_FILE = POSTSCRIPT_NAME + ".otf"
CHARSET_FILE = "charset.txt"
LICENSE_FILE = "LICENSE"
METADATA_FILE = "METADATA.json"

# pyftsubset 参数（与 B 卡 §2.4 记录一致）。
#
# `--no-recalc-timestamp` 是显式声明（该版本已是默认）：产物必须**逐字节可复现**，
# 否则每次构建都会改写 `head.modified`，产物哈希就失去比对意义。
SUBSET_ARGS = [
    "--no-hinting",
    "--no-recalc-timestamp",
    "--drop-tables+=DSIG,LTSH,VDMX,hdmx,PCLT",
    "--name-IDs=0,1,2,3,4,5,6,7,13,14",
    "--notdef-outline",
    "--recommended-glyphs",
]

# 清单外码位：必须**无字形**，用于如实标注"不是完整 CJK"。
OUTSIDE_CODEPOINTS = [0x20000, 0x2A700, 0x3400, 0x9FA6, 0xE000]

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
OUT_DIR = os.path.join(ROOT, "pkg", "runtime", "theme", "embedded")


def sha256_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def sha256_file(path: str) -> str:
    return sha256_bytes(open(path, "rb").read())


def build_charset() -> list[int]:
    """ASCII 可打印 + GB2312 全部可解码码位（去重排序）。

    说明：GB2312 的码位集合由 Python 标准库 codec 给出，与 B 卡 §2.2 冻结的
    "GB2312 一级 + 二级常用字 + ASCII + 常用标点"一致；汉字数为 6763。
    """
    cps = set(range(0x20, 0x7F))
    for hi in range(0xA1, 0xFF):
        for lo in range(0xA1, 0xFF):
            try:
                ch = bytes([hi, lo]).decode("gb2312")
            except UnicodeDecodeError:
                continue
            cps.add(ord(ch))
    return sorted(cps)


def write_charset(path: str, cps: list[int]) -> None:
    """一行一个码位：便于逐项核对与 diff（pyftsubset 的 --unicodes-file 接受此格式）。"""
    with open(path, "w", encoding="utf-8", newline="\n") as f:
        for cp in cps:
            f.write("U+%04X\n" % cp)


def subset(source: str, charset_path: str, out_font: str) -> None:
    cmd = [sys.executable, "-m", "fontTools.subset", source,
           "--unicodes-file=" + charset_path,
           "--output-file=" + out_font] + SUBSET_ARGS
    print("[run] " + " ".join(cmd))
    res = subprocess.run(cmd, capture_output=True, text=True)
    if res.stdout.strip():
        print("[stdout] " + res.stdout.strip())
    if res.returncode != 0:
        print("[stderr] " + res.stderr.strip(), file=sys.stderr)
        raise SystemExit("pyftsubset 失败，rc=%d" % res.returncode)


def rename_font(src: str, dst: str) -> None:
    """改名为 Goxml Sans SC，并保留 OFL 声明。

    官方文件 nameID 0 未声明 Reserved Font Name，但 nameID 7 明示
    "Noto is a trademark of Google Inc."；衍生品沿用上游品牌名不合适。
    """
    from fontTools.ttLib import TTFont

    # recalcTimestamp=False 是**可复现性要求**：默认 True 会在保存时把
    # `head.modified` 改成当前时间，导致同样的输入产出不同的字节/哈希。
    f = TTFont(src, recalcTimestamp=False)
    names = {
        1: FAMILY_NAME,
        2: "Regular",
        3: "1.000;GOXML;" + POSTSCRIPT_NAME,
        4: FAMILY_NAME,
        5: FONT_VERSION,
        6: POSTSCRIPT_NAME,
        7: FAMILY_NAME + " is a subset of Noto Sans SC (SIL OFL 1.1).",
        13: "This Font Software is licensed under the SIL Open Font License, "
            "Version 1.1.",
        14: "http://scripts.sil.org/OFL",
    }
    for nid, val in names.items():
        f["name"].setName(val, nid, 3, 1, 0x409)
    f.save(dst)


def verify_artifacts() -> dict:
    """校验已落地产物与 METADATA.json 完全一致，返回元数据。"""
    meta_path = os.path.join(OUT_DIR, METADATA_FILE)
    if not os.path.exists(meta_path):
        raise SystemExit("缺少元数据：" + meta_path)
    meta = json.load(open(meta_path, encoding="utf-8"))

    checks = [
        (FONT_FILE, meta["subset"]["sha256"], meta["subset"]["bytes"]),
        (CHARSET_FILE, meta["charset"]["sha256"], meta["charset"]["bytes"]),
        (LICENSE_FILE, meta["license"]["sha256"], meta["license"]["bytes"]),
    ]
    bad = []
    for name, want_sha, want_bytes in checks:
        path = os.path.join(OUT_DIR, name)
        if not os.path.exists(path):
            bad.append("%s 缺失" % name)
            continue
        got_sha, got_bytes = sha256_file(path), os.path.getsize(path)
        if got_sha != want_sha:
            bad.append("%s sha256 不符：%s != %s" % (name, got_sha, want_sha))
        if got_bytes != want_bytes:
            bad.append("%s 字节数不符：%d != %d" % (name, got_bytes, want_bytes))

    from fontTools.ttLib import TTFont

    f = TTFont(os.path.join(OUT_DIR, FONT_FILE), lazy=True)
    for nid, want in ((1, FAMILY_NAME), (4, FAMILY_NAME), (6, POSTSCRIPT_NAME)):
        got = f["name"].getDebugName(nid)
        if got != want:
            bad.append("nameID%d 不符：%r != %r" % (nid, got, want))

    if bad:
        raise SystemExit("产物校验失败：\n  - " + "\n  - ".join(bad))
    print("[verify] 产物与 METADATA.json 一致：字体 / 字符集 / 许可证 / 字体名")
    return meta


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--source", help="官方 NotoSansSC-Regular.otf 路径")
    ap.add_argument("--license", dest="license_path", help="官方仓库 LICENSE 路径")
    ap.add_argument("--verify-only", action="store_true",
                    help="只校验已落地产物，不重新生成")
    args = ap.parse_args()

    try:
        import fontTools
    except ImportError:
        raise SystemExit("需要 fontTools：python -m pip install fonttools==" + FONT_TOOL_VERSION)
    if fontTools.version != FONT_TOOL_VERSION:
        raise SystemExit("fontTools 版本必须是 %s（当前 %s）——版本变化会改变产物字节"
                         % (FONT_TOOL_VERSION, fontTools.version))

    if args.verify_only:
        verify_artifacts()
        return

    if not args.source or not args.license_path:
        raise SystemExit("生成模式需要 --source 与 --license")

    # 1. 输入校验：一字节不符就停。
    got_sha, got_bytes = sha256_file(args.source), os.path.getsize(args.source)
    if (got_sha, got_bytes) != (SOURCE_SHA256, SOURCE_BYTES):
        raise SystemExit("源字体与冻结值不符：\n  实际 %s / %d\n  期望 %s / %d"
                         % (got_sha, got_bytes, SOURCE_SHA256, SOURCE_BYTES))
    lic_sha, lic_bytes = sha256_file(args.license_path), os.path.getsize(args.license_path)
    if (lic_sha, lic_bytes) != (LICENSE_SHA256, LICENSE_BYTES):
        raise SystemExit("LICENSE 与冻结值不符：%s / %d" % (lic_sha, lic_bytes))

    os.makedirs(OUT_DIR, exist_ok=True)

    # 2. 字符集
    cps = build_charset()
    charset_path = os.path.join(OUT_DIR, CHARSET_FILE)
    write_charset(charset_path, cps)
    hanzi = sum(1 for c in cps if 0x4E00 <= c <= 0x9FFF)
    print("[charset] codepoints=%d hanzi=%d" % (len(cps), hanzi))
    if (len(cps), hanzi) != (7540, 6763):
        raise SystemExit("字符集规模偏离冻结值（7540 / 6763），不得静默改变覆盖范围")

    # 3. 子集 + 改名
    raw = os.path.join(OUT_DIR, ".subset_raw.otf")
    subset(args.source, charset_path, raw)
    out_font = os.path.join(OUT_DIR, FONT_FILE)
    rename_font(raw, out_font)
    os.remove(raw)

    # 4. 许可证全文
    lic_dst = os.path.join(OUT_DIR, LICENSE_FILE)
    with open(args.license_path, "rb") as src, open(lic_dst, "wb") as dst:
        dst.write(src.read())

    # 5. 覆盖核验（写进元数据前先自证）
    from fontTools.ttLib import TTFont

    f = TTFont(out_font, lazy=True)
    cmap = f.getBestCmap()
    missing = [c for c in cps if c not in cmap]
    if missing:
        raise SystemExit("子集缺少 %d 个清单内码位，例如 %r"
                         % (len(missing), ["U+%04X" % c for c in missing[:10]]))

    # 6. 元数据
    meta = {
        "familyName": FAMILY_NAME,
        "postScriptName": POSTSCRIPT_NAME,
        "version": FONT_VERSION,
        "upstream": UPSTREAM,
        "source": {
            "file": os.path.basename(UPSTREAM["path"]),
            "bytes": SOURCE_BYTES,
            "sha256": SOURCE_SHA256,
            "gitBlobSha1": SOURCE_GIT_BLOB_SHA1,
        },
        "subset": {
            "file": FONT_FILE,
            "bytes": os.path.getsize(out_font),
            "sha256": sha256_file(out_font),
        },
        "charset": {
            "file": CHARSET_FILE,
            "bytes": os.path.getsize(charset_path),
            "sha256": sha256_file(charset_path),
            "codepoints": len(cps),
            "hanzi": hanzi,
        },
        "license": {
            "file": LICENSE_FILE,
            "bytes": LICENSE_BYTES,
            "sha256": LICENSE_SHA256,
            "name": UPSTREAM["license"],
        },
        "tool": {
            "name": "fontTools.pyftsubset",
            "version": FONT_TOOL_VERSION,
            "args": SUBSET_ARGS,
        },
        "coverage": {
            "charset": "ASCII 可打印 + GB2312（一级+二级常用字）",
            "completeCJK": False,
            "note": "不是完整 CJK 覆盖：清单外码位无字形，属已知边界而非缺陷",
            "outsideCodepoints": ["U+%04X" % c for c in OUTSIDE_CODEPOINTS],
        },
    }
    with open(os.path.join(OUT_DIR, METADATA_FILE), "w", encoding="utf-8", newline="\n") as fh:
        json.dump(meta, fh, ensure_ascii=False, indent=2)
        fh.write("\n")

    print("[out] %s" % out_font)
    print("[out] size=%d sha256=%s" % (meta["subset"]["bytes"], meta["subset"]["sha256"]))
    verify_artifacts()


if __name__ == "__main__":
    main()
