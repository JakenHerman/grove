/* ──────────────────────────────────────────────────────────────────
   grove · documentation chrome

   Each guide page ships a bare <article class="doc-content"
   data-doc-slug="..."> with the actual prose.  This script reads the
   slug, consults the manifest below, and injects everything else:
   the left sidebar, the right-hand "on this page" TOC, and the
   prev/next nav at the foot of the article.

   Keeping the content-as-HTML and the chrome-as-JS split means:
     • Every page is deep-linkable, crawlable, and readable without JS.
     • The sidebar and topbar live in exactly one place.
     • Adding a new guide page is: write an HTML file + add a line to
       MANIFEST below.
   ────────────────────────────────────────────────────────────────── */

(function () {
  "use strict";

  // ── Manifest: flat order drives prev/next; grouping drives the sidebar.
  //    `slug` must match the HTML file's `data-doc-slug`.
  //    `title` is what renders in the sidebar and prev/next.
  //    Keep this list in reading order.
  const MANIFEST = [
    {
      group: "Getting started",
      pages: [
        { slug: "index",    title: "Introduction",  href: "./index.html"  },
      ],
    },
    {
      group: "Modeling",
      pages: [
        { slug: "modeling", title: "Problems, variables, constraints", href: "./modeling.html" },
      ],
    },
    {
      group: "Solving",
      pages: [
        { slug: "solving",          title: "Solve and Result",      href: "./solving.html"          },
        { slug: "sensitivity",      title: "Sensitivity analysis",  href: "./sensitivity.html"      },
        { slug: "integer-variables",title: "Integer variables",     href: "./integer-variables.html"},
      ],
    },
    {
      group: "Integration",
      pages: [
        { slug: "file-io", title: "File I/O and solvers", href: "./file-io.html" },
      ],
    },
    {
      group: "Reference",
      pages: [
        { slug: "examples",      title: "Examples",      href: "./examples.html"      },
        { slug: "api-reference", title: "API reference", href: "./api-reference.html" },
        { slug: "roadmap",       title: "Roadmap",       href: "./roadmap.html"       },
      ],
    },
  ];

  function flatPages() {
    return MANIFEST.flatMap((g) => g.pages);
  }

  // ── Slugify a heading's text so it becomes a stable anchor.
  function slugify(text) {
    return text
      .toLowerCase()
      .replace(/[^\p{Letter}\p{Number}\s-]/gu, "")
      .trim()
      .replace(/\s+/g, "-");
  }

  // ── Sidebar (left rail) ─────────────────────────────────────────
  function buildSidebar(currentSlug) {
    const aside = document.querySelector("aside.sidebar");
    if (!aside) return;

    const frag = document.createDocumentFragment();

    MANIFEST.forEach((group) => {
      const gDiv = document.createElement("div");
      gDiv.className = "sidebar-group";

      const h = document.createElement("h4");
      h.className = "sidebar-group-title";
      h.textContent = group.group;
      gDiv.appendChild(h);

      const ul = document.createElement("ul");
      group.pages.forEach((p) => {
        const li = document.createElement("li");
        const a = document.createElement("a");
        a.href = p.href;
        a.textContent = p.title;
        if (p.slug === currentSlug) a.setAttribute("aria-current", "page");
        li.appendChild(a);
        ul.appendChild(li);
      });
      gDiv.appendChild(ul);
      frag.appendChild(gDiv);
    });

    aside.appendChild(frag);
  }

  // ── Right rail: "On this page" TOC, built from <h2>/<h3>.
  //    Also stamps an id and copy-link anchor onto every heading.
  function buildTOC(article) {
    const toc = document.querySelector("aside.toc");
    const headings = article.querySelectorAll("h2, h3");
    if (!toc || headings.length === 0) {
      if (toc) toc.remove();
      return [];
    }

    const title = document.createElement("div");
    title.className = "toc-title";
    title.textContent = "On this page";
    toc.appendChild(title);

    const ul = document.createElement("ul");
    const seen = new Set();
    const tocLinks = [];

    headings.forEach((h) => {
      // Derive / ensure id
      let id = h.id || slugify(h.textContent || "");
      if (!id) return;
      let base = id, n = 1;
      while (seen.has(id)) { id = base + "-" + (++n); }
      seen.add(id);
      h.id = id;

      // Heading anchor (the ¶ that appears on hover)
      const anchor = document.createElement("a");
      anchor.className = "heading-anchor";
      anchor.href = "#" + id;
      anchor.setAttribute("aria-label", "Direct link to " + (h.textContent || ""));
      anchor.textContent = "#";
      h.appendChild(document.createTextNode(" "));
      h.appendChild(anchor);

      // TOC entry
      const li = document.createElement("li");
      const a = document.createElement("a");
      a.href = "#" + id;
      // Heading text, excluding the appended "#"
      a.textContent = (h.firstChild && h.firstChild.nodeType === Node.TEXT_NODE)
        ? h.firstChild.nodeValue.trim()
        : (h.textContent || "").replace(/\s*#\s*$/, "").trim();
      a.className = h.tagName.toLowerCase(); // "h2" or "h3"
      li.appendChild(a);
      ul.appendChild(li);
      tocLinks.push({ id, a });
    });

    toc.appendChild(ul);
    return tocLinks;
  }

  // ── IntersectionObserver to highlight the active TOC entry.
  function wireScrollSpy(tocLinks, article) {
    if (tocLinks.length === 0 || !("IntersectionObserver" in window)) return;

    const map = new Map(tocLinks.map(({ id, a }) => [id, a]));
    let activeId = null;

    const setActive = (id) => {
      if (id === activeId) return;
      if (activeId) {
        const prev = map.get(activeId);
        if (prev) prev.classList.remove("active");
      }
      activeId = id;
      if (id) {
        const cur = map.get(id);
        if (cur) cur.classList.add("active");
      }
    };

    const observer = new IntersectionObserver((entries) => {
      // Pick the top-most intersecting heading.
      const visible = entries
        .filter((e) => e.isIntersecting)
        .map((e) => ({ id: e.target.id, top: e.boundingClientRect.top }))
        .sort((a, b) => a.top - b.top);
      if (visible.length > 0) setActive(visible[0].id);
    }, {
      // Fire when heading crosses the line ~15% from viewport top.
      rootMargin: "-15% 0px -72% 0px",
      threshold: 0,
    });

    article.querySelectorAll("h2, h3").forEach((h) => {
      if (h.id) observer.observe(h);
    });
  }

  // ── Prev / next navigation at the bottom.
  function buildPrevNext(currentSlug) {
    const nav = document.querySelector("nav.prev-next");
    if (!nav) return;

    const pages = flatPages();
    const idx = pages.findIndex((p) => p.slug === currentSlug);
    if (idx < 0) return;

    const prev = idx > 0 ? pages[idx - 1] : null;
    const next = idx < pages.length - 1 ? pages[idx + 1] : null;

    if (prev) {
      const a = document.createElement("a");
      a.className = "prev" + (next ? "" : " only-prev");
      a.href = prev.href;
      a.innerHTML = `<span class="label">← Previous</span><span class="title">${escapeHTML(prev.title)}</span>`;
      nav.appendChild(a);
    }
    if (next) {
      const a = document.createElement("a");
      a.className = "next" + (prev ? "" : " only-next");
      a.href = next.href;
      a.innerHTML = `<span class="label">Next →</span><span class="title">${escapeHTML(next.title)}</span>`;
      nav.appendChild(a);
    }
  }

  function escapeHTML(s) {
    return String(s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;")
      .replace(/'/g, "&#39;");
  }

  // ── Mobile hamburger drawer.
  function wireHamburger() {
    const btn = document.querySelector(".hamburger");
    const sidebar = document.querySelector("aside.sidebar");
    if (!btn || !sidebar) return;

    // Insert a scrim we can dim-out the page with.
    const scrim = document.createElement("div");
    scrim.className = "drawer-scrim";
    document.body.appendChild(scrim);

    const close = () => { sidebar.classList.remove("open"); scrim.classList.remove("open"); };
    const open  = () => { sidebar.classList.add("open");    scrim.classList.add("open");    };

    btn.addEventListener("click", () => {
      sidebar.classList.contains("open") ? close() : open();
    });
    scrim.addEventListener("click", close);
    // Also close when a sidebar link is tapped.
    sidebar.addEventListener("click", (e) => {
      if (e.target.tagName === "A") close();
    });
    // Escape key closes drawer.
    document.addEventListener("keydown", (e) => {
      if (e.key === "Escape") close();
    });
  }

  // ── Syntax highlighting (highlight.js is loaded from the CDN on
  //    each page; we just wait for it to be ready).
  function applyHighlighting() {
    if (typeof window.hljs === "undefined") return;
    document.querySelectorAll("pre code").forEach((block) => {
      try { window.hljs.highlightElement(block); } catch (_) { /* no-op */ }
    });
  }

  // ── Entrypoint
  function init() {
    const article = document.querySelector("article.doc-content");
    if (!article) return;
    const slug = article.dataset.docSlug;

    // Topbar brand: mark the "Docs" link as current.
    document
      .querySelectorAll('.topbar-links a[data-nav="docs"]')
      .forEach((a) => a.setAttribute("aria-current", "page"));

    buildSidebar(slug);
    const tocLinks = buildTOC(article);
    wireScrollSpy(tocLinks, article);
    buildPrevNext(slug);
    wireHamburger();
    applyHighlighting();
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
