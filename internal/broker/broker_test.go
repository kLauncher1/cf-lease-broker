package broker

import (
	"testing"
	"time"
)

func TestPoolAndLeaseLifecycle(t *testing.T) {
	pm := NewPoolManager()
	engine := NewLeaseEngine(pm)

	accounts := pm.ListAccounts()
	if len(accounts) < 3 {
		t.Fatalf("expected at least 3 initial accounts, got %d", len(accounts))
	}

	// 1. Grant Lease
	voucher, err := engine.GrantLease("analytics-agent", "Workers", 1*time.Hour)
	if err != nil {
		t.Fatalf("GrantLease failed: %v", err)
	}
	if voucher.LeaseID == "" || voucher.TokenKey == "" {
		t.Errorf("expected non-empty LeaseID and TokenKey")
	}
	if voucher.IsRevoked {
		t.Errorf("newly granted lease must not be revoked")
	}

	// 2. Revoke Lease
	err = engine.RevokeLease(voucher.LeaseID)
	if err != nil {
		t.Fatalf("RevokeLease failed: %v", err)
	}

	leases := engine.ListLeases()
	if len(leases) != 1 || !leases[0].IsRevoked {
		t.Errorf("expected 1 revoked lease in registry")
	}

	// 3. Failover Trigger
	err = pm.TriggerFailover(voucher.AccountID)
	if err != nil {
		t.Fatalf("TriggerFailover failed: %v", err)
	}

	optAcc, err := pm.SelectOptimalAccount()
	if err != nil {
		t.Fatalf("expected next healthy account after failover: %v", err)
	}
	if optAcc.ID == voucher.AccountID {
		t.Errorf("optimal account must not be the failed account")
	}

	// 4. Test Pool Quota Management Helpers
	healthyIDs := pm.ListHealthyAccountIDs()
	if len(healthyIDs) != len(accounts)-1 {
		t.Errorf("expected %d healthy accounts, got %d", len(accounts)-1, len(healthyIDs))
	}

	targetID := healthyIDs[0]
	origAcc, err := pm.GetAccount(targetID)
	if err != nil {
		t.Fatalf("GetAccount failed: %v", err)
	}

	if err := pm.UpdateR2Storage(targetID, 50.5); err != nil {
		t.Fatalf("UpdateR2Storage failed: %v", err)
	}
	if err := pm.IncrementKVRead(targetID); err != nil {
		t.Fatalf("IncrementKVRead failed: %v", err)
	}
	if err := pm.IncrementWorkerUsed(targetID, 100); err != nil {
		t.Fatalf("IncrementWorkerUsed failed: %v", err)
	}

	updatedAcc, err := pm.GetAccount(targetID)
	if err != nil {
		t.Fatalf("GetAccount after updates failed: %v", err)
	}
	if updatedAcc.R2StorageMB != origAcc.R2StorageMB+50.5 {
		t.Errorf("expected R2 storage %.1f, got %.1f", origAcc.R2StorageMB+50.5, updatedAcc.R2StorageMB)
	}
	if updatedAcc.KVReadCount != origAcc.KVReadCount+1 {
		t.Errorf("expected KV read count %d, got %d", origAcc.KVReadCount+1, updatedAcc.KVReadCount)
	}
	if updatedAcc.WorkerUsed != origAcc.WorkerUsed+100 {
		t.Errorf("expected Worker used %d, got %d", origAcc.WorkerUsed+100, updatedAcc.WorkerUsed)
	}
}

func TestLeaseVoucherValidation(t *testing.T) {
	pm := NewPoolManager()
	engine := NewLeaseEngine(pm)

	v, err := engine.GrantLease("api-service", "KV", 1*time.Hour)
	if err != nil {
		t.Fatalf("GrantLease failed: %v", err)
	}

	// 1. Valid lookup
	got, exists := engine.GetLease(v.LeaseID)
	if !exists || got.TokenKey != v.TokenKey {
		t.Fatalf("GetLease failed, exists=%v", exists)
	}

	// 2. Constant-time validation with valid token
	if !engine.ValidateVoucher(v.LeaseID, v.TokenKey) {
		t.Error("expected valid voucher token validation to return true")
	}

	// 3. Constant-time validation with wrong token
	if engine.ValidateVoucher(v.LeaseID, "wrong_token_key") {
		t.Error("expected wrong token validation to return false")
	}

	// 4. Non-existent lease ID
	if engine.ValidateVoucher("non_existent_id", v.TokenKey) {
		t.Error("expected non-existent lease ID to return false")
	}

	// 5. Revoked voucher validation
	if err := engine.RevokeLease(v.LeaseID); err != nil {
		t.Fatalf("RevokeLease failed: %v", err)
	}
	if engine.ValidateVoucher(v.LeaseID, v.TokenKey) {
		t.Error("expected revoked voucher validation to return false")
	}
}
