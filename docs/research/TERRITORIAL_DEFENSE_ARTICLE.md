# Municipal Coordination at Network Speeds: How Distributed Compute Infrastructure Rebuilds Democratic Constraint on Executive Authority

**NTARI Research Series, Part III: The Material Culture of Democratic Deliberation**

**Authors:** Network Theory Applied Research Institute
**Date:** 2026-03-10
**Classification:** Policy Analysis

---

## Abstract

Parts I and II of this analysis established that the democratic information velocity crisis—the structural mismatch between executive actors achieving certainty at network speeds and democratic institutions deliberating at postal timescales—has progressively eroded checking capacity on concentrated power across all governance domains, most acutely in war powers. This third paper examines a structural solution: how federated cooperative computing infrastructure can rebuild deliberative capacity and democratic constraint by enabling continuous asynchronous coordination at the municipal scale. We analyze SoHoLINK, a distributed peer-to-peer compute marketplace, as a case study in how material infrastructure can embed constitutional constraint directly into the technological substrate of governance. We argue that the emergence of municipal-scale cooperative networks creates the conditions for a new form of democratic institution: the "network-speed municipality" that can coordinate collective action, aggregate distributed knowledge, and impose deliberative friction on centralized power—all at speeds compatible with the decision timescales of modern governance. The implication is that rebuilding democratic constraint does not require faster deliberation (impossible) or slower executive action (undesirable), but rather material infrastructure that makes deliberation operationally necessary before action becomes irreversible.

---

## 1. The Municipal Scale as the Locus of Democratic Reconstruction

### 1.1 Why Municipal Scale Matters

Part II demonstrated that the constitutional checking mechanisms on executive war power were eroded not primarily through formal legal revision but through the systematic destruction of material conditions that once made democratic deliberation operationally necessary. The telegraph, radio, satellite surveillance, precision weapons, and drone technology progressively compressed the time available for deliberation while expanding the scope of consequential action within the executive domain.

The analysis revealed a critical insight for governance reconstruction: **reversing this erosion does not require returning to eighteenth-century communications technology**. It requires building new material infrastructure at scales where deliberation can be operationally embedded as a structural requirement for action.

The municipal scale is the strategic locus for this reconstruction because it is the largest geographic unit at which:

1. **Distributed knowledge aggregation is operationally tractable** — a city of 500,000 residents can maintain meaningful mutual knowledge and accountability in ways that nations of 300+ million cannot.

2. **Continuous asynchronous coordination is feasible** — municipal governance operates on timescales measured in weeks and months, compatible with network-mediated deliberation, unlike national security decisions measured in hours.

3. **Exit and voice both remain available** — residents can leave municipalities or organize within them in ways that constrain executive overreach, whereas exit from national states is negligible and voice is attenuated by scale.

4. **Material infrastructure can be collectively governed** — municipalities can build, operate, and maintain infrastructure within their boundaries in ways that transcend the jurisdictional reach of centralized national systems.

### 1.2 The Historical Moment: From Postal States to Network States

The NTARI analysis in Parts I and II implicitly defined the contemporary nation-state as a "postal state"—a governance structure designed for and dependent on communications timescales measured in days and weeks. The deliberative institutions of the postal state—legislatures with formal procedures, committee systems, floor debates, recorded votes—are inherently slow because they were designed to be slow. The friction they impose was a feature, not a bug.

But postal-state institutions are now hopelessly misaligned with executive capability timescales. A legislature designed to deliberate at postal speed cannot check an executive operating at network speed. This mismatch is not a temporary coordination problem; it is a structural erosion of democratic capacity.

The emergence of network-speed municipal infrastructure creates the possibility of a fundamentally different form of democratic governance: the **network-state municipality**, which operates deliberation at speeds compatible with modern action timescales while preserving—and indeed amplifying—the checking capacity of distributed knowledge and asynchronous consensus-building.

This is not a return to the postal state. It is the construction of a new institutional form entirely.

---

## 2. SoHoLINK as Distributed Democratic Infrastructure

### 2.1 The Architecture of Decentralized Coordination

SoHoLINK is a federated peer-to-peer compute marketplace designed for SOHO (Small Office/Home Office) hardware. Its architectural features, understood through the lens of democratic reconstruction, represent a coherent system for embedding deliberative constraint into technological infrastructure.

**Core architectural elements:**

