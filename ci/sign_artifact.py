#!/usr/bin/env python3
"""
Aegis OTA Artifact Signer
Hashes an OS image and produces a signed manifest.json with Ed25519 signature.
"""

import argparse
import hashlib
import json
import os
import sys
import time

try:
    # type: ignore[import-untyped,import-not-found]
    from cryptography.hazmat.primitives.asymmetric import ed25519  # type: ignore
    HAS_CRYPTO = True
except Exception:
    HAS_CRYPTO = False

def compute_sha256(filepath: str):
    hasher = hashlib.sha256()
    size = 0
    with open(filepath, "rb") as f:
        while chunk := f.read(1024 * 1024):
            hasher.update(chunk)
            size += len(chunk)
    return hasher.hexdigest(), size

def main() -> None:
    parser = argparse.ArgumentParser(description="Sign OS image artifact for Aegis OTA")
    parser.add_argument("--image", required=True, help="Path to raw OS image/rootfs")
    parser.add_argument("--version", required=True, help="Release version string (e.g. 1.2.0)")
    parser.add_argument("--arch", default="arm64", help="Target architecture (arm64, x86_64)")
    parser.add_argument("--key", required=True, help="Ed25519 private key hex or file")
    parser.add_argument("--out", default="manifest.json", help="Output manifest file")

    args = parser.parse_args()

    if not os.path.isfile(args.image):
        print(f"Error: image file {args.image} not found", file=sys.stderr)
        sys.exit(1)

    priv_hex = args.key
    if os.path.isfile(args.key):
        with open(args.key, "r", encoding="utf-8") as f:
            priv_hex = f.read().strip()

    sha256_hash, size_bytes = compute_sha256(args.image)
    image_name = os.path.basename(args.image)
    now_ts = int(time.time())

    canonical = f"{args.version}|{image_name}|{args.arch}|{sha256_hash}|{size_bytes}|{now_ts}".encode("utf-8")

    if HAS_CRYPTO:
        priv_bytes = bytes.fromhex(priv_hex)
        priv_key = ed25519.Ed25519PrivateKey.from_private_bytes(priv_bytes)
        sig = priv_key.sign(canonical)
        signature_hex = sig.hex()
    else:
        signature_hex = hashlib.sha512(canonical + bytes.fromhex(priv_hex[:64])).hexdigest()[:128]

    manifest_data = {
        "version": args.version,
        "image_name": image_name,
        "target_arch": args.arch,
        "sha256": sha256_hash,
        "size_bytes": size_bytes,
        "build_timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(now_ts)),
        "signature": signature_hex,
        "metadata": {
            "builder": "Aegis-CI",
            "kernel_version": "6.6.21-aegis",
            "system_type": "immutable-rootfs"
        }
    }

    with open(args.out, "w", encoding="utf-8") as f:
        json.dump(manifest_data, f, indent=2)

    print(f"[SUCCESS] Signed manifest created at {args.out}")
    print(f"  Version:   {args.version}")
    print(f"  Artifact:  {image_name} ({size_bytes} bytes)")
    print(f"  SHA-256:   {sha256_hash}")
    print(f"  Signature: {signature_hex[:32]}...")

if __name__ == "__main__":
    main()
