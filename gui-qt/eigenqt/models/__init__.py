"""
models/ — Qt models bridging eigenqt.rpc to QML views.

TranscriptModel: session transcript with 16ms delta coalescing
SessionsModel: sessions list with live updates
ApprovalsModel: pending approvals per session
SessionStateModel: session state for control strip (model, effort, perm, title, goal)
CommandsModel: slash-command list for composer popup
ReplyWatcher: detects background session replies → desktop notify + unread marker
"""

from .sessions import SessionsModel
from .transcript import TranscriptModel
from .approvals import ApprovalsModel
from .session_state import SessionStateModel
from .commands import CommandsModel
from .reply_watch import ReplyWatcher

__all__ = [
    "SessionsModel",
    "TranscriptModel",
    "ApprovalsModel",
    "SessionStateModel",
    "CommandsModel",
    "ReplyWatcher",
]
