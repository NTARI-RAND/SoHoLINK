# lipu pi nasin sama — Janus-Facing Architecture (toki pona)

> **toki lili pi lipu ni:** lipu ni li lipu pi kulupu, li tan nasin NTARI "P2-002". lipu suli li lipu Inli "CONFORMANCE.md" (tenpo pi 2026-10-05). ilo sona li pali e lipu ni; kulupu li wile lukin e ona kepeken nasin "P2-002" kipisi "3.1".
>
> **Note:** This is a community rendering under NTARI policy P2-002. The complete document is the English original CONFORMANCE.md (snapshot 2026-10-05). Machine-assisted draft pending community review per P2-002 section 3.1.
>
> sina lukin e pakala lon toki ni la o pona e ona: o pali e "fork" lon
> https://github.com/NTARI-RAND/SoHoLINK, o pana e "pull request". pana sina
> li pona tawa mi mute.
>
> **lipu Inli li lawa.** lipu ni li tawa lukin, li tawa ala sona pi nasin lawa: lipu ni li ante tan `CONFORMANCE.md` la lipu Inli li lawa. nimi lawa (MUST, MUST NOT, SHOULD, SHOULD NOT, MAY), nimi pi poki sona, nimi sitelen en nimi ilo li awen lon toki Inli, tan ni: ona li lipu lawa pi nasin sama.

---

poki lipu ni li toki e ona kepeken nimi pi nasin JFA. ona li toki e ni **lon open**, li toki e ijo pi ilo esun anu pi kepeken lon ma lon monsi, tan nasin pi nanpa toki pi nasin JFA. toki ale pi nasin sama li linja tawa ilo en lukin ni: ilo en lukin li wawa e toki ni. linja li lon ala la sitelen li toki e ni: toki ni li ijo pi tenpo lili (stand-in). toki pi linja ala li toki esun taso.

nasin ni li **Janus-Facing Architecture (JFA)** — lipu wan pi nasin ilo pi kulupu NTARI. ona li lipu pi jan ale (free documentation), li lon poki pi jan ale pi pali ni (nasin "AGPL-3.0"). ona li toki e nimi pi pali, li toki ala e nimi pi ilo esun lon tenpo ale; poki lipu ni li toki e pali ni: ona li pali e seme.

## toki pi pali

ilo SoHoLINK li **ilo insa** (coordinator) pi **kipisi ilo** (substrate) JFA: ona li lawa e pali lon poka pi ilo Node — ona li sona e ilo Node kepeken lipu pi ken pali ona, li alasa e ilo pona tawa pali, li pana e tenpo pali, li lawa e nasin pali tan open tawa pini, li toki e mani pana (fee), li pini e esun kepeken mani pi ma suli (fiat), li lawa e utala — en ona li wan e lupa lukin mute (federation). ilo insa li **pali pi ken ante** (pluggable role): ilo ale pi nasin sama li ken kama lon ma ona, en lupa lukin li ken pali e ilo insa ona, anu ona li ken alasa e ilo Node pona lon nasin poka. ilo insa ni li pali tawa toki awen ni pi kipisi ilo: **lawa pali li lon ilo ni: jan pali jo li ken jo e ilo ni — lupa lili wan pi tomo ilo (hosting chokepoint) pi ken ala weka li lon ala.**

| nimi pi poki lipu ni | pali lon nasin JFA |
|---|---|
| Coordinator (poki lipu ni) | ilo insa: ona li lawa e pali lon poka pi ilo Node; ona li pali pi ken ante (pluggable); ilo ante li ken kama lon ma ona (replaceable) |
| Frontend (ilo Cloudy en ilo ante) | lupa lukin: ilo tawa jan kulupu; jan kulupu en esun pi jan kulupu li lon ni |
| Node | ilo pi jan pali jo — jan pali jo li jo e ilo ni |
| poki leko `participants`, lupa (portal), ilo pali (agent) | **selo pi jan kulupu pi tenpo tawa** (transitional member surfaces) — ken ni li ken pi lupa lukin, taso ona li lon ni lon tenpo lili, li awen e tawa ona; lipu CLAUDE.md li sitelen e ona sama ni |

