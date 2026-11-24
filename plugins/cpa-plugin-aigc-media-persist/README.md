# cpa-plugin-aigc-media-persist

CLIProxyAPI 官方媒体产物持久化转存插件（AIGC Permanent Media Archiving Plugin）。

---

## 1. 核心定位与设计哲学

在多模态图片、视频生成任务中，上游第三方供应商（如阿里 DashScope、火山引擎 Seedance、MiniMax 等）通常只提供带签名的临时下载链接（通常 24 小时过期失效）。

`cpa-plugin-aigc-media-persist` 介入任务完成前的 **`PhaseBeforeComplete`** 阶段：

1. **自动识别外部临时媒体**：检查产物 `Artifacts` 中的 HTTP/HTTPS 远程视频与图片；
2. **流式转存至自建对象存储**：将外部文件流式下载并上传至自建 TOS 存储桶（路径前缀 `aigc/outputs/YYYYMMDD/{sha256}.ext`）；
3. **安全替换与元数据对齐**：将 `item.URI` 更新为自建永久地址，`item.StorageProvider` 标为 `"tos"`，并在 `content_generations.output` JSON 中全局替换该 URL；
4. **财务安全与优雅降级（Graceful Fallback）**：
   - **核心原则**：上游生成成功即代表任务成功并锁定扣费；
   - 转存是一项增值增强。**若转存发生网络抖动或超时，插件严格保留原厂商地址，绝不将任务标记为失败，绝不触发平台向用户的误退款**。

---

## 2. 配置说明 (Config YAML)

插件通过加载配置 YAML 运行：

```yaml
endpoint: "https://tos-cn-beijing.volces.com" # 火山引擎 TOS Endpoint
region: "cn-beijing" # 火山引擎 TOS 所在区域 (默认 cn-beijing)
bucket: "my-tos-bucket" # 火山引擎 TOS 存储桶 Bucket 名称
access-key: "AKLT..." # 火山引擎 IAM Access Key
secret-key: "..." # 火山引擎 IAM Secret Key
public-base-url: "https://cdn.example.com" # 对象公网访问域名或 CDN 加速域名 (可选)
output-prefix: "aigc/outputs" # 产物存储路径前缀 (默认 aigc/outputs)
max-file-bytes: 209715200 # 单文件上限 (默认 200MB)
download-timeout-seconds: 60 # 单文件下载超时 (默认 60s)
concurrency: 4 # 并发转存数限制 (默认 4)
skip-domains: # 自定义排除跳过的域名列表
  - "my-internal-cdn.com"
```

---

## 3. 构建与测试

```bash
make build # 编译 C-Shared 动态库并拷贝至 bin/plugins/
make test  # 运行全量单元测试
```
