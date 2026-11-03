#!/usr/bin/env python3
"""Regenerate the small skopeo export fixture; requires skopeo on PATH."""

import copy
import hashlib
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile


def add(archive, name, content):
    entry = tarfile.TarInfo(name)
    entry.size = len(content)
    entry.mode = 0o644
    archive.addfile(entry, io.BytesIO(content))


directory = Path(__file__).resolve().parent
config = copy.deepcopy(json.loads((directory / "konflux-config.json").read_text()))
layer = io.BytesIO()
with tarfile.open(fileobj=layer, mode="w") as archive:
    add(archive, "fixture.txt", b"Konflux archive format regression fixture\n")
layer = layer.getvalue()
config["rootfs"] = {"type": "layers", "diff_ids": ["sha256:" + hashlib.sha256(layer).hexdigest()]}
config["history"] = [{"created_by": "update archive test fixture"}]
config = json.dumps(config).encode()
config_name = hashlib.sha256(config).hexdigest() + ".json"
manifest = [{"Config": config_name, "RepoTags": ["localhost/oma-update-fixture:latest"], "Layers": ["layer.tar"]}]
with tempfile.TemporaryDirectory() as temporary:
    source = Path(temporary) / "docker.tar"
    with tarfile.open(source, mode="w") as archive:
        add(archive, config_name, config)
        add(archive, "layer.tar", layer)
        add(archive, "manifest.json", json.dumps(manifest).encode())
    subprocess.run([
        "skopeo", "copy", "--format", "oci",
        "docker-archive:" + str(source),
        "oci-archive:" + str(directory / "konflux-oci.tar") + ":oma-update-fixture",
    ], check=True)
