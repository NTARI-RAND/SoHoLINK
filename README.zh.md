> 社区翻译（草稿）—— NTARI 政策 P2-002《全球多语言广播政策》。来源：README.md（英文原版，快照日期 2026-07-29）。本文为机器辅助的社区翻译草稿，尚待区域维护者依据 P2-002 §3.1 审校。根据 §2.2，核心技术规范仍以英文为准。
>
> 如发现译文有误，欢迎 fork 仓库并提交 Pull Request
> 来改进翻译：https://github.com/NTARI-RAND/SoHoLINK。翻译修正与代码贡献同样宝贵，我们诚挚欢迎。

# SoHoLINK

[![Go 版本](https://img.shields.io/badge/Go-1.24%2B-blue.svg)](https://golang.org)
[![许可证](https://img.shields.io/badge/License-AGPL%203.0-blue.svg)](LICENSE.txt)
[![CI](https://github.com/NTARI-RAND/SoHoLINK/actions/workflows/ci.yml/badge.svg)](https://github.com/NTARI-RAND/SoHoLINK/actions/workflows/ci.yml)

**SoHoLINK / Cloudy 计算基底（compute substrate）的参考协调器（reference coordinator）。**
SoHoLINK 将参与者自有设备上闲置的计算、存储和打印能力转化为一个有组织协调的市场：
成员通过前端贡献硬件并获得真实的（法币）报酬；其他成员则按需购买计算、存储、打印
和 CDN 容量。

> **一句话概括其角色：** SoHoLINK 是**协调器**——它在各节点之间编排工作并完成结算。
> 它**不是**成员经济，并且是一个**可替换**的组成部分。完整的角色声明（以该架构自身
> 的术语表述）见 [`CONFORMANCE.md`](CONFORMANCE.md)。

---

## SoHoLINK 是什么

SoHoLINK 为 Janus-Facing Architecture（JFA）基底执行**节点侧编排**：

- **节点识别** —— 节点通过带签名的能力清单（capability listings）声明自己愿意承接的工作。
- **匹配与调度** —— 将工作负载放置到节点上（感知本地性、感知空闲状态、尊重节点的退出选择）。
- **雇佣生命周期** —— 报价 → 接受/拒绝 → 运行 → 报告 → 结算，并附带争议窗口期。
- **费用声明** —— 费用只以协调器签名的、清晰可读且可提出异议的消息形式存在。
- **法币结算** —— 通过 Stripe Connect 向贡献者支付报酬。
- **前端联邦** —— 属于设计目标（见下文*现状*），尚未构建。

协调器是一个**可插拔、可替换的角色**。任何符合规范的实现都可以替换它，前端也可以
运行自己的协调器，或直接与节点进行匹配。网络中没有任何环节被要求必须经由这个特定
的协调器路由——正是这种非特权地位，防止了协调器演变为不可移除的中心枢纽。

## SoHoLINK *不是*什么

- **不是 JFA 的成员经济。** 没有成员发行的信用，没有声誉契约（reputation covenant），
  没有对话封存记录（dialog-sealed record），也没有面向成员的争议裁决。按照架构规则，
  这些都位于**前端**（Cloudy）中。
- **没有代币，没有钱包。** 结算完全通过 Stripe Connect 以法币进行，并与任何前端的
  成员信用严格分离。
- **通信线路上不出现自然人。** SoHoLINK 的交互对手是前端和节点；它处理的唯一身份是
  工作负载身份（带有 SPIFFE 绑定的 `NodeID`）。成员身份、信用、声誉和个人身份信息
  （PII）均由前端负责。

> **过渡性组件。** 节点代理（`internal/agent`、`cmd/agent`）、成员门户
> （`internal/portal`、`cmd/portal`、`web/`）以及 `participants` 表属于前端所有的
> 能力，因 SoHoLINK 早期身兼双重角色而暂时托管在本仓库中。它们会保持可用并被标记
> 为过渡性组件，等待迁移到前端。请勿将其视为协调器的长期职责。

---

## 它在技术栈中的位置

```
Front end (Cloudy)  ──┐
                      ├──►  sohocloud-protocol  (the dependency leaf: coordination only)
Coordinator (this) ──┘
```

- SoHoLINK 以已发布的版本标签**消费 [`sohocloud-protocol`](https://github.com/NTARI-RAND/sohocloud-protocol)**
  ——这是它与前端共享的“细腰”（thin-waist）协调模块。
- 它在自身既有 HTTP 路由之外**同时提供该协议的 `/v0` 通信线路（wire）**，因此符合
  规范的前端可以通过标准协议与它通信，而过渡性路由仍可继续工作。
- 它**不**依赖任何前端，也**不**依赖任何成员经济代码。成员经济位于此层之上，
  绝不能向下迁移到这一层中。

---

## 本仓库遵守的 JFA 不变量

| 不变量 | 在本仓库中的实现方式 |
|---|---|
| 通信线路上绝不出现自然人 | 交互对手是前端和节点；线路上的身份是 SPIFFE `NodeID` |
| 单一参与者身份 | 只有一张 `participants` 表——数据库模式和代码中均不区分提供方/消费方 |
| 费用透明可读 | 费用只以协调器签名的 `FeeDeclaration` 消息形式存在 |
| 法币归法币，信用留原处 | 结算完全通过 Stripe Connect 进行，与成员信用严格分离 |
| 无信息不对称 | 定价、计量和收益对参与者可见 |
| 治理界面分离 | 管理/治理运行在独立的、仅限本地访问的界面上——绝不放在公共站点上，也绝不藏在角色开关（role flags）之后 |
| 输入与输出的许可来源对等 | AGPL-3.0；DCO 签署（sign-off）；无 CLA |

---

## 技术栈

| 层 | 技术 |
|---|---|
| 语言 | Go 1.24+ |
| Web | 通过 Go `html/template` 服务端渲染 HTML——无 JS 框架、无构建步骤（必须能在使用 3G 网络的 2019 款 Android 手机及智能电视浏览器中正常工作） |
| 数据库 | PostgreSQL 16 + TimescaleDB，`pgx/v5`（不使用 ORM） |
| 对象存储 | MinIO（兼容 S3） |
| 身份 | SPIFFE/SPIRE——mTLS、短生命周期的 X.509 SVID |
| 支付 | Stripe Connect（仅法币） |
| 部署 | Docker Compose，置于 Cloudflare 隧道之后 |
| CI | GitHub Actions |

---

## 仓库结构

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

## 构建

先决条件：Go 1.24+、一个 PostgreSQL 16 + TimescaleDB 实例，以及 Docker（用于运行
完整技术栈）。

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

数据库迁移在服务启动时自动应用（golang-migrate，幂等）。完整的生产技术栈——
Postgres、SPIRE、orchestrator、portal、入口网关（ingress）——通过 `deploy/` 目录
下的 Docker Compose 运行。

---

## 现状（如实说明）

- **已构建并在运行：** 节点识别、匹配/调度、雇佣生命周期、法币结算（Stripe Connect）、
  协调器签名的费用声明、`/v0` 协调通信线路、运营方或 SPIFFE 认证，以及 SPIFFE 工作
  负载身份，均已在 **[soholink.org](https://soholink.org)** 的生产环境中运行（信任域
  `spiffe://soholink.org`）。
- **单协调器部署。** 跨协调器联邦与见证检查点（witnessed checkpoints）属于**设计目标，
  尚未构建**——该架构的开放问题 7（自主计算换来的是*机械性*退出，而非*经济性*退出）
  在此完全适用；这一问题已被明确指出，但尚未解决。
- **运营方注册**（前端作为运营方）已在测试中得到验证，但尚未在生产环境中完成端到端
  流程，等待双因素邮件传输机制就绪。
- **尚无正式运行的试点节点。** 首个试点面向紧邻 NTARI 总部的一个高密度住宅社区。
- **成员经济被有意放在别处**——信用、契约和记录位于上一层的前端（Cloudy）中。

---

## 文档

| 文档 | 说明 |
|---|---|
| [`CONFORMANCE.md`](CONFORMANCE.md) | 以 JFA 术语表述的角色声明；不变量与机制的绑定关系；已明确命名的临时替代方案（stand-ins） |
| [`CLAUDE.md`](CLAUDE.md) | 工程背景、约定与工作历史 |
| `docs/` | 架构、运维手册及按会话记录的文档 |

共享的协调协议在 [`sohocloud-protocol`](https://github.com/NTARI-RAND/sohocloud-protocol)
仓库中定义（`SPEC.md`）。

---

## 许可证

AGPL-3.0-or-later——见 [`LICENSE.txt`](LICENSE.txt)。AGPL 使整个基底始终保持为永久的
公共资源（commons）：你可以阅读它、重新实现它、分叉（fork）它，也可以随时离开。

*Network Theory Applied Research Institute, Inc. — 501(c)(3) — EIN 92-3047136 — info@ntari.org*
