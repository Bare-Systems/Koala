package inference

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/Bare-Systems/Koala/proto/inferencev1"
)

const testBufSize = 1 << 20 // 1 MiB

// fakeWorker is a configurable in-process gRPC implementation.
type fakeWorker struct {
	pb.UnimplementedInferenceWorkerServer

	// analyzeErr, if non-nil, is returned by AnalyzeFrame.
	analyzeErr error
	// analyzeResp, if non-nil, is returned by AnalyzeFrame on success.
	analyzeResp *pb.FrameResponse

	// healthStatus is returned by WorkerHealth.status.
	healthStatus string
	// healthContract is returned by WorkerHealth.contract_version.
	healthContract string
	// healthCode overrides the gRPC status code for WorkerHealth.
	healthCode codes.Code

	// calls counts AnalyzeFrame invocations.
	calls int
}

func (f *fakeWorker) AnalyzeFrame(_ context.Context, _ *pb.FrameRequest) (*pb.FrameResponse, error) {
	f.calls++
	if f.analyzeErr != nil {
		return nil, f.analyzeErr
	}
	if f.analyzeResp != nil {
		return f.analyzeResp, nil
	}
	return &pb.FrameResponse{ModelVersion: "test-v1"}, nil
}

func (f *fakeWorker) WorkerHealth(_ context.Context, _ *pb.HealthRequest) (*pb.HealthResponse, error) {
	if f.healthCode != codes.OK {
		return nil, status.Error(f.healthCode, "worker error")
	}
	s := f.healthStatus
	if s == "" {
		s = "ok"
	}
	c := f.healthContract
	if c == "" {
		c = contractVersion
	}
	return &pb.HealthResponse{Status: s, ContractVersion: c}, nil
}

