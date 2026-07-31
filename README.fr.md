# SoHoLINK

> Traduction communautaire (version préliminaire) — politique NTARI P2-002, Diffusion multilingue mondiale. Source : README.md (original anglais, instantané du 2026-07-29). Brouillon communautaire assisté par machine, en attente de relecture par le mainteneur régional conformément à P2-002 §3.1. Les spécifications techniques de référence restent en anglais conformément au §2.2.
>
> Vous avez remarqué une erreur de traduction ? N'hésitez pas à la corriger
> vous-même : forkez le dépôt https://github.com/NTARI-RAND/SoHoLINK et ouvrez
> une pull request. Les corrections de traduction sont des contributions
> précieuses, tout autant que le code.

[![Version de Go](https://img.shields.io/badge/Go-1.24%2B-blue.svg)](https://golang.org)
[![Licence](https://img.shields.io/badge/License-AGPL%203.0-blue.svg)](LICENSE.txt)
[![CI](https://github.com/NTARI-RAND/SoHoLINK/actions/workflows/ci.yml/badge.svg)](https://github.com/NTARI-RAND/SoHoLINK/actions/workflows/ci.yml)

**Le coordinateur de référence du substrat de calcul SoHoLINK / Cloudy.** SoHoLINK
transforme la capacité inutilisée de calcul, de stockage et d'impression des machines
détenues par les participants en une place de marché coordonnée : les membres
contribuent du matériel via un front end et perçoivent des paiements réels (en monnaie
fiat) ; d'autres membres achètent à la demande de la capacité de calcul, de stockage,
d'impression et de CDN.

> **Le rôle, en une ligne :** SoHoLINK est le **coordinateur** — il orchestre le travail
> entre les nœuds et le règle. Ce n'est **pas** l'économie des membres, et c'est une
> pièce **remplaçable**. Voir [`CONFORMANCE.md`](CONFORMANCE.md) pour la déclaration
> de rôle complète, exprimée dans les termes propres à l'architecture.

---

## Ce qu'est SoHoLINK

SoHoLINK assure l'**orchestration côté nœud** pour un substrat Janus-Facing Architecture
(JFA) :

- **Reconnaissance des nœuds** — les nœuds annoncent le travail qu'ils accepteront via des listes de capacités signées.
- **Appariement et ordonnancement** — placement des charges de travail sur les nœuds (en tenant compte de la localité, de l'inactivité et du retrait volontaire).
- **Cycle de vie de l'emploi** — offre → acceptation/refus → exécution → rapport → règlement, avec une fenêtre de contestation.
- **Déclarations de frais** — les frais n'existent que sous forme de messages signés par le coordinateur, lisibles et contestables.
- **Règlement en monnaie fiat** — versements aux contributeurs via Stripe Connect.
- **Fédération des front ends** — un objectif de conception (voir *État d'avancement* ci-dessous), pas encore réalisé.

Le coordinateur est un **rôle enfichable et remplaçable.** Toute implémentation conforme
peut le remplacer, et un front end peut faire tourner son propre coordinateur ou apparier
directement les nœuds. Rien dans le réseau n'est obligé de passer par ce coordinateur en
particulier — c'est cette absence de privilège qui empêche un coordinateur de devenir un
pôle central impossible à retirer.

## Ce que SoHoLINK n'est *pas*

- **Pas l'économie des membres JFA.** Aucun crédit émis par les membres, aucun pacte de
  réputation, aucun registre scellé par le dialogue, aucun arbitrage des litiges côté
  membre. Tout cela vit dans le **front end** (Cloudy), par règle architecturale.
- **Aucun token, aucun portefeuille.** Le règlement se fait uniquement en monnaie fiat via
  Stripe Connect, strictement séparé du crédit membre de tout front end.
- **Aucune personne sur le fil réseau.** Les contreparties de SoHoLINK sont des front ends
  et des nœuds ; la seule identité qu'il manipule est l'identité de charge de travail (un
  `NodeID` doté d'une liaison SPIFFE). L'identité des membres, le crédit, la réputation et
  les données personnelles (PII) relèvent du front end.

> **Surfaces transitoires.** L'agent de nœud (`internal/agent`, `cmd/agent`), le portail
> des membres (`internal/portal`, `cmd/portal`, `web/`) et la table `participants` sont des
> capacités appartenant au front end, actuellement hébergées dans ce dépôt, héritées de
> l'époque du double rôle de SoHoLINK. Elles sont maintenues en état de marche et étiquetées
> comme transitoires, en attente de migration vers le front end. Ne les considérez pas comme
> le rôle à long terme du coordinateur.

---

## Sa place dans la pile

```
Front end (Cloudy)  ──┐
                      ├──►  sohocloud-protocol  (the dependency leaf: coordination only)
Coordinator (this) ──┘
```

- SoHoLINK **consomme [`sohocloud-protocol`](https://github.com/NTARI-RAND/sohocloud-protocol)**
  à une version publiée (tag) — le module de coordination « taille fine » (thin waist) qu'il partage avec le front end.
- Il **expose le fil `/v0` du protocole** en parallèle de ses propres routes HTTP existantes,
  de sorte qu'un front end conforme lui parle via le protocole standard pendant que les
  routes transitoires continuent de fonctionner.
- Il ne dépend d'**aucun** front end ni d'**aucun** code de l'économie des membres. L'économie
  des membres vit au-dessus de cette couche et ne doit jamais descendre dedans.

---

## Invariants JFA que ce dépôt respecte

| Invariant | Comment il est respecté ici |
|---|---|
| Les personnes n'apparaissent jamais sur le fil réseau | Les contreparties sont des front ends et des nœuds ; l'identité sur le fil est un `NodeID` SPIFFE |
| Identité de participant unique | Une seule table `participants` — aucune séparation fournisseur/consommateur dans le schéma ni dans le code |
| Lisibilité des frais | Les frais n'existent que sous forme de messages `FeeDeclaration` signés par le coordinateur |
| Le fiat reste fiat ; le crédit reste chez lui | Le règlement se fait uniquement via Stripe Connect ; strictement séparé du crédit membre |
| Aucune asymétrie d'information | La tarification, le comptage et les gains sont visibles pour les participants |
| Surface de gouvernance séparée | L'administration et la gouvernance tournent sur une interface distincte, accessible en local uniquement — jamais sur le site public, jamais derrière des indicateurs de rôle |
| Provenance entrante = sortante | AGPL-3.0 ; signature DCO ; pas de CLA |

---

## Pile technologique

| Couche | Technologie |
|---|---|
| Langage | Go 1.24+ |
| Web | HTML rendu côté serveur avec `html/template` de Go — aucun framework JS, aucune étape de build (doit fonctionner sur un téléphone Android de 2019 en 3G et dans les navigateurs de Smart TV) |
| Base de données | PostgreSQL 16 + TimescaleDB, `pgx/v5` (sans ORM) |
| Stockage objet | MinIO (compatible S3) |
| Identité | SPIFFE/SPIRE — mTLS, SVID X.509 à durée de vie courte |
| Paiements | Stripe Connect (monnaie fiat uniquement) |
| Déploiement | Docker Compose derrière un tunnel Cloudflare |
| CI | GitHub Actions |

---

## Structure du dépôt

```
cmd/
  orchestrator/   Coordinator service: matching, employment lifecycle, /v0 wire, settlement hooks
  portal/         Member portal (transitional, front-end-owned)
  agent/          Node agent (transitional, front-end-owned)
  seed/           Dev/load-test seeder — never point at production
internal/
  api/            Control-plane HTTP API (+ protocol /v0 handlers via protocoladapter)
  orchestrator/   Node registry, matching, placement scoring, job token issuance
  scheduler/      Scoring-based placement (class, freshness, capacity, locality, idle)
  protocoladapter/ Implements coordinator.Coordinator; mounts the /v0 wire
  payment/        Stripe Connect: onboarding, charge, payout, webhook
  identity/       SPIFFE integration, mTLS config, SPIFFE-binding middleware
  store/          PostgreSQL pool + migrations
  agent/ portal/  Transitional front-end-owned capabilities (see note above)
web/              Server-rendered templates + CSS
deploy/           Compose stack, SPIRE config, backup + deploy scripts
docs/             Architecture, operations, and session records
```

---

## Build

Prérequis : Go 1.24+, une instance PostgreSQL 16 + TimescaleDB et Docker (pour la pile
complète).

```bash
git clone https://github.com/NTARI-RAND/SoHoLINK
cd SoHoLINK

# Build the coordinator and portal binaries
go build ./cmd/orchestrator
go build ./cmd/portal

# Run tests (unit)
go test ./...

# Run integration tests against an isolated test database
#   (DB name MUST contain "test" — fixtures refuse to touch any other database)
TEST_DATABASE_URL="postgres://postgres:changeme@localhost:5432/soholink_test?sslmode=disable" \
  go test -tags integration ./...
```

Les migrations de base de données sont appliquées au démarrage du service (golang-migrate,
idempotent). La pile de production complète — Postgres, SPIRE, orchestrator, portal,
ingress — s'exécute via Docker Compose sous `deploy/`.

---

## État d'avancement (en toute franchise)

- **Réalisé et en fonctionnement :** la reconnaissance des nœuds, l'appariement et
  l'ordonnancement, le cycle de vie de l'emploi, le règlement en monnaie fiat (Stripe
  Connect), les déclarations de frais signées par le coordinateur, le fil de coordination
  `/v0`, l'authentification par opérateur ou SPIFFE et l'identité de charge de travail
  SPIFFE sont en production sur **[soholink.org](https://soholink.org)** (domaine de
  confiance `spiffe://soholink.org`).
- **Déploiement à coordinateur unique.** La fédération entre coordinateurs et les points de
  contrôle attestés sont un **objectif de conception, pas une réalisation** — le problème
  ouvert n° 7 de l'architecture (le calcul souverain achète une sortie *mécanique*, et non
  *économique*) s'applique pleinement ; il est nommé, non résolu.
- **L'inscription des opérateurs** (front end en tant qu'opérateur) est vérifiée par les
  tests mais n'a pas encore abouti de bout en bout en production, en attente du transport de
  courrier à deux facteurs.
- **Aucun nœud pilote en service pour l'instant.** Le premier pilote visera une communauté
  résidentielle dense adjacente au siège de NTARI.
- **L'économie des membres est délibérément ailleurs** — le crédit, le pacte et le registre
  vivent dans le front end (Cloudy), une couche plus haut.

---

## Documentation

| Document | Description |
|---|---|
| [`CONFORMANCE.md`](CONFORMANCE.md) | Déclaration de rôle en termes JFA ; correspondances entre invariants et mécanismes ; substituts nommés |
| [`CLAUDE.md`](CLAUDE.md) | Contexte d'ingénierie, conventions et historique de travail |
| `docs/` | Architecture, guides d'exploitation (runbooks) et comptes rendus par session |

Le protocole de coordination partagé est spécifié dans le dépôt
[`sohocloud-protocol`](https://github.com/NTARI-RAND/sohocloud-protocol)
(`SPEC.md`).

---

## Licence

AGPL-3.0-or-later — voir [`LICENSE.txt`](LICENSE.txt). L'AGPL fait de tout le substrat un
bien commun permanent : lisez-le, réimplémentez-le, forkez-le, partez.

*Network Theory Applied Research Institute, Inc. — 501(c)(3) — EIN 92-3047136 — info@ntari.org*
