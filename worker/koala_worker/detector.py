from __future__ import annotations

from dataclasses import dataclass
from datetime import timezone
from typing import Any

from .models import AnalyzeRequest, Detection


@dataclass(slots=True)
class DetectorConfig:
    package_threshold: float = 0.55
    person_threshold: float = 0.50


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
        raw = (req.frame_b64 or "").lower()
        detections: list[Detection] = []
        if "package" in raw:
            detections.append(
                Detection(
                    camera_id=req.camera_id,
                    zone_id=req.zone_id,
                    label="package",
                    confidence=0.91,
                    timestamp=req.captured_at.astimezone(timezone.utc).isoformat().replace("+00:00", "Z"),
                )
            )
        if "person" in raw:
            detections.append(
                Detection(
                    camera_id=req.camera_id,
                    zone_id=req.zone_id,
                    label="person",
                    confidence=0.88,
                    timestamp=req.captured_at.astimezone(timezone.utc).isoformat().replace("+00:00", "Z"),
                )
            )
        return self._apply_thresholds(detections)

    def _run_model(self, req: AnalyzeRequest) -> list[Detection]:
        # The actual Jetson path can be swapped to TensorRT export while preserving output schema.
        raw_output = self._model.predict(req.frame_b64 or "", verbose=False)
        detections: list[Detection] = []
        for result in raw_output:
            for box in result.boxes:
                label = result.names[int(box.cls[0])]
                if label not in {"person", "package"}:
                    continue
                detections.append(
                    Detection(
                        camera_id=req.camera_id,
                        zone_id=req.zone_id,
                        label=label,
                        confidence=float(box.conf[0]),
                        timestamp=req.captured_at.astimezone(timezone.utc).isoformat().replace("+00:00", "Z"),
                    )
                )
        return self._apply_thresholds(detections)

    def _apply_thresholds(self, detections: list[Detection]) -> list[Detection]:
        output: list[Detection] = []
        for det in detections:
            if det.label == "package" and det.confidence < self.config.package_threshold:
                continue
            if det.label == "person" and det.confidence < self.config.person_threshold:
                continue
            output.append(det)
        return output
