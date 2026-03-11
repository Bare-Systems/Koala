from __future__ import annotations

import json
import os
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any

from .detector import YoloDetector
from .models import AnalyzeRequest


class WorkerHandler(BaseHTTPRequestHandler):
    detector = YoloDetector()

    def do_GET(self) -> None:  # noqa: N802
        if self.path != "/v1/health":
            self._send_json(HTTPStatus.NOT_FOUND, {"status": "not_found"})
            return
        self._send_json(HTTPStatus.OK, {"status": "ok"})

    def do_POST(self) -> None:  # noqa: N802
        if self.path != "/v1/analyze/frame":
            self._send_json(HTTPStatus.NOT_FOUND, {"status": "not_found"})
            return

        content_len = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(content_len)
        try:
            payload = json.loads(raw.decode("utf-8") or "{}")
            request = AnalyzeRequest.from_dict(payload)
        except Exception as exc:
            self._send_json(HTTPStatus.BAD_REQUEST, {"status": "invalid", "error": str(exc)})
            return

        detections = [d.to_dict() for d in self.detector.analyze(request)]
        self._send_json(
            HTTPStatus.OK,
            {
                "model_version": "yolo-mvp-v1",
                "detections": detections,
            },
        )

    def log_message(self, *_args: Any) -> None:
        return

    def _send_json(self, status: HTTPStatus, payload: dict[str, Any]) -> None:
        body = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def _serve_http() -> None:
    server = ThreadingHTTPServer(("0.0.0.0", 8090), WorkerHandler)
    server.serve_forever()


def main() -> None:
    transport = os.environ.get("KOALA_TRANSPORT", "grpc").lower()
    if transport == "http":
        _serve_http()
    else:
        # Default: gRPC transport
        from .grpc_server import serve as grpc_serve  # noqa: PLC0415

        grpc_serve()


if __name__ == "__main__":
    main()
