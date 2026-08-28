# CPA AIGC Business Plugin (`cpa-plugin-aigc-business`)

AIGC 内容生成领域业务与状态机管理插件。

---

## 核心职责

1. **业务状态机**：管理 AIGC 任务全生命周期的状态变更（`accepted` $\to$ `running` $\to$ `succeeded`/`failed`）。
2. **CAS 乐观锁防并发**：严格自增版本号 `revision`，杜绝并发竞争覆盖。
3. **分布式 Worker 租约**：提供 `Claim` / `Release` 调度租约机制，支持多节点并发抢占与超时重试。
4. **数据存储解耦**：自身不直连 MySQL，全部通过宿主数据总线（`host.db.query` / `host.db.exec`）向统一存储底座提交 SQL。

---

## 编译与安装

```bash
cd plugins/cpa-plugin-aigc-business
make clean && make build
```

编译产物会自动拷贝至 `../../bin/plugins/linux/amd64/aigc-business.so`。
