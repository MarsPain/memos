#!/usr/bin/env python3
"""Validate the repository's capability-aware documentation context system."""

from __future__ import annotations

import re
import sys
from dataclasses import dataclass
from pathlib import Path
from urllib.parse import unquote


ROOT = Path(__file__).resolve().parents[1]
AGENT_MAP_BUDGET = 140
MARKDOWN_LINK = re.compile(r"(?<!!)\[[^\]]+\]\(([^)]+)\)")
INLINE_CODE = re.compile(r"`[^`]*`")
EXTERNAL_SCHEMES = ("http://", "https://", "mailto:", "tel:", "data:")
EXCLUDED_PARTS = {
    ".git",
    ".pnpm-store",
    ".worktrees",
    "__pycache__",
    "build",
    "dist",
    "node_modules",
    "vendor",
}
@dataclass(frozen=True)
class ArtifactRegistry:
    root: Path
    human_entrypoint: Path
    agent_entrypoint: Path | None
    agent_entrypoints: tuple[Path, ...]
    architecture_entrypoint: Path
    docs_index: Path | None
    context: Path | None
    context_map: Path | None
    context_glossaries: tuple[Path, ...]
    adr_dirs: tuple[Path, ...]
    agent_configs: tuple[Path, ...]
    issue_tracker_kind: str | None
    markdown_files: tuple[Path, ...]


def relative(root: Path, path: Path) -> str:
    return path.relative_to(root).as_posix()


def is_excluded(root: Path, path: Path) -> bool:
    return any(part in EXCLUDED_PARTS for part in path.relative_to(root).parts)


def discover_markdown_files(root: Path, issue_tracker_kind: str | None = None) -> tuple[Path, ...]:
    files = {path for path in root.glob("*.md") if path.is_file()}
    docs = root / "docs"
    if docs.is_dir():
        files.update(path for path in docs.rglob("*.md") if path.is_file() and not is_excluded(root, path))
    scratch = root / ".scratch"
    if issue_tracker_kind == "local" and scratch.is_dir():
        files.update(path for path in scratch.rglob("*.md") if path.is_file() and not is_excluded(root, path))
    files.update(
        path
        for name in ("CONTEXT.md", "CONTEXT-MAP.md")
        for path in root.rglob(name)
        if path.is_file() and not is_excluded(root, path)
    )
    files.update(
        path
        for path in root.rglob("docs/adr/*.md")
        if path.is_file() and not is_excluded(root, path)
    )
    return tuple(sorted(files))


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
        if not in_fence:
            links.extend(MARKDOWN_LINK.findall(INLINE_CODE.sub("code", line)))
    return links


def normalize_link(root: Path, source: Path, raw_link: str) -> Path | None:
    link = raw_link.strip()
    if link.startswith("<") and link.endswith(">"):
        link = link[1:-1]
    if not link or link.startswith("#") or link.startswith(EXTERNAL_SCHEMES):
        return None
    link = unquote(link.split("#", 1)[0].split("?", 1)[0])
    if not link or link.startswith("/"):
        # Root-relative links are normally application routes, not repository files.
        return None
    return (source.parent / link).resolve()


def link_targets(root: Path, source: Path) -> set[Path]:
    targets: set[Path] = set()
    if not source.is_file():
        return targets
    for raw_link in links_outside_fences(source):
        target = normalize_link(root, source, raw_link)
        if target is not None:
            targets.add(target)
    return targets


def target_is_reachable(root: Path, target: Path, sources: tuple[Path, ...], *, area: bool = False) -> bool:
    resolved_target = target.resolve()
    for source in sources:
        for linked in link_targets(root, source):
            if linked == resolved_target:
                return True
            if area:
                try:
                    linked.relative_to(resolved_target)
                    return True
                except ValueError:
                    pass
    return False


