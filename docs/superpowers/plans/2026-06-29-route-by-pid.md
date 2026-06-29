# RouteByPid Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `RouteByPid` API that lets business code route to a service when it already has a business-provided, route-capable `actor.PID`, without making private services discoverable by normal selector queries.

**Architecture:** Keep search-style selectors (`Select`, `SelectByRule`, `SelectByServiceType`, `SelectByFilterAndChoice`) unchanged and repository-index based. Add route-style forwarding through `Router -> EndpointManager -> Repository bus factory`, where `EndpointManager.RouteByPid` uses `GetDispatcher(receiver)` so a receiver that is not in service discovery can still get a temporary dispatcher when its PID contains enough routing information. The framework does not complete partial receiver PID fields; business code owns the route PID contract.

**Tech Stack:** Go, EmberEngine `actor`, `interfaces`, `router`, `cluster/endpoints`, `cluster/endpoints/repository`, `core/rpc`, existing `go test` workflow.

---

## Important Implementation Constraints

- Do not change the semantics of `SelectByPid`: it remains repository lookup by `receiver.ServiceUid`.
- Do not make private/node-visible services searchable through `SelectByServiceType`, `SelectByRule`, or `SelectByFilterAndChoice`.
- `RouteByPid` is for a business-provided route PID. The receiver may be incomplete, but it must contain the fields required by the selected RPC protocol and remote business dispatch path.
- Do not make the framework infer missing service identity from `Name` or `Address` in this change.
- Preserve `MessageBusFactory` behavior by routing bus creation through repository-owned bus construction, not direct `msgbus.NewMessageBus` in `EndpointManager`.
- Before editing Go symbols during execution, follow repository guidance in `AGENTS.md` and run GitNexus impact analysis for the target symbol where tooling is available. At minimum assess callers of `ISelector`, `INodeRouter`, `IRpcSelector`, `Router`, and `EndpointManager`.
- Do not commit changes unless the user explicitly asks for commits.

---

## File Structure

### Modified files

- `engine/pkg/interfaces/ISelector.go`
  - Owns the public selector/router interface. `RouteByPid` already exists; update its comment to document the route PID contract.

- `engine/pkg/interfaces/INodeContext.go`
  - Adds `RouteByPid` to the narrow `INodeRouter` interface used by `core/rpc` and services.

- `engine/pkg/interfaces/IRpc.go`
  - Adds `RouteByPid(receiver *actor.PID) IBus` to `IRpcSelector` for business-facing RPC handler usage.

- `engine/pkg/cluster/endpoints/repository/selector.go`
  - Adds an exported thin bus constructor so `EndpointManager` can preserve the configured `MessageBusFactory`.

- `engine/pkg/cluster/endpoints/endpoints.go`
  - Implements the core route behavior: find sender dispatcher from repository, get receiver dispatcher via `GetDispatcher(receiver)`, return an `IBus`.

- `engine/pkg/router/selector.go`
  - Adds `Router.RouteByPid` and delegates to `EndpointManager.RouteByPid`.

- `engine/pkg/core/rpc/selector.go`
  - Adds `Handler.RouteByPid(receiver)` so business services can use the new route API.

- `engine/pkg/router/selector_test.go`
  - Adds nil endpoint-manager coverage for router-level `RouteByPid`.

- `engine/pkg/cluster/endpoints/endpoints_test.go`
  - Adds route behavior coverage for receiver PIDs that are not present in repository discovery indexes.

- `engine/pkg/sysService/healthservice/healthservice_test.go`
  - Updates `mockRouter` to satisfy the extended `INodeRouter` interface.

### Files explicitly not modified

- Do not modify `engine/pkg/cluster/endpoints/repository/selector.go` search methods (`Select`, `SelectByRule`, `SelectByServiceType`, `SelectByFilterAndChoice`) except for adding the thin bus constructor.
- Do not modify discovery registration logic to publish private services.
- Do not add a new `RouteTarget` type in this change; business PID construction remains outside framework scope.

---

## Task 1: Document and Extend Interfaces

**Files:**
- Modify: `engine/pkg/interfaces/ISelector.go`
- Modify: `engine/pkg/interfaces/INodeContext.go`
- Modify: `engine/pkg/interfaces/IRpc.go`
- Test: compile checks through existing packages

- [ ] **Step 1: Update `ISelector.RouteByPid` comment**

