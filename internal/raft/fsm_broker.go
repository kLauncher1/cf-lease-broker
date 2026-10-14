package raft

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kLauncher1/cf-lease-broker/internal/broker"
)

// BrokerFSM is the concrete Finite State Machine executing replicated transactions across the broker cluster.
type BrokerFSM struct {
	mu           sync.RWMutex
	poolManager  *broker.PoolManager
	leaseEngine  *broker.LeaseEngine
	appliedCount int64
}

// NewBrokerFSM creates a state machine bound to the resource pool and lease engines.
func NewBrokerFSM(pm *broker.PoolManager, le *broker.LeaseEngine) *BrokerFSM {
	return &BrokerFSM{
		poolManager: pm,
		leaseEngine: le,
	}
}

// UpdateQuotaPayload specifies incremental consumption updates replicated to all nodes.
type UpdateQuotaPayload struct {
	AccountID   string  `json:"account_id"`
	WorkerDelta int64   `json:"worker_delta,omitempty"`
	R2DeltaMB   float64 `json:"r2_delta_mb,omitempty"`
	KVReadDelta int64   `json:"kv_read_delta,omitempty"`
}

// RevokeLeasePayload specifies a lease to invalidate across the cluster.
type RevokeLeasePayload struct {
	LeaseID string `json:"lease_id"`
}

// FSMSnapshot represents a point-in-time state snapshot for log compaction and fast sync.
type FSMSnapshot struct {
	Timestamp time.Time              `json:"timestamp"`
	Accounts  []broker.AccountStatus `json:"accounts"`
	Leases    []broker.LeaseVoucher  `json:"leases"`
}

// Apply executes a committed log entry deterministically against local state.
func (fsm *BrokerFSM) Apply(entry LogEntry) (interface{}, error) {
	atomic.AddInt64(&fsm.appliedCount, 1)

	switch entry.Type {
	case CmdGrantLease:
		var voucher broker.LeaseVoucher
		if err := json.Unmarshal(entry.Data, &voucher); err != nil {
			return nil, fmt.Errorf("invalid grant lease payload: %w", err)
		}
		if fsm.leaseEngine != nil {
			fsm.leaseEngine.ApplyVoucher(voucher)
		}
		return voucher, nil

	case CmdRevokeLease:
		var req RevokeLeasePayload
		if err := json.Unmarshal(entry.Data, &req); err != nil {
			// Fallback if plain string passed
			req.LeaseID = string(entry.Data)
		}
		if req.LeaseID == "" {
			return nil, errors.New("empty lease ID in revoke payload")
		}
		if fsm.leaseEngine != nil {
			_ = fsm.leaseEngine.RevokeLease(req.LeaseID)
		}
		return map[string]string{"status": "revoked", "lease_id": req.LeaseID}, nil

	case CmdSyncAccount:
		// Attempt single account first
		var single broker.AccountStatus
		if err := json.Unmarshal(entry.Data, &single); err == nil && single.ID != "" {
			if fsm.poolManager != nil {
				fsm.poolManager.UpsertAccount(single)
			}
			return single, nil
		}

		// Attempt account list
		var list []broker.AccountStatus
		if err := json.Unmarshal(entry.Data, &list); err == nil {
			if fsm.poolManager != nil {
				fsm.poolManager.SyncAccounts(list)
			}
			return list, nil
		}
		return nil, errors.New("unrecognized sync account payload format")

	case CmdUpdateQuota:
		var qp UpdateQuotaPayload
		if err := json.Unmarshal(entry.Data, &qp); err != nil {
			return nil, fmt.Errorf("invalid update quota payload: %w", err)
		}
		if fsm.poolManager != nil {
			if qp.WorkerDelta != 0 {
				_ = fsm.poolManager.IncrementWorkerUsed(qp.AccountID, qp.WorkerDelta)
			}
			if qp.R2DeltaMB != 0 {
				_ = fsm.poolManager.UpdateR2Storage(qp.AccountID, qp.R2DeltaMB)
			}
			for i := int64(0); i < qp.KVReadDelta; i++ {
				_ = fsm.poolManager.IncrementKVRead(qp.AccountID)
			}
		}
		return qp, nil

	case CmdNoop:
		return nil, nil

	default:
		return nil, fmt.Errorf("unsupported raft command type: %s", entry.Type)
	}
}

// Snapshot serializes all active accounts and leases for log compaction.
func (fsm *BrokerFSM) Snapshot() ([]byte, error) {
	fsm.mu.RLock()
	defer fsm.mu.RUnlock()

	var accounts []broker.AccountStatus
	if fsm.poolManager != nil {
		accounts = fsm.poolManager.ListAccounts()
	}

	var leases []broker.LeaseVoucher
	if fsm.leaseEngine != nil {
		leases = fsm.leaseEngine.ListLeases()
	}

	snap := FSMSnapshot{
		Timestamp: time.Now().UTC(),
		Accounts:  accounts,
		Leases:    leases,
	}

	return json.Marshal(snap)
}

// Restore deserializes state snapshot and hydrates local engines.
func (fsm *BrokerFSM) Restore(snapshot []byte) error {
	fsm.mu.Lock()
	defer fsm.mu.Unlock()

	var snap FSMSnapshot
	if err := json.Unmarshal(snapshot, &snap); err != nil {
		return fmt.Errorf("failed to decode snapshot: %w", err)
	}

	if fsm.poolManager != nil && len(snap.Accounts) > 0 {
		fsm.poolManager.SyncAccounts(snap.Accounts)
	}

	if fsm.leaseEngine != nil {
		for _, lse := range snap.Leases {
			fsm.leaseEngine.ApplyVoucher(lse)
		}
	}

	return nil
}

// AppliedCount returns the number of log entries executed on this state machine.
func (fsm *BrokerFSM) AppliedCount() int64 {
	return atomic.LoadInt64(&fsm.appliedCount)
}
