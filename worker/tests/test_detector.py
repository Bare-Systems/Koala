import unittest
from datetime import datetime, timezone

from koala_worker.detector import DetectorConfig, YoloDetector
from koala_worker.models import AnalyzeRequest


class DetectorTests(unittest.TestCase):
    def test_detector_heuristic_labels(self) -> None:
        detector = YoloDetector(config=DetectorConfig(package_threshold=0.1, person_threshold=0.1))
        req = AnalyzeRequest(
            camera_id="cam_front_1",
            zone_id="front_door",
            frame_b64="package_person",
            captured_at=datetime.now(timezone.utc),
        )
        detections = detector.analyze(req)
        labels = sorted([d.label for d in detections])
        self.assertEqual(labels, ["package", "person"])

    def test_detector_thresholding(self) -> None:
        detector = YoloDetector(config=DetectorConfig(package_threshold=0.95, person_threshold=0.95))
        req = AnalyzeRequest(
            camera_id="cam_front_1",
            zone_id="front_door",
            frame_b64="package_person",
            captured_at=datetime.now(timezone.utc),
        )
        detections = detector.analyze(req)
        self.assertEqual(detections, [])


if __name__ == "__main__":
    unittest.main()