1. **P2P Mesh Discovery** — Nodes announce capabilities via signed multicast announcements verified through Ed25519 cryptography. This creates a self-organizing network with no central coordinator, enabling continuous node discovery and capability aggregation without requiring centralized authority to maintain network state.

2. **FedScheduler Orchestration** — A lightweight, intentionally non-monopolistic scheduler that distributes workload placement decisions across participating nodes based on constraints, capabilities, and reputation. Unlike Kubernetes's centralized control plane, FedScheduler preserves distributed decision-making.

3. **Compliance Standards Framework** — A system of voluntarily-adopted compliance tiers (baseline, high-security, data-residency, GPU-certified) with cryptographic attestation, reputation tracking, and audit trails. Nodes can participate in the broader mesh while simultaneously coordinating around shared standards.

4. **Cooperative Governance** — Nodes are owned and operated by individuals, small businesses, and community organizations rather than centralized cloud providers. Governance remains distributed at every level.

**Why this architecture matters for democratic infrastructure:**

The defining feature of democratic institutions is not speed or efficiency; it is **distributed ownership of both information and decision authority**. Postal-state legislatures achieved this through geographic representation and committee specialization. But postal-state structures are fundamentally limited by their communications timescales.

SoHoLINK achieves distributed authority through technological means: no single actor can know the complete state of the network without aggregating reports from other nodes; no single actor can place workloads without the cooperation of other nodes; no single actor can enforce standards without distributed attestation and audit. The technology embeds the structural requirement for coordination.

### 2.2 Municipal Compute Groups as Governance Infrastructure

The Phase 2 compliance framework for SoHoLINK introduces "compute groups"—voluntary federations of nodes organized around shared standards and commitment levels. The initial proposal includes groups like "US-East-Secure" (nodes meeting high-security standards), "EU-GDPR" (nodes in EU jurisdiction with data residency commitments), and "GPU-Cloud" (nodes with certified GPU hardware).

From a governance perspective, compute groups represent a new institutional form: **the standards-based municipal federation**. A municipality could establish a compute group—let's call it "Denver-Civic"—with membership requirements that encode civic commitments:

- Network isolation (CLONE_NEWNET on Linux, Hyper-V on Windows)
- Zero-trust firewall policies
- Audit logs maintained in municipal custody
- Compliance checks performed through municipal processes
- Reputation tracking tied to local governance bodies

This is not a new idea—municipal technology cooperatives exist, municipal broadband networks exist, municipal data trusts are being established. What SoHoLINK provides is the infrastructure layer that makes such cooperatives operationally coherent and scalable.

### 2.3 Network-Speed Deliberation Through Asynchronous Consensus

Part I identified the fundamental problem of platform governance: parliamentary systems designed for asynchronous deliberation struggle to maintain coherence when deliberative cycles take days or weeks while platform state changes happen in milliseconds.

Municipal compute groups invert this problem. The deliberative cycles are measured in weeks or months (municipal council meetings, community forums, civic processes), while the computational coordination happens at network speed. The mismatch is inverted.

A municipal council can establish consensus-based standards for resource usage, data protection, and community benefit through normal deliberative processes (town halls, committee work, recorded votes). Those standards are then encoded in OPA policies that operate at network speed, enforcing constraints across the distributed compute infrastructure without requiring further deliberation for routine operations.

When circumstances change—new threats emerge, new technologies become available, new community values crystallize—the municipality can update standards through deliberative processes, and the policies are updated. But the ongoing operation of governance happens at network speed while being constrained by municipally-determined standards.

This is not "faster deliberation." It is **deliberation at appropriate timescales with enforcement at operational timescales**.

---

## 3. Democratic Constraint Through Pre-Authorized Action Thresholds

### 3.1 The Problem of Executive Escalation

Part II demonstrated that executive war power has expanded through a combination of:

1. **Epistemic monopoly** — The executive possesses information that Congress cannot fully access
2. **Temporal asymmetry** — The executive can act before Congress can deliberate
3. **Facts on the ground** — Once the executive acts, the costs of reversal are so high that congressional opposition becomes politically impractical

The result is that formal checking mechanisms (the War Powers Resolution) exist but are operationally meaningless because the structural conditions that made them operationally necessary have been eroded.

The same logic applies at every level of governance. A municipal executive can:

- Issue emergency declarations that activate enhanced powers before city council can meet
- Commit municipal resources before deliberative processes conclude
- Negotiate agreements that create path dependencies making reversal costly
- Control information flows that shape what the council deliberates about

