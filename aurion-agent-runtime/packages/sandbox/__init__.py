"""Sandbox package — pluggable sandbox backends for agent code execution."""
from packages.sandbox.base import Sandbox, SandboxProvider
from packages.sandbox.factory import create_sandbox

__all__ = ["Sandbox", "SandboxProvider", "create_sandbox"]
