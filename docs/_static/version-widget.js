(() => {
  const panel = document.createElement("aside");
  panel.className = "docs-version-panel";
  panel.setAttribute("aria-label", "Documentation versions and links");
  panel.innerHTML = [
    '<div class="docs-version-panel__heading"><span aria-hidden="true">▤</span><strong>Documentation</strong></div>',
    '<section aria-labelledby="docs-versions-title"><h2 id="docs-versions-title">Versions</h2><a href="/" aria-current="page">latest</a><a href="/">v0.1.0</a></section>',
    '<section aria-labelledby="docs-project-title"><h2 id="docs-project-title">Project</h2><a href="https://github.com/aice09/goweb-template">Project Home</a><a href="https://vercel.com/carl-angelo-nievarezs-projects/docs">Builds</a></section>',
    '<form class="docs-version-panel__search" action="/search.html" method="get"><label for="docs-version-search">Search docs</label><input id="docs-version-search" name="q" type="search" placeholder="Search docs"></form>'
  ].join("");
  document.body.appendChild(panel);
})();