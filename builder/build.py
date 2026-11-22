#!/usr/bin/env python3
"""Assemble the ASCII site into one self-contained HTML page.

Pipeline:
  content/site.json  --+
  bin/asciigen (C++) --+--> build/assets.json --+
  web/app.js         -----------------------------+--> dist/index.html
  web/template.html  -----------------------------+
  lab/*.html         ---------------------------------> dist/lab/

Standard library only; runs on Python 3.9+.
"""

import argparse
import datetime
import hashlib
import html
import json
import os
import shutil
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SECTIONS = ["about", "experience", "skills", "certifications", "education", "projects", "contact"]


class BuildError(Exception):
    pass


def load_json(path):
    try:
        with open(path, "r", encoding="utf-8") as f:
            return json.load(f)
    except FileNotFoundError:
        raise BuildError("missing file: %s" % os.path.relpath(path, ROOT))
    except json.JSONDecodeError as e:
        raise BuildError("%s is not valid JSON: %s" % (os.path.relpath(path, ROOT), e))


def require(cond, msg):
    if not cond:
        raise BuildError("content/site.json: " + msg)


def is_str(v):
    return isinstance(v, str) and v.strip() != ""


def validate(c):
    """Check the content file's shape so a typo fails the build, not the page."""
    for key in ("name", "title", "tagline", "location"):
        require(is_str(c.get(key)), "'%s' must be a non-empty string" % key)
    require(isinstance(c.get("summary"), list) and all(is_str(s) for s in c["summary"]), "'summary' must be a list of strings")
    require(isinstance(c.get("links"), list), "'links' must be a list")
    for i, link in enumerate(c["links"]):
        require(is_str(link.get("label")) and is_str(link.get("url")), "links[%d] needs 'label' and 'url'" % i)
        require(link["url"].startswith("https://"), "links[%d].url must start with https://" % i)
    require(isinstance(c.get("email", ""), str), "'email' must be a string (may be empty)")
    for i, e in enumerate(c.get("experience", [])):
        for key in ("role", "org", "place", "dates"):
            require(is_str(e.get(key)), "experience[%d].%s must be a non-empty string" % (i, key))
        require(isinstance(e.get("points"), list) and all(is_str(p) for p in e["points"]), "experience[%d].points must be a list of strings" % i)
    for i, g in enumerate(c.get("skills", [])):
        require(is_str(g.get("group")) and isinstance(g.get("items"), list) and all(is_str(s) for s in g["items"]), "skills[%d] needs 'group' and 'items'" % i)
    for i, x in enumerate(c.get("certifications", [])):
        require(is_str(x.get("name")) and is_str(x.get("dates")), "certifications[%d] needs 'name' and 'dates'" % i)
    for i, x in enumerate(c.get("education", [])):
        for key in ("school", "degree", "dates"):
            require(is_str(x.get(key)), "education[%d].%s must be a non-empty string" % (i, key))
    for i, p in enumerate(c.get("projects", [])):
        for key in ("name", "stack", "text"):
            require(is_str(p.get(key)), "projects[%d].%s must be a non-empty string" % (i, key))
        href = p.get("href", "")
        require(isinstance(href, str), "projects[%d].href must be a string" % i)
        require(href == "" or (not href.startswith(("http:", "javascript:", "//"))), "projects[%d].href must be a relative link or https" % i)


def run_engine(engine, content, seed, out_path):
    if not os.path.isfile(engine):
        raise BuildError("C++ engine not found at %s - run 'make engine' first" % os.path.relpath(engine, ROOT))
    cmd = [engine, "--name", content["name"], "--tagline", content["title"],
           "--headings", ",".join(SECTIONS), "--seed", str(seed), "--out", out_path]
    proc = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, universal_newlines=True)
    if proc.returncode != 0:
        raise BuildError("asciigen failed (%d): %s" % (proc.returncode, proc.stderr.strip()))
    if proc.stderr.strip():
        sys.stderr.write("asciigen: " + proc.stderr)
    assets = load_json(out_path)
    for key in ("banners", "constellation"):
        if key not in assets:
            raise BuildError("asciigen output is missing '%s'" % key)
    missing = [s for s in SECTIONS if s not in assets["banners"].get("headings", {})]
    if missing:
        raise BuildError("asciigen output is missing headings: %s" % ", ".join(missing))
    return assets


