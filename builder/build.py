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
import re
import shutil
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SECTIONS = ["about", "education", "certifications", "experience", "skills", "projects", "contact"]


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


SMART = {
    "\u2018": "'", "\u2019": "'", "\u201a": "'", "\u2032": "'",
    "\u201c": '"', "\u201d": '"', "\u201e": '"', "\u2033": '"',
    "\u2013": "-", "\u2014": "-", "\u2212": "-", "\u2026": "...",
    "\u00a0": " ", "\u2009": " ", "\u202f": " ", "\u00b7": "-", "\u2022": "-",
}


def to_ascii(obj, path="content"):
    """The canvas draws ASCII only: map typographic punctuation to ASCII, reject anything else."""
    if isinstance(obj, str):
        out = "".join(SMART.get(ch, ch) for ch in obj)
        bad = sorted({ch for ch in out if ord(ch) > 126})
        if bad:
            raise BuildError("%s contains characters the ASCII canvas can't draw: %s"
                             % (path, ", ".join("%r (U+%04X)" % (c, ord(c)) for c in bad)))
        return out
    if isinstance(obj, list):
        return [to_ascii(v, "%s[%d]" % (path, i)) for i, v in enumerate(obj)]
    if isinstance(obj, dict):
        return {k: to_ascii(v, "%s.%s" % (path, k)) for k, v in obj.items()}
    return obj


