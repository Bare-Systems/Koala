"""Integration tests for the gRPC InferenceWorker server."""
from __future__ import annotations

import base64
import threading
import time

import grpc  # type: ignore[import-untyped]
import pytest

from koala_worker.grpc_server import CONTRACT_VERSION, InferenceWorkerServicer, create_server
from koala_worker.proto import koala_inference_pb2 as pb
from koala_worker.proto import koala_inference_pb2_grpc as pb_grpc

# Use a high ephemeral port to avoid conflicts in CI.
TEST_PORT = 15051


@pytest.fixture(scope="module")
def grpc_stub() -> pb_grpc.InferenceWorkerStub:
    server = create_server(port=TEST_PORT)
    server.start()
    # Give the server a moment to bind.
    time.sleep(0.05)
    channel = grpc.insecure_channel(f"localhost:{TEST_PORT}")
    stub = pb_grpc.InferenceWorkerStub(channel)
    yield stub
    server.stop(grace=0)


class TestWorkerHealth:
    def test_returns_ok(self, grpc_stub: pb_grpc.InferenceWorkerStub) -> None:
        resp = grpc_stub.WorkerHealth(pb.HealthRequest())
        assert resp.status == "ok"

    def test_reports_contract_version(self, grpc_stub: pb_grpc.InferenceWorkerStub) -> None:
        resp = grpc_stub.WorkerHealth(pb.HealthRequest())
        assert resp.contract_version == CONTRACT_VERSION


class TestAnalyzeFrame:
    def _make_request(self, hint: str, camera_id: str = "cam1", zone_id: str = "front_door") -> pb.FrameRequest:
        frame_bytes = base64.b64decode(base64.b64encode(hint.encode()))
        return pb.FrameRequest(
            camera_id=camera_id,
            zone_id=zone_id,
            frame=frame_bytes,
            captured_at_unix_ms=int(time.time() * 1000),
            contract_version=CONTRACT_VERSION,
        )

    def test_package_hint_returns_package_detection(self, grpc_stub: pb_grpc.InferenceWorkerStub) -> None:
        req = self._make_request("package")
        resp = grpc_stub.AnalyzeFrame(req)
        assert resp.model_version != ""
        labels = [d.label for d in resp.detections]
        assert "package" in labels

    def test_person_hint_returns_person_detection(self, grpc_stub: pb_grpc.InferenceWorkerStub) -> None:
        req = self._make_request("person")
        resp = grpc_stub.AnalyzeFrame(req)
        labels = [d.label for d in resp.detections]
        assert "person" in labels

    def test_no_hint_returns_empty_detections(self, grpc_stub: pb_grpc.InferenceWorkerStub) -> None:
        req = self._make_request("nothing_here_at_all_xyz")
        resp = grpc_stub.AnalyzeFrame(req)
        assert resp.detections == []

    def test_detection_fields_populated(self, grpc_stub: pb_grpc.InferenceWorkerStub) -> None:
        req = self._make_request("package", camera_id="cam_front", zone_id="front_door")
        resp = grpc_stub.AnalyzeFrame(req)
        det = resp.detections[0]
        assert det.camera_id == "cam_front"
        assert det.zone_id == "front_door"
        assert 0 < det.confidence <= 1.0
        assert det.timestamp_unix_ms > 0

    def test_contract_version_mismatch_fails(self, grpc_stub: pb_grpc.InferenceWorkerStub) -> None:
        req = pb.FrameRequest(
            camera_id="cam1",
            zone_id="front_door",
            frame=b"test",
            captured_at_unix_ms=int(time.time() * 1000),
            contract_version="99",
        )
        with pytest.raises(grpc.RpcError) as exc_info:
            grpc_stub.AnalyzeFrame(req)
        assert exc_info.value.code() == grpc.StatusCode.FAILED_PRECONDITION


class TestAnalyzeBatch:
    def test_batch_returns_response_per_frame(self, grpc_stub: pb_grpc.InferenceWorkerStub) -> None:
        frames = [
            pb.FrameRequest(
                camera_id="cam1",
                zone_id="front_door",
                frame=base64.b64decode(base64.b64encode(hint.encode())),
                captured_at_unix_ms=int(time.time() * 1000),
                contract_version=CONTRACT_VERSION,
            )
            for hint in ["package", "person", "nothing"]
        ]
        resp = grpc_stub.AnalyzeBatch(pb.BatchRequest(frames=frames))
        assert len(resp.responses) == 3
