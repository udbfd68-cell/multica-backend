"""Tests for sandbox factory."""

import pytest
from unittest.mock import patch

from packages.sandbox.factory import get_provider
from packages.sandbox.docker_sandbox import DockerSandboxProvider
from packages.sandbox.e2b_sandbox import E2BSandboxProvider
from packages.sandbox.daytona_sandbox import DaytonaSandboxProvider


class TestSandboxFactory:
    def test_default_is_docker(self):
        provider = get_provider()
        assert isinstance(provider, DockerSandboxProvider)

    def test_docker_provider(self):
        provider = get_provider("docker")
        assert isinstance(provider, DockerSandboxProvider)

    def test_e2b_provider(self):
        provider = get_provider("e2b")
        assert isinstance(provider, E2BSandboxProvider)

    def test_daytona_provider(self):
        provider = get_provider("daytona")
        assert isinstance(provider, DaytonaSandboxProvider)

    def test_unknown_provider_raises(self):
        with pytest.raises(ValueError, match="Unknown sandbox provider"):
            get_provider("unknown")

    def test_env_var_override(self):
        with patch.dict("os.environ", {"SANDBOX_PROVIDER": "e2b"}):
            provider = get_provider()
            assert isinstance(provider, E2BSandboxProvider)
