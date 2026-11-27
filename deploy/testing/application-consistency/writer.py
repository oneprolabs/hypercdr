import hashlib
import itertools
import json
import os
import signal
import time


stopping = False


def stop(signum, frame):
    global stopping
    stopping = True


def read_records(path):
    if not os.path.exists(path):
        return
    with open(path, encoding="utf-8") as stream:
        for line in stream:
            yield json.loads(line)


def validate():
    sequence = 0
    records = itertools.zip_longest(
        read_records("/ledger/records.jsonl"),
        read_records("/commits/records.jsonl"),
    )
    for expected, (record, commit) in enumerate(records, 1):
        if record != commit:
            raise RuntimeError("cross-volume ledger/commit invariant failed")
        digest = hashlib.sha256(str(expected).encode()).hexdigest()
        if record != {"sequence": expected, "digest": digest}:
            raise RuntimeError("sequence or checksum invariant failed")
        sequence = expected
    return sequence


def main():
    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    sequence = validate()
    print(json.dumps({"event": "validated-start", "sequence": sequence}), flush=True)
    with open("/ledger/records.jsonl", "a", encoding="utf-8") as ledger, open(
        "/commits/records.jsonl", "a", encoding="utf-8"
    ) as commits:
        while not stopping:
            sequence += 1
            record = json.dumps(
                {
                    "sequence": sequence,
                    "digest": hashlib.sha256(str(sequence).encode()).hexdigest(),
                },
                sort_keys=True,
            ) + "\n"
            ledger.write(record)
            ledger.flush()
            os.fsync(ledger.fileno())
            time.sleep(0.02)
            commits.write(record)
            commits.flush()
            os.fsync(commits.fileno())
            time.sleep(0.18)
    print(json.dumps({"event": "validated-stop", "sequence": validate()}), flush=True)


if __name__ == "__main__":
    main()
