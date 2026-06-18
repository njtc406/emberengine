# Service Core Interface Tightening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor Service so it remains the framework's base capability container and runtime capability composer while narrowing `IService` usage and centralizing lifecycle state in `serviceState`.

**Architecture:** Keep `core.Service` as the embedded base capability container. Do not split Mailbox/RPC/Timer/Event/Concurrent/Profiler fields into artificial runtime structs. Add narrow service capability interfaces, keep `IService` as a migration-period composite, move lifecycle status and stop guard logic into a focused `serviceState`, return lifecycle operation failures as `error`, and narrow endpoint publication through exported interfaces that keep `INodeEndpointManager` type-consistent.

**Tech Stack:** Go, EmberEngine core packages, standard `testing`, `sync`, `sync/atomic`, existing `go test` workflow.

---

## Important implementation constraints

- Do not restructure `core.Service` into `serviceRuntime`, `serviceDispatch`, `serviceLifecycle`, or `serviceEndpointBinding`.
- Do not move Mailbox/RPC/Timer/Event/Concurrent/Profiler fields out of `Service`.
- Do not commit an intentionally non-compiling intermediate state.
- Lifecycle statuses describe phases only. Do not add `SvcStatusInitFailed`, `SvcStatusStartFailed`, or `SvcStatusStopFailed`.
- `Init`, `Start`, and `Stop` operation failures must be returned as `error` to the scheduler/caller. The scheduler decides whether to retry, rebuild, remove, alert, or force exit.
- `Stop()` is migrated in this plan from `Stop()` to `Stop() error`; all touched callers/tests must handle or intentionally ignore the returned error.
- Before editing Go symbols during execution, follow repository guidance in `AGENTS.md` and run impact analysis for the target symbol where GitNexus tooling is available.

---

## File Structure

### Created files

- `engine/pkg/core/service_state.go`
	- Owns service status transitions and stop idempotency.
	- Does not store lifecycle operation errors; errors are returned directly by `Init`, `Start`, and `Stop`.
  - Must not import mailbox/rpc/event packages.

- `engine/pkg/core/service_state_test.go`
	- Unit tests for stable status ordering, state transitions, idempotent stop request, and terminal-state protection.

- `engine/pkg/core/service_interface_test.go`
  - Compile-time assertions that `*Service` satisfies the new narrow interfaces.

### Modified files

- `engine/pkg/interfaces/IService.go`
  - Split broad service capabilities into narrow interfaces.
	- Change lifecycle control signature from `Stop()` to `Stop() error`.
  - Keep `IService` as a migration-period composite interface.
  - Keep legacy `IIdentifiable`, `ILifecycle`, and `IServiceHandler` as compatibility composites.

- `engine/pkg/interfaces/INodeContext.go`
  - Update `INodeEndpointManager` method parameters if endpoint manager concrete methods are narrowed.

- `engine/pkg/def/consts.go`
	- Keep the existing lifecycle phase statuses unchanged; do not add failure statuses.

- `engine/pkg/core/service.go`
	- Replace direct `status` and `stopRequested` field usage with `state serviceState`.
	- Change `Stop()` to `Stop() error` and return release/cleanup failures to the caller.
  - Keep Mailbox/RPC/Timer/Event/Concurrent/Profiler fields in `Service`.

- `engine/pkg/services/services.go`
	- Handle `Stop() error` during start rollback and `StopAll()`.

- `engine/pkg/services/services_test.go`
	- Update test service implementations for the `Stop() error` signature if compile errors require it.

- `engine/pkg/core/service_init.go`
  - Use `serviceState` in `Init` and init rollback paths.

- `engine/pkg/core/service_lifecycle_test.go`
	- Update lifecycle tests that currently access `s.status` directly and adapt `Stop()` assertions to `Stop() error`.

- `engine/pkg/core/sysctl_registry.go`
	- Replace direct `s.status` access with `s.GetStatus()` after `Service.status` moves into `serviceState`.

- `engine/pkg/cluster/endpoints/endpoints.go`
  - Narrow endpoint manager method parameters to exported interfaces from `engine/pkg/interfaces`.
  - Keep event payload behavior unchanged unless tests prove otherwise.

- `engine/pkg/cluster/endpoints/endpoints_test.go`
  - Update mocks to satisfy narrowed endpoint interfaces.

- `docs/superpowers/specs/2026-06-12-service-core-decoupling-design.md`
	- Align lifecycle error handling with this plan and add implementation audit note for remaining full `IService` usage.

### Files explicitly not created

- Do not create `engine/pkg/core/service_runtime.go`.
- Do not create `engine/pkg/core/service_dispatch.go`.
- Do not create `engine/pkg/core/service_endpoint.go`.

---

## Task 1: Add narrow service interfaces

**Files:**
- Modify: `engine/pkg/interfaces/IService.go`
- Test: `engine/pkg/core/service_interface_test.go`

- [ ] **Step 1: Write compile-time assertions for target interfaces**

Create `engine/pkg/core/service_interface_test.go` with this content:

```go
package core

import inf "github.com/njtc406/emberengine/engine/pkg/interfaces"

func compileTimeServiceInterfaceAssertions() {
	var _ inf.IService = (*Service)(nil)
	var _ inf.IServiceRef = (*Service)(nil)
	var _ inf.IServiceState = (*Service)(nil)
	var _ inf.IServiceControl = (*Service)(nil)
	var _ inf.IServiceHooks = (*Service)(nil)
	var _ inf.IServiceRuntime = (*Service)(nil)
	var _ inf.IServiceRPC = (*Service)(nil)
	var _ inf.IJobReceiver = (*Service)(nil)
	var _ inf.IMailboxChannel = (*Service)(nil)
	var _ inf.IMessageInvoker = (*Service)(nil)
	var _ inf.IServiceProfiler = (*Service)(nil)
	var _ inf.ILogger = (*Service)(nil)
	var _ inf.IRpcHandler = (*Service)(nil)
	var _ inf.IListener = (*Service)(nil)
}
```

Note: `IJobReceiver` has the same method signature as `IMailboxChannel` (`PostJob(job IMailboxJob) error`). `IJobReceiver` is the new semantic name for "this service can receive jobs"; `IMailboxChannel` is the existing mailbox-oriented name. `*Service` satisfies both. The compile assertion for `IMailboxChannel` is included to guarantee backward compatibility.

- [ ] **Step 2: Run compile test to verify missing interfaces**

Run:

```powershell
go test ./engine/pkg/core -run TestDoesNotExist
```

Expected: FAIL to compile with errors like `undefined: inf.IServiceRef`, `undefined: inf.IServiceState`, or similar missing interface names.

- [ ] **Step 3: Replace the service interface section**

In `engine/pkg/interfaces/IService.go`, replace the current `IService`, `ILifecycle`, `IServiceHandler`, and `IIdentifiable` definitions with this block. Keep the existing imports and keep `IServiceProfiler`, `INamed`, `IServer`, `IActor`, and `ILogger` below this block.

```go
// IService 服务完整能力接口。
//
// 迁移期保留为组合接口。新框架内部代码应优先依赖下面的窄接口，
// 只有服务构造、用户服务约束、Module.GetService 等需要完整服务能力的位置
// 才应使用 IService。
type IService interface {
	IServiceRef
	IServiceState
	IServiceControl
	IServiceHooks
	IServiceRuntime
	IServiceRPC
	IJobReceiver
	IMessageInvoker
	IServiceProfiler
	ILogger
	IRpcHandler
}

// IServiceRef 表示服务身份引用能力，不包含状态读取。
type IServiceRef interface {
	IServer
	INamed
}

// IServiceState 表示服务状态读取能力。
type IServiceState interface {
	IsClosed() bool
	GetStatus() int32
}

// IServiceControl 表示框架内部生命周期控制能力。
type IServiceControl interface {
	Init(src interface{}, serviceInitConf *config.ServiceInitConf, cfg interface{}) error
	Start() error
	// Temporary signature for Task 1 only. Task 3 migrates this to Stop() error
	// together with Service.Stop and all direct callers so no commit is left broken.
	Stop()
}

// IServiceHooks 表示用户服务生命周期 Hook。
type IServiceHooks interface {
	OnInit() error
	OnStart() error
	OnStarted() error
	OnRelease()
}

// IServiceRuntime 表示服务运行时上下文访问能力。
type IServiceRuntime interface {
	GetServiceCfg() interface{}
	GetMailbox() IMailbox
	GetNodeContext() INodeContext
	GetRouter() INodeRouter
}

// IServiceRPC 表示服务 RPC 发布、路由和可见性能力。
type IServiceRPC interface {
	IsPrivate() bool
	IsRemoteCallable() bool
	GetVisibility() def.ServiceVisibility
	IsPrimarySecondaryMode() bool
	GetRpcHandler() IRpcHandler
}

// IJobReceiver 表示服务接收 Job 的能力。
// 与 IMailboxChannel 方法签名相同（PostJob(job IMailboxJob) error），
// 语义上强调"服务作为 Job 接收者"的角色。*Service 同时满足两者。
type IJobReceiver interface {
	PostJob(job IMailboxJob) error
}

// ILifecycle 兼容旧生命周期接口。
//
// Deprecated: 新代码应按场景使用 IServiceControl 或 IServiceHooks。
type ILifecycle interface {
	IServiceControl
	IServiceHooks
}

// IServiceHandler 兼容旧运行时访问接口。
//
// Deprecated: 新代码应按场景使用 IServiceRuntime 或 IServiceRPC。
type IServiceHandler interface {
	IServiceRuntime
	IServiceRPC
}

// IIdentifiable 兼容旧身份和状态组合接口。
//
// Deprecated: 新代码应按场景使用 IServiceRef 或 IServiceState。
type IIdentifiable interface {
	IServiceRef
	IServiceState
}

// IServiceEndpointPublisher 表示 EndpointManager 发布服务需要的能力。
type IServiceEndpointPublisher interface {
	IServiceRef
	IServiceState
	IServiceRPC
	IServiceRuntime
}

// IServiceEndpointLifecycle 表示 EndpointManager 下线/节点服务转换需要的能力。
type IServiceEndpointLifecycle interface {
	IServiceRef
	IServiceRPC
}
```

