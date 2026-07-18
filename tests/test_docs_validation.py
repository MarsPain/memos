from __future__ import annotations

import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from scripts.validate_docs import validate_repository


ROOT = Path(__file__).resolve().parents[1]


def write(root: Path, relative_path: str, content: str) -> None:
    path = root / relative_path
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content.strip() + "\n", encoding="utf-8")


def make_minimal(root: Path, *, agent_name: str = "AGENTS.md", with_docs: bool = False) -> None:
    docs_link = "\n- [Docs](docs/README.md)" if with_docs else ""
    write(root, "README.md", f"# Example\n\n[Agent map]({agent_name}) and [architecture](ARCHITECTURE.md).")
    write(root, agent_name, f"# Agent Map\n\n- [Architecture](ARCHITECTURE.md){docs_link}")
    write(root, "ARCHITECTURE.md", "# Architecture\n\nMinimal system map.")
    if with_docs:
        write(root, "docs/README.md", "# Documentation")


def validation_errors(root: Path) -> list[str]:
    _, errors = validate_repository(root)
    return errors


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

    def test_minimal_repository_does_not_require_optional_capabilities(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            make_minimal(root)
            self.assertEqual(validation_errors(root), [])

    def test_claude_md_can_be_the_only_agent_entrypoint(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            make_minimal(root, agent_name="CLAUDE.md")
            self.assertEqual(validation_errors(root), [])

    def test_single_context_and_root_adr_are_discovered_lazily(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            make_minimal(root, with_docs=True)
            write(root, "CONTEXT.md", "# Domain Glossary\n\n**Memo**: A note.")
            write(root, "docs/adr/0001-storage.md", "# Store Memos Relationally\n\nStatus: Accepted")
            write(root, "docs/README.md", "# Documentation\n\n- [ADRs](adr/)")
            self.assertEqual(validation_errors(root), [])

    def test_multi_context_map_reaches_context_glossary_and_adr(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            make_minimal(root)
            write(root, "CONTEXT-MAP.md", "# Context Map\n\n- [Ordering](src/ordering/CONTEXT.md)")
            write(root, "src/ordering/CONTEXT.md", "# Ordering\n\n- [Decisions](docs/adr/)")
            write(root, "src/ordering/docs/adr/0001-orders.md", "# Order Identity\n\nStatus: Accepted")
            self.assertEqual(validation_errors(root), [])

    def test_root_context_and_context_map_cannot_compete(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            make_minimal(root)
            write(root, "CONTEXT.md", "# Glossary")
            write(root, "CONTEXT-MAP.md", "# Context Map")
            self.assertIn("CONTEXT.md and CONTEXT-MAP.md cannot both be authoritative", validation_errors(root))

    def test_agent_configuration_requires_direct_entrypoint_and_index_links(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            make_minimal(root, with_docs=True)
            write(root, "docs/agents/domain.md", "# Domain Docs")
            write(root, "AGENTS.md", "# Agent Map\n\n- [Architecture](ARCHITECTURE.md)\n- [Docs](docs/README.md)\n- [Domain](docs/agents/domain.md)")
            write(root, "docs/README.md", "# Documentation\n\n- [Agent configuration](agents/)")
            self.assertIn(
                "agent configuration is not linked from docs/README.md: docs/agents/domain.md",
                validation_errors(root),
            )

    def test_local_issue_must_link_to_canonical_parent_spec(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            make_minimal(root, with_docs=True)
            write(root, "docs/agents/issue-tracker.md", "# Issue tracker: Local Markdown")
            write(root, "docs/product-specs/search.md", "# Search Spec")
            write(
                root,
                "AGENTS.md",
                "# Agent Map\n\n- [Architecture](ARCHITECTURE.md)\n- [Docs](docs/README.md)\n- [Tracker](docs/agents/issue-tracker.md)",
            )
            write(
                root,
                "docs/README.md",
                "# Documentation\n\n- [Tracker](agents/issue-tracker.md)\n- [Agents](agents/)\n- [Specs](product-specs/)",
            )
            issue = ".scratch/search/issues/01-index.md"
            write(root, issue, "# Build Index\n\nStatus: ready-for-agent\nBlocked by: none")
            self.assertIn(f"implementation issue has no Parent spec field: {issue}", validation_errors(root))

            write(
                root,
                issue,
                "# Build Index\n\nParent spec: [Search](../../../docs/product-specs/search.md)\nStatus: ready-for-agent\nBlocked by: none",
            )
            self.assertEqual(validation_errors(root), [])

    def test_local_issue_dependency_must_resolve_to_a_sibling(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            make_minimal(root, with_docs=True)
            write(root, "docs/agents/issue-tracker.md", "# Issue tracker: Local Markdown")
            write(root, "docs/product-specs/search.md", "# Search Spec")
            write(
                root,
                "AGENTS.md",
                "# Agent Map\n\n- [Architecture](ARCHITECTURE.md)\n- [Docs](docs/README.md)\n- [Tracker](docs/agents/issue-tracker.md)",
            )
            write(
                root,
                "docs/README.md",
                "# Documentation\n\n- [Tracker](agents/issue-tracker.md)\n- [Agents](agents/)\n- [Specs](product-specs/)",
            )
            issue = ".scratch/search/issues/02-query.md"
            write(
                root,
                issue,
                "# Query Index\n\nParent spec: [Search](../../../docs/product-specs/search.md)\nStatus: ready-for-agent\nBlocked by: 99",
            )
            self.assertIn(
                f"implementation issue dependency 99 does not resolve to a sibling issue: {issue}",
                validation_errors(root),
            )

    def test_external_tracker_rejects_competing_scratch_issues(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            make_minimal(root, with_docs=True)
            write(root, "docs/agents/issue-tracker.md", "# Issue tracker: GitHub")
            write(
                root,
                "AGENTS.md",
                "# Agent Map\n\n- [Architecture](ARCHITECTURE.md)\n- [Docs](docs/README.md)\n- [Tracker](docs/agents/issue-tracker.md)",
            )
            write(root, "docs/README.md", "# Documentation\n\n- [Tracker](agents/issue-tracker.md)\n- [Agents](agents/)")
            write(root, ".scratch/example/issues/01.md", "# Competing Issue")
            self.assertIn(
                "external issue tracker is configured but .scratch contains competing Markdown issues",
                validation_errors(root),
            )

    def test_legacy_exec_plan_archive_does_not_require_lifecycle_buckets(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            make_minimal(root, with_docs=True)
            write(root, "docs/exec-plans/archive/0001.md", "# Historical Plan")
            write(root, "docs/README.md", "# Documentation\n\n- [Legacy plans](exec-plans/)")
            self.assertEqual(validation_errors(root), [])

    def test_design_exploration_does_not_require_an_adr(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            make_minimal(root, with_docs=True)
            write(root, "docs/design-docs/search.md", "# Search Exploration\n\nTwo alternatives remain open.")
            write(root, "docs/README.md", "# Documentation\n\n- [Designs](design-docs/)")
            self.assertEqual(validation_errors(root), [])

    def test_adr_requires_explicit_status(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            make_minimal(root, with_docs=True)
            write(root, "docs/adr/0001-storage.md", "# Storage Decision")
            write(root, "docs/README.md", "# Documentation\n\n- [ADRs](adr/)")
            self.assertIn("ADR has no explicit status: docs/adr/0001-storage.md", validation_errors(root))

    def test_live_plan_state_outside_tracker_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            make_minimal(root, with_docs=True)
            write(root, "docs/agents/issue-tracker.md", "# Issue tracker: Local Markdown")
            write(root, "docs/PLANS.md", "# Plans\n\n## Active\n\nCurrent task.")
            write(
                root,
                "AGENTS.md",
                "# Agent Map\n\n- [Architecture](ARCHITECTURE.md)\n- [Docs](docs/README.md)\n- [Tracker](docs/agents/issue-tracker.md)",
            )
            write(
                root,
                "docs/README.md",
                "# Documentation\n\n- [Plans](PLANS.md)\n- [Tracker](agents/issue-tracker.md)\n- [Agents](agents/)",
            )
            errors = validation_errors(root)
            self.assertIn("docs/PLANS.md contains live active state owned by the issue tracker", errors)


if __name__ == "__main__":
    unittest.main()