def script_json(obj):
    """JSON that is safe to drop inside a <script> element."""
    s = json.dumps(obj, separators=(",", ":"), ensure_ascii=True)
    return s.replace("</", "<\\/").replace("<!--", "<\\!--")


def esc(s):
    return html.escape(s, quote=True)


def a11y_html(c):
    """Semantic copy of the content for screen readers, search engines and no-JS visitors."""
    out = []
    out.append('<button id="back-ascii" type="button">back to the ASCII view</button>')
    out.append("<h1>%s</h1>" % esc(c["name"]))
    out.append('<p class="meta">%s &middot; %s</p>' % (esc(c["title"]), esc(c["location"])))
    out.append("<p>%s</p>" % esc(c["tagline"]))
    out.append('<nav aria-label="Sections"><ul>')
    for s in SECTIONS:
        out.append('<li><a href="#%s" data-section="%s">%s</a></li>' % (s, s, s.capitalize()))
    out.append("</ul></nav>")

    out.append('<section id="about" aria-labelledby="h-about"><h2 id="h-about">About</h2>')
    for p in c["summary"]:
        out.append("<p>%s</p>" % esc(p))
    out.append("</section>")

    out.append('<section id="experience" aria-labelledby="h-experience"><h2 id="h-experience">Experience</h2>')
    for e in c.get("experience", []):
        out.append("<article><h3>%s &mdash; %s</h3>" % (esc(e["role"]), esc(e["org"])))
        out.append('<p class="meta">%s &middot; %s</p><ul>' % (esc(e["place"]), esc(e["dates"])))
        for p in e["points"]:
            out.append("<li>%s</li>" % esc(p))
        out.append("</ul></article>")
    out.append("</section>")

    out.append('<section id="skills" aria-labelledby="h-skills"><h2 id="h-skills">Skills</h2>')
    for g in c.get("skills", []):
        out.append("<h3>%s</h3><p>%s</p>" % (esc(g["group"]), esc(", ".join(g["items"]))))
    out.append("</section>")

    out.append('<section id="certifications" aria-labelledby="h-certifications"><h2 id="h-certifications">Certifications</h2><ul>')
    for x in c.get("certifications", []):
        out.append("<li>%s <span class=\"meta\">(%s)</span></li>" % (esc(x["name"]), esc(x["dates"])))
    out.append("</ul></section>")

    out.append('<section id="education" aria-labelledby="h-education"><h2 id="h-education">Education</h2><ul>')
    for x in c.get("education", []):
        out.append("<li>%s, %s <span class=\"meta\">(%s)</span></li>" % (esc(x["degree"]), esc(x["school"]), esc(x["dates"])))
    out.append("</ul></section>")

    out.append('<section id="projects" aria-labelledby="h-projects"><h2 id="h-projects">Projects</h2>')
    for p in c.get("projects", []):
        title = esc(p["name"])
        if p.get("href"):
            title = '<a href="%s">%s</a>' % (esc(p["href"]), title)
        out.append('<article><h3>%s</h3><p class="meta">%s</p><p>%s</p></article>' % (title, esc(p["stack"]), esc(p["text"])))
    out.append("</section>")

    out.append('<section id="contact" aria-labelledby="h-contact"><h2 id="h-contact">Contact</h2><ul>')
    for link in c["links"]:
        out.append('<li><a href="%s" rel="noopener noreferrer" target="_blank">%s</a></li>' % (esc(link["url"]), esc(link["label"])))
    if c.get("email"):
        out.append('<li><a href="mailto:%s">%s</a></li>' % (esc(c["email"]), esc(c["email"])))
    out.append("</ul></section>")
    return "\n".join(out)


