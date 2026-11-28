#!/usr/bin/env python3
"""Check the built site against the project's rules.

The same rules as the ASCII tree project, adapted for a website:
  - one self-contained page: exactly one inline <script> and one inline <style>,
    no external scripts, stylesheets, fonts, images or imports;
  - no network requests from page code (no fetch, XHR, WebSocket, sendBeacon, EventSource);
  - only characters on screen: no arc(), radial gradients or shadowBlur;
  - clock-driven animation: no setTimeout or setInterval;
  - no // comment after code on the same line;
  - outbound links only to the https URLs listed in content/site.json.
Exits 1 and prints every problem when a rule is broken.
"""

import json
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

BANNED_CODE = [
    (r"\.arc\s*\(", "painted arc"),
    (r"createRadialGradient", "radial gradient"),
    (r"shadowBlur", "shadowBlur glow"),
    (r"\bsetTimeout\s*\(", "setTimeout (use the clock)"),
    (r"\bsetInterval\s*\(", "setInterval (use the clock)"),
    (r"\bfetch\s*\(", "network request (fetch)"),
    (r"XMLHttpRequest", "network request (XHR)"),
    (r"\bWebSocket\b", "network request (WebSocket)"),
    (r"sendBeacon", "network request (sendBeacon)"),
    (r"\bEventSource\b", "network request (EventSource)"),
    (r"\beval\s*\(", "eval"),
    (r"new\s+Function\s*\(", "new Function"),
    (r"\.innerHTML\s*=", "innerHTML assignment"),
]


def check_page(path, allowed_urls, problems):
    with open(path, "r", encoding="utf-8") as f:
        page = f.read()
    rel = os.path.relpath(path, ROOT)
    scripts = re.findall(
        r"<script\b([^>]*)>(.*?)</script>", page, flags=re.DOTALL | re.IGNORECASE
    )
    styles = re.findall(
        r"<style\b[^>]*>(.*?)</style>", page, flags=re.DOTALL | re.IGNORECASE
    )
    if len(scripts) != 1:
        problems.append(f"{rel}: expected exactly one <script>, found {len(scripts)}")
    if len(styles) != 1:
        problems.append(f"{rel}: expected exactly one <style>, found {len(styles)}")
    for attrs, _ in scripts:
        if "src" in attrs.lower():
            problems.append(f"{rel}: external <script src> is not allowed")
    if re.search(r"<link\b[^>]*rel=[\"']?stylesheet", page, re.IGNORECASE):
        problems.append(f"{rel}: external stylesheet")
    if re.search(
        r"<(img|iframe|object|embed|video|audio|source)\b", page, re.IGNORECASE
    ):
        problems.append(f"{rel}: embedded media element")
    if re.search(r"@import|@font-face|url\(", "".join(styles), re.IGNORECASE):
        problems.append(f"{rel}: CSS import, font or url()")
    if re.search(
        r"\son[a-z]+\s*=",
        re.sub(r"<script\b.*?</script>", "", page, flags=re.DOTALL | re.IGNORECASE),
        re.IGNORECASE,
    ):
        problems.append(f"{rel}: inline event handler attribute (breaks the CSP)")
    if re.search(
        r"\sstyle\s*=",
        re.sub(r"<script\b.*?</script>", "", page, flags=re.DOTALL | re.IGNORECASE),
        re.IGNORECASE,
    ):
        problems.append(f"{rel}: inline style attribute (breaks the CSP)")

    code = scripts[0][1] if scripts else ""
    for pat, what in BANNED_CODE:
        for m in re.finditer(pat, code):
            line = code.count("\n", 0, m.start()) + 1
            problems.append(f"{rel}: {what} at script line {line}")

    for m in re.finditer(r"https?://[^\s\"'<>)\\]+", page):
        url = m.group(0)
        if url.startswith("http://"):
            problems.append(f"{rel}: plain http URL {url}")
        elif not any(url.startswith(a) for a in allowed_urls):
            problems.append(f"{rel}: URL not listed in content/site.json: {url}")
    return code


def check_source_comments(path, problems):
    with open(path, "r", encoding="utf-8") as f:
        for n, line in enumerate(f, 1):
            stripped = line.strip()
            if stripped.startswith("//") or "//" not in stripped:
                continue
            body = re.sub(
                r"'(?:\\.|[^'\\])*'|\"(?:\\.|[^\"\\])*\"|/(?![*/])(?:\\.|[^/\\\n])+/[gimsuy]*",
                "",
                stripped,
            )
            if "//" in body:
                problems.append(
                    f"{os.path.relpath(path, ROOT)}:{n}: // comment after code"
                )


def main():
    problems = []
    with open(os.path.join(ROOT, "content", "site.json"), "r", encoding="utf-8") as f:
        content = json.load(f)
    allowed = [l["url"] for l in content.get("links", [])]
    dist = os.path.join(ROOT, "dist")
    index = os.path.join(dist, "index.html")
    if not os.path.isfile(index):
        print("lint: dist/index.html not found - run the build first")
        return 1
    check_page(index, allowed, problems)
    lab = os.path.join(dist, "lab")
    if os.path.isdir(lab):
        for name in sorted(os.listdir(lab)):
            if name.endswith(".html"):
                check_page(os.path.join(lab, name), allowed, problems)
    check_source_comments(os.path.join(ROOT, "web", "app.js"), problems)
    size = os.path.getsize(index)
    if size > 400 * 1024:
        problems.append(
            "dist/index.html is %.0f KB; budget is 400 KB" % (size / 1024.0)
        )
    if problems:
        print(f"lint: {len(problems)} problem(s)")
        for p in problems:
            print("  - " + p)
        return 1
    scope = "index + lab" if os.path.isdir(lab) else "index"
    print(f"lint: ok ({size // 1024} KB, {scope})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