def discover_registry(root: Path) -> ArtifactRegistry:
    agents = tuple(path for path in (root / "AGENTS.md", root / "CLAUDE.md") if path.is_file())
    agent_entrypoint = root / "AGENTS.md" if (root / "AGENTS.md").is_file() else (agents[0] if agents else None)
    issue_tracker = root / "docs/agents/issue-tracker.md"
    issue_tracker_kind: str | None = None
    if issue_tracker.is_file():
        content = issue_tracker.read_text(encoding="utf-8")
        issue_tracker_kind = "local" if re.search(r"(?mi)^#\s+Issue tracker:\s*Local Markdown\s*$", content) else "external"

    context = root / "CONTEXT.md" if (root / "CONTEXT.md").is_file() else None
    context_map = root / "CONTEXT-MAP.md" if (root / "CONTEXT-MAP.md").is_file() else None
    context_glossaries = tuple(
        sorted(
            path
            for path in root.rglob("CONTEXT.md")
            if path.is_file() and path != context and not is_excluded(root, path)
        )
    )
    adr_dirs = tuple(
        sorted(
            path
            for path in root.rglob("adr")
            if path.is_dir() and path.parent.name == "docs" and not is_excluded(root, path)
        )
    )
    agent_config_dir = root / "docs/agents"
    agent_configs = tuple(sorted(agent_config_dir.glob("*.md"))) if agent_config_dir.is_dir() else ()
    docs_index = root / "docs/README.md" if (root / "docs/README.md").is_file() else None

    return ArtifactRegistry(
        root=root,
        human_entrypoint=root / "README.md",
        agent_entrypoint=agent_entrypoint,
        agent_entrypoints=agents,
        architecture_entrypoint=root / "ARCHITECTURE.md",
        docs_index=docs_index,
        context=context,
        context_map=context_map,
        context_glossaries=context_glossaries,
        adr_dirs=adr_dirs,
        agent_configs=agent_configs,
        issue_tracker_kind=issue_tracker_kind,
        markdown_files=discover_markdown_files(root, issue_tracker_kind),
    )


def check_entrypoints(registry: ArtifactRegistry, errors: list[str]) -> None:
    root = registry.root
    for path, role in (
        (registry.human_entrypoint, "human entrypoint"),
        (registry.architecture_entrypoint, "architecture entrypoint"),
    ):
        if not path.is_file():
            errors.append(f"missing {role}: {relative(root, path)}")
    if registry.agent_entrypoint is None:
        errors.append("missing cross-agent entrypoint: AGENTS.md or declared alternative")
        return

    line_count = len(registry.agent_entrypoint.read_text(encoding="utf-8").splitlines())
    if line_count > AGENT_MAP_BUDGET:
        errors.append(
            f"{relative(root, registry.agent_entrypoint)} exceeds {AGENT_MAP_BUDGET}-line map budget: {line_count}"
        )

    required_targets = [registry.architecture_entrypoint]
    if registry.docs_index is not None:
        required_targets.append(registry.docs_index)
    entrypoint_links = link_targets(root, registry.agent_entrypoint)
    for target in required_targets:
        if target.resolve() not in entrypoint_links:
            errors.append(
                f"{relative(root, registry.agent_entrypoint)} does not link to {relative(root, target)}"
            )

    if len(registry.agent_entrypoints) > 1:
        shared_owners = [
            path
            for path in registry.agent_entrypoints
            if re.search(r"(?m)^##\s+Agent skills\s*$", path.read_text(encoding="utf-8"))
        ]
        if len(shared_owners) > 1:
            names = ", ".join(relative(root, path) for path in shared_owners)
            errors.append(f"shared Agent skills block has multiple owners: {names}")


def check_docs_index(registry: ArtifactRegistry, errors: list[str]) -> None:
    root = registry.root
    docs = root / "docs"
    docs_files = tuple(path for path in registry.markdown_files if docs in path.parents)
    if docs_files and registry.docs_index is None:
        errors.append("missing docs index: docs/README.md")
        return
    if registry.docs_index is None:
        return

    index_sources = (registry.docs_index,)
    for path in sorted(docs.glob("*.md")):
        if path.name == "README.md":
            continue
        if not target_is_reachable(root, path, index_sources):
            errors.append(f"top-level canonical document is not linked from docs/README.md: {relative(root, path)}")

    for area in sorted(path for path in docs.iterdir() if path.is_dir()):
        if not any(path.is_file() for path in area.rglob("*.md")):
            continue
        if not target_is_reachable(root, area, index_sources, area=True):
            errors.append(f"documentation area is not linked from docs/README.md: {relative(root, area)}")


