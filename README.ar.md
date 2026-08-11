> **ترجمة مجتمعية (مسودة)** — سياسة NTARI رقم P2-002، البث العالمي المتعدد اللغات. المصدر: README.md (الأصل الإنجليزي، لقطة بتاريخ 2026-07-29). مسودة مجتمعية بمساعدة الترجمة الآلية، في انتظار مراجعة المشرف الإقليمي وفقًا للبند §3.1 من P2-002. تبقى المواصفات التقنية الأساسية بالإنجليزية وفقًا للبند §2.2.
>
> تصحيحات الترجمة مساهمات نرحّب بها ونقدّرها كسائر المساهمات، فإذا لاحظت خطأً
> في هذه الترجمة فيمكنك إصلاحه بنفسك عبر إنشاء fork للمستودع وفتح pull
> request: https://github.com/NTARI-RAND/SoHoLINK

# SoHoLINK

[![إصدار Go](https://img.shields.io/badge/Go-1.24%2B-blue.svg)](https://golang.org)
[![الترخيص](https://img.shields.io/badge/License-AGPL%203.0-blue.svg)](LICENSE.txt)
[![CI](https://github.com/NTARI-RAND/SoHoLINK/actions/workflows/ci.yml/badge.svg)](https://github.com/NTARI-RAND/SoHoLINK/actions/workflows/ci.yml)

**المنسّق المرجعي لركيزة الحوسبة SoHoLINK / Cloudy.** يحوّل SoHoLINK القدرة الخاملة
في الحوسبة والتخزين والطباعة لدى الأجهزة التي يملكها المشاركون إلى سوق منسَّقة:
يساهم الأعضاء بعتادهم عبر واجهة أمامية ويكسبون مدفوعات حقيقية بالعملة النظامية (fiat)؛
ويشتري أعضاء آخرون قدرات الحوسبة والتخزين والطباعة وشبكة توصيل المحتوى (CDN) عند الطلب.

> **الدور في سطر واحد:** SoHoLINK هو **المنسّق** — يُنظّم توزيع العمل
> على العُقد ويُسوّي حساباته. وهو **ليس** اقتصاد الأعضاء، وهو جزء
> **قابل للاستبدال**. راجع [`CONFORMANCE.md`](CONFORMANCE.md) للاطلاع على إعلان الدور
> الكامل بمصطلحات المعمارية نفسها.

---

## ما هو SoHoLINK

يؤدّي SoHoLINK **التنسيق التشغيلي على جانب العُقدة** لركيزة قائمة على معمارية
JFA (Janus-Facing Architecture):

- **التعرّف على العُقد** — تُعلن العُقد عن العمل الذي ستؤديه عبر قوائم قدرات موقَّعة.
- **المطابقة والجدولة** — توزيع أحمال العمل على العُقد (مع مراعاة الموقع، ومراعاة الخمول، واحترام الانسحاب).
- **دورة حياة التوظيف** — العرض → القبول/الرفض → التنفيذ → الإبلاغ → التسوية، مع نافذة زمنية للنزاعات.
- **إعلانات الرسوم** — لا توجد الرسوم إلا كرسائل موقَّعة من المنسّق، مفهومة وقابلة للطعن.
- **التسوية بالعملة النظامية** — صرف المدفوعات للمساهمين عبر Stripe Connect.
- **اتحاد الواجهات الأمامية** — هدف تصميمي (راجع *الحالة* أدناه)، ولم يُبنَ بعد.

المنسّق هو **دور قابل للتوصيل والاستبدال.** يمكن لأي تنفيذ مطابق أن يحلّ محلّه،
ويمكن لأي واجهة أمامية أن تُشغّل منسّقها الخاص أو أن تُطابق مباشرةً مع العُقد.
لا شيء في الشبكة مُلزَم بالمرور عبر هذا المنسّق بعينه — وانعدام هذا الامتياز
هو ما يمنع أي منسّق من أن يصبح محورًا لا يمكن إزالته.

## ما *ليس* SoHoLINK

- **ليس اقتصاد الأعضاء في JFA.** لا رصيد يُصدره الأعضاء، ولا ميثاق سمعة،
  ولا سجل مختوم بالحوار، ولا فصل في النزاعات موجَّه للأعضاء. كل ذلك يقيم في **الواجهة
  الأمامية** (Cloudy)، بحكم القاعدة المعمارية.
- **لا رموز (tokens) ولا محافظ (wallets).** التسوية بالعملة النظامية الخالصة عبر Stripe Connect،
  وتبقى مفصولة فصلًا صارمًا عن رصيد الأعضاء في أي واجهة أمامية.
- **لا أشخاص على مسار البيانات (on the wire).** الأطراف المقابلة لـ SoHoLINK هي الواجهات الأمامية والعُقد؛
  والهوية الوحيدة التي يتعامل معها هي هوية حِمل العمل (`NodeID` مع ارتباط SPIFFE).
  أما هوية الأعضاء ورصيدهم وسمعتهم ومعلوماتهم الشخصية (PII) فهي شؤون تخصّ الواجهة الأمامية.

> **أسطح انتقالية.** إن وكيل العُقدة (`internal/agent`, `cmd/agent`)، وبوابة
> الأعضاء (`internal/portal`, `cmd/portal`, `web/`)، وجدول `participants` هي
> قدرات تملكها الواجهة الأمامية لكنها مُستضافة حاليًا في هذا المستودع من حقبة الدور المزدوج
> السابقة لـ SoHoLINK. تُصان عاملةً وتُوسَم بأنها انتقالية، في انتظار ترحيلها إلى
> الواجهة الأمامية. لا تعتبرها دور المنسّق على المدى الطويل.

---

## موضعه في الحزمة

```
Front end (Cloudy)  ──┐
                      ├──►  sohocloud-protocol  (the dependency leaf: coordination only)
Coordinator (this) ──┘
```

- يَستهلك SoHoLINK **[`sohocloud-protocol`](https://github.com/NTARI-RAND/sohocloud-protocol)**
  عند وسم إصدار منشور — وهي وحدة التنسيق ذات "الخصر النحيل" التي يتشاركها مع الواجهة الأمامية.
- ويُقدّم **مسار `/v0` الخاص بالبروتوكول** إلى جانب مسارات HTTP الحالية الخاصة به، فتتحدّث
  الواجهة الأمامية المطابقة إليه عبر البروتوكول القياسي بينما تبقى المسارات الانتقالية
  عاملة.
- ولا يعتمد على **أي** واجهة أمامية ولا على **أي** شيفرة تخصّ اقتصاد الأعضاء. فاقتصاد الأعضاء يقيم
  فوق هذه الطبقة، ولا يجوز أبدًا أن يهاجر نزولًا إليها.

---

## ثوابت JFA التي يحملها هذا المستودع

| الثابت | كيف يُستوفى هنا |
|---|---|
| لا يظهر الأشخاص أبدًا على مسار البيانات | الأطراف المقابلة هي الواجهات الأمامية والعُقد؛ والهوية على مسار البيانات هي `NodeID` من نوع SPIFFE |
| هوية مشارك واحدة | جدول `participants` واحد — لا فصل بين المزوّد والمستهلك في المخطط ولا في الشيفرة |
| وضوح الرسوم | لا توجد الرسوم إلا كرسائل `FeeDeclaration` موقَّعة من المنسّق |
| العملة النظامية تبقى نظامية؛ والرصيد يبقى في موضعه | التسوية بـ Stripe Connect الخالص؛ مفصولة فصلًا صارمًا عن رصيد الأعضاء |
| لا عدم تكافؤ في المعلومات | التسعير والقياس والأرباح مرئية للمشاركين |
| فصل سطح الحَوْكمة | تعمل الإدارة والحَوْكمة على واجهة منفصلة محلية فقط — لا على الموقع العام، ولا خلف أعلام أدوار |
| المنشأ الداخل = الخارج | AGPL-3.0؛ توقيع DCO؛ بلا CLA |

---

## الحزمة التقنية

| الطبقة | التقنية |
|---|---|
| اللغة | Go 1.24+ |
| الويب | HTML مُصيَّر على الخادم عبر `html/template` في Go — بلا إطار JS وبلا خطوة بناء (يجب أن يعمل على هاتف Android من طراز 2019 على شبكة 3G وفي متصفحات التلفزيونات الذكية) |
| قاعدة البيانات | PostgreSQL 16 + TimescaleDB، و`pgx/v5` (بلا ORM) |
| تخزين الكائنات | MinIO (متوافق مع S3) |
| الهوية | SPIFFE/SPIRE — mTLS، وشهادات SVID من نوع X.509 قصيرة العمر |
| المدفوعات | Stripe Connect (بالعملة النظامية فقط) |
| النشر | Docker Compose خلف نفق Cloudflare |
| CI | GitHub Actions |

---

## هيكل المستودع

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

## البناء

المتطلبات المسبقة: Go 1.24+، ونسخة عاملة من PostgreSQL 16 + TimescaleDB، وDocker (لتشغيل
الحزمة الكاملة).

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

تُطبَّق ترحيلات قاعدة البيانات عند بدء تشغيل الخدمة (golang-migrate، وهي عملية خاملة التكرار).
أما حزمة الإنتاج الكاملة — Postgres وSPIRE والمنسّق والبوابة وبوابة الدخول (ingress) — فتعمل عبر
Docker Compose تحت `deploy/`.

---

## الحالة (بصراحة)

- **مبنيّ وقيد التشغيل:** التعرّف على العُقد، والمطابقة والجدولة، ودورة حياة التوظيف،
  والتسوية بالعملة النظامية (Stripe Connect)، وإعلانات الرسوم الموقَّعة من المنسّق، ومسار التنسيق
  `/v0`، والمصادقة بالمُشغّل أو بـ SPIFFE، وهوية أحمال العمل عبر SPIFFE — كلها تعمل
  في الإنتاج على **[soholink.org](https://soholink.org)** (نطاق الثقة
  `spiffe://soholink.org`).
- **نشرٌ بمنسّق واحد.** أما الاتحاد بين المنسّقين ونقاط التحقّق المشهودة فهي
  **هدف تصميمي لم يُبنَ** — والمشكلة المفتوحة رقم 7 في المعمارية
  (الحوسبة السيادية تشتري خروجًا *ميكانيكيًا* لا *اقتصاديًا*) تنطبق بالكامل، وهي مُسمّاة
  لا محلولة.
- **تسجيل المُشغّل** (الواجهة الأمامية بصفة مُشغّل) مُتحقَّق منه في الاختبارات لكنه لم
  يكتمل من طرف إلى طرف في الإنتاج، في انتظار ناقل البريد للمصادقة الثنائية.
- **لا توجد عُقد تجريبية حيّة بعد.** يستهدف البرنامج التجريبي الأول مجتمعًا سكنيًا كثيف
  السكان مجاورًا لمقر NTARI.
- **اقتصاد الأعضاء موجود في مكان آخر بشكل متعمَّد** — فالرصيد والميثاق والسجل تقيم في
  الواجهة الأمامية (Cloudy)، طبقةً واحدة أعلى.

---

## التوثيق

| المستند | الوصف |
|---|---|
| [`CONFORMANCE.md`](CONFORMANCE.md) | إعلان الدور بمصطلحات JFA؛ وربط الثوابت بالآليات؛ والبدائل المؤقتة المُسمّاة |
| [`CLAUDE.md`](CLAUDE.md) | السياق الهندسي والاصطلاحات وسجل العمل |
| `docs/` | المعمارية، وأدلة التشغيل، وسجلات كل جلسة |

بروتوكول التنسيق المشترك موصَّف في مستودع
[`sohocloud-protocol`](https://github.com/NTARI-RAND/sohocloud-protocol)
(في `SPEC.md`).

---

## الترخيص

AGPL-3.0-or-later — راجع [`LICENSE.txt`](LICENSE.txt). يحفظ ترخيص AGPL الركيزة بأكملها
كمشاع دائم: اقرأها، وأعِد تنفيذها، وانسخها (fork)، وارحل.

*Network Theory Applied Research Institute, Inc. — 501(c)(3) — EIN 92-3047136 — info@ntari.org*
