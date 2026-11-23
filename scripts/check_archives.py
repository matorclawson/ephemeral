"""Check that the release archives in dist/ list exactly the expected members."""
from pathlib import Path
import gzip
import posixpath
import sys
import tarfile

root = Path(__file__).resolve().parents[1]
name = "ephemeral-" + (root / "VERSION").read_text().strip()
dist = root / "dist"


def members(path, prefix):
    # Decompress the whole gzip stream first: a truncated or corrupt end raises here.
    with gzip.open(path, "rb") as z:
        while z.read(1 << 20):
            pass
    top = set()
    with tarfile.open(path, "r:gz") as tar:
        for m in tar:
            n = m.name
            if n.startswith("/") or ".." in n.split("/"):
                sys.exit(f"{path.name}: unsafe member path: {n}")
            if n != prefix and not n.startswith(prefix + "/"):
                sys.exit(f"{path.name}: unexpected member outside {prefix}: {n}")
            if m.isreg():
                f = tar.extractfile(m)
                while f.read(1 << 20):  # read every member to its end
                    pass
            elif m.isdir():
                pass
            elif m.issym():
                target = posixpath.normpath(posixpath.join(posixpath.dirname(n), m.linkname))
                if m.linkname.startswith("/") or not (target == prefix or target.startswith(prefix + "/")):
                    sys.exit(f"{path.name}: symlink escapes {prefix}: {n} -> {m.linkname}")
            elif m.islnk():
                if not (m.linkname == prefix or m.linkname.startswith(prefix + "/")) or ".." in m.linkname.split("/"):
                    sys.exit(f"{path.name}: hard link escapes {prefix}: {n} -> {m.linkname}")
            else:
                sys.exit(f"{path.name}: unsupported member type for {n}")
            if n != prefix:
                top.add(n[len(prefix) + 1:].split("/")[0])
    return top


binary = members(dist / (name + "-linux-arm64.tar.gz"), name + "-linux-arm64")
want = {"ephemeral", "LICENSE", "README.md", "CHANGELOG.md", "examples", "notices"}
if binary != want:
    sys.exit(f"{name}-linux-arm64.tar.gz: top-level entries {sorted(binary)}, want {sorted(want)}")

source = members(dist / (name + "-source.tar.gz"), name)
need = {"LICENSE", "README.md", "CHANGELOG.md", "VERSION", "Makefile", "go.mod", "scripts", "vendor"}
if not need <= source:
    sys.exit(f"{name}-source.tar.gz: missing {sorted(need - source)}")
bad = source & {"dist", ".git", "bin", "ephemeral"}
if bad:
    sys.exit(f"{name}-source.tar.gz: unexpected {sorted(bad)}")
