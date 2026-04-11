// pool.go manages a pool of N simulated agents with smooth ramp-up,
// steady-state, and ramp-down phases.
//
// The pool implements the three-phase scenario model:
//  1. Ramp-up: agents are started exponentially (doubling every rampStep)
//     until N is reached. This avoids thundering-herd on the gateway.
//  2. Steady-state: all agents run at full load for the configured duration.
//  3. Ramp-down: agents are cancelled in reverse order with short delays.
//
// Each agent runs in its own goroutine. The pool tracks global metrics and
// provides a Done channel for the runner to await completion.
package simulator

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/schollz/progressbar/v3"
)

// PoolConfig configures a simulation pool.
type PoolConfig struct {
	// Target number of concurrent agents.
	AgentCount int
	// How long to ramp from 0 to AgentCount. Exponential schedule.
	RampDuration time.Duration
	// How long to run at full load.
	SteadyDuration time.Duration
	// How long to ramp down.
	RampDownDuration time.Duration
	// Gateway address for all agents.
	GatewayAddr string
	// TenantID for all agents.
	TenantID string
	// Agent cert issuer. Pool issues individual certs per agent.
	CA *TestCA
	// Agent configuration template (GatewayAddr, TenantID, CA overridden).
	AgentCfgTemplate AgentConfig
	// Prometheus metrics (shared across all agents).
	Metrics *SimulatorMetrics
	// Seed for deterministic scenarios (0 = random).
	Seed uint64
	// ShowProgress enables a terminal progress bar (disable in CI).
	ShowProgress bool
}

// AgentPool manages N concurrent simulated agents.
type AgentPool struct {
	cfg     PoolConfig
	agents  []*SimAgent
	cancels []context.CancelFunc
	wg      sync.WaitGroup
	started atomic.Int64
	stopped atomic.Int64
	errors  atomic.Int64
	mu      sync.Mutex
	log     *slog.Logger
}

// NewAgentPool creates a pool but does not start it.
func NewAgentPool(cfg PoolConfig) *AgentPool {
	return &AgentPool{
		cfg:    cfg,
		agents: make([]*SimAgent, 0, cfg.AgentCount),
		log:    slog.Default().With("component", "agent_pool"),
	}
}

// Run executes the full ramp-up → steady → ramp-down lifecycle.
// It blocks until the pool finishes or ctx is cancelled.
func (p *AgentPool) Run(ctx context.Context) error {
	p.log.Info("pool starting",
		"agents", p.cfg.AgentCount,
		"ramp_duration", p.cfg.RampDuration,
		"steady_duration", p.cfg.SteadyDuration,
	)

	var bar *progressbar.ProgressBar
	if p.cfg.ShowProgress {
		bar = progressbar.NewOptions(p.cfg.AgentCount,
			progressbar.OptionSetDescription("Ramping agents"),
			progressbar.OptionShowCount(),
			progressbar.OptionSetWidth(40),
		)
	}

	// Phase 1: Ramp-up.
	if err := p.rampUp(ctx, bar); err != nil {
		return fmt.Errorf("ramp-up failed: %w", err)
	}

	if bar != nil {
		_ = bar.Finish()
	}

	p.log.Info("steady state reached",
		"agents_active", p.started.Load(),
		"steady_duration", p.cfg.SteadyDuration,
	)

	// Phase 2: Steady-state — wait or until ctx cancelled.
	select {
	case <-ctx.Done():
		p.log.Info("pool cancelled during steady state")
	case <-time.After(p.cfg.SteadyDuration):
		p.log.Info("steady state complete; beginning ramp-down")
	}

	// Phase 3: Ramp-down.
	p.rampDown()
	p.wg.Wait()

	p.log.Info("pool finished",
		"started", p.started.Load(),
		"stopped", p.stopped.Load(),
		"errors", p.errors.Load(),
	)
	return nil
}

