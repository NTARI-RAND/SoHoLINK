> Traduction communautaire (brouillon) — politique NTARI P2-002, Diffusion multilingue mondiale. Source : CONFORMANCE.md (original anglais, instantané du 2026-10-05). Brouillon communautaire assisté par machine, en attente de relecture par le mainteneur régional conformément à P2-002 §3.1. Les spécifications techniques essentielles restent en anglais conformément au §2.2.
>
> Vous avez remarqué une erreur de traduction ? N'hésitez pas à la corriger
> vous-même : forkez le dépôt https://github.com/NTARI-RAND/SoHoLINK et
> ouvrez une pull request. Les corrections de traduction sont des
> contributions précieuses, tout autant que le code.

> L'original anglais fait foi. Cette traduction est proposée pour la lecture, non pour l'interprétation : là où elle diverge de `CONFORMANCE.md`, c'est l'anglais qui prévaut. Les mots-clés normatifs (MUST, MUST NOT, SHOULD, SHOULD NOT, MAY), les noms de champs, les étiquettes et les identifiants restent en anglais, car ils constituent le contrat de conformité.

# Conformité — Janus-Facing Architecture

L'autodescription du dépôt dans les termes propres à l'architecture, énoncée **avant** tout élément propre au produit ou au déploiement, conformément à la règle d'ordonnancement de l'architecture. Chaque affirmation de conformité est rattachée au mécanisme et à la vérification qui la font respecter ; à défaut, elle est étiquetée comme substitution. Une prose non rattachée relève du marketing.

L'architecture est **Janus-Facing Architecture (JFA)** — le document d'architecture unifié de NTARI, documentation libre placée sous les communs AGPL-3.0 du projet. Elle nomme des rôles, jamais des produits ; ce dépôt déclare le rôle qu'il remplit.

## Déclaration de rôle

SoHoLINK est le **coordinateur** d'un **substrat** JFA : l'orchestration côté nœud — la reconnaissance des nœuds au moyen des annonces de capacités, l'appariement et la planification, le cycle de vie de l'emploi, les déclarations de frais, le règlement en monnaie fiat, le traitement des litiges — et la fédération des frontends. Le coordinateur est un **rôle interchangeable** : toute implémentation conforme peut le remplacer, et un frontend peut exécuter son propre coordinateur ou s'apparier directement avec des nœuds. La garantie du substrat qu'il sert : **une coordination sur une infrastructure que les participants peuvent détenir — aucun point d'étranglement d'hébergement inamovible.**

| Terme de ce dépôt | Rôle dans l'architecture |
|---|---|
| Coordinator (ce dépôt) | le coordinateur : l'orchestration côté nœud, interchangeable, remplaçable |
| Frontend (Cloudy et autres) | un frontend : l'application destinée aux membres ; là où vivent les membres et l'économie des membres |
| Node | l'infrastructure détenue par les participants |
| Table `participants`, portail, agent | **surfaces transitoires destinées aux membres** — des capacités appartenant au frontend, hébergées ici en attendant leur migration, étiquetées comme telles dans CLAUDE.md |

## Invariants et leurs rattachements

| Invariant (architecture) | Mécanisme ici | Vérification |
|---|---|---|
| **Les personnes n'apparaissent jamais sur le fil** | À long terme, le coordinateur ne modélise aucune personne : ses contreparties sont des frontends et des nœuds ; l'identité sur le fil est l'identité de charge de travail (NodeID SPIFFE) via `sohocloud-protocol` | Protocole consommé à une étiquette publiée ; les surfaces transitoires relatives aux personnes sont étiquetées, non niées |
| **Identité unique du participant** | Une seule table `participants` (migration 011) — aucune séparation fournisseur/consommateur n'existe dans le schéma ni dans le code | Schéma ; une recherche grep de `providers`/`consumers` ne renvoie que l'historique des migrations |
| **Lisibilité des frais** | Les frais n'existent que sous la forme de messages `FeeDeclaration` signés par le coordinateur — dotés d'un auteur, lisibles, contestables | Paquet `fees/` du protocole + SPEC |
| **Le fiat reste fiat ; le crédit reste chez lui** | Le règlement se fait uniquement en monnaie fiat (Stripe Connect) ; aucun jeton, aucun portefeuille ; strictement séparé du crédit émis par les membres de tout frontend | Invariant de CLAUDE.md ; le paquet de paiement ne touche qu'à Stripe |
| **Aucune asymétrie d'information** | Les prix, le comptage de la consommation et les gains sont visibles par tous les participants | Surfaces du portail ; tables de comptage |
| **Surface de gouvernance séparée** | Portail d'administration sur un port distinct, uniquement local, jamais exposé publiquement, et non des indicateurs de rôle sur le portail public | Configuration de déploiement |
| **Provenance entrant = sortant** | AGPL-3.0 ; DCO (`Signed-off-by` à chaque commit), aucun CLA | Licence ; workflow DCO (voir les substitutions : application suspendue à titre de mesure provisoire désignée) |

