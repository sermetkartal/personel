package grpcserver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"

	"github.com/personel/gateway/internal/config"
	"github.com/personel/gateway/internal/heartbeat"
	"github.com/personel/gateway/internal/liveview"
	natspub "github.com/personel/gateway/internal/nats"
	"github.com/personel/gateway/internal/observability"
	"github.com/personel/gateway/internal/postgres"
	"github.com/personel/gateway/internal/vault"
	personelv1 "github.com/personel/proto/personel/v1"
)

// Server wraps the gRPC server with all dependencies wired in.
type Server struct {
	grpc    *grpc.Server
	handler *streamHandler
	logger  *slog.Logger
}

// Deps holds all external dependencies required to construct a gateway Server.
type Deps struct {
	DB        *postgres.Pool
	Publisher *natspub.Publisher
	Vault     *vault.Client
	Monitor   *heartbeat.Monitor
	Router    *liveview.Router
	Metrics   *observability.Metrics
	Logger    *slog.Logger
	Cfg       *config.GatewayConfig
}

// New constructs a gRPC server with mTLS credentials and all interceptors wired.
func New(deps Deps) (*Server, error) {
	tlsCreds, err := buildTLSCredentials(deps.Cfg.GRPC)
	if err != nil {
		return nil, fmt.Errorf("grpcserver: build TLS credentials: %w", err)
	}

	rl := NewRateLimiter(deps.Cfg.RateLimit, deps.Metrics)

	handler := &streamHandler{
		db:         deps.DB,
		pub:        deps.Publisher,
		vc:         deps.Vault,
		rl:         rl,
		hvMonitor:  deps.Monitor,
		lvRouter:   deps.Router,
		metrics:    deps.Metrics,
		logger:     deps.Logger,
		maxUnacked: deps.Cfg.Backpressure.MaxUnackedBatches,
		serverVer:  deps.Cfg.ServerVersion,
	}
	if handler.maxUnacked == 0 {
		handler.maxUnacked = 16 // conservative default
	}

	unaryAuth, streamAuth := authInterceptor(deps.DB, deps.Vault, deps.Metrics, deps.Logger)

	grpcSrv := grpc.NewServer(
		grpc.Creds(tlsCreds),
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(
			loggingUnaryInterceptor(deps.Logger),
			unaryAuth,
		),
		grpc.ChainStreamInterceptor(
			loggingStreamInterceptor(deps.Logger),
			streamAuth,
		),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle: 5 * time.Minute,
			Time:              30 * time.Second,
			Timeout:           10 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             12 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.MaxRecvMsgSize(deps.Cfg.GRPC.MaxRecvMsgSize),
	)

	personelv1.RegisterAgentServiceServer(grpcSrv, &agentServiceWrapper{handler: handler})

	return &Server{grpc: grpcSrv, handler: handler, logger: deps.Logger}, nil
}

// Serve starts listening on the configured address and blocks until the server
// is stopped.
func (s *Server) Serve(ctx context.Context, listenAddr string) error {
	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("grpcserver: listen on %s: %w", listenAddr, err)
	}
	s.logger.InfoContext(ctx, "grpcserver: listening", slog.String("addr", listenAddr))

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.grpc.Serve(lis)
	}()

	select {
	case <-ctx.Done():
		s.logger.InfoContext(ctx, "grpcserver: context cancelled, initiating graceful stop")
		s.grpc.GracefulStop()
		return nil
	case err := <-errCh:
		return fmt.Errorf("grpcserver: serve error: %w", err)
	}
}

// GracefulStop initiates a graceful shutdown with the given timeout.
// New streams are rejected; existing streams are allowed to complete.
func (s *Server) GracefulStop(timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	stopped := make(chan struct{})
	go func() {
		s.grpc.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
		s.logger.Info("grpcserver: graceful stop completed")
	case <-ctx.Done():
		s.logger.Warn("grpcserver: graceful stop timeout, forcing stop")
		s.grpc.Stop()
	}
}

// buildTLSCredentials constructs mTLS server credentials from PEM files.
// It requires the client CA (tenant CA) for client certificate validation.
func buildTLSCredentials(cfg config.GRPCConfig) (credentials.TransportCredentials, error) {
	cert, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load server cert/key: %w", err)
	}

	caCertPEM, err := os.ReadFile(cfg.TLSClientCAFile)
	if err != nil {
		return nil, fmt.Errorf("read client CA file %s: %w", cfg.TLSClientCAFile, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCertPEM) {
		return nil, fmt.Errorf("parse client CA certificate from %s", cfg.TLSClientCAFile)
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    pool,
		// RequireAndVerifyClientCert enforces mTLS on every connection.
		ClientAuth: tls.RequireAndVerifyClientCert,
		MinVersion: tls.VersionTLS13,
	}
	return credentials.NewTLS(tlsCfg), nil
}

// loggingUnaryInterceptor logs unary RPC calls with method, duration, and status.
func loggingUnaryInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		logger.InfoContext(ctx, "grpc.unary",
			slog.String("method", info.FullMethod),
			slog.Duration("elapsed", time.Since(start)),
			slog.String("error", errStr(err)),
		)
		return resp, err
	}
}

// loggingStreamInterceptor logs stream RPC lifecycle events.
func loggingStreamInterceptor(logger *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		err := handler(srv, ss)
		logger.InfoContext(ss.Context(), "grpc.stream",
			slog.String("method", info.FullMethod),
			slog.Duration("elapsed", time.Since(start)),
			slog.String("error", errStr(err)),
		)
		return err
	}
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// agentServiceWrapper adapts streamHandler to the generated AgentServiceServer interface.
type agentServiceWrapper struct {
	personelv1.UnimplementedAgentServiceServer
	handler *streamHandler
}

func (w *agentServiceWrapper) Stream(stream personelv1.AgentService_StreamServer) error {
	return w.handler.Stream(stream)
}
