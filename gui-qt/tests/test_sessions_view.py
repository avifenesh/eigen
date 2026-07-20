import os
import subprocess
import sys
import textwrap
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def test_sessions_view_remove_and_prune_actions_are_clickable():
    script = r"""
from pathlib import Path

from PySide6.QtCore import (
    QAbstractListModel,
    QModelIndex,
    QPoint,
    QPointF,
    QSize,
    QTimer,
    QUrl,
    Qt,
    QtMsgType,
    Property,
    Signal,
    Slot,
    qInstallMessageHandler,
)
from PySide6.QtGui import QGuiApplication
from PySide6.QtQuick import QQuickView
from PySide6.QtQuickControls2 import QQuickStyle
from PySide6.QtTest import QTest


ROOT = Path.cwd()
SIZE = QSize(420, 800)
ISSUE_MARKERS = (
    "ReferenceError",
    "TypeError",
    "Unable to assign",
    "Cannot assign",
    "Cannot read property",
)
EXPECTED_PLACEHOLDER_COLOR = "#65716f"


def color_name(value):
    if hasattr(value, "name"):
        return value.name().lower()
    return str(value).lower()


class FakeSessionsModel(QAbstractListModel):
    IdRole = Qt.UserRole + 1
    TitleRole = Qt.UserRole + 2
    DirRole = Qt.UserRole + 3
    ModelRole = Qt.UserRole + 4
    StatusRole = Qt.UserRole + 5
    TurnsRole = Qt.UserRole + 6
    UpdatedRole = Qt.UserRole + 7
    UnreadRole = Qt.UserRole + 8

    pruningChanged = Signal()
    removingChanged = Signal()
    exportingChanged = Signal()
    actionErrorChanged = Signal()
    actionMessageChanged = Signal()
    queryChanged = Signal()
    totalCountChanged = Signal()
    filteredCountChanged = Signal()

    def __init__(self):
        super().__init__()
        self.calls = []
        self._all_rows = [
            {
                "id": "s-empty",
                "title": "Empty scratch",
                "dir": "/repo/eigen",
                "model": "gpt-5",
                "status": "idle",
                "turns": 0,
                "updated": 1_783_200_000_000,
            },
            {
                "id": "s-run",
                "title": "Qt sessions",
                "dir": "/repo/eigen/gui-qt",
                "model": "local-qwen",
                "status": "working",
                "turns": 2,
                "updated": 1_783_210_000_000_000_000,
            },
        ]
        self._rows = list(self._all_rows)
        self._pruning = False
        self._removing = set()
        self._exporting = set()
        self._action_error = ""
        self._action_message = ""
        self._query = ""
        self.remove_delay_ms = 0

    def roleNames(self):
        return {
            self.IdRole: b"sessionId",
            self.TitleRole: b"title",
            self.DirRole: b"dir",
            self.ModelRole: b"modelName",
            self.StatusRole: b"status",
            self.TurnsRole: b"turns",
            self.UpdatedRole: b"updated",
            self.UnreadRole: b"unread",
        }

    def rowCount(self, parent=QModelIndex()):
        return 0 if parent.isValid() else len(self._rows)

    def data(self, index, role=Qt.DisplayRole):
        if not index.isValid() or index.row() >= len(self._rows):
            return ""
        row = self._rows[index.row()]
        if role == self.IdRole:
            return row["id"]
        if role == self.TitleRole:
            return row["title"]
        if role == self.DirRole:
            return row["dir"]
        if role == self.ModelRole:
            return row["model"]
        if role == self.StatusRole:
            return row["status"]
        if role == self.TurnsRole:
            return row["turns"]
        if role == self.UpdatedRole:
            return row["updated"]
        if role == self.UnreadRole:
            return False
        return ""

    @Property(bool, notify=pruningChanged)
    def pruning(self):
        return self._pruning

    @Property(list, notify=removingChanged)
    def removing(self):
        return sorted(self._removing)

    @Property(list, notify=exportingChanged)
    def exporting(self):
        return sorted(self._exporting)

    @Property(str, notify=actionErrorChanged)
    def actionError(self):
        return self._action_error

    @Property(str, notify=actionMessageChanged)
    def actionMessage(self):
        return self._action_message

    @Property(str, notify=queryChanged)
    def query(self):
        return self._query

    @query.setter
    def query(self, value):
        value = (value or "").strip()
        if value == self._query:
            return
        self._query = value
        self.queryChanged.emit()
        self._apply_filter()

    @Property(int, notify=totalCountChanged)
    def totalCount(self):
        return len(self._all_rows)

    @Property(int, notify=filteredCountChanged)
    def filteredCount(self):
        return len(self._rows)

    @Slot()
    def refresh(self):
        self.calls.append(("Sessions",))

    @Slot(str, result=bool)
    def isRemoving(self, session_id):
        return session_id in self._removing

    @Slot(str, result=bool)
    def isExporting(self, session_id):
        return session_id in self._exporting

    @Slot(str)
    def exportSession(self, session_id):
        self.calls.append(("ExportSession", session_id))
        self._action_message = ""
        self.actionMessageChanged.emit()
        self._exporting.add(session_id)
        self.exportingChanged.emit()
        QTimer.singleShot(0, lambda: self._finish_export(session_id))

    @Slot(str)
    def removeSession(self, session_id):
        self.calls.append(("RemoveSession", session_id))
        self._removing.add(session_id)
        self.removingChanged.emit()
        QTimer.singleShot(self.remove_delay_ms, lambda: self._finish_remove(session_id))

    @Slot()
    def pruneSessions(self):
        self.calls.append(("PruneSessions",))
        self._pruning = True
        self.pruningChanged.emit()
        QTimer.singleShot(0, self._finish_prune)

    @Slot()
    def clearActionError(self):
        self._action_error = ""
        self.actionErrorChanged.emit()

    @Slot()
    def clearActionMessage(self):
        self._action_message = ""
        self.actionMessageChanged.emit()

    def set_action_error(self, text):
        self._action_error = text
        self.actionErrorChanged.emit()

    def _finish_export(self, session_id):
        self._exporting.discard(session_id)
        self.exportingChanged.emit()
        self._action_message = f"Exported {session_id} to /home/user/eigen-exports/{session_id}.jsonl"
        self.actionMessageChanged.emit()

    def _finish_remove(self, session_id):
        self._removing.discard(session_id)
        self.removingChanged.emit()
        self._all_rows = [data for data in self._all_rows if data["id"] != session_id]
        self.totalCountChanged.emit()
        for row, data in enumerate(self._rows):
            if data["id"] == session_id:
                self.beginRemoveRows(QModelIndex(), row, row)
                del self._rows[row]
                self.endRemoveRows()
                self.filteredCountChanged.emit()
                return

    def _finish_prune(self):
        self._pruning = False
        self.pruningChanged.emit()
        kept_all = [row for row in self._all_rows if row["turns"] > 0]
        count = len(self._all_rows) - len(kept_all)
        if count == 0:
            self._action_message = "No empty sessions to prune"
            self.actionMessageChanged.emit()
            return
        self._all_rows = kept_all
        self.totalCountChanged.emit()
        self._action_message = f"Pruned {count} empty session" + ("" if count == 1 else "s")
        self.actionMessageChanged.emit()
        kept = self._filtered_rows()
        self.beginResetModel()
        self._rows = kept
        self.endResetModel()
        self.filteredCountChanged.emit()

    def _filtered_rows(self):
        query = self._query.lower()
        if not query:
            return list(self._all_rows)
        return [
            row
            for row in self._all_rows
            if query in row["title"].lower()
            or query in row["dir"].lower()
            or query in row["model"].lower()
            or query in row["id"].lower()
        ]

    def _apply_filter(self):
        self.beginResetModel()
        self._rows = self._filtered_rows()
        self.endResetModel()
        self.filteredCountChanged.emit()


def pump(app, rounds=12):
    for _ in range(rounds):
        app.processEvents()


def item_visibility_score(item):
    width = float(item.property("width") or 0)
    height = float(item.property("height") or 0)
    score = width * height
    if width <= 0 or height <= 0:
        score -= 1_000_000
    probe = item
    while probe is not None:
        if probe.property("visible") is False:
            score -= 1_000_000
        opacity = probe.property("opacity")
        if opacity is not None and float(opacity) <= 0.01:
            score -= 1_000_000
        probe = probe.parentItem()
    return score


def find_item(item, object_name):
    matches = []

    def collect(candidate):
        if candidate is None:
            return
        if candidate.objectName() == object_name:
            matches.append(candidate)
        for child in candidate.childItems():
            collect(child)

    collect(item)
    if not matches:
        return None
    return max(matches, key=item_visibility_score)


def assert_item_inside_window(item, label):
    width = float(item.property("width") or 0)
    height = float(item.property("height") or 0)
    if width <= 0 or height <= 0:
        raise AssertionError(f"{label} has invalid size {width}x{height}")
    top_left = item.mapToScene(QPointF(0, 0))
    bottom_right = item.mapToScene(QPointF(width, height))
    if (
        top_left.x() < -0.5
        or top_left.y() < -0.5
        or bottom_right.x() > SIZE.width() + 0.5
        or bottom_right.y() > SIZE.height() + 0.5
    ):
        raise AssertionError(
            f"{label} is outside the rendered window: "
            f"({top_left.x():.1f}, {top_left.y():.1f}) -> "
            f"({bottom_right.x():.1f}, {bottom_right.y():.1f})"
        )


def item_center(item):
    width = float(item.property("width") or 0)
    height = float(item.property("height") or 0)
    if width <= 0 or height <= 0:
        raise AssertionError(f"{item.objectName()} has invalid size {width}x{height}")
    point = item.mapToScene(QPointF(width / 2, height / 2))
    return QPoint(max(0, min(SIZE.width() - 1, int(point.x()))), max(0, min(SIZE.height() - 1, int(point.y()))))


def click_item(app, window, root, object_name):
    pump(app, 8)
    item = find_item(root, object_name)
    if item is None:
        raise AssertionError(f"missing item {object_name}")
    assert_item_inside_window(item, object_name)
    QTest.mouseClick(window, Qt.LeftButton, Qt.NoModifier, item_center(item))
    QTest.qWait(20)
    pump(app, 18)
    return item


def assert_no_qml_issues(messages):
    issues = [
        record for record in messages
        if record["type"] in (QtMsgType.QtCriticalMsg, QtMsgType.QtFatalMsg)
        or (
            record["type"] == QtMsgType.QtWarningMsg
            and (record["file"].endswith(".qml") or any(marker in record["message"] for marker in ISSUE_MARKERS))
        )
    ]
    if issues:
        raise AssertionError(f"QML issues: {issues[:8]}")


QQuickStyle.setStyle("Basic")
app = QGuiApplication([])
messages = []


def capture_qt_message(mode, context, message):
    messages.append({
        "type": mode,
        "file": context.file or "",
        "line": context.line or 0,
        "message": message,
    })


previous_handler = qInstallMessageHandler(capture_qt_message)
try:
    late_sessions = FakeSessionsModel()
    late_view = QQuickView()
    late_view.setResizeMode(QQuickView.SizeRootObjectToView)
    late_view.setWidth(SIZE.width())
    late_view.setHeight(SIZE.height())
    late_view.engine().addImportPath(str(ROOT / "eigenqt" / "qml"))
    late_view.setSource(QUrl.fromLocalFile(str(ROOT / "eigenqt" / "qml" / "SessionsView.qml")))
    if late_view.status() == QQuickView.Error or late_view.rootObject() is None:
        raise AssertionError([error.toString() for error in late_view.errors()])
    late_view.show()
    late_root = late_view.rootObject()
    pump(app, 20)
    if late_root.property("qaAutoPruneAttempted") is not False:
        raise AssertionError("Sessions view auto-pruned before a model was attached")
    late_root.setProperty("sessionsModel", late_sessions)
    pump(app, 20)
    QTest.qWait(180)
    pump(app, 30)
    if ("PruneSessions",) not in late_sessions.calls:
        raise AssertionError(f"Late sessions model did not auto-prune: {late_sessions.calls}")
    if late_root.property("qaAutoPruneAttempted") is not True:
        raise AssertionError("Late sessions model did not record auto-prune attempt")
    if late_sessions.totalCount != 1:
        raise AssertionError(f"Late auto-prune did not remove empty session: total={late_sessions.totalCount}")
    late_view.hide()
    late_view.setSource(QUrl())
    pump(app, 8)

    sessions = FakeSessionsModel()
    view = QQuickView()
    view.setResizeMode(QQuickView.SizeRootObjectToView)
    view.setWidth(SIZE.width())
    view.setHeight(SIZE.height())
    view.engine().addImportPath(str(ROOT / "eigenqt" / "qml"))
    view.setInitialProperties({"sessionsModel": sessions})
    view.setSource(QUrl.fromLocalFile(str(ROOT / "eigenqt" / "qml" / "SessionsView.qml")))
    if view.status() == QQuickView.Error or view.rootObject() is None:
        raise AssertionError([error.toString() for error in view.errors()])
    view.show()
    root = view.rootObject()
    opened = []
    root.sessionClicked.connect(lambda session_id: opened.append(session_id))
    pump(app, 30)
    assert_no_qml_issues(messages)
    QTest.qWait(180)
    pump(app, 30)
    if ("PruneSessions",) not in sessions.calls:
        raise AssertionError(f"Sessions view did not auto-prune on load: {sessions.calls}")
    if root.property("qaAutoPruneAttempted") is not True:
        raise AssertionError("Sessions view did not record auto-prune attempt")
    if sessions.totalCount != 1:
        raise AssertionError(f"Auto-prune did not remove the empty session: total={sessions.totalCount}")

    search = click_item(app, view, root, "sessionsSearchField")
    if color_name(search.property("placeholderTextColor")) != EXPECTED_PLACEHOLDER_COLOR:
        raise AssertionError(
            f"Sessions search placeholder color regressed: {color_name(search.property('placeholderTextColor'))}"
        )
    for key in (Qt.Key_G, Qt.Key_U, Qt.Key_I):
        QTest.keyClick(view, key)
    pump(app, 20)
    if sessions.query != "gui" or sessions.rowCount() != 1:
        raise AssertionError(f"Search did not filter to gui session: query={sessions.query!r} rows={sessions.rowCount()}")
    if find_item(root, "sessionsRemoveButton_s_empty") is not None:
        raise AssertionError("Search still exposed the empty non-matching session")
    dir_label = find_item(root, "sessionsDirLabel_s_run")
    if dir_label is None:
        raise AssertionError("Sessions directory label did not render")
    if dir_label.property("qaText") != "gui-qt":
        raise AssertionError(f"Sessions directory label was not compact: {dir_label.property('qaText')!r}")
    updated_label = find_item(root, "sessionsUpdatedLabel_s_run")
    if updated_label is None:
        raise AssertionError("Sessions updated timestamp label did not render")
    updated_text = updated_label.property("qaText") or ""
    if (
        not updated_text
        or "Invalid" in updated_text
        or "NaN" in updated_text
        or (updated_text != "just now" and "ago" not in updated_text)
    ):
        raise AssertionError(f"Sessions updated timestamp was not normalized: {updated_text!r}")

    row = find_item(root, "sessionsRow_s_run")
    if row is None:
        raise AssertionError("Sessions row did not expose a keyboard target")
    row.forceActiveFocus(Qt.TabFocusReason)
    pump(app, 8)
    if row.property("qaVisualFocus") is not True:
        raise AssertionError("Sessions row did not expose visual keyboard focus")
    QTest.keyClick(view, Qt.Key_Return)
    pump(app, 12)
    if opened != ["s-run"]:
        raise AssertionError(f"Focused session row did not open with Return: {opened}")
    opened.clear()

    export = click_item(app, view, root, "sessionsExportButton_s_run")
    if not export.property("qaTextFits"):
        raise AssertionError("Export button text does not fit")
    if ("ExportSession", "s-run") not in sessions.calls:
        raise AssertionError(f"Export did not call model slot: {sessions.calls}")
    if "s-run" not in sessions.actionMessage:
        raise AssertionError(f"Export did not surface an action message: {sessions.actionMessage!r}")
    action_message = find_item(root, "sessionsActionMessage")
    action_message_text = find_item(root, "sessionsActionMessageText")
    action_message_dismiss = find_item(root, "sessionsActionMessageDismissButton")
    if action_message is None or action_message.property("visible") is not True:
        raise AssertionError("Sessions action message banner did not render")
    if action_message_text is None or "s-run" not in action_message_text.property("text"):
        raise AssertionError(f"Sessions action message text was wrong: {action_message_text.property('text') if action_message_text else None}")
    if action_message_dismiss is None or not action_message_dismiss.property("qaTextFits"):
        raise AssertionError("Sessions action message dismiss button did not fit")
    action_message_dismiss.click()
    pump(app, 18)
    if sessions.actionMessage != "":
        raise AssertionError("Sessions action message dismiss did not clear the model")

    sessions.set_action_error("Could not remove s-run: daemon offline")
    pump(app, 20)
    action_error = find_item(root, "sessionsActionError")
    action_error_text = find_item(root, "sessionsActionErrorText")
    action_error_dismiss = find_item(root, "sessionsActionErrorDismissButton")
    if action_error is None or action_error.property("visible") is not True:
        raise AssertionError("Sessions action error banner did not render")
    if action_error_text is None or "daemon offline" not in action_error_text.property("text"):
        raise AssertionError(f"Sessions action error text was wrong: {action_error_text.property('text') if action_error_text else None}")
    if action_error_dismiss is None or not action_error_dismiss.property("qaTextFits"):
        raise AssertionError("Sessions action error dismiss button did not fit")
    action_error_dismiss.click()
    pump(app, 18)
    if sessions.actionError != "":
        raise AssertionError("Sessions action error dismiss did not clear the model")

    prune = find_item(root, "sessionsPruneButton")
    if prune is None or not prune.property("qaTextFits"):
        raise AssertionError("Auto prune button did not render cleanly")
    before_prune_calls = len([call for call in sessions.calls if call == ("PruneSessions",)])
    click_item(app, view, root, "sessionsPruneButton")
    after_prune_calls = len([call for call in sessions.calls if call == ("PruneSessions",)])
    if after_prune_calls != before_prune_calls + 1:
        raise AssertionError(f"Auto prune did not call model slot: {sessions.calls}")
    if sessions.totalCount != 1 or sessions.rowCount() != 1:
        raise AssertionError(f"Manual prune changed non-empty sessions: total={sessions.totalCount} rows={sessions.rowCount()}")

    resume = click_item(app, view, root, "sessionsResumeButton_s_run")
    if opened != ["s-run"]:
        raise AssertionError(f"Resume did not emit sessionClicked: {opened}")
    if not resume.property("qaTextFits"):
        raise AssertionError("Resume button text does not fit")

    remove = click_item(app, view, root, "sessionsRemoveButton_s_run")
    if not remove.property("qaTextFits"):
        raise AssertionError("Remove button text does not fit")
    cancel = click_item(app, view, root, "sessionsRemoveCancelButton_s_run")
    if not cancel.property("qaTextFits"):
        raise AssertionError("Cancel button text does not fit")
    if any(call[0] == "RemoveSession" for call in sessions.calls):
        raise AssertionError(f"Cancel still removed a session: {sessions.calls}")

    click_item(app, view, root, "sessionsRemoveButton_s_run")
    confirm = find_item(root, "sessionsRemoveConfirmButton_s_run")
    if confirm is None:
        raise AssertionError("Confirm remove button did not render")
    if not confirm.property("qaTextFits"):
        raise AssertionError("Confirm remove button text does not fit")
    sessions.remove_delay_ms = 70
    click_item(app, view, root, "sessionsRemoveConfirmButton_s_run")
    if ("RemoveSession", "s-run") not in sessions.calls:
        raise AssertionError(f"Confirm did not call remove: {sessions.calls}")
    pending_confirm = find_item(root, "sessionsRemoveConfirmButton_s_run")
    pending_cancel = find_item(root, "sessionsRemoveCancelButton_s_run")
    pending_remove = find_item(root, "sessionsRemoveButton_s_run")
    pending_resume = find_item(root, "sessionsResumeButton_s_run")
    pending_export = find_item(root, "sessionsExportButton_s_run")
    if pending_confirm is None:
        raise AssertionError("Pending remove confirm button disappeared")
    if pending_confirm.property("qaText") != "Removing..." or pending_confirm.property("enabled") is not False:
        raise AssertionError(
            "Pending remove did not render a disabled Removing button: "
            f"text={pending_confirm.property('qaText')!r} enabled={pending_confirm.property('enabled')}"
        )
    if pending_confirm.property("qaTextFits") is not True:
        raise AssertionError("Pending remove text does not fit")
    if pending_cancel is None or pending_cancel.property("enabled") is not False:
        raise AssertionError("Pending remove did not keep cancel disabled in the confirm group")
    if pending_remove is not None and pending_remove.property("visible") is True:
        raise AssertionError("Pending remove fell back to the non-confirm Remove button")
    if pending_resume is not None and pending_resume.property("visible") is True:
        raise AssertionError("Pending remove kept the Resume action alongside the confirmation")
    if pending_export is not None and pending_export.property("visible") is True:
        raise AssertionError("Pending remove kept the Export action alongside the confirmation")
    click_item(app, view, root, "sessionsRemoveConfirmButton_s_run")
    if sessions.calls.count(("RemoveSession", "s-run")) != 1:
        raise AssertionError(f"Pending remove allowed duplicate calls: {sessions.calls}")
    QTest.qWait(90)
    pump(app, 18)
    if sessions.rowCount() != 0:
        raise AssertionError(f"Confirm remove did not remove the row: {sessions.rowCount()}")

    assert_no_qml_issues(messages)
finally:
    qInstallMessageHandler(previous_handler)
"""
    env = os.environ.copy()
    env.setdefault("QT_QPA_PLATFORM", "offscreen")
    env.setdefault("QML_DISABLE_DISK_CACHE", "1")
    env.setdefault("PYTHONFAULTHANDLER", "1")

    result = subprocess.run(
        [sys.executable, "-c", textwrap.dedent(script)],
        cwd=ROOT,
        env=env,
        text=True,
        capture_output=True,
        timeout=15,
    )

    assert result.returncode == 0, result.stdout + result.stderr
