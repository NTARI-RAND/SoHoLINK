> **Tradução comunitária (rascunho)** — política P2-002 da NTARI, Transmissão Multilíngue Global. Fonte: README.md (original em inglês, snapshot de 2026-07-29). Rascunho comunitário assistido por máquina, pendente de revisão pelo mantenedor regional conforme a P2-002 §3.1. As especificações técnicas centrais permanecem em inglês conforme a §2.2.
>
> Notou algum erro nesta tradução? Correções de tradução são contribuições
> valiosas e muito bem-vindas: faça um fork do repositório e abra um pull
> request em https://github.com/NTARI-RAND/SoHoLINK.

# SoHoLINK

[![Versão do Go](https://img.shields.io/badge/Go-1.24%2B-blue.svg)](https://golang.org)
[![Licença](https://img.shields.io/badge/License-AGPL%203.0-blue.svg)](LICENSE.txt)
[![CI](https://github.com/NTARI-RAND/SoHoLINK/actions/workflows/ci.yml/badge.svg)](https://github.com/NTARI-RAND/SoHoLINK/actions/workflows/ci.yml)

**O coordenador de referência do substrato de computação SoHoLINK / Cloudy.** O SoHoLINK
transforma a capacidade ociosa de computação, armazenamento e impressão de máquinas
pertencentes aos participantes em um mercado coordenado: os membros contribuem com
hardware por meio de um front end e recebem pagamentos reais (em moeda fiduciária);
outros membros compram capacidade de computação, armazenamento, impressão e CDN sob demanda.

> **Papel, em uma linha:** o SoHoLINK é o **coordenador** — ele orquestra o trabalho
> entre os nós e o liquida. Ele **não** é a economia de membros, e é uma parte
> **substituível**. Consulte [`CONFORMANCE.md`](CONFORMANCE.md) para a declaração
> completa de papel nos termos da própria arquitetura.

---

## O que o SoHoLINK é

O SoHoLINK realiza a **orquestração do lado do nó** para um substrato Janus-Facing
Architecture (JFA):

- **Reconhecimento de nós** — os nós anunciam que trabalho farão por meio de listagens de capacidade assinadas.
- **Pareamento e agendamento** — alocação de cargas de trabalho nos nós (ciente de localidade, ciente de ociosidade e respeitando o opt-out).
- **Ciclo de vida do emprego** — oferta → aceitar/recusar → executar → reportar → liquidar, com uma janela de contestação.
- **Declarações de tarifas** — as tarifas existem apenas como mensagens assinadas pelo coordenador, legíveis e contestáveis.
- **Liquidação em moeda fiduciária** — repasses aos contribuidores via Stripe Connect.
- **Federação de front ends** — um objetivo de projeto (veja *Status* abaixo), ainda não construído.

O coordenador é um papel **plugável e substituível.** Qualquer implementação conforme
pode substituí-lo, e um front end pode executar seu próprio coordenador ou fazer o
pareamento diretamente com os nós. Nada na rede é obrigado a passar por este
coordenador em particular — é essa ausência de privilégio que impede um coordenador
de se tornar um hub inamovível.

## O que o SoHoLINK *não* é

- **Não é a economia de membros do JFA.** Não há crédito emitido por membros, nem pacto de
  reputação, nem registro selado por diálogo, nem julgamento de disputas voltado ao membro.
  Isso vive no **front end** (Cloudy), por regra arquitetural.
- **Sem tokens, sem carteiras.** A liquidação é puramente em moeda fiduciária via Stripe
  Connect, mantida estritamente separada do crédito de membro de qualquer front end.
- **Nenhuma pessoa na via de comunicação.** As contrapartes do SoHoLINK são front ends e
  nós; a única identidade que ele manipula é a identidade da carga de trabalho (um `NodeID`
  com vínculo SPIFFE). Identidade de membro, crédito, reputação e PII (dados pessoais) são
  assuntos do front end.

> **Superfícies transitórias.** O agente de nó (`internal/agent`, `cmd/agent`), o portal do
> membro (`internal/portal`, `cmd/portal`, `web/`) e a tabela `participants` são
> capacidades pertencentes ao front end que atualmente estão hospedadas neste repositório,
> herdadas da antiga era de papel duplo do SoHoLINK. Elas são mantidas em funcionamento e
> rotuladas como transitórias, aguardando migração para o front end. Não as trate como o
> papel de longo prazo do coordenador.

---

## Onde ele se encaixa na pilha

```
Front end (Cloudy)  ──┐
                      ├──►  sohocloud-protocol  (the dependency leaf: coordination only)
Coordinator (this) ──┘
```

- O SoHoLINK **consome o [`sohocloud-protocol`](https://github.com/NTARI-RAND/sohocloud-protocol)**
  em uma tag de versão publicada — o módulo de coordenação de cintura estreita (*thin waist*) que ele compartilha com o front end.
- Ele **serve a via `/v0` do protocolo** ao lado de suas próprias rotas HTTP existentes, de
  modo que um front end conforme fala com ele pelo protocolo padrão enquanto as rotas
  transitórias continuam funcionando.
- Ele **não** depende de nenhum front end nem de nenhum código da economia de membros. A
  economia de membros vive acima desta camada e nunca deve migrar para baixo, para dentro dela.

---

## Invariantes do JFA que este repositório sustenta

| Invariante | Como é atendido aqui |
|---|---|
| Pessoas nunca aparecem na via de comunicação | As contrapartes são front ends e nós; a identidade na via é um `NodeID` SPIFFE |
| Identidade única de participante | Uma única tabela `participants` — sem separação provedor/consumidor no esquema ou no código |
| Legibilidade das tarifas | As tarifas existem apenas como mensagens `FeeDeclaration` assinadas pelo coordenador |
| A moeda fiduciária continua fiduciária; o crédito fica em casa | A liquidação é puramente Stripe Connect; estritamente separada do crédito de membro |
| Sem assimetria de informação | Preços, medição e ganhos são visíveis para os participantes |
| Superfície de governança separada | A administração/governança roda em uma interface separada, apenas local — nunca no site público, nunca atrás de flags de papel |
| Procedência de entrada = de saída | AGPL-3.0; assinatura DCO (*sign-off*); sem CLA |

---

## Pilha tecnológica

| Camada | Tecnologia |
|---|---|
| Linguagem | Go 1.24+ |
| Web | HTML renderizado no servidor via `html/template` do Go — sem framework JS, sem etapa de build (precisa funcionar em um celular Android de 2019 em 3G e em navegadores de Smart TV) |
| Banco de dados | PostgreSQL 16 + TimescaleDB, `pgx/v5` (sem ORM) |
| Armazenamento de objetos | MinIO (compatível com S3) |
| Identidade | SPIFFE/SPIRE — mTLS, SVIDs X.509 de curta duração |
| Pagamentos | Stripe Connect (apenas moeda fiduciária) |
| Implantação | Docker Compose atrás de um túnel Cloudflare |
| CI | GitHub Actions |

---

## Estrutura do repositório

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

Pré-requisitos: Go 1.24+, uma instância de PostgreSQL 16 + TimescaleDB e Docker (para a
pilha completa).

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

As migrações do banco de dados são aplicadas na inicialização do serviço (golang-migrate,
idempotente). A pilha de produção completa — Postgres, SPIRE, orchestrator, portal, ingress
— roda via Docker Compose em `deploy/`.

---

## Status (honesto)

- **Construído e em funcionamento:** reconhecimento de nós, pareamento/agendamento, o ciclo
  de vida do emprego, liquidação em moeda fiduciária (Stripe Connect), declarações de tarifas
  assinadas pelo coordenador, a via de coordenação `/v0`, autenticação por operador ou SPIFFE
  e a identidade de carga de trabalho SPIFFE estão em produção em
  **[soholink.org](https://soholink.org)** (domínio de confiança `spiffe://soholink.org`).
- **Implantação com um único coordenador.** A federação entre coordenadores e os checkpoints
  testemunhados são um **objetivo de projeto, não construídos** — o problema aberto 7 da
  arquitetura (computação soberana compra saída *mecânica*, não *econômica*) se aplica
  integralmente e está nomeado, não resolvido.
- **O cadastro de operador** (front end como operador) está verificado em testes, mas ainda
  não foi concluído de ponta a ponta em produção, aguardando o transporte de e-mail de dois
  fatores.
- **Ainda não há nós piloto ativos.** O primeiro piloto tem como alvo uma comunidade
  residencial densa adjacente à sede da NTARI.
- **A economia de membros está deliberadamente em outro lugar** — crédito, pacto e registro
  vivem no front end (Cloudy), uma camada acima.

---

## Documentação

| Documento | Descrição |
|---|---|
| [`CONFORMANCE.md`](CONFORMANCE.md) | Declaração de papel em termos do JFA; vínculos entre invariantes e mecanismos; substitutos nomeados |
| [`CLAUDE.md`](CLAUDE.md) | Contexto de engenharia, convenções e o histórico de trabalho |
| `docs/` | Arquitetura, runbooks de operação e registros por sessão |

O protocolo de coordenação compartilhado é especificado no repositório
[`sohocloud-protocol`](https://github.com/NTARI-RAND/sohocloud-protocol)
(`SPEC.md`).

---

## Licença

AGPL-3.0-or-later — veja [`LICENSE.txt`](LICENSE.txt). A AGPL mantém todo o substrato como
um bem comum permanente: leia-o, reimplemente-o, faça um fork dele, vá embora.

*Network Theory Applied Research Institute, Inc. — 501(c)(3) — EIN 92-3047136 — info@ntari.org*