Rationale: `IServiceRef` must not embed `IIdentifiable`, because `IIdentifiable` includes status methods and would blur the `IServiceRef` / `IServiceState` split. `IServiceRuntime` must not expose `GetLogger()` because `ILogger` already owns logger access. `Stop()` remains temporarily void in Task 1 so the interface split can be committed independently; Task 3 performs the coordinated `Stop() error` migration.

- [ ] **Step 4: Run compile test again**

Run:

```powershell
go test ./engine/pkg/core -run TestDoesNotExist
```

Expected: PASS compile with output similar to `ok github.com/njtc406/emberengine/engine/pkg/core [no tests to run]`.

- [ ] **Step 5: Commit**

Run:

```powershell
git add engine/pkg/interfaces/IService.go engine/pkg/core/service_interface_test.go
git commit -m "refactor: split service capability interfaces"
```

Expected: commit succeeds.

---

## Task 2: Add phase-only `serviceState`

**Files:**
- Modify: `engine/pkg/def/consts.go`
- Create: `engine/pkg/core/service_state.go`
- Test: `engine/pkg/core/service_state_test.go`

- [ ] **Step 1: Write failing state tests**

Create `engine/pkg/core/service_state_test.go` with this content:

```go
package core

import (
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/def"
)

func TestServiceStatusOrdering(t *testing.T) {
	if !(def.SvcStatusUnknown < def.SvcStatusInit && def.SvcStatusInit < def.SvcStatusStarting) {
		t.Fatalf("initial service statuses should keep lifecycle ordering")
	}
	if !(def.SvcStatusStarting < def.SvcStatusRunning && def.SvcStatusRunning < def.SvcStatusReady) {
		t.Fatalf("running service statuses should keep lifecycle ordering")
	}
	if !(def.SvcStatusReady < def.SvcStatusClosing && def.SvcStatusClosing < def.SvcStatusClosed) {
		t.Fatalf("closing service statuses should keep lifecycle ordering")
	}
	if !(def.SvcStatusClosed < def.SvcStatusRetire) {
		t.Fatalf("retired status should remain after closed")
	}
}

func TestServiceStateHappyPathTransitions(t *testing.T) {
	var state serviceState

	if state.Load() != def.SvcStatusUnknown {
		t.Fatalf("initial status = %d, want %d", state.Load(), def.SvcStatusUnknown)
	}
	if !state.TryInit() {
		t.Fatalf("TryInit() should succeed from Unknown")
	}
	if !state.TryStarting() {
		t.Fatalf("TryStarting() should succeed from Init")
	}
	if !state.MarkRunning() {
		t.Fatalf("MarkRunning() should succeed from Starting")
	}
	if !state.MarkReady() {
		t.Fatalf("MarkReady() should succeed from Running")
	}
	if !state.TryClosing() {
		t.Fatalf("TryClosing() should succeed from Ready")
	}
	state.MarkClosed()
	if state.Load() != def.SvcStatusClosed {
		t.Fatalf("status = %d, want Closed", state.Load())
	}
	if !state.IsClosed() {
		t.Fatalf("IsClosed() should be true for Closed")
	}
}

func TestServiceStateRequestStopIsIdempotent(t *testing.T) {
	var state serviceState
	if !state.RequestStop() {
		t.Fatalf("first RequestStop() should return true")
	}
	if state.RequestStop() {
		t.Fatalf("second RequestStop() should return false")
	}
}

func TestServiceStateDoesNotOverwriteClosedWithRunning(t *testing.T) {
	var state serviceState
	state.MarkClosed()
	if state.MarkRunning() {
		t.Fatalf("MarkRunning() should fail after Closed")
	}
	if state.Load() != def.SvcStatusClosed {
		t.Fatalf("status = %d, want Closed", state.Load())
	}
}

func TestServiceStateStoreIfMutableKeepsClosedTerminal(t *testing.T) {
	var state serviceState
	state.MarkClosed()
	if state.StoreIfMutable(def.SvcStatusRunning) {
		t.Fatalf("StoreIfMutable should reject updates after Closed")
	}
	if state.Load() != def.SvcStatusClosed {
		t.Fatalf("status = %d, want Closed", state.Load())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```powershell
go test ./engine/pkg/core -run "TestServiceStatusOrdering|TestServiceState" -count=1
```

Expected: FAIL to compile because `serviceState` is not defined.

- [ ] **Step 3: Confirm lifecycle statuses remain phase-only**

Inspect the service status block in `engine/pkg/def/consts.go` and keep it phase-only:

```go
const (
	SvcStatusUnknown  int32 = iota // 未运行
	SvcStatusInit                  // 初始化
	SvcStatusStarting              // 启动中
	SvcStatusRunning               // 运行中（已进入集群缓存，但不可被普通路由选中）
	SvcStatusReady                 // 已就绪（可被普通路由选中）
	SvcStatusClosing               // 关闭中
	SvcStatusClosed                // 关闭
	SvcStatusRetire                // 退休
)
```

Do not add failure statuses. `Init`, `Start`, and `Stop` failures are returned as `error` and handled by the scheduler/caller.

- [ ] **Step 4: Implement `serviceState` without lifecycle error fields**

Create `engine/pkg/core/service_state.go`:

```go
package core

import (
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/def"
)

