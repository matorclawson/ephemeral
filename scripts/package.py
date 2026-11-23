"""Package binary, redistributable dependency notices and offline buildable source."""
from pathlib import Path
import hashlib
import tarfile
import tempfile
import shutil

root = Path(__file__).resolve().parents[1]
dist = root / "dist"
name = "ephemeral-" + (root / "VERSION").read_text().strip()
files = []
with tempfile.TemporaryDirectory() as folder:
    pkg = Path(folder) / (name + "-linux-arm64")
    pkg.mkdir()
    shutil.copy2(dist / "linux-arm64" / "ephemeral", pkg)
    (pkg / "examples").mkdir()
    for example in ["approval.jsonl", "demo.py", "parallel.py", "README.md"]:
        shutil.copy2(root / "examples" / example, pkg / "examples")
    for filename in ["README.md", "CHANGELOG.md"]:
        shutil.copy2(root / filename, pkg)
    for path in sorted(root.glob("LICENSE*")):
        shutil.copy2(path, pkg)
    for path in (root / "vendor").rglob("*"):
        if path.is_file() and path.name.upper().startswith(("LICENSE", "COPYING", "NOTICE", "AUTHORS")):
            target = pkg / "notices" / path.relative_to(root / "vendor")
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(path, target)
    binary = dist / (pkg.name + ".tar.gz")
    with tarfile.open(binary, "w:gz") as tar:
        tar.add(pkg, arcname=pkg.name)
    files.append(binary)

src = dist / (name + "-source.tar.gz")
with tarfile.open(src, "w:gz", compresslevel=5) as tar:
    for path in sorted(root.iterdir()):
        if path.name in {"dist", "bin", ".git", "ephemeral"}:
            continue
        tar.add(path, arcname=f"{name}/{path.name}")
files.append(src)
(dist / "SHA256SUMS").write_text("".join(
    f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}\n" for path in files))
