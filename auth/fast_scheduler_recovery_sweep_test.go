package auth

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestSweepFastSchedulerRecoveredRestoresExpiredCooldown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	acc := newFastSchedulerTestAccount(1, HealthTierHealthy, 100, 4)
	s := (&Store{accounts: []*Account{acc}, backgroundCtx: ctx}).withMaxConcurrency(4)
	s.rebuildAccountIndex()
	s.SetSchedulerEngine("indexed")

	s.MarkCooldown(acc, time.Minute, "forbidden")
	scheduler := s.getFastScheduler()
	if scheduler.Contains(acc.DBID) {
		t.Fatal("cooling account was not removed from the index")
	}

	// 冷却纯靠时间到期:不产生任何事件,索引不会自动回插。
	acc.mu.Lock()
	acc.CooldownUtil = time.Now().Add(-time.Second)
	acc.mu.Unlock()
	if scheduler.Contains(acc.DBID) {
		t.Fatal("index restored the account without an event")
	}

	if got := s.sweepFastSchedulerRecovered(); got != 1 {
		t.Fatalf("sweepFastSchedulerRecovered() = %d, want 1", got)
	}
	if !scheduler.Contains(acc.DBID) {
		t.Fatal("expired account was not restored to the index")
	}
	// 回插后立即可选,无需等待全局 miss 兜底令牌。
	s.indexedMissFallbackNS.Store(time.Now().UnixNano())
	got := s.NextExcludingWithDispatch(0, nil, nil, DispatchPolicyStandard)
	if got == nil || got.DBID != acc.DBID {
		t.Fatalf("NextExcludingWithDispatch() = %v, want account 1", got)
	}
	s.Release(got)
}

func TestSweepFastSchedulerRecoveredSkipsUnavailableAccounts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cooling := newFastSchedulerTestAccount(1, HealthTierHealthy, 100, 4)
	disabled := newFastSchedulerTestAccount(2, HealthTierHealthy, 100, 4)
	s := (&Store{accounts: []*Account{cooling, disabled}, backgroundCtx: ctx}).withMaxConcurrency(4)
	s.rebuildAccountIndex()
	s.SetSchedulerEngine("indexed")

	s.MarkCooldown(cooling, time.Minute, "forbidden")
	atomic.StoreInt32(&disabled.Disabled, 1)
	s.fastSchedulerUpdate(disabled)

	if got := s.sweepFastSchedulerRecovered(); got != 0 {
		t.Fatalf("sweepFastSchedulerRecovered() = %d, want 0", got)
	}
	scheduler := s.getFastScheduler()
	if scheduler.Contains(cooling.DBID) {
		t.Fatal("account still cooling was restored")
	}
}

func TestNeedsClaudeUnauthorizedRecovery(t *testing.T) {
	now := time.Now()
	newBanned := func() *Account {
		return &Account{
			DBID: 1, UpstreamType: UpstreamClaude, AccessToken: "at", RefreshToken: "rt",
			Status: StatusCooldown, CooldownReason: "unauthorized", CooldownUtil: now.Add(-time.Minute),
			HealthTier: HealthTierBanned,
		}
	}
	if !newBanned().needsClaudeUnauthorizedRecovery(now, time.Hour) {
		t.Fatal("expired 401 isolation should be recovered")
	}
	active := newBanned()
	active.CooldownUtil = now.Add(time.Hour)
	if active.needsClaudeUnauthorizedRecovery(now, time.Hour) {
		t.Fatal("active 401 cooldown must not be recovered early")
	}
	recent := newBanned()
	recent.LastRecoveryProbeAt = now.Add(-time.Minute)
	if recent.needsClaudeUnauthorizedRecovery(now, time.Hour) {
		t.Fatal("recovery must respect the probe interval")
	}
	noRT := newBanned()
	noRT.RefreshToken = ""
	if noRT.needsClaudeUnauthorizedRecovery(now, time.Hour) {
		t.Fatal("account without refresh token cannot be recovered by refresh")
	}
	disabledOnly := newBanned()
	disabledOnly.HealthTier = HealthTierWarm
	atomic.StoreInt32(&disabledOnly.Disabled, 1)
	if !disabledOnly.needsClaudeUnauthorizedRecovery(now, time.Hour) {
		t.Fatal("lingering Disabled flag should be recovered")
	}
	healthy := newBanned()
	healthy.HealthTier = HealthTierHealthy
	if healthy.needsClaudeUnauthorizedRecovery(now, time.Hour) {
		t.Fatal("healthy account needs no recovery")
	}
	codex := newBanned()
	codex.UpstreamType = ""
	if codex.needsClaudeUnauthorizedRecovery(now, time.Hour) {
		t.Fatal("non-Claude accounts use the generic recovery probe")
	}
}