## Substitutions et résidus ouverts

- **Surfaces transitoires destinées aux membres.** L'agent de nœud, le portail des membres, la table `participants` et l'installateur sont des capacités appartenant au frontend, hébergées ici depuis l'époque du double rôle, en attendant leur migration. Elles sont étiquetées, maintenues en état de fonctionnement, et jamais décrites comme le rôle à long terme du coordinateur.
- **La fédération des coordinateurs est une cible de conception, non construite.** Les points de contrôle attestés par témoins (le modèle de transparence des certificats réservé dans la couche registre et l'ébauche `anchor/` du protocole) sont la cible désignée pour la non-équivocation entre coordinateurs. D'ici là, il s'agit d'un déploiement à coordinateur unique : le problème ouvert 7 de l'architecture — le calcul souverain offre une sortie mécanique, non économique — s'applique pleinement et est nommé, non résolu.
- **Authentification du frontend en tant qu'opérateur — CONSTRUITE sur le fil de coordination.** La surface `/v0` côté nœud et l'enrôlement des clés publiques de nœud acceptent désormais une transmission d'opérateur 2-sur-7 (jeu de sept clés en rotation, en-tête `X-SohoCloud-Operator`, fenêtre anti-rejeu par `(operator,coordinator)`) via un sélecteur opérateur-ou-SPIFFE : la présence de l'en-tête sélectionne le chemin opérateur ; son absence conserve le chemin SPIFFE existant pour les nœuds directs/satellites. Dans les deux chemins, l'authenticité du nœud provient de la propre signature ed25519 de chaque message (vérifiée par rapport à `node_protocol_keys`), de sorte que le chemin de relais par opérateur n'ajoute aucun SVID par nœud. Vérification : tests unitaires `OperatorAuth`/`OperatorOrSPIFFE` ; suite B du harnais de conformité. Résidu : aucun opérateur n'a encore mené l'enrôlement à son terme de bout en bout (bloqué par le transport des e-mails de 2FA) ; le chemin est donc vérifié en test, mais pas encore en production.
- **La contrepartie du règlement en monnaie fiat** (identité de versement du membre ou frontend) est une question délibérément laissée ouverte ; quelle que soit la réponse, elle reste du côté fiat, sans jamais être confondue avec le crédit des membres.
- **L'application du DCO est suspendue à titre de mesure provisoire désignée (2026-07-12).** Le workflow `dco.yml` est désactivé — non supprimé — pour la phase de mise en place de la pile, par décision de l'intendant. Conformément à la norme de provenance de l'architecture : le pacte lui-même n'est jamais suspendu (les contributions entrent toujours dans les communs AGPL selon le principe entrant = sortant, et le Contributor Covenant n'est pas affecté) ; le rétablissement fait l'objet d'un engagement et tient en une seule action (`gh workflow enable DCO`) ; et la lacune est assumée honnêtement — les commits effectués pendant cette fenêtre ne peuvent pas être certifiés rétroactivement, c'est pourquoi la fenêtre est maintenue courte, et son début comme sa fin sont consignés. Les commits rédigés par des agents continuent quoi qu'il en soit de porter `Signed-off-by` pendant toute la fenêtre.

## Déclaration de dépendances

Consomme `sohocloud-protocol` à une étiquette de version publiée — la feuille de dépendances que partagent ce coordinateur et les frontends. Ne dépend d'aucun frontend ni d'aucun code de l'économie des membres ; l'économie des membres vit au-dessus de cette couche et ne doit pas migrer vers le bas, dans celle-ci.

## Notes propres au produit (en dernier, conformément à la règle d'ordonnancement)

Déploiement de référence sur soholink.org ; domaine de confiance `spiffe://soholink.org`. Cloudy est le frontend de référence. Aucun des deux n'est privilégié par le protocole.
