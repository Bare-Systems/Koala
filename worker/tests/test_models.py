import unittest
from datetime import datetime, timezone

from koala_worker.models import AnalyzeRequest


class AnalyzeRequestTests(unittest.TestCase):
    def test_analyze_request_from_dict_parses_timestamp(self) -> None:
        now = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
        payload = {
            "camera_id": "cam_front_1",
            "zone_id": "front_door",
            "frame_b64": "package",
            "captured_at": now,
        }

        req = AnalyzeRequest.from_dict(payload)
        self.assertEqual(req.camera_id, "cam_front_1")
        self.assertEqual(req.zone_id, "front_door")
        self.assertIsNotNone(req.captured_at.tzinfo)

    def test_analyze_request_defaults_timestamp_when_missing(self) -> None:
        req = AnalyzeRequest.from_dict(
            {"camera_id": "cam_front_1", "zone_id": "front_door", "frame_b64": "person"}
        )
        self.assertIsNotNone(req.captured_at.tzinfo)


if __name__ == "__main__":
    unittest.main()