def check_domain_context(registry: ArtifactRegistry, errors: list[str]) -> None:
    root = registry.root
    if registry.context is not None and registry.context_map is not None:
        errors.append("CONTEXT.md and CONTEXT-MAP.md cannot both be authoritative")
    if registry.context_map is None:
        return

    mapped = {
        target
        for target in link_targets(root, registry.context_map)
        if target.name == "CONTEXT.md"
    }
    if not mapped:
        errors.append("CONTEXT-MAP.md does not link to any context glossary")
    for glossary in registry.context_glossaries:
        if glossary.resolve() not in mapped:
            errors.append(f"context glossary is not linked from CONTEXT-MAP.md: {relative(root, glossary)}")


def check_adrs(registry: ArtifactRegistry, errors: list[str]) -> None:
    root = registry.root
    for adr_dir in registry.adr_dirs:
        adrs = tuple(path for path in adr_dir.glob("*.md") if path.name != "README.md")
        if not adrs:
            continue
        if adr_dir == root / "docs/adr":
            sources = tuple(path for path in (registry.architecture_entrypoint, registry.docs_index) if path is not None)
        else:
            context_root = adr_dir.parent.parent
            context_glossary = context_root / "CONTEXT.md"
            sources = tuple(path for path in (context_glossary, registry.docs_index) if path is not None and path.is_file())
        if not target_is_reachable(root, adr_dir, sources, area=True):
            errors.append(f"ADR area is not reachable from its owning context: {relative(root, adr_dir)}")

        for adr in adrs:
            content = adr.read_text(encoding="utf-8")
            status = re.search(r"(?mi)^\s*(?:-\s*)?\*{0,2}Status\*{0,2}:\s*(.+)$", content)
            if status is None:
                errors.append(f"ADR has no explicit status: {relative(root, adr)}")
            elif "supersed" in status.group(1).lower() and not MARKDOWN_LINK.search(status.group(1)):
                errors.append(f"superseded ADR status has no successor link: {relative(root, adr)}")


def check_agent_configuration(registry: ArtifactRegistry, errors: list[str]) -> None:
    root = registry.root
    if not registry.agent_configs:
        return
    if registry.agent_entrypoint is None or registry.docs_index is None:
        return

    agent_links = link_targets(root, registry.agent_entrypoint)
    index_links = link_targets(root, registry.docs_index)
    for config in registry.agent_configs:
        if config.resolve() not in agent_links:
            errors.append(f"agent configuration is not linked from the agent entrypoint: {relative(root, config)}")
        if config.resolve() not in index_links:
            errors.append(f"agent configuration is not linked from docs/README.md: {relative(root, config)}")

    tracker = root / "docs/agents/issue-tracker.md"
    if not tracker.is_file():
        return
    tracker_content = tracker.read_text(encoding="utf-8")
    product_specs = root / "docs/product-specs"
    if product_specs.is_dir() and any(product_specs.glob("*.md")) and re.search(
        r"\.scratch/[^\n`]*spec\.md", tracker_content
    ):
        errors.append("issue tracker config assigns specs to .scratch while docs/product-specs is active")

    scratch = root / ".scratch"
    scratch_files = tuple(scratch.rglob("*.md")) if scratch.is_dir() else ()
    if registry.issue_tracker_kind == "external" and scratch_files:
        errors.append("external issue tracker is configured but .scratch contains competing Markdown issues")
    if registry.issue_tracker_kind != "local":
        return

    for path in scratch_files:
        if path.name == "spec.md" or "specs" in path.parts:
            errors.append(f"product spec is stored in the issue tracker instead of docs/product-specs: {relative(root, path)}")
        if "issues" not in path.parts:
            continue
        content = path.read_text(encoding="utf-8")
        if not re.search(r"(?mi)^Status:\s*\S+", content):
            errors.append(f"implementation issue has no Status field: {relative(root, path)}")
        blocked_by = re.search(r"(?mi)^Blocked by:\s*(.+)$", content)
        if blocked_by is None:
            errors.append(f"implementation issue has no Blocked by field: {relative(root, path)}")
        elif blocked_by.group(1).strip().lower() != "none":
            issue_number = path.name.split("-", 1)[0]
            for dependency in (value.strip() for value in blocked_by.group(1).split(",")):
                if not re.fullmatch(r"\d{2}", dependency):
                    errors.append(f"implementation issue has invalid dependency {dependency!r}: {relative(root, path)}")
                    continue
                if dependency == issue_number:
                    errors.append(f"implementation issue depends on itself: {relative(root, path)}")
                    continue
                if not any(path.parent.glob(f"{dependency}-*.md")):
                    errors.append(
                        f"implementation issue dependency {dependency} does not resolve to a sibling issue: {relative(root, path)}"
                    )
        if not re.search(r"(?mi)^Parent spec:\s*", content):
            errors.append(f"implementation issue has no Parent spec field: {relative(root, path)}")
            continue
        spec_links = []
        for raw_link in links_outside_fences(path):
            target = normalize_link(root, path, raw_link)
            if target is None:
                continue
            try:
                target.relative_to(product_specs.resolve())
                spec_links.append(target)
            except ValueError:
                pass
        if not spec_links:
            errors.append(f"implementation issue does not link to docs/product-specs: {relative(root, path)}")


