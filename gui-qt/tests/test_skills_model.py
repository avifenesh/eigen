"""Test SkillsModel and ProposalsModel logic."""
import pytest
from unittest.mock import Mock
from eigenqt.models.skills import SkillsModel, ProposalsModel


@pytest.fixture
def fake_client():
    """Mock RPC client."""
    client = Mock()
    client.connected = Mock()
    client.connected.connect = Mock()
    client.call = Mock()
    return client


def test_skills_model_init(fake_client):
    """Test SkillsModel initialization."""
    model = SkillsModel(fake_client)

    assert model.rowCount() == 0
    assert model.load_error == ""
    assert fake_client.connected.connect.called


def test_skills_model_fetch_result(fake_client):
    """Test SkillsModel handles Skills RPC result."""
    model = SkillsModel(fake_client)

    # Simulate RPC result
    result = {
        "result": {
            "skills": [
                {"name": "skill1", "description": "desc1", "source": "user", "path": "/path/1"},
                {"name": "skill2", "description": "desc2", "source": "project", "path": "/path/2"},
            ],
            "proposals": []
        }
    }

    model._on_skills_result(result)

    assert model.rowCount() == 2

    # Check first skill
    idx = model.index(0, 0)
    assert model.data(idx, model.NameRole) == "skill1"
    assert model.data(idx, model.DescriptionRole) == "desc1"
    assert model.data(idx, model.SourceRole) == "user"
    assert model.data(idx, model.PathRole) == "/path/1"


def test_skills_model_surfaces_load_error_and_clears_on_retry(fake_client):
    """Failed Skills loads expose a retryable load_error."""
    callbacks = []
    fake_client.call.side_effect = lambda method, callback=None, **kwargs: callbacks.append(callback)
    model = SkillsModel(fake_client)

    model.refresh()
    callbacks[-1]({"error": {"message": "daemon offline"}})

    assert model.load_error == "daemon offline"
    assert model.rowCount() == 0

    model.refresh()

    assert model.load_error == ""
    callbacks[-1]({
        "result": {
            "skills": [{"name": "qt-proof", "description": "fresh", "source": "project", "path": "/fresh"}],
            "proposals": [],
        }
    })
    assert model.rowCount() == 1
    idx = model.index(0, 0)
    assert model.data(idx, model.NameRole) == "qt-proof"
    assert model.load_error == ""


def test_skills_model_surfaces_load_error_without_dropping_rows(fake_client):
    """Skills RPC errors should be visible without clearing the last good list."""
    model = SkillsModel(fake_client)
    model._on_skills_result({
        "result": {
            "skills": [{"name": "skill1", "description": "desc1", "source": "user", "path": "/path/1"}],
            "proposals": [],
        }
    })

    model._on_skills_result({"error": {"message": "daemon offline"}})

    assert model.load_error == "daemon offline"
    assert model.rowCount() == 1

    callbacks = []
    fake_client.call.side_effect = lambda method, callback=None, **kwargs: callbacks.append(callback)
    model.refresh()

    assert model.load_error == ""
    assert fake_client.call.call_args[0][0] == "Skills"


def test_skills_model_ignores_stale_fetch_callbacks(fake_client):
    """Older Skills replies must not overwrite a newer installed-skills refresh."""
    callbacks = []
    fake_client.call.side_effect = lambda method, callback=None, **kwargs: callbacks.append(callback)
    model = SkillsModel(fake_client)

    model.refresh()
    first_callback = callbacks[-1]
    model.refresh()
    second_callback = callbacks[-1]

    second_callback({
        "result": {
            "skills": [{"name": "qt-proof", "description": "fresh", "source": "project", "path": "/fresh"}],
            "proposals": [],
        }
    })
    assert model.rowCount() == 1
    idx = model.index(0, 0)
    assert model.data(idx, model.NameRole) == "qt-proof"
    assert model.data(idx, model.PathRole) == "/fresh"

    first_callback({
        "result": {
            "skills": [{"name": "old-proof", "description": "stale", "source": "user", "path": "/stale"}],
            "proposals": [],
        }
    })
    assert model.rowCount() == 1
    idx = model.index(0, 0)
    assert model.data(idx, model.NameRole) == "qt-proof"
    assert model.data(idx, model.PathRole) == "/fresh"


