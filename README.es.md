> Traducción comunitaria (borrador) — Política P2-002 de NTARI, Difusión Global Multilingüe. Fuente: README.md (original en inglés, captura del 2026-07-29). Borrador comunitario asistido por máquina, pendiente de revisión por el mantenedor regional conforme a P2-002 §3.1. Las especificaciones técnicas centrales permanecen en inglés conforme a §2.2.
>
> ¿Encontraste un error en esta traducción? Tu corrección es una contribución
> bienvenida y valorada: haz un fork del repositorio y abre un pull request en
> https://github.com/NTARI-RAND/SoHoLINK.

# SoHoLINK

[![Versión de Go](https://img.shields.io/badge/Go-1.24%2B-blue.svg)](https://golang.org)
[![Licencia](https://img.shields.io/badge/License-AGPL%203.0-blue.svg)](LICENSE.txt)
[![CI](https://github.com/NTARI-RAND/SoHoLINK/actions/workflows/ci.yml/badge.svg)](https://github.com/NTARI-RAND/SoHoLINK/actions/workflows/ci.yml)

**El coordinador de referencia del sustrato de cómputo SoHoLINK / Cloudy.** SoHoLINK
convierte la capacidad ociosa de cómputo, almacenamiento e impresión de las máquinas
propiedad de los participantes en un mercado coordinado: los miembros aportan hardware
a través de un front end y reciben pagos reales (en moneda fiduciaria); otros miembros
compran capacidad de cómputo, almacenamiento, impresión y CDN bajo demanda.

> **Su rol, en una línea:** SoHoLINK es el **coordinador**: orquesta el trabajo
> entre nodos y lo liquida. **No** es la economía de miembros, y es una pieza
> **reemplazable**. Consulte [`CONFORMANCE.md`](CONFORMANCE.md) para la declaración
> completa del rol en los términos propios de la arquitectura.

---

## Qué es SoHoLINK

SoHoLINK realiza la **orquestación del lado de los nodos** para un sustrato de
Janus-Facing Architecture (JFA):

- **Reconocimiento de nodos** — los nodos anuncian qué trabajo realizarán mediante listados de capacidades firmados.
- **Emparejamiento y planificación** — colocación de cargas de trabajo en los nodos (consciente de la localidad, consciente del estado ocioso y respetuosa de las exclusiones voluntarias).
- **Ciclo de vida del empleo** — oferta → aceptar/rechazar → ejecutar → reportar → liquidar, con una ventana de disputa.
- **Declaraciones de tarifas** — las tarifas existen únicamente como mensajes firmados por el coordinador, legibles y contestables.
- **Liquidación en moneda fiduciaria** — pagos a los contribuyentes mediante Stripe Connect.
- **Federación de front ends** — un objetivo de diseño (véase *Estado* más abajo), aún no construido.

El coordinador es un **rol conectable y reemplazable.** Cualquier implementación conforme
puede sustituirlo, y un front end puede ejecutar su propio coordinador o emparejarse
directamente con los nodos. Nada en la red está obligado a enrutar a través de este
coordinador en particular; esa ausencia de privilegio es lo que impide que un coordinador
se convierta en un hub imposible de remover.

## Qué *no* es SoHoLINK

- **No es la economía de miembros de la JFA.** Sin crédito emitido por miembros, sin pacto
  de reputación, sin registro sellado por diálogo, sin adjudicación de disputas de cara a
  los miembros. Todo eso vive en el **front end** (Cloudy), por regla arquitectónica.
- **Sin tokens, sin billeteras.** La liquidación es puramente en moneda fiduciaria mediante
  Stripe Connect, mantenida estrictamente separada del crédito de miembros de cualquier
  front end.
- **Sin personas en el cable.** Las contrapartes de SoHoLINK son front ends y nodos; la
  única identidad que maneja es la identidad de carga de trabajo (un `NodeID` con
  vinculación SPIFFE). La identidad de los miembros, el crédito, la reputación y la
  información personal identificable (PII) son asunto del front end.

> **Superficies transicionales.** El agente de nodo (`internal/agent`, `cmd/agent`), el
> portal de miembros (`internal/portal`, `cmd/portal`, `web/`) y la tabla `participants`
> son capacidades propiedad del front end alojadas actualmente en este repositorio,
> herencia de la era anterior de doble rol de SoHoLINK. Se mantienen funcionando y
> etiquetadas como transicionales, a la espera de su migración al front end. No las trate
> como el rol de largo plazo del coordinador.

---

## Dónde se ubica en el stack

```
Front end (Cloudy)  ──┐
                      ├──►  sohocloud-protocol  (the dependency leaf: coordination only)
Coordinator (this) ──┘
```

- SoHoLINK **consume [`sohocloud-protocol`](https://github.com/NTARI-RAND/sohocloud-protocol)**
  en una etiqueta de versión publicada: el módulo de coordinación de "cintura delgada"
  (thin waist) que comparte con el front end.
- **Sirve el cable `/v0` del protocolo** junto a sus propias rutas HTTP existentes, de
  modo que un front end conforme se comunica con él mediante el protocolo estándar
  mientras las rutas transicionales siguen funcionando.
- No depende de **ningún** front end ni de **ningún** código de la economía de miembros.
  La economía de miembros vive por encima de esta capa y nunca debe migrar hacia abajo,
  dentro de ella.

---

## Invariantes de la JFA que este repositorio sostiene

| Invariante | Cómo se cumple aquí |
|---|---|
| Las personas nunca aparecen en el cable | Las contrapartes son front ends y nodos; la identidad en el cable es un `NodeID` de SPIFFE |
| Identidad única de participante | Una sola tabla `participants` — sin división proveedor/consumidor en el esquema ni en el código |
| Legibilidad de las tarifas | Las tarifas existen únicamente como mensajes `FeeDeclaration` firmados por el coordinador |
| La moneda fiduciaria se queda fiduciaria; el crédito se queda en casa | La liquidación es puramente Stripe Connect; estrictamente separada del crédito de miembros |
| Sin asimetría de información | Los precios, la medición y las ganancias son visibles para los participantes |
| Superficie de gobernanza separada | La administración/gobernanza se ejecuta en una interfaz separada, de acceso exclusivamente local — nunca en el sitio público, nunca detrás de banderas de rol |
| Procedencia entrante = saliente | AGPL-3.0; firma DCO (sign-off); sin CLA |

---

## Stack tecnológico

| Capa | Tecnología |
|---|---|
| Lenguaje | Go 1.24+ |
| Web | HTML renderizado en el servidor mediante `html/template` de Go — sin framework de JS, sin paso de build (debe funcionar en un teléfono Android de 2019 con 3G y en navegadores de Smart TV) |
| Base de datos | PostgreSQL 16 + TimescaleDB, `pgx/v5` (sin ORM) |
| Almacenamiento de objetos | MinIO (compatible con S3) |
| Identidad | SPIFFE/SPIRE — mTLS, SVID X.509 de corta duración |
| Pagos | Stripe Connect (solo moneda fiduciaria) |
| Despliegue | Docker Compose detrás de un túnel de Cloudflare |
| CI | GitHub Actions |

---

## Estructura del repositorio

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

## Compilación

Requisitos previos: Go 1.24+, una instancia de PostgreSQL 16 + TimescaleDB y Docker
(para el stack completo).

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

Las migraciones de la base de datos se aplican al arrancar el servicio (golang-migrate,
idempotentes). El stack completo de producción — Postgres, SPIRE, orquestador, portal,
ingress — se ejecuta mediante Docker Compose bajo `deploy/`.

---

## Estado (honesto)

- **Construido y en funcionamiento:** el reconocimiento de nodos, el emparejamiento y la
  planificación, el ciclo de vida del empleo, la liquidación en moneda fiduciaria (Stripe
  Connect), las declaraciones de tarifas firmadas por el coordinador, el cable de
  coordinación `/v0`, la autenticación operador-o-SPIFFE y la identidad de carga de
  trabajo SPIFFE están en producción en **[soholink.org](https://soholink.org)** (dominio
  de confianza `spiffe://soholink.org`).
- **Despliegue de coordinador único.** La federación entre coordinadores y los puntos de
  control atestiguados son un **objetivo de diseño, no algo construido** — el problema
  abierto 7 de la arquitectura (el cómputo soberano compra una salida *mecánica*, no
  *económica*) aplica en su totalidad y está nombrado, no resuelto.
- La **inscripción de operadores** (front end como operador) está verificada en pruebas,
  pero no se ha completado de extremo a extremo en producción, a la espera del transporte
  de correo de dos factores.
- **Todavía no hay nodos piloto en vivo.** El primer piloto apunta a una comunidad
  residencial densa adyacente a la sede de NTARI.
- **La economía de miembros está deliberadamente en otro lugar** — el crédito, el pacto y
  el registro viven en el front end (Cloudy), una capa más arriba.

---

## Documentación

| Documento | Descripción |
|---|---|
| [`CONFORMANCE.md`](CONFORMANCE.md) | Declaración del rol en términos de la JFA; vinculaciones de invariante a mecanismo; sustitutos nombrados |
| [`CLAUDE.md`](CLAUDE.md) | Contexto de ingeniería, convenciones y el historial de trabajo |
| `docs/` | Arquitectura, runbooks de operaciones y registros por sesión |

El protocolo de coordinación compartido está especificado en el repositorio
[`sohocloud-protocol`](https://github.com/NTARI-RAND/sohocloud-protocol)
(`SPEC.md`).

---

## Licencia

AGPL-3.0-or-later — véase [`LICENSE.txt`](LICENSE.txt). La AGPL mantiene todo el sustrato
como un bien común permanente: léalo, reimpleméntelo, bifúrquelo (fork), váyase.

*Network Theory Applied Research Institute, Inc. — 501(c)(3) — EIN 92-3047136 — info@ntari.org*
