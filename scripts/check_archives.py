"""Check that the release archives in dist/ list exactly the expected members."""
from pathlib import Path
import sys
import tarfile

root = Path(__file__).resolve().parents[1]
name = "ephemeral-" + (root / "VERSION").read_text().strip()
dist = root / "dist"


def members(path, prefix):
    # getnames() reads the whole archive and raises on a truncated or corrupt one.
    names = tarfile.open(path, "r:gz").getnames()
    top = set()
    for n in names:
        if n != prefix and not n.startswith(prefix + "/"):
            sys.exit(f"{path.name}: unexpected member outside {prefix}: {n}")
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
