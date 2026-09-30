#!/usr/bin/env python3
"""Refuse to publish an APK a modern phone will not install.

Android needs native libraries STORED (mmap) and, since Android 15, on a
16 KB page boundary. gogio writes them deflated and 4-byte aligned, and
the installer says only "app not installed" with the reason in logcat.
"""
import struct
import sys
import zipfile

PAGE = 16 * 1024


def data_offset(path, info):
    with open(path, "rb") as f:
        f.seek(info.header_offset + 26)
        name_len, extra_len = struct.unpack("<HH", f.read(4))
    return info.header_offset + 30 + name_len + extra_len


def main(path):
    problems = []
    z = zipfile.ZipFile(path)

    libs = [i for i in z.infolist() if i.filename.endswith(".so")]
    if not libs:
        problems.append("no native libraries at all; the app has no Go in it")

    for i in libs:
        if i.compress_type != zipfile.ZIP_STORED:
            problems.append(
                f"{i.filename} is compressed; Android mmaps it and will refuse to install"
            )
        off = data_offset(path, i)
        if off % PAGE:
            problems.append(
                f"{i.filename} starts at {off}, not a {PAGE}-byte boundary"
            )

    names = z.namelist()
    if "AndroidManifest.xml" not in names:
        problems.append("no AndroidManifest.xml")
    if "classes.dex" not in names:
        problems.append("no classes.dex; the Java side did not build")
    if not any(n.startswith("META-INF/") and n.endswith(".RSA") for n in names):
        problems.append("not signed")

    if problems:
        print("this APK would not install:")
        for p in problems:
            print("  -", p)
        return 1

    print(f"{path}: {len(libs)} native libs, stored and {PAGE}-aligned, signed")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1]))
