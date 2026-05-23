# RPC Spec Delta

## MODIFIED Requirements

### Requirement: 非 Reply 请求必须支持去重与授权检查

#### Scenario: 携带幂等键的请求走幂等去重检查

- **GIVEN** 一个非 Reply 请求携带非空 `IdempotencyKey`
- **WHEN** Handler 处理该请求
- **THEN** 必须使用去重器按完整 `IdempotencyKey` 做去重
- **AND** 不得使用 `ReqId` 作为业务幂等键

#### Scenario: 未携带幂等键的请求跳过幂等去重

- **GIVEN** 一个非 Reply 请求未携带 `IdempotencyKey`
- **WHEN** Handler 处理该请求
- **THEN** 必须跳过业务幂等去重

#### Scenario: 授权器启用时在 decode 前做授权检查

- **GIVEN** Handler 已注入且启用了 Authorizer
- **WHEN** 处理普通请求
- **THEN** 必须在 payload decode 之前执行授权检查
- **AND** 未授权请求必须被拒绝
