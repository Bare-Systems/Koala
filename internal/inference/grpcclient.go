package inference

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/barelabs/koala/internal/zone"
	pb "github.com/barelabs/koala/proto/inferencev1"
)

const contractVersion = "1"

// circuitState represents the state of the circuit breaker.
type circuitState int

const (
	circuitClosed   circuitState = iota // normal operation
	circuitOpen                         // blocking requests after threshold failures
	circuitHalfOpen                     // probing after timeout
)

// GRPCClient implements Client using gRPC transport with an integrated
// circuit breaker to avoid hammering an unavailable worker.
type GRPCClient struct {
	conn   *grpc.ClientConn
	stub   pb.InferenceWorkerClient
	target string

	// Circuit breaker state
	mu               sync.Mutex
	state            circuitState
	failureCount     int
	failureThreshold int
	lastFailure      time.Time
	openDuration     time.Duration
}

// NewGRPCClient creates a gRPC inference client with a circuit breaker.
// target should be a gRPC address such as "localhost:50051".
// failureThreshold is the number of consecutive failures before tripping the circuit.
// openDuration is how long to wait before allowing a probe request through.
func NewGRPCClient(target string, failureThreshold int, openDuration time.Duration) (*GRPCClient, error) {
	if failureThreshold <= 0 {
		failureThreshold = 5
	}
	if openDuration <= 0 {
		openDuration = 15 * time.Second
	}
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("grpc dial %s: %w", target, err)
	}
	return &GRPCClient{
		conn:             conn,
		stub:             pb.NewInferenceWorkerClient(conn),
		target:           target,
		state:            circuitClosed,
		failureThreshold: failureThreshold,
		openDuration:     openDuration,
	}, nil
}

// Close releases the underlying gRPC connection.
func (c *GRPCClient) Close() error {
	return c.conn.Close()
}

func (c *GRPCClient) AnalyzeFrame(ctx context.Context, req FrameRequest) (FrameResponse, error) {
	if err := c.checkCircuit(); err != nil {
		return FrameResponse{}, err
	}

	var frameBytes []byte
	if req.FrameB64 != "" {
		var decErr error
		frameBytes, decErr = base64.StdEncoding.DecodeString(req.FrameB64)
		if decErr != nil {
			return FrameResponse{}, fmt.Errorf("decode frame_b64: %w", decErr)
		}
	}

	pbReq := &pb.FrameRequest{
		CameraId:        req.CameraID,
		ZoneId:          req.ZoneID,
		Frame:           frameBytes,
		CapturedAtUnixMs: req.Captured.UnixMilli(),
		ContractVersion: contractVersion,
	}

	pbResp, err := c.stub.AnalyzeFrame(ctx, pbReq)
	if err != nil {
		c.recordFailure()
		return FrameResponse{}, fmt.Errorf("grpc AnalyzeFrame: %w", err)
	}

	c.recordSuccess()
	resp := FrameResponse{
		ModelVersion: pbResp.GetModelVersion(),
		Detections:   make([]Detection, 0, len(pbResp.GetDetections())),
	}
	for _, d := range pbResp.GetDetections() {
		det := Detection{
			CameraID:   d.GetCameraId(),
			ZoneID:     d.GetZoneId(),
			Label:      d.GetLabel(),
			Confidence: float64(d.GetConfidence()),
			Timestamp:  time.UnixMilli(d.GetTimestampUnixMs()).UTC(),
		}
		if b := d.GetBbox(); b != nil {
			det.BBox = zone.BBox{
				X: float64(b.GetX()),
				Y: float64(b.GetY()),
				W: float64(b.GetWidth()),
				H: float64(b.GetHeight()),
			}
		}
		resp.Detections = append(resp.Detections, det)
	}
	return resp, nil
}

func (c *GRPCClient) WorkerHealth(ctx context.Context) (HealthResponse, error) {
	if err := c.checkCircuit(); err != nil {
		return HealthResponse{}, err
	}

	pbResp, err := c.stub.WorkerHealth(ctx, &pb.HealthRequest{})
	if err != nil {
		c.recordFailure()
		// Translate gRPC Unavailable to a typed error the caller can inspect.
		if status.Code(err) == codes.Unavailable {
			return HealthResponse{Status: "degraded"}, nil
		}
		return HealthResponse{}, fmt.Errorf("grpc WorkerHealth: %w", err)
	}

	// Enforce contract version compatibility.
	workerContract := pbResp.GetContractVersion()
	if workerContract != "" && workerContract != contractVersion {
		c.recordFailure()
		return HealthResponse{}, fmt.Errorf("inference contract version mismatch: orchestrator=%s worker=%s", contractVersion, workerContract)
	}

	c.recordSuccess()
	return HealthResponse{Status: pbResp.GetStatus()}, nil
}

// ─── Circuit breaker ─────────────────────────────────────────────────────────

func (c *GRPCClient) checkCircuit() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch c.state {
	case circuitClosed:
		return nil
	case circuitOpen:
		if time.Since(c.lastFailure) >= c.openDuration {
			c.state = circuitHalfOpen
			return nil
		}
		return fmt.Errorf("inference circuit open: worker unavailable (last failure %s ago)", time.Since(c.lastFailure).Round(time.Second))
	case circuitHalfOpen:
		return nil
	}
	return nil
}

func (c *GRPCClient) recordFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failureCount++
	c.lastFailure = time.Now()
	if c.failureCount >= c.failureThreshold {
		c.state = circuitOpen
	}
}

func (c *GRPCClient) recordSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failureCount = 0
	c.state = circuitClosed
}
