# Running the examples

From the unpacked source root on Linux, run `make examples` (Go 1.25+, Python 3, Make). It builds a portable local CLI, runs the four examples, and removes the example databases. To run one example using the ARM64 binary archive, extract it on Linux ARM64, enter the extracted directory and use the commands below. The Go example belongs to the source archive.

| Example | Run from the package root | What to look for |
|---|---|---|
| Approval in raw JSON | See the shell commands below | The run appears under waiting until a decision arrives. History shows when it was received and consumed. |
| Worker and human approval | `python3 examples/demo.py` | A worker claims and heartbeats a drafting activity. After it returns a draft, a signal closes the review. |
| Quick and full reviews | `python3 examples/parallel.py` | Quick returns one result. Full returns both results in definition order even though the length check finishes first. |
| Retry after reopening the store | `CGO_ENABLED=0 go run -mod=vendor ./examples/retry` | Attempt 1 fails. A newly opened Go store claims attempt 2 and prints HELLO. |

## Send your first workflow directly to the CLI

Each line of approval.jsonl is a complete request. Run the file against a fresh database. Each output line corresponds to the input line in the same position.

Run as: your normal user. Where: the repository root or unpacked package, with the ephemeral binary there. sudo: no. Use the same shell for the next command, which reuses $dbdir.

```sh
dbdir=$(mktemp -d)
./ephemeral "$dbdir/example.db" < examples/approval.jsonl
```

The third response lists draft-42 as waiting. The fifth has State completed and Value {"approved":true}. Inspect the same database again to read the first two events:

Run as: the same user, shell and directory as above. sudo: no.

```sh
echo '{"op":"history","id":"draft-42","limit":2}' | ./ephemeral "$dbdir/example.db"
```

For the next page, set after to the Sequence of the last returned event. To repeat the whole example, create another temporary directory because workflow IDs are unique. The two Python examples use temporary databases and accept EPHEMERAL_BIN to select an installed executable.
