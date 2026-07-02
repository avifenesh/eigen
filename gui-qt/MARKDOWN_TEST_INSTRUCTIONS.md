# Markdown Rendering Test Instructions

## Prerequisites

1. guiserver is running: `~/.eigen/guiserver.sock` exists
2. eigen daemon is running (for model responses)
3. DISPLAY=:0 is available (X11/Wayland with GUI)

## Manual Test Steps

### 1. Create a test session and send markdown demo

```bash
cd gui-qt

# Launch the Qt app
DISPLAY=:0 .venv/bin/python3 main.py
```

### 2. In the GUI:

1. Click "New Session" (or open an existing session)
2. Send this prompt:

```
Give me a markdown demo with:
- A heading (h2 and h3)
- A paragraph with **bold**, *italic*, `code`, and a [link](https://example.com)
- A bullet list (3 items)
- A numbered list (3 items)
- A table (2 columns, 3 rows including header)
- A Python code block (8+ lines with syntax highlighting)
- A blockquote

Format everything nicely.
```

### 3. Verify rendering

Once the model responds, verify:

- ✓ Headings render at correct sizes (h2 > h3 > body)
- ✓ Inline formatting: **bold**, *italic*, `code` with gray background
- ✓ Links are blue (#5fb0c4) and clickable
- ✓ Bullet list has • markers, numbered list has 1. 2. 3.
- ✓ Code blocks have:
  - Raised surface with #0a1012 background
  - Lang badge (e.g., "python")
  - Copy button (top-right)
  - Syntax highlighting (purple keywords, green strings, blue functions, etc.)
  - Horizontal scroll for long lines
- ✓ Table has borders and alternating row background
- ✓ Blockquote has left teal border + inset background

### 4. Take screenshot

Use your OS screenshot tool and save to:

```
gui-qt/screenshots/markdown-demo.png
```

## Automated Test (pytest)

The pipeline is already tested via pytest:

```bash
cd gui-qt
.venv/bin/python3 -m pytest tests/test_markdown.py -v
```

All tests should pass. Performance test shows ~0.8ms parse time for realistic markdown (target: <5ms).

## Performance Verification

```bash
cd gui-qt
.venv/bin/python3 -c "
import time
from eigenqt.markdown import parse

# 10k-char message (streaming case)
text = 'Here is a long paragraph. ' * 400
start = time.perf_counter()
blocks = parse(text)
elapsed = (time.perf_counter() - start) * 1000
print(f'Parse time: {elapsed:.2f}ms (target: <5ms)')
"
```

Expected: ~1-2ms (well under target).
