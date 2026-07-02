#!/usr/bin/env python3
"""
test_config_reviewers.py — Pytest for ConfigModel, RuleChainsModel, ReviewersModel.

Tests model instantiation and basic role/data structure.
"""

import pytest
from PySide6.QtCore import Qt, QModelIndex
from PySide6.QtQml import QQmlApplicationEngine
from PySide6.QtGui import QGuiApplication

from eigenqt.rpc.client import RpcClient
from eigenqt.models.config import ConfigModel, RuleChainsModel
from eigenqt.models.reviewers import ReviewersModel


@pytest.fixture
def app():
    """Create QGuiApplication."""
    import sys
    app = QGuiApplication.instance() or QGuiApplication(sys.argv)
    yield app


@pytest.fixture
def client(app):
    """Create RpcClient (won't connect, but models can instantiate)."""
    return RpcClient()


def test_config_model_instantiation(client):
    """ConfigModel instantiates and exposes expected roles."""
    model = ConfigModel(client)
    assert model is not None
    roles = model.roleNames()
    assert b"key" in roles.values()
    assert b"desc" in roles.values()
    assert b"value" in roles.values()
    assert b"options" in roles.values()
    assert b"multi" in roles.values()
    assert b"allowEmpty" in roles.values()


def test_rule_chains_model_instantiation(client):
    """RuleChainsModel instantiates and exposes expected roles."""
    model = RuleChainsModel(client)
    assert model is not None
    roles = model.roleNames()
    assert b"roleName" in roles.values()
    assert b"desc" in roles.values()
    assert b"chain" in roles.values()
    assert b"custom" in roles.values()
    assert b"models" in roles.values()


def test_reviewers_model_instantiation(client):
    """ReviewersModel instantiates and exposes expected roles."""
    model = ReviewersModel(client)
    assert model is not None
    roles = model.roleNames()
    assert b"repo" in roles.values()
    assert b"paused" in roles.values()
    # Check properties
    assert model.available == False  # No connection yet
    assert model.count == 0
    assert model.paused_count == 0


if __name__ == "__main__":
    pytest.main([__file__, "-v"])
