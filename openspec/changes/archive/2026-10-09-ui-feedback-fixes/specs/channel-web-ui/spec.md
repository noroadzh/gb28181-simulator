## ADDED Requirements

### Requirement: Root Path Redirects to Dashboard

Web 路由的根路径 `/` MUST 重定向到 `/dashboard`，使打开 Web 界面的用户直接看到仪表盘视图。

#### Scenario: User opens root path

WHEN user navigates to the web UI root path "/"
THEN browser MUST redirect to "/dashboard"
AND Dashboard page MUST load immediately showing system overview, channel list, and live log

### Requirement: Account List Displays Data Correctly (No "undefined" String)

`AccountsView.vue` 表格列的字段名 MUST 与后端返回的 JSON key 大小写一致，消除 "undefined" 显示。后端 JSON 返回 `username`（小写）与 `created_at`（小写）。

#### Scenario: Account list renders with data

WHEN platform node has accounts in the system
AND user navigates to the accounts management page
THEN table MUST display correct username values (not "undefined")
AND table MUST display correct creation timestamps (not "undefined")
AND no "undefined" string MUST appear anywhere in the account list

### Requirement: Accounts Page Shows Clear Message for Non-Platform Nodes

当用户通过 URL 直接访问账号管理页时，若当前选中的节点不是 platform 类型（或无 platform 节点），页面 MUST 显示明确的说明信息，而非仅显示空表格或无数据提示。

#### Scenario: Device node user visits accounts page

WHEN user navigates to the accounts management page
AND the selected node is a device node (not platform)
THEN page MUST display a visible warning or notice: "账号管理仅适用于 platform 类型节点，当前节点类型为 device"
AND table MUST remain empty or show an appropriate empty state

### Requirement: Channel Upload Entry Is Directly Accessible

通道卡片的媒体源配置入口 MUST 支持直接从卡片操作区域点击"上传媒体"按钮打开上传对话框，无需通过嵌套下拉菜单进入。

#### Scenario: User uploads media file to a channel

WHEN user is on the channel list page
AND clicks the upload/media button on a channel card
THEN upload dialog MUST open immediately without navigating through dropdown menus
AND selected file MUST be uploaded and bound to that channel automatically
