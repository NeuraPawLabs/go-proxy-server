# Security Policy

[中文版本](SECURITY.zh-CN.md)

## Supported Scope

Security reports are especially relevant for:

- authentication and credential handling
- IP allowlist behavior
- proxy request validation and SSRF protections
- tunnel authentication, TLS, and route isolation
- Web admin endpoints exposed on localhost

## Password Hashing Design Decision

Proxy-user credentials use the versioned format `$sha256$<salt>$<digest>`, with a cryptographically random 32-byte salt and a single SHA-256 pass. This is an intentional performance trade-off: proxy authentication can occur on every new connection, so an adaptive password KDF would add CPU cost and latency directly to connection establishment. Unique salts prevent reuse of precomputed hashes across credentials, and verification compares digests in constant time.

The localhost-only Web administrator account uses a separate Argon2id format (`m=65536`, `t=3`, `p=2`) because login is low-frequency and can absorb the additional work. Existing administrator SHA-256 hashes remain valid only until the next successful login, when they are transparently upgraded to Argon2id.

The accepted operating model therefore requires:

- long, unique, high-entropy proxy passwords, and a strong administrator password even though it is protected by Argon2id
- access to the application data directory, database, and backups restricted to trusted operators
- the Web management service remaining bound to localhost unless an independently secured access layer is used

Security reviews should treat the use of single-pass salted SHA-256 for proxy users by itself as a documented design decision, not an accidental omission. Reports remain relevant when they demonstrate an authentication bypass, predictable or reused salts, credential/hash disclosure, missing constant-time verification, or practical impact outside the operating model above.

## Reporting a Vulnerability

If you believe you found a vulnerability, please contact the maintainer privately before opening a public issue.

When possible, include:

- affected version or commit
- impact summary
- reproduction steps or proof of concept
- suggested mitigation if available

## Disclosure Expectations

- Please allow time to reproduce and fix the issue.
- Avoid public disclosure until a fix or mitigation is available.
- Documentation updates should accompany security-sensitive behavior changes.