In `engine/pkg/interfaces/ISelector.go`, replace the current `RouteByPid` comment with this block:

```go
	// RouteByPid 根据业务提供的可路由 PID 构造消息路由。
	//
	// receiver 可以不来自服务发现，也可以不是完整的服务发现 PID；
	// 但业务必须保证它包含当前 RPC 协议和远端分发所需的最小字段。
	// 框架不会根据 name/address 推断或补全缺失的服务身份。
	RouteByPid(sender, receiver *actor.PID) IBus
```

- [ ] **Step 2: Add the method to `INodeRouter`**

In `engine/pkg/interfaces/INodeContext.go`, update `INodeRouter` to include `RouteByPid` after `SelectByPid`:

```go
type INodeRouter interface {
	Select(sender *actor.PID, options ...SelectParamBuilder) IBus
	SelectByPid(sender, receiver *actor.PID) IBus
	RouteByPid(sender, receiver *actor.PID) IBus
	SelectByRule(sender *actor.PID, rule func(pid *actor.PID) bool) IBus
	SelectByServiceUid(sender *actor.PID, receiverServiceUid string) IBus
}
```

- [ ] **Step 3: Add the business-facing RPC selector method**

In `engine/pkg/interfaces/IRpc.go`, update `IRpcSelector` to include `RouteByPid` after `SelectByPid`:

```go
type IRpcSelector interface {
	// 选择相同Partition的服务,如果需要选择其他Partition的服务,使用下面的SelectByOpt
	Select(options ...SelectParamBuilder) IBus

	SelectByOpt(options ...SelectParamBuilder) IBus

	SelectByPid(receiver *actor.PID) IBus

	RouteByPid(receiver *actor.PID) IBus

	SelectByServiceUid(receiverServiceUid string) IBus

	// SelectByRule 根据自定义规则选择服务
	SelectByRule(rule func(pid *actor.PID) bool) IBus

	// SelectSlavers 选择从服务
	SelectSlavers(options ...SelectParamBuilder) IBus

	//SelectWithFilter(filter func(pid *actor.PID) bool, options ...SelectParamBuilder) IBus // 这个目前有问题，请勿使用
}
```

- [ ] **Step 4: Run compile to observe expected missing implementations**

Run:

```powershell
go test ./engine/pkg/router ./engine/pkg/core/rpc ./engine/pkg/sysService/healthservice
```

Expected: FAIL to compile with missing `RouteByPid` implementations on `*router.Router`, `*rpc.Handler`, and test mocks that implement `INodeRouter`.

---

## Task 2: Preserve MessageBusFactory Through Repository Bus Construction

**Files:**
- Modify: `engine/pkg/cluster/endpoints/repository/selector.go`
- Test: `engine/pkg/cluster/endpoints/endpoints_test.go`

- [ ] **Step 1: Add an exported repository bus constructor**

In `engine/pkg/cluster/endpoints/repository/selector.go`, add this method immediately after `newMessageBus`:

```go
func (r *Repository) NewBus(sender inf.IRpcDispatcher, receiver inf.IRpcDispatcher, err error) inf.IBus {
	return r.newMessageBus(sender, receiver, err)
}
```

- [ ] **Step 2: Run repository tests**

Run:

```powershell
go test ./engine/pkg/cluster/endpoints/repository
```

Expected: PASS. This task only exports existing behavior and should not affect selector semantics.

---

## Task 3: Implement Core Routing in EndpointManager

**Files:**
- Modify: `engine/pkg/cluster/endpoints/endpoints.go`
- Test: `engine/pkg/cluster/endpoints/endpoints_test.go`

- [ ] **Step 1: Write failing EndpointManager route test**

Add this test to `engine/pkg/cluster/endpoints/endpoints_test.go`. If helper constructors already exist in this file, reuse them and keep the same package style.

