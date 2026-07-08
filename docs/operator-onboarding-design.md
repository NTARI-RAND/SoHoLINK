# SoHoLINK Operator Onboarding — Decision-Ready Design

**Status:** Ready for build, pending the short "Open for user" list at the end.
**Scope:** Reshape `soholink.org` from a member portal into an **operator/coordinator console** that onboards frontend **platforms** (Cloudy, Fruitful), not persons. Covers UI, onboarding state machine, conformance harness, operator registry + migrations, the signing seam, governance-port separation, and the non-destructive member-portal disposition.
**Source of truth reconciled:** the two area designs (backend-workflow, ui), the security review (7 findings), and the architecture review (4 findings). **Every CONFIRMED review fix is folded into the design below and flagged `[SEC-fix]` / `[ARCH-fix]` at the point it applies.**

---

## 0. The three resolved product decisions

| # | Decision | Resolution |
|---|----------|------------|
| 1 | Onboarding trust model | **HYBRID.** Mechanical gates (contact 2FA, conformance) are public self-service on `soholink.org` and fully automated. The final `active` flip is a deliberate human action on the local-only governance port `:8090`. |
| 2 | Member-portal disposition | **NON-DESTRUCTIVE.** Every existing member route stays live and unchanged; a persistent transitional banner is added and `/` is re-parented to the operator console. No deletion, no data migration. |
| 3 | Conformance transport | **SoHoLINK issues signed challenges; the operator signs and returns.** SoHoLINK is always the initiator/grader and never dials operator infrastructure. Challenges use **fresh per-onboarding values**, not the public vectors verbatim `[SEC-fix]`. |

Two governance-surface questions the reviews escalated (public-write `POST /operators`, and pre-authorization of the applicant) are **not** silently resolved here — they are folded into the design as the **invite-token model** (below) and surfaced in "Open for user" only where a genuine choice remains.

---

## 1. Governance-port separation — the load-bearing invariant

Per SoHoLINK `CLAUDE.md`: *"Governance layer architecturally separated — admin portal runs on a separate local-only port (8090), never exposed publicly, not hidden behind role flags on the public portal."*

Two muxes, two trust surfaces:

- **Public surface (`soholink.org`, portal service behind NGINX/Caddy):** operator identity landing, self-service onboarding funnel, operator dashboard, conformance run endpoints. **No privileged lifecycle action ever lives here.**
- **Governance surface (`:8090`, local-only):** operator **activation**, **revocation**, **contact-rebind**, **lockout clear**, admin review of registered keys + conformance verdicts.

`[SEC-fix]` (network isolation, sec note 7): the route split is necessary but not sufficient. The build **must** additionally:
1. Bind `:8090` to `127.0.0.1`/loopback (or a private admin network), never `0.0.0.0`.
2. Put an **independent admin credential** (2-person control) on the `:8090` handlers themselves, so network isolation is defense-in-depth, not the sole control.
3. Audit every public handler for any ability to reach `localhost:8090` (SSRF). The conformance flow already never dials the operator; keep that pull-only discipline internally.

---

## 2. Operator registry — data model (Go / pgx, migrations 021–023)

Canon-native: raw **32-byte** Ed25519 public keys (not SPKI PEM), mirroring Agrinet's reconciled `operatorRepository` shape. New ENUMs use `CREATE TYPE` (not `ALTER TYPE ADD VALUE`), so the same-transaction enum rule in `CLAUDE.md` is not tripped. Migrations 021–023 extend 001–020 with no renumbering and no destructive change; the fresh registry grandfathers nothing `[ARCH-fix: confirmed coherent]`.

### 021_operators.sql