// rampUp starts agents according to an exponential schedule.
// Schedule: start 1, 2, 4, 8, ... agents per step until AgentCount is reached.
// Each step takes rampDuration/log2(AgentCount) so total ramp time is ~rampDuration.
func (p *AgentPool) rampUp(ctx context.Context, bar *progressbar.ProgressBar) error {
	n := p.cfg.AgentCount
	if n <= 0 {
		return nil
	}

	// Compute step interval: divide ramp duration by number of steps.
	steps := 0
	for s := 1; s < n; s *= 2 {
		steps++
	}
	if steps == 0 {
		steps = 1
	}
	stepInterval := p.cfg.RampDuration / time.Duration(steps)
	if stepInterval < 10*time.Millisecond {
		stepInterval = 10 * time.Millisecond
	}

	// Start with 1 and double each step.
	target := 1
	for p.started.Load() < int64(n) {
		// Start min(target, remaining) agents.
		remaining := n - int(p.started.Load())
		startNow := target
		if startNow > remaining {
			startNow = remaining
		}

		for i := 0; i < startNow; i++ {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err := p.startOneAgent(ctx); err != nil {
				p.errors.Add(1)
				p.log.Error("failed to start agent", "error", err)
				// Continue — partial ramp is acceptable.
			}
			if bar != nil {
				_ = bar.Add(1)
			}
		}

		if p.started.Load() >= int64(n) {
			break
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(stepInterval):
		}

		if target < n {
			target *= 2
		}
	}

	return nil
}

// startOneAgent issues a cert and starts a single agent goroutine.
func (p *AgentPool) startOneAgent(ctx context.Context) error {
	endpointID := uuid.New().String()

	cert, err := p.cfg.CA.IssueAgentCert(endpointID)
	if err != nil {
		return fmt.Errorf("issue cert for %s: %w", endpointID, err)
	}

	agentCfg := p.cfg.AgentCfgTemplate
	agentCfg.GatewayAddr = p.cfg.GatewayAddr
	agentCfg.TenantID = p.cfg.TenantID
	agentCfg.EndpointID = endpointID
	agentCfg.TLSConfig = p.cfg.CA.ClientTLSConfig(cert, "gateway.personel.test")
	agentCfg.Metrics = p.cfg.Metrics
	if p.cfg.Seed != 0 {
		agentCfg.Seed = p.cfg.Seed ^ uint64(p.started.Load())
	}

	agent := NewSimAgent(agentCfg, cert)

	agentCtx, cancel := context.WithCancel(ctx)

	p.mu.Lock()
	p.agents = append(p.agents, agent)
	p.cancels = append(p.cancels, cancel)
	p.mu.Unlock()

	p.started.Add(1)
	p.wg.Add(1)

	go func() {
		defer p.wg.Done()
		defer p.stopped.Add(1)
		agent.Run(agentCtx)
	}()

	return nil
}

// rampDown cancels agents in reverse order with short delays.
func (p *AgentPool) rampDown() {
	p.mu.Lock()
	cancels := make([]context.CancelFunc, len(p.cancels))
	copy(cancels, p.cancels)
	p.mu.Unlock()

	p.log.Info("ramp-down starting", "agents", len(cancels))

	stepDelay := p.cfg.RampDownDuration / time.Duration(len(cancels)+1)
	if stepDelay < time.Millisecond {
		stepDelay = time.Millisecond
	}
	// Cap per-agent delay so ramp-down does not take forever.
	if stepDelay > 100*time.Millisecond {
		stepDelay = 100 * time.Millisecond
	}

	for i := len(cancels) - 1; i >= 0; i-- {
		cancels[i]()
		if i > 0 {
			time.Sleep(stepDelay)
		}
	}
}

// ActiveAgents returns the current count of connected agents.
func (p *AgentPool) ActiveAgents() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	count := 0
	for _, a := range p.agents {
		if a.Connected() {
			count++
		}
	}
	return count
}

// Stats returns pool summary statistics.
type PoolStats struct {
	Started  int64
	Stopped  int64
	Errors   int64
	Active   int
}

// Stats returns current pool statistics.
func (p *AgentPool) Stats() PoolStats {
	return PoolStats{
		Started: p.started.Load(),
		Stopped: p.stopped.Load(),
		Errors:  p.errors.Load(),
		Active:  p.ActiveAgents(),
	}
}
