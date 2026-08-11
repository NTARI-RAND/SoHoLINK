*toki pona:* lipu ni li lipu lili pi kulupu jan. ona li tan lawa NTARI "P2-002". lipu suli ale li lipu "README.md" pi toki Inli (tenpo suno 2026-07-29). ilo sona li pali e lipu ni; kulupu o lukin o pona e ona kepeken "P2-002" § 3.1.

*English:* This is a condensed community rendering under NTARI policy P2-002. The complete document is the English original README.md (snapshot 2026-07-29). Machine-assisted draft pending community review per P2-002 section 3.1.

sina lukin e pakala lon toki ni la o pona e ona: o pali e "fork" lon
https://github.com/NTARI-RAND/SoHoLINK, o pana e "pull request". pana sina
li pona tawa mi mute.

# SoHoLINK

## ni li seme

SoHoLINK li **ilo lawa** ("coordinator") pi kulupu ilo "SoHoLINK / Cloudy". jan
li jo e ilo sona ona; tenpo mute la ilo ni li pali ala. SoHoLINK li kama jo e
wawa pali ni, e poki awen ni, e ken sitelen ni, li pana e ona tawa esun wan. jan
pana li kama jo e **mani lon** — ona li mani pi kulupu ala. jan ante li ken esun e
wawa pali, e poki awen, e sitelen, e pana sona ("CDN").

ona li pana e pali tawa ilo sona, li pini e pali, li pana e mani. taso ona li
**lawa ala** e nasin mani pi jan kulupu. ona li ijo pi ken ante: jan li ken pali e ilo
lawa ante, anu ilo sinpin li ken toki tawa ilo sona kepeken ala ona. tan ni la
ilo lawa wan li ken ala kama nena wan pi kulupu ale. (o lukin e `CONFORMANCE.md`.)

## ona li pali e seme

- **sona e ilo sona** — ilo sona li pana e lipu pi ken pali ona, kepeken sitelen
  ken.
- **pana e pali** — ilo sona li poka anu seme? li pali ala anu seme? jan li wile
  ala la, pali li tawa ala ona.
- **nasin pali** — pana e wile → kama ala kama → pali → toki e pini → pana e
  mani. tenpo pi toki utala li lon kin.
- **lipu mani** — mani pi ilo lawa li lon lipu pi sitelen ken taso. jan ale li
  ken lukin e ona, li ken toki utala e ona.
- **pana e mani** — kepeken ilo "Stripe Connect". mani lon taso; mani sitelen
  ("token") ala; poki mani ala.

## ona li seme ala

jan li lon ala nasin toki ni. SoHoLINK li toki tawa ilo sinpin en ilo sona taso;
nimi pi ilo pali (`NodeID`, nasin "SPIFFE") li nimi wan taso pi ona. sona pi jan,
mani pi kulupu, sona pi pona jan, sitelen awen pi esun — ale ni li lon ilo sinpin
(Cloudy), lon sewi.

ijo pi ilo sinpin li lon poki ni lon tenpo ni taso: `cmd/agent`, `cmd/portal`, en
poki `participants`. ona li kama tawa Cloudy lon tenpo kama.

## ilo pi insa ona

toki ilo "Go", poki sona "PostgreSQL", nasin nimi "SPIFFE/SPIRE". lipu sitelen li
tan ilo lawa taso — nasin "JS" ala, tan ni: ona o pali pona lon ilo lili pi tenpo
2019, lon nasin toki "3G", lon ilo lukin "Smart TV".

## tenpo ni la

- ni li pali, li lon [soholink.org](https://soholink.org): sona e ilo sona, pana
  e pali, nasin pali, pana e mani, lipu mani, nasin toki `/v0`.
- ilo lawa **wan** taso li lon. kulupu pi ilo lawa mute li lon ala: jan li ken
  weka e ilo ona, taso ona li ken ala weka tan esun ale ("open problem" #7). lipu
  li toki e pakala ni, li pona ala e ona.
- ilo sona pi jan kulupu li lon ala. pali open nanpa wan li kama lon ma pi tomo
  mute, lon poka pi tomo lawa NTARI.

## sina ken kama pali

```bash
git clone https://github.com/NTARI-RAND/SoHoLINK
cd SoHoLINK
go build ./cmd/orchestrator
go test ./...
```

nasin toki pi kulupu ale li lon poki
[`sohocloud-protocol`](https://github.com/NTARI-RAND/sohocloud-protocol) (lipu
`SPEC.md`). sona pali li lon `CLAUDE.md` en `docs/`.

lawa lipu: AGPL-3.0-or-later — o lukin, o pali sin, o kipisi, o weka. kulupu ilo
ni li ijo pi jan ale lon tenpo ale.

*Network Theory Applied Research Institute, Inc. — 501(c)(3) — EIN 92-3047136 — info@ntari.org*