type serviceState struct {
	status        int32
	stopRequested atomic.Bool
}

func (s *serviceState) Load() int32 {
	return atomic.LoadInt32(&s.status)
}

func (s *serviceState) IsClosed() bool {
	return s.Load() >= def.SvcStatusClosing
}

func (s *serviceState) TryInit() bool {
	return atomic.CompareAndSwapInt32(&s.status, def.SvcStatusUnknown, def.SvcStatusInit)
}

func (s *serviceState) TryStarting() bool {
	return atomic.CompareAndSwapInt32(&s.status, def.SvcStatusInit, def.SvcStatusStarting)
}

func (s *serviceState) TryClosing() bool {
	for {
		old := s.Load()
		if old >= def.SvcStatusClosing {
			return false
		}
		if atomic.CompareAndSwapInt32(&s.status, old, def.SvcStatusClosing) {
			return true
		}
	}
}

func (s *serviceState) MarkRunning() bool {
	return atomic.CompareAndSwapInt32(&s.status, def.SvcStatusStarting, def.SvcStatusRunning)
}

func (s *serviceState) MarkReady() bool {
	return atomic.CompareAndSwapInt32(&s.status, def.SvcStatusRunning, def.SvcStatusReady)
}

func (s *serviceState) StoreIfMutable(status int32) bool {
	for {
		old := s.Load()
		if old == status || old >= def.SvcStatusClosed {
			return false
		}
		if atomic.CompareAndSwapInt32(&s.status, old, status) {
			return true
		}
	}
}

func (s *serviceState) MarkClosed() {
	atomic.StoreInt32(&s.status, def.SvcStatusClosed)
}

func (s *serviceState) MarkRetire() bool {
	return atomic.CompareAndSwapInt32(&s.status, def.SvcStatusClosed, def.SvcStatusRetire)
}

func (s *serviceState) RequestStop() bool {
	return s.stopRequested.CompareAndSwap(false, true)
}
```

- [ ] **Step 5: Run state tests**

Run:

```powershell
go test ./engine/pkg/core -run "TestServiceStatusOrdering|TestServiceState" -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

Run:

```powershell
git add engine/pkg/def/consts.go engine/pkg/core/service_state.go engine/pkg/core/service_state_test.go
git commit -m "feat: add service lifecycle state machine"
```

Expected: commit succeeds.

---

## Task 3: Wire `serviceState` into `Service`

**Files:**
- Modify: `engine/pkg/interfaces/IService.go`
- Modify: `engine/pkg/core/service.go`
- Modify: `engine/pkg/core/service_init.go`
- Modify: `engine/pkg/core/service_lifecycle_test.go`
- Modify: `engine/pkg/core/sysctl_registry.go`
- Modify: `engine/pkg/services/services.go`
- Modify: `engine/pkg/services/services_test.go` if compile errors require test doubles to match `Stop() error`.

- [ ] **Step 1: Update lifecycle tests to use `state` helpers**

In `engine/pkg/core/service_lifecycle_test.go`, replace direct atomic status setup with `s.state` helpers and update `Stop()` calls for the new `error` return. The file should contain these tests after migration:

```go
func TestServiceStop_Idempotent(t *testing.T) {
	s := &Service{}
	s.state.TryInit()
	s.state.TryStarting()
	s.state.MarkRunning()

	if err := s.Stop(); err != nil {
		t.Fatalf("first Stop() error = %v", err)
	}
	if s.GetStatus() != def.SvcStatusClosed {
		t.Errorf("status after first Stop = %d, want %d", s.GetStatus(), def.SvcStatusClosed)
	}

	if err := s.Stop(); err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}
	if s.GetStatus() != def.SvcStatusClosed {
		t.Errorf("status after second Stop = %d, want %d", s.GetStatus(), def.SvcStatusClosed)
	}
}

func TestServiceStop_FromUnknownStatus(t *testing.T) {
	s := &Service{}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if s.GetStatus() != def.SvcStatusClosed {
		t.Errorf("status after Stop from Unknown = %d, want %d", s.GetStatus(), def.SvcStatusClosed)
	}
}

func TestServiceStart_FromUnknownStatus(t *testing.T) {
	s := &Service{}

	err := s.Start()
	if err == nil {
		t.Fatal("Start should fail when service has not been initialized")
	}
	if s.GetStatus() != def.SvcStatusUnknown {
		t.Errorf("status should remain Unknown when Start is called before Init, got %d", s.GetStatus())
	}
}
```

Preserve the existing concurrent stop, running start, closed start, and `setStatus` tests, but update their setup to use `s.state.TryInit()`, `s.state.TryStarting()`, `s.state.MarkRunning()`, or `s.state.MarkClosed()`. All `Stop()` calls in tests must assert or explicitly ignore the returned error.

- [ ] **Step 2: Run lifecycle tests to verify they fail against current Service**

Run:

```powershell
go test ./engine/pkg/core -run "TestServiceStop|TestServiceStart|TestSetStatus" -count=1
```

Expected: FAIL to compile or fail tests because `Service` still has `status` and `stopRequested` fields instead of `state` wiring, and `Stop()` still has the old signature.

- [ ] **Step 3: Replace state fields in `Service`**

