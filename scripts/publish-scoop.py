#!/usr/bin/env python3
"""Publish the GoReleaser-generated Scoop manifest after release verification."""
import argparse
import base64
import json
import os
from pathlib import Path
import subprocess


def validate(path, tag, sums):
    manifest = json.loads(path.read_text())
    if manifest.get("version") != tag[1:]:
        raise ValueError("Scoop manifest version differs from release")
    checksums = {row.split()[1]: row.split()[0] for row in sums.read_text().splitlines()}
    for key, arch in (("64bit", "amd64"), ("arm64", "arm64")):
        archive = f"translate_{tag[1:]}_windows_{arch}.zip"
        row = manifest["architecture"][key]
        expected = f"https://github.com/daviddwlee84/translate/releases/download/{tag}/{archive}"
        if row["url"] != expected or row["hash"] != checksums[archive]:
            raise ValueError("Scoop manifest differs from verified release payload")
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--dist", type=Path, default=Path("dist"))
    args = parser.parse_args()
    paths = list(args.dist.rglob("translate.json"))
    if len(paths) != 1:
        raise SystemExit("expected exactly one GoReleaser Scoop manifest")
    desired = validate(paths[0], args.tag, args.dist / "checksums.txt")
    if not os.environ.get("GH_TOKEN"):
        raise SystemExit("the existing Scoop token is required")
    endpoint = "repos/daviddwlee84/scoop-bucket/contents/bucket/translate.json"
    result = subprocess.run(["gh", "api", endpoint + "?ref=main"], text=True, capture_output=True)
    current = None
    if result.returncode == 0:
        current = json.loads(result.stdout)
        if json.loads(base64.b64decode(current["content"])) == desired:
            print("Scoop manifest is already current")
            return
    elif "HTTP 404" not in result.stderr:
        raise SystemExit("could not read the current Scoop manifest")
    body = {"message": "translate " + args.tag, "branch": "main", "content": base64.b64encode(paths[0].read_bytes()).decode()}
    if current:
        body["sha"] = current["sha"]
    subprocess.run(["gh", "api", "--method", "PUT", endpoint, "--input", "-"], input=json.dumps(body), text=True, stdout=subprocess.DEVNULL, check=True)
    print("published verified Scoop manifest")


if __name__ == "__main__":
    main()
