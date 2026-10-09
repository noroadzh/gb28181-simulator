# Spec Delta

## ADDED Requirements

### Requirement: 单个通道的运行时增删

`NodeProfile` MUST 提供在不可变拷贝路径上新增和删除单个通道的方法：`WithChannelAdded(ch Channel)` 在现有通道列表追加一个通道（重复 id 报错、空 id 报错）；`WithChannelRemoved(id string)` 移除指定 id 的通道（未知 id 报错）。两者 MUST 返回新 profile 副本，不就地修改原 profile。

#### Scenario: 运行时新增单个通道

- **WHEN** profile 含 1 个通道，调用 `WithChannelAdded(NewChannel("34020000001320000099","通道2",""))`
- **THEN** 返回的 profile 的 `Channels()` 含 2 个通道，新通道 id 在末尾

#### Scenario: 新增重复 id 报错

- **WHEN** 调用 `WithChannelAdded` 时传入的 id 与已有通道重复
- **THEN** 返回错误且原 profile 不变

#### Scenario: 运行时删除单个通道

- **WHEN** profile 含 2 个通道，调用 `WithChannelRemoved("<通道1 id>")`
- **THEN** 返回的 profile 的 `Channels()` 仅含 1 个通道

#### Scenario: 删除未知 id 报错

- **WHEN** 调用 `WithChannelRemoved` 时 id 不存在
- **THEN** 返回错误且原 profile 不变