```go
func TestEndpointManagerRouteByPid_UsesTemporaryDispatcherForUnknownRemoteReceiver(t *testing.T) {
	em := NewEndpointManager()
	em.nodeUid = "local-node"
	em.repository = repository.NewRepository(nil)

	sender := actor.NewPID("127.0.0.1:6670", "local-node", 1, "sender", "system", "SenderService", 1, def.RpcTypeGrpc)
	receiver := actor.NewPID("127.0.0.1:6671", "remote-node", 1, "scene-1", "scene", "SceneService", 1, def.RpcTypeGrpc)

	em.repository.AddWithMeta("", client.NewDispatcher(nil, sender, nil), def.SvcStatusReady, def.ServiceVisibilityCluster)

	if got := em.repository.SelectByServiceUid(receiver.GetServiceUid()); got != nil {
		t.Fatalf("receiver should not be preloaded in repository, got %T", got)
	}

	bus := em.RouteByPid(sender, receiver)
	if bus == nil {
		t.Fatalf("expected non-nil bus")
	}

	if got := em.repository.SelectByServiceUid(receiver.GetServiceUid()); got == nil {
		t.Fatalf("expected receiver to be cached as temporary dispatcher")
	}
}
```

Ensure the file imports these packages if they are not already imported:

```go
import (
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/cluster/endpoints/repository"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/rpc/client"
)
```

- [ ] **Step 2: Run the new test to verify it fails**

Run:

```powershell
go test ./engine/pkg/cluster/endpoints -run TestEndpointManagerRouteByPid_UsesTemporaryDispatcherForUnknownRemoteReceiver -count=1
```

Expected: FAIL to compile with `em.RouteByPid undefined`.

- [ ] **Step 3: Implement `EndpointManager.RouteByPid`**

In `engine/pkg/cluster/endpoints/endpoints.go`, add this method near `GetDispatcher`:

```go
func (em *EndpointManager) RouteByPid(sender, receiver *actor.PID) inf.IBus {
	if em == nil || em.repository == nil {
		return nil
	}

	var senderDispatcher inf.IRpcDispatcher
	if sender != nil {
		senderDispatcher = em.repository.SelectByServiceUid(sender.GetServiceUid())
	}

	if receiver == nil {
		return em.repository.NewBus(senderDispatcher, nil, def.ErrServiceNotFound)
	}

	receiverDispatcher := em.GetDispatcher(receiver)
	if receiverDispatcher == nil || actor.IsRetired(receiverDispatcher.GetPid()) {
		return em.repository.NewBus(senderDispatcher, receiverDispatcher, def.ErrServiceNotFound)
	}

	return em.repository.NewBus(senderDispatcher, receiverDispatcher, nil)
}
```

- [ ] **Step 4: Run the EndpointManager route test**

Run:

```powershell
go test ./engine/pkg/cluster/endpoints -run TestEndpointManagerRouteByPid_UsesTemporaryDispatcherForUnknownRemoteReceiver -count=1
```

Expected: PASS.

- [ ] **Step 5: Run all endpoint tests**

Run:

```powershell
go test ./engine/pkg/cluster/endpoints ./engine/pkg/cluster/endpoints/repository
```

Expected: PASS.

---

## Task 4: Add Router Delegation

**Files:**
- Modify: `engine/pkg/router/selector.go`
- Modify: `engine/pkg/router/selector_test.go`
- Test: `engine/pkg/router/selector_test.go`

- [ ] **Step 1: Write failing router nil test**

Add this test to `engine/pkg/router/selector_test.go`:

```go
func TestRouter_NilEndpoints_RouteByPidReturnsNil(t *testing.T) {
	r := NewRouter(nil)
	assert.Nil(t, r.RouteByPid(testPID("S"), testPID("R")))
}
```

- [ ] **Step 2: Run router test to verify it fails**

Run:

```powershell
go test ./engine/pkg/router -run TestRouter_NilEndpoints_RouteByPidReturnsNil -count=1
```

Expected: FAIL to compile with `r.RouteByPid undefined`.

- [ ] **Step 3: Implement Router delegation**

In `engine/pkg/router/selector.go`, add this method after `SelectByPid`:

```go
func (r *Router) RouteByPid(sender, receiver *actor.PID) inf.IBus {
	em := r.endpointManager()
	if em == nil {
		return nil
	}
	return em.RouteByPid(sender, receiver)
}
```

- [ ] **Step 4: Run router tests**

Run:

```powershell
go test ./engine/pkg/router
```

Expected: PASS.

---

## Task 5: Add Business-Facing RPC Handler API

**Files:**
- Modify: `engine/pkg/core/rpc/selector.go`
- Test: package compile through `engine/pkg/core/rpc`

- [ ] **Step 1: Run compile to verify handler is missing `RouteByPid`**