def test_proposals_model_init(fake_client):
    """Test ProposalsModel initialization."""
    model = ProposalsModel(fake_client)

    assert model.rowCount() == 0
    assert model.load_error == ""
    assert fake_client.connected.connect.called


def test_proposals_model_fetch_result(fake_client):
    """Test ProposalsModel handles Skills RPC result (extracts proposals)."""
    model = ProposalsModel(fake_client)

    # Simulate RPC result with proposals
    result = {
        "result": {
            "skills": [],
            "proposals": [
                {"name": "prop1", "description": "dream skill 1", "path": "/dream/1"},
                {"name": "prop2", "description": "dream skill 2", "path": "/dream/2"},
            ]
        }
    }

    model._on_skills_result(result)

    assert model.rowCount() == 2

    # Check first proposal
    idx = model.index(0, 0)
    assert model.data(idx, model.NameRole) == "prop1"
    assert model.data(idx, model.DescriptionRole) == "dream skill 1"
    assert model.data(idx, model.PathRole) == "/dream/1"


def test_proposals_model_surfaces_load_error_and_clears_on_retry(fake_client):
    """Failed proposal refreshes expose a retryable load_error."""
    callbacks = []
    fake_client.call.side_effect = lambda method, callback=None, **kwargs: callbacks.append(callback)
    model = ProposalsModel(fake_client)

    model.refresh()
    callbacks[-1]({"error": "daemon offline"})

    assert model.load_error == "daemon offline"
    assert model.rowCount() == 0

    model.refresh()

    assert model.load_error == ""
    callbacks[-1]({
        "result": {
            "skills": [],
            "proposals": [{"name": "qt-qa", "description": "fresh proposal", "path": "/fresh"}],
        }
    })
    assert model.rowCount() == 1
    idx = model.index(0, 0)
    assert model.data(idx, model.NameRole) == "qt-qa"
    assert model.load_error == ""


def test_proposals_model_surfaces_load_error_without_dropping_rows(fake_client):
    """Proposal load errors should be visible without clearing the proposal list."""
    model = ProposalsModel(fake_client)
    model._on_skills_result({
        "result": {
            "skills": [],
            "proposals": [{"name": "prop1", "description": "dream skill 1", "path": "/dream/1"}],
        }
    })

    model._on_skills_result({"error": "proposal daemon offline"})

    assert model.load_error == "proposal daemon offline"
    assert model.rowCount() == 1

    callbacks = []
    fake_client.call.side_effect = lambda method, callback=None, **kwargs: callbacks.append(callback)
    model.refresh()

    assert model.load_error == ""
    assert fake_client.call.call_args[0][0] == "Skills"


def test_proposals_model_ignores_stale_fetch_callbacks(fake_client):
    """Older Skills replies must not overwrite a newer proposals refresh."""
    callbacks = []
    fake_client.call.side_effect = lambda method, callback=None, **kwargs: callbacks.append(callback)
    model = ProposalsModel(fake_client)

    model.refresh()
    first_callback = callbacks[-1]
    model.refresh()
    second_callback = callbacks[-1]

    second_callback({
        "result": {
            "skills": [],
            "proposals": [{"name": "qt-qa", "description": "fresh proposal", "path": "/fresh"}],
        }
    })
    assert model.rowCount() == 1
    idx = model.index(0, 0)
    assert model.data(idx, model.NameRole) == "qt-qa"
    assert model.data(idx, model.PathRole) == "/fresh"

    first_callback({
        "result": {
            "skills": [],
            "proposals": [{"name": "old-qa", "description": "stale proposal", "path": "/stale"}],
        }
    })
    assert model.rowCount() == 1
    idx = model.index(0, 0)
    assert model.data(idx, model.NameRole) == "qt-qa"
    assert model.data(idx, model.PathRole) == "/fresh"


