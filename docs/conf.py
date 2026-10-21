from datetime import date

project = "goweb-template"
author = "goweb-template contributors"
release = "0.1.0"
year = date.today().year
copyright = f"{year}, {author}"

extensions = ["myst_parser"]
myst_enable_extensions = ["colon_fence", "deflist", "tasklist"]
source_suffix = {
    ".rst": "restructuredtext",
    ".md": "markdown",
}
root_doc = "index"
exclude_patterns = ["_build", "Thumbs.db", ".DS_Store"]

html_theme = "sphinx_rtd_theme"
html_title = "goweb-template documentation"
html_theme_options = {"navigation_depth": 3}
