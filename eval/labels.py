#!/usr/bin/env python3
"""Harvest human finding-labels from forge review threads → labels jsonl.

Convention (eval/README §8.5): a ccr-posted review comment carries a
`ccr:fp=<fingerprint>` footer; a human replies to it with a line starting
`ccr:label=<important|minor|debatable|wrong>` (free-text rationale may follow,
on the same line or below — it becomes `note`). This script joins the two and
emits one jsonl line per label, keyed by fingerprint — the same join key
posterior.py labels use, so human and posterior labels merge in eval rollups.

Usage:
    python3 eval/labels.py github <owner>/<repo> <pr-number> [--out labels.jsonl]

Output line shape:
    {"fingerprint": "abc123def456" | null, "label": "wrong", "note": "...",
     "tags": ["textbook"], "path": "x/y.py", "line": 216,
     "source": "github:owner/repo#9", "comment_url": "...", "reply_id": 123,
     "by": "login", "at": "..."}

- fingerprint is null for comments posted before the footer convention —
  path/line are kept so the label can be back-filled by hand.
- `--out` appends and dedups on reply_id, so re-harvesting a PR is idempotent.
- v1 harvests inline (diff-anchored) threads only; labels on the summary
  comment's fallback list have no per-finding thread to hang on.

Requires a `gh` CLI authenticated for the target repo.
"""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from pathlib import Path

FP_RE = re.compile(r"ccr:fp=([0-9a-f]{6,40})")
LABEL_RE = re.compile(r"^\s*ccr:label=(important|minor|debatable|wrong)\b[ \t:—-]*(.*)$", re.M)
CCR_HEAD = "devloop code-review"  # fp-less fallback: recognize ccr comments by header


def _gh_json(path: str) -> list[dict]:
    out = subprocess.run(
        ["gh", "api", path, "--paginate"], capture_output=True, text=True, check=True
    ).stdout
    # --paginate concatenates arrays; gh emits them back-to-back
    items: list[dict] = []
    dec = json.JSONDecoder()
    i, s = 0, out.strip()
    while i < len(s):
        obj, j = dec.raw_decode(s, i)
        items.extend(obj if isinstance(obj, list) else [obj])
        i = j
        while i < len(s) and s[i] in " \n\r\t":
            i += 1
    return items


def harvest_github(repo: str, pr: int) -> list[dict]:
    comments = _gh_json(f"repos/{repo}/pulls/{pr}/comments")
    by_id = {c["id"]: c for c in comments}
    labels: list[dict] = []
    for c in comments:
        m = LABEL_RE.search(c.get("body") or "")
        parent = by_id.get(c.get("in_reply_to_id") or -1)
        if not m or parent is None:
            continue
        pbody = parent.get("body") or ""
        if CCR_HEAD not in pbody and not FP_RE.search(pbody):
            continue  # a labelled reply, but not on a ccr finding
        fp = FP_RE.search(pbody)
        # note = same-line remainder + any following lines of the reply
        note = (m.group(2) + (c.get("body") or "")[m.end():]).strip()
        labels.append(
            {
                "fingerprint": fp.group(1) if fp else None,
                "label": m.group(1),
                "note": note,
                "path": parent.get("path"),
                "line": parent.get("line") or parent.get("original_line"),
                "source": f"github:{repo}#{pr}",
                "comment_url": parent.get("html_url"),
                "reply_id": c["id"],
                "by": (c.get("user") or {}).get("login"),
                "at": c.get("created_at"),
            }
        )
    return labels


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("forge", choices=["github"])
    ap.add_argument("repo", help="owner/repo")
    ap.add_argument("pr", type=int)
    ap.add_argument("--out", type=Path, help="append (deduped on reply_id); default: stdout")
    args = ap.parse_args()

    labels = harvest_github(args.repo, args.pr)
    if not args.out:
        for rec in labels:
            print(json.dumps(rec, ensure_ascii=False))
        return 0

    seen: set[int] = set()
    if args.out.exists():
        for line in args.out.read_text(encoding="utf-8").splitlines():
            if line.strip():
                seen.add(json.loads(line).get("reply_id"))
    fresh = [r for r in labels if r["reply_id"] not in seen]
    with args.out.open("a", encoding="utf-8") as f:
        for rec in fresh:
            f.write(json.dumps(rec, ensure_ascii=False) + "\n")
    print(f"harvested {len(labels)} label(s), {len(fresh)} new → {args.out}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