def test_proposals_model_accept(fake_client):
    """Test ProposalsModel.accept() removes row only after AcceptSkill succeeds."""
    model = ProposalsModel(fake_client)
    callbacks = []
    fake_client.call.side_effect = lambda *args, callback=None, **kwargs: callbacks.append(callback)
    events = []
    model.proposal_done.connect(lambda name, action, success, error: events.append((name, action, success, error)))

    # Seed proposals
    result = {
        "result": {
            "skills": [],
            "proposals": [
                {"name": "prop1", "description": "dream skill 1", "path": "/dream/1"},
                {"name": "prop2", "description": "dream skill 2", "path": "/dream/2"},
            ]
        }
    }
    model._on_skills_result(result)
    assert model.rowCount() == 2

    # Accept prop1
    model.accept("prop1")

    # Still present while the RPC is in flight.
    assert model.rowCount() == 2

    callbacks[-1]({"result": True})

    # Check row removed after success.
    assert model.rowCount() == 1
    idx = model.index(0, 0)
    assert model.data(idx, model.NameRole) == "prop2"
    assert events == [("prop1", "accept", True, "")]

    # Check RPC called (params passed as kwargs)
    fake_client.call.assert_called_once()
    call_args = fake_client.call.call_args
    assert call_args[0][0] == "AcceptSkill"
    assert call_args[0][1] == "prop1"  # positional arg, not params kwarg


def test_proposals_model_reject(fake_client):
    """Test ProposalsModel.reject() removes row only after RejectSkill succeeds."""
    model = ProposalsModel(fake_client)
    callbacks = []
    fake_client.call.side_effect = lambda *args, callback=None, **kwargs: callbacks.append(callback)
    events = []
    model.proposal_done.connect(lambda name, action, success, error: events.append((name, action, success, error)))

    # Seed proposals
    result = {
        "result": {
            "skills": [],
            "proposals": [
                {"name": "prop1", "description": "dream skill 1", "path": "/dream/1"},
                {"name": "prop2", "description": "dream skill 2", "path": "/dream/2"},
            ]
        }
    }
    model._on_skills_result(result)
    assert model.rowCount() == 2

    # Reject prop2
    model.reject("prop2")

    assert model.rowCount() == 2

    callbacks[-1]({"result": True})

    # Check row removed after success.
    assert model.rowCount() == 1
    idx = model.index(0, 0)
    assert model.data(idx, model.NameRole) == "prop1"
    assert events == [("prop2", "reject", True, "")]

    # Check RPC called (params passed as kwargs)
    fake_client.call.assert_called_once()
    call_args = fake_client.call.call_args
    assert call_args[0][0] == "RejectSkill"
    assert call_args[0][1] == "prop2"  # positional arg, not params kwarg


def test_proposals_model_action_failure_keeps_row(fake_client):
    """Failed proposal actions keep the proposal and report a displayable error."""
    model = ProposalsModel(fake_client)
    callbacks = []
    fake_client.call.side_effect = lambda *args, callback=None, **kwargs: callbacks.append(callback)
    events = []
    model.proposal_done.connect(lambda name, action, success, error: events.append((name, action, success, error)))

    model._on_skills_result({
        "result": {
            "skills": [],
            "proposals": [
                {"name": "prop1", "description": "dream skill 1", "path": "/dream/1"},
            ],
        }
    })

    model.accept("prop1")
    callbacks[-1]({"error": {"message": "write denied"}})

    assert model.rowCount() == 1
    idx = model.index(0, 0)
    assert model.data(idx, model.NameRole) == "prop1"
    assert events == [("prop1", "accept", False, "write denied")]


def test_proposals_model_pending_actions_do_not_duplicate(fake_client):
    """A pending proposal action blocks duplicate and opposite actions for that proposal."""
    model = ProposalsModel(fake_client)
    callbacks = []
    fake_client.call.side_effect = lambda *args, callback=None, **kwargs: callbacks.append(callback)

    model._on_skills_result({
        "result": {
            "skills": [],
            "proposals": [
                {"name": "prop1", "description": "dream skill 1", "path": "/dream/1"},
                {"name": "prop2", "description": "dream skill 2", "path": "/dream/2"},
            ],
        }
    })

    model.accept("prop1")
    model.accept("prop1")
    model.reject("prop1")
    model.reject("prop2")

    assert [call.args for call in fake_client.call.call_args_list] == [
        ("AcceptSkill", "prop1"),
        ("RejectSkill", "prop2"),
    ]

    callbacks[0]({"error": "accept denied"})
    model.reject("prop1")

    assert fake_client.call.call_args_list[-1].args == ("RejectSkill", "prop1")