## nasin awen en linja ona

| nasin awen (nasin JFA) | ilo lon ni | lukin |
|---|---|---|
| **jan li lon ala linja lon tenpo ale** | lon tenpo kama suli la ilo insa li sitelen ala e jan: ona li esun lon poka pi lupa lukin en ilo Node taso; nimi lon linja li nimi pi pali ilo (workload identity) (SPIFFE NodeID) kepeken `sohocloud-protocol` | ona li kepeken nasin insa lon nimi nanpa open (published tag); selo pi jan pi tenpo tawa li jo e sitelen, jan li toki ala e ni: "ona li lon ala" |
| **nimi wan taso pi jan pali jo** | poki leko `participants` wan taso (tawa nanpa 011) — tu pi jan pana en jan kama jo (provider/consumer) li lon ala, lon nasin ilo (schema) anu lon sitelen ilo | nasin ilo (schema); ilo grep li alasa e `providers`/`consumers` la ona li pana e lipu pi tawa pini taso |
| **jan ale li ken lukin e mani pana (fee)** | mani pana li lon nasin wan taso: toki `FeeDeclaration` (ilo insa li sitelen e nimi ona lon ona) — jan li sitelen e ona, jan ale li ken lukin e ona, li ken utala e ona | poki ilo `fees/` pi nasin insa + SPEC |
| **mani pi ma suli li awen mani pi ma suli; mani pi kulupu li awen lon tomo ona** | pini esun li kepeken mani pi ma suli taso (Stripe Connect); sitelen mani (tokens) li lon ala, poki mani (wallets) li lon ala; ona li weka wawa tan mani pi jan kulupu (jan kulupu li pana e ona) pi lupa lukin ale | nasin awen lon lipu CLAUDE.md; poki ilo pi pana mani li toki tawa Stripe taso |
| **sona pi sama ala li lon ala** | jan pali jo ale li ken lukin e nanpa mani pi esun, e nanpa pi kepeken ilo, e mani kama | selo pi lupa (portal); poki leko pi nanpa kepeken |
| **selo lawa li lon poka ante** | lupa pi jan lawa (admin portal) li lon lupa nanpa (port) ante, li ken kama tan poka taso (local-only); ona li open ala tawa jan ale lon tenpo ale; ona li sitelen pali (role-flags) ala lon lupa pi jan ale | nasin pi pana ilo lon ma (deploy configuration) |
| **ma tan: pana insa = pana weka** (inbound = outbound) | AGPL-3.0; DCO (`Signed-off-by` lon pana ale (commit)), lipu CLA ala | lipu ken (license); nasin pali DCO (o lukin e ijo pi tenpo lili: lawa wawa li lape, li jo e nimi "tenpo lili") |

## ijo pi tenpo lili en ijo pi pini ala

