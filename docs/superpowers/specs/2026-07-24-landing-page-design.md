# FeatherFlags Landing Page

## Purpose
A static marketing page for the FeatherFlags open-source project, published via GitHub Pages, to drive visitors to the GitHub repo.

## Location & Publishing
- Single file: `docs/index.html` in this repo, on `main`.
- No build step — self-contained HTML with inline `<style>`, no external dependencies (no CDN, no JS framework).
- GitHub Pages configured (manually, in repo Settings → Pages) to serve from `main` / `docs`.

## Visual style
Dev/terminal, dark mode:
- Near-black background, monospace font for code/headings accents, a terminal-green or amber accent color for links/buttons/prompts.
- Simple static syntax-highlight coloring inside code blocks (no JS highlighter — hand-colored spans).
- Mobile-first responsive: feature grid collapses to a single column below ~640px.

## Sections
1. **Hero** — "🪶 featherflags" title, tagline ("Lightweight, self-hosted feature flags for React Native / Expo apps"), two CTAs: "View on GitHub" (external link) and "Quick start" (anchor scroll).
2. **Features** — 4-card grid: 3 environments, deterministic percentage rollouts, attribute targeting, fail-safe by design. Content sourced from README.
3. **Quick start** — code block(s) mirroring the README's `docker compose up -d` + create-project/flag curl examples.
4. **Footer / final CTA** — GitHub link repeated + license mention.

No JavaScript beyond an optional native CSS/HTML smooth-scroll anchor (`scroll-behavior: smooth`) — no JS files.

## Out of scope
- Custom domain setup
- Analytics / tracking
- Multi-page site, blog, docs beyond quick start snippet