def build_info(content_bytes, assets_bytes, app_bytes):
    digest = hashlib.sha256(content_bytes + assets_bytes + app_bytes).hexdigest()[:10]
    commit = ""
    try:
        proc = subprocess.run(["git", "-C", ROOT, "rev-parse", "--short", "HEAD"], stdout=subprocess.PIPE,
                              stderr=subprocess.DEVNULL, universal_newlines=True)
        if proc.returncode == 0:
            commit = proc.stdout.strip()
    except OSError:
        pass
    epoch = os.environ.get("SOURCE_DATE_EPOCH")
    utc = datetime.timezone.utc
    when = datetime.datetime.fromtimestamp(int(epoch), tz=utc) if epoch else datetime.datetime.now(utc)
    return {"id": commit or digest, "date": when.strftime("%Y-%m-%d")}


def main(argv=None):
    ap = argparse.ArgumentParser(description="Build the ASCII site into dist/")
    ap.add_argument("--content", default=os.path.join(ROOT, "content", "site.json"))
    ap.add_argument("--engine", default=os.path.join(ROOT, "bin", "asciigen"))
    ap.add_argument("--out", default=os.path.join(ROOT, "dist"))
    ap.add_argument("--seed", type=int, default=2026)
    args = ap.parse_args(argv)

    try:
        content = load_json(args.content)
        validate(content)
        build_dir = os.path.join(ROOT, "build")
        os.makedirs(build_dir, exist_ok=True)
        assets_path = os.path.join(build_dir, "assets.json")
        assets = run_engine(args.engine, content, args.seed, assets_path)

        with open(os.path.join(ROOT, "web", "app.js"), "r", encoding="utf-8") as f:
            app = f.read()
        with open(os.path.join(ROOT, "web", "template.html"), "r", encoding="utf-8") as f:
            template = f.read()

        info = build_info(json.dumps(content, sort_keys=True).encode(), json.dumps(assets, sort_keys=True).encode(), app.encode())
        for marker in ("/*@CONTENT@*/null", "/*@ASSETS@*/null", "/*@BUILD@*/null"):
            if app.count(marker) != 1:
                raise BuildError("web/app.js must contain %s exactly once" % marker)
        app = app.replace("/*@CONTENT@*/null", script_json(content))
        app = app.replace("/*@ASSETS@*/null", script_json(assets))
        app = app.replace("/*@BUILD@*/null", script_json(info))
        if "</script" in app.lower():
            raise BuildError("web/app.js must not contain a closing script tag")

        for marker in ("/*@APP@*/", "<!--@A11Y@-->", "@TITLE@", "@DESC@"):
            if marker not in template:
                raise BuildError("web/template.html is missing %s" % marker)
        page = template.replace("@TITLE@", esc("%s - %s" % (content["name"], content["title"])))
        page = page.replace("@DESC@", esc("%s, %s. %s" % (content["name"], content["title"], content["tagline"])))
        page = page.replace("<!--@A11Y@-->", a11y_html(content))
        page = page.replace("/*@APP@*/", app)

        if os.path.isdir(args.out):
            shutil.rmtree(args.out)
        os.makedirs(args.out)
        with open(os.path.join(args.out, "index.html"), "w", encoding="utf-8", newline="\n") as f:
            f.write(page)

        lab_src = os.path.join(ROOT, "lab")
        copied = []
        if os.path.isdir(lab_src):
            lab_dst = os.path.join(args.out, "lab")
            os.makedirs(lab_dst)
            for name in sorted(os.listdir(lab_src)):
                if name.endswith(".html"):
                    shutil.copyfile(os.path.join(lab_src, name), os.path.join(lab_dst, name))
                    copied.append("lab/" + name)
        for p in content.get("projects", []):
            href = p.get("href", "")
            if href and not href.startswith("https://") and href not in copied:
                sys.stderr.write("warning: project link %s has no matching file in lab/\n" % href)

        size = os.path.getsize(os.path.join(args.out, "index.html"))
        print("built dist/index.html (%.1f KB, build %s)%s" % (size / 1024.0, info["id"],
              (" + " + ", ".join(copied)) if copied else ""))
        return 0
    except BuildError as e:
        sys.stderr.write("build failed: %s\n" % e)
        return 1


if __name__ == "__main__":
    sys.exit(main())
