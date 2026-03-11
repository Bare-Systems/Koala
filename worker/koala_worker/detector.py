from __future__ import annotations

from dataclasses import dataclass, field
from datetime import timezone
from typing import Any

from .models import AnalyzeRequest, BBox, Detection

# Maps raw model output class names to canonical Koala labels.
# Handles COCO / custom-model name variations so the rest of the pipeline
# only ever sees "person" or "package".
LABEL_MAP: dict[str, str] = {
    "person": "person",
    # YOLO COCO classes that are semantically "a package at a door"
    "package": "package",
    "box": "package",
    "parcel": "package",
    "suitcase": "package",
    "backpack": "package",
    "handbag": "package",
}


@dataclass
class DetectorConfig:
    package_threshold: float = 0.55
    person_threshold: float = 0.50
    # Per-camera label threshold overrides: {camera_id: {label: threshold}}
    # Falls back to the global threshold when a camera_id is not present.
    camera_thresholds: dict[str, dict[str, float]] = field(default_factory=dict)

    def threshold_for(self, camera_id: str, label: str) -> float:
        """Return the confidence threshold for (camera_id, label), with fallback."""
        per_cam = self.camera_thresholds.get(camera_id, {})
        if label in per_cam:
            return per_cam[label]
        if label == "package":
            return self.package_threshold
        if label == "person":
            return self.person_threshold
        return 1.0  # unknown label: reject by default


class YoloDetector:
    """YOLO-family abstraction with deterministic fallback for local testing."""

    def __init__(self, config: DetectorConfig | None = None) -> None:
        self.config = config or DetectorConfig()
        self._model = self._maybe_load_model()

    def _maybe_load_model(self) -> Any:
        try:
            from ultralytics import YOLO  # type: ignore

            return YOLO("yolov8n.pt")
        except Exception:
            return None

    def analyze(self, req: AnalyzeRequest) -> list[Detection]:
        if self._model is None:
            return self._heuristic(req)
        return self._run_model(req)

    def _heuristic(self, req: AnalyzeRequest) -> list[Detection]:
        # Frame payload hints let replay fixtures drive deterministic tests without GPU runtime.
        # BBox defaults to full frame (0, 0, 1, 1) since heuristic has no real geometry.
        raw = (req.frame_b64 or "").lower()
        detections: list[Detection] = []
        ts = req.captured_at.astimezone(timezone.utc).isoformat().replace("+00:00", "Z")
        if "package" in raw:
            detections.append(Detection(
                camera_id=req.camera_id, zone_id=req.zone_id,
                label="package", confidence=0.91, timestamp=ts,
                bbox=BBox(x=0.0, y=0.0, w=1.0, h=1.0),
            ))
        if "person" in raw:
            detections.append(Detection(
                camera_id=req.camera_id, zone_id=req.zone_id,
                label="person", confidence=0.88, timestamp=ts,
                bbox=BBox(x=0.0, y=0.0, w=1.0, h=1.0),
            ))
        return self._apply_thresholds(detections, req.camera_id)

    def _run_model(self, req: AnalyzeRequest) -> list[Detection]:
        # The actual Jetson path can be swapped to TensorRT export while preserving output schema.
        raw_output = self._model.predict(req.frame_b64 or "", verbose=False)
        detections: list[Detection] = []
        ts = req.captured_at.astimezone(timezone.utc).isoformat().replace("+00:00", "Z")
        for result in raw_output:
            for box in result.boxes:
                raw_label = result.names[int(box.cls[0])]
                canonical = LABEL_MAP.get(raw_label)
                if canonical is None:
                    continue
                # box.xywhn gives normalized [x_center, y_center, w, h].
                # Convert to top-left origin: x = cx - w/2, y = cy - h/2.
                try:
                    cx, cy, bw, bh = (float(v) for v in box.xywhn[0])
                    bbox = BBox(x=cx - bw / 2, y=cy - bh / 2, w=bw, h=bh)
                except Exception:
                    bbox = BBox()
                detections.append(Detection(
                    camera_id=req.camera_id, zone_id=req.zone_id,
                    label=canonical, confidence=float(box.conf[0]),
                    timestamp=ts, bbox=bbox,
                ))
        return self._apply_thresholds(detections, req.camera_id)

    def _apply_thresholds(self, detections: list[Detection], camera_id: str) -> list[Detection]:
        return [
            det for det in detections
            if det.confidence >= self.config.threshold_for(camera_id, det.label)
        ]
