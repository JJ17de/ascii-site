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
SECTIONS = [
    "about",
    "education",
    "certifications",
    "experience",
    "skills",
    "projects",
    "contact",
]


class BuildError(Exception):
    pass


def load_json(path):
    try:
        with open(path, "r", encoding="utf-8") as f:
            return json.load(f)
    except FileNotFoundError:
        raise BuildError(f"missing file: {os.path.relpath(path, ROOT)}")
    except json.JSONDecodeError as e:
        raise BuildError(f"{os.path.relpath(path, ROOT)} is not valid JSON: {e}")


def require(cond, msg):
    if not cond:
        raise BuildError("content/site.json: " + msg)


def is_str(v):
    return isinstance(v, str) and v.strip() != ""


SMART = {
    "\u2018": "'",
    "\u2019": "'",
    "\u201a": "'",
    "\u2032": "'",
    "\u201c": '"',
    "\u201d": '"',
    "\u201e": '"',
    "\u2033": '"',
    "\u2013": "-",
    "\u2014": "-",
    "\u2212": "-",
    "\u2026": "...",
    "\u00a0": " ",
    "\u2009": " ",
    "\u202f": " ",
    "\u00b7": "-",
    "\u2022": "-",
}


def to_ascii(obj, path="content"):
    """The canvas draws ASCII only: map typographic punctuation to ASCII, reject anything else."""
    if isinstance(obj, str):
        out = "".join(SMART.get(ch, ch) for ch in obj)
        bad = sorted({ch for ch in out if ord(ch) > 126})
        if bad:
            raise BuildError(
                "{} contains characters the ASCII canvas can't draw: {}".format(
                    path, ", ".join(f"{c!r} (U+{ord(c):04X})" for c in bad)
                )
            )
        return out
    if isinstance(obj, list):
        return [to_ascii(v, f"{path}[{i}]") for i, v in enumerate(obj)]
    if isinstance(obj, dict):
        return {k: to_ascii(v, f"{path}.{k}") for k, v in obj.items()}
    return obj


def validate(c):
    """Check the content file's shape so a typo fails the build, not the page."""
    for key in ("name", "title", "tagline", "location"):
        require(is_str(c.get(key)), f"'{key}' must be a non-empty string")
    require(
        isinstance(c.get("summary"), list) and all(is_str(s) for s in c["summary"]),
        "'summary' must be a list of strings",
    )
    require(isinstance(c.get("links"), list), "'links' must be a list")
    for i, link in enumerate(c["links"]):
        require(
            is_str(link.get("label")) and is_str(link.get("url")),
            f"links[{i}] needs 'label' and 'url'",
        )
        require(
            link["url"].startswith("https://"),
            f"links[{i}].url must start with https://",
        )
        require(
            link.get("icon", "link") in ("profile", "code", "mail", "link"),
            f"links[{i}].icon must be one of profile, code, mail, link",
        )
    require(
        isinstance(c.get("email", ""), str), "'email' must be a string (may be empty)"
    )
    for i, e in enumerate(c.get("experience", [])):
        for key in ("role", "org", "place", "dates"):
            require(
                is_str(e.get(key)), f"experience[{i}].{key} must be a non-empty string"
            )
        require(
            isinstance(e.get("points"), list) and all(is_str(p) for p in e["points"]),
            f"experience[{i}].points must be a list of strings",
        )
        require(
            e.get("art", "redrocks")
            in ("redrocks", "pikespeak", "vegas", "denver", "beach"),
            f"experience[{i}].art must be one of redrocks, pikespeak, vegas, denver, beach",
        )
    for i, g in enumerate(c.get("skills", [])):
        require(
            is_str(g.get("group"))
            and isinstance(g.get("items"), list)
            and all(is_str(s) for s in g["items"]),
            f"skills[{i}] needs 'group' and 'items'",
        )
    for i, x in enumerate(c.get("certifications", [])):
        require(
            is_str(x.get("name")) and is_str(x.get("dates")),
            f"certifications[{i}] needs 'name' and 'dates'",
        )
    for i, x in enumerate(c.get("education", [])):
        for key in ("school", "degree", "dates"):
            require(
                is_str(x.get(key)), f"education[{i}].{key} must be a non-empty string"
            )
        require(
            x.get("emblem", "mountains") in ("mountains", "island"),
            f"education[{i}].emblem must be mountains or island",
        )
        require(
            isinstance(x.get("note", ""), str), f"education[{i}].note must be a string"
        )
    require(isinstance(c.get("whoami", ""), str), "'whoami' must be a string")
    require(
        isinstance(c.get("contactIntro", ""), str), "'contactIntro' must be a string"
    )
    stack = c.get("stack", [])
    require(isinstance(stack, list), "'stack' must be a list")
    for i, x in enumerate(stack):
        require(
            is_str(x.get("label"))
            and isinstance(x.get("items"), list)
            and x["items"]
            and all(is_str(v) for v in x["items"]),
            f"stack[{i}] needs 'label' and a non-empty 'items' list",
        )
    taglines = c.get("taglines", {})
    require(isinstance(taglines, dict), "'taglines' must be an object")
    for k, v in taglines.items():
        require(
            k in SECTIONS,
            "taglines.{} is not a section (use one of {})".format(
                k, ", ".join(SECTIONS)
            ),
        )
        require(is_str(v), f"taglines.{k} must be a non-empty string")
    for i, p in enumerate(c.get("projects", [])):
        for key in ("name", "stack", "text"):
            require(
                is_str(p.get(key)), f"projects[{i}].{key} must be a non-empty string"
            )
        href = p.get("href", "")
        require(isinstance(href, str), f"projects[{i}].href must be a string")
        require(
            href == "" or (not href.startswith(("http:", "javascript:", "//"))),
            f"projects[{i}].href must be a relative link or https",
        )


