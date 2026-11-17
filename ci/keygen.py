#!/usr/bin/env python3
"""
Aegis OTA Cryptographic Key Generator
Generates Ed25519 public/private key pairs for image signing.
"""

import os
from cryptography.hazmat.primitives.asymmetric import ed25519
from cryptography.hazmat.primitives import serialization

def main():
    # Generate private key
    private_key = ed25519.Ed25519PrivateKey.generate()
    public_key = private_key.public_key()

    # Export raw hex bytes
    priv_bytes = private_key.private_bytes(
        encoding=serialization.Encoding.Raw,
        format=serialization.PrivateFormat.Raw,
        encryption_algorithm=serialization.NoEncryption()
    )
    pub_bytes = public_key.public_bytes(
        encoding=serialization.Encoding.Raw,
        format=serialization.PublicFormat.Raw
    )

    priv_hex = priv_bytes.hex()
    pub_hex = pub_bytes.hex()

    print("\n--- Aegis OTA Ed25519 Keypair ---")
    print(f"Private Key (KEEP SECRET): {priv_hex}")
    print(f"Public Key  (EMBED IN OS): {pub_hex}")

    # Save to keys/ directory
    os.makedirs("keys", exist_ok=True)
    with open("keys/aegis_release.pub", "w") as f:
        f.write(pub_hex)
    with open("keys/aegis_release.key", "w") as f:
        f.write(priv_hex)

    print("\nSaved to keys/aegis_release.pub and keys/aegis_release.key")

if __name__ == "__main__":
    main()