In `engine/pkg/core/service.go`, remove the following fields:

```go
status        int32
stopRequested atomic.Bool
initErr       error
```

and replace them with:

```go
state serviceState
```

Then remove the `sync/atomic` import from `service.go` if no longer used there.

Rationale: `initErr` was used to block `Start()` after a failed `Init()`. Since `Init()` now returns errors directly and the scheduler/caller owns the retry-or-rebuild decision, `initErr` storage is no longer needed. The `Start()` method's `initErr` guard (see Step 4) is removed accordingly.

- [ ] **Step 4: Update `Start` and `rollbackStart`**

In `engine/pkg/core/service.go`:

**Remove the `initErr` guard.** Delete these lines from `Start()`:

```go
if s.initErr != nil {
	return fmt.Errorf("service[%s] init failed: %w", s.GetName(), s.initErr)
}
```

Since `Init()` now returns errors directly and the scheduler owns the retry-or-rebuild decision, `Start()` no longer needs to check a stored `initErr`.

**Replace starting CAS logic** with:

```go
if !s.state.TryStarting() {
	return fmt.Errorf("service[%s] status[%d] cannot start", s.GetName(), s.GetStatus())
}
```

Do not persist init/start failure errors in `Service`. `Init()` and `Start()` return their errors directly to the scheduler/caller.

Replace `s.setStatus(def.SvcStatusRunning)` with:

```go
s.state.MarkRunning()
```

Replace `s.setStatus(def.SvcStatusReady)` with:

```go
s.state.MarkReady()
```

In each `Start` failure branch, call `s.rollbackStart(...)`, mark the service `Closed`, and return the exact error. This keeps status phase-only while making the failed instance non-reusable unless the scheduler creates a new instance.

There are three `Start` failure branches that need `s.state.MarkClosed()` after `rollbackStart`:

1. **`OnStart` failure** (line ~139):
```go
if err := s.src.OnStart(); err != nil {
	s.rollbackStart(nil, false)
	s.state.MarkClosed()
	return err
}
```

2. **Endpoint manager nil** (line ~155):
```go
em := s.GetEndpointManager()
if em == nil {
	err := fmt.Errorf("service[%s] endpoint manager is nil", s.GetName())
	s.rollbackStart(nil, false)
	s.state.MarkClosed()
	return err
}
```

3. **`OnStarted` failure** (line ~161):
```go
if err := s.src.OnStarted(); err != nil {
	s.rollbackStart(em, true)
	s.state.MarkClosed()
	return err
}
```

In `rollbackStart`, remove the `atomic.StoreInt32(&s.status, def.SvcStatusClosed)` line — do **not** replace it with `s.state.MarkClosed()`. The caller owns the final phase transition.

- [ ] **Step 5: Update `Stop`, lifecycle interfaces, `IsClosed`, `GetStatus`, `setStatus`, and `isRunning`**

In `engine/pkg/interfaces/IService.go`, update `IServiceControl` from the temporary Task 1 signature to:

```go
type IServiceControl interface {
	Init(src interface{}, serviceInitConf *config.ServiceInitConf, cfg interface{}) error
	Start() error
	Stop() error
}
```

Because `ILifecycle` embeds `IServiceControl`, this also updates the lifecycle compatibility interface.

In `engine/pkg/core/service.go`, change the signature to:

```go
func (s *Service) Stop() error
```

Update the idempotency/closing guard to use:

```go
if !s.state.RequestStop() {
	return nil
}
if !s.state.TryClosing() && s.state.Load() >= def.SvcStatusClosing {
	return nil
}
```

Convert release/cleanup failures, if any are exposed during this migration, into the returned `error`. In the current implementation, `release()` → `releaseWithEndpoint()` uses `recover()` to catch panics from `OnRelease()` and does not return errors; `mailbox.Suspend()`, `ITimerScheduler.Stop()`, and `IConcurrent.Close()` are best-effort and do not return errors. Therefore `Stop()` will return `nil` on success in this migration. The `error` return is added now so that future cleanup steps that may fail (e.g., flushing buffers, closing external connections) can propagate errors without another signature change. At the end of successful cleanup, replace direct status store with:

```go
s.state.MarkClosed()
return nil
```

If cleanup returns an error after the status has reached `Closing`, return that error to the caller. Do not introduce `SvcStatusStopFailed`; the caller decides whether to retry or force close.

Update these methods:

```go
func (s *Service) IsClosed() bool { return s.state.IsClosed() }

func (s *Service) GetStatus() int32 { return s.state.Load() }

func (s *Service) setStatus(status int32) {
	s.state.StoreIfMutable(status)
}

func (s *Service) isRunning() bool {
	status := s.state.Load()
	return status == def.SvcStatusRunning || status == def.SvcStatusReady
}
```

- [ ] **Step 6: Update `Init` and rollback init**

In `engine/pkg/core/service_init.go`, remove the `sync/atomic` import and any `s.initErr` reset logic. `Init()` returns initialization errors directly and no longer stores them on `Service`.

Replace the defer body:

```go
s.rollbackInitResources()
```

with:

```go
// 初始化失败时只回滚资源并返回 error；不写入 Failed 状态。
s.rollbackInitResources()
s.state.MarkClosed()
```