```sql
CREATE TYPE operator_status AS ENUM ('active','revoked');
CREATE TYPE operator_onboarding_state AS ENUM ('pending_verification','verified','active');

CREATE TABLE operators (
  id                     TEXT PRIMARY KEY,          -- stable slug, e.g. 'cloudy'
  name                   TEXT NOT NULL,
  email                  TEXT NOT NULL,             -- normalized lowercase/trim
  phone                  TEXT NOT NULL,             -- E.164 normalized
  status                 operator_status NOT NULL DEFAULT 'active',
  onboarding_state       operator_onboarding_state NOT NULL DEFAULT 'pending_verification',
  email_verified         BOOLEAN NOT NULL DEFAULT FALSE,
  phone_verified         BOOLEAN NOT NULL DEFAULT FALSE,
  conformance_passed_at  TIMESTAMPTZ,               -- set by harness
  conformance_keyset_hash BYTEA,                    -- [SEC-fix] hash of the 7 keys that passed
  invite_token_id        TEXT REFERENCES operator_invites(id),  -- [SEC/ARCH-fix] provenance of creation
  created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
  -- [SEC-fix] uniqueness enforced ONLY on verified/active rows, not on abandoned pending rows:
  CONSTRAINT uq_operators_email_live UNIQUE (email)   -- see partial-index note below
);
-- Replace the table-level UNIQUE with partial unique indexes so a squatted pending
-- row cannot block a legitimate applicant [SEC-fix, finding 4]:
--   CREATE UNIQUE INDEX uq_operators_email_live ON operators(email)
--     WHERE onboarding_state <> 'pending_verification';
--   CREATE UNIQUE INDEX uq_operators_phone_live ON operators(phone)
--     WHERE onboarding_state <> 'pending_verification';
-- Slug (id) remains a hard PRIMARY KEY; well-known slugs are reserved (see operator_invites).

CREATE TABLE operator_keys (
  id                   TEXT PRIMARY KEY,            -- uuid
  operator_id          TEXT NOT NULL REFERENCES operators(id) ON DELETE CASCADE,
  key_index            INT  NOT NULL CHECK (key_index BETWEEN 0 AND 6),
  public_key           BYTEA NOT NULL,             -- raw 32-byte ed25519
  algo                 TEXT NOT NULL DEFAULT 'ed25519',
  state                TEXT NOT NULL DEFAULT 'active', -- active|expired|retired|revoked
  usage_count          INT  NOT NULL DEFAULT 0,
  expiration_threshold INT  NOT NULL,               -- drawn from EXPIRATIONS at insert
  created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX uq_opkeys_active ON operator_keys(operator_id,key_index) WHERE state='active';
CREATE INDEX idx_opkeys_operator ON operator_keys(operator_id,state);

CREATE TABLE operator_verifications (              -- 2FA codes
  operator_id  TEXT NOT NULL REFERENCES operators(id) ON DELETE CASCADE,
  channel      TEXT NOT NULL,                      -- email|phone
  code         TEXT NOT NULL,
  attempts     INT  NOT NULL DEFAULT 0,
  session_id   TEXT NOT NULL,                      -- [SEC-fix] bound to applicant session
  expires_at   TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (operator_id, channel)
);

CREATE TABLE operator_invites (                    -- [SEC/ARCH-fix] admin-minted creation tokens
  id           TEXT PRIMARY KEY,                   -- uuid
  token_hash   BYTEA NOT NULL,                     -- store hash, not the token
  reserved_slug TEXT,                              -- optional: pin a well-known slug (e.g. 'cloudy')
  issued_by    TEXT NOT NULL,
  expires_at   TIMESTAMPTZ NOT NULL,
  consumed_at  TIMESTAMPTZ,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 022_operator_replay.sql (anti-replay; durable; fail-closed)

```sql
CREATE TABLE operator_replay (                     -- sliding-window seq per (operator,coordinator)
  operator_id    TEXT NOT NULL,
  coordinator_id TEXT NOT NULL,
  seq_high       BIGINT NOT NULL DEFAULT 0,
  seq_window     BYTEA  NOT NULL DEFAULT '\x00'::bytea,   -- 256-bit bitmap
  PRIMARY KEY (operator_id, coordinator_id)
);
CREATE TABLE operator_nonces (
  nonce       BYTEA PRIMARY KEY,
  operator_id TEXT NOT NULL,
  scope       TEXT NOT NULL DEFAULT 'production',  -- [SEC-fix] 'production' | 'conformance'
  expires_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_operator_nonces_expiry ON operator_nonces(expires_at);
```

### 023_conformance_runs.sql

```sql
CREATE TABLE conformance_runs (
  run_id       TEXT PRIMARY KEY,                   -- uuid
  operator_id  TEXT NOT NULL REFERENCES operators(id) ON DELETE CASCADE,
  started_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  finished_at  TIMESTAMPTZ,
  status       TEXT NOT NULL DEFAULT 'running',    -- running|passed|failed
  keyset_hash  BYTEA,                              -- [SEC-fix] key-set this run graded against
  results      JSONB NOT NULL DEFAULT '{}'::jsonb  -- per-check verdicts
);
CREATE INDEX idx_conformance_runs_operator ON conformance_runs(operator_id, started_at DESC);

CREATE TABLE conformance_challenges (              -- [SEC-fix] one row per issued challenge
  challenge_id TEXT PRIMARY KEY,
  run_id       TEXT NOT NULL REFERENCES conformance_runs(run_id) ON DELETE CASCADE,
  idx          INT  NOT NULL,                      -- challenge index within run
  nonce        BYTEA NOT NULL,                     -- fresh CSPRNG >=16B, single-use
  expected     JSONB NOT NULL,                     -- SoHoLINK-computed oracle (never public vectors)
  consumed_at  TIMESTAMPTZ,
  UNIQUE (run_id, idx)
);
```

**Backfill:** none. Fresh registry.

---

## 3. Repository (`internal/operator`) — the single verify chokepoint

`GetActiveKeyMap(ctx, id) (map[int]KeyRecord, error)` returns **nil** unless *all* hold:
- operator exists, `status='active'` **and** `onboarding_state='active'`,
- at least one active key,
- **all** active keys share one algo (algo-pin).

This one predicate collapses the onboarding gate, revocation, and algo-pin into the hot path with zero extra branches. A pending/verified operator authenticates nothing.

Other methods: `CreateOperator` (invite-gated), `AddKeys`, `StartVerification`, `CheckVerification` (session-bound), `MarkVerified`, `ActivateOperator` (8090 only), `Revoke`, `RebindContact` (8090 only), `SeqCheckAndAdvance(tx,…)` + `NonceInsert(tx,…)` (both in **one** `pgx.Tx`, fail-closed), `RecordUsageAndExpire` (Phase-A lazy expiry, refuses to drop below `MIN_ACTIVE_KEYS`), `RegisterReplacementKey` (Phase-B, retire+insert in one tx, idempotent).

`[SEC-fix, finding 6]` — key-set fingerprint binding:
- On conformance pass, store `conformance_keyset_hash = H(sorted 7 pubkeys)` on both the run and the operator.
- `ActivateOperator` requires the **current** key-set hash to equal `conformance_keyset_hash`.
- **Any** key add/rotate/replace before activation clears `conformance_passed_at` and `conformance_keyset_hash`.
- **Operator-authorized rotate is disabled pre-activation** (there is no established key set to authorize against yet); rotate is available only after `active`.

---

## 4. Onboarding state machine

States on `operators.onboarding_state`: `pending_verification → verified → active`. `operators.status` (`active|revoked`) is orthogonal. Sub-flags `email_verified`, `phone_verified`, `conformance_passed_at`, `conformance_keyset_hash` gate the transitions.

```
          invite token                 email_verified                conformance green
          + slug + 7 keys              AND phone_verified             (fresh challenges,
              │                            (session-bound)             keyset_hash stored)
              ▼                               ▼                            │
   ┌─────────────────────┐  T2   ┌───────────┐   T3 (precondition,        │
   │ pending_verification│──────▶│  verified │   not a state change)──────┤
   └─────────────────────┘       └───────────┘                            │
        ▲ T1 (public, invite-gated)      │                                │
                                         │  T4: DELIBERATE admin action on :8090
                                         │      verified AND conformance_passed_at IS NOT NULL
                                         │      AND current keyset_hash == conformance_keyset_hash
                                         ▼
                                   ┌───────────┐
                                   │  active   │  ← only here does GetActiveKeyMap return non-nil
                                   └───────────┘
```

- **T1 (application) — public, invite-gated `[SEC/ARCH-fix]`.** Applicant presents a valid admin-minted invite token, then creates the operator in `pending_verification` with NOT-NULL normalized email+phone, the reserved slug (if the invite pins one), and the 7 public keys. This resolves the architecture review's finding 1 (public self-asserted creation was a new pre-trust attack surface): creation now originates from a governance action (the minted invite), preserving the resolved model's intent while keeping the mechanical gates self-service. Duplicate live email/phone → 409 (pending rows excluded from uniqueness).
- **T2 (2FA).** Both `email_verified` AND `phone_verified` → `verified`. 10-min TTL, 5-attempt cap. `[SEC-fix, finding 5]`: code issuance/consumption is **bound to the applicant session** (`operator_verifications.session_id`), and the rate-limit counter is keyed to the authenticated applicant session, **not** the target `operator_id` — so a third party cannot grief a victim's lockout. `verify/check` additionally requires a **signature over the 2FA code under a registered key**, so "verified" means the *same* party controls both the contact channel and the signing keys (cross-binds the two authenticators before admin review).
- **T3 (conformance).** Running the harness to green sets `conformance_passed_at` **and** `conformance_keyset_hash`. Precondition on activation, not itself a state transition.
- **T4 (activation).** Human admin action on `:8090` only. Requires `verified` AND conformance pass AND current-keyset-hash match.

**Port split:** T1–T3 and their endpoints live on `soholink.org`; T4 plus revoke, contact-rebind, and lockout-clear live **only** on `:8090`.

---

## 5. Endpoints

### Public (`soholink.org`)

| Method + path | Purpose |
|---|---|
| `POST /operators/apply` | invite-gated: mint applicant session + create `pending_verification` operator `{invite_token, name, slug, email, phone, password}` |
| `POST /operators/{id}/keys` | register exactly 7 raw-32B ed25519 public keys (base64); validates count, decodes, rejects dupes; clears any prior conformance pass |
| `POST /operators/{id}/verify/start` | `{channel}` → send 2FA code (session-bound) |
| `POST /operators/{id}/verify/check` | `{channel, code, sig_over_code}` → mark channel verified; advances to `verified` when both |
| `POST /operators/{id}/conformance/start` | → `{run_id, challenges[]}` (fresh CSPRNG-nonced, SoHoLINK-computed oracles) |
| `POST /operators/{id}/conformance/{run}/submit` | `{responses[]}` → graded results; on full pass sets `conformance_passed_at` + `keyset_hash` |
| `POST /operators/{id}/rotate` | **post-activation only** `[SEC-fix]`; signed canon rotate, routed through replay CAS then `RegisterReplacementKey` |
| `GET /operators/{id}` | status-aware dashboard (public info; no secrets) |
| `GET /operators/{id}/fees` | fee declarations view/submit |
| `POST /operators/verify` | stateless PURE verify of a transmission (integration/testing; no side effects) |

### Governance (`:8090`, local-only, independent admin auth)

| Method + path | Purpose |
|---|---|
| `POST /operators/invites` | mint an invite token (optionally pin a reserved slug) |
| `GET /admin/operators` | queue: pending / verified+passed / active / revoked |
| `GET /admin/operators/{id}` | full detail: registered pubkeys, verify bools, latest run verdicts |
| `POST /admin/operators/{id}/activate` | verified + conformance + keyset-hash match → active |
| `POST /admin/operators/{id}/revoke` | active → revoked (kill switch; high-blast-radius confirm) |
| `POST /admin/operators/{id}/rebind-contact` | `[SEC-fix]` post-verification contact change (never public) |
| `GET /operators/lockouts` · `POST /operators/lockouts/{id}/clear` | lockout management |

---

## 6. Verification middleware — `OperatorAuth`

Lives in `internal/api`, applied in front of the httpjson node-side handlers (`SubmitListing`/`Heartbeat`/`PollJobs`/`Decline`/`ReportJob`) — the exact seam where SPIFFE binding is applied today.

`[SEC-fix, finding 8]` — **the two auth paths are mutually exclusive per route, not additive.** A node-side operation that expects an operator token **REQUIRES** it (present-and-valid); absence is **not** "fall through to SPIFFE." Exactly one authenticator is defined per handler, so an attacker cannot strip `X-SohoCloud-Operator` to downgrade to a weaker path. (The original additive "no token → proceed" shape is dropped.)

**Transport:** `X-SohoCloud-Operator` header carrying base64(compact `OperatorTransmission`). Ed25519 sigs are 64B so two fit a header; at the ML-DSA swap, fall back to a JSON-body token.

**Verify order** (pure verify first, then durable CAS):
1. Reject if `Nonce` absent or `len < 16`.
2. Reject if `len(Sig0) != Verifier.SigLen(Algo)` or `len(Sig1) != …` (per-algo, never hardcoded 64).
3. Reject if `Idx0 == Idx1`.
4. Reject if `|now − TsUnixNano| > 5min`, checked on the exact signed `int64` (never round-tripped through `time.Time`).
5. `km := GetActiveKeyMap(OperatorID)`; reject if nil.
6. Reject if `km[Idx0]` or `km[Idx1]` absent, or either row's algo `!=` signed `Algo` (algo pin — preserved; blocks ml-dsa→ed25519 downgrade while active keys are ml-dsa).
7. Recompute canon bytes with `Verifier`; verify both sigs.
8. Anti-replay CAS (`SeqCheckAndAdvance` + `NonceInsert`, **scope='production'**) in one tx, fail-closed.
9. `RecordUsageAndExpire` (downstream of CAS ⇒ exactly-once); set `X-Operator-Swap-Required` if any index hit threshold.

On success, store operator id + indices in request context (`RequireOperator` retrieves it), analogous to `identity.SPIFFEIDFromContext`.

---

## 7. Canon operator transmission (add to `sohocloud-protocol`, package `operator/`)

Domain tag `sohocloud/operator/v0`. Field order (Sig fields excluded):

```go
canon.New("sohocloud/operator/v0").
    String(OperatorID).
    Int64(TsUnixNano).
    Bytes(Nonce).
    Uint64(Seq).
    String(Algo).
    Uint64(uint64(Idx0)).   // [ARCH-fix, finding 3] Idx0/Idx1 are int; convert explicitly
    Uint64(uint64(Idx1)).
    Sum()
```

Rotation uses a distinct tag `sohocloud/operator-rotate/v0` covering `{operator_id,key_index,new_public_key,algo,ts,nonce,seq}`, signed by **two current active keys**, verified over the new-key bytes **before** insert.

`[SEC-fix, finding 3]` — **conformance transmissions are domain-separated from production**: conformance challenges are built under `sohocloud/operator-conformance/v0`, so a conformance response can never be replayed onto the production `OperatorAuth` path (which only accepts `…/operator/v0`), and vice-versa.

---

## 8. Signing / verification seam (add to `sohocloud-protocol`)

Keeps the stdlib-only dependency-leaf invariant — the seam adds an interface, **no third-party PQC dependency**.

```go
type Signer interface {
    Sign(msg []byte) ([]byte, error)
    Public() []byte
    Algo() string
}
type Verifier interface {
    Verify(pub, msg, sig []byte) bool
    SigLen() int
    Algo() string
}
```

- **Now:** Ed25519 impl — `ed25519.Sign`/`ed25519.Verify`, `SigLen()=64`, `Algo()="ed25519"`.
- **At Go 1.27 (`crypto/mldsa` stdlib landing):** a second impl `Algo()="ml-dsa-65"`, `SigLen()≈3309`, selected by domain-tag version (`sohocloud/operator/v1-mldsa`) as a whole-set atomic rotation. `expectedLen(algo)` is `Verifier.SigLen()`.

`[ARCH-fix, finding 2]` — **scope statement (explicit):** this seam covers **Layer C only** (frontend↔coordinator operator identity — the credential actually being swapped). The six core coordination messages (`listing`, `employment` Assignment/Decline/JobReport, `fees` FeeDeclaration, Heartbeat) remain **ed25519-hardcoded** and would need their own tag-versioned migration (e.g. `sohocloud/listing/v1-mldsa`) when PQC lands for Layer B. Do not read "drop-in ML-DSA" as making the whole protocol PQC-ready — it does not.

---

## 9. Conformance harness (`internal/operator/conformance`)

**Core principle `[SEC-fix, findings 1 & 2]`:** `testdata/vectors.json` publishes, for every message, the fields **and** the `canonical_bytes_hex`, `signature_hex`, and even the raw `seed_hex` (private seed) — confirmed on disk (6 messages, 25 primitives). Therefore the **public vectors can never be the live grading oracle**: an operator with no canon at all could echo the published hex and a single offline `ed25519.Sign` over a known constant. The harness uses **fresh per-onboarding inputs** and SoHoLINK-computed expectations. The static vectors remain a **public bootstrap/self-test aid only**.

Every challenge carries a **fresh CSPRNG nonce (≥16B) bound to `{operator_id, run_id, challenge_index}`, single-use**, consumed on first response. "Idempotent for support" means a resubmit returns the **same graded result**, never re-accepts a new signature. Consumed conformance nonces persist in `operator_nonces` with `scope='conformance'` (fail-closed).

### Suite A — canonical bytes (freshly generated) `[SEC-fix, finding 1]`

SoHoLINK generates **new** message field-sets at onboarding time (randomized NodeID/JobID/timestamps/seqs/fee bps for each of the 6 message types + the operator-transmission field-set), computes the expected canonical bytes with the reference canon **on its own side**, and requires the operator to return canonical bytes for **those fresh inputs**. Grade = byte-equality against SoHoLINK's freshly-computed expectation (stored in `conformance_challenges.expected`), **not** against `vectors.json`. The operator must also return a **signature over the fresh canonical bytes under a registered key** — so a real canon implementation is necessary to pass. FAIL detail shows the first-differing hex offset.

### Suite B — signature / 2-of-7 transmission (the sound liveness proof)

SoHoLINK issues a fresh `{nonce≥16B, ts, seq, Idx0≠Idx1}` under `sohocloud/operator-conformance/v0`. The operator returns a full transmission; SoHoLINK runs the **real** `OperatorAuth` verify path (steps 1–7) against the 7 registered public keys. Passing here structurally proves the operator can authenticate in production.

`[SEC-fix, finding 2]` — **no self-reported verdicts.** The old "operator returns a `verify-verdict` boolean" negative check is **removed** — a party with no verifier could hard-code the expected boolean. Verification capability is proven **structurally**: SoHoLINK verifying the operator's produced signatures already proves signing; Suite B's real-path verify is the liveness proof. Rejection of bad input, where required, must be observable in a SoHoLINK-checkable *artifact* (a different signed output), never a claimed boolean.

### Suite C — count-expiry / lazy-swap (whitepaper §4.2.2.1)

A **scratch operator context** with 5 mock keys at thresholds `{3,6,9,12,365}`, isolated from the applicant's real `operator_keys` rows and usage counters (runs against a conformance-scoped namespace so it cannot mutate the registered 7 keys). Drives transmissions until each key crosses its threshold; asserts `X-Operator-Swap-Required` fires at each drawn threshold (the crossing transmission still verifies — expired-until-attempted-once semantics), that a signed rotate (`…/operator-rotate/v0`, two current keys, new pubkey bound inside the signature) reinstalls an index, and that the replacement takes over. Key five is exercised until expired. Five gradable rows.

**Result:** all three suites green → `conformance_passed_at` + `conformance_keyset_hash` set on the run and operator. Admission (T4) gates on `verified` AND latest run `passed` AND current keyset-hash match.

---

## 10. UI — the reshaped `soholink.org` operator console

Server-rendered Go `html/template` against `web/templates`, styled with existing `web/static/css/portal.css` **verbatim** — no new tokens, no JS framework, no build step, 3G / 2019-Android safe (`details/summary` handles disclosures; the only scripts are the existing font-swap + auth-refresh, both degrade). All new pages extend `layout.html` with a reshaped nav; reuse `.hero`, `.card-grid/.card`, `.stat-grid`, `.table-wrap`, `.badge`, `.btn`, `.form-group`, the split-panel pattern from `login.html`/`register.html`, and the numbered-step list.

**Templates to add** (`web/templates/`): `operator_landing.html` (reshapes `index.html`), `operator_apply.html`, `operator_keys.html`, `operator_verify.html`, `operator_conformance.html`, `operator_dashboard.html`, `operator_fees.html`; on `:8090`: `admin_operators.html`, `admin_operator_detail.html`; plus one partial `transitional_banner.html`.

### Public flow (5 steps)

1. **`GET /` — operator console landing.** Reuse `.hero`, eyebrow "Substrate coordinator". H1 "The Substrate coordinator. *Onboard your platform.*" Hero-sub: *"SoHoLINK coordinates the Substrate compute economy — node recognition, employment lifecycle, settlement, and federation of frontends. Operator platforms enroll here; members belong to the platforms."* CTAs: "Apply as an operator" → `/operators/apply`; "Read the protocol" → SPEC. A `.card-grid` of 3 (Operators / Coordination / Federation — the last labeled design-target). One muted line + link is the **only** member entrypoint from root: *"Contributing a device or running a member account? The member portal is moving to Cloudy → /portal"*.
2. **`/operators/apply` — Step 1** (split-panel). Left: numbered 1–2–3–4–5 step list (Apply → Register keys → Verify contact → Prove conformance → Admission) + "What you'll need" (7 Ed25519 public keys from `operator-keygen`, contact email, phone for 2FA, **invite token**). Right: form → `POST /operators/apply` with invite token, platform name, operator slug (monospace), email, phone (E.164), applicant password.
3. **`/operators/{id}/keys` — Step 2.** Card "Operator-holds-keys": *"You generate all 7 keypairs locally… register only the PUBLIC keys — SoHoLINK never sees a private key."* 7 form rows (base64 raw-32B). After success, a table of the 7 with truncated `<code>` + "registered" badge.
4. **`/operators/{id}/verify` — Step 3.** Two cards (EMAIL / PHONE), each a state badge, "Send code" button, 6-digit-code input + a signed-code field, "Confirm". When both verified, an accent card "Contact verified — proceed to conformance →".
5. **`/operators/{id}/conformance` — Step 4.** "Start a new run" (`POST conformance/start`). Three `.section-label` groups, each a `.table-wrap`: **Canonical bytes** (rows: the 6 message types + OperatorTransmission, PASS/FAIL + first-differing hex offset), **Signatures & 2-of-7** (transmission signature 2-of-7; note that the negative check is proven structurally, not self-reported), **Key expiry & lazy swap** (5 rows, thresholds 3/6/9/12/365, "swap fired" column). Summary `.stat-grid` on top (checks passed N/M, run status, last run). A no-JS **"Run by hand"** `<details>` renders `challenges[]` as copy `<code>` blocks + a `<textarea>` to paste results JSON → same submit handler (usable on a 3G phone via `cmd/operator-conformance`). On pass, accent card "Conformance passed — awaiting operator admission."

**`GET /operators/{id}` — Step 5 + post-admission dashboard (status-aware).** While verified+passed-but-not-active: centered "Awaiting admission" card + read-only recap. Once active: full dashboard — `.stat-grid` (active keys, onboarding, conformance, transmissions/24h), a signing-keys table (index | truncated pubkey | status badge | usage | expires-at (threshold), amber badge near threshold) with the note *"Rotation is operator-driven and signed — SoHoLINK admits, expires, and records; it never holds your private keys."*, a lifecycle timeline (created → verified → passed → activated), and a fee-declarations preview. Revoke/rotate-force are **not** here — privileged actions live on `:8090`.

**`/operators/{id}/fees`** — fee-declaration table + submit form (no-information-asymmetry: rates visible). FeeDeclaration is one of the 6 vectors.

### Governance console (`:8090`, nav stripped to brand + "Governance")

- `GET /admin/operators`: queue counts `.stat-grid` (Pending / Verified & passed / Active / Revoked) + a table (Operator | Name | State | Email✓ | Phone✓ | Conformance | Registered | Action). "Review →" on verified+passed rows.
- `GET /admin/operators/{id}`: full detail — recap `.stat-grid`, table of the 7 registered pubkeys, table of the latest run's per-check verdicts, verify booleans, invite provenance. Two action forms: **"Activate operator"** (enabled only when verified+passed **and keyset-hash matches**), and a red-bordered danger zone **"Revoke operator"** with two-step confirm + *"high blast radius — denies every fronted member"* warning.

---

## 11. Member-portal disposition — NON-DESTRUCTIVE (confirmed)

Per `CLAUDE.md` "transitional reality": the portal/participants/agent/installer are **Cloudy-owned capabilities transitionally hosted here**, kept honest by labeling, not deletion. Production is live (Shenandoah-pilot contributors, Stripe payouts).

- Every existing member/participant route (`/login`, `/register`, `/join`, `/dashboard`, `/opt-out`, `/download`, `/consumer/marketplace`, `/provider/*`) stays live and unchanged.
- Add `transitional_banner.html` (surface-alt bg, amber left accent): *"Transitional — the member portal is moving to Cloudy. This surface stays live during migration."* Included at the top of every member content block.
- Re-parent nav in `layout.html`: primary nav becomes operator-facing; a single muted "Member portal" link points to the member routes. The member hero moves off `/` (now the operator landing).
- **No route deletions, no data changes.** `[ARCH-fix: confirmed non-destructive & coherent]`

Recommended now: **nav-demotion** (zero-risk) rather than path-prefixing member routes under `/portal/*` (a larger change; do it later).

---

## 12. Ordered build plan

1. **Migrations 021–023** — `operators`, `operator_keys`, `operator_verifications`, `operator_invites`, `operator_replay`, `operator_nonces` (with `scope`), `conformance_runs`, `conformance_challenges`. Partial unique indexes on live email/phone only. Verify via the Phase-1 integration test with `TEST_DATABASE_URL`.
2. **`sohocloud-protocol`: signing seam** — `Signer`/`Verifier` interfaces + Ed25519 impls (stdlib-only). Scope comment: Layer C only.
3. **`sohocloud-protocol`: `operator/` package** — canon `OperatorTransmission` (`…/operator/v0`, with `uint64(Idx)` conversions), rotate (`…/operator-rotate/v0`), and the domain-separated conformance tag (`…/operator-conformance/v0`).
4. **`internal/operator` repository** — `GetActiveKeyMap` chokepoint (status+onboarding+algo-pin), invite-gated `CreateOperator`, keyset-hash binding, replay CAS + nonce (one tx, fail-closed), lazy-expiry + signed rotate.
5. **2FA (T2)** — session-bound codes + signed-code cross-binding; per-session (not per-target) rate-limit; wire the chosen transport (see Open for user).
6. **Conformance harness** — Suite A (fresh SoHoLINK-computed oracles), Suite B (real `OperatorAuth` path), Suite C (isolated scratch namespace); fresh single-use nonces persisted with `scope='conformance'`.
7. **`OperatorAuth` middleware** — mutually-exclusive-per-route (required, not additive) on the httpjson node-side seam; the 9-step verify order.
8. **Governance `:8090`** — invite minting, activation (with keyset-hash match), revoke, contact-rebind, lockout-clear; independent admin credential; bind to loopback.
9. **Public console templates** — landing reshape, apply, keys, verify, conformance, dashboard, fees.
10. **Governance console templates** — admin queue + detail.
11. **Member-portal re-label** — `transitional_banner.html` partial + `layout.html` nav re-parent. No route/data changes.
12. **`cmd/operator-keygen` + `cmd/operator-conformance`** (Cloudy-side tooling; the keygen is referenced by the apply page; the conformance tool automates the paste-back flow).
13. **Deployment hardening** — confirm `:8090` binds loopback/private only; SSRF audit of public handlers for `localhost:8090` reachability; per-IP 401 rate-limiting (never IP-scoped lockout — Cloudy's fleet shares one egress IP).

---

## 13. Open for user (genuine decisions needed before implementation)

1. **Key custody (go-live blocker).** Where Cloudy stores its 7 private keys, and whether ≥2 live in a separate co-signer/HSM trust domain so 2-of-7 is real threshold protection rather than anti-substitution hygiene. Until decided, 2-of-7 defends only against key-substitution, not single-host compromise.
2. **Fleet sharding / kill-switch blast radius.** One operator identity for all of Cloudy means any revocation or all-keys-expired event denies every fronted member. Confirm whether to shard Cloudy across per-region operator identities before production reliance.
3. **2FA transport.** SoHoLINK has no Twilio dependency today. Add Twilio Verify (phone) + SMTP (email) per the Agrinet reference, or route both through an existing notification path?
4. **`EXPIRATIONS` distribution.** Keep `{3,6,9,12,365}` verbatim per §4.2.2.1, or drop/de-weight the `3` given `KEYS_PER_TX=2` and the `MIN_ACTIVE_KEYS` floor (a `3` threshold risks stranding the operator near the floor)?
5. **Seq window sizing.** Adopted per-`(operator_id, coordinator_id)` with a 256-bit sliding-window bitmap. Confirm the window size is adequate for Cloudy's concurrency/retry volume.
6. **Fee authorship.** Does the operator **declare** fees through the console (self-declared, visible per no-asymmetry — the design assumes this), or does the coordinator **set** them and the operator only views? The whitepaper doesn't fix authorship.
7. **Invite-token policy.** The design gates operator creation behind an admin-minted invite token (resolving the security + architecture findings on public creation). Confirm this is acceptable for the intended operator set (Cloudy, Fruitful) rather than fully-open self-service; and confirm whether well-known slugs (`cloudy`) should be pre-reserved on the invite.
```
