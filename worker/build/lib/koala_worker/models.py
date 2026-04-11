from __future__ import annotations

from dataclasses import dataclass, asdict, field
from datetime import datetime, timezone
from typing import Any


@dataclass(slots=True)
class BBox:
    """Axis-aligned bounding box in normalized (0–1) frame coordinates."""
    x: float = 0.0
    y: float = 0.0
    w: float = 1.0
    h: float = 1.0


@dataclass(slots=True)
class Detection:
    camera_id: str
    zone_id: str
    label: str
    confidence: float
    timestamp: str
    bbox: BBox = field(default_factory=BBox)

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)


@dataclass(slots=True)
class AnalyzeRequest:
    camera_id: str
    zone_id: str
    frame_b64: str | None
    captured_at: datetime

    @classmethod
    def from_dict(cls, payload: dict[str, Any]) -> "AnalyzeRequest":
        captured_raw = payload.get("captured_at")
        if isinstance(captured_raw, str):
            captured = datetime.fromisoformat(captured_raw.replace("Z", "+00:00"))
        else:
            captured = datetime.now(timezone.utc)

        return cls(
            camera_id=str(payload["camera_id"]),
            zone_id=str(payload["zone_id"]),
            frame_b64=payload.get("frame_b64"),
            captured_at=captured.astimezone(timezone.utc),
        )