Replace:

```go
if !atomic.CompareAndSwapInt32(&s.status, def.SvcStatusUnknown, def.SvcStatusInit) {
	return nil
}
```

with:

```go
if !s.state.TryInit() {
	return nil
}
```

The chosen policy is: after `Init()` or `Start()` failure and rollback, the same `Service` instance transitions to `Closed`; the scheduler/caller may decide to build a fresh instance and retry.

- [ ] **Step 7: Update sysctl status reads**

In `engine/pkg/core/sysctl_registry.go`, replace direct access such as:

```go
status := atomic.LoadInt32(&s.status)
```

with:

```go
status := s.GetStatus()
```

Remove the `sync/atomic` import from that file if it becomes unused.

- [ ] **Step 8: Update service manager Stop callers**

In `engine/pkg/services/services.go`, handle `Stop() error` during start rollback and full shutdown.

During `Start()` rollback, replace:

```go
started[i].Stop()
```

with:

```go
if stopErr := started[i].Stop(); stopErr != nil {
	sm.WithField("service", started[i].GetName()).Errorf("Rollback Stop Service failed, err: %v", stopErr)
}
```

During `StopAll()`, replace:

```go
sm.runServices[i].Stop()
```

with:

```go
if err := sm.runServices[i].Stop(); err != nil {
	sm.WithField("service", sm.runServices[i].GetName()).Errorf("Stop Service failed, err: %v", err)
}
```

If `engine/pkg/services/services_test.go` contains test services that implement `Stop()`, update them to `Stop() error { return nil }`.

- [ ] **Step 9: Run lifecycle and state tests**

Run:

```powershell
go test ./engine/pkg/core -run "TestServiceStatusOrdering|TestServiceState|TestServiceStop|TestServiceStart|TestSetStatus" -count=1
```

Expected: PASS.

- [ ] **Step 10: Run full core tests**

Run:

```powershell
go test ./engine/pkg/core -count=1
```

Expected: PASS.

- [ ] **Step 11: Commit**

Run:

```powershell
git add engine/pkg/interfaces/IService.go engine/pkg/core/service.go engine/pkg/core/service_init.go engine/pkg/core/service_lifecycle_test.go engine/pkg/core/sysctl_registry.go engine/pkg/services/services.go engine/pkg/services/services_test.go
git commit -m "refactor: wire service state into service lifecycle"
```

Expected: commit succeeds.

---

## Task 4: Narrow endpoint publication dependency

**Files:**
- Modify: `engine/pkg/interfaces/INodeContext.go`
- Modify: `engine/pkg/cluster/endpoints/endpoints.go`
- Modify: `engine/pkg/cluster/endpoints/endpoints_test.go`

- [ ] **Step 1: Inspect endpoint signatures and interface contract**

Run:

```powershell
Select-String -Path engine/pkg/interfaces/INodeContext.go,engine/pkg/cluster/endpoints/endpoints.go -Pattern "INodeEndpointManager|AddService|ServiceReady|RemoveService|ToNodeService" -Context 2,2
```

Expected: output shows `INodeEndpointManager` and concrete endpoint methods currently accept full `IService`.

- [ ] **Step 2: Update `INodeEndpointManager` to exported narrow interfaces**

In `engine/pkg/interfaces/INodeContext.go`, change the endpoint manager interface to:

```go
type INodeEndpointManager interface {
	CreatePid(partition int32, serviceId, serviceType, serviceName string, version int64, rpcType string) *actor.PID
	AddService(svc IServiceEndpointPublisher)
	ServiceReady(svc IServiceEndpointPublisher)
	RemoveService(svc IServiceEndpointLifecycle)
	ToNodeService(svc IServiceEndpointLifecycle)
}
```

Rationale: the concrete `EndpointManager` must still satisfy `INodeEndpointManager`. `RemoveService` and `ToNodeService` need `GetVisibility()` / `IsPrimarySecondaryMode()`, so `IServiceRef` alone is insufficient.

- [ ] **Step 3: Narrow concrete endpoint method parameters**

In `engine/pkg/cluster/endpoints/endpoints.go`, change method signatures to:

```go
func (em *EndpointManager) AddService(svc inf.IServiceEndpointPublisher) {
```

```go
func (em *EndpointManager) ServiceReady(svc inf.IServiceEndpointPublisher) {
```

```go
func (em *EndpointManager) RemoveService(svc inf.IServiceEndpointLifecycle) {
```

```go
func (em *EndpointManager) ToNodeService(svc inf.IServiceEndpointLifecycle) {
```

Do not change method bodies in this step. `AddService` keeps access to `GetMailbox()` through `IServiceEndpointPublisher` embedding `IServiceRuntime`.

- [ ] **Step 4: Run endpoint tests to reveal mock gaps**

Run:

```powershell
go test ./engine/pkg/cluster/endpoints -count=1
```

Expected: PASS or compile errors in `endpointTestService` indicating missing methods required by the narrowed exported interfaces.

- [ ] **Step 5: Fix endpoint mock methods if needed**

If `endpointTestService` in `engine/pkg/cluster/endpoints/endpoints_test.go` lacks methods required by `IServiceEndpointPublisher`, add these concrete methods using existing fields:

