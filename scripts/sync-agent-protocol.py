#!/usr/bin/env python3
"""Copy the authoritative V1 Go contracts into the standalone Agent module."""
import argparse
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[1]
FILES = ('messages.go', 'contract.go', 'contract_test.go')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    drift = []
    for name in FILES:
        source = ROOT / 'backend/internal/protocol' / name
        target = ROOT / 'agent/comm-agent/pkg/protocol' / name
        content = source.read_bytes()
        if not target.exists() or target.read_bytes() != content:
            if args.check:
                drift.append(str(target.relative_to(ROOT)))
            else:
                target.write_bytes(content)
    if drift:
        print('Protocol drift: ' + ', '.join(drift), file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