// newTestClient starts an in-process gRPC server backed by srv and returns a
// connected GRPCClient.  Callers must call the returned cleanup function.
func newTestClient(t *testing.T, srv *fakeWorker) (*GRPCClient, func()) {
	t.Helper()
	lis := bufconn.Listen(testBufSize)
	s := grpc.NewServer()
	pb.RegisterInferenceWorkerServer(s, srv)
	go func() { _ = s.Serve(lis) }()

	conn, err := grpc.NewClient(
		"passthrough://bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}

	client := &GRPCClient{
		conn:             conn,
		stub:             pb.NewInferenceWorkerClient(conn),
		target:           "bufnet",
		state:            circuitClosed,
		failureThreshold: 3,
		openDuration:     5 * time.Millisecond,
	}
	return client, func() {
		_ = conn.Close()
		s.Stop()
		_ = lis.Close()
	}
}

// ─── AnalyzeFrame ─────────────────────────────────────────────────────────────

func TestGRPCClient_AnalyzeFrame_Success(t *testing.T) {
	srv := &fakeWorker{
		analyzeResp: &pb.FrameResponse{
			ModelVersion: "yolo-v1",
			Detections: []*pb.Detection{
				{
					CameraId:   "cam1",
					ZoneId:     "front_door",
					Label:      "package",
					Confidence: 0.92,
					Bbox:       &pb.BBox{X: 0.1, Y: 0.2, Width: 0.3, Height: 0.4},
				},
			},
		},
	}
	client, cleanup := newTestClient(t, srv)
	defer cleanup()

	resp, err := client.AnalyzeFrame(context.Background(), FrameRequest{
		CameraID: "cam1",
		ZoneID:   "front_door",
		Captured: time.Now(),
	})
	if err != nil {
		t.Fatalf("AnalyzeFrame: %v", err)
	}
	if resp.ModelVersion != "yolo-v1" {
		t.Fatalf("expected model_version=yolo-v1, got %q", resp.ModelVersion)
	}
	if len(resp.Detections) != 1 {
		t.Fatalf("expected 1 detection, got %d", len(resp.Detections))
	}
	det := resp.Detections[0]
	if det.Label != "package" {
		t.Fatalf("expected label=package, got %q", det.Label)
	}
	// proto float32 → float64 introduces ~1e-7 error; use tolerance comparison.
	if det.Confidence < 0.91 || det.Confidence > 0.93 {
		t.Fatalf("expected confidence≈0.92, got %f", det.Confidence)
	}
	// proto float32 → float64 loses ~1e-7; use ≈ comparison.
	approxEq := func(got, want float64) bool { return got >= want-1e-3 && got <= want+1e-3 }
	if !approxEq(det.BBox.X, 0.1) || !approxEq(det.BBox.Y, 0.2) ||
		!approxEq(det.BBox.W, 0.3) || !approxEq(det.BBox.H, 0.4) {
		t.Fatalf("unexpected bbox: %+v", det.BBox)
	}
}

func TestGRPCClient_AnalyzeFrame_SetsRequestID(t *testing.T) {
	// Verify a request ID is attached — just check it's non-empty by inspecting
	// what gets sent. We use a recording server for this.
	var seenRequestID string
	// Wrap with a custom AnalyzeFrame that captures the request_id.
	lis := bufconn.Listen(testBufSize)
	s := grpc.NewServer()
	recorder := &requestIDRecorder{requestID: &seenRequestID}
	pb.RegisterInferenceWorkerServer(s, recorder)
	go func() { _ = s.Serve(lis) }()
	conn, _ := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	client := &GRPCClient{
		conn:             conn,
		stub:             pb.NewInferenceWorkerClient(conn),
		state:            circuitClosed,
		failureThreshold: 3,
		openDuration:     15 * time.Second,
	}
	defer func() { _ = conn.Close(); s.Stop(); _ = lis.Close() }()

	_, _ = client.AnalyzeFrame(context.Background(), FrameRequest{CameraID: "c1", ZoneID: "z1", Captured: time.Now()})
	if seenRequestID == "" {
		t.Fatal("expected request_id to be set, got empty string")
	}
}

type requestIDRecorder struct {
	pb.UnimplementedInferenceWorkerServer
	requestID *string
}

func (r *requestIDRecorder) AnalyzeFrame(_ context.Context, req *pb.FrameRequest) (*pb.FrameResponse, error) {
	*r.requestID = req.GetRequestId()
	return &pb.FrameResponse{ModelVersion: "test-v1"}, nil
}

// ─── Circuit breaker ──────────────────────────────────────────────────────────

func TestGRPCClient_CircuitBreaker_TripsAfterThreshold(t *testing.T) {
	srv := &fakeWorker{analyzeErr: status.Error(codes.Unavailable, "worker down")}
	client, cleanup := newTestClient(t, srv)
	defer cleanup()
	client.failureThreshold = 2

	req := FrameRequest{CameraID: "c1", ZoneID: "z1", Captured: time.Now()}
	// First two calls should reach the server and fail (tripping the circuit).
	for i := 0; i < 2; i++ {
		_, err := client.AnalyzeFrame(context.Background(), req)
		if err == nil {
			t.Fatalf("call %d: expected error, got nil", i)
		}
	}
	if client.state != circuitOpen {
		t.Fatalf("expected circuit=open after %d failures, got %d", client.failureThreshold, client.state)
	}
}

func TestGRPCClient_CircuitBreaker_OpenReturnsErrorWithoutCallingServer(t *testing.T) {
	srv := &fakeWorker{analyzeErr: status.Error(codes.Unavailable, "down")}
	client, cleanup := newTestClient(t, srv)
	defer cleanup()
	client.failureThreshold = 1

	req := FrameRequest{CameraID: "c1", ZoneID: "z1", Captured: time.Now()}
	// Trip the circuit.
	_, _ = client.AnalyzeFrame(context.Background(), req)
	callsAfterTrip := srv.calls

	// Next call should be blocked by the open circuit — server must not be hit.
	_, err := client.AnalyzeFrame(context.Background(), req)
	if err == nil {
		t.Fatal("expected error from open circuit, got nil")
	}
	if !strings.Contains(err.Error(), "circuit open") {
		t.Fatalf("expected 'circuit open' in error, got: %v", err)
	}
	if srv.calls != callsAfterTrip {
		t.Fatalf("server was called while circuit is open (calls before=%d, after=%d)", callsAfterTrip, srv.calls)
	}
}

func TestGRPCClient_CircuitBreaker_HalfOpenAfterTimeout(t *testing.T) {
	srv := &fakeWorker{}
	client, cleanup := newTestClient(t, srv)
	defer cleanup()
	client.failureThreshold = 1
	client.openDuration = 5 * time.Millisecond

	// Trip the circuit.
	srv.analyzeErr = status.Error(codes.Unavailable, "down")
	req := FrameRequest{CameraID: "c1", ZoneID: "z1", Captured: time.Now()}
	_, _ = client.AnalyzeFrame(context.Background(), req)

	// Wait for openDuration to expire.
	time.Sleep(20 * time.Millisecond)

	// Remove the server error so the probe succeeds.
	srv.analyzeErr = nil
	_, err := client.AnalyzeFrame(context.Background(), req)
	if err != nil {
		t.Fatalf("expected probe through half-open circuit to succeed, got: %v", err)
	}
	if client.state != circuitClosed {
		t.Fatalf("expected circuit to reset to closed after successful probe, got %d", client.state)
	}
}

func TestGRPCClient_CircuitBreaker_SuccessResetsCount(t *testing.T) {
	srv := &fakeWorker{}
	client, cleanup := newTestClient(t, srv)
	defer cleanup()
	client.failureThreshold = 3

	req := FrameRequest{CameraID: "c1", ZoneID: "z1", Captured: time.Now()}
	// Two failures that don't trip the circuit yet.
	srv.analyzeErr = status.Error(codes.Unavailable, "down")
	for i := 0; i < 2; i++ {
		_, _ = client.AnalyzeFrame(context.Background(), req)
	}
	if client.failureCount != 2 {
		t.Fatalf("expected failureCount=2, got %d", client.failureCount)
	}

	// One success resets the count.
	srv.analyzeErr = nil
	_, err := client.AnalyzeFrame(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if client.failureCount != 0 {
		t.Fatalf("expected failureCount=0 after success, got %d", client.failureCount)
	}
	if client.state != circuitClosed {
		t.Fatalf("expected circuit closed, got %d", client.state)
	}
}

// ─── Contract version ─────────────────────────────────────────────────────────

func TestGRPCClient_WorkerHealth_ContractVersionMismatch(t *testing.T) {
	srv := &fakeWorker{healthContract: "99"} // wrong version
	client, cleanup := newTestClient(t, srv)
	defer cleanup()

	_, err := client.WorkerHealth(context.Background())
	if err == nil {
		t.Fatal("expected contract mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "contract version mismatch") {
		t.Fatalf("expected 'contract version mismatch' in error, got: %v", err)
	}
}

func TestGRPCClient_WorkerHealth_OK(t *testing.T) {
	srv := &fakeWorker{healthStatus: "ok", healthContract: contractVersion}
	client, cleanup := newTestClient(t, srv)
	defer cleanup()

	resp, err := client.WorkerHealth(context.Background())
	if err != nil {
		t.Fatalf("WorkerHealth: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("expected status=ok, got %q", resp.Status)
	}
}

func TestGRPCClient_WorkerHealth_GRPCUnavailable_ReturnsDegraded(t *testing.T) {
	srv := &fakeWorker{healthCode: codes.Unavailable}
	client, cleanup := newTestClient(t, srv)
	defer cleanup()

	resp, err := client.WorkerHealth(context.Background())
	if err != nil {
		t.Fatalf("Unavailable must return degraded, not error: %v", err)
	}
	if resp.Status != "degraded" {
		t.Fatalf("expected status=degraded, got %q", resp.Status)
	}
}

func TestGRPCClient_WorkerHealth_EmptyContractVersionAccepted(t *testing.T) {
	// Workers that omit contract_version (e.g. very old builds) should still work.
	srv := &fakeWorker{healthStatus: "ok", healthContract: ""}
	client, cleanup := newTestClient(t, srv)
	defer cleanup()

	resp, err := client.WorkerHealth(context.Background())
	if err != nil {
		t.Fatalf("empty contract_version must not fail: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("expected ok, got %q", resp.Status)
	}
}
