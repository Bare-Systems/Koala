#!/usr/bin/env python3
"""
Evaluation runner for the Koala detection pipeline.

Reads a JSON fixture file (same format as tests/fixtures/replay/front_door_cases.json),
runs the Python detector against each frame_tag hint, and prints a confusion matrix
with precision / recall / F1 for package and person labels.

Usage:
    python3 -m tools.eval_runner [--fixtures PATH] [--pkg-threshold FLOAT] [--person-threshold FLOAT]
"""
from __future__ import annotations

import argparse
import json
import sys
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path

# Allow running as a module from the worker/ directory.
sys.path.insert(0, str(Path(__file__).parent.parent))

from koala_worker.detector import DetectorConfig, YoloDetector
from koala_worker.models import AnalyzeRequest

DEFAULT_FIXTURES = Path(__file__).parent.parent.parent / "tests/fixtures/replay/front_door_cases.json"


@dataclass
class ConfusionMatrix:
    tp: int = 0
    fp: int = 0
    tn: int = 0
    fn: int = 0

    def record(self, expected: bool, predicted: bool) -> None:
        if expected and predicted:
            self.tp += 1
        elif not expected and predicted:
            self.fp += 1
        elif not expected and not predicted:
            self.tn += 1
        else:
            self.fn += 1

    @property
    def precision(self) -> float:
        denom = self.tp + self.fp
        return self.tp / denom if denom else 1.0

    @property
    def recall(self) -> float:
        denom = self.tp + self.fn
        return self.tp / denom if denom else 1.0

    @property
    def f1(self) -> float:
        p, r = self.precision, self.recall
        return 2 * p * r / (p + r) if (p + r) else 0.0

    def summary(self) -> str:
        return (
            f"precision={self.precision:.2f}  recall={self.recall:.2f}  f1={self.f1:.2f}  "
            f"TP={self.tp}  FP={self.fp}  TN={self.tn}  FN={self.fn}"
        )


@dataclass
class EvalResult:
    name: str
    expected_package: bool
    expected_person: bool
    predicted_package: bool
    predicted_person: bool
    passed: bool


def run_eval(fixtures_path: Path, pkg_threshold: float, person_threshold: float) -> int:
    with open(fixtures_path) as f:
        cases = json.load(f)

    cfg = DetectorConfig(package_threshold=pkg_threshold, person_threshold=person_threshold)
    detector = YoloDetector(config=cfg)

    pkg_cm = ConfusionMatrix()
    person_cm = ConfusionMatrix()
    results: list[EvalResult] = []
    now = datetime.now(timezone.utc)

    for case in cases:
        req = AnalyzeRequest(
            camera_id="eval_cam",
            zone_id="front_door",
            frame_b64=case["frame_tag"],
            captured_at=now,
        )
        detections = detector.analyze(req)
        labels = {d.label for d in detections}

        pred_pkg = "package" in labels
        pred_person = "person" in labels
        exp_pkg = bool(case.get("expected_package", False))
        exp_person = bool(case.get("expected_person", False))

        pkg_cm.record(exp_pkg, pred_pkg)
        person_cm.record(exp_person, pred_person)

        passed = pred_pkg == exp_pkg and pred_person == exp_person
        results.append(EvalResult(
            name=case["name"],
            expected_package=exp_pkg,
            expected_person=exp_person,
            predicted_package=pred_pkg,
            predicted_person=pred_person,
            passed=passed,
        ))

    # Print per-case results.
    width = max(len(r.name) for r in results) + 2
    header = f"{'Case':<{width}}  {'pkg exp':>7}  {'pkg got':>7}  {'per exp':>7}  {'per got':>7}  {'OK':>4}"
    print(header)
    print("-" * len(header))
    for r in results:
        marker = "✓" if r.passed else "✗"
        print(
            f"{r.name:<{width}}  "
            f"{'yes' if r.expected_package else 'no':>7}  "
            f"{'yes' if r.predicted_package else 'no':>7}  "
            f"{'yes' if r.expected_person else 'no':>7}  "
            f"{'yes' if r.predicted_person else 'no':>7}  "
            f"{marker:>4}"
        )

    print()
    print(f"package: {pkg_cm.summary()}")
    print(f"person:  {person_cm.summary()}")

    failures = sum(1 for r in results if not r.passed)
    print(f"\n{len(results) - failures}/{len(results)} cases passed")

    if pkg_cm.precision < 0.90 or pkg_cm.recall < 0.90:
        print("FAIL: package precision/recall below 0.90 gate", file=sys.stderr)
        return 1
    return 0


def main() -> None:
    parser = argparse.ArgumentParser(description="Koala detection evaluation runner")
    parser.add_argument("--fixtures", type=Path, default=DEFAULT_FIXTURES)
    parser.add_argument("--pkg-threshold", type=float, default=0.55)
    parser.add_argument("--person-threshold", type=float, default=0.50)
    args = parser.parse_args()

    sys.exit(run_eval(args.fixtures, args.pkg_threshold, args.person_threshold))


if __name__ == "__main__":
    main()
