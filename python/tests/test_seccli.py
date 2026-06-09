"""Hermetic tests for the seccli wrapper.

No Go build is required: the binary invocation seam (``seccli._runner.run``) is
stubbed to return recorded JSON fixtures, so these tests exercise deserialization
and error handling against the canonical schema without ever executing a binary.
"""

from __future__ import annotations

import os
import unittest
from pathlib import Path
from unittest import mock

import seccli
from seccli import _runner
from seccli.errors import SecCliError, SecCliNotFoundError

FIXTURES = Path(__file__).parent / "fixtures"


def read_fixture(name: str) -> str:
    return (FIXTURES / name).read_text(encoding="utf-8")


class GetTests(unittest.TestCase):
    def test_get_deserializes_document(self):
        payload = read_fixture("get.json")
        with mock.patch.object(_runner, "run", return_value=payload) as run:
            doc = seccli.get("ACME")

        # The wrapper asked for JSON and passed the ticker through.
        args = run.call_args.args[0]
        self.assertEqual(args[0], "get")
        self.assertIn("ACME", args)
        self.assertIn("--format", args)
        self.assertIn("json", args)

        self.assertEqual(doc.metadata.ticker, "ACME")
        self.assertEqual(doc.metadata.cik, 1234567)
        self.assertEqual(doc.metadata.schema_version, "1.0.0")
        self.assertEqual([s.item for s in doc.sections], ["1", "1A", "7", "8"])
        self.assertEqual(len(doc.statements), 1)

    def test_null_cell_stays_none(self):
        payload = read_fixture("get.json")
        with mock.patch.object(_runner, "run", return_value=payload):
            doc = seccli.get("ACME")

        rnd = next(r for r in doc.statements[0].rows if r.concept == "acme:ResearchExpense")
        self.assertEqual(rnd.values[0], 50000000.0)
        self.assertIsNone(rnd.values[1], "absent cell must decode as None, never 0")

    def test_get_forwards_options(self):
        with mock.patch.object(_runner, "run", return_value=read_fixture("get.json")) as run:
            seccli.get("ACME", form="10-Q", year=2023, section="1A", no_cache=True)
        args = run.call_args.args[0]
        self.assertEqual(args[args.index("--type") + 1], "10-Q")
        self.assertEqual(args[args.index("--year") + 1], "2023")
        self.assertEqual(args[args.index("--section") + 1], "1A")
        self.assertIn("--no-cache", args)

    def test_binary_error_raises(self):
        with mock.patch.object(_runner, "run", side_effect=SecCliError("sec-cli: no filing found")):
            with self.assertRaises(SecCliError) as ctx:
                seccli.get("NOPE")
        self.assertIn("no filing found", str(ctx.exception))


class DiffTests(unittest.TestCase):
    def test_diff_deserializes_change_set(self):
        with mock.patch.object(_runner, "run", return_value=read_fixture("diff.json")) as run:
            cs = seccli.diff("ACME", frm=2023, to=2024)

        args = run.call_args.args[0]
        self.assertEqual(args[0], "diff")
        self.assertEqual(args[args.index("--from") + 1], "2023")
        self.assertEqual(args[args.index("--to") + 1], "2024")

        self.assertEqual(cs.curr.period_end, "2024-12-31")
        self.assertEqual(len(cs.statements), 1)

        rev = next(r for r in cs.statements[0].rows if r.concept == "us-gaap:Revenues")
        self.assertEqual(rev.status, "changed")
        self.assertAlmostEqual(rev.cells[0].abs, 100.0)

        # Percent against a zero base decodes as None, not 0.
        mkt = next(r for r in cs.statements[0].rows if r.concept == "us-gaap:MarketingExpense")
        self.assertIsNone(mkt.cells[0].pct)

        # Added/removed rows carry a one-sided value.
        added = next(r for r in cs.statements[0].rows if r.status == "added")
        self.assertIsNone(added.cells[0].prev)
        self.assertEqual(added.cells[0].curr, 75.0)

        self.assertEqual(cs.sections[0].item, "1A")
        self.assertEqual([p.kind for p in cs.sections[0].paragraphs], ["removed", "added"])


class BinaryLocationTests(unittest.TestCase):
    def test_env_override_wins(self):
        with mock.patch.dict(os.environ, {_runner.BINARY_ENV: "/custom/sec-cli"}):
            self.assertEqual(_runner.find_binary(), "/custom/sec-cli")

    def test_missing_binary_raises(self):
        with mock.patch.dict(os.environ, {}, clear=True):
            with mock.patch.object(_runner.os.path, "exists", return_value=False):
                with mock.patch.object(_runner.shutil, "which", return_value=None):
                    with self.assertRaises(SecCliNotFoundError):
                        _runner.find_binary()


if __name__ == "__main__":
    unittest.main()
