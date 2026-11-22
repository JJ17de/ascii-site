#!/usr/bin/env python3
"""Screenshot the built site at a few viewports and scroll positions.

Usage: python tests/shoot.py [WIDTHxHEIGHT ...]
Needs Playwright (python -m pip install playwright && python -m playwright install chromium).
Screenshots go to tests/frames/. Fails on any console error or network request.
"""
import pathlib
import sys

from playwright.sync_api import sync_playwright

HERE = pathlib.Path(__file__).resolve().parent
PAGE = (HERE.parent / "dist" / "index.html").as_uri()
FRAMES = HERE / "frames"
FRAMES.mkdir(exist_ok=True)

INIT = """
window.__now = 0;
performance.now = () => window.__now;
const __cbs = [];
window.requestAnimationFrame = cb => { __cbs.push(cb); return __cbs.length; };
window.__step = (ms, n) => { for (let i = 0; i < (n || 1); i++) { window.__now += ms; const c = __cbs.splice(0); for (const f of c) f(window.__now); } };
"""

viewports = sys.argv[1:] or ["1440x900", "1080x1920", "390x844"]
problems = []
with sync_playwright() as p:
    browser = p.chromium.launch()
    for vp in viewports:
        w, h = (int(v) for v in vp.split("x"))
        page = browser.new_page(viewport={"width": w, "height": h})
        page.on("console", lambda m: problems.append(m.text) if m.type == "error" else None)
        page.on("pageerror", lambda e: problems.append(str(e)))
        page.on("request", lambda r: problems.append("request: " + r.url) if not r.url.startswith(("file:", "data:")) else None)
        page.add_init_script(INIT)
        page.goto(PAGE)
        page.evaluate("window.__step(1000/30, 15)")
        page.screenshot(path=str(FRAMES / ("%s_intro.png" % vp)))
        page.evaluate("window.__step(1000/30, 90)")
        page.screenshot(path=str(FRAMES / ("%s_hero.png" % vp)))
        for i, key in enumerate(["PageDown", "PageDown", "PageDown", "End"]):
            page.keyboard.press(key)
            page.evaluate("window.__step(1000/30, 45)")
            page.screenshot(path=str(FRAMES / ("%s_scroll%d.png" % (vp, i + 1))))
        page.close()
    browser.close()
if problems:
    print("problems:\n  " + "\n  ".join(problems))
    sys.exit(1)
print("ok: screenshots in", FRAMES)
