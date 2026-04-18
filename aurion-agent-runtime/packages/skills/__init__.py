"""Skills Loader — discovers and loads agent skills from YAML/Python.

Skills are reusable, composable capability modules that can be attached
to agents. Each skill defines:
  - A system prompt extension
  - A set of tools to activate
  - Optional MCP servers to connect
  - Optional files to mount in the sandbox
"""

from __future__ import annotations

import os
from pathlib import Path
from typing import Any

import structlog
import yaml

from packages.core.models import Skill, SkillCreate, ToolDefinition

logger = structlog.get_logger()


class SkillsLoader:
    """Loads and manages agent skills."""

    def __init__(self, skills_dir: str | None = None):
        self._skills_dir = skills_dir or os.environ.get("SKILLS_DIR", "./skills")
        self._skills: dict[str, Skill] = {}

    async def load_all(self) -> list[Skill]:
        """Discover and load all skills from the skills directory."""
        skills_path = Path(self._skills_dir)
        if not skills_path.exists():
            logger.info("skills_dir_not_found", path=self._skills_dir)
            return []

        for skill_file in skills_path.glob("*/skill.yaml"):
            try:
                skill = self._load_skill_file(skill_file)
                self._skills[skill.name] = skill
            except Exception as e:
                logger.warning("skill_load_failed", file=str(skill_file), error=str(e))

        # Also look for .yml extension
        for skill_file in skills_path.glob("*/skill.yml"):
            try:
                skill = self._load_skill_file(skill_file)
                if skill.name not in self._skills:
                    self._skills[skill.name] = skill
            except Exception as e:
                logger.warning("skill_load_failed", file=str(skill_file), error=str(e))

        logger.info("skills_loaded", count=len(self._skills))
        return list(self._skills.values())

    def _load_skill_file(self, path: Path) -> Skill:
        """Parse a skill.yaml file into a Skill model."""
        with open(path) as f:
            data = yaml.safe_load(f)

        # Read prompt file if referenced
        prompt = data.get("system_prompt", "")
        prompt_file = data.get("prompt_file")
        if prompt_file:
            prompt_path = path.parent / prompt_file
            if prompt_path.exists():
                prompt = prompt_path.read_text()

        # Parse tools
        tools = []
        for t in data.get("tools", []):
            tools.append(ToolDefinition(
                name=t["name"],
                description=t.get("description", ""),
                input_schema=t.get("input_schema", {}),
            ))

        return Skill(
            id=data.get("id", path.parent.name),
            name=data.get("name", path.parent.name),
            description=data.get("description", ""),
            system_prompt=prompt,
            tools=tools,
            mcp_servers=data.get("mcp_servers", []),
            files=data.get("files", {}),
            tags=data.get("tags", []),
        )

    def get_skill(self, name: str) -> Skill | None:
        """Get a skill by name."""
        return self._skills.get(name)

    def list_skills(self) -> list[Skill]:
        """List all loaded skills."""
        return list(self._skills.values())

    def apply_skills(
        self,
        skill_names: list[str],
        base_system_prompt: str,
        tools: list[ToolDefinition],
    ) -> tuple[str, list[ToolDefinition]]:
        """Apply selected skills to an agent, extending system prompt and tools.

        Returns the extended (system_prompt, tools) tuple.
        """
        prompt_parts = [base_system_prompt]
        combined_tools = list(tools)
        seen_tool_names = {t.name for t in tools}

        for name in skill_names:
            skill = self._skills.get(name)
            if not skill:
                logger.warning("skill_not_found", name=name)
                continue

            if skill.system_prompt:
                prompt_parts.append(f"\n## Skill: {skill.name}\n{skill.system_prompt}")

            for t in skill.tools:
                if t.name not in seen_tool_names:
                    combined_tools.append(t)
                    seen_tool_names.add(t.name)

        return "\n".join(prompt_parts), combined_tools
