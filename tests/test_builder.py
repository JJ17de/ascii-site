"""Unit tests for the Python builder (standard library only: python -m unittest)."""

import copy
import json
import os
import sys
import unittest

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, os.path.join(ROOT, "builder"))

import build


def sample():
    with open(os.path.join(ROOT, "content", "site.json"), "r", encoding="utf-8") as f:
        return json.load(f)


class ValidateTests(unittest.TestCase):
    def test_real_content_is_valid(self):
        build.validate(sample())

    def test_missing_name_fails(self):
        c = sample()
        c["name"] = ""
        with self.assertRaises(build.BuildError):
            build.validate(c)

    def test_http_link_rejected(self):
        c = sample()
        c["links"][0]["url"] = "http://example.com"
        with self.assertRaises(build.BuildError):
            build.validate(c)

    def test_javascript_project_link_rejected(self):
        c = sample()
        c["projects"][0]["href"] = "javascript:alert(1)"
        with self.assertRaises(build.BuildError):
            build.validate(c)

    def test_points_must_be_strings(self):
        c = sample()
        c["experience"][0]["points"] = ["ok", 3]
        with self.assertRaises(build.BuildError):
            build.validate(c)


class EscapingTests(unittest.TestCase):
    def test_script_json_cannot_close_the_script(self):
        s = build.script_json(
            {"x": "</script><script>alert(1)</script>", "y": "<!-- hi"}
        )
        self.assertNotIn("</script", s.lower())
        self.assertNotIn("<!--", s)
        self.assertEqual(
            json.loads(s.replace("<\\/", "</").replace("<\\!--", "<!--"))["x"],
            "</script><script>alert(1)</script>",
        )

    def test_a11y_html_escapes_content(self):
        c = copy.deepcopy(sample())
        c["name"] = "<img src=x onerror=alert(1)>"
        out = build.a11y_html(c)
        self.assertNotIn("<img", out)
        self.assertIn("&lt;img", out)

    def test_a11y_has_every_section(self):
        out = build.a11y_html(sample())
        for s in build.SECTIONS:
            self.assertIn(f'id="{s}"', out)


class BuiltPageTests(unittest.TestCase):
    """Runs only after `make site` has produced dist/index.html."""

    def setUp(self):
        self.path = os.path.join(ROOT, "dist", "index.html")
        if not os.path.isfile(self.path):
            self.skipTest("dist/index.html not built")
        with open(self.path, "r", encoding="utf-8") as f:
            self.page = f.read()

    def test_markers_replaced(self):
        for marker in (
            "@CONTENT@",
            "@ASSETS@",
            "@BUILD@",
            "@APP@",
            "@A11Y@",
            "@TITLE@",
            "@DESC@",
        ):
            self.assertNotIn(marker, self.page)

    def test_one_script_one_style(self):
        self.assertEqual(self.page.lower().count("<script"), 1)
        self.assertEqual(self.page.lower().count("<style"), 1)

    def test_no_private_contact_details(self):
        self.assertNotIn("758-9104", self.page)
        self.assertNotIn("Clearance", self.page)


if __name__ == "__main__":
    unittest.main()
