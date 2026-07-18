#!/usr/bin/env python3
"""Validate the repository's managed documentation context system."""

from __future__ import annotations

import re
import sys
from pathlib import Path
from urllib.parse import unquote


ROOT = Path(__file__).resolve().parents[1]

REQUIRED_FILES = (
    "README.md",
    "AGENTS.md",
    "ARCHITECTURE.md",
    "SECURITY.md",
    "docs/README.md",
    "docs/DESIGN.md",
    "docs/BACKEND.md",
    "docs/FRONTEND.md",
    "docs/DATA.md",
    "docs/SECURITY.md",
    "docs/PRODUCT_SENSE.md",
    "docs/ROADMAP.md",
    "docs/PLANS.md",
    "docs/design-docs/ai-native-notes.md",
    "docs/product-specs/ai-native-notes.md",
)

REQUIRED_DIRS = (
    "docs/design-docs",
    "docs/product-specs",
    "docs/exec-plans/active",
    "docs/exec-plans/completed",
    "docs/exec-plans/tech-debt",
    "docs/generated",
    "docs/references",
)

ROOT_MAPS = (
    "README.md",
    "AGENTS.md",
    "ARCHITECTURE.md",
    "docs/README.md",
    "docs/PLANS.md",
)

CORE_REACHABLE_FILES = (
    "docs/DESIGN.md",
    "docs/BACKEND.md",
    "docs/FRONTEND.md",
    "docs/DATA.md",
    "docs/SECURITY.md",
    "docs/PRODUCT_SENSE.md",
    "docs/ROADMAP.md",
    "docs/PLANS.md",
    "docs/design-docs/ai-native-notes.md",
    "docs/product-specs/ai-native-notes.md",
    "docs/exec-plans/active/0001-ai-foundation.md",
    "docs/exec-plans/completed/0000-harness-context-foundation.md",
    "docs/exec-plans/tech-debt/0001-legacy-plan-indexing.md",
)

AI_DESIGN_REQUIRED_LINKS = (
    "../DATA.md",
    "../SECURITY.md",
    "../product-specs/ai-native-notes.md",
    "../ROADMAP.md",
    "../PLANS.md",
)

MARKDOWN_LINK = re.compile(r"(?<!!)\[[^\]]+\]\(([^)]+)\)")
EXTERNAL_SCHEMES = ("http://", "https://", "mailto:", "tel:", "data:")


def relative(path: Path) -> str:
    return path.relative_to(ROOT).as_posix()


def managed_markdown_files() -> list[Path]:
    files = {ROOT / path for path in REQUIRED_FILES}
    for directory in (
        "docs/design-docs",
        "docs/product-specs",
        "docs/exec-plans",
        "docs/generated",
        "docs/references",
    ):
        files.update((ROOT / directory).rglob("*.md"))
    return sorted(files)


def links_outside_fences(path: Path) -> list[str]:
    links: list[str] = []
    in_fence = False
    fence_marker = ""
    for line in path.read_text(encoding="utf-8").splitlines():
        stripped = line.lstrip()
        if stripped.startswith("```") or stripped.startswith("~~~"):
            marker = stripped[:3]
            if not in_fence:
                in_fence = True
                fence_marker = marker
            elif marker == fence_marker:
                in_fence = False
                fence_marker = ""
            continue
        if in_fence:
            continue
        links.extend(MARKDOWN_LINK.findall(line))
    return links


def normalize_link(source: Path, raw_link: str) -> Path | None:
    link = raw_link.strip()
    if link.startswith("<") and link.endswith(">"):
        link = link[1:-1]
    if not link or link.startswith("#") or link.startswith(EXTERNAL_SCHEMES):
        return None
    link = unquote(link.split("#", 1)[0].split("?", 1)[0])
    if not link:
        return None
    if link.startswith("/"):
        return ROOT / link.lstrip("/")
    return (source.parent / link).resolve()


def check_presence(errors: list[str]) -> None:
    for path in REQUIRED_FILES:
        if not (ROOT / path).is_file():
            errors.append(f"missing required file: {path}")
    for path in REQUIRED_DIRS:
        if not (ROOT / path).is_dir():
            errors.append(f"missing required directory: {path}")


def check_root_maps(errors: list[str]) -> None:
    agents = ROOT / "AGENTS.md"
    if agents.is_file():
        line_count = len(agents.read_text(encoding="utf-8").splitlines())
        if line_count > 140:
            errors.append(f"AGENTS.md exceeds 140-line map budget: {line_count}")
        content = agents.read_text(encoding="utf-8")
        for target in ("ARCHITECTURE.md", "docs/README.md", "docs/PLANS.md"):
            if target not in content:
                errors.append(f"AGENTS.md does not link to {target}")

    architecture = ROOT / "ARCHITECTURE.md"
    if architecture.is_file():
        content = architecture.read_text(encoding="utf-8")
        for target in ("docs/DESIGN.md", "docs/design-docs/ai-native-notes.md"):
            if target not in content:
                errors.append(f"ARCHITECTURE.md does not link to {target}")


def check_ai_design_ownership(errors: list[str]) -> None:
    design = ROOT / "docs/design-docs/ai-native-notes.md"
    if not design.is_file():
        return
    links = set(links_outside_fences(design))
    for target in AI_DESIGN_REQUIRED_LINKS:
        if target not in links:
            errors.append(f"AI architecture design does not link to canonical owner: {target}")


def check_links(errors: list[str]) -> None:
    for source in managed_markdown_files():
        if not source.is_file():
            continue
        for raw_link in links_outside_fences(source):
            target = normalize_link(source, raw_link)
            if target is None:
                continue
            try:
                target.relative_to(ROOT)
            except ValueError:
                errors.append(f"{relative(source)} links outside repository: {raw_link}")
                continue
            if not target.exists():
                errors.append(f"broken link in {relative(source)}: {raw_link}")


def check_reachability(errors: list[str]) -> None:
    reachable: set[Path] = set()
    for source_name in ROOT_MAPS:
        source = ROOT / source_name
        if not source.is_file():
            continue
        for raw_link in links_outside_fences(source):
            target = normalize_link(source, raw_link)
            if target is not None:
                reachable.add(target.resolve())
    for path in CORE_REACHABLE_FILES:
        if (ROOT / path).resolve() not in reachable:
            errors.append(f"canonical document is not linked from a root/docs map: {path}")


def check_plan_hygiene(errors: list[str]) -> None:
    for bucket in ("active", "completed", "tech-debt"):
        directory = ROOT / "docs/exec-plans" / bucket
        plans = [path for path in directory.glob("*.md") if path.name != "README.md"] if directory.is_dir() else []
        if not plans:
            errors.append(f"plan bucket has no concrete plan: docs/exec-plans/{bucket}")


def main() -> int:
    errors: list[str] = []
    check_presence(errors)
    check_root_maps(errors)
    check_ai_design_ownership(errors)
    check_links(errors)
    check_reachability(errors)
    check_plan_hygiene(errors)

    if errors:
        print("documentation validation failed:")
        for error in sorted(set(errors)):
            print(f"- {error}")
        return 1

    print(f"documentation validation passed ({len(managed_markdown_files())} managed Markdown files)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