```go
func (s *endpointTestService) IsPrivate() bool {
	return s.visibility == def.ServiceVisibilityNode
}

func (s *endpointTestService) IsRemoteCallable() bool {
	return s.visibility == def.ServiceVisibilityCluster
}

func (s *endpointTestService) GetVisibility() def.ServiceVisibility {
	return s.visibility
}

func (s *endpointTestService) IsPrimarySecondaryMode() bool {
	return s.isPrimarySecondaryMode
}

func (s *endpointTestService) GetRpcHandler() inf.IRpcHandler {
	return nil
}

func (s *endpointTestService) GetServiceCfg() interface{} {
	return nil
}

func (s *endpointTestService) GetNodeContext() inf.INodeContext {
	return nil
}

func (s *endpointTestService) GetRouter() inf.INodeRouter {
	return nil
}
```

Do not add `PostJob` unless the compiler asks for `IJobReceiver`; endpoint publication should not require it.

- [ ] **Step 6: Run endpoint and core tests**

Run:

```powershell
go test ./engine/pkg/cluster/endpoints ./engine/pkg/core -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

Run:

```powershell
git add engine/pkg/interfaces/INodeContext.go engine/pkg/cluster/endpoints/endpoints.go engine/pkg/cluster/endpoints/endpoints_test.go
git commit -m "refactor: narrow endpoint service dependency"
```

Expected: commit succeeds.

---

## Task 5: Audit remaining `IService` usage and document decisions

**Files:**
- Modify: `docs/superpowers/specs/2026-06-12-service-core-decoupling-design.md`
- Optional Modify: Go files only if a remaining `inf.IService` parameter can be trivially narrowed without behavior change and without expanding this plan's scope.

- [ ] **Step 1: Search remaining full-service dependencies**

Run:

```powershell
Select-String -Path engine/pkg/**/*.go -Pattern "inf\.IService|interfaces\.IService" | Select-Object -First 120
```

Expected: output lists remaining direct uses of package-qualified `IService`.

- [ ] **Step 2: Search unqualified `IService` declarations and parameters**

Run:

```powershell
Select-String -Path engine/pkg/**/*.go -Pattern "\bIService\b" | Select-Object -First 160
```

Expected: output includes interface definitions and any unqualified use sites.

- [ ] **Step 3: Classify each remaining usage**

Use this classification rule while reviewing the command output:

```text
Keep full IService:
- Service.src: user service implementation must provide complete service behavior during migration.
- Module.GetService(): migration-period compatibility for user modules.
- User-facing service construction constraints that intentionally require complete service capability.

Candidate for later narrowing:
- Any manager/router/event parameter that only calls GetPid/GetName/GetStatus/PostJob.
- Any health/readiness path that only calls IServiceRef + IServiceState.

Already narrowed in this plan:
- Endpoint publication path via IServiceEndpointPublisher / IServiceEndpointLifecycle.
```

- [ ] **Step 4: Align design spec and add audit note**

In `docs/superpowers/specs/2026-06-12-service-core-decoupling-design.md`:

**First**, remove or rewrite all design text that contradicts this plan. Specifically:

- Section 2.1: Remove "新增失败状态，改善 Init / Start / Stop 失败后的诊断能力" from goals.
- Section 5.2: Remove `initErr error`, `startErr error`, `stopErr error` from `serviceState` internal fields.
- Section 6.1: Remove `SvcStatusInitFailed`, `SvcStatusStartFailed`, `SvcStatusStopFailed`.
- Section 6.2: Remove all Failed state transitions from the state migration diagram.
- Section 6.3: Remove `MarkInitFailed`, `MarkStartFailed`, `MarkStopFailed`, `InitErr`, `StartErr`, `StopErr` methods.
- Section 9.1 rollback: Remove `state.MarkInitFailed(err)` and `state.initErr = err`.
- Section 9.2 rollback: Remove `state.MarkStartFailed(err)`.
- Section 9.3: Remove `state.MarkStopFailed(err)` branch.
- Section 11.1: Remove the entire "失败状态" subsection.
- Section 11.2: Remove the entire "错误记录" subsection.
- Section 12.2: Remove test cases for InitFailed/StartFailed/StopFailed.
- Section 13 阶段 3: Remove "补齐失败状态" phase entirely.
- Section 16: Remove "Init / Start / Stop 失败能记录 InitFailed / StartFailed / StopFailed" from success criteria.

Replace with: lifecycle operation failures are returned as `error` from `Init`, `Start`, and `Stop`; the scheduler/caller owns failure policy decisions; no failure status is stored in `serviceState`.

Then, under section `8.1 允许继续依赖完整 IService 的位置`, add this paragraph:

```markdown
实施阶段必须审计所有剩余 `IService` 使用点，并按以下规则处理：

