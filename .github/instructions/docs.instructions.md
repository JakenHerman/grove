---
applyTo: "docs/**,README.md,ROADMAP.md"
---

# grove — documentation instructions

## The rule

Documentation must not be stale. Every PR that changes a public
surface updates every doc artifact that references that surface, in
the same PR. No exceptions. See the "What to update when you change
X" table in `AGENTS.md` for the exact mapping.

The four (and only four) doc artifacts are:

- `README.md`
- `ROADMAP.md`
- `docs/index.html`
- `docs/guide/*.html`

Do not create new floating `.md` files as a workaround. Design
notes belong in issue comments or a new guide page.

## Guide site (`docs/guide/`)

The guide is a hand-rolled static site served as-is by GitHub Pages
from `main:/docs`. **No build step. No bundler. No framework.** Do
not propose adding one.

Every guide page has this shape:

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <title>... · grove</title>
    <link rel="stylesheet" href="./assets/docs.css">
  </head>
  <body>
    <article class="doc-content" data-doc-slug="my-slug">
      <!-- prose, headings, code samples -->
    </article>
    <script src="./assets/docs.js"></script>
  </body>
</html>
```

`docs/guide/assets/docs.js` injects the sidebar, topbar, "on this
page" TOC, and prev/next nav from a single `MANIFEST` constant.

When adding or moving a page:

1. Create `docs/guide/<slug>.html` with the skeleton above.
2. Use `<h2>` for sections (they become TOC entries), `<h3>` for
   sub-sections, and `<pre><code class="language-go">` (or
   `language-bash`, etc.) for code samples.
3. Add or move the entry in `MANIFEST` in
   `docs/guide/assets/docs.js` at the correct reading-order position
   with a sensible `group`.
4. Add a row to the docs-pointer table in `README.md`.
5. Preview with `python3 -m http.server 8000 -d docs` and check the
   sidebar highlight, TOC, and prev/next on the new page.

**Do not** hand-edit sidebar, TOC, or prev/next markup on individual
pages. They are generated. Update the `MANIFEST` instead.

## README.md

- Short. It is a pointer into `docs/guide/`, not a second copy of
  the guide.
- Never duplicate marketing copy from `docs/index.html` into
  `README.md`. Landing-page messaging lives in `docs/index.html`
  only.
- If a release's "coming in v0.X" note in `README.md` has shipped,
  update or remove it.

## ROADMAP.md

- Source of truth for the release plan, cited by the landing page
  and `docs/guide/roadmap.html`.
- When an epic's checklist is fully ticked, flip the section header
  from `Target: Qx` to `Status: **shipped.**` and tick every
  sub-bullet.
- Speculative work with no issue yet belongs in the
  `## Beyond 1.0 — speculative` section.

## Landing page (`docs/index.html`)

- Owns marketing messaging for the project.
- Do not add a build step, framework, or `package.json` here.

## Sample outputs in docs

If a page shows solver output, sensitivity output, or LP/MPS text
that the code produces, and the code changes the format, regenerate
the sample in the same PR. Stale sample output is a stale-docs bug.
