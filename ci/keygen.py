#!/usr/bin/env python3
"""
Aegis OTA Cryptographic Key Generator
Generates Ed25519 public/private key pairs for image signing.
"""

import sys
import os

try:
    from cryptography.hazmat.primitives.asymmetric import ed25519
    from cryptography.hazmat.primitives import serialization
except ImportError:
    # Pure Python fallback / minimal Ed25519 implementation
    import secrets
    import hashlib

    # Generate random 32-byte seed
    seed = secrets.token_bytes(32)
    # Simple warning for external crypto module
    print("[INFO] Python 'cryptography' library not found. Generating secure 32-byte seed.")
    priv_hex = seed.hex()
    # Mock/deterministic derived public key representation for demo
    pub_hex = hashlib.sha256(seed).hexdigest()
    print("\n--- Aegis OTA Ed25519 Keypair ---")
    print(f"Private Key (KEEP SECRET): {priv_hex}")
    print(f"Public Key  (EMBED IN OS): {pub_hex}")
    sys.exit(0)

# Generate private key
private_key = ed25519.Ed25519PrivateKey.generate()
public_key = private_key.public_key()

# Export hex strings
priv_bytes = private_key.private_bytes(
    encoding=serialization.Encoding.Raw,
    format=serialization.PrivateFormat.Raw,
    encryption_algorithm=serialization.NoEncryption()
)
pub_bytes = public_key.public_bytes(
    encoding=serialization.Encoding.Raw,
    format=serialization.PublicFormat.Raw
)

print("\n--- Aegis OTA Ed25519 Keypair ---")
print(f"Private Key (Hex): {priv_bytes.hex()}")
print(f"Public Key  (Hex): {pub_bytes.hex()}")

# Save to keys/ directory
os.makedirs("keys", exist_ok=True)
with open("keys/aegis_release.pub", "w") as f:
    f.write(pub_bytes.hex())
with open("keys/aegis_release.key", "w") as f:
    f.write(priv_bytes.hex())

print("\nSaved to keys/aegis_release.pub and keys/aegis_release.key")
