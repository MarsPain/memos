from __future__ import annotations

import subprocess
import sys
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


class DocsValidationTest(unittest.TestCase):
    def test_documentation_context_is_valid(self) -> None:
        result = subprocess.run(
            [sys.executable, "scripts/validate_docs.py"],
            cwd=ROOT,
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()
