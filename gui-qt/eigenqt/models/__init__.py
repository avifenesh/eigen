"""
models/ — Qt models bridging eigenqt.rpc to QML views.

TranscriptModel: session transcript with 16ms delta coalescing
SessionsModel: sessions list with live updates
ApprovalsModel: pending approvals per session
"""

from .sessions import SessionsModel
from .transcript import TranscriptModel
from .approvals import ApprovalsModel

__all__ = ["SessionsModel", "TranscriptModel", "ApprovalsModel"]
