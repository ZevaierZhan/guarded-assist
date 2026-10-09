"""Build the server bundles, native clients, skill and checksums for a Release."""
import argparse
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]
TARGETS = [(os_name, arch) for os_name in ("windows", "linux", "darwin") for arch in ("amd64", "arm64")]


def build(package, target, output):
    print(f"Build {package} for {target[0]}/{target[1]}", flush=True)
    env = dict(os.environ, CGO_ENABLED="0", GOPROXY="off", GOOS=target[0], GOARCH=target[1])
    subprocess.run(["go", "build", "-trimpath", "-ldflags=-s -w", "-o", str(output), package], cwd=ROOT, env=env, check=True)


def archive(directory, output):
    if output.suffix == ".zip":
        with zipfile.ZipFile(output, "w", zipfile.ZIP_DEFLATED) as z:
            for path in sorted(directory.rglob("*")):
                if path.is_file():
                    z.write(path, path.relative_to(directory).as_posix())
    else:
        with tarfile.open(output, "w:gz") as t:
            for path in sorted(directory.iterdir()):
                t.add(path, arcname=path.name)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", default="dist/release")
    args = parser.parse_args()
    output = (ROOT / args.output).resolve()
    output.mkdir(parents=True, exist_ok=True)
    if any(output.iterdir()):
        parser.error("output directory must be empty; choose a new directory")
    with tempfile.TemporaryDirectory(prefix="guarded-assist-release-") as temp:
        temp = Path(temp)
        clients = temp / "clients"
        clients.mkdir()
        for target in TARGETS:
            os_name, arch = target
            name = "assist.exe" if target == ("windows", "amd64") else f"assist-{os_name}-{arch}" + (".exe" if os_name == "windows" else "")
            binary = clients / name
            build("./cmd/assist", target, binary)
            binary.chmod(0o755)
            shutil.copy2(binary, output / name)
        for target in TARGETS:
            os_name, arch = target
            bundle = temp / f"server-{os_name}-{arch}"
            shutil.copytree(clients, bundle)
            suffix = ".exe" if os_name == "windows" else ""
            for name, package in (("assist-server", "./cmd/assist-server"), ("assistctl", "./cmd/assistctl")):
                binary = bundle / (name + suffix)
                build(package, target, binary)
                binary.chmod(0o755)
            shutil.copy2(ROOT / "README.md", bundle / "README.md")
            shutil.copy2(ROOT / "LICENSE", bundle / "LICENSE")
            archive(bundle, output / (f"guarded-assist-server-{os_name}-{arch}" + (".zip" if os_name == "windows" else ".tar.gz")))
        archive(ROOT / "skills", output / "guarded-assist-skill.zip")
    lines = [f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}\n" for path in sorted(output.iterdir()) if path.is_file()]
    (output / "SHA256SUMS").write_text("".join(lines), encoding="utf-8")


if __name__ == "__main__":
    main()