- 如果调用方需要用户服务完整行为、`Module.GetService()` 兼容能力或迁移期完整约束，可以保留 `IService`；
- 如果调用方只读取身份、状态、可见性、运行时上下文或 Job 投递能力，必须改为对应窄接口；
- 无法立即收窄的位置必须在代码注释或后续任务中说明原因；
- Endpoint 发布路径已通过 `IServiceEndpointPublisher` / `IServiceEndpointLifecycle` 收窄，且 `INodeEndpointManager` 必须与 concrete `EndpointManager` 保持签名一致。
```

- [ ] **Step 5: Run docs check and focused tests**

Run:

```powershell
git diff --check -- docs/superpowers/specs/2026-06-12-service-core-decoupling-design.md; go test ./engine/pkg/core ./engine/pkg/cluster/endpoints -count=1
```

Expected: no `git diff --check` output and tests PASS.

- [ ] **Step 6: Commit**

Run:

```powershell
git add docs/superpowers/specs/2026-06-12-service-core-decoupling-design.md
git commit -m "docs: document service interface narrowing audit"
```

Expected: commit succeeds.

---

## Task 6: Final verification

**Files:**
- No source files expected.

- [ ] **Step 1: Format touched Go files**

Run:

```powershell
gofmt -w engine/pkg/interfaces/IService.go engine/pkg/interfaces/INodeContext.go engine/pkg/def/consts.go engine/pkg/core/service_state.go engine/pkg/core/service_state_test.go engine/pkg/core/service_interface_test.go engine/pkg/core/service.go engine/pkg/core/service_init.go engine/pkg/core/service_lifecycle_test.go engine/pkg/core/sysctl_registry.go engine/pkg/services/services.go engine/pkg/services/services_test.go engine/pkg/cluster/endpoints/endpoints.go engine/pkg/cluster/endpoints/endpoints_test.go
```

Expected: command exits 0.

- [ ] **Step 2: Run focused tests**

Run:

```powershell
go test ./engine/pkg/core ./engine/pkg/cluster/endpoints -count=1
```

Expected: PASS.

- [ ] **Step 3: Run race tests for touched packages**

Run:

```powershell
go test -race ./engine/pkg/core ./engine/pkg/cluster/endpoints -count=1
```

Expected: PASS. `serviceState` only uses atomic status and stop guards, so the state machine should remain race-safe.

- [ ] **Step 4: Run broader engine tests**

Run:

```powershell
go test ./engine/pkg/... -count=1
```

Expected: PASS. If unrelated packages fail, capture the failing package and error output before deciding whether it is in scope.

- [ ] **Step 5: Check formatting and changed files**

Run:

```powershell
git diff --check; git status --short
```

Expected: no `git diff --check` output. `git status --short` only shows intended files or is clean after commits.

- [ ] **Step 6: Final commit if verification changed formatting**

If `gofmt` changed files after previous commits, run:

```powershell
git add engine/pkg/interfaces/IService.go engine/pkg/interfaces/INodeContext.go engine/pkg/def/consts.go engine/pkg/core/service_state.go engine/pkg/core/service_state_test.go engine/pkg/core/service_interface_test.go engine/pkg/core/service.go engine/pkg/core/service_init.go engine/pkg/core/service_lifecycle_test.go engine/pkg/core/sysctl_registry.go engine/pkg/services/services.go engine/pkg/services/services_test.go engine/pkg/cluster/endpoints/endpoints.go engine/pkg/cluster/endpoints/endpoints_test.go docs/superpowers/specs/2026-06-12-service-core-decoupling-design.md
git commit -m "chore: finalize service interface tightening"
```

Expected: commit succeeds, or Git reports nothing to commit.

---

## Self-Review

### Spec coverage

- Service remains base capability container and runtime capability composer: covered by file structure and explicit non-created files.
- Interface tightening: covered by Task 1 and Task 4.
- `serviceState`: covered by Task 2 and Task 3.
- Lifecycle operation failures: covered by Task 2 and Task 3. They are returned as `error` from `Init`, `Start`, and `Stop`; no failure status is introduced.
- Endpoint narrowing: covered by Task 4, including `INodeEndpointManager` signature consistency and `GetMailbox()` through `IServiceEndpointPublisher`.
- Remaining full `IService` audit: covered by Task 5.
- Tests: covered by unit, lifecycle, endpoint, package, and race test steps.

### Placeholder scan

No unresolved placeholder markers or deliberately broken committed state remain in this plan. The phrase `Candidate for later narrowing` is a classification label used during audit, not an implementation placeholder.

### Type consistency

- `IServiceRef` contains identity only and no status methods.
- `IServiceState` owns `IsClosed()` and `GetStatus()`.
- `IIdentifiable` remains as a compatibility composite of `IServiceRef + IServiceState`.
- `IServiceRuntime` does not duplicate `ILogger.GetLogger()`.
- `IServiceEndpointPublisher` includes `IServiceRuntime` so `EndpointManager.AddService` can keep using `GetMailbox()`.
- `IServiceEndpointLifecycle` includes `IServiceRPC` so `RemoveService` and `ToNodeService` can keep using visibility and primary-secondary information.
- `INodeEndpointManager` and concrete `EndpointManager` method signatures are updated together.
- `serviceState` does not contain lifecycle error fields; operation errors are propagated by return values.

---

## Execution handoff

Plan complete and saved to `docs/superpowers/plans/2026-06-15-service-core-interface-tightening.md`. Two execution options:

**1. Subagent-Driven (recommended)** - dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** - execute tasks in this session using executing-plans, batch execution with checkpoints.

Which approach?
