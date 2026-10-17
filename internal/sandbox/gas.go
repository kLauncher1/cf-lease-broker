package sandbox

import (
	"fmt"
	"sync"
)

// Standard deterministic gas weights per WebAssembly instruction classification.
const (
	// GasBasicOp costs 1 gas unit for register moves, nop, drop, and constants.
	GasBasicOp uint64 = 1

	// GasArithmetic costs 2 gas units for integer/floating point math and bitwise operations.
	GasArithmetic uint64 = 2

	// GasMemoryAccess costs 10 gas units for i32.load, i32.store, and bound-checked buffer reads/writes.
	GasMemoryAccess uint64 = 10

	// GasBranchLoop costs 5 gas units per loop cycle, block branch, and conditional jump.
	GasBranchLoop uint64 = 5

	// GasFunctionCall costs 20 gas units for call, call_indirect, and stack frame allocation.
	GasFunctionCall uint64 = 20

	// GasMemoryGrowPerPage costs 100 gas units per 64 KiB memory page allocation.
	GasMemoryGrowPerPage uint64 = 100
)

// GasMeter enforces strict instruction budgeting to prevent infinite loops and runaway compute.
type GasMeter struct {
	mu        sync.Mutex
	limit     uint64
	consumed  uint64
	loopSteps uint64
	exhausted bool
}

// NewGasMeter creates a thread-safe deterministic gas accountant with a given gas ceiling.
func NewGasMeter(limit uint64) *GasMeter {
	if limit == 0 {
		limit = DefaultGasLimit
	}
	return &GasMeter{
		limit:     limit,
		consumed:  0,
		loopSteps: 0,
		exhausted: false,
	}
}

// Consume debits a specific instruction weight against the remaining budget.
// If the debited cost would exceed the limit, it marks the meter exhausted and returns ErrGasExhausted.
func (g *GasMeter) Consume(cost uint64) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.exhausted {
		return ErrGasExhausted
	}

	// Prevent uint64 overflow
	if g.consumed+cost < g.consumed || g.consumed+cost > g.limit {
		g.consumed = g.limit
		g.exhausted = true
		return fmt.Errorf("%w: gas required (%d) exceeds remaining budget (%d)",
			ErrGasExhausted, cost, g.limit-g.consumed)
	}

	g.consumed += cost
	return nil
}

// ConsumeLoopStep debits GasBranchLoop and increments loop counter to trap infinite iterations.
func (g *GasMeter) ConsumeLoopStep() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.exhausted {
		return ErrGasExhausted
	}

	cost := GasBranchLoop
	if g.consumed+cost < g.consumed || g.consumed+cost > g.limit {
		g.consumed = g.limit
		g.exhausted = true
		return fmt.Errorf("%w: infinite loop trapped at iteration %d (budget %d exhausted)",
			ErrGasExhausted, g.loopSteps+1, g.limit)
	}

	g.loopSteps++
	g.consumed += cost
	return nil
}

// Remaining returns the number of unused gas units available in the current budget.
func (g *GasMeter) Remaining() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.exhausted || g.consumed >= g.limit {
		return 0
	}
	return g.limit - g.consumed
}

// Used returns the cumulative gas units debited during this execution run.
func (g *GasMeter) Used() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.consumed
}

// Limit returns the total gas ceiling configured for the meter.
func (g *GasMeter) Limit() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.limit
}

// LoopSteps returns the total number of loop iterations executed.
func (g *GasMeter) LoopSteps() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.loopSteps
}

// IsExhausted returns true if the budget was depleted during execution.
func (g *GasMeter) IsExhausted() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.exhausted
}

// Reset resets the gas counter and sets a new budget ceiling for subsequent runs.
func (g *GasMeter) Reset(budget uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if budget == 0 {
		budget = DefaultGasLimit
	}
	g.limit = budget
	g.consumed = 0
	g.loopSteps = 0
	g.exhausted = false
}

// Snapshot exports the instantaneous gas metrics.
func (g *GasMeter) Snapshot() GasTracker {
	g.mu.Lock()
	defer g.mu.Unlock()

	rem := uint64(0)
	if !g.exhausted && g.consumed < g.limit {
		rem = g.limit - g.consumed
	}

	return GasTracker{
		Limit:     g.limit,
		Consumed:  g.consumed,
		Remaining: rem,
		LoopSteps: g.loopSteps,
		Exhausted: g.exhausted,
	}
}
