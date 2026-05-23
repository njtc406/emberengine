# RPC Idempotency Key Dedup Tasks

## 1. Protocol And Data Model

- [x] 1.1 Add `IdempotencyKey` to `engine/pkg/actor/actor.proto` `Message`.
- [x] 1.2 Regenerate protobuf Go code for actor messages.
- [x] 1.3 Add idempotency key accessors to envelope meta interfaces and implementation.
- [x] 1.4 Ensure `MsgEnvelope.ToProtoMsg` writes `IdempotencyKey` into `actor.Message`.

## 2. Client API And MessageBus

- [x] 2.1 Add `IdempotencyKey` to `dto.BusOption`.
- [x] 2.2 Add `dto.WithIdempotencyKey(key string)` builder.
- [x] 2.3 Propagate idempotency key from `CallWithOpt` into request meta.
- [x] 2.4 Propagate idempotency key from `AsyncCallWithOpt` into request meta.
- [x] 2.5 Propagate idempotency key from `SendWithOpt` into request meta.
- [x] 2.6 Preserve existing non-option `Call`、`AsyncCall`、`Send` behavior with empty idempotency key.
- [x] 2.7 Ensure `Send` carries `SenderPid` but does not allocate or carry `ReqId`.

## 3. Deduplicator

- [x] 3.1 Extend `IDeDuplicator` with `SeenKey(key string) bool`.
- [x] 3.2 Implement `SeenKey` for TTL deduplicator using the complete key unchanged.
- [x] 3.3 Implement `SeenKey` for LRU deduplicator using the complete key unchanged.
- [x] 3.4 Keep existing `Seen(serviceUid, id)` temporarily for compatibility if still referenced.

## 4. Remote Handler Semantics

- [x] 4.1 Replace `ReqId` based request dedup in remote handler with `IdempotencyKey` based dedup.
- [x] 4.2 Ensure requests without `IdempotencyKey` skip business idempotency dedup.
- [x] 4.3 Ensure idempotency dedup uses only the complete `IdempotencyKey` and does not append `SenderPid` implicitly.
- [x] 4.4 Keep authorization behavior separate from idempotency behavior.

## 5. ReqId Simplification

- [x] 5.1 Simplify `RpcMonitor.GenSeq` to use node-local atomic increment only.
- [x] 5.2 Remove epoch and mask logic that existed for cross-restart dedup uniqueness.
- [x] 5.3 Keep Call/AsyncCall monitor registration using the generated `ReqId` before dispatch.
- [x] 5.4 Keep Send out of `ReqId` allocation and monitor wait-state registration.

## 6. Tests And Validation

- [x] 6.1 Add MessageBus tests for `WithIdempotencyKey` propagation on CallWithOpt.
- [x] 6.2 Add MessageBus tests for `WithIdempotencyKey` propagation on AsyncCallWithOpt.
- [x] 6.3 Add MessageBus tests for `WithIdempotencyKey` propagation on SendWithOpt.
- [x] 6.4 Add remote handler tests: first idempotency key is delivered, repeated key is dropped.
- [x] 6.5 Add remote handler tests: same `ReqId` without `IdempotencyKey` is not treated as duplicate.
- [x] 6.6 Add MessageBus tests that Send carries `SenderPid` and leaves `ReqId` unset.
- [x] 6.7 Run `go test ./engine/pkg/rpc/message/msgbus ./engine/pkg/rpc/remote/handler ./engine/pkg/utils/dedup ./engine/pkg/monitor -count=1`.