def run_engine(engine, content, seed, out_path):
    if not os.path.isfile(engine):
        raise BuildError(
            f"C++ engine not found at {os.path.relpath(engine, ROOT)} - run 'make engine' first"
        )
    cmd = [
        engine,
        "--name",
        content["name"],
        "--tagline",
        content["title"],
        "--headings",
        ",".join(SECTIONS),
        "--seed",
        str(seed),
        "--out",
        out_path,
    ]
    proc = subprocess.run(cmd, capture_output=True, text=True, check=False)
    if proc.returncode != 0:
        raise BuildError(f"asciigen failed ({proc.returncode}): {proc.stderr.strip()}")
    if proc.stderr.strip():
        sys.stderr.write("asciigen: " + proc.stderr)
    assets = load_json(out_path)
    for key in ("banners", "constellation"):
        if key not in assets:
            raise BuildError(f"asciigen output is missing '{key}'")
    missing = [s for s in SECTIONS if s not in assets["banners"].get("headings", {})]
    if missing:
        raise BuildError(
            "asciigen output is missing headings: {}".format(", ".join(missing))
        )
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
    out.append("<h1>{}</h1>".format(esc(c["name"])))
    out.append(
        '<p class="meta">{} &middot; {}</p>'.format(esc(c["title"]), esc(c["location"]))
    )
    out.append("<p>{}</p>".format(esc(c["tagline"])))
    if c.get("whoami"):
        out.append("<p>{}</p>".format(esc(c["whoami"])))
    out.append('<nav aria-label="Sections"><ul>')
    for s in SECTIONS:
        out.append(f'<li><a href="#{s}" data-section="{s}">{s.capitalize()}</a></li>')
    out.append("</ul></nav>")

    out.append(
        '<section id="about" aria-labelledby="h-about"><h2 id="h-about">About</h2>'
    )
    quip(out, c, "about")
    for p in c["summary"]:
        out.append(f"<p>{esc(p)}</p>")
    out.append("</section>")

    out.append(
        '<section id="education" aria-labelledby="h-education"><h2 id="h-education">Education</h2>'
    )
    quip(out, c, "education")
    out.append("<ul>")
    for x in c.get("education", []):
        out.append(
            '<li>{}, {} <span class="meta">({})</span></li>'.format(
                esc(x["degree"]), esc(x["school"]), esc(x["dates"])
            )
        )
    out.append("</ul></section>")

    out.append(
        '<section id="certifications" aria-labelledby="h-certifications"><h2 id="h-certifications">Certifications</h2>'
    )
    quip(out, c, "certifications")
    out.append("<ul>")
    for x in c.get("certifications", []):
        out.append(
            '<li>{} <span class="meta">({})</span></li>'.format(
                esc(x["name"]), esc(x["dates"])
            )
        )
    out.append("</ul></section>")

    out.append(
        '<section id="experience" aria-labelledby="h-experience"><h2 id="h-experience">Experience</h2>'
    )
    quip(out, c, "experience")
    for e in c.get("experience", []):
        out.append(
            "<article><h3>{} &mdash; {}</h3>".format(esc(e["role"]), esc(e["org"]))
        )
        out.append(
            '<p class="meta">{} &middot; {}</p><ul>'.format(
                esc(e["place"]), esc(e["dates"])
            )
        )
        for p in e["points"]:
            out.append(f"<li>{esc(p)}</li>")
        out.append("</ul></article>")
    out.append("</section>")

    out.append(
        '<section id="skills" aria-labelledby="h-skills"><h2 id="h-skills">Skills</h2>'
    )
    quip(out, c, "skills")
    for g in c.get("skills", []):
        out.append(
            "<h3>{}</h3><p>{}</p>".format(esc(g["group"]), esc(", ".join(g["items"])))
        )
    out.append("</section>")

    out.append(
        '<section id="projects" aria-labelledby="h-projects"><h2 id="h-projects">Projects</h2>'
    )
    quip(out, c, "projects")
    for p in c.get("projects", []):
        title = esc(p["name"])
        if p.get("href"):
            title = '<a href="{}">{}</a>'.format(esc(p["href"]), title)
        out.append(
            '<article><h3>{}</h3><p class="meta">{}</p><p>{}</p></article>'.format(
                title, esc(p["stack"]), esc(p["text"])
            )
        )
    out.append("</section>")

    out.append(
        '<section id="contact" aria-labelledby="h-contact"><h2 id="h-contact">Contact</h2>'
    )
    quip(out, c, "contact")
    out.append("<ul>")
    for link in c["links"]:
        out.append(
            '<li><a href="{}" rel="noopener noreferrer" target="_blank">{}</a></li>'.format(
                esc(link["url"]), esc(link["label"])
            )
        )
    if c.get("email"):
        out.append(
            '<li><a href="mailto:{}">{}</a></li>'.format(
                esc(c["email"]), esc(c["email"])
            )
        )
    out.append("</ul></section>")
    if c.get("stack"):
        out.append("<footer><h2>How this site is built</h2><ul>")
        for x in c["stack"]:
            out.append(
                "<li>{}: {}</li>".format(esc(x["label"]), esc(", ".join(x["items"])))
            )
        out.append("</ul></footer>")
    return "\n".join(out)


