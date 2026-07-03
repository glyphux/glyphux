# Glyphux — Product Requirements Document

**Version:** 1.0.0
**Status:** Draft — Foundational
**Classification:** Open Core — Public Core (Apache-2.0) + Commercial Ecosystem
**Last Updated:** June 2026

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Product Definition](#2-product-definition)
3. [The Composition Contract — The Architectural Center](#3-the-composition-contract)
4. [Core Design Principles](#4-core-design-principles)
5. [System Architecture](#5-system-architecture)
6. [Installation and First-Run Experience](#6-installation-and-first-run-experience)
7. [The Capability System](#7-the-capability-system)
8. [The Plugin System](#8-the-plugin-system)
9. [The Theme System](#9-the-theme-system)
10. [Permissions and Trust Model](#10-permissions-and-trust-model)
11. [Data Architecture](#11-data-architecture)
12. [The Marketplace and Package System](#12-the-marketplace-and-package-system)
13. [Blocks, Presets, and Bundles — The Builder Artifacts](#13-blocks-presets-and-bundles--the-builder-artifacts)
14. [AI Integration](#14-ai-integration)
15. [Non-Functional Requirements](#15-non-functional-requirements)
16. [Implementation Phases — Vertical Slices](#16-implementation-phases)
17. [Production Readiness](#17-production-readiness)
18. [Licensing and Open-Core Strategy](#18-licensing-and-open-core-strategy)
19. [Success Metrics and Validation](#19-success-metrics-and-validation)
20. [Architectural Decision Records](#20-architectural-decision-records)
21. [Glossary](#21-glossary)
22. [Constraints and Non-Goals](#22-constraints-and-non-goals)

---

## 1. Executive Summary

Glyphux is a **composable application platform**. It lets developers, agencies, and product teams build websites, client portals, internal tools, marketplaces, SaaS dashboards, membership sites, and e-commerce applications by composing content, layout, capabilities, and themes through a single unified contract — then self-host the result anywhere, on a single Go binary, with no managed-cloud dependency.

Glyphux is not a CMS, although a CMS is one of the things you can build with it. It is the substrate beneath a CMS. The distinction matters: a CMS manages content; Glyphux manages **composition** — the typed, versioned relationship between content, layout, capabilities, and presentation that every digital product is assembled from.

### The Central Thesis

> **The platform is defined by its composition contract. Every interface — API, CLI, SDK, themes, plugins, and eventually the visual builder — is a client of that contract.**

This single principle determines the entire architecture. The composition contract is the center; everything else is a consumer of it. The headless API serves the composition. Themes render the composition. Plugins register capabilities into the composition. The CLI scaffolds the composition. The visual builder — built last — edits the composition. No interface owns composition; they all speak to it.

### What Makes Glyphux Different

| System | What it does | What it misses |
|--------|-------------|----------------|
| WordPress | Monolithic CMS + plugin ecosystem | PHP-coupled, raw-DB plugins, security blast radius, not composable |
| Strapi / Payload | Headless, code-first content modeling | Punts composition to the developer's frontend; no first-party rendering or builder |
| Webflow / Builder.io | Visual layout composition | Proprietary, cloud-locked, not self-hostable, weak data modeling |
| Shopify | Commerce platform | Single-vertical, closed, not a general application platform |
| Retool / Bubble | Application composition (low-code) | Cloud-locked, proprietary runtime, not headless-first |
| **Glyphux** | **Composition contract spanning content, layout, and application — headless-first, self-hostable, single binary, open core** | — |

### The Formal Definition

```
Glyphux is a composable application platform
built on a layered composition contract.

Composable   → content, layout, capabilities, and themes are composed,
               not coded from scratch, through one typed contract
Application  → the output is a running application, not just managed content
Platform     → third parties extend it through a stable, versioned,
               capability-scoped public API — the same API first-party
               features are built on
Composition  → the typed, versioned relationship between every node in
               the system; the single source of truth
Contract     → a stable, versioned interface; every other surface (API,
               CLI, SDK, themes, plugins, builder) is a client of it
```

### Strategic Posture

Glyphux launches **headless-first, developer-first**. The first release is a content + data modeling engine with a clean rendering contract, sold to developers who already understand headless systems. The visual builder — the hardest single piece of software in the vision — is built only after the composition contract is proven by real external usage, and ships as the best client of that contract rather than its foundation.

A first-class objective threads through the whole arc: **time-to-premium-result without surrendering ownership** — the shortest credible path from a running binary to a launched, premium, self-hosted site or app (§2.5). This is the gap incumbents leave (WordPress isn't fast/clean to stand up; Webflow isn't owned; headless tools hand you the frontend), and it elevates fast setup, the wizard, and premium starter bundles to launch-critical. A single Glyphux deployment can ultimately serve four surfaces — public site, authenticated end-user dashboard, owner management panel, and platform management (§2.6) — with the structural parts deliverable early and the deep operational workflows deferred to Application Composition (Layer 3). Glyphux is the platform such products are built *on*; it is not a vertical-application vendor (ADR-019).

The product is validated not when it can express the founder's own application designs (that proves only internal consistency), but when an external developer builds something the platform's designers did not anticipate.

---

## 2. Product Definition

### 2.1 Vision

Any developer or team should be able to compose a production-grade digital product — website, portal, internal tool, marketplace, membership site, or commerce application — from typed, reusable building blocks, and self-host it anywhere, without surrendering ownership, extensibility, or architectural correctness to a closed cloud platform.

### 2.2 Mission

Glyphux eliminates the false choice between (a) flexible-but-headless systems that abandon you at the rendering layer and (b) easy-but-closed visual platforms that lock your data and logic in a vendor cloud. It does this by making **composition** — not content, not code, not pages — the primary unit of the system, expressed as a stable contract that every interface consumes.

### 2.3 Core Value Propositions

**For developers and technical founders:**
- Type-safe content and data modeling with a clean, versioned rendering contract
- Self-host anywhere: single Go binary, embedded database option, no required external services
- Extend through a real plugin API — the same one first-party features use — with capability-scoped permissions
- No vendor lock-in, no phone-home, open-core (Apache-2.0) foundation

**For agencies and freelancers:**
- Build client products fast from composable capabilities instead of from scratch
- Ship the same platform to every client; differentiate with themes and plugins
- Hand clients a self-hostable system they own, not a subscription they rent

**For product teams:**
- One platform spanning content, commerce, membership, and marketplace as composed capabilities
- A marketplace of vetted, capability-scoped extensions
- A path from headless API to visual editing without re-platforming

### 2.4 Target Users

**Primary (V1 — headless):**
- Developers who already understand headless content systems (the Strapi / Payload / Sanity buyer)
- Technical founders building data-driven products who want ownership and self-hosting

**Secondary (post-builder):**
- Agencies and freelancers building client sites and portals
- Designers working within a theme/composition system
- Non-technical product owners using the visual builder (the WordPress / Webflow buyer)

### 2.5 Positioning Statement

> Glyphux is the composable application platform: build content-driven products visually or via code, then self-host anywhere. The CMS is one use case, not the product.

The name carries the brand; the category is carried by the tagline. Glyphux leads with the headless content/API layer for developers, then leads with the visual builder for non-technical buyers once it is genuinely good — the same product, two go-to-market motions, sequenced by readiness.

**Core objective — time-to-premium-result, without surrendering ownership.** A first-class, measurable objective (not a tagline): the shortest credible path from `glyphuxd` running to a launched, premium, self-hosted site or app. This is the gap the incumbents leave open — WordPress is flexible but not fast or clean to stand up (LAMP stack, config, plugin sprawl); Webflow is fast and polished but cloud-locked and not yours; headless tools (Strapi/Payload) are powerful but hand you the entire frontend to build. Glyphux's differentiator is *the combination*: **fast AND premium AND yours AND a real platform underneath** — no single one of which is novel, but the conjunction of which none of them offers. The speed and polish are the surface; the composition contract and self-hosting are what keep the speed from being a trap (unlike Webflow's speed, which costs ownership; unlike WordPress's flexibility, which costs the clean fast start).

The things that *deliver* this objective are therefore launch-critical, not afterthoughts: the single-binary fast setup, the graphical interactive wizard (§6), first-party **premium starter bundles/themes** (§13), and the visual builder (Phase 4). Note honestly: "quick to a premium site" is gated far more by the *quality and breadth of starter bundles/themes* than by the engine (WordPress's famous 5-minute install delivered premium sites only once its theme ecosystem matured over a decade). So first-party premium bundles are a launch-critical deliverable, and the objective is realistic only in proportion to how many good bundles exist.

**Anti-positioning (what Glyphux is NOT).** Not a WordPress clone, and not a closed site-builder. Glyphux is the platform that *also happens to be* the fastest clean path to a premium, owned site — it does not trade away the platform to be a builder, nor the fast-premium-result to be pure headless infrastructure.

### 2.6 Target Scenarios — The Multi-Surface Platform (Forward-Looking)

A single Glyphux deployment can serve **four distinct surfaces** for one product, and seeing them together shows the platform's full reach. Some are deliverable in the V1–Phase-4 arc; the deepest are deliberately deferred to Application Composition (Layer 3, Phase 5). This section records the possibility *and* the boundaries.

```
One Glyphux deployment, four surfaces:

1. Public site            visitors browse                         → V1–Phase-4 (composition + theme/builder + media)
2. End-user dashboard     visitors AUTHENTICATE and transact      → structural parts V1–Phase-3; deep logic Layer-3
                          (log in, role-specific views, pay,
                           manage account/membership)
3. Owner management panel  the business runs day-to-day ops       → capability-driven version V1–Phase-3;
                          (manage products/orders/members/             bespoke domain workflows Layer-3
                           content/users/media)
4. Platform management     the builder/freelancer runs the         → covered today (the Glyphux admin, Surface 2)
                          whole Glyphux instance
```

**What is genuinely deliverable early (V1–Phase-4).** The *structural* parts of authenticated, transactional products: authenticated users with roles (`identity`), role-scoped access and gated content (`permissions`, `membership`), checkout and payments (`commerce`), media, and a (white-labeled, §5.7) admin through which the business manages content, products, orders, members, and users. For an **e-commerce startup** or a **membership/fitness site**, this is close to a complete product early, because commerce and membership are first-party capabilities. The owner's management panel, in this arc, *is* the white-labeled Glyphux admin exposing capability-driven management — not nothing, but generic rather than bespoke.

**What is deferred to Application Composition (Layer 3, Phase 5).** The *operational logic* that turns a content-site-with-logins into a true management system: scheduling engines, enrollment/timetabling workflows, clinical record flows, inventory-triggered fulfillment — the multi-step, side-effectful, domain-specific logic of a **school management system** or **hospital management system**. These are bespoke applications; Glyphux's path to them is the gated Layer-3 layer, and a bespoke owner operations dashboard with domain workflows is a Layer-3 expansion.

**Who builds what — the load-bearing boundary.** Glyphux provides the **primitives and the platform** (identity, roles, content, commerce, membership, media, the admin, and — later — Application Composition for custom workflows). The **freelancer/builder supplies the domain-specific logic** — early via custom plugins/capabilities they write, later via Layer-3 composition. What a builder can assemble grows as Layer 3 matures. In this model the freelancer manages the entire platform on Glyphux while the business owner manages their operations through their (white-labeled) panel — a true division of roles, with the builder building *on* Glyphux's primitives.

**Anti-goal (critical).** Glyphux does **not** ship ready-made vertical applications — no first-party school management system, hospital management system, or fitness-operations product. Doing so would turn Glyphux from a platform into a vertical-application vendor maintaining domain logic across healthcare, education, retail, and fitness simultaneously — the over-unification trap at its largest scale, where a platform trying to *be* four vertical products becomes four under-resourced ones. Glyphux is the substrate these applications are built *on* (as WordPress is the substrate a hospital site is built on, not a hospital system Automattic ships). This boundary is recorded as a decision in ADR-019.

---

## 3. The Composition Contract

### 3.1 Why the Contract Is the Center

Every digital product is an assembly of relationships: this content type renders through that layout, gated by these permissions, extended by those capabilities, presented via this theme. Most platforms encode these relationships implicitly, scattered across code, database schemas, and template files. Glyphux makes the relationship itself the **explicit, typed, versioned, single source of truth** — the composition contract.

Once the contract is the center, the architecture's hardest questions answer themselves:
- *What renders content?* A theme — a client of the contract that reads composition and emits output.
- *What does a plugin do?* Registers capabilities, content types, blocks, and event handlers into the contract.
- *What does the builder edit?* The composition. It is a client of the contract, not its owner.
- *What does the API serve?* The resolved composition.
- *Why can't themes contain business logic?* Because themes render composition; they do not own it.

### 3.2 The Three Layers of Composition

The composition contract is **not one flat API**. It spans three layers of fundamentally different complexity. Conflating them is the single largest architectural risk; each layer must be independently complete and independently versioned, so that the higher, harder layers cannot pollute the simpler, foundational one.

```
Composition Contract
│
├── Layer 1 — Content Composition          [v1 — foundational, shippable alone]
│   Content types, fields, relations, localization,
│   validation, versioning, draft/publish, media
│   Comparable to: Payload, Strapi, Directus
│
├── Layer 2 — Layout Composition           [v0 — additive, builder-era]
│   Blocks, slots, regions, sections, navigation,
│   rich text, responsive composition
│   Comparable to: Webflow, Builder.io
│
└── Layer 3 — Application Composition       [not yet public — additive, far-future]
    Event chains, side effects, workflows
    ("on form submit → create lead → notify → charge → render")
    Comparable to: Retool, Bubble, Appsmith
```

**Versioning rule:** Each layer carries its own version. V1 ships Content Composition complete (`content-composition/v1`), gestures at Layout (`layout-composition/v0`, experimental), and does not encode Application Composition at all. A consumer can fully use Layer 1 while ignoring Layers 2 and 3. Layer 3's complexity (side effects, event orchestration) must never leak down into Layer 1's data model.

### 3.3 The Contract as Multi-Client

```
                    ┌─────────────────────────────┐
                    │   COMPOSITION CONTRACT      │
                    │   (typed, versioned, SSOT)  │
                    └─────────────────────────────┘
                          ▲    ▲    ▲    ▲    ▲
          ┌───────────────┘    │    │    │    └───────────────┐
          │            ┌───────┘    │    └───────┐            │
     ┌────┴────┐  ┌────┴────┐  ┌────┴────┐  ┌────┴────┐  ┌────┴────┐
     │   API   │  │   CLI   │  │   SDK   │  │ Themes  │  │ Builder │
     │ (serve) │  │(scaffold)│ │ (typed) │  │(render) │  │ (edit)  │
     └─────────┘  └─────────┘  └─────────┘  └─────────┘  └─────────┘
                                              read-only      edits
                                              view           composition
```

Every surface is a client. None is privileged. The builder is added last and holds no special access the public SDK lacks — this is the discipline that proves the contract is genuinely public and complete.

### 3.4 Contract Representation

The composition is represented as typed, serializable documents (the on-disk / on-wire form) backed by typed Go interfaces (the in-process form). It is:
- **Declarative** — describes what composes with what, not how to execute it
- **Diffable** — version-controlled; line-level changes are meaningful
- **Validatable** — every composition is checked against the contract schema before it is accepted
- **Versioned** — the contract schema has an explicit version; consumers declare the version they target

A representative (illustrative, not final) shape:

```yaml
composition:
  contract_version: content-composition/v1

  content_types:
    article:
      fields:
        title:    { type: string, required: true, localized: true }
        body:     { type: richtext }
        author:   { type: relation, to: user }
        cover:    { type: media, accept: [image] }
      versioning: { drafts: true, history: true }

  capabilities:
    - commerce        # registers product/order content types, payment events
    - membership      # registers gated-content rules, subscription state

  rendering:
    theme: corporate
    routes:
      /articles/:slug: { renders: article, layout: article_detail }
```

---

## 4. Core Design Principles

These are architectural axioms. Every decision is justified by at least one.

**Principle 1 — The composition contract is the single source of truth.**
Content schemas, routes, permissions, rendering, and extension points all derive from the composition. A change to the composition propagates to every client (API, themes, SDK, builder) through the contract. Nothing about the running product exists outside the composition that the contract can't see.

**Principle 2 — Every interface is a client of the contract.**
The API, CLI, SDK, themes, plugins, and the visual builder are all consumers. None owns composition. New surfaces are added by writing new clients, never by privileging one client with private access to internals.

**Principle 3 — Headless-first; rendering is a client, not the core.**
The platform is fully usable and sellable with no renderer at all, via the API. Themes and the builder are layers on top of a complete headless core. This is enforced by building the API and contract before any rendering surface.

**Principle 4 — Plugins interact only through versioned, capability-scoped domain APIs — never raw resources.**
No plugin gets raw database handles, raw filesystem access, or unrestricted network by default. Plugins speak to typed domain APIs (content, users, media, payments) that the platform controls and versions. Persistent plugin data lives in a scoped, platform-mediated namespace, never in raw tables. This is the anti-`$wpdb` rule and it is non-negotiable.

**Principle 5 — Themes render composition; they never mutate it.**
A theme receives a read-only view of the resolved composition and emits output. It has no persistence, no mutation API, no "helper" that secretly edits state. If a theme needs something changed, it emits an event and the platform decides. (The theme analog of Principle 4.)

**Principle 6 — First-party capabilities are built on the public extension API.**
Commerce, membership, and marketplace are first-party plugins that go through the same public capability and extension system third parties use. This dogfoods the API and guarantees it is powerful enough to be worth extending. If a first-party feature needs something the public API can't express, the API is extended publicly — never bypassed privately.

**Principle 7 — Capability composition with explicit dependency resolution.**
Capabilities declare what they require; the platform resolves the transitive dependency graph, rejects cycles at validation time, and surfaces implicit inclusions. Selecting `commerce` auto-resolves `content`, `users`, `permissions`. No capability hard-depends on another except through the declared graph.

**Principle 8 — Security and observability are first-class, capability-scoped, and adversarial-by-default.**
The plugin author is assumed untrusted (marketplace threat model). Sandboxing, capability enforcement, install-time consent, and audit logging are baked into the runtime, not bolted on. Enforcement happens at the boundary — the boundary strips a plugin of anything it didn't declare.

**Principle 9 — Self-hostable, single binary, no required cloud.**
The platform ships as one Go binary, runs on a cheap VPS, supports an embedded database for the "host anywhere" promise, and has zero phone-home. Managed/hosted offerings, if any, are a convenience layer, never a dependency.

**Principle 10 — Convention over configuration, configuration over magic.**
Sensible defaults cover the common case; every default is overridable; nothing happens implicitly that isn't declared in the composition or explicit in the SDK.

---

## 5. System Architecture

### 5.1 The Layered Model

Glyphux is organized into strict layers. No layer reaches upward; no layer bleeds into another.

```
┌──────────────────────────────────────────────────────────────┐
│  CLIENTS                                                       │
│  API · CLI · SDK · Themes · Visual Builder (last)              │
│  Each is a consumer of the composition contract.               │
├──────────────────────────────────────────────────────────────┤
│  EXTENSION LAYER                                               │
│  Plugin Runtime (WASM + RPC) · Theme Runtime · Capability      │
│  Registry · Public Extension API                               │
│  Untrusted, sandboxed, capability-scoped.                      │
├──────────────────────────────────────────────────────────────┤
│  COMPOSITION CONTRACT  (the center)                           │
│  Typed schema · Validator · Versioning · Resolver              │
│  The single source of truth.                                   │
├──────────────────────────────────────────────────────────────┤
│  DOMAIN APIS  (what plugins and clients are allowed to touch)  │
│  Content · Identity · Media · Permissions · Events · (Payments…)│
│  Typed, versioned interfaces over the kernel. Never raw.       │
├──────────────────────────────────────────────────────────────┤
│  KERNEL  (first-party only, compiled-in)                      │
│  Content engine · Router · Identity (accounts, sessions,       │
│  OAuth, MFA) · Permission engine · Media pipeline · Event bus  │
│  · Storage abstraction · Scheduler · Security primitives       │
│  (CSRF/CORS, rate limiting, secrets, input sanitization)       │
├──────────────────────────────────────────────────────────────┤
│  ADAPTERS  (infrastructure behind the kernel)                  │
│  Database (SQLite / Postgres; connection pooling + streaming)  │
│  · Object storage (FS / S3 / MinIO) · Cache · Queue            │
└──────────────────────────────────────────────────────────────┘
```

### 5.2 The Communication Law

```
Clients     ──read/write through──▶  Domain APIs (via Composition Contract)
Plugins     ──register into──────▶   Capability Registry + Domain APIs
Plugins     ──subscribe/emit─────▶   Event Bus
Themes      ──read-only view of──▶   Resolved Composition
Kernel      ──calls──────────────▶   Adapters

Plugins  ✗  never touch  →  Adapters / raw DB / raw FS directly
Themes   ✗  never mutate  →  Composition or any state
Clients  ✗  never bypass  →  the Composition Contract to reach the kernel
Adapters ✗  never know    →  Plugins or Clients exist
```

### 5.3 Repository Structure (Open Core)

```
github.com/glyphux/
├── glyphux/                    # The Apache-2.0 core binary
│   ├── cmd/
│   │   ├── glyphuxd/           # PRIMARY entrypoint: the server daemon. Boots the
│   │   │                      #   embedded server, serves the first-run web wizard,
│   │   │                      #   then the running platform. This is what every
│   │   │                      #   deployment target launches.
│   │   └── glyphux/            # SECONDARY: optional developer CLI (cobra) for
│   │                          #   scripting/automation/CI. NOT the install path.
│   ├── internal/
│   │   ├── setup/             # First-run web wizard (served by the daemon):
│   │   │                      #   admin account, DB choice, site config → writes
│   │   │                      #   the initial composition. Cross-platform, one
│   │   │                      #   codebase reused by all deployment scenarios.
│   │   ├── composition/       # Contract: schema, validator, versioning, resolver
│   │   ├── content/           # Content engine (types, fields, relations, versioning)
│   │   ├── media/             # Media pipeline (upload, transform, serve)
│   │   ├── identity/          # Accounts, sessions, credentials, OAuth/social, MFA
│   │   ├── permissions/       # Capability-based permission engine
│   │   ├── security/          # CSRF/CORS, rate limiting, secrets provider,
│   │   │                      #   input sanitization, session hardening
│   │   ├── events/            # Event bus (in-process, durable option)
│   │   ├── router/            # Request routing, composition → route resolution
│   │   ├── capability/        # Capability registry + dependency resolver
│   │   ├── plugin/            # Plugin runtime: WASM host (wazero) + gRPC broker
│   │   ├── theme/             # Theme runtime: read-only composition view + renderer
│   │   ├── storage/           # Storage abstraction (adapters behind it)
│   │   ├── db/                # Database abstraction (SQLite / Postgres);
│   │   │                      #   connection pooling + streaming/cursors (Postgres)
│   │   ├── api/               # HTTP/JSON + GraphQL transport over domain APIs
│   │   ├── adminui/           # Embedded admin/builder SPA assets (go:embed of the
│   │   │                      #   built React app from admin-ui/); served by daemon
│   │   ├── consent/           # Install-time capability consent engine
│   │   └── audit/             # Audit logging
│   ├── pkg/                   # Public SDK surface (importable by plugin authors)
│   │   ├── sdk/               # Plugin/theme author SDK (Go)
│   │   └── contract/          # Public composition contract types
│   └── api/                   # WIT definitions, protobuf, OpenAPI/GraphQL schema
│
├── admin-ui/                  # The admin shell + visual builder (ONE React app).
│                              #   React + TypeScript + Vite + Tailwind. Built to
│                              #   static assets, embedded into the binary via
│                              #   internal/adminui (go:embed). Talks to core ONLY
│                              #   through the public JS SDK — a client, not privileged.
│
├── capabilities/              # First-party capabilities (separate packages, dogfood public API)
│   ├── commerce/
│   ├── membership/
│   ├── marketplace/
│   ├── notifications/         # Email / SMS / push / in-app (depends on events + mailer)
│   ├── seo/
│   └── forms/
│
├── themes/                    # First-party reference themes
│   ├── headless/              # The "no theme" / pure-API default
│   └── starter/
│
├── sdk-js/                    # JS/TS SDK (admin-ui, JS plugins, user frontends)
├── sdk-rust/                  # Rust SDK (for WASM plugin authors)
├── desktop/                   # POST-V1: thin native wrappers (.dmg/.exe/.AppImage)
│                              #   whose only job is "place binary, launch daemon,
│                              #   open browser to the wizard". No setup logic here.
├── deploy/                    # POST-V1: one-click deploy templates (Docker image,
│                              #   Coolify/Dokploy/Railway/Render templates) that
│                              #   provision and land the user on the web wizard.
└── registry/                  # Marketplace registry service (commercial)
```

### 5.4 The Kernel Analogy

| OS Kernel Concept | Glyphux Equivalent |
|---|---|
| System calls | Domain APIs (the only sanctioned way to touch the system) |
| Kernel modules | Plugins (sandboxed, capability-scoped) |
| Process isolation | WASM sandbox / RPC process boundary |
| File system | Storage abstraction + media pipeline |
| Scheduler | Job runner / scheduled jobs capability |
| Permissions / capabilities | Capability-based permission engine |
| Init / PID 1 | The `glyphuxd` daemon — boots the platform and serves the first-run wizard |
| The shell | The `glyphux` developer CLI (optional, secondary surface) |
| Device drivers | Adapters (DB, storage, cache, queue) |
| User programs | Themes, the builder, external frontends |

### 5.5 Layer Decision Rule

```
Ask of anything being designed:

  Does it define a typed relationship in the system?    → Composition Contract
  Does it manage core domain state?                     → Kernel (first-party)
  Does it expose sanctioned access to state?            → Domain API
  Does it install a capability / extend behavior?       → Plugin (capability)
  Does it render composition to output?                 → Theme
  Does it bridge to real infrastructure?                → Adapter

  If none of the above → it does not belong in Glyphux core.
```

This rule is the governance mechanism that prevents core bloat — the failure that turned WordPress core into a swamp.

### 5.6 Frontend and UI Surfaces

"The frontend of Glyphux" is three distinct surfaces with three different answers. Conflating them is a category error; the PRD treats them separately.

**Surface 1 — The frontend of products people *build* with Glyphux: framework-agnostic, always.**
This is the entire point of headless-first. A developer pulls content through the API/SDK and renders it in whatever they choose — Next.js, Nuxt, Astro, SvelteKit, Remix, a Flutter app, plain HTML, or nothing. Glyphux does not know or care. The built-in `headless` theme emits JSON only, so the platform is fully usable with zero rendering opinion. **Glyphux never dictates the end-product's frontend stack.**

**Surface 2 — Glyphux's own admin / wizard / visual builder UI: one opinionated stack.**
This is an application *Glyphux itself ships* — the first-run wizard (§6), the admin shell (Phase 1), and the visual builder (Phase 4). It is **not** framework-agnostic and should not be: making it agnostic would mean building it more than once. It is one application in one stack:

> **React + TypeScript + Vite + Tailwind, built as a SPA, compiled to static assets, embedded in the binary via `go:embed` (`internal/adminui`), talking to the core only through the public JS SDK (`sdk-js`).**

Rationale — and the deciding factor is the **visual builder (Phase 4), which is a load-bearing pillar of the vision, not optional**:
- The admin shell and the builder must share one stack, because the builder *is* the admin shell grown up; two stacks would be needless duplication.
- The builder is a nested, multi-target, rule-based drag-and-drop canvas — not a sortable list. The mature ecosystem for exactly this problem is overwhelmingly React: **dnd-kit** (composable DnD primitives: sensors, droppables, custom collision detection, drag overlays, keyboard a11y) and, above it, **page-builder frameworks Craft.js and Puck** that supply the editor state model, nested node tree, serialization, and props panels — i.e. they solve a large fraction of "build a visual builder" rather than just "make things draggable." Both are React-only; Vue has no equivalent at that maturity. (Vue's `vuedraggable`/SortableJS is excellent for *list reordering* — and would fully suffice for the admin shell alone — but tops out at list-shaped problems and would require reimplementing dnd-kit's primitives plus the Craft.js/Puck layer in user code for the canvas.)
- `go:embed` of the built static assets preserves the single-binary, host-anywhere promise: the admin UI ships *inside* `glyphuxd`, served as static files, with **no Node runtime required in production**.

Critically, **the admin UI being React does not make Glyphux "a React platform."** The admin is just another *client of the composition contract* (Principle 2), exactly like the CLI or a theme — it holds no privileged access the public SDK lacks. The platform's API is language-neutral; the admin happens to be React the way a product's web console is built in some stack without the product being "a [that-stack] product." Surface 1 is entirely unaffected by Surface 2's choice.

**Surface 3 — Themes that render via Glyphux's *own* renderer: a bounded rendering contract.**
For users who want Glyphux to render their public site (rather than bringing their own frontend), themes consume a read-only composition view and emit output (§9). The templating technology is a defined contract — Go templates / a typed template layer in V1 — with room to add a JS/SSR rendering client later without changing the contract. Bounded and versioned, not open-ended. This is distinct from Surface 2: the theme renderer produces the *public site*; the React SPA is the *admin/builder*.

```
Surface 1 — user's product frontend   → framework-AGNOSTIC (Next/Nuxt/Astro/Flutter/…)
Surface 2 — Glyphux admin + builder     → React+TS+Vite+Tailwind, embedded via go:embed
Surface 3 — themes (optional renderer) → bounded contract (Go templates v1; JS/SSR later)
```

### 5.7 Admin UI Quality Bar and Configurability

The admin UI (Surface 2) is held to a **premium design and configurability standard**. Because "premium" and "configurable" are unfalsifiable as adjectives, this section states them as concrete, buildable commitments a team can implement and a reviewer can check.

**Design system (what "premium" means concretely):**
- A single **design-token system** (color, type scale, spacing, radius, elevation, motion) is the source of truth for all admin UI; no ad-hoc values in components. Tokens are defined once and consumed everywhere (the admin's own internal equivalent of composition discipline).
- A deliberate **typography system** (display, body, and utility/mono roles) with a defined type scale and weights — not framework defaults.
- A **component library** built on the chosen stack (the shadcn/ui + Tailwind approach is the reference baseline) so every surface — wizard, admin shell, builder — is visually and behaviorally consistent.
- **Motion** used deliberately for state transitions and micro-interactions, with reduced-motion respected.
- A **quality floor, non-negotiable:** fully responsive down to mobile, visible keyboard focus, WCAG 2.1 AA accessibility (contrast, focus order, ARIA, keyboard operability), and clear empty/error/loading states written in the interface's voice.

**Configurability (what "richly configurable" means concretely):**
- **Admin theming via tokens:** the admin UI's own appearance is themeable through the token system — at minimum light/dark and a brandable accent/logo, so agencies can white-label the admin they hand to clients. (Distinct from Surface-3 site themes; this is the *admin chrome* itself.)
- **Layout/density configurability:** users can adjust density (comfortable/compact) and persist layout preferences (panel arrangement, sidebar state).
- **Configurable surfaces:** dashboards, list views (columns, filters, sort, saved views), and the navigation/menu are user-configurable, not hard-coded — driven by data/config the same way composition is.
- **Extensible by capabilities:** plugins register admin pages, dashboard widgets, and builder panels through the public API (`RegisterAdminPage`, §8.3), so the admin surface is extended by the ecosystem, not only by core — the admin is configurable *and* extensible by the same capability model as everything else.

**Sequencing honesty:** the quality bar and token/theming foundation are established with the Phase-1 admin shell (so they are designed in from the start, not retrofitted), but the *depth* of configurability grows across phases — basic theming and density in Phase 1, rich configurable dashboards/list-views and the full builder polish in Phase 4. The bar is "premium and consistent at every phase," not "every configurability feature at once." A polished, richly-configurable admin in front of an empty platform still sells nothing (§6.6) — the quality bar applies to whatever ships, in order.

---

## 6. Installation and First-Run Experience

### 6.1 The Installer Is a Wizard, Not a Terminal Command

Glyphux's first-run experience is a **graphical, browser-based setup wizard** — not a terminal CLI flow. This is a deliberate divergence from developer-orchestration tooling (where the user lives in a terminal). Glyphux's users span developers, agencies, freelancers, and — after the builder ships — non-technical product owners. The front door must be a wizard, not a command line.

A developer CLI (`glyphux`) exists, but as a **secondary, optional** surface for scripting, automation, and CI — never as the install path. This mirrors the proven pattern: WordPress ships a web install wizard (`install.php`) with `wp-cli` as a separate developer tool; Ghost offers a one-click/hosted path alongside the `ghost` CLI for developers. The graphical wizard is primary; the CLI is for power users.

### 6.2 One Wizard, Served by the Binary — The Unifying Decision

Because Glyphux can be hosted locally *or* in the cloud, "installation" is not one experience. The unifying architectural decision that collapses every scenario into one codebase:

> **The setup wizard is a web application served by the Glyphux daemon (`glyphuxd`) itself — not a per-OS native installer.**

The wizard lives in `internal/setup` and runs as the daemon's first-run web flow. Every deployment target reuses the *same* wizard. OS-specific native installers (where they exist) become thin shells whose only job is "place the binary, launch the daemon, open the browser" — all setup intelligence lives in the cross-platform web wizard.

Philosophically, this is consistent with the rest of the platform: **the wizard is just another client of the composition contract** (Principle 2). Its job is to produce the *initial composition* — site config, admin account, database choice, first content types. It is not a special snowflake; it is the first thing that writes a composition.

### 6.3 The Three Deployment Scenarios

```
SCENARIO 1 — Local / self-hosted (laptop, on-prem, a machine the user controls)
  Distribution:   single binary, or (post-V1) a native wrapper (.dmg/.exe/.AppImage)
  First run:      daemon starts the embedded server (SQLite, single binary),
                  opens the user's browser to http://localhost:PORT
  Wizard:         site name → admin account → database (SQLite default /
                  point at Postgres) → done. No terminal required.
  Analogy:        a modernized WordPress install.php, but self-contained.

SCENARIO 2 — Cloud / server self-hosted (their VM, Docker, Coolify/Dokploy, etc.)
  Distribution:   container image or binary; (post-V1) one-click deploy templates
  First run:      headless server — no desktop window possible; the daemon serves
                  the SAME web wizard over HTTP on first boot
  Wizard:         admin hits the server's address in a browser, completes the
                  identical wizard; first-run is locked after completion
  Analogy:        Ghost/self-hosted-app first-boot setup screen.

SCENARIO 3 — Managed cloud (a future Glyphux-operated hosting offering)
  Distribution:   none — the user signs up
  First run:      infra is pre-provisioned; the user lands directly in the
                  later steps of the SAME wizard (skip DB/infra steps)
  Status:         POST-V1 commercial option, not a launch feature.
  Analogy:        WordPress.com / Shopify onboarding.
```

The wizard code is written **once** and reused across all three. Scenarios differ only in (a) how the binary arrives and (b) whether the browser is opened locally or reached over the network.

### 6.4 First-Run Security

- The first-run wizard is reachable **only until setup completes**; afterward the route is permanently disabled (no re-running install against a live instance).
- In the cloud/server scenario, first-run is protected against a race (a setup token printed to the server log or passed via env, required to complete the wizard) so an attacker cannot reach an unconfigured instance's wizard before the owner does.
- HTTPS is required for any non-localhost wizard access in production.

### 6.5 Distribution Surfaces (Sequenced)

```
V1:        single binary download; daemon serves the web wizard.
           Docker image (serves the wizard over HTTP).
POST-V1:   thin native wrappers (.dmg/.exe/.AppImage) — desktop/ in repo.
           One-click deploy templates (Coolify/Dokploy/Railway/Render) — deploy/.
POST-V1:   managed hosting (Scenario 3).
```

### 6.6 Scope Discipline

A setup wizard is satisfying, visible, demo-able work — and it is **not the wedge**. The Phase 1 headless content core is the product; the wizard is onboarding polish on top of it. A gorgeous wizard in front of an empty platform sells nothing. Therefore: the *single-wizard architecture* is decided now (it shapes the daemon and the repo structure), but the wizard is **built in Phase 1 only as far as** "create admin + choose database + boot," and grows alongside the product. Native wrappers and one-click templates are explicitly post-V1.

---

## 7. The Capability System

### 7.1 What Capabilities Are

A capability is a declared, composable feature — `content`, `users`, `media`, `permissions`, `commerce`, `membership`, `forms`. Capabilities declare **what** a composition requires, independent of **how** they are implemented. They are the unit of composition at the feature level and the unit of permission at the trust level.

Capabilities serve a dual role, and the two roles must be kept conceptually distinct (this distinction is load-bearing — see §7.3):
- **Domain surfaces** — "this plugin interacts with the content system" (normal, ubiquitous).
- **Resource permissions** — "this plugin needs file access / network / scheduled jobs" (scrutinized, risk-graded).

### 7.2 Capability Dependency Graph

Selecting a capability auto-resolves its transitive dependencies. The resolver runs at composition-validation time.

```
content         → (root capability — no dependencies)
identity        → (kernel-backed: accounts, sessions, OAuth, MFA)
permissions     → identity
media           → storage
forms           → content
seo             → content
mailer          → (no required deps; provider adapter)
notifications   → events, mailer            # email / SMS / push / in-app
membership      → identity, permissions, content, notifications
payments        → events
commerce        → content, identity, permissions, payments, events, notifications
marketplace     → commerce, identity, permissions, packaging, signing
tenancy         → identity, permissions     # PLANNED — deferred, not V1 (see §11.5)
ai              → content, events            # provider-agnostic (Claude/OpenAI/Ollama); enhancement
stock-media     → media                      # Unsplash/Pixabay/Pexels adapters; fetch-and-store; enhancement
```

Declaring `commerce` automatically resolves `content`, `identity`, `permissions`, `payments`, `events`, `notifications`. Implicit inclusions are surfaced as warnings. **Content is the root** — every higher capability branches from it, which is precisely why the content layer is the V1 wedge. `identity` is kernel-backed (it is not an optional plugin) but participates in the graph so capabilities can declare their dependence on it. `tenancy` appears in the graph as a *planned* capability so the model is forward-compatible, but it is explicitly deferred (§11.5).

### 7.3 Two-Axis Manifest: API Surface vs. Resource Permission

A plugin manifest separates the two axes explicitly, because they carry different trust models, enforcement points, and review criteria.

```yaml
name: commerce
version: 1.0.0
runtime: rpc                 # rpc | wasm — selects the loader
license: commercial
requires:
  core: ">=1.0.0"
  contract: content-composition/v1

api:                         # DOMAIN SURFACES — normal, what core functionality it speaks to
  - content: [read, write]   # capabilities are SCOPED, not boolean
  - users: [read]
  - events: [emit, subscribe]
  - payments: [charge, refund]

permissions:                 # RESOURCE GRANTS — scrutinized, risk-graded
  - admin_ui                 # may register admin pages
  - scheduled_jobs           # may register background jobs
  - network: [api.stripe.com] # outbound network, allowlisted
  # raw database / raw filesystem are NOT grantable to third parties
```

A request for the **content API** is routine. A request for raw **database access** is a red flag a reviewer must scrutinize — which is impossible if both live in one undifferentiated field. Raw DB/FS access is near-forbidden for third-party plugins (§10.4).

### 7.4 Capability Scoping

Capabilities are **scopable, not boolean**, from v1 — retrofitting scopes onto a boolean system is a breaking change across every plugin. `payments: [refund]` differs from `payments: [charge, refund]`. `content: [read]` differs from `content: [read, write, delete]`. The grant grammar starts minimal but the *shape* (a capability plus a scope set) is fixed from the first release.

### 7.5 Resolution Rules

```
1. Explicit capabilities take precedence over implicit ones.
2. Circular dependencies are rejected at composition-validation time.
3. Implicit capabilities use default implementations (overridable).
4. All resolved capabilities are recorded in the compiled composition.
5. `glyphux composition show --capabilities` prints the resolved tree.
```

---

## 8. The Plugin System

### 8.1 Three Tiers, One Contract

Plugins are routed to one of three runtime tiers by their profile. All three speak the **same** unified extension contract (events, capabilities, manifest) — only the transport and isolation differ. This is the resolution of the RPC-vs-WASM question: it was never one-or-the-other; it is *which tier does this plugin belong to*.

```
TIER A — In-process Go        [first-party / official only — NOT third party]
  For: the kernel modules themselves (content engine, router, permissions,
       media, cache, search). Compiled into the binary. Full trust.
  Why not third party: zero sandbox, full blast radius, traps ecosystem in Go.

TIER B — WASM (primary third-party tier)         [Wazero host + WIT contract]
  For: small, numerous, untrusted, latency-sensitive plugins —
       SEO rules, form validation, content transforms, custom blocks,
       pricing rules, workflow steps.
  Why: deny-by-default sandbox, language-agnostic, in-process speed,
       single-binary preserved (Wazero is pure Go, no CGO).

TIER C — RPC / out-of-process (heavy third-party tier)   [gRPC, go-plugin model]
  For: large, few, comparatively trusted, system-level plugins —
       commerce, booking, CRM, payments, analytics, AI integrations,
       membership, email marketing.
  Why: own dependencies, outbound network, long-lived state, own datastore;
       process isolation is a security feature (e.g. PCI scope for payments).
```

### 8.2 Why Not In-Tree Compilation for Third Parties

Go's `plugin` package and "recompile plugins into the binary" are rejected for third-party use: the `plugin` package is platform-restricted and toolchain-version-coupled; in-tree compilation gives zero sandboxing and forces every author into Go. First-party kernel modules are the only Tier-A code, and they are compiled in precisely because they are trusted and on the hottest paths.

### 8.3 The Unified Extension Contract

All tiers register through one conceptual interface. Transport differs (Go interface for Tier A, WIT-generated bindings for Tier B, gRPC for Tier C), the contract does not.

```go
// Conceptual shape — the public extension contract every tier implements.
type Plugin interface {
    Manifest() Manifest                 // name, version, runtime, requires, api, permissions
    Register(host HostAPI) error        // register capabilities, content types, blocks, hooks
}

// HostAPI is the ONLY surface a plugin can reach. It is capability-gated:
// methods backed by a capability the manifest didn't declare are not exposed.
type HostAPI interface {
    // Composition / content domain
    RegisterContentType(def ContentTypeDef) error
    RegisterBlock(def BlockDef) error              // Layer-2 (layout) extension point
    Content() ContentAPI                            // scoped per manifest api: content
    Users() UsersAPI                                // scoped per manifest api: users
    Media() MediaAPI

    // Eventing (the "actions and filters" equivalent)
    On(event EventName, handler Handler) error      // subscribe
    Emit(event EventName, payload any) error         // emit

    // UI / lifecycle
    RegisterAdminPage(def AdminPageDef) error        // requires permission: admin_ui
    RegisterJob(def JobDef) error                    // requires permission: scheduled_jobs

    // Scoped persistence — NEVER raw DB
    Store() ScopedKV                                 // per-plugin namespaced storage
}
```

Key properties, mirrored from the kernel's own design so first-party and third-party code are honest siblings:
- `Content()`, `Users()` etc. are **scoped** — the returned API enforces the manifest's declared scopes.
- Persistence is a **scoped key-value / namespaced table** the platform mediates — never a raw DB handle.
- Mutation is **event-driven and API-mediated**; a plugin cannot reach past the HostAPI into kernel internals.
- A capability not declared in the manifest is **not present** in the HostAPI surface the plugin receives — enforcement is at the boundary, not by convention.

### 8.4 The Event System (Actions and Filters)

A single event bus underlies all tiers. Plugins subscribe to and emit lifecycle and domain events. Subscribing to a sensitive event (e.g. `user.created`, `payment.completed`) requires the corresponding capability scope, because the subscriber sees sensitive data flow — emitting and subscribing carry different risk and are scoped separately.

```
Lifecycle:  composition.before_build   composition.after_build
            content.before_save        content.after_save
            content.published          content.unpublished
            request.before_render      request.after_render
Domain:     user.created               user.deleted
            order.placed               payment.completed   payment.refunded
            membership.started         membership.expired
Plugin:     plugin.loaded              plugin.error
```

### 8.5 Loading Order

Plugins load in topological order of their declared dependencies; circular dependencies are rejected at load time.

---

## 9. The Theme System

### 9.1 Themes Are Renderers — A Hard Law

> **Themes never mutate composition.**

This is the theme analog of the plugins-never-touch-raw-storage law, and it is enforced identically.

```
Theme Runtime
    ↓ receives
Read-only Composition View   (typed, no mutation methods exist on it)
    ↓ produces
Rendered Output (HTML / JSON / other)
```

A theme has **no** persistence, **no** mutation API, and **no** "helper" that secretly edits state. If a theme needs something changed, it emits an event and the platform decides — exactly like a plugin. A theme that wants logic is a plugin wearing a costume; the boundary refuses it.

### 9.2 Theme Capabilities

A theme:
- Declares which content types and layouts it can render
- Receives a read-only, typed view of the resolved composition for a given route
- Emits output through the rendering contract
- May ship assets (CSS, JS, fonts, images)
- May declare slots/regions it exposes (Layer-2 composition surface)

A theme may **not**:
- Write to the database, filesystem, or any state
- Call domain APIs that mutate
- Embed business logic that belongs in a capability/plugin

### 9.3 Rendering Contract

The rendering contract is the typed interface between the resolved composition and a theme. Because rendering is just another client of the composition contract (Principle 3), the platform is fully usable headlessly with the built-in `headless` "theme" that emits JSON only. Visual themes and, later, the builder are additional clients of the same contract.

For V1, the rendering target is server-side templating (Go templates / a typed template layer) plus static asset delivery. The contract is designed so that a JS/SSR rendering client can be added later without changing the contract — the headless-first discipline forces the rendering contract to stay clean.

---

## 10. Permissions and Trust Model

### 10.1 Adversarial by Default

Unlike a developer-orchestration tool whose plugin authors are trusted contributors, Glyphux's modal plugin author is an **anonymous marketplace seller**. The trust model is adversarial from the kernel up. This is the single most important way Glyphux's extension system differs from a trusted-author plugin system, and it cannot be bolted on later — it changes what the kernel is allowed to assume.

### 10.2 Three Points of Consent and Enforcement

Different mechanisms decide grants at different times; Glyphux uses all three:

```
1. Marketplace review (publish time)   — static analysis + human review;
                                          risk-grades capabilities, flags raw-resource requests.
2. Install-time consent (install time) — the admin sees a consent screen:
                                          "commerce requests: content[read,write], payments[charge,refund],
                                           admin UI, network→api.stripe.com — Allow?"
                                          This is the trust surface WordPress never had.
3. Runtime enforcement (call time)     — the boundary (WASM host / RPC broker) exposes ONLY
                                          declared-and-consented capabilities; everything else
                                          literally does not exist to the plugin.
```

### 10.3 The Boundary Is the Enforcement Point

The WASM host and RPC broker are not merely transport. They are where a plugin is stripped of anything it did not declare and the admin did not consent to. Capabilities granted = host functions exposed. If enforcement lived elsewhere, plugins would find the elsewhere.

### 10.4 Raw Resources Are Near-Forbidden

Third-party plugins **cannot** request raw database access, raw filesystem access, or unrestricted network. Rationale: raw DB access bypasses every other capability boundary, makes the data layer unevolvable, and is WordPress's deepest structural wound (`$wpdb`). Plugins that need persistence get a **scoped, platform-mediated** key-value/table namespace. Plugins that need network get an **allowlisted** set of hosts. First-party kernel code is the only code with raw access, because it *is* the platform.

### 10.5 Capability-Based Permission Engine

User-facing and plugin-facing permissions share one capability-based engine: roles map to capability scopes; the engine enforces at the domain-API boundary; policies are data, hot-updatable, and audit-logged. Every sensitive grant and every cross-boundary call is recorded by the audit subsystem.

---

## 11. Data Architecture

### 11.1 Storage Philosophy

The "host anywhere, single binary" promise drives the data layer:
- **SQLite (embedded)** is the default — one binary, one file, zero external services. This is what makes Glyphux deployable on a $5 VPS or a laptop.
- **PostgreSQL** is the scale option, selected by configuration, for multi-instance and high-concurrency deployments.
- The database is reached only through the kernel's database abstraction; **no client or plugin ever sees it directly.**

### 11.2 Why Plugins Never Touch the Database

Stated as a principle in §4 and §10.4; restated here as a data-architecture invariant: because plugins interact only through typed domain APIs and a scoped storage namespace, the underlying schema can evolve freely across versions without breaking the ecosystem. This is the property that makes "safe upgrades" real rather than aspirational, and it is the single largest divergence from WordPress.

### 11.3 Content Versioning and Drafts

The content engine supports draft/publish state and version history as first-class concerns of Layer-1 composition, not as a plugin. These are foundational because membership, commerce, and editorial workflows all depend on them.

### 11.4 Media Pipeline

Media is a kernel concern: upload, validation, transformation (resize/format), and serving, behind a storage abstraction with adapters for local filesystem (default), S3, and MinIO. Plugins access media only through the scoped `Media()` domain API.

**Media library (the user-facing gallery).** Above the pipeline sits a **media library**: the searchable, browsable surface where a user uploads, organizes (folders/tags), searches, previews, and selects media to use in a design. The library is an admin/builder UI surface (Surface 2, §5.6) — a *client* of the `Media()` domain API, holding no privileged access — and is built in two stages: a basic library manager in Phase 1 (slice 1.6) and a rich in-builder media picker in Phase 4 (drop assets onto the canvas, pick for block fields). The library stores per-asset metadata (dimensions, type, alt text, tags, source/attribution).

**Media editing scope (tiered, to avoid ballooning).** Basic editing — crop, resize, rotate, format conversion — is part of the pipeline already and is exposed in the library/picker as a real V1-class feature. Heavy editing (filters, layers, "Photoshop-lite in the browser") is **explicitly deferred** as a possible future capability, so "media editing" does not quietly grow into building an image editor. The same tiered discipline applied to the builder and to composition layers applies here.

### 11.4a Stock Media Integration (Capability)

Integration with royalty-free stock providers (Unsplash, Pixabay, Pexels) is a **`stock-media` capability** (§7), not a kernel concern — provider-agnostic behind an adapter exactly as storage, AI, and payments are. It exposes a scoped search/import API, depends on `media`, and requires **allowlisted network** to the provider APIs (per the capability permission model, §6.3). Third parties can add new providers as plugins registering additional stock adapters.

**Import semantics — fetch-and-store, never hot-link (decision).** When a user imports a stock asset, the capability **fetches the asset and stores it in the user's own media library/storage**, recording the provider, license, and required attribution metadata — it does **not** hot-link to the provider's CDN. Rationale: hot-linking reintroduces an external runtime dependency (the site breaks if the provider changes, rate-limits, or removes the asset), which contradicts Glyphux's self-hostable "you own it" identity; and fetch-and-store is what respects provider API terms cleanly. Attribution metadata travels with the asset so themes/templates can surface it where a provider requires it. This is a first-party capability sequenced in Phase 3, an enhancement (like AI), not a pillar.

### 11.5 Multi-Tenancy (Planned Capability — Deferred, Not V1)

Multi-tenancy is a **planned capability that is deliberately deferred past V1**, and the deferral is a considered decision, not an oversight. Tenancy is one of the most invasive things a platform can add: it touches every query, every permission check, every cache key, and every connection-pool strategy. Building it speculatively before a real tenant exists is waste; retrofitting it onto a data model that ignored it is painful. The resolution is to **design for it without building it**:

- The data model, identity system, and permission engine are designed so a `tenancy` capability (schema-scoped or row-scoped) can be added **without redesign** — every domain table carries the seam (a nullable tenant scope) a future tenancy layer would activate, and the permission engine's capability checks are written to accept a tenant dimension that is a no-op until tenancy is enabled.
- `tenancy` appears in the capability dependency graph (§7.2) as `tenancy → identity, permissions`, so its resolution rules are specified in advance.
- Connection pooling (§11.6) is designed so a future schema-per-tenant or pool-per-tenant strategy can slot in behind the database abstraction.

**It is not built in V1.** It is scheduled only when a real deployment requires it, so its order-of-magnitude complexity does not tax the foundational phases. This mirrors the layered-composition discipline: do not let a heavy, far-future concern pollute the simple, foundational one before it has earned its place.

### 11.6 Connection Pooling and Streaming

Database connection management lives in the adapter layer, behind the database abstraction, and is never exposed to plugins or clients:

- **Pooling:** the Postgres adapter maintains a connection pool with sane auto-sizing (min/max connections, lifetime, idle timeout, health-check interval, acquire timeout). SQLite (the default, embedded) needs minimal pooling and uses a lightweight strategy.
- **Streaming / cursors:** large result sets are served via streaming cursors rather than buffering entire result sets in memory, so content-heavy reads and exports do not blow the memory budget (§15.1).
- **Forward-compatibility:** the pool strategy is designed so a future per-tenant pooling or schema-routing layer (for the deferred tenancy capability) can be introduced without changing the abstraction's consumers.

---

## 12. The Marketplace and Package System

### 12.1 The Package Model

Plugins and themes are distributed as signed packages with a manifest (§7.3). The package system handles resolution, versioning (semver), signing, and updates. The same package machinery serves both plugins and themes; only the registry namespace differs.

### 12.2 Licensing Is Machine-Readable

The manifest `license` field is informational metadata that lets the marketplace filter and display (free / paid / GPL / proprietary / commercial) and lets users avoid licenses incompatible with their needs. The field documents a separation that the **runtime boundary** (separate WASM sandbox / RPC process = separate work) already guarantees — it does not create that separation.

### 12.3 Commercial Ecosystem

Monetization happens entirely at the ecosystem layer: paid themes and plugins, sold through the marketplace, are clean because the RPC/WASM boundary makes proprietary extensions a separate work from the Apache-2.0 core. First-party premium capabilities (commerce, membership, marketplace) are sold the same way and go through the same public API (Principle 6). Paid extensions are protected by signing, offline entitlement, and update-gating rather than source encryption (§12.5), and the marketplace is designed to grow into a multi-vendor third-party seller platform (§12.6).

### 12.4 The Marketplace Is a Trust Surface

The registry performs publish-time review (§10.2): static analysis of declared vs. used capabilities, flagging of raw-resource requests, signing verification, and semver/compatibility checks against the declared core and contract versions. The host refuses to load a package whose declared contract version is incompatible — regardless of tier.

### 12.5 Paid Extension Protection

Paid themes, plugins, presets, and bundles are protected by securing the **commercial relationship** (entitlement, updates, support, authenticity), **not** by encrypting the code. This choice is deliberate and is consistent with Glyphux's identity ("self-host anywhere, you own it, no phone-home").

**Why not source encryption / DRM.** To run an extension the host must decrypt it, so the decryption key is on the user's own machine — making runtime encryption obfuscation, not protection (the WordPress "nulled plugin" history demonstrates it is routinely defeated at high vendor cost). It is also legally hazardous: GPL-licensed extensions (a real part of the catalog) require source availability, so shipping them encrypted may violate their license. And it contradicts the "you own your instance" promise. **Source-encryption-as-DRM is therefore rejected.**

**What protects paid extensions instead — four mechanisms, all offline-first:**

1. **Signing (integrity + authenticity).** Every package is signed (§12.1, §12.4); the host verifies the signature locally at install/load. This delivers what encryption is usually reaching for — proof the package is genuine and untampered — protecting buyers (no malicious builds) and vendors (no malware shipped under their name).

2. **Offline-verifiable entitlement tokens (no phone-home).** A purchase issues a cryptographically signed entitlement token (buyer/license ID, entitled extensions, validity window, allowed scope), signed with the marketplace's private key. The host ships the marketplace's public key and verifies the token **locally, offline, at install and load** — no network call to confirm entitlement. Air-gapped and offline installs work indefinitely; the buyer can manually import the token where needed.

3. **Update-gating (the primary lever).** What buyers actually pay for in a healthy ecosystem is updates, security patches, and support — not a one-time code drop. Updates are fetched from the marketplace *when the instance is online and chooses to update*, and the entitlement token's validity window is checked at **download** time. Offline instances simply don't receive updates (a natural consequence, never a forced outage). Combined with the compatibility contract (ADR-008): an un-renewed or pirated copy is frozen at its version and, as the platform's contract version advances, eventually becomes incompatible and the host declines to load it — anti-piracy pressure as a *side effect of normal versioning*, not a DRM kill-switch.

4. **Encrypted, authenticated delivery + secrets at rest.** Paid packages are delivered to entitled buyers over TLS with signed payloads. Separately, secrets (license keys, gateway credentials, OAuth tokens) are **encrypted at rest** in the secrets store (§15.3). This is where encryption legitimately belongs — protecting secrets and transport, not hiding installed code.

**Graceful expiry — never a kill-switch.** When an entitlement window lapses, the installed extension **keeps working**; only *new updates* stop until renewal (the perpetual-fallback model). No license check ever disables a running production site. This is the firm rule: licensing gates *updates and support*, never *execution of already-installed code*.

**Revocation trade-off (stated honestly).** Offline-verifiable tokens make revocation (fraud, chargeback) harder, since an offline instance can't learn of it. Mitigations: short-ish token windows with automatic online renewal (a revoked license simply stops getting its next token and the current one expires within weeks/months), plus a revocation list distributed through normal update checks for online instances. A determined offline abuser can ride one token to expiry — a small, bounded loss accepted deliberately rather than dragged back toward a phone-home kill-switch.

**Lessons from comparable models (the goal is to make piracy *irrelevant*, not impossible).** Laravel's commercial products (Nova, Spark) are the closest real-world precedent and validate this design: their protection is a license-key + entitlement model where a purchase includes one year of updates, expiry degrades gracefully (the installed version keeps working but its dependencies drift toward incompatibility over time), distribution is gated through a private package registry credential, and the visible "unregistered" DRM marker is trivially stripped by nulled copies — which Laravel's own team publicly shrugs at, because piracy does not meaningfully dent the business. Nulled Nova demonstrably exists; it simply doesn't matter. Three sharpenings follow for Glyphux:

- **Convenience is the strongest protection.** Laravel's most effective de-facto deterrent is that the legitimate path (updates through the package manager, "it just works") is dramatically more convenient than chasing a pirate registry for every patch. Glyphux should invest in making the legitimate marketplace update path so seamless — and the pirated path so manually painful, perpetually behind, and drifting toward incompatibility via the compatibility contract (ADR-008) — that nulling is a downgrade in experience, not a saving. This is a product-experience investment, not a DRM one.
- **Segment expectations by audience.** Developer/agency buyers behave like Laravel's (low piracy: production stakes, legal/reputational risk, low price relative to billings). Non-technical and future marketplace (§12.6) buyers behave more like the WordPress-plugin audience (more price-sensitive, more null-tolerant). Protection will not be uniform across both populations; price the more null-prone tier accordingly rather than expecting DRM to equalize them.
- **Accept bounded piracy.** Chasing zero piracy with heavier DRM punishes paying users (Laravel's own forums show legitimate users annoyed by license nags that nulls don't experience) while nulls strip the DRM anyway. The achievable, correct goal is making piracy *irrelevant* — via convenience, fair pricing, earned trust, and update-gating — not making it impossible. This reinforces the rejection of source-encryption DRM and phone-home (ADR-016). Note the one advantage Glyphux cannot copy: Laravel's low piracy rests partly on a decade of ecosystem trust and goodwill; Glyphux must earn that rather than assume it.

### 12.6 Third-Party Seller Marketplace (Growth Path)

Glyphux's marketplace is designed to grow from a first-party/curated catalog into a **multi-vendor commercial marketplace** where independent developers and agencies list and sell their own themes, plugins, blocks, presets, and bundles — the role CodeCanyon / ThemeForest play for the PHP/WordPress world, but native to Glyphux and built on its capability and entitlement model.

This is an explicit *forward direction*, not a V1 deliverable. The foundations are laid by the existing design so the growth path needs no re-architecture:
- The **`marketplace` capability** (Phase 3, slice 3.5) already provides packaging, signing, versioning, and multi-vendor primitives.
- The **entitlement + update-gating model** (§12.5) is per-license and vendor-agnostic, so third-party sellers inherit the same protection as first-party.
- The **publish-time trust review** (§12.4) and **capability-scoped manifests** (§7.3) mean third-party paid listings are vetted and sandboxed by the same rules — critical when the catalog opens to arbitrary sellers (the adversarial trust model, §9, was built for exactly this).
- The **four-artifact model** (§13) gives sellers clear, distinct product types to list.

When this opens, the additional concerns to design at that time (noted now so they are not foreclosed): seller onboarding/identity and payout, revenue split, refund/chargeback handling tied to the revocation flow (§12.5), seller reputation/ratings, and marketplace-level curation and takedown for the larger adversarial surface a public seller base creates. None of these change the core architecture; they are commercial and operational layers on top of the existing trust, entitlement, and packaging machinery.

### 12.7 Release Naming Convention

Glyphux uses **two versioning systems simultaneously**, as mature software does (Android, Ubuntu, macOS): machine-facing semantic versions and human-facing codenames. They serve different purposes and must not be conflated.

**Semantic versions are authoritative — the contract.** Semver (`MAJOR.MINOR.PATCH`) is the single source of truth for compatibility, dependencies, and update-gating. Everything the machine reads — `requires.core`, `requires.contract`, the compatibility contract (ADR-008), entitlement/update logic (§12.5) — uses the number, never the codename. A codename is never a dependency identifier and never appears in a manifest's compatibility fields.

**Codenames are human-facing decoration — never a replacement.** Major releases carry a codename alongside the number ("Glyphux 1.0 'Jenson'"), for marketing, community, and memorability. The number always leads; the codename always accompanies. This avoids the failure Android itself corrected around v10 — dropping public dessert names because "is Pie newer than Oreo?" was cognitively worse than "is 9 newer than 8?" The number answers ordering and compatibility; the name gives the release identity.

**The theme: historic typefaces and punchcutters.** Codenames are drawn from historic, public-domain typefaces and the punchcutters who cut them — an opening run such as **Jenson, Garamond, Caslon, Baskerville, Bodoni, Didot**, reserving the most iconic names for major milestone releases. Rationale: a "glyph" *is* a letterform, so a typeface series is the brand's own meaning extended into the release line — intentional, not arbitrary decoration; it flatters both the developer and designer audiences; and it signals the premium-craft objective (§2.5, §5.7). The well is effectively bottomless (centuries of named faces).

**Constraints on codename selection:**
- Favor **historic, public-domain names** (16th–18th century punchcutters and faces) over 20th-century proprietary families — both to avoid trademark questions (some modern typeface names are registered marks) and because the historic names carry more gravitas.
- Favor names that are **distinctive and reasonably pronounceable** across the platform's international audience.
- Reserve the most iconic names for **major (MAJOR-version) releases**; the series signals magnitude by which names are spent where.

**When codenames begin — a scope guard.** Codenames start at **1.0** and apply only to **major/milestone releases**. Pre-1.0 phase and preview builds use plain semver (`0.x.y`) and phase labels — giving a phase-build a codename adds ceremony to internal engineering work. This convention is recorded now (it is a cheap one-time decision) but is *acted on only at the 1.0-era*, not during the Phase 0–4 build. Naming releases is not a substitute for shipping them.

---

## 13. Blocks, Presets, and Bundles — The Builder Artifacts

WordPress fuses four things into "a builder" (Divi/Elementor/Astra): the builder *engine*, the *widgets*, the *templates/demos*, and the *theme*. Glyphux keeps them as four distinct, clean concepts, because the fusion is precisely the lock-in and the swamp Glyphux exists to avoid. This section is the Rosetta stone between the familiar WordPress mental model and Glyphux's artifacts, and it defines what users build with and what sellers distribute.

### 13.1 The Core Inversion: The Builder Is First-Party; Its Output Is Portable Composition

In WordPress, the builder engine is a third-party plugin (Elementor, Divi), which is *why* there is chaos: competing builders, content authored in one builder breaking when disabled, opaque shortcode markup. In Glyphux there is **one** builder, it is first-party (Phase 4, §5.6), and **what it produces is not builder-proprietary markup — it is Layer-2 Layout Composition** (§3.2): typed, serializable, validatable composition that the API serves and any theme renders. Content built in the Glyphux builder is never trapped in the builder; the composition is the truth, the builder is one editing client of it. This single inversion eliminates WordPress's worst lock-in.

### 13.2 The Four Artifacts

```
WordPress mental model        →   Glyphux artifact            →   Distributed as
─────────────────────────────────────────────────────────────────────────────────
Builder engine (Elementor)    →   The first-party builder    →   ships with platform
                                  (edits Layer-2 composition)     (not a marketplace item)

Widget / module (a hero,      →   Block                      →   a plugin that registers
 a pricing table, a form)         (registered via the public      blocks (free or paid),
                                   API; logic-bearing blocks       Tier-B WASM / Tier-C RPC
                                   run as sandboxed plugins)

Template / layout / page      →   Composition Preset         →   a marketplace package
 pattern (a saved section          (a serialized fragment of       (free or paid)
 or page you drop in)              Layer-2 composition)

Demo site / starter site /    →   Composition Bundle         →   a marketplace package
 full template import              (pages + presets + sample       (free or paid)
                                   content + a theme reference)

Theme / skin (Astra)          →   Theme (Surface 3 renderer) →   a marketplace package
                                  (styling + slots; NEVER          (free or paid)
                                   owns layout or logic)
```

**Block** — a registered, composable unit (hero, grid, cards, form, pricing table). First-party blocks ship with the platform; third-party blocks are registered by plugins through the public extension API (`RegisterBlock`, §8.3; slice 4.2). A block with logic runs as a sandboxed, capability-scoped plugin (custom blocks are explicitly a Tier-B/WASM use case, §8.1). A "pro widget pack" is a commercial plugin registering a set of blocks.

**Composition Preset** — a serialized fragment of Layer-2 composition: a saved arrangement of blocks with placeholder content, at any scale (a section, a page, a multi-page pattern). "Import a template" = merge a preset's composition into yours. Because a preset is just typed composition, it is portable across themes and tied to no specific builder — cleaner than any WordPress template format.

**Composition Bundle** — a preset collection at site scale: pages + presets + sample content + a theme reference. "Import this starter site / demo" = import a bundle. This is the Astra/Divi "import the demo and it looks right" experience, expressed as typed composition.

**Theme** — the renderer (Surface 3, §9). Supplies styling, design tokens, and the slot/region definitions presets target. It does **not** contain layout logic or the builder. Consequently, re-skinning a built page = swapping the theme, with layout composition untouched — the payoff of "themes render composition; they never own it" (§9.1, ADR-007).

### 13.3 The Compatibility Contract (The Hard Part — A Phase-4 Design Concern)

The deceptively difficult problem — the one WordPress builders spent a decade on — is not the import mechanism (merging composition is trivial) but **graceful degradation**: a preset designed against one theme's slots and one set of blocks must behave predictably when imported into a different theme or when a referenced block plugin is absent. Glyphux has a structural advantage because everything is typed composition rather than opaque markup, so this is *validatable* rather than silently broken. The compatibility contract, specified explicitly in Phase 4, governs:

- What a **preset may assume** (which blocks, which slots) — declared in the preset's manifest.
- How a **theme declares the slots/regions** a preset can target.
- What happens to a **block the current setup lacks**: import-time validation surfaces "this preset needs the `pricing-table` block — install it?" rather than rendering garbage.
- **Versioning**: presets and bundles declare the contract version, theme compatibility, and block dependencies; the marketplace checks these at publish and the host at import.

This is the difference between "import works like magic" and "import works like WordPress." It is real Phase-4 work and is called out as such (slice 4.x).

### 13.4 Marketplace Distribution and Monetization

All four distributable artifacts flow through the one signed-package marketplace (§11–§12): free and paid **block plugins** (widgets), free and paid **composition presets** (templates), free and paid **composition bundles** (starter sites/demos), and free and paid **themes** (renderers). Each carries a manifest with `license`, `requires.core`, `requires.contract`, declared block dependencies, and (for presets/bundles) theme compatibility. The WordPress monetization model (free themes + pro theme/template packs + premium widget bundles) maps directly — and every paid artifact is clean because the RPC/WASM boundary (for blocks) and the read-only theme contract (for themes) keep proprietary work a separate work from the Apache-2.0 core (§16).

---

## 14. AI Integration

AI in Glyphux is an **enhancement, not a pillar**, and — critically — **AI is never a kernel concern and never a privileged surface.** It is consumed only through *existing* architectural slots: a capability, a builder client, and a Layer-3 action type. An AI plugin is sandboxed and capability-scoped like any plugin; AI-generated composition is validated like any composition; an AI agent action runs in the Layer-3 engine like any action. Nothing about AI bypasses the model. This is what keeps "add AI" from becoming the hole through which the architectural discipline leaks out.

### 14.1 The Four AI Surfaces

```
Surface                              Home in Glyphux                 Phase
───────────────────────────────────────────────────────────────────────────────
1. AI for the products users build   `ai` capability (§7)           Phase 3 (first-party)
2. AI authoring inside the builder   Builder client feature         Phase 4 (needs builder)
3. AI agent workflows                Layer-3 action type            Phase 5 (gated)
4. AI dev/scaffolding tooling        CLI/SDK tooling                Opportunistic / maybe external
```

**Surface 1 — AI as a capability for built products.** A developer building on Glyphux wants "summarize this," "generate descriptions," "semantic search/embeddings." This is an `ai` capability in the §7 sense: it depends on `content` and `events`, exposes scoped domain APIs (`generate`, `embed`, `classify`), and is **provider-agnostic behind an adapter** (Claude, OpenAI, Ollama/local, etc.) exactly as storage has FS/S3/MinIO adapters. A plugin wanting AI declares `api: [ai: [generate]]` and receives a scoped, rate-limited surface — **never a raw API key, never raw network to the provider** (allowlisted through the capability). First-party, can land in Phase 3 alongside notifications.

**Surface 2 — AI authoring in the builder ("seamless development").** "Describe the page and the builder assembles it," "generate a content model from a prompt," "rewrite this section." The key discipline: **AI in the builder emits composition, not markup.** AI output is a Layer-2 composition fragment (blocks + presets) run through the *same validation path as a preset import* (§13.3) — so "AI used a block you don't have" is a catchable, explainable condition, not silent breakage. This is the structural advantage over WordPress AI builders (which emit opaque markup). Phase 4; cannot precede the builder.

**Surface 3 — AI agent workflows.** "On form submit, an agent classifies the lead, drafts a reply, updates the CRM, schedules follow-up." Architecturally honest statement: **this IS Layer-3 Application Composition** (§3.2) where one action type is "invoke an agent." It is not a separate engine — it is the event-chain-with-side-effects engine (slice 5.2's action registry) with an LLM as an action. It therefore inherits Layer-3's entire deferral rationale: gated to Phase 5, built only after Layers 1–2 are mature and externally validated. Building agent orchestration before the workflow engine exists would be starting the house at the roof.

**Surface 4 — AI dev/scaffolding tooling.** "Generate a plugin skeleton / content type / theme." This is CLI/SDK-time developer productivity, the least entangled with the runtime. It may be better served by existing agentic-coding tools (e.g. Claude Code) pointed at a well-documented SDK than by Glyphux building its own. Opportunistic; possibly out of scope.

### 14.2 Provider-Agnostic by Adapter

AI features age fast and couple you to providers. The `ai` capability treats Claude/OpenAI/Ollama/local models as **swappable adapters behind a stable internal contract** — the same discipline as database, storage, and payment adapters. This protects against "we built everything around one provider's API shape and it changed." No model provider's specifics leak above the adapter boundary.

### 14.3 Sequencing Discipline

AI agent workflows (Surface 3) are the most *exciting* and the most *premature* of the four. The pull to ship "Glyphux AI Agents" as an early flagship will be strong — it demos well and it is what is hot. But it sits on Layer-3, on the builder (Phase 4), on the extension system (Phase 2), on the headless core (Phase 1). The order is: Surface 1 early (clean capability), Surface 2 when the builder lands, Surface 3 only when Layer-3 is real. AI being an enhancement means it never reorders the phase sequence to accommodate itself.

---

## 15. Non-Functional Requirements

### 15.1 Performance Targets

```
Cold start (single binary, SQLite):        < 200 ms
Idle memory (core, SQLite):                < 30 MB
Content API read (cached):                 < 10 ms p50
Content API read (uncached, indexed):      < 50 ms p95
WASM plugin hook invocation:               < 1 ms p95 (in-process)
RPC plugin call (local):                   < 10 ms p95
Page render (server-side, cached theme):   < 50 ms p95
```

### 15.2 Reliability

- Graceful shutdown with in-flight request draining.
- Plugin crash isolation: a Tier-C plugin crash never takes down the core; a Tier-B WASM trap is contained and surfaced as an error.
- Health and readiness endpoints.
- Idempotent migrations with a stable, version-controlled schema baseline.

### 15.3 Security

- Adversarial plugin model (§10) enforced at the boundary.
- Kernel security primitives (§5.1): CSRF/CORS handling, rate limiting, input sanitization, and session hardening — first-class, not plugins.
- Identity hardening: secure session handling, credential storage best practice, OAuth/social, and MFA in the kernel `identity` concern.
- All secrets via environment/secret provider, never in composition files; the secrets store encrypts secrets (license keys, gateway credentials, OAuth tokens) **at rest** (§12.5).
- OWASP-aligned: access control at the domain-API boundary, input validation in the content engine, supply-chain checks in the marketplace (signing + review).
- Audit log for all sensitive operations and cross-boundary calls.
- HTTPS by default in production; required for any non-localhost first-run wizard access (§6.4).

### 15.4 Observability

- Structured logging, request IDs, and basic metrics built into the kernel (not a plugin).
- An `observability` capability can extend this with tracing/metrics backends.
- Every plugin boundary crossing is traceable.

### 15.5 Portability

- Single static Go binary; Linux (x86_64/arm64), macOS, Windows.
- No required external services in the default (SQLite + local-FS) configuration.
- Cross-platform install via script, Homebrew, Scoop, and direct binary.

### 15.6 Backward Compatibility

- The composition contract is explicitly versioned; a core release supports a documented range of contract versions.
- Plugins/themes declare `requires.core` and `requires.contract`; incompatible packages are refused at load, never crash the host.
- Breaking changes to the public extension API require a contract major-version bump.


---

## 16. Implementation Phases

This roadmap is organized into **phases**, each delivered as **vertical slices**. A vertical slice cuts through every layer (kernel → domain API → contract → client) to deliver one end-to-end, demonstrable, production-deployable capability — never a horizontal layer in isolation. The team should be able to deploy and use the output of every phase, not just the final one.

The sequencing is **derived from the capability dependency graph** (§7.2), not chosen for convenience: content is the root, so it comes first; everything else branches from it. The hardest surface (the visual builder) comes only after the composition contract is proven by real external usage.

> **Discipline gate between every phase:** No new abstraction enters the shared/core surface unless it retires real duplicated code or solves a real external use case. No new layer is justified by something imagined while designing — only by something learned from shipping. Phase N+1 does not start until Phase N is deployed and has produced at least one piece of external feedback.

### Phase 0 — Foundations (Walking Skeleton)

**Goal:** A single binary whose `glyphuxd` daemon boots, serves an HTTP endpoint and a minimal first-run web wizard, loads a trivial composition, and persists to SQLite. Proves the spine — including the wizard-as-client model — end-to-end before any feature.

**Vertical slices:**
- **0.1 — Daemon + config + boot:** `glyphuxd` daemon, config loader, graceful start/stop, health endpoint. Single static binary, SQLite embedded. (`glyphux` developer CLI is a thin secondary wrapper, not the boot path.)
- **0.2 — Composition contract v0 + validator:** parse a composition document, validate against the schema, reject invalid ones. The contract type lives in `pkg/contract` (public from day one).
- **0.3 — Database abstraction + SQLite adapter + migrations:** schema baseline, idempotent migration runner, no raw access leaking above the abstraction.
- **0.4 — Minimal domain-API boundary:** one trivial domain API (e.g. a "ping" content read) exercised through the contract, proving the Communication Law plumbing.
- **0.5 — First-run web wizard (skeleton):** the daemon serves a minimal browser wizard on first boot — create admin account, choose database (SQLite default / Postgres DSN), set site name — which writes the *initial composition*, then locks the first-run route. This proves the single-wizard model (§6) and the wizard-as-composition-client idea at the spine level, scoped to the bare minimum per §6.6.

**Definition of done:** the daemon deploys to a VPS and runs locally; boots in < 200 ms; on first run serves the web wizard over HTTP (local: opens browser; server: reachable over network with a setup token); completes setup writing an initial composition; serves health + one contract-driven endpoint; survives restart with persisted state and a locked first-run route. CI builds cross-platform binaries.

### Phase 1 — Headless Content Core (THE WEDGE)

**Goal:** A complete, sellable, headless content + data modeling engine — the Strapi/Payload/Sanity-class product. This is the first thing real external developers use. **No renderer, no builder.**

**Vertical slices:**
- **1.1 — Content types & fields:** define content types and fields (string, richtext, number, boolean, date, relation, media) via composition; persisted and migrated automatically.
- **1.2 — Content CRUD domain API:** typed Content API (read/write/delete, scoped) exposed over HTTP/JSON. This is the headless product's primary surface.
- **1.3 — Relations & validation:** typed relations between content types; field validation enforced in the content engine.
- **1.4 — Localization:** per-field localization in the content model.
- **1.5 — Drafts, publish, versioning:** draft/publish state and version history as Layer-1 concerns.
- **1.6 — Media pipeline + library:** upload, validate, transform (crop/resize/rotate/format), serve; local-FS adapter default; scoped Media API. Plus a **basic media library UI** (Surface 2) — upload, browse, search, tag, preview, basic edit — as a client of the Media API.
- **1.7 — Identity & sessions:** accounts, sessions, credentials, OAuth/social providers, MFA (kernel `identity`). Designed with a no-op tenant seam (§11.5) so tenancy can be added later without redesign.
- **1.8 — Permission engine v1:** capability-based permissions; roles → capability scopes; enforced at the domain-API boundary. Capability checks accept a tenant dimension that is a no-op until tenancy is enabled.
- **1.9 — Security primitives:** CSRF/CORS handling, rate limiting, secrets provider, input sanitization, session hardening — kernel-level, per Principle 8, not a plugin.
- **1.10 — Connection pooling + streaming:** Postgres connection pool with auto-sizing and lifecycle management; streaming cursors for large reads/exports; behind the DB abstraction, never exposed (§11.6).
- **1.11 — Typed SDK (Go + JS):** generated/typed client SDKs consuming the content API — the second client of the contract (after the raw API), proving multi-client. The JS SDK (`sdk-js`) is also the surface the admin UI consumes.
- **1.12 — GraphQL transport (optional within phase):** GraphQL surface over the same domain APIs.
- **1.13 — Wizard + admin shell (grown, not gold-plated):** extend the Phase-0 wizard skeleton into a usable first-run + minimal admin surface for managing content types, content, media, and identity through the browser — still a client of the contract, no privileged access. **Built in the committed admin stack (React + TS + Vite + Tailwind, embedded via `go:embed`; §5.6)** so it shares the exact stack the Phase-4 builder will extend. **Establishes the design-token system, component library, accessibility quality floor, and basic admin theming (light/dark, brandable accent) from the start (§5.7)** — these are designed in, not retrofitted. Kept deliberately utilitarian in *scope* (not in *quality*); this is onboarding/management polish on the headless core (§6.6), not the up-market builder (Phase 4). For list-shaped interactions here (reorder fields, sort types) a sortable library suffices; the heavy DnD canvas (dnd-kit + Craft.js/Puck) arrives only in Phase 4.

**Definition of done:** an external developer can model content, push/pull it via API and SDK, manage media and identity, and self-host the whole thing as one binary — **without any help from the Glyphux team.** This is the release that gets put in front of real developers.

**Phase-1 success metric (the only one that counts):** not downloads, not stars — *how many assumptions did external developers break?* Every "wrong" usage becomes documentation, a bug, or a missing abstraction.

### Phase 2 — Extension System (Make It a Platform)

**Goal:** Turn the headless product into a *platform* by shipping the public extension API and both third-party plugin tiers — proven by reimplementing a first-party capability through it.

**Vertical slices:**
- **2.1 — Unified extension contract + HostAPI:** the public `Plugin`/`HostAPI` surface in `pkg/sdk`; capability-gated, scoped domain APIs.
- **2.2 — Event bus:** subscribe/emit, scoped by capability; lifecycle + domain events.
- **2.3 — Capability registry + dependency resolver:** declare/resolve capabilities, reject cycles, surface implicit inclusions.
- **2.4 — WASM runtime (Tier B):** Wazero host + WIT contract; deny-by-default sandbox; scoped HostAPI exposure; scoped KV persistence.
- **2.5 — RPC runtime (Tier C):** gRPC broker, process supervision, crash isolation, allowlisted network.
- **2.6 — Manifest + two-axis permissions:** api vs. permissions axes; scoped grants; `requires.core`/`requires.contract` enforcement.
- **2.7 — Install-time consent engine:** the admin consent screen; the trust surface.
- **2.8 — Audit logging:** record sensitive grants and boundary crossings.
- **2.9 — Dogfood: `forms` capability as a first-party plugin** built entirely on the public API (Principle 6) — the proof the API is real.

**Definition of done:** a third-party developer can publish a WASM plugin (e.g. an SEO-meta or content-transform plugin) and an RPC plugin, declare scoped capabilities, and have an admin install it through a consent flow — with the boundary enforcing scopes. First-party `forms` ships through the identical public path.

### Phase 3 — First-Party Capabilities (Prove the Ecosystem Economics)

**Goal:** Ship the premium capabilities that demonstrate the commercial model, each as a first-party plugin on the public extension API, each dogfooding a different stress on the API.

**Vertical slices (sequenced by dependency):**
- **3.1 — `notifications` capability** (Tier C/RPC): email / SMS / push / in-app, over events + a mailer adapter (region-relevant providers, e.g. Resend/Vonage/FCM). Built early because commerce and membership depend on it. Validates the RPC tier with a low-risk, broadly-useful capability.
- **3.2 — `seo` capability** (Tier B/WASM): content-derived meta, sitemaps, structured data. Low-risk, validates the WASM tier under real load.
- **3.3 — `commerce` capability** (Tier C/RPC): product & order content types, checkout components, payment events, admin pages, allowlisted payment-gateway network. Validates the heavy RPC tier and payment-scope isolation. Integrates region-relevant gateways (e.g. Paystack/Flutterwave/Stripe).
- **3.4 — `membership` capability** (Tier C/RPC): gated content, subscription state, recurring billing; depends on identity, permissions, content, notifications, and commerce primitives.
- **3.5 — `marketplace` capability:** packaging, signing, versioning, multi-vendor — the capability that also underpins the public marketplace. Includes the **paid-extension protection model** (§12.5): signing, offline-verifiable entitlement tokens, update-gating, and graceful expiry — no source DRM, no phone-home. Lays the foundation for the future third-party seller marketplace (§12.6).
- **3.6 — `ai` capability (optional, enhancement)** (Tier C/RPC): provider-agnostic AI behind an adapter (Claude/OpenAI/Ollama/local) exposing scoped `generate`/`embed`/`classify` domain APIs; no raw keys or raw provider network reach plugins (§14.1, Surface 1). Sequenced last in the phase and only if it earns its place — AI is an enhancement, not a pillar (§14).
- **3.7 — `stock-media` capability (optional, enhancement)** (Tier C/RPC): provider-agnostic stock-media search/import (Unsplash/Pixabay/Pexels adapters), allowlisted network, **fetch-and-store into the user's own media library with attribution** — never hot-link (§11.4a). Depends on `media`; an enhancement, not a pillar.

**Definition of done:** each capability is installable, scoped, and consented like any third-party plugin; none has private access the public API lacks. The commercial ecosystem story is demonstrable end-to-end (a paid plugin installed through the marketplace, enforced at the boundary).

### Phase 4 — Layout Composition + Visual Builder (Move Up-Market)

**Goal:** Add Layer-2 (Layout Composition) and ship the visual builder as the best *client* of the composition contract — not its foundation. This is deliberately last because it is the hardest software in the vision and because the builder can only be designed well once you know, from real usage, what it is editing.

**Vertical slices:**
- **4.1 — Layout Composition contract (`layout-composition/v1`):** blocks, slots, regions, sections — additive to Layer 1, never polluting it.
- **4.2 — Block registration (extension point):** plugins/themes register blocks via the public API (§13.2); first-party blocks ship in-tree, third-party blocks arrive as plugins (logic-bearing blocks as Tier-B WASM).
- **4.3 — Server-side layout rendering:** themes render Layer-2 composition.
- **4.4 — Visual builder (client):** nested, multi-target, rule-based drag-and-drop editing of the composition through the public contract; holds no privileged access the SDK lacks. **Extends the same React stack the Phase-1 admin shell established (§5.6)** — built on dnd-kit primitives with a page-builder framework (Craft.js or Puck) supplying the editor state model, node tree, and serialization. This is the load-bearing pillar that justified the React choice; it is not a separate app from the admin shell, it is the admin shell grown up.
- **4.5 — Live preview:** the editor renders the same composition the API serves.
- **4.6 — Composition presets & bundles + compatibility contract (§13):** save/import presets (composition fragments) and bundles (starter sites: pages + presets + sample content + theme ref); the **import-time compatibility contract** — preset manifests declaring block/slot assumptions, themes declaring available slots, missing-block detection that prompts "install the `pricing-table` block?" rather than rendering garbage. This is the hard part of the builder (§13.3) and is explicit Phase-4 work.
- **4.7 — Marketplace distribution of builder artifacts (§13.4):** blocks, presets, bundles, and themes published as signed packages with declared dependencies and license; free/paid; compatibility checked at publish and import.
- **4.8 — AI authoring in the builder (optional, enhancement; §14.1 Surface 2):** prompt-to-composition assistance that **emits Layer-2 composition validated through the same path as preset import (4.6)** — never opaque markup. Depends on the `ai` capability (3.6) and the builder existing; ships only as an enhancement once the builder is real.
- **4.9 — In-builder media picker:** a rich media picker inside the builder — browse/search the media library, drop assets onto the canvas and into block fields, and (if the `stock-media` capability is installed) search and import stock media inline. A client of the Media API and the `stock-media` capability; extends the Phase-1 library into the builder.

**Definition of done:** a non-technical user composes a page visually; the output is identical to what the API/theme produces; the builder uses only the public contract. Up-market go-to-market motion (agencies, designers, non-technical owners) becomes possible.

### Phase 5 — Application Composition (Far Future, Gated)

**Goal:** Layer-3 (event chains, side effects, workflows) — the Retool/Bubble-class capability. Kept private and gated until Layers 1–2 are mature and externally validated, precisely so its order-of-magnitude complexity never leaks downward.

**Vertical slices (indicative only — not committed):**
- **5.1 — Workflow contract (`application-composition/v0`):** declarative event→action chains.
- **5.2 — Action registry:** capabilities register actions (create lead, notify, charge, render, **invoke AI agent**). AI agent workflows are not a separate engine — they are Layer-3 workflows where one action type invokes an agent (§14.1, Surface 3); they inherit Layer-3's full deferral.
- **5.3 — Workflow execution engine:** durable, observable, with compensation/rollback.

**Definition of done:** deferred until Phases 1–4 are in production and demand is proven by external users — not by internal design ambition.

### Phase Sequencing Rationale

```
Phase 0  Walking skeleton        — prove the spine
Phase 1  Headless content        — THE WEDGE; sellable to developers; content is root
Phase 2  Extension system        — become a platform; prove the public API by dogfooding
Phase 3  First-party capabilities— prove ecosystem economics; commerce/membership/marketplace
Phase 4  Layout + builder        — move up-market; builder as client, built last
Phase 5  Application composition — far future, gated, demand-driven
```

Each phase is independently deployable and sellable. If Phase 1 finds no external developers who build something unanticipated, no later phase will rescue it — that signal is the gate, and it is cheaper to learn it at Phase 1 than at Phase 4.

AI threads through as an *enhancement* without reordering the sequence: the `ai` capability is an optional Phase-3 slice (Surface 1), AI authoring is an optional Phase-4 slice on top of the builder (Surface 2), and AI agent workflows are Layer-3 and therefore Phase-5/gated (Surface 3). AI never jumps the queue (§14).

---

## 17. Production Readiness

Every vertical slice is "done" only when it is production-ready by the criteria below. Production readiness is not a final phase; it is a per-slice gate.

### 17.1 Per-Slice Definition of Done

```
□ Functionality complete and demoable end-to-end (vertical, not horizontal)
□ Automated tests: unit + integration covering the slice's domain-API boundary
□ Contract version respected; backward compatibility verified for the supported range
□ Security review: capability scoping enforced at the boundary; no raw-resource leakage
□ Observability: structured logs, request IDs, key metrics emitted
□ Performance within the §15.1 targets for the relevant operation
□ Documentation: API reference + at least one external-facing usage example
□ Migration: idempotent, reversible where feasible, with a schema baseline update
□ Deployable: builds into the single binary; runs in the default SQLite config
□ CI green: cross-platform build, lint, test, and the `boundary-verify` check
```

### 17.2 The `boundary-verify` Check (CI Gate)

A required CI check, analogous to a strict linter, that enforces the architectural invariants statically:
- No plugin/theme code imports kernel internals (only `pkg/sdk` and `pkg/contract`).
- No domain-API implementation exposes raw DB/FS handles across the boundary.
- No theme code path can reach a mutation API.
- Every capability used is declared in a manifest.

This check is what makes license separation, safe upgrades, and the trust model *real rather than documented*. It is the mechanism, not the promise.

### 17.3 Release Engineering

- Semantic versioning for the core; documented contract-version support range per release.
- Signed release binaries; reproducible builds where feasible.
- Cross-platform CI matrix (Linux x86_64/arm64, macOS, Windows).
- Marketplace packages signed and compatibility-checked at publish and install.

### 17.4 Operational Readiness

- Single-binary deploy with documented VPS and container paths.
- Health/readiness endpoints; graceful shutdown with request draining.
- Backup guidance for the SQLite file and Postgres; restore drills documented.
- Upgrade path: in-place binary swap with forward migrations; rollback guidance.

---

## 18. Licensing and Open-Core Strategy

### 18.1 Core License

The Glyphux core is **Apache-2.0**. Rationale: maximize adoption and enterprise comfort, avoid derivative-work disputes over plugins, and place no copyleft obligation on the ecosystem. Under Apache-2.0 the license stops forcing the extension architecture's hand — proprietary plugins are clean regardless of tier, and the RPC/WASM boundary provides the work-separation independently of license.

> **AGPL note:** If, post-launch, cloud strip-mining (a third party reselling Glyphux as a managed SaaS without contribution) becomes a real threat, an AGPL-core posture is the targeted remedy — it would protect against SaaS free-riding while leaving the plugin/theme business intact, given the clean extension boundary. This is recorded as a future option, not a v1 decision.

### 18.2 Ecosystem Monetization

Monetization lives entirely at the ecosystem layer: paid/commercial/proprietary themes and plugins via the marketplace, and first-party premium capabilities (commerce, membership, marketplace) sold through the same channel. The manifest `license` field makes posture machine-readable; the runtime boundary makes proprietary extensions a clean separate work.

### 18.3 Standalone Product; Shared Infrastructure Is Extracted, Not Designed Up-Front

Glyphux is a **standalone product** with no dependency on any other codebase. Nothing in this document requires another product, repository, library, or build to exist. The dev team can implement Glyphux end-to-end with no external project context.

If Glyphux's owners maintain other systems that happen to share architectural patterns (composition/graph contracts, capability resolution, manifest/semver/signing, plugin lifecycle), those patterns are reused as *ideas implemented in parallel*, never as a shared dependency. A shared internal library is an **extraction, not a foundation**: a piece earns its way into shared code only when two systems independently require the *same implementation with the same semantics*, proven by the pain of maintaining it twice — never predicted by elegance up front. The genuinely neutral, extractable candidates are pure algorithms (DAG primitives, semver solving, manifest validation, dependency-resolution math), **not** behavior (capability *enforcement*, permission *enforcement*, node *semantics*), which differ by trust model and timing between systems — Glyphux's enforcement is runtime and adversarial. Share math, not behavior, and only after duplication proves it. Until then, Glyphux depends on nothing but itself.

---

## 19. Success Metrics and Validation

### 19.1 The Platform Test (The Only One That Matters)

> Internal consistency is an engineering test. Whether a stranger can build something the designers did not imagine is the platform test. Only the second is commercial validation.

The five-domain internal exercise (can the platform express the founder's own application designs) proves only that the architecture is self-consistent — valuable, but **not** market validation. External validation must come from users who were not involved in the design.

The platform is validated at the moment someone builds something that makes the team say: *"I didn't know the platform could do that."* That is when ownership of the architecture transfers from the team to the community.

### 19.2 Phase-Gated Metrics

```
Phase 1 (headless):   # of external developers who built something unanticipated
                      # of broken assumptions converted to docs/bugs/abstractions
                      (NOT downloads, NOT stars, NOT social metrics)

Phase 2 (platform):   # of third-party plugins published by people outside the team
                      first-party forms capability shipped through the public API

Phase 3 (ecosystem):  first paid plugin sold/installed through the marketplace
                      commerce + membership live as first-party-via-public-API

Phase 4 (builder):    # of non-technical users who composed and shipped a page
                      builder uses only the public contract (verified by boundary-verify)
```

### 19.3 The Founder Discipline Note

The dominant risk is not picking the wrong architecture; it is building yet another beautiful system instead of shipping the rough first slice and handing it to a stranger. The next meaningful design decisions come from code and users, not from another diagram. Avoid context-switching across parallel projects; ship the ugly headless slice of Glyphux first; then find the one external developer who will use it wrong.

---

## 20. Architectural Decision Records

### ADR-001 — Go as the core language
**Status:** Accepted.
**Decision:** The Glyphux core is implemented in Go.
**Rationale:** single static cross-platform binary with no runtime dependency (the "host anywhere" promise); pure-Go WASM host (Wazero) preserves the single-binary story; strong stdlib for HTTP/templates/testing; fast builds; good concurrency for a live runtime.
**Rejected:** PHP (WordPress's coupling and security model is the thing being escaped); Node (runtime dependency, weaker single-binary story); Rust (disproportionate complexity for this workload, though used for WASM plugin authoring).

### ADR-002 — The composition contract is the architectural center
**Status:** Accepted.
**Decision:** A typed, versioned composition contract is the single source of truth; every interface is a client of it.
**Rationale:** it makes the platform's hard questions (what renders, what plugins do, what the builder edits) answerable by one principle, and forces headless-first, theme-as-renderer, and builder-as-client discipline.

### ADR-003 — Headless-first; the visual builder is built last
**Status:** Accepted.
**Decision:** Ship a complete headless content core first; build the visual builder only after the contract is externally validated.
**Rationale:** the builder is the hardest single piece of software in the vision; building it first would block the entire launch and would be designed in ignorance of how the contract is really used. Headless-first yields a sellable product in months and a clean rendering contract.

### ADR-004 — Apache-2.0 core, commercial ecosystem
**Status:** Accepted.
**Decision:** Apache-2.0 for the core; monetization at the ecosystem layer.
**Rationale:** maximizes adoption, avoids derivative-work disputes, leaves proprietary plugins clean. AGPL retained as a targeted future remedy against SaaS strip-mining only if it materializes.

### ADR-005 — Three plugin tiers (in-process / WASM / RPC), one contract
**Status:** Accepted.
**Decision:** First-party kernel = in-process Go; small untrusted third-party = WASM (Wazero); heavy third-party = RPC (gRPC). All speak one unified extension contract.
**Rationale:** plugin population spans light-numerous-untrusted-latency-sensitive (WASM's strength: deny-by-default sandbox, language-agnostic, in-process speed, single-binary preserved) to heavy-few-trusted-system-level (RPC's strength: own deps, network, isolation, e.g. PCI scope). The RPC-vs-WASM question is resolved by routing per plugin profile, not choosing one.

### ADR-006 — Plugins interact only through scoped domain APIs; never raw resources
**Status:** Accepted.
**Decision:** No third-party plugin gets raw DB/FS/network; persistence is a scoped namespace; network is allowlisted. Enforced at the boundary and by `boundary-verify` in CI.
**Rationale:** raw access (WordPress's `$wpdb`) bypasses every capability boundary and makes the schema unevolvable. This invariant is what makes safe upgrades and the trust model real.

### ADR-007 — Themes render composition; they never mutate it
**Status:** Accepted.
**Decision:** Themes receive a read-only composition view and emit output; no persistence, no mutation, no helpers; changes via events only.
**Rationale:** the theme analog of ADR-006; keeps presentation free of business logic and keeps composition owned by the contract, not the renderer.

### ADR-008 — Adversarial trust model with three-point consent
**Status:** Accepted.
**Decision:** Assume untrusted plugin authors; enforce via marketplace review (publish), install-time consent (install), and runtime boundary enforcement (call).
**Rationale:** a public marketplace inverts the trusted-author assumption; this cannot be added later because it changes what the kernel may assume. Install-time consent is the trust surface WordPress lacked.

### ADR-009 — SQLite default, Postgres for scale
**Status:** Accepted.
**Decision:** Embedded SQLite is the default; Postgres is configuration-selected for scale; neither is reachable by plugins/clients.
**Rationale:** SQLite delivers the single-binary, host-anywhere, $5-VPS promise; Postgres covers multi-instance scale; the abstraction keeps the schema evolvable.

### ADR-010 — Standalone product; shared infrastructure is extracted from proven duplication, never designed as a foundation
**Status:** Accepted.
**Decision:** Build Glyphux as a fully standalone product with no dependency on any other codebase. Where architectural patterns are shared with other systems the owners maintain, reuse them as ideas implemented in parallel; extract a shared library only when two systems independently prove they need the same implementation with the same semantics.
**Rationale:** a hand-off spec must be self-contained — the dev team implements Glyphux with no external project context. Coupling two pre-PMF systems couples their failure and roadmaps; premature shared abstractions encode one system's assumptions into the other. Extract math (DAG, semver, manifest validation, dependency resolution), never behavior (enforcement, semantics), and only after duplication reveals the true shared shape. Glyphux depends on nothing but itself.

### ADR-011 — Web-based first-run wizard over per-OS native installers; CLI is secondary
**Status:** Accepted.
**Decision:** The primary install/first-run experience is a browser-based setup wizard served by the `glyphuxd` daemon itself, written once and reused across local, cloud/server, and (future) managed deployments. The `glyphux` CLI is a secondary, optional developer surface, not the install path. Per-OS native wrappers and one-click deploy templates are thin shells around the binary and are post-V1.
**Rationale:** Glyphux's users are not all terminal-native (agencies, freelancers, and eventually non-technical owners), so a terminal-first install (the dev-CLI model) is wrong for this product. Serving the wizard from the daemon collapses local and cloud hosting into one codebase, because a headless server cannot pop a native window but can serve HTTP. The wizard is itself a client of the composition contract (Principle 2) — it writes the initial composition — so it introduces no new privileged surface. The pattern is proven (WordPress `install.php` + `wp-cli`; Ghost hosted/one-click + `ghost` CLI).
**Rejected:** terminal-only CLI install (excludes non-developer users; wrong front door); per-OS native installers as the primary path (triples the surface, fragments setup logic, and still can't serve the cloud/headless scenario).
**Scope guard:** the wizard is built minimally in Phase 0 (admin + DB + boot) and grown utilitarian in Phase 1; it is explicitly *not* the up-market visual builder (Phase 4). A polished wizard in front of an empty platform sells nothing.

### ADR-012 — Admin/builder UI is a React SPA embedded in the binary; end-product frontend stays agnostic
**Status:** Accepted.
**Decision:** Glyphux's own admin shell, wizard, and visual builder are one application built in React + TypeScript + Vite + Tailwind, compiled to static assets and embedded into the binary via `go:embed`, consuming the core only through the public JS SDK. The frontend of products users *build* with Glyphux remains entirely framework-agnostic.
**Rationale:** the decision is driven by the visual builder (Phase 4), which is load-bearing, not optional. The builder is a nested, multi-target, rule-based DnD canvas whose mature ecosystem — dnd-kit plus the page-builder frameworks Craft.js and Puck — is React-only; Vue's `vuedraggable`/SortableJS suffices for list reordering (and for the admin shell alone) but not for the canvas, and Vue has no Craft.js/Puck equivalent. The admin shell and builder must share one stack (the builder is the admin grown up), so the builder's needs decide the shared stack. `go:embed` preserves the single-binary promise with no production Node runtime. The admin is a contract client (Principle 2), not a privileged surface, so choosing React does not make Glyphux "a React platform" and does not touch the user's frontend choice.
**Rejected:** Nuxt/Vue (DX-nicer for the admin alone, but its strengths are public-site SSR — the theme renderer's job, not the admin's — and it lacks the builder ecosystem; would force reimplementing dnd-kit + Craft.js/Puck or maintaining two stacks); framework-agnostic admin (would mean building the admin/builder more than once).
**Caveat recorded:** this holds *because* the builder is core. If the builder were ever dropped and Glyphux stayed headless-plus-list-shaped-admin, Vue/Nuxt + vuedraggable would be a defensible, DX-nicer choice and the decision would reopen.

### ADR-013 — Identity, security, and pooling are kernel concerns; notifications and tenancy are capabilities
**Status:** Accepted.
**Decision:** Identity (accounts, sessions, OAuth, MFA), security primitives (CSRF/CORS, rate limiting, secrets, sanitization), and database connection pooling/streaming are first-class kernel/adapter concerns, compiled in and never exposed as raw resources. Notifications is a first-party capability (depends on events + mailer). Multi-tenancy is a planned capability, designed-for but deferred past V1.
**Rationale:** identity, security, and pooling must be universal and trusted — every capability depends on them and none should be optional or pluggable in a way that weakens them; they belong below the domain-API boundary. Notifications is composed per-deployment (not every install needs it) and several capabilities depend on it, so it fits the capability graph. Tenancy is the most invasive possible data-model concern; building it speculatively is waste and retrofitting is painful, so the data model, identity, permission engine, and pooling carry no-op seams that let a `tenancy` capability activate later without redesign — but it is not built until a real tenant demands it, keeping its complexity out of the foundational phases.
**Rejected:** identity/notifications as a single bundled concern (different trust and optionality); tenancy from V1 (invasive, speculative, taxes every foundational phase before there is a tenant); tenancy ignored entirely (retrofit without seams is a redesign).

### ADR-014 — The builder is first-party and produces portable composition; blocks/presets/bundles are the distributable artifacts
**Status:** Accepted.
**Decision:** There is one first-party builder (Phase 4) whose output is typed Layer-2 Layout Composition, not builder-proprietary markup. The distributable builder artifacts are Blocks (plugin-registered widgets), Composition Presets (template/page fragments), Composition Bundles (starter sites), and Themes (renderers) — all distributed through the one marketplace, free or paid. A preset↔theme↔block compatibility contract validates imports at publish and import time.
**Rationale:** WordPress fuses builder-engine, widgets, templates, and theme, which causes its lock-in (content trapped in a third-party builder; opaque markup; competing builders) — the swamp Glyphux exists to avoid. Making the builder first-party and its output portable composition means content is never trapped in the builder, themes can be swapped without rebuilding layout (ADR-007), and "import a demo site" is a validatable composition merge rather than silent breakage. The four-artifact split is the Rosetta stone for the WordPress mental model and the structure the marketplace and sellers live in.
**Rejected:** builder-as-third-party-plugin (the WordPress model — reintroduces lock-in and competing-builder chaos); builder output as proprietary markup (unvalidatable, non-portable, theme-locked); fusing theme and builder/layout (prevents re-skinning without rebuilding).

### ADR-015 — AI is an enhancement consumed through existing slots; never a kernel concern or privileged surface
**Status:** Accepted.
**Decision:** AI enters Glyphux only as (1) a provider-agnostic `ai` capability for built products, (2) a builder authoring feature that emits validated composition, (3) a Layer-3 action type for agent workflows, and (4) optional dev/scaffolding tooling. It is never in the kernel, never a privileged surface, and never reorders the phase sequence. Agent workflows are Layer-3 Application Composition with an LLM action, inheriting Layer-3's Phase-5 deferral. AI providers are swappable adapters behind a stable contract.
**Rationale:** "add AI" is the most tempting place to break the layering (an LLM call in the kernel, a privileged AI surface bypassing capability scoping). Confining AI to existing slots — capability, builder client, action type — means AI plugins are sandboxed and scoped like any plugin, AI-generated composition is validated like any composition, and AI actions run in the Layer-3 engine like any action. Provider-agnostic adapters protect against fast-aging provider coupling. AI being an enhancement (the user's explicit framing) means it never jumps the queue ahead of the headless core, extension system, or builder.
**Rejected:** AI as a kernel concern (breaks layering, couples core to providers); AI agent workflows as an early flagship (sits on Layer-3 → builder → extension system → headless core, none of which exist early; starting the house at the roof); a privileged AI surface (bypasses the capability and boundary model that the whole platform depends on).

### ADR-016 — Paid extensions protected by signing + offline entitlement + update-gating; no source DRM, no phone-home, expiry never disables installed code
**Status:** Accepted.
**Decision:** Protect paid themes/plugins/presets/bundles by securing the commercial relationship, not the bytes: package signing (integrity/authenticity), offline-verifiable signed entitlement tokens (local verification, no phone-home), update-gating at download time (the primary lever, reinforced by the compatibility contract), and encrypted-at-rest secrets + encrypted authenticated delivery. Entitlement expiry stops new updates only; it never disables already-installed code. Source-encryption-as-DRM and phone-home license checks are both rejected.
**Rationale:** runtime decryption puts the key on the user's machine (obfuscation, not protection — see WordPress "nulled" history), and encrypting GPL-licensed extensions may violate their license; both contradict "host anywhere, you own it." Phone-home licensing reintroduces the cloud runtime dependency the platform exists to avoid (Principle 9, ADR-001) and can take down a paying customer's live site on a failed check. Offline signed tokens verify entitlement locally and work air-gapped; update-gating protects the thing buyers actually pay for (updates/support), and the compatibility contract (ADR-008) turns frozen pirated copies incompatible over time as a side effect of normal versioning rather than a DRM kill-switch. Graceful expiry (perpetual fallback) ensures licensing never interrupts production.
**Trade-off accepted:** offline tokens weaken revocation; mitigated by short token windows with online auto-renewal plus a revocation list on normal update checks. A determined offline abuser can ride one token to expiry — a bounded, deliberate loss preferred over a phone-home kill-switch. The goal is making piracy *irrelevant* (via convenience, fair pricing, trust, and update-gating), not impossible — validated by Laravel's commercial products, whose nulled copies demonstrably exist yet do not dent the business (§12.5, "Lessons from comparable models").
**Forward path:** the same signing + entitlement + trust-review machinery supports a future multi-vendor third-party seller marketplace (§12.6) without re-architecture.

### ADR-017 — Stock media is a provider-agnostic capability that imports into the user's own storage; the media library is a builder/admin client
**Status:** Accepted.
**Decision:** Royalty-free stock-media integration (Unsplash/Pixabay/Pexels) is a `stock-media` capability, provider-agnostic behind adapters, with allowlisted network, that **fetches and stores** imported assets in the user's own media library (with license/attribution metadata) rather than hot-linking. The media library/gallery is an admin/builder UI surface (Surface 2) and a client of the `Media()` API, not a kernel concern.
**Rationale:** stock providers are swappable adapters like AI/storage/payment providers, so they belong in the capability model, not the kernel; third parties can add providers as plugins. Fetch-and-store keeps sites working if a provider changes/rate-limits/removes an asset and honors provider API terms — hot-linking would reintroduce an external runtime dependency, contradicting the self-hostable "you own it" identity (Principle 9). The gallery is UI over the existing media pipeline, needing no new primitive.
**Rejected:** stock media in the kernel (wrong layer, couples core to providers); hot-linking to provider CDNs (external runtime dependency, breakage risk, against self-hosting promise); heavy in-browser image editing in V1 (a product unto itself — basic crop/resize only, deferred otherwise).

### ADR-018 — The admin UI is held to a concrete premium-design and configurability bar, designed in from Phase 1
**Status:** Accepted.
**Decision:** The admin/wizard/builder UI is held to a stated quality bar expressed as buildable commitments (design-token system, typography system, component library, deliberate motion, and a non-negotiable floor of responsiveness + keyboard focus + WCAG 2.1 AA + written empty/error states) and concrete configurability (token-based admin theming incl. light/dark and white-label accent/logo, density/layout preferences, configurable dashboards/list-views/navigation, and capability-extensible admin surfaces). The token/theming/quality foundation is established with the Phase-1 admin shell; configurability depth grows across phases.
**Rationale:** "premium" and "configurable" are unfalsifiable as adjectives and unbuildable as requirements; expressing them as tokens, an accessibility floor, and named configurable surfaces makes them implementable and reviewable. White-labelable admin theming serves the agency/freelancer buyer who hands the admin to clients. Designing the token system and quality floor in from Phase 1 avoids an expensive retrofit, since the Phase-4 builder extends the same UI.
**Rejected:** "make it premium" as the spec (unbuildable); retrofitting a design system after the admin exists (expensive rework); gold-plating configurability before the platform has content to configure (§6.6 — quality bar applies to whatever ships, in order, not everything at once).

### ADR-019 — Glyphux is the platform vertical apps are built on, not a vertical-application vendor
**Status:** Accepted.
**Decision:** Glyphux provides primitives and the platform (identity, roles, content, commerce, membership, media, admin, and — later — Application Composition for custom workflows) plus four deployable surfaces (public site, authenticated end-user dashboard, owner management panel, platform management). The domain-specific logic of any vertical (school/hospital/fitness/etc.) is supplied by the builder/freelancer — early via custom plugins/capabilities, later via Layer-3 composition. Glyphux ships no first-party vertical applications.
**Rationale:** the multi-surface reach is real and a genuine strength, but shipping vertical solutions would make Glyphux maintain domain logic across healthcare, education, retail, and fitness at once — four under-resourced vertical products instead of one platform (the over-unification trap at maximum scale). Platforms win as substrate (WordPress hosts a hospital's site; it is not a hospital system its maker ships). The structural parts of authenticated/transactional products (auth, roles, gated content, payments, capability-driven management) are deliverable in the V1–Phase-4 arc; the deep operational workflows are Layer-3/Phase-5 — neither is foreclosed, but neither becomes a first-party vertical product.
**Rejected:** first-party school/hospital/fitness systems (turns a platform into a fragmented multi-vertical vendor); pulling deep operational workflows forward into early phases (drags the gated Layer-3 into the foundation — the recurring trap); leaving the multi-surface possibility unstated (a real strength worth recording, provided the boundary is drawn).

### ADR-020 — Time-to-premium-result is a first-class objective; setup speed, wizard, and starter bundles are launch-critical
**Status:** Accepted.
**Decision:** Optimize for time-to-premium-result — the shortest credible path from `glyphuxd` running to a launched, premium, self-hosted site/app — as a first-class objective, without surrendering ownership or the platform underneath. This elevates single-binary fast setup, the graphical wizard (§6), first-party premium starter bundles/themes (§13), and the builder (Phase 4) from "nice" to launch-critical, and shapes prioritization accordingly.
**Rationale:** the differentiator is the *conjunction* (fast AND premium AND owned AND a real platform) that no incumbent offers — WordPress isn't fast/clean to set up, Webflow isn't owned, headless tools hand you the frontend. Making this an explicit objective (not a tagline) forces the deliverables that produce it to be sequenced as core. Honest constraint recorded: "quick to a premium site" is gated by the quality/breadth of starter bundles far more than by the engine, so first-party premium bundles are a launch-critical deliverable, and the objective is realistic only in proportion to bundle availability.
**Rejected:** treating "fast and premium" as marketing copy rather than a prioritization-shaping objective (lets the deliverables that produce it slip); pursuing speed by becoming a closed builder (sacrifices ownership/platform — the anti-positioning, §2.5); over-promising premium-site-in-minutes before the bundle ecosystem exists (sets a bar the catalog can't yet meet).

### ADR-021 — Semver is authoritative; codenames are human-facing decoration from 1.0, themed on historic typefaces
**Status:** Accepted.
**Decision:** Use two versioning systems simultaneously. Semantic versions are the authoritative contract for all machine-facing compatibility, dependency, and update logic — codenames never appear in manifests or compatibility fields. Human-facing codenames decorate (never replace) the version number on major releases, beginning at 1.0, themed on historic public-domain typefaces and punchcutters (Jenson, Garamond, Caslon, Baskerville, Bodoni, Didot…), with iconic names reserved for major milestones. Pre-1.0 and phase/preview builds use plain semver only.
**Rationale:** semver must remain authoritative because the whole compatibility and update-gating model (ADR-008, §12.5) depends on machine-comparable numbers; a codename cannot express ordering or compatibility (the mistake Android corrected by dropping dessert names at v10). A typeface theme is uniquely apt because a "glyph" is a letterform — the series is the brand's own meaning extended, not arbitrary decoration — and it flatters both developer and designer audiences while signaling the premium-craft objective. Historic public-domain names avoid trademark questions and carry more gravitas. Deferring codenames to the 1.0-era avoids adding ceremony to internal phase builds.
**Rejected:** codename as the primary/only identifier (breaks dependency resolution and ordering); naming pre-1.0/phase builds (ceremony over substance during the build); gemstone/mineral theme (generic-premium, says nothing specifically Glyphux) and writing-systems theme (pronunciation and cultural-sensitivity issues, shallower usable well) — both considered, both lose to typefaces on brand-fit.

---

## 21. Glossary

**Composition** — the typed, versioned relationship between content, layout, capabilities, and presentation; the single source of truth.

**`glyphuxd`** — the server daemon; the primary entrypoint that boots the platform, serves the first-run web wizard, and then serves the running product across all deployment scenarios.

**`glyphux` CLI** — the optional, secondary developer surface for scripting, automation, and CI; never the install path.

**Setup wizard** — the browser-based first-run flow served by the daemon; writes the initial composition; reused across local, cloud/server, and managed deployments.

**Identity** — the kernel concern covering accounts, sessions, credentials, OAuth/social providers, and MFA; foundational to permissions, membership, and commerce.

**Notifications** — a first-party capability for email / SMS / push / in-app messaging, built on the event bus and a mailer adapter.

**Tenancy** — a planned, deferred capability for multi-tenant isolation (schema- or row-scoped); designed-for via no-op seams in the data model, identity, permissions, and pooling, but not built in V1.

**Admin UI (Surface 2)** — Glyphux's own admin shell, wizard, and visual builder: one React + TypeScript + Vite + Tailwind SPA embedded in the binary via `go:embed`, consuming the core through the public JS SDK; a contract client, not a privileged surface.

**End-product frontend (Surface 1)** — the frontend a user builds with Glyphux (Next, Nuxt, Astro, Flutter, etc.); always framework-agnostic.

**Composition Contract** — the stable, versioned interface describing composition; every interface (API, CLI, SDK, theme, plugin, builder) is a client of it.

**Content Composition (Layer 1)** — content types, fields, relations, localization, validation, versioning, media. The foundational, independently shippable layer.

**Layout Composition (Layer 2)** — blocks, slots, regions, sections; the layer the visual builder edits.

**Application Composition (Layer 3)** — event chains, side effects, workflows; the low-code application layer; gated/far-future.

**Capability** — a declared, composable feature and unit of permission; carries a domain-surface axis and a resource-permission axis.

**Capability Scope** — the bounded grant within a capability (e.g. `payments:[refund]`), scopable from v1.

**Domain API** — a typed, versioned, scoped interface over the kernel; the only sanctioned way for clients/plugins to touch state.

**Plugin** — a sandboxed, capability-scoped extension; routed to a WASM (Tier B) or RPC (Tier C) runtime; first-party kernel modules are Tier-A in-process.

**Theme** — a renderer; receives a read-only composition view and emits output; never mutates state. Supplies styling, design tokens, and slot/region definitions; never owns layout or logic.

**Block** — a registered, composable layout unit (hero, grid, form, pricing table); first-party blocks ship in-tree, third-party blocks are registered by plugins; logic-bearing blocks run as sandboxed Tier-B WASM plugins. The Glyphux equivalent of a WordPress widget/module.

**Composition Preset** — a serialized fragment of Layer-2 composition (a section, page, or pattern) with placeholder content; imported by merging into a composition; portable across themes. The Glyphux equivalent of a builder template/layout.

**Composition Bundle** — a site-scale preset collection (pages + presets + sample content + theme reference); the Glyphux equivalent of a WordPress demo/starter-site import.

**Compatibility contract** — the Phase-4 rules governing how presets declare block/slot assumptions, how themes declare available slots, and how missing blocks are detected at import — enabling graceful degradation instead of silent breakage.

**`ai` capability** — a provider-agnostic AI feature (Claude/OpenAI/Ollama/local behind adapters) exposing scoped generate/embed/classify domain APIs; an enhancement, never a kernel concern or privileged surface.

**Media library** — the user-facing gallery (Surface 2) for uploading, organizing, searching, previewing, and selecting media; a client of the `Media()` API, with a basic manager in Phase 1 and an in-builder picker in Phase 4.

**`stock-media` capability** — provider-agnostic integration with royalty-free stock providers (Unsplash/Pixabay/Pexels) behind adapters; fetches and stores imports into the user's own library with attribution; never hot-links. An enhancement.

**Admin design system** — the token system (color, type, spacing, radius, elevation, motion), component library, and accessibility floor that define the admin UI's premium quality bar; also the basis for white-labelable admin theming (§5.7).

**Time-to-premium-result** — Glyphux's first-class objective: the shortest credible path from a running binary to a launched, premium, self-hosted site/app, without surrendering ownership; delivered by fast setup, the wizard, and premium starter bundles (§2.5).

**Four surfaces** — the public site, authenticated end-user dashboard, owner management panel, and platform-management admin that one Glyphux deployment can serve (§2.6); structural parts deliverable early, deep operational workflows deferred to Layer 3.

**Vertical-application boundary** — Glyphux provides primitives and the platform; builders supply domain-specific logic. Glyphux ships no first-party vertical applications (school/hospital/fitness systems); it is the substrate they are built on (ADR-019).

**Release codename** — a human-facing name decorating a major release's semantic version from 1.0 onward (themed on historic typefaces: Jenson, Garamond, Caslon…); marketing/memorability only, never a dependency identifier — semver remains authoritative (§12.7, ADR-021).

**Entitlement token** — a cryptographically signed artifact proving a buyer's right to an extension (license ID, entitled extensions, validity window, scope); verified locally and offline by the host, with no phone-home.

**Update-gating** — the primary paid-extension protection: updates are delivered only to valid entitlements at download time; installed code keeps running on expiry (perpetual fallback), and the compatibility contract eventually renders un-renewed copies incompatible.

**HostAPI** — the only surface a plugin can reach; capability-gated so undeclared capabilities are not present.

**boundary-verify** — the CI check enforcing the architectural invariants statically.

**Vertical slice** — a unit of work cutting through every layer to deliver one end-to-end deployable capability.

---

## 22. Constraints and Non-Goals

**Constraints:**
- Single static Go binary; default config requires no external services.
- The composition contract is the only path from client to kernel state.
- Third-party plugins never receive raw DB/FS/unrestricted-network.
- Themes never mutate state.
- First-party capabilities use only the public extension API.
- The builder is first-party and emits portable Layer-2 composition, never proprietary markup.
- AI is consumed only through existing slots (capability / builder client / Layer-3 action), never the kernel, never a privileged surface, and is provider-agnostic behind adapters.
- Paid extensions are protected by signing + offline entitlement + update-gating, never by source encryption or phone-home; entitlement expiry never disables installed code.

**Non-Goals (V1):**
- Not a managed cloud product; self-hosting is the primary mode.
- Not a visual builder at launch (Phase 4, after external validation).
- Not multi-tenant at launch (data model stays forward-compatible).
- Not a multi-vendor third-party seller marketplace at launch — the marketplace starts first-party/curated and grows into a CodeCanyon-style seller platform later (§12.6), on the same machinery.
- Not an Application-Composition / low-code workflow platform at launch (Phase 5, gated).
- Not an AI-agent-workflow product at launch — agent workflows are Layer-3 and inherit its Phase-5 deferral; AI is an enhancement, not a pillar.
- Not dependent on any external codebase or sister project; the spec is fully self-contained.
- Not a single-vertical product (commerce, membership, marketplace are composed capabilities, not the product).

---

*End of Document*

*Glyphux — The Composable Application Platform*
*PRD v1.0.0 — Open Core (Apache-2.0) + Commercial Ecosystem*