Run:

```powershell
go test ./engine/pkg/core/rpc -run TestDoesNotExist
```

Expected: FAIL if `*Handler` is required to satisfy `IRpcSelector` during compilation, or PASS if no compile-time assertion exists yet. Continue to Step 2 either way.

- [ ] **Step 2: Implement `Handler.RouteByPid`**

In `engine/pkg/core/rpc/selector.go`, add this method after `SelectByPid`:

```go
func (h *Handler) RouteByPid(receiver *actor.PID) inf.IBus {
	rt := h.getRouter()
	if rt == nil {
		return nil
	}
	return rt.RouteByPid(h.GetPid(), receiver)
}
```

- [ ] **Step 3: Run RPC package compile**

Run:

```powershell
go test ./engine/pkg/core/rpc
```

Expected: PASS.

---

## Task 6: Update Test Mocks for Extended `INodeRouter`

**Files:**
- Modify: `engine/pkg/sysService/healthservice/healthservice_test.go`
- Test: `engine/pkg/sysService/healthservice`

- [ ] **Step 1: Run healthservice tests to confirm mock failure**

Run:

```powershell
go test ./engine/pkg/sysService/healthservice -count=1
```

Expected before mock update: FAIL to compile if `mockRouter` is used as `inf.INodeRouter` and lacks `RouteByPid`.

- [ ] **Step 2: Add `RouteByPid` to `mockRouter`**

In `engine/pkg/sysService/healthservice/healthservice_test.go`, add this method beside the other mock router methods:

```go
func (mr *mockRouter) RouteByPid(_, _ *actor.PID) inf.IBus { return nil }
```

- [ ] **Step 3: Run healthservice tests**

Run:

```powershell
go test ./engine/pkg/sysService/healthservice -count=1
```

Expected: PASS.

---

## Task 7: Verify Selector Semantics Did Not Change

**Files:**
- Test only: `engine/pkg/cluster/endpoints/repository/repository_test.go`
- Test only: `engine/pkg/cluster/endpoints/endpoints_test.go`
- Test only: `engine/pkg/router/selector_test.go`

- [ ] **Step 1: Run focused route and selector tests**

Run:

```powershell
go test ./engine/pkg/router ./engine/pkg/cluster/endpoints ./engine/pkg/cluster/endpoints/repository ./engine/pkg/core/rpc ./engine/pkg/sysService/healthservice
```

Expected: PASS.

- [ ] **Step 2: Run broader engine package tests**

Run:

```powershell
go test ./engine/pkg/...
```

Expected: PASS. If unrelated existing tests fail, record the failing package and error, then rerun the focused tests above to confirm `RouteByPid` scope remains healthy.

- [ ] **Step 3: Confirm unchanged search behavior by code inspection**

Check these methods and verify no logic changed except additions from this plan:

```go
func (r *Repository) SelectByPid(sender, receiver *actor.PID) inf.IBus
func (r *Repository) SelectByRule(sender *actor.PID, rule func(pid *actor.PID) bool) inf.IBus
func (r *Repository) Select(sender *actor.PID, options ...inf.SelectParamBuilder) inf.IBus
func (r *Repository) SelectByServiceType(sender *actor.PID, partition int32, serviceType, serviceName string) inf.IBus
func (r *Repository) SelectByFilterAndChoice(sender *actor.PID, filter func(pid *actor.PID) bool, choice func(pids []*actor.PID) []*actor.PID) inf.IBus
```

Expected: private services remain non-searchable through normal selector APIs.

---

## Self-Review Checklist

- Spec coverage: This plan covers the agreed `RouteByPid` name, the business-owned partial PID contract, router/interface propagation, EndpointManager temporary dispatcher routing, and tests.
- Placeholder scan: No implementation step uses unresolved placeholders. Each code-changing step includes the exact code to add.
- Type consistency: All public method signatures use `RouteByPid(sender, receiver *actor.PID) IBus` at router/interface layers and `RouteByPid(receiver *actor.PID) IBus` at business-facing `IRpcSelector`/`Handler` layer.
- Scope check: This is a single focused API addition. It does not introduce `RouteTarget`, does not alter discovery, and does not modify search selectors.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-06-29-route-by-pid.md`. Two execution options:

**1. Subagent-Driven (recommended)** - Dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints.

Choose the execution mode before implementation starts.
