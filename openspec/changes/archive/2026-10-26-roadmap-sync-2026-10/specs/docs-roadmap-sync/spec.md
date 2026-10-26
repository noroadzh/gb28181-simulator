# Spec Delta

## ADDED Requirements

### Requirement: 归档目录全覆盖

路线图 MUST 让 `openspec/changes/archive/` 下的每个目录都能被定位：要么作为编号阶段行出现（`编号` 列即目录名去掉日期前缀），要么在文档内单独登记。新增归档若不对应既有编号，MUST 在同一提交内补登记为递增编号的新行，状态 `✅ 已归档`，说明列带与目录前缀一致的归档日期。

#### Scenario: 跟进型归档被补登记

- **WHEN** 某 change 归档后不对应路线图中已有的任何编号
- **THEN** 路线图在同一提交内新增一行：编号为当前最大编号 +1，`编号` 列取该 change 名，状态 `✅ 已归档`，说明列带 `（YYYY-MM-DD）` 且日期等于归档目录前缀

#### Scenario: 目录与路线图不一致即为漂移

- **WHEN** `openspec/changes/archive/` 中存在某个目录，而路线图中既无同名编号行、也无单独登记
- **THEN** 该状态 MUST 视为文档漂移，由下一次归档执行人或专项 cleanup change 修正
