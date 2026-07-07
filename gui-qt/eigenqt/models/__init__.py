"""
models/ — Qt models bridging eigenqt.rpc to QML views.

TranscriptModel: session transcript with 16ms delta coalescing
SessionsModel: sessions list with live updates
ApprovalsModel: pending approvals per session
SessionStateModel: session state for control strip (model, effort, perm, title, goal, provider modes)
CommandsModel: slash-command list for composer popup
ReplyWatcher: detects background session replies → desktop notify + unread marker
DiffModel: parse unified diffs into rows for the diff view
FileTreeModel: flatten nested file trees with expand state
TasksModel: background agents/tasks with polling and cancel
ObserveModel: observability summary with route-scoped polling
RoutingModel: model/provider routing catalog with route-health stats
MachinesModel: remote host list plus on-demand remote session drill-in
CronsModel: scheduled-work snapshot with route-scoped polling
PluginsModel: installed plugin and marketplace inventory
"""

from .sessions import SessionsModel
from .transcript import TranscriptModel
from .approvals import ApprovalsModel
from .session_state import SessionStateModel
from .commands import CommandsModel
from .reply_watch import ReplyWatcher
from .worktree import DiffModel, FileTreeModel
from .live import LiveSessionsModel
from .tasks import TasksModel
from .home import DashboardModel, FeedModel
from .board import BoardModel, KanbanModel
from .skills import SkillsModel, ProposalsModel
from .memory import MemoryModel
from .observe import ObserveModel
from .routing import RoutingModel
from .machines import MachinesModel
from .crons import CronsModel
from .plugins import PluginsModel

__all__ = [
    "SessionsModel",
    "TranscriptModel",
    "ApprovalsModel",
    "SessionStateModel",
    "CommandsModel",
    "ReplyWatcher",
    "DiffModel",
    "FileTreeModel",
    "LiveSessionsModel",
    "TasksModel",
    "DashboardModel",
    "FeedModel",
    "BoardModel",
    "KanbanModel",
    "SkillsModel",
    "ProposalsModel",
    "MemoryModel",
    "ObserveModel",
    "RoutingModel",
    "MachinesModel",
    "CronsModel",
    "PluginsModel",
]
