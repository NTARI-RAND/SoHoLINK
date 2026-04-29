# examples/

Reference templates for operator use. These files are starting points — copy
and edit them; do not modify the originals in place.

## allowlist.example.json

Unsigned allowlist template. Copy it to a working file, replace the placeholder
digests with real `sha256:...` digests of published images, bump `version` and
`issued_at`, then sign with `allowlist-sign`. See
[docs/operations/allowlist-signing.md](../docs/operations/allowlist-signing.md)
for the full procedure.