def check_links(registry: ArtifactRegistry, errors: list[str]) -> None:
    root = registry.root
    for source in registry.markdown_files:
        for raw_link in links_outside_fences(source):
            target = normalize_link(root, source, raw_link)
            if target is None:
                continue
            try:
                target.relative_to(root)
            except ValueError:
                errors.append(f"{relative(root, source)} links outside repository: {raw_link}")
                continue
            if not target.exists():
                errors.append(f"broken link in {relative(root, source)}: {raw_link}")


def check_work_artifact_boundaries(registry: ArtifactRegistry, errors: list[str]) -> None:
    root = registry.root
    if registry.issue_tracker_kind is None:
        return

    plans = root / "docs/PLANS.md"
    if plans.is_file():
        content = plans.read_text(encoding="utf-8")
        for heading in ("Active", "Tech Debt"):
            if re.search(rf"(?mi)^##\s+{re.escape(heading)}\s*$", content):
                errors.append(f"docs/PLANS.md contains live {heading.lower()} state owned by the issue tracker")
        if "does not own work status" not in content:
            errors.append("docs/PLANS.md does not declare its historical, non-authoritative status")

    roadmap = root / "docs/ROADMAP.md"
    if roadmap.is_file() and re.search(r"(?mi)^Status:\s*", roadmap.read_text(encoding="utf-8")):
        errors.append("docs/ROADMAP.md contains live status owned by the issue tracker")

    design_dir = root / "docs/design-docs"
    if design_dir.is_dir():
        for design in design_dir.glob("*.md"):
            content = design.read_text(encoding="utf-8")
            if re.search(r"(?i)implementation (?:has )?not started|separate execution plan", content):
                errors.append(f"design doc contains live execution-plan state: {relative(root, design)}")

    for path in (root / "AGENTS.md", root / "docs/README.md"):
        if not path.is_file():
            continue
        content = path.read_text(encoding="utf-8")
        if re.search(r"(?i)exec-plans/.{0,40}(?:own|track).{0,30}(?:current|active|debt)", content):
            errors.append(f"{relative(root, path)} treats legacy exec-plans as a live source of truth")


def validate_repository(root: Path) -> tuple[ArtifactRegistry, list[str]]:
    registry = discover_registry(root.resolve())
    errors: list[str] = []
    check_entrypoints(registry, errors)
    check_docs_index(registry, errors)
    check_domain_context(registry, errors)
    check_adrs(registry, errors)
    check_agent_configuration(registry, errors)
    check_links(registry, errors)
    check_work_artifact_boundaries(registry, errors)
    return registry, sorted(set(errors))


def main() -> int:
    registry, errors = validate_repository(ROOT)
    if errors:
        print("documentation validation failed:")
        for error in errors:
            print(f"- {error}")
        return 1

    print(f"documentation validation passed ({len(registry.markdown_files)} managed Markdown files)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
