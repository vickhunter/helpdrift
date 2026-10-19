#!/usr/bin/env python3
"""Build the static docs site in public/ from Markdown files in docs/."""

from __future__ import annotations

import html
import pathlib
import re

ROOT = pathlib.Path(__file__).resolve().parents[1]
DOCS = ROOT / "docs"
PUBLIC = ROOT / "public"

PAGE = """<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{title}</title>
<link rel="stylesheet" href="{css}">
</head>
<body>
<header>
<p class="mark"><a href="{home}">helpdrift</a></p>
<nav>{nav}</nav>
</header>
<main>
{body}
</main>
</body>
</html>
"""


def rewrite_url(url: str) -> str:
    path, _, frag = url.partition("#")
    if path.endswith(".md"):
        path = path[:-3] + ".html"
    if frag:
        return path + "#" + frag
    return path


def inline(text: str) -> str:
    escaped = html.escape(text)
    escaped = re.sub(
        r"`([^`]+)`",
        lambda m: "<code>" + m.group(1) + "</code>",
        escaped,
    )
    escaped = re.sub(
        r"\[([^\]]+)\]\(([^)\s]+)\)",
        lambda m: '<a href="' + html.escape(rewrite_url(m.group(2)), quote=True) + '">' + m.group(1) + "</a>",
        escaped,
    )
    escaped = re.sub(r"\*\*([^*]+)\*\*", r"<strong>\1</strong>", escaped)
    return escaped


def markdown_to_html(src: str) -> str:
    lines = src.replace("\r\n", "\n").split("\n")
    out: list[str] = []
    i = 0
    while i < len(lines):
        line = lines[i]
        if line.startswith("```"):
            fence = []
            i += 1
            while i < len(lines) and not lines[i].startswith("```"):
                fence.append(lines[i])
                i += 1
            i += 1
            out.append("<pre><code>" + html.escape("\n".join(fence)) + "</code></pre>")
            continue
        if line.startswith("#"):
            level = len(line) - len(line.lstrip("#"))
            level = min(level, 6)
            out.append(f"<h{level}>{inline(line[level:].strip())}</h{level}>")
            i += 1
            continue
        stripped = line.strip()
        if stripped.startswith("<!--") and stripped.endswith("-->"):
            i += 1
            continue
        if line.startswith("|"):
            rows = []
            while i < len(lines) and lines[i].startswith("|"):
                rows.append(lines[i])
                i += 1
            out.append(table_html(rows))
            continue
        if line.startswith("- "):
            items = []
            while i < len(lines) and lines[i].startswith("- "):
                items.append("<li>" + inline(lines[i][2:].strip()) + "</li>")
                i += 1
            out.append("<ul>" + "".join(items) + "</ul>")
            continue
        if not line.strip():
            i += 1
            continue
        para = [line.strip()]
        i += 1
        while i < len(lines) and lines[i].strip() and not lines[i].startswith(("#", "-", "```")):
            para.append(lines[i].strip())
            i += 1
        out.append("<p>" + inline(" ".join(para)) + "</p>")
    return "\n".join(out)


def table_html(rows: list[str]) -> str:
    parsed = []
    for row in rows:
        cells = [c.strip() for c in row.strip().strip("|").split("|")]
        if cells and all(set(c) <= set("-: ") and c for c in cells):
            continue
        parsed.append(cells)
    if not parsed:
        return ""
    head = "<tr>" + "".join("<th>" + inline(c) + "</th>" for c in parsed[0]) + "</tr>"
    body = []
    for cells in parsed[1:]:
        body.append("<tr>" + "".join("<td>" + inline(c) + "</td>" for c in cells) + "</tr>")
    return "<table><thead>" + head + "</thead><tbody>" + "".join(body) + "</tbody></table>"


def first_heading(src: str, fallback: str) -> str:
    for line in src.splitlines():
        if line.startswith("# "):
            return line[2:].strip()
    return fallback


def main() -> None:
    files = sorted(p for p in DOCS.rglob("*.md") if p.is_file())
    if not files:
        raise SystemExit("docs/: no Markdown files")
    pages = []
    for path in files:
        rel = path.relative_to(DOCS)
        pages.append((rel, path.read_text(encoding="utf-8")))

    PUBLIC.mkdir(parents=True, exist_ok=True)
    css_src = DOCS / "site.css"
    if css_src.is_file():
        (PUBLIC / "site.css").write_text(css_src.read_text(encoding="utf-8"), encoding="utf-8")

    nav_items = []
    for rel, src in pages:
        href = rel.with_suffix(".html").as_posix()
        if href == "index.html":
            href = "index.html"
        label = "Home" if href == "index.html" else first_heading(src, rel.stem)
        nav_items.append((href, label))

    for rel, src in pages:
        href = rel.with_suffix(".html").as_posix()
        depth = len(rel.parts) - 1
        prefix = "../" * depth
        nav = " ".join(
            f'<a href="{prefix}{html.escape(link, quote=True)}">{html.escape(label)}</a>'
            for link, label in nav_items
        )
        title = first_heading(src, "helpdrift")
        body = markdown_to_html(src)
        page = PAGE.format(
            title=html.escape(title),
            css=prefix + "site.css",
            home=prefix + "index.html",
            nav=nav,
            body=body,
        )
        dest = PUBLIC / rel.with_suffix(".html")
        dest.parent.mkdir(parents=True, exist_ok=True)
        dest.write_text(page, encoding="utf-8")
        print("wrote", dest.relative_to(ROOT))


if __name__ == "__main__":
    main()
