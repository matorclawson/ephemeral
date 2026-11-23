"""Compare two persistent JSON CLIs on the same local filesystem (Linux)."""
import json
import sqlite3
import statistics
import subprocess
import sys
import tempfile
import threading
import time
from pathlib import Path

def trial(binary):
    with tempfile.TemporaryDirectory(dir=".") as directory:
        db = Path(directory) / "workflows.db"
        with subprocess.Popen([binary, db], text=True, stdin=subprocess.PIPE, stdout=subprocess.PIPE) as proc:
            def call(op, **fields):
                proc.stdin.write(json.dumps(dict(op=op, **fields)) + "\n")
                proc.stdin.flush()
                return json.loads(proc.stdout.readline())
            call("register", name="job", version=1, steps=[dict(kind="activity", name="work")])
            results = {}
            for case, count in [("idle", 200), ("busy_writer", 50), ("workflow", 200)]:
                samples = []
                for i in range(count):
                    if case == "busy_writer":
                        locked = threading.Event()
                        def hold():
                            with sqlite3.connect(db) as conn:
                                conn.execute("BEGIN IMMEDIATE")
                                locked.set()
                                time.sleep(.020)
                                conn.rollback()
                        thread = threading.Thread(target=hold)
                        thread.start()
                        locked.wait()
                    start = time.perf_counter_ns()
                    if case == "workflow":
                        run = str(i)
                        call("start", id=run, name="job", version=1, queue="q", value=i)
                        task = call("poll", queue="q", leaseMS=60000)
                        call("finish", id=run, token=task["Token"], value=i)
                    else:
                        assert call("poll", queue="q", leaseMS=60000) is None
                    samples.append((time.perf_counter_ns()-start)/1e6)
                    if case == "busy_writer":
                        thread.join()
                results[case] = [round(statistics.median(samples),3), round(sorted(samples)[count*95//100-1],3)]
            proc.stdin.close()
            assert proc.wait() == 0
        return results
for i in range(3):
    for binary in (sys.argv[1:] if i%2==0 else reversed(sys.argv[1:])):
        print(i+1,binary,trial(binary),flush=True)
