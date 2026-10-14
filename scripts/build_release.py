"""Build small, portable Windows executables and a checksummed release ZIP."""

import argparse
import hashlib
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import zipfile


ROOT = Path(__file__).resolve().parent.parent


def run(*args, cwd=ROOT, env=None):
    subprocess.run(args, cwd=cwd, env=env, check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", default="dev")
    args = parser.parse_args()
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9.+-]*", args.version):
        parser.error("version must contain only letters, digits, dots, + and -")
    for tool in ("go", "windres"):
        if not shutil.which(tool):
            parser.error(f"{tool} is not on PATH; see README.md build prerequisites")

    dist = ROOT / "dist"
    dist.mkdir(exist_ok=True)
    run(sys.executable, "scripts/make_icon.py")
    run("windres", "-i", "proxypulse.rc", "-o",
        "../cmd/proxypulse-gui/rsrc.syso", "--target=pe-x86-64",
        "--input-format=rc", "--output-format=coff", cwd=ROOT / "assets")
    env = dict(os.environ, GOOS="windows", GOARCH="amd64", CGO_ENABLED="0")
    flags = f"-s -w -X main.version={args.version}"
    for name in ("proxypulse", "proxypulse-gui"):
        linker = flags + (" -H windowsgui" if name.endswith("-gui") else "")
        run("go", "build", "-trimpath", "-buildvcs=false", "-ldflags", linker,
            "-o", str(dist / f"{name}.exe"), f"./cmd/{name}", env=env)

    archive = dist / f"ProxyPulse-{args.version}-windows-x64.zip"
    with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED, compresslevel=9) as package:
        for name in ("proxypulse-gui.exe", "proxypulse.exe"):
            package.write(dist / name, name)
        for name in ("README.md", "LICENSE", "CHANGELOG.md", "assets/screenshot.png"):
            package.write(ROOT / name, name)

    artifacts = [dist / "proxypulse-gui.exe", dist / "proxypulse.exe", archive]
    checksums = []
    for path in artifacts:
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        checksums.append(f"{digest}  {path.name}\n")
        print(f"{path.name}: {path.stat().st_size / 1048576:.2f} MiB")
    (dist / "SHA256SUMS.txt").write_text("".join(checksums), encoding="utf-8")


if __name__ == "__main__":
    main()
