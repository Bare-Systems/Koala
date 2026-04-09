import unittest
from datetime import datetime, timezone

from koala_worker.detector import LABEL_MAP, DetectorConfig, YoloDetector
from koala_worker.models import AnalyzeRequest

_NOW = datetime.now(timezone.utc)


def _req(frame_b64: str, camera_id: str = "cam_front_1") -> AnalyzeRequest:
    return AnalyzeRequest(
        camera_id=camera_id, zone_id="front_door",
        frame_b64=frame_b64, captured_at=_NOW,
    )


class TestDetectorHeuristic(unittest.TestCase):
    def test_package_and_person_hints(self) -> None:
        det = YoloDetector(config=DetectorConfig(package_threshold=0.1, person_threshold=0.1))
        labels = sorted(d.label for d in det.analyze(_req("package_person")))
        self.assertEqual(labels, ["package", "person"])

    def test_global_threshold_rejects_low_confidence(self) -> None:
        det = YoloDetector(config=DetectorConfig(package_threshold=0.95, person_threshold=0.95))
        self.assertEqual(det.analyze(_req("package_person")), [])

    def test_per_camera_threshold_override_lower(self) -> None:
        # Camera "cam_hi" has a lower person threshold → person passes.
        cfg = DetectorConfig(
            person_threshold=0.95,
            camera_thresholds={"cam_hi": {"person": 0.10}},
        )
        det = YoloDetector(config=cfg)
        labels = [d.label for d in det.analyze(_req("person", camera_id="cam_hi"))]
        self.assertIn("person", labels)

    def test_per_camera_threshold_override_higher(self) -> None:
        # Camera "cam_strict" has a higher package threshold → package rejected.
        cfg = DetectorConfig(
            package_threshold=0.10,
            camera_thresholds={"cam_strict": {"package": 0.99}},
        )
        det = YoloDetector(config=cfg)
        labels = [d.label for d in det.analyze(_req("package", camera_id="cam_strict"))]
        self.assertNotIn("package", labels)

    def test_camera_not_in_overrides_uses_global(self) -> None:
        cfg = DetectorConfig(
            package_threshold=0.10,
            camera_thresholds={"other_cam": {"package": 0.99}},
        )
        det = YoloDetector(config=cfg)
        # "cam_front_1" not in overrides → falls back to global 0.10 → passes
        labels = [d.label for d in det.analyze(_req("package", camera_id="cam_front_1"))]
        self.assertIn("package", labels)


class TestLabelMap(unittest.TestCase):
    def test_canonical_labels_present(self) -> None:
        self.assertEqual(LABEL_MAP["person"], "person")
        self.assertEqual(LABEL_MAP["package"], "package")

    def test_yolo_aliases_map_to_package(self) -> None:
        for alias in ("suitcase", "backpack", "handbag", "box", "parcel"):
            with self.subTest(alias=alias):
                self.assertEqual(LABEL_MAP.get(alias), "package")

    def test_unknown_label_not_in_map(self) -> None:
        self.assertIsNone(LABEL_MAP.get("traffic_cone"))


class TestDetectorConfig(unittest.TestCase):
    def test_threshold_for_uses_per_camera(self) -> None:
        cfg = DetectorConfig(
            package_threshold=0.55,
            camera_thresholds={"cam1": {"package": 0.70}},
        )
        self.assertEqual(cfg.threshold_for("cam1", "package"), 0.70)

    def test_threshold_for_falls_back_to_global(self) -> None:
        cfg = DetectorConfig(package_threshold=0.55)
        self.assertEqual(cfg.threshold_for("cam_unknown", "package"), 0.55)

    def test_threshold_for_unknown_label_rejects(self) -> None:
        cfg = DetectorConfig()
        self.assertEqual(cfg.threshold_for("cam1", "bicycle"), 1.0)


if __name__ == "__main__":
    unittest.main()