def validate(c):
    """Check the content file's shape so a typo fails the build, not the page."""
    for key in ("name", "title", "tagline", "location"):
        require(is_str(c.get(key)), "'%s' must be a non-empty string" % key)
    require(isinstance(c.get("summary"), list) and all(is_str(s) for s in c["summary"]), "'summary' must be a list of strings")
    require(isinstance(c.get("links"), list), "'links' must be a list")
    for i, link in enumerate(c["links"]):
        require(is_str(link.get("label")) and is_str(link.get("url")), "links[%d] needs 'label' and 'url'" % i)
        require(link["url"].startswith("https://"), "links[%d].url must start with https://" % i)
        require(link.get("icon", "link") in ("profile", "code", "mail", "link"),
                "links[%d].icon must be one of profile, code, mail, link" % i)
    require(isinstance(c.get("email", ""), str), "'email' must be a string (may be empty)")
    for i, e in enumerate(c.get("experience", [])):
        for key in ("role", "org", "place", "dates"):
            require(is_str(e.get(key)), "experience[%d].%s must be a non-empty string" % (i, key))
        require(isinstance(e.get("points"), list) and all(is_str(p) for p in e["points"]), "experience[%d].points must be a list of strings" % i)
        require(e.get("art", "redrocks") in ("redrocks", "pikespeak", "vegas", "denver", "beach"), "experience[%d].art must be one of redrocks, pikespeak, vegas, denver, beach" % i)
    for i, g in enumerate(c.get("skills", [])):
        require(is_str(g.get("group")) and isinstance(g.get("items"), list) and all(is_str(s) for s in g["items"]), "skills[%d] needs 'group' and 'items'" % i)
    for i, x in enumerate(c.get("certifications", [])):
        require(is_str(x.get("name")) and is_str(x.get("dates")), "certifications[%d] needs 'name' and 'dates'" % i)
    for i, x in enumerate(c.get("education", [])):
        for key in ("school", "degree", "dates"):
            require(is_str(x.get(key)), "education[%d].%s must be a non-empty string" % (i, key))
        require(x.get("emblem", "mountains") in ("mountains", "island"), "education[%d].emblem must be mountains or island" % i)
        require(isinstance(x.get("note", ""), str), "education[%d].note must be a string" % i)
    require(isinstance(c.get("whoami", ""), str), "'whoami' must be a string")
    require(isinstance(c.get("contactIntro", ""), str), "'contactIntro' must be a string")
    stack = c.get("stack", [])
    require(isinstance(stack, list), "'stack' must be a list")
    for i, x in enumerate(stack):
        require(is_str(x.get("label")) and isinstance(x.get("items"), list) and x["items"] and all(is_str(v) for v in x["items"]),
                "stack[%d] needs 'label' and a non-empty 'items' list" % i)
    taglines = c.get("taglines", {})
    require(isinstance(taglines, dict), "'taglines' must be an object")
    for k, v in taglines.items():
        require(k in SECTIONS, "taglines.%s is not a section (use one of %s)" % (k, ", ".join(SECTIONS)))
        require(is_str(v), "taglines.%s must be a non-empty string" % k)
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
    out.append("<h1>%s</h1>" % esc(c["name"]))
    out.append('<p class="meta">%s &middot; %s</p>' % (esc(c["title"]), esc(c["location"])))
    out.append("<p>%s</p>" % esc(c["tagline"]))
    if c.get("whoami"):
        out.append("<p>%s</p>" % esc(c["whoami"]))
    out.append('<nav aria-label="Sections"><ul>')
    for s in SECTIONS:
        out.append('<li><a href="#%s" data-section="%s">%s</a></li>' % (s, s, s.capitalize()))
    out.append("</ul></nav>")

    out.append('<section id="about" aria-labelledby="h-about"><h2 id="h-about">About</h2>')
    quip(out, c, "about")
    for p in c["summary"]:
        out.append("<p>%s</p>" % esc(p))
    out.append("</section>")

    out.append('<section id="education" aria-labelledby="h-education"><h2 id="h-education">Education</h2>')
    quip(out, c, "education")
    out.append("<ul>")
    for x in c.get("education", []):
        out.append("<li>%s, %s <span class=\"meta\">(%s)</span></li>" % (esc(x["degree"]), esc(x["school"]), esc(x["dates"])))
    out.append("</ul></section>")

    out.append('<section id="certifications" aria-labelledby="h-certifications"><h2 id="h-certifications">Certifications</h2>')
    quip(out, c, "certifications")
    out.append("<ul>")
    for x in c.get("certifications", []):
        out.append("<li>%s <span class=\"meta\">(%s)</span></li>" % (esc(x["name"]), esc(x["dates"])))
    out.append("</ul></section>")

    out.append('<section id="experience" aria-labelledby="h-experience"><h2 id="h-experience">Experience</h2>')
    quip(out, c, "experience")
    for e in c.get("experience", []):
        out.append("<article><h3>%s &mdash; %s</h3>" % (esc(e["role"]), esc(e["org"])))
        out.append('<p class="meta">%s &middot; %s</p><ul>' % (esc(e["place"]), esc(e["dates"])))
        for p in e["points"]:
            out.append("<li>%s</li>" % esc(p))
        out.append("</ul></article>")
    out.append("</section>")

    out.append('<section id="skills" aria-labelledby="h-skills"><h2 id="h-skills">Skills</h2>')
    quip(out, c, "skills")
    for g in c.get("skills", []):
        out.append("<h3>%s</h3><p>%s</p>" % (esc(g["group"]), esc(", ".join(g["items"]))))
    out.append("</section>")

    out.append('<section id="projects" aria-labelledby="h-projects"><h2 id="h-projects">Projects</h2>')
    quip(out, c, "projects")
    for p in c.get("projects", []):
        title = esc(p["name"])
        if p.get("href"):
            title = '<a href="%s">%s</a>' % (esc(p["href"]), title)
        out.append('<article><h3>%s</h3><p class="meta">%s</p><p>%s</p></article>' % (title, esc(p["stack"]), esc(p["text"])))
    out.append("</section>")

    out.append('<section id="contact" aria-labelledby="h-contact"><h2 id="h-contact">Contact</h2>')
    quip(out, c, "contact")
    out.append("<ul>")
    for link in c["links"]:
        out.append('<li><a href="%s" rel="noopener noreferrer" target="_blank">%s</a></li>' % (esc(link["url"]), esc(link["label"])))
    if c.get("email"):
        out.append('<li><a href="mailto:%s">%s</a></li>' % (esc(c["email"]), esc(c["email"])))
    out.append("</ul></section>")
    if c.get("stack"):
        out.append('<footer><h2>How this site is built</h2><ul>')
        for x in c["stack"]:
            out.append("<li>%s: %s</li>" % (esc(x["label"]), esc(", ".join(x["items"]))))
        out.append("</ul></footer>")
    return "\n".join(out)


def quip(out, c, sid):
    q = c.get("taglines", {}).get(sid)
    if q:
        out.append('<p class="meta">%s</p>' % esc(q))


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
        content = to_ascii(load_json(args.content))
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
        # Placeholders look like /*@NAME@*/null. Allow any whitespace before
        # "null" so code formatters (Prettier etc.) don't break the build.
        for name, value in (("CONTENT", content), ("ASSETS", assets), ("BUILD", info)):
            pattern = re.compile(r"/\*@%s@\*/\s*null" % name)
            if len(pattern.findall(app)) != 1:
                raise BuildError("web/app.js must contain /*@%s@*/null exactly once" % name)
            data = script_json(value)
            app = pattern.sub(lambda m: data, app)
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