These are not failures of the current officeholders; they are structural features of executive-centered governance.

### 3.2 Pre-Authorized Action Thresholds as Constitutional Machinery

A network-speed municipality could establish **pre-authorized action thresholds**: formally deliberated and voted upon standards that specify conditions under which executive action is permitted without further authorization, conditions requiring council notification, and conditions requiring pre-authorization or supermajority consent.

Example framework:

```
ACTION THRESHOLD FRAMEWORK (Municipal Deliberation Example)

Routine Operations (Executive discretion):
- Resource allocation within 10% of budget line items
- Standard procurement below $50k
- Personnel decisions within established policies
- Routine communications and information requests

Notification Required (48-hour council notice):
- Resource allocation exceeding 10% of budget line items
- Emergency proclamations
- New contracts exceeding $50k
- Deployment of municipal digital infrastructure for new purposes
- Changes to data access policies

Pre-Authorization Required (Council vote before action):
- Declaration of civic emergency beyond defined scope
- Creation of new governance structures or authorities
- Commitment of municipal assets for multi-year obligations
- Fundamental changes to resource allocation frameworks

Supermajority Required (2/3+ council vote):
- Changes to constitutional framework (governance structures, voting rules)
- Establishment of new enforceable standards across municipal systems
- Fundamental changes to resource sharing policies
- Authorization of unprecedented actions falling outside established thresholds
```

These thresholds are not merely advisory. They are enforced through the material infrastructure of the system.

### 3.3 Infrastructure-Enforced Constraints

The critical innovation is **encoding these thresholds into the infrastructure itself**. In a municipal compute group:

1. **Routine operations proceed without deliberation** — The municipality's standard workloads (water treatment monitoring, waste management, traffic coordination) run without requiring council approval each time.

2. **Threshold-crossing actions trigger notifications** — When an action approaches a notification threshold, the system automatically alerts council leadership and creates an audit record. The action proceeds if no objection is raised within the notification window, but it is recorded and visible.

3. **Authorization-required actions block execution** — When an action requires pre-authorization, the system cannot complete the action until the appropriate vote occurs and the decision is recorded on-chain (in an immutable ledger maintained by the municipal compute group).

4. **Supermajority requirements are cryptographically enforced** — Certain classes of changes require that a supermajority of council members (verified through cryptographic signing keys) authorize the change. The system will not execute without the required number of valid signatures.

This is not governance by algorithm. Algorithms do not make the policy decisions (humans do, through established deliberative processes). Rather, it is governance **through infrastructure that makes policy executable**.

The material culture of executive command—the Situation Room, the budget authority, the procurement power—is replaced with a material culture of distributed coordination: a municipal ledger, a distributed executor, an immutable audit trail, cryptographic authorization requirements.

---

## 4. Reframing Territorial Defense Through Municipal Coordination

### 4.1 Defense as Collective Action Coordination

The framing of "territorial defense" is typically military: the capacity of a nation to resist invasion or coercion through force. But the democratic information velocity crisis has created a different kind of vulnerability: the capacity of distributed actors to coordinate collective response to threats has been systematically eroded by the same technological asymmetries that have expanded executive war power.

A municipality facing a genuinely threatening situation—cyberattack on critical infrastructure, disinformation campaigns, economic coercion—faces the same problem as a legislative body facing presidential overreach: it needs to coordinate a response faster than its normal deliberative processes allow, but it also needs those deliberative processes to maintain legitimacy and prevent overreach.

A network-speed municipality with pre-authorized action thresholds can solve this problem structurally.

### 4.2 Defensive Coordination Within Established Standards

If a municipality's compute infrastructure is distributed across hundreds or thousands of cooperative nodes, all of them networked through the SoHoLINK mesh, the municipality possesses a resource for collective action that is fundamentally different from centralized infrastructure.

Consider a scenario: A city's water treatment monitoring systems come under coordinated cyberattack. The attack does not compromise functionality (modern industrial control systems are designed with air gaps and redundancy), but it does generate noise and confusion that could mask a second-phase attack.

In a traditional centralized infrastructure model:
- The attack is detected by IT personnel
- A decision is escalated to the executive (mayor, city manager)
- The executive authorizes a response
- The response is implemented

This is vulnerable to exactly the problems Part II identified: the response authority is concentrated, the decision is made under time pressure, the deliberative institutions are bypassed in the name of emergency.

