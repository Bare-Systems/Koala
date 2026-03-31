"""gRPC inference worker server.

Implements the InferenceWorker service defined in koala_inference.proto.
Preserves the deterministic fallback path so tests work without a GPU.

Transport is selected by the KOALA_TRANSPORT env var:
  - "grpc"  → gRPC server only (default going forward)
  - "http"  → legacy HTTP server (backward compat)
  - unset   → gRPC
"""
from __future__ import annotations

import base64
import os
from concurrent import futures
from datetime import datetime, timezone
from typing import Any

import grpc  # type: ignore[import-untyped]

from .detector import YoloDetector
from .models import AnalyzeRequest, Detection
from .proto import koala_inference_pb2 as pb
from .proto import koala_inference_pb2_grpc as pb_grpc

CONTRACT_VERSION = "1"
WORKER_VERSION = "0.1.0"
GRPC_PORT = int(os.environ.get("KOALA_GRPC_PORT", "6706"))


def _detection_to_pb(d: Detection, timestamp_ms: int) -> pb.Detection:
    bbox = pb.BBox(x=d.bbox.x, y=d.bbox.y, width=d.bbox.w, height=d.bbox.h)
    return pb.Detection(
        camera_id=d.camera_id,
        zone_id=d.zone_id,
        label=d.label,
        confidence=float(d.confidence),
        bbox=bbox,
        timestamp_unix_ms=timestamp_ms,
    )


_JPEG_MAGIC = b"\xff\xd8"


def _request_from_pb(req: pb.FrameRequest) -> AnalyzeRequest:
    """Convert a gRPC FrameRequest to our internal AnalyzeRequest.

    For real JPEG frames (magic bytes 0xFF 0xD8) the frame is base64-encoded
    before being passed to the detector, matching the HTTP path.

    For non-JPEG bytes that are valid UTF-8 the raw text is used as the
    frame_b64 hint value. This allows deterministic test fixtures to pass
    plain-text hints like "package" or "person" without a real GPU runtime.
    """
    captured_at = datetime.fromtimestamp(req.captured_at_unix_ms / 1000.0, tz=timezone.utc)
    frame_b64: str | None = None
    if req.frame:
        if req.frame[:2] == _JPEG_MAGIC:
            frame_b64 = base64.b64encode(req.frame).decode()
        else:
            try:
                frame_b64 = req.frame.decode("utf-8")
            except UnicodeDecodeError:
                frame_b64 = base64.b64encode(req.frame).decode()
    return AnalyzeRequest(
        camera_id=req.camera_id,
        zone_id=req.zone_id,
        frame_b64=frame_b64,
        captured_at=captured_at,
    )


class InferenceWorkerServicer(pb_grpc.InferenceWorkerServicer):
    """gRPC service implementation for Koala inference."""

    def __init__(self, detector: YoloDetector | None = None) -> None:
        self._detector = detector or YoloDetector()

    def AnalyzeFrame(  # noqa: N802
        self,
        request: pb.FrameRequest,
        context: grpc.ServicerContext,  # type: ignore[type-arg]
    ) -> pb.FrameResponse:
        if request.contract_version and request.contract_version != CONTRACT_VERSION:
            context.set_code(grpc.StatusCode.FAILED_PRECONDITION)
            context.set_details(
                f"contract version mismatch: orchestrator={request.contract_version} worker={CONTRACT_VERSION}"
            )
            return pb.FrameResponse()

        internal_req = _request_from_pb(request)
        try:
            detections = self._detector.analyze(internal_req)
        except Exception as exc:  # noqa: BLE001
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(str(exc))
            return pb.FrameResponse()

        timestamp_ms = int(internal_req.captured_at.timestamp() * 1000)
        return pb.FrameResponse(
            model_version="yolo-mvp-v1",
            detections=[_detection_to_pb(d, timestamp_ms) for d in detections],
        )

    def AnalyzeBatch(  # noqa: N802
        self,
        request: pb.BatchRequest,
        context: grpc.ServicerContext,  # type: ignore[type-arg]
    ) -> pb.BatchResponse:
        responses: list[pb.FrameResponse] = []
        for frame_req in request.frames:
            resp = self.AnalyzeFrame(frame_req, context)
            if context.code() and context.code() != grpc.StatusCode.OK:
                return pb.BatchResponse()
            responses.append(resp)
        return pb.BatchResponse(
            model_version="yolo-mvp-v1",
            responses=responses,
        )

    def WorkerHealth(  # noqa: N802
        self,
        request: pb.HealthRequest,
        context: grpc.ServicerContext,  # type: ignore[type-arg]
    ) -> pb.HealthResponse:
        return pb.HealthResponse(
            status="ok",
            version=WORKER_VERSION,
            contract_version=CONTRACT_VERSION,
        )


def serve(port: int = GRPC_PORT, detector: YoloDetector | None = None) -> None:
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
    pb_grpc.add_InferenceWorkerServicer_to_server(InferenceWorkerServicer(detector), server)
    server.add_insecure_port(f"[::]:{port}")
    server.start()
    server.wait_for_termination()


def create_server(port: int = GRPC_PORT, detector: YoloDetector | None = None) -> Any:
    """Create (but do not start) a gRPC server. Useful for testing."""
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
    pb_grpc.add_InferenceWorkerServicer_to_server(InferenceWorkerServicer(detector), server)
    server.add_insecure_port(f"[::]:{port}")
    return server
