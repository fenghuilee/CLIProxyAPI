# CPA Portal Admin 插件 (cpa-plugin-portal-admin)

CPA 管理端扩展插件，专用于对 **Portal 平台**中所有用户的 **API 用量审计 (Usage)**、**AIGC 内容生成任务 (Content Generation)** 以及 **充值与财务账本 (Recharge & Wallet Ledger)** 进行全景管理与运维干预。

---

## 一、 功能特性

1. **📊 运营大盘**：
   - 注册用户总数、累计充值总额、累计消耗积分、API 请求总量、Token 消耗、AIGC 任务总数。
   - Top 5 用户消费排行、Top 5 热门模型调用统计、近 14 天用量趋势。
2. **👥 用户与余额管控**：
   - 全员用户列表查看、搜索、启停状态切换。
   - **手动调账与充值**：支持直接增减用户可用积分，底层强行锁事务并自动写入不可变 `wallet_ledger` 流水记录。
3. **💰 充值订单与流水**：
   - 全员微信/支付宝充值订单检索与状态监控。
   - **异常手动补单**：针对支付成功但回调超时的订单，支持管理员一键补单入账。
   - **充值套餐管理**：可视化维护微信/支付宝金额与赠送积分套餐。
   - **全局资金流水**：全景检索充值、扣费、冻结、解冻、调账流水。
4. **📈 API 用量与请求审计**：
   - 全员 API 请求明细日志查询（支持按用户、API Key、模型、Provider、成功/失败状态多维过滤）。
   - 失败请求错误分析（直接展示 HTTP 错误码与上游错误详情）。
5. **🎬 全员 AIGC 任务中心**：
   - 视频/图片生成任务实时看板（状态、阶段、进度、耗时）。
   - 任务详情查看、Prompt 查看、生成产物（视频/图片）在线播放与预览。
   - **僵尸任务强制取消**与**异常任务冻结额度退费**。

---

## 二、 CPA 配置文件集成 (`config.yaml`)

在 CPA 的 `config.yaml` 中配置启用该插件：

```yaml
plugins:
  enabled: true
  dir: "plugins"
  configs:
    portal-admin:
      enabled: true
      priority: 10
      db_host: "127.0.0.1"
      db_port: 3306
      db_user: "root"
      db_password: "your_password"
      db_name: "cliproxyapi"
      db_charset: "utf8mb4"
      # 或直接配置完整 DSN:
      # dsn: "root:your_password@tcp(127.0.0.1:3306)/cliproxyapi?charset=utf8mb4&parseTime=True&loc=Local"
```

---

## 三、 编译与构建

在 Linux / macOS / Windows 环境下：

```bash
cd plugins/portal-admin
make build
```

编译完成后将生成 `portal-admin.so`（Linux）、`portal-admin.dylib`（macOS）或 `portal-admin.dll`（Windows），放置于 CPA 的 `plugins/` 目录下即可热加载。

---

## 四、 访问控制台

1. 启动 CPA 并确保加载 `portal-admin` 插件。
2. 打开 **CPA-Manager-Plus** 或 CPA Web 管理控制台，在侧边栏中将自动出现菜单项：
   - **`运营中心`**
3. 点击即可在 iframe 中直接打开全功能响应式运营控制台。