- **selo pi jan kulupu pi tenpo tawa.** ilo pali pi ilo Node (node agent), lupa pi jan kulupu (member portal), poki leko `participants`, en ilo pi pana ilo (installer) li ken pi lupa lukin; ona li lon ni tan tenpo pini pi pali tu, li awen e tawa ona. ona li jo e sitelen, li awen pali; jan li toki ala e ni lon tenpo ale: "ona li pali pi tenpo kama suli pi ilo insa."
- **kulupu pi ilo insa mute (federation) li wile pi lipu nasin; jan li pali ala e ona.** sitelen awen pi jan lukin (witnessed checkpoints) (nasin pi lukin open pi lipu lon (certificate-transparency): ona li awen lon kipisi sona, en ijo open `anchor/` pi nasin insa) li wile pi nimi tawa ni: ilo insa wan li ken ala toki tu ante tawa ilo insa ante (non-equivocation). tenpo ni li pini ala la ilo insa wan taso li lon ma: wile sona nanpa 7 pi pini ala pi nasin JFA — ilo sona pi lawa sama (sovereign compute) li pana e ken pi weka kepeken ilo, li pana ala e ken pi weka kepeken mani — li lawa ale, li jo e nimi, li pini ala.
- **lupa lukin li jan pi ilo suli (operator), en ilo li lukin e lon ona (authentication) — pali li pini (BUILT) lon linja pi ilo insa.** tenpo ni la selo `/v0` lon poka pi ilo Node en kama sin pi nimi open pi ilo Node (node-pubkey enrollment) li kama jo e toki pi jan pi ilo suli kepeken nimi open 2 tan 7 (2-of-7) (kulupu pi nimi open luka tu li kama sin lon tenpo, lipu lawa `X-SohoCloud-Operator`, tenpo pi pana sin (replay window) tawa `(operator,coordinator)` ale) kepeken ilo pi wile ni: nasin pi jan pi ilo suli anu nasin SPIFFE. lipu lawa li lon la ona li wile e nasin pi jan pi ilo suli; ona li lon ala la nasin SPIFFE pi tenpo ni li awen tawa ilo Node pi linja poka anu pi poka weka (direct/satellite). lon nasin tu la lon pi ilo Node li kama tan sitelen nimi ed25519 pi toki wan wan (ilo li lukin e ona kepeken `node_protocol_keys`); tan ni la nasin pi pana tawa kepeken jan pi ilo suli (operator-relay) li pana ala e SVID sin tawa ilo Node wan wan. lukin: ilo lukin lili (unit tests) `OperatorAuth`/`OperatorOrSPIFFE`; ilo lukin pi nasin sama (conformance harness) kulupu lukin B (Suite B). ijo pi pini ala: tenpo ni la jan pi ilo suli ala li pini e kama sin tan open tawa pini (ni li ken ala tan ni: nasin pi pana lipu (mail transport) tawa 2FA li pali ala lon tenpo ni); tan ni la nasin li pona lon ilo lukin, taso lon tenpo ni la ona li lon ala pali lon (production).
- **jan ante pi pini esun kepeken mani pi ma suli** (nimi pi jan kulupu tawa kama jo mani, anu lupa lukin) li wile sona open; jan li wile e ni. toki pi pini tu ale li awen lon poka pi mani pi ma suli, li kama ala wan tawa mani pi jan kulupu lon tenpo ale.
- **lawa wawa DCO li lape lon tenpo lili pi nimi (2026-07-12).** nasin pali `dco.yml` li lape — jan li weka ala e ona — tawa pali wawa pi open pi kulupu ilo (stack-establishment push), tan wile pi jan awen (steward). tan nasin pi ma tan pi nasin JFA: toki awen li lape ala lon tenpo ale (pana li awen tawa poki pi jan ale AGPL kepeken nasin pana insa = pana weka (inbound = outbound); Contributor Covenant li ante ala); jan li toki awen e ni: lawa wawa li kama sin, li pali wan taso (`gh workflow enable DCO`); en lupa ni li toki lon — pana (commits) lon tenpo lupa li ken ala kama jo e sitelen lon tan tenpo kama; tan ni la tenpo lupa li awen lili, en open ona en pini ona li lon lipu awen. pana (commits) tan ilo pali (agent) li awen jo e `Signed-off-by` lon tenpo lupa ale, li awen sama ni.

## toki pi ijo wile

ona li kepeken `sohocloud-protocol` lon nimi nanpa open (version tag) — ona li lipu lili pi ijo wile ala (dependency leaf); ilo insa ni en lupa lukin li kepeken ona sama. ona li wile ala e lupa lukin, li wile ala e sitelen ilo pi esun pi jan kulupu; esun pi jan kulupu li lon sewi pi kipisi ni; ona o tawa ala anpa, insa kipisi ni.

## toki pi ilo esun taso (lon pini, tan nasin pi nanpa toki)

ilo SoHoLINK pi kepeken nanpa wan (reference deployment) li lon soholink.org; ma pi pilin awen (trust domain) li `spiffe://soholink.org`. ilo Cloudy li lupa lukin pi kepeken nanpa wan. nasin insa li pana ala e ken suli tawa ona tu.