In a network-speed municipality with pre-authorized thresholds:
- The attack is detected by distributed monitoring nodes
- The detection triggers a pre-authorized response: system isolation, logging intensification, automated notification to council
- The municipality's nodes (distributed across the city, owned by residents and businesses) receive an authenticated instruction: "increase monitoring of potential second-stage attack vectors; record all network traffic from municipal systems"
- This is executed automatically because it falls within pre-authorized thresholds
- Council is notified within 48 hours and can authorize escalation if needed
- The municipality has effectively coordinated a response across distributed infrastructure without centralizing decision authority

### 4.3 Collective Municipal Action Without Overreach

The crucial insight is that this framework prevents both:

1. **Paralysis from decentralization** — The municipality doesn't have to wait for a full council vote to respond to acute threats
2. **Overreach from centralization** — The executive can't unilaterally escalate beyond pre-authorized thresholds without council authorization

This is possible only if:

- **The infrastructure is genuinely distributed** — No single actor controls the compute capacity; it is owned by residents and organizations
- **The standards are pre-deliberated** — The thresholds and authorized actions were voted on through deliberative processes before crisis struck
- **Execution is cryptographically enforced** — The system will not execute actions outside authorized parameters regardless of who commands it

A municipality could establish standards that include:

```
AUTHORIZED COLLECTIVE ACTIONS (within pre-approved municipal standards):

Response to Critical Infrastructure Threats:
- Nodes may reduce network connectivity if authorized by supermajority vote
- Nodes may increase logging/monitoring within authorized scopes
- Nodes may isolate systems from external networks if threat is verified
- Nodes must maintain audit trail of all actions
- All actions expire after 72 hours unless extended by council vote

Information Security Events:
- Nodes may perform automated security scans if authorized standard
- Nodes may block specific external IPs/domains if threat is verified
- Nodes may coordinate distributed rate-limiting if DDoS detected
- Actions require post-incident council review within 5 business days

All Actions Subject To:
- Supermajority vote required for actions outside standard scope
- Audit trail maintained and accessible to all council members
- Public notice within 24 hours of execution
- Automatic expiration unless renewed through deliberation
```

These are not offensive capabilities. They are defensive responses within a pre-established framework, with built-in sunset clauses and deliberative checkpoints.

---

## 5. The Architecture of Network-Speed Municipal Authority

### 5.1 Governance Through Infrastructure, Not Algorithms

A critical distinction: this framework does not replace human deliberation with algorithmic decision-making. It embeds human deliberation into infrastructure.

The algorithm does not decide whether a threat exists or what response is appropriate. Humans (elected officials, security experts, community members in public meetings) make those decisions through established deliberative processes. The algorithm enforces their decisions.

The material culture of this governance system includes:

1. **The Municipal Ledger** — An immutable record (maintained through distributed consensus across municipal nodes) of all authorized actions, thresholds, votes, and policy changes. Not blockchain theatre; a genuine cryptographically-verified audit trail.

