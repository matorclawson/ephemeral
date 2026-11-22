"""Run from the repository root after make build. Python is only a CLI client."""
import json
import os
import subprocess
import tempfile
from pathlib import Path

with tempfile.TemporaryDirectory() as folder:
    db = str(Path(folder) / "demo.db")
    def call(op, **fields):
        result = subprocess.run([os.environ.get("EPHEMERAL_BIN", "./ephemeral"), db], input=json.dumps(dict(op=op, **fields)),
                                capture_output=True, text=True, check=True)
        return json.loads(result.stdout)

    call("register", name="review", version=1, steps=[
        {"kind":"activity", "name":"draft", "attempts":3, "backoff_ms":10},
        {"kind":"signal", "name":"approved"}])
    call("start", id="review-1", name="review", version=1, queue="editor",
         value={"topic":"durable workflows"}, attributes={"owner":"editor"})
    task = call("poll", queue="editor", leaseMS=30000)
    call("heartbeat", id=task["Run"], token=task["Token"], leaseMS=30000)
    call("finish", id=task["Run"], token=task["Token"], value={"draft":"draft-42"})
    assert call("list", state="waiting", limit=10) == ["review-1"]
    call("signal", id="review-1", key="decision-1", name="approved", value=True)
    assert call("describe", id="review-1")["State"] == "completed"
    for event in call("history", id="review-1", limit=100):
        print(event["Sequence"], event["Kind"], event["Data"])
