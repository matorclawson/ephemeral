"""Run two review modes through the Ephemeral JSON CLI."""
import json
import os
import subprocess
import tempfile
from pathlib import Path

binary = os.environ.get("EPHEMERAL_BIN", "./ephemeral")


def run(db, op, **fields):
    request = json.dumps({"op": op, **fields})
    reply = subprocess.run([binary, str(db)], input=request, text=True,
                           stdout=subprocess.PIPE, check=True)
    return json.loads(reply.stdout)


def review(db, mode, queues):
    run_id = f"review-{mode}"
    run(db, "start", id=run_id, name="review", version=1, queue="parent",
        value={"title": "Durable workflows", "mode": mode})

    # Finish the full review's children in reverse declaration order.
    for queue in queues:
        task = run(db, "poll", queue=queue, leaseMS=30000)
        assert task and task["Activity"] == queue, task
        title = task["Input"]["title"]
        passed = bool(title.strip()) if queue == "title-present" else len(title) <= 60
        run(db, "finish", id=task["Run"], token=task["Token"],
            value={"check": queue, "passed": passed})

    # Poll the parent queue to commit the join and advance the workflow.
    run(db, "poll", queue="parent", leaseMS=30000)
    result = run(db, "describe", id=run_id)
    assert result["State"] == "completed", result
    return result["Value"]


with tempfile.TemporaryDirectory() as folder:
    db = Path(folder) / "reviews.db"
    children = []
    for check in ["title-present", "title-length"]:
        run(db, "register", name=check, version=1,
            steps=[{"kind": "activity", "name": check}])
        children.append({"name": check, "version": 1, "queue": check})
    run(db, "register", name="review", version=1, steps=[
        {"kind": "switch", "field": "mode", "cases": {"full": "both"}, "default": "quick"},
        {"kind": "child", "label": "quick", "children": children[:1], "next": "$end"},
        {"kind": "parallel", "label": "both", "children": children},
    ])
    quick = review(db, "quick", ["title-present"])
    full = review(db, "full", ["title-length", "title-present"])
    assert quick == {"check": "title-present", "passed": True}
    assert full == [quick, {"check": "title-length", "passed": True}]
    print("quick:", quick)
    print("full: ", full)
