> सामुदायिक अनुवाद (मसौदा) — NTARI नीति P2-002, Global Multilingual Broadcast। स्रोत: README.md (अंग्रेज़ी मूल, स्नैपशॉट 2026-07-29)। P2-002 §3.1 के अनुसार क्षेत्रीय मेंटेनर की समीक्षा तक लंबित, मशीन-सहायता से तैयार सामुदायिक मसौदा। §2.2 के अनुसार मूल तकनीकी विनिर्देश अंग्रेज़ी में ही रहते हैं।
>
> अनुवाद में कोई त्रुटि दिखे तो कृपया उसे स्वयं सुधारने में मदद करें:
> रिपॉज़िटरी को फ़ॉर्क (fork) करें और अपना सुधार पुल रिक्वेस्ट (pull request)
> के रूप में भेजें — https://github.com/NTARI-RAND/SoHoLINK। अनुवाद-सुधार
> हमारे लिए किसी भी अन्य योगदान जितने ही मूल्यवान हैं, और उनका सदा स्वागत है।

# SoHoLINK

[![Go संस्करण](https://img.shields.io/badge/Go-1.24%2B-blue.svg)](https://golang.org)
[![लाइसेंस](https://img.shields.io/badge/License-AGPL%203.0-blue.svg)](LICENSE.txt)
[![CI](https://github.com/NTARI-RAND/SoHoLINK/actions/workflows/ci.yml/badge.svg)](https://github.com/NTARI-RAND/SoHoLINK/actions/workflows/ci.yml)

**SoHoLINK / Cloudy कंप्यूट सब्सट्रेट का संदर्भ (reference) कोऑर्डिनेटर।** SoHoLINK
प्रतिभागियों के स्वामित्व वाली मशीनों की निष्क्रिय (idle) कंप्यूट, स्टोरेज और प्रिंट क्षमता को
एक समन्वित मार्केटप्लेस में बदल देता है: सदस्य एक फ्रंट एंड के माध्यम से हार्डवेयर का योगदान
करते हैं और वास्तविक (फ़िएट) भुगतान कमाते हैं; अन्य सदस्य माँग पर कंप्यूट, स्टोरेज, प्रिंट और
CDN क्षमता खरीदते हैं।

> **भूमिका, एक पंक्ति में:** SoHoLINK **कोऑर्डिनेटर** है — यह नोड्स के बीच काम का
> ऑर्केस्ट्रेशन करता है और उसका निपटान (settle) करता है। यह सदस्य अर्थव्यवस्था **नहीं** है,
> और यह एक **प्रतिस्थापन-योग्य** हिस्सा है। आर्किटेक्चर की अपनी शब्दावली में पूर्ण
> भूमिका-घोषणा के लिए [`CONFORMANCE.md`](CONFORMANCE.md) देखें।

---

## SoHoLINK क्या है

SoHoLINK एक Janus-Facing Architecture (JFA) सब्सट्रेट के लिए **नोड-पक्षीय ऑर्केस्ट्रेशन**
करता है:

- **नोड पहचान (recognition)** — नोड्स हस्ताक्षरित क्षमता-सूचियों (signed capability listings) के माध्यम से घोषित करते हैं कि वे कौन-सा काम करेंगे।
- **मैचिंग और शेड्यूलिंग** — वर्कलोड को नोड्स पर रखना (locality-aware, idle-aware, opt-out का सम्मान करने वाला)।
- **रोज़गार जीवनचक्र** — प्रस्ताव → स्वीकार/अस्वीकार → निष्पादन → रिपोर्ट → निपटान, एक विवाद-विंडो के साथ।
- **शुल्क घोषणाएँ** — शुल्क केवल कोऑर्डिनेटर-हस्ताक्षरित, सुपाठ्य, चुनौती-योग्य संदेशों के रूप में ही मौजूद होते हैं।
- **फ़िएट निपटान** — Stripe Connect के माध्यम से योगदानकर्ताओं को भुगतान।
- **फ्रंट एंड्स का फ़ेडरेशन** — एक डिज़ाइन लक्ष्य (नीचे *स्थिति* देखें), अभी निर्मित नहीं।

कोऑर्डिनेटर एक **प्लग करने योग्य, प्रतिस्थापन-योग्य भूमिका है।** कोई भी अनुरूप (conformant)
कार्यान्वयन इसकी जगह ले सकता है, और कोई फ्रंट एंड अपना स्वयं का कोऑर्डिनेटर चला सकता है
या सीधे नोड्स से मैच कर सकता है। नेटवर्क में किसी भी चीज़ के लिए इसी विशेष कोऑर्डिनेटर से
होकर गुज़रना अनिवार्य नहीं है — यही गैर-विशेषाधिकार किसी कोऑर्डिनेटर को एक अहटाए-न-जा-सकने
वाला केंद्र (hub) बनने से रोकता है।

## SoHoLINK क्या *नहीं* है

- **JFA सदस्य अर्थव्यवस्था नहीं।** कोई सदस्य-जारी क्रेडिट नहीं, कोई प्रतिष्ठा अनुबंध
  (reputation covenant) नहीं, कोई डायलॉग-सील्ड रिकॉर्ड नहीं, कोई सदस्य-सम्मुख विवाद-निर्णय
  नहीं। ये सब, आर्किटेक्चर के नियम से, **फ्रंट एंड** (Cloudy) में रहते हैं।
- **कोई टोकन नहीं, कोई वॉलेट नहीं।** निपटान Stripe Connect के माध्यम से विशुद्ध फ़िएट है,
  जिसे किसी भी फ्रंट एंड के सदस्य-क्रेडिट से सख़्ती से अलग रखा जाता है।
- **वायर पर कोई व्यक्ति नहीं।** SoHoLINK के प्रतिपक्ष (counterparties) फ्रंट एंड और नोड्स
  हैं; यह जिस एकमात्र पहचान को संभालता है वह वर्कलोड-पहचान है (SPIFFE बाइंडिंग वाला एक
  `NodeID`)। सदस्य-पहचान, क्रेडिट, प्रतिष्ठा और PII फ्रंट-एंड के सरोकार हैं।

> **संक्रमणकालीन सतहें।** नोड एजेंट (`internal/agent`, `cmd/agent`), सदस्य पोर्टल
> (`internal/portal`, `cmd/portal`, `web/`), और `participants` तालिका फ्रंट-एंड के
> स्वामित्व वाली क्षमताएँ हैं, जो SoHoLINK के पहले के दोहरी-भूमिका दौर से फ़िलहाल इसी
> रिपॉज़िटरी में होस्ट की जा रही हैं। इन्हें चालू हालत में रखा गया है और संक्रमणकालीन के रूप
> में लेबल किया गया है; फ्रंट एंड में माइग्रेशन लंबित है। इन्हें कोऑर्डिनेटर की दीर्घकालिक
> भूमिका न मानें।

---

## स्टैक में यह कहाँ बैठता है

```
Front end (Cloudy)  ──┐
                      ├──►  sohocloud-protocol  (the dependency leaf: coordination only)
Coordinator (this) ──┘
```

- SoHoLINK एक प्रकाशित संस्करण-टैग पर **[`sohocloud-protocol`](https://github.com/NTARI-RAND/sohocloud-protocol)
  का उपभोग करता है** — वह thin-waist समन्वय मॉड्यूल जिसे यह फ्रंट एंड के साथ साझा करता है।
- यह अपने मौजूदा HTTP रूट्स के साथ-साथ **प्रोटोकॉल का `/v0` वायर सर्व करता है**, ताकि कोई
  अनुरूप फ्रंट एंड मानक प्रोटोकॉल पर इससे संवाद कर सके और संक्रमणकालीन रूट्स भी काम
  करती रहें।
- यह **किसी भी** फ्रंट एंड और **किसी भी** सदस्य-अर्थव्यवस्था कोड पर निर्भर नहीं है। सदस्य
  अर्थव्यवस्था इस परत के ऊपर रहती है और उसे कभी भी नीचे इस परत में नहीं उतरना चाहिए।

---

## JFA इनवैरिएंट जिनका यह रिपॉज़िटरी पालन करती है

| इनवैरिएंट | यहाँ इसे कैसे पूरा किया गया है |
|---|---|
| व्यक्ति कभी वायर पर नहीं आते | प्रतिपक्ष फ्रंट एंड और नोड्स हैं; वायर-पहचान एक SPIFFE `NodeID` है |
| एकल प्रतिभागी-पहचान | एक ही `participants` तालिका — स्कीमा या कोड में प्रदाता/उपभोक्ता का कोई विभाजन नहीं |
| शुल्क की सुपाठ्यता | शुल्क केवल कोऑर्डिनेटर-हस्ताक्षरित `FeeDeclaration` संदेशों के रूप में मौजूद हैं |
| फ़िएट फ़िएट रहता है; क्रेडिट अपने घर में रहता है | निपटान विशुद्ध Stripe Connect है; सदस्य-क्रेडिट से सख़्ती से अलग |
| कोई सूचना-विषमता नहीं | मूल्य-निर्धारण, मीटरिंग और कमाई प्रतिभागियों को दिखाई देती है |
| गवर्नेंस सतह पृथक है | एडमिन/गवर्नेंस एक अलग, केवल-लोकल इंटरफ़ेस पर चलता है — कभी सार्वजनिक साइट पर नहीं, कभी role flags के पीछे नहीं |
| प्रोवेनेंस इनबाउंड = आउटबाउंड | AGPL-3.0; DCO साइन-ऑफ़; कोई CLA नहीं |

---

## टेक्नोलॉजी स्टैक

| परत | टेक्नोलॉजी |
|---|---|
| भाषा | Go 1.24+ |
| वेब | Go `html/template` के माध्यम से सर्वर-रेंडर्ड HTML — कोई JS फ्रेमवर्क नहीं, कोई बिल्ड-स्टेप नहीं (3G पर 2019 के Android फ़ोन और Smart TV ब्राउज़रों में काम करना अनिवार्य है) |
| डेटाबेस | PostgreSQL 16 + TimescaleDB, `pgx/v5` (कोई ORM नहीं) |
| ऑब्जेक्ट स्टोरेज | MinIO (S3-संगत) |
| पहचान (Identity) | SPIFFE/SPIRE — mTLS, अल्पजीवी X.509 SVIDs |
| भुगतान | Stripe Connect (केवल फ़िएट) |
| डिप्लॉय | Cloudflare टनल के पीछे Docker Compose |
| CI | GitHub Actions |

---

## रिपॉज़िटरी संरचना

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

## बिल्ड

पूर्वापेक्षाएँ: Go 1.24+, एक PostgreSQL 16 + TimescaleDB इंस्टेंस, और Docker (पूरे स्टैक
के लिए)।

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

डेटाबेस माइग्रेशन सेवा के स्टार्टअप पर लागू किए जाते हैं (golang-migrate, idempotent)।
पूरा प्रोडक्शन स्टैक — Postgres, SPIRE, orchestrator, portal, ingress — `deploy/` के
अंतर्गत Docker Compose के माध्यम से चलता है।

---

## स्थिति (ईमानदार आकलन)

- **निर्मित और चालू:** नोड पहचान, मैचिंग/शेड्यूलिंग, रोज़गार जीवनचक्र, फ़िएट निपटान
  (Stripe Connect), कोऑर्डिनेटर-हस्ताक्षरित शुल्क-घोषणाएँ, `/v0` समन्वय-वायर,
  ऑपरेटर-या-SPIFFE प्रमाणीकरण, और SPIFFE वर्कलोड-पहचान — ये सब
  **[soholink.org](https://soholink.org)** पर प्रोडक्शन में लाइव हैं (trust domain
  `spiffe://soholink.org`)।
- **एकल-कोऑर्डिनेटर डिप्लॉयमेंट।** क्रॉस-कोऑर्डिनेटर फ़ेडरेशन और साक्षी-सत्यापित
  चेकपॉइंट (witnessed checkpoints) एक **डिज़ाइन लक्ष्य हैं, बने नहीं हैं** — आर्किटेक्चर
  की खुली समस्या 7 (सॉवरेन कंप्यूट *यांत्रिक* निकास खरीदता है, *आर्थिक* नहीं) पूरी तरह
  लागू होती है और उसे नाम दिया गया है, हल नहीं किया गया।
- **ऑपरेटर नामांकन** (front-end-as-operator) परीक्षणों में सत्यापित है लेकिन प्रोडक्शन में
  एंड-टू-एंड पूरा नहीं हुआ है; दो-कारक (two-factor) मेल ट्रांसपोर्ट लंबित है।
- **अभी कोई लाइव पायलट नोड नहीं।** पहला पायलट NTARI के मुख्यालय से सटे एक घने
  आवासीय समुदाय को लक्षित करता है।
- **सदस्य अर्थव्यवस्था जान-बूझकर कहीं और है** — क्रेडिट, अनुबंध (covenant) और रिकॉर्ड
  एक परत ऊपर, फ्रंट एंड (Cloudy) में रहते हैं।

---

## दस्तावेज़ीकरण

| दस्तावेज़ | विवरण |
|---|---|
| [`CONFORMANCE.md`](CONFORMANCE.md) | JFA शब्दावली में भूमिका-घोषणा; इनवैरिएंट-से-तंत्र बाइंडिंग; नामित स्टैंड-इन |
| [`CLAUDE.md`](CLAUDE.md) | इंजीनियरिंग संदर्भ, परिपाटियाँ, और कार्य-इतिहास |
| `docs/` | आर्किटेक्चर, ऑपरेशन रनबुक, और प्रति-सत्र रिकॉर्ड |

साझा समन्वय प्रोटोकॉल [`sohocloud-protocol`](https://github.com/NTARI-RAND/sohocloud-protocol)
रिपॉज़िटरी (`SPEC.md`) में विनिर्दिष्ट है।

---

## लाइसेंस

AGPL-3.0-or-later — देखें [`LICENSE.txt`](LICENSE.txt)। AGPL पूरे सब्सट्रेट को एक स्थायी
कॉमन्स बनाए रखता है: इसे पढ़ें, फिर से कार्यान्वित करें, फ़ोर्क करें, छोड़ कर जाएँ।

*Network Theory Applied Research Institute, Inc. — 501(c)(3) — EIN 92-3047136 — info@ntari.org*
