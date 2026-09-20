#!/usr/bin/env python3
"""Rewrite an APK so its native libraries are STORED and page-aligned.

gogio writes every zip entry with Deflate. Android mmaps native
libraries straight out of the APK, so a compressed one cannot be
loaded and the install is refused - with no reason the user can see.

`zipalign -p` is not enough on its own: it aligns entries that are
already uncompressed and silently leaves compressed ones alone. The
entry has to be rewritten as STORED first, which is what this does.
The caller re-signs afterwards, because rewriting the zip invalidates
whatever signature was there.
"""
import shutil
import struct
import sys
import zipfile

PAGE = 16 * 1024


def repack(src_path, dst_path):
    src = zipfile.ZipFile(src_path)
    stored = 0
    with zipfile.ZipFile(dst_path, "w") as out:
        for info in src.infolist():
            base = info.filename.rsplit("/", 1)[-1]
            # Signatures are invalidated by rewriting; the caller signs
            # again. Carrying them over would leave a broken v1 block.
            if info.filename.startswith("META-INF/") and (
                base == "MANIFEST.MF"
                or base.endswith(".SF")
                or base.endswith(".RSA")
                or base.endswith(".DSA")
                or base.endswith(".EC")
            ):
                continue

            data = src.read(info.filename)
            zi = zipfile.ZipInfo(info.filename, date_time=info.date_time)
            zi.external_attr = info.external_attr
            zi.create_system = info.create_system

            if info.filename.endswith(".so"):
                zi.compress_type = zipfile.ZIP_STORED
                # Pad the extra field so the DATA - not the header -
                # lands on a page boundary.
                header = out.fp.tell() + 30 + len(zi.filename.encode())
                zi.extra = b"\0" * ((-header) % PAGE)
                stored += 1
            else:
                zi.compress_type = info.compress_type

            out.writestr(zi, data)

    if stored == 0:
        print("repack: no native libraries found", file=sys.stderr)
        return 1
    print(f"repack: {stored} native libs stored and {PAGE}-aligned")
    return 0


if __name__ == "__main__":
    sys.exit(repack(sys.argv[1], sys.argv[2]))
