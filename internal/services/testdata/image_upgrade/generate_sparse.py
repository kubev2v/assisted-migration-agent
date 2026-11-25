#!/usr/bin/env python3
"""Regenerate sparse PAX archives without materializing their 1 PiB holes."""

import io
from pathlib import Path
import tarfile


directory = Path(__file__).resolve().parent
for version in ("0.1", "1.0"):
    major, minor = version.split(".")
    records = {
        "GNU.sparse.major": major,
        "GNU.sparse.minor": minor,
        "GNU.sparse.realsize": str(1 << 50),
        "GNU.sparse.name": "sparse",
    }
    payload = b"x"
    if version == "0.1":
        records.update({"GNU.sparse.map": "0,1", "GNU.sparse.numblocks": "1"})
    else:
        payload = b"1\n0\n1\n".ljust(512, b"\0") + payload

    output = io.BytesIO()
    with tarfile.open(fileobj=output, mode="w", format=tarfile.PAX_FORMAT) as archive:
        entry = tarfile.TarInfo("sparse")
        entry.size = len(payload)
        entry.pax_headers = records
        archive.addfile(entry, io.BytesIO(payload))
    (directory / ("sparse-pax-" + version + ".tar")).write_bytes(output.getvalue())
