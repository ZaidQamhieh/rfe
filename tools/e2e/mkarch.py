import sys, zipfile, tarfile, io, os, zlib

# usage: mkarch.py KIND OUT [args]
kind, out = sys.argv[1], sys.argv[2]
args = sys.argv[3:]


def zadd(z, name, data, mode=0o100644):
    zi = zipfile.ZipInfo(name)
    zi.external_attr = mode << 16
    z.writestr(zi, data)


if kind == "zipnames":  # name=content pairs
    with zipfile.ZipFile(out, "w") as z:
        for a in args:
            n, c = a.split("=", 1)
            zadd(z, n, c)
elif kind == "zipsym":  # symlink entry l -> target, plus ok.txt
    with zipfile.ZipFile(out, "w") as z:
        zadd(z, "ok.txt", "fine")
        zadd(z, "sym", args[0], mode=0o120777)
elif kind == "tarsym":  # symlink + hardlink + regular
    with tarfile.open(out, "w:gz") as t:
        d = b"fine"
        ti = tarfile.TarInfo("ok.txt"); ti.size = len(d); t.addfile(ti, io.BytesIO(d))
        ti = tarfile.TarInfo("sym"); ti.type = tarfile.SYMTYPE; ti.linkname = args[0]; t.addfile(ti)
        ti = tarfile.TarInfo("hard"); ti.type = tarfile.LNKTYPE; ti.linkname = "ok.txt"; t.addfile(ti)
elif kind == "many":  # N empty entries
    n = int(args[0])
    with zipfile.ZipFile(out, "w", zipfile.ZIP_STORED) as z:
        for i in range(n):
            z.writestr("e/%d" % i, b"")
elif kind == "big":  # one entry of N bytes of zeros, then a small trailer
    n = int(args[0])
    with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED, compresslevel=1) as z:
        with z.open("big.bin", "w", force_zip64=True) as f:
            blk = b"\0" * (1 << 20)
            for _ in range(n >> 20):
                f.write(blk)
        zadd(z, "after.txt", "x")
else:
    sys.exit("bad kind")
