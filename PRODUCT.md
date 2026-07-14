# Product

## Register

product

## Users

Developers (initially the maintainer) managing feature flags for React Native apps. Context: mid-task, IDE open, dark environment; they came to flip a flag or check a rollout and want to leave in under a minute. Secondary audience: GitHub visitors evaluating the project as a portfolio piece — the dashboard screenshot is the project's storefront.

## Product Purpose

featherflags is a self-hosted feature flag service (Go API + Postgres + Redis) with three environments (development / staging / production). The dashboard is the admin surface: create projects and flags, toggle per environment, set percentage rollouts, edit targeting conditions, copy API keys. Success = a flag change lands in production in under 30 seconds with zero doubt about which environment was touched.

## Brand Personality

Precise, calm, trustworthy. A sober dev-tool in the Linear/Vercel lineage: dense but never cramped, dark-first, no ornament. The interface should feel like infrastructure — it disappears into the task.

## Anti-references

- SaaS marketing dashboards: hero metrics, gradient cards, illustration mascots.
- Over-rounded "friendly" admin templates (Bootstrap/AdminLTE feel).
- Terminal cosplay: fake scanlines, all-monospace body text.

## Design Principles

1. **Environment is the safety rail** — dev/staging/production must be unmistakable at every touchpoint; a production toggle should feel heavier than a development one.
2. **State over decoration** — color exists to communicate on/off, rollout %, and environment; never as garnish.
3. **Density with hierarchy** — many flags on screen at once, but the eye always knows the row, the environment, and the state it's about to change.
4. **Fail-visible** — API errors, stale data, and unsaved edits are surfaced inline, never swallowed.

## Accessibility & Inclusion

WCAG AA: body text ≥4.5:1 on the dark surface, toggle state never conveyed by color alone (position + label), full keyboard operability for toggles and sliders, `prefers-reduced-motion` respected.