2. **The Policy Encoder** — Tools (built into the municipal compute group's compliance framework) that translate deliberatively-decided policies into executable constraints. When council votes to set a threshold, the policy encoder translates that vote into operational rules.

3. **The Distributed Executor** — The FedScheduler itself, constrained by pre-authorized thresholds, so that workloads attempting to exceed authorized parameters are simply rejected by the system.

4. **The Verification Infrastructure** — Regular public audits (conducted by municipal officials and potentially external auditors) that verify the system is operating within authorized parameters and that no unauthorized actions have occurred.

### 5.2 Scaling Municipal Coordination: From City to City-State

The key architectural insight is that a single municipality's compute infrastructure is useful, but a **federation of municipalities** using compatible governance standards and shared infrastructure creates something qualitatively different: a city-state scale governance entity.

If Denver, Boulder, and Fort Collins each maintain distributed compute groups with compatible standards, they can federate into a "Front Range Civic Network" that:

- Coordinates on shared standards without requiring centralized authority
- Aggregates compute capacity without concentrating control
- Enables cross-municipal deliberation on shared challenges
- Maintains mutual audit and verification of compliance

This is not a new national government. It is a network of municipal governments coordinating at network speed through shared infrastructure.

The implications are profound: it creates a governance scale that is neither nation nor city, but something between them—with the distributed authority structures of cities but the policy coordination capacity of states.

### 5.3 The Emergence of Network-State Municipalities

Part II argued that the centralized nation-state (the "postal state") is structurally misaligned with modern communications and weapons technologies, and that this misalignment cannot be corrected through formal legal reform alone. The alignment of formal law with material conditions has eroded too completely.

The emergence of network-speed municipalities with pre-authorized action thresholds represents the emergence of a new form of state: the **network-state municipality**. It is:

- **Genuinely distributed** — Authority is distributed across residents and their cooperative institutions, not concentrated in executive branch
- **Network-speed capable** — Can coordinate action at the timescale of modern threats and opportunities
- **Deliberatively constrained** — Every action remains subject to the constraint of pre-authorized thresholds set through genuine deliberative processes
- **Resilient to overreach** — No single actor can escalate beyond pre-authorized parameters; doing so requires cryptographic cooperation across distributed infrastructure
- **Transparent and auditable** — Every action is recorded, visible, and subject to post-hoc review and accountability

This is not a regression to premodern city-states. It is the emergence of a qualitatively new form of governance: the **internet-native state**, in which the material substrate of governance (the distributed compute infrastructure, the cryptographic verification systems, the immutable ledgers) embeds constitutional constraint directly into technological operations.

---

## 6. Implications and Open Questions

### 6.1 How This Addresses the Democratic Information Velocity Crisis

The democratic information velocity crisis identified in Part I operates through three mechanisms:

1. **Epistemic monopoly** — Central actors achieve certainty before distributed actors can aggregate knowledge
2. **Temporal asymmetry** — Central actors can execute before distributed actors can deliberate
3. **Normative erosion** — When constraints are consistently violated with impunity, they lose legitimacy

Network-speed municipalities address all three:

1. **Distributed knowledge aggregation** — Cooperative compute infrastructure maintains knowledge at the municipal level without centralizing authority; a mayor cannot withhold information from city council or the public when it is recorded on the municipal ledger.

2. **Pre-authorized thresholds** — By deliberating in advance about which actions fall into which categories, the municipality converts temporal asymmetry into a feature: routine decisions proceed fast, extraordinary decisions require deliberation but within a framework already established.

3. **Infrastructure-enforced constraints** — When constraints are embedded in technology rather than merely codified in law, they cannot be violated through reinterpretation; the system simply refuses to execute unauthorized actions.

### 6.2 Why This Matters Beyond Governance Theory

The analysis in Parts I and II demonstrated that the erosion of democratic constraint on executive power is not primarily a moral failure of individual officials but a **structural consequence of technology and material conditions**. The solution is therefore not moral exhortation (vote better officials into office) or legal reform (pass better laws), but rather **material reconstruction**: building infrastructure that makes democratic constraint operationally necessary.

SoHoLINK and similar systems represent the first comprehensive system for doing this at municipal scale. This is not to claim that SoHoLINK solves all problems or that municipal-scale governance is the final form of democratic institutions. But it represents a proof-of-concept: **distributed governance infrastructure can be built at network speed while maintaining the checking mechanisms of democratic deliberation.**

### 6.3 Remaining Technical and Political Challenges

Several critical challenges remain unresolved:

1. **Cryptographic verification at municipal scale** — How do you ensure that the cryptographic keys authorizing municipal actions are protected from compromise while remaining accessible to elected officials? Key management at scale is hard.

2. **Cross-municipal federation standards** — How do different municipalities coordinate on compatible standards and policies? What prevents federation from collapsing into a new form of centralized authority?

3. **Emergency escalation procedures** — Some situations (natural disasters, genuine military threats) may require actions that transcend pre-authorized thresholds. What procedures allow rapid escalation while preventing permanent erosion of constraints?

4. **Equity and access** — Distributed compute infrastructure requires hardware ownership and network access. How are municipalities ensuring equitable participation across socioeconomic lines?

5. **Compatibility with existing governmental structures** — How do these new network-speed municipalities coexist with and potentially integrate with existing county, state, and federal governance?

These are not merely technical problems. They are problems that require continued deliberation, experimentation, and iterative refinement—exactly the kind of asynchronous, distributed, standards-based approach that the infrastructure itself enables.

### 6.4 The Larger Question: Governance at Network Speeds

The postal state—the nation-state designed for communications and weapons technologies of the nineteenth and twentieth centuries—is becoming increasingly difficult to maintain as a functional democratic system. The erosion of executive checking capacity is not the only symptom; it manifests across all governance domains.

The question facing democracies is not whether to maintain the postal state in its current form (it is already breaking down), but what will replace it. The alternatives currently under discussion include:

1. **Authoritarian centralization** — Srinivasan's Network State, in which democratic constraints are abandoned and replaced with transparent plutocratic governance
2. **Inertial decay** — Continued slow erosion of democratic institutions with no replacement, resulting in de facto executive rule without formal constitutional change
3. **Network-speed reconstruction** — Building new material infrastructure that enables democratic governance to operate at speeds compatible with modern action timescales

This paper argues for the third approach. It is not inevitable. It requires material investment, sustained deliberation, and the construction of new institutions. But it is structurally possible in ways that were impossible before distributed computing networks became commonplace.

---

## 7. Conclusion: The Material Culture of Democratic Reconstruction

Parts I and II of this analysis demonstrated that the erosion of democratic constraint has a technological and material basis: the alignment between constitutional design and material conditions has been systematically destroyed by a century of advances in communications and weapons technology. Formal legal reform cannot restore that alignment because the material conditions have fundamentally changed.

Part III has argued that the solution is not to restore the postal state but to **build a new material substrate for democratic governance**: distributed computing infrastructure that operates at network speeds while embedding constitutional constraints into its technological operations.

SoHoLINK, understood as democratic infrastructure rather than merely as a technology for distributed computing, represents a proof-of-concept for this approach. It demonstrates that:

- **Distributed authority can be maintained at network speeds** through cryptographic verification and consensus mechanisms
- **Pre-authorized action thresholds can constrain executive power** by making escalation beyond thresholds technically impossible without authorization
- **Deliberation can remain meaningful** when it is asynchronous, distributed, and focused on setting standards rather than handling routine operations
- **Accountability can be infrastructure-enforced** through immutable ledgers and cryptographic verification

The emergence of network-speed municipalities is not guaranteed. It requires continued investment in cooperative infrastructure, deliberate political choice to build these systems, and sustained attention to ensuring they are genuinely democratic rather than merely distributed.

But the alternative—accepting that democratic governance is structurally incompatible with modern technology and retreating into either authoritarian centralization or inertial decay—is untenable. The postal state is dying. The question is what replaces it.

This paper argues that the answer lies in material infrastructure: in building the digital equivalent of the printing press, the telegraph, and the town square—infrastructure that enables distributed knowledge aggregation and collective deliberation at scales that allow democratic constraint on power to be operationally necessary, not merely legally mandated.

The network-state municipality is not the end of this process. It is the beginning of a conversation about what governance looks like when the material substrate of democratic institutions is rebuilt for the twenty-first century.

---

## References

Bacevich, A. J. (2010). *Washington rules: America's path to permanent war*. Metropolitan Books.

Cole, D. (2017). *Engines of liberty: The power of citizen activists to make constitutional law*. Basic Books.

Ely, J. H. (1993). *War and responsibility: Constitutional lessons of Vietnam and its aftermath*. Princeton University Press.

Fisher, L. (2004). *Presidential war power* (2nd ed.). University Press of Kansas.

Goldsmith, J. (2012). *Power and constraint: The accountable presidency after 9/11*. W. W. Norton.

Koh, H. H. (1990). *The national security constitution: Sharing power after the Iran-Contra affair*. Yale University Press.

Network Theory Applied Research Institute. (2025). *Addressing democratic information velocity* (Document P1-002). NTARI.

Network Theory Applied Research Institute. (2025). *The material culture of democratic deliberation, Part I: Information systems and parliamentary constraint*. NTARI Node Nexus.

Network Theory Applied Research Institute. (2025). *The material culture of democratic deliberation, Part II: Executive war power, communications technology, and the collapse of deliberative constraint*. NTARI Node Nexus.

Schlesinger, A. M., Jr. (1973). *The imperial presidency*. Houghton Mifflin.

Siddarth, D., & Weyl, E. G. (2024). We need network societies, not network states. *Collective Intelligence Project*.

Srinivasan, B. (2022). *The network state: How to start a new country*. 1729.com.

Turley, J. (2011). Madisonian tectonics: How constitutional structure and constitutional rights expand and contract together. *George Washington Law Review*, 84, 1–70.

---

**Word Count:** ~6,500
**Classification:** Policy Analysis / Governance Theory
**Prepared for:** SoHoLINK Project Documentation
