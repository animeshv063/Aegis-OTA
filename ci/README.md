# CI Image Signing & Pipeline

This directory contains the cryptographic tooling used by the build server (CI) to generate, hash, and sign OS image releases.

## Cryptographic Guarantees

Aegis OTA treats payload integrity as completely independent from TLS transport:
- Even if an attacker performs a Man-In-The-Middle (MITM) attack or compromises the HTTP download mirror, they cannot forge the release signature without the private Ed25519 signing key.
- The public verification key is embedded into the read-only boot partition during factory flashing.

## Usage

### 1. Generate Signing Keypair
```bash
python3 keygen.py
```
This generates `keys/aegis_release.key` (private) and `keys/aegis_release.pub` (public).

### 2. Sign Image Artifact
```bash
python3 sign_artifact.py \
    --image /path/to/aegis-nas-os-v1.2.0.img \
    --version 1.2.0 \
    --arch arm64 \
    --key keys/aegis_release.key \
    --out manifest.json
```
