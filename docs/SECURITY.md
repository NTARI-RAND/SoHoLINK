# SoHoLINK Security Policy

*Version: 1.0 | Last Updated: 2026-03-06*

Network Theory Applied Research Institute takes the security of SoHoLINK seriously. We appreciate the work of security researchers who help us keep our platform safe for everyone.

---

## Reporting a Vulnerability

**Email:** security@soholink.network  
**PGP Fingerprint:** `XXXX XXXX XXXX XXXX XXXX  XXXX XXXX XXXX XXXX XXXX`  
*(Full public key available at [keys.openpgp.org](https://keys.openpgp.org) — search for security@soholink.network)*

> ⚠️ Please **do not** report security vulnerabilities via GitHub Issues, the public forum, or any other public channel. Use the email above only.

---

## What to Include in Your Report

To help us triage quickly, please include:

1. **Description** of the vulnerability and potential impact
2. **Affected component** (e.g. `internal/httpapi/webhook.go`, P2P mesh, payment flow)
3. **Steps to reproduce** — a minimal proof-of-concept if possible
4. **Suggested severity** (Critical / High / Medium / Low) and your reasoning
5. Your name or handle (for acknowledgement, if desired)

---

## Our Response SLA

| Milestone | Target |
|-----------|--------|
| Acknowledgement of report | Within **48 hours** |
| Initial triage and severity assessment | Within **5 business days** |
| Patch released for Critical severity | Within **14 days** of confirmation |
| Patch released for High severity | Within **30 days** of confirmation |
| Patch released for Medium / Low | Next scheduled release |
| Public disclosure (coordinated) | After patch is available to all users |

We will keep you informed of progress throughout the process.

---

## Scope

### In Scope

The following are explicitly in scope for responsible disclosure:

- SoHoLINK HTTP API (`internal/httpapi/`)
- Payment processing flows (`internal/payment/`)
- Authentication and challenge-response (`internal/httpapi/auth*.go`)
- P2P peer discovery mesh (`internal/p2p/`)
- OPA policy evaluation (`configs/policies/`)
- LBTAS rating and escrow flows (`internal/lbtas/`)
- Content safety screening (`internal/store/safety.go`)
- Stripe webhook signature verification (`internal/httpapi/webhook.go`)
- SQLite data integrity and injection

### Out of Scope

The following are **not** in scope:

- Denial of service attacks against our own infrastructure (test against your own deployment)
- Social engineering of SoHoLINK staff
- Physical attacks against operator hardware
- Vulnerabilities in third-party dependencies where the upstream project is already aware
- Issues requiring full administrative access to the host system to exploit
- Theoretical attacks with no practical exploit path
- Scanner-generated reports without manual verification

---

## Safe Harbour

We will not pursue civil or criminal action against researchers who:

1. Report vulnerabilities to us promptly and in good faith through this policy
2. Avoid accessing or modifying user data beyond what is strictly necessary to demonstrate the issue
3. Do not disrupt or degrade services
4. Do not exploit the vulnerability beyond a minimal proof-of-concept
5. Allow us a reasonable time to patch before any public disclosure

We consider good-faith security research a valuable contribution and will work with researchers collaboratively.

---

## Bug Bounty

At this time, SoHoLINK does **not** offer monetary bug bounties. We do offer:

- Public acknowledgement in the release notes (with your permission)
- A letter of recognition for significant findings
- Early access to new features for active contributors

A formal bounty programme may be introduced in a future release.

---

## Known Security Properties

For reference, the following security controls are currently in place:

| Control | Status |
|---------|--------|
| TLS on API server | ✅ Configurable (`tls_cert_file`/`tls_key_file`) |
| Request body size limit (4 MB cap) | ✅ Global middleware |
| CORS allowlist | ✅ `allowed_origins` config field |
| Rate limiting on auth challenge | ✅ 10 req/min per IP |
| Stripe webhook HMAC-SHA256 verification | ✅ `internal/httpapi/webhook.go` |
| Payment idempotency keys | ✅ Wallet topups |
| Ed25519 signed P2P announcements | ✅ Anti-replay timestamp window |
| CSAM hash blocklist | ✅ SHA-256 screening on all uploads |
| DID blocklist with propagation | ✅ Federation-wide |
| OPA policy enforcement | ✅ Resource sharing limits |
| Self-review prevention (LBTAS) | ✅ Both rating directions |
| Auto-updater HTTPS enforcement | ✅ `release_url` must be `https://`; redirects to non-HTTPS rejected |
| Auto-updater checksum verification | ✅ SHA-256 vs official `checksums.txt`; `crypto/subtle` constant-time compare |
| Auto-updater temp file isolation | ✅ `0600` permissions set before binary data written |

---

## Disclosure Timeline

We follow **coordinated disclosure**: we will work with you to develop a patch before any public announcement. The default embargo period is **90 days** from confirmed vulnerability, after which we support responsible disclosure even if a patch is not yet available.

---

*This policy is inspired by [disclose.io](https://disclose.io) safe harbour standards.*
