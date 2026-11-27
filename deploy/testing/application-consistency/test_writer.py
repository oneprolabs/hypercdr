import hashlib
import tracemalloc
import unittest
from unittest.mock import patch

import writer


def records(count):
    for sequence in range(1, count + 1):
        yield {
            "sequence": sequence,
            "digest": hashlib.sha256(str(sequence).encode()).hexdigest(),
        }


class StreamingValidatorTests(unittest.TestCase):
    def test_large_history_uses_bounded_memory(self):
        with patch.object(writer, "read_records", side_effect=lambda path: records(50000)):
            tracemalloc.start()
            try:
                self.assertEqual(writer.validate(), 50000)
                self.assertLess(tracemalloc.get_traced_memory()[1], 1024 * 1024)
            finally:
                tracemalloc.stop()

    def test_missing_commit_is_rejected(self):
        with patch.object(
            writer,
            "read_records",
            side_effect=lambda path: records(2 if path.startswith("/ledger") else 1),
        ):
            with self.assertRaisesRegex(RuntimeError, "cross-volume"):
                writer.validate()

    def test_invalid_checksum_is_rejected(self):
        with patch.object(
            writer,
            "read_records",
            side_effect=lambda path: iter([{"sequence": 1, "digest": "invalid"}]),
        ):
            with self.assertRaisesRegex(RuntimeError, "checksum"):
                writer.validate()


if __name__ == "__main__":
    unittest.main()