def quip(out, c, sid):
    q = c.get("taglines", {}).get(sid)
    if q:
        out.append(f'<p class="meta">{esc(q)}</p>')


def build_info(content_bytes, assets_bytes, app_bytes):
    digest = hashlib.sha256(content_bytes + assets_bytes + app_bytes).hexdigest()[:10]
    commit = ""
    try:
        proc = subprocess.run(
            ["git", "-C", ROOT, "rev-parse", "--short", "HEAD"],
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            text=True,
            check=False,
        )
        if proc.returncode == 0:
            commit = proc.stdout.strip()
    except OSError:
        pass
    epoch = os.environ.get("SOURCE_DATE_EPOCH")
    utc = datetime.timezone.utc
    when = (
        datetime.datetime.fromtimestamp(int(epoch), tz=utc)
        if epoch
        else datetime.datetime.now(utc)
    )
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
        with open(
            os.path.join(ROOT, "web", "template.html"), "r", encoding="utf-8"
        ) as f:
            template = f.read()

        info = build_info(
            json.dumps(content, sort_keys=True).encode(),
            json.dumps(assets, sort_keys=True).encode(),
            app.encode(),
        )
        # Placeholders look like /*@NAME@*/null. Allow any whitespace before
        # "null" so code formatters (Prettier etc.) don't break the build.
        for name, value in (("CONTENT", content), ("ASSETS", assets), ("BUILD", info)):
            pattern = re.compile(rf"/\*@{name}@\*/\s*null")
            if len(pattern.findall(app)) != 1:
                raise BuildError(
                    f"web/app.js must contain /*@{name}@*/null exactly once"
                )
            data = script_json(value)
            app = pattern.sub(lambda m, data=data: data, app)
        if "</script" in app.lower():
            raise BuildError("web/app.js must not contain a closing script tag")

        for marker in ("/*@APP@*/", "<!--@A11Y@-->", "@TITLE@", "@DESC@"):
            if marker not in template:
                raise BuildError(f"web/template.html is missing {marker}")
        page = template.replace(
            "@TITLE@", esc("{} - {}".format(content["name"], content["title"]))
        )
        page = page.replace(
            "@DESC@",
            esc(
                "{}, {}. {}".format(
                    content["name"], content["title"], content["tagline"]
                )
            ),
        )
        page = page.replace("<!--@A11Y@-->", a11y_html(content))
        page = page.replace("/*@APP@*/", app)

        if os.path.isdir(args.out):
            shutil.rmtree(args.out)
        os.makedirs(args.out)
        with open(
            os.path.join(args.out, "index.html"), "w", encoding="utf-8", newline="\n"
        ) as f:
            f.write(page)

        lab_src = os.path.join(ROOT, "lab")
        copied = []
        if os.path.isdir(lab_src):
            lab_dst = os.path.join(args.out, "lab")
            os.makedirs(lab_dst)
            for name in sorted(os.listdir(lab_src)):
                if name.endswith(".html"):
                    shutil.copyfile(
                        os.path.join(lab_src, name), os.path.join(lab_dst, name)
                    )
                    copied.append("lab/" + name)
        for p in content.get("projects", []):
            href = p.get("href", "")
            if href and not href.startswith("https://") and href not in copied:
                sys.stderr.write(
                    f"warning: project link {href} has no matching file in lab/\n"
                )

        size = os.path.getsize(os.path.join(args.out, "index.html"))
        print(
            "built dist/index.html ({:.1f} KB, build {}){}".format(
                size / 1024.0, info["id"], (" + " + ", ".join(copied)) if copied else ""
            )
        )
        return 0
    except BuildError as e:
        sys.stderr.write(f"build failed: {e}\n")
        return 1


if __name__ == "__main__":
    sys.exit(main())
