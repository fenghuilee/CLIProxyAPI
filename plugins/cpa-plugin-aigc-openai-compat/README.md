# CPA Plugin AIGC OpenAI-Compat

`cpa-plugin-aigc-openai-compat` 是一个针对 `CLIProxyAPI` 的统一多模态 AIGC 驱动插件，实现了 `aigc.ContentGenerationDriver` 接口。

本插件将 **阿里 DashScope (Qwen-Image)**、**火山方舟 (Doubao Seedance 视频)**、**火山方舟 (Doubao Seedream 生图与图层拆分)** 以及 **通用 OpenAI-Compatible（标准生图/视频）** 四大适配器收敛整合到单一驱动中。

---

## 适配器与支持特性

| Adapter 名称 | 模态 | 目标平台 | 特性与参数支持 |
| :--- | :--- | :--- | :--- |
| **`qwen-image`** | `image` | 阿里 DashScope | • 文生图、图生图、涂抹局部重绘<br>• 支持 `prompt_extend`、`enable_thinking`、水印控制 |
| **`volcengine-seedance`** | `video` | 火山方舟 ARK | • 文本/图片生成视频（首尾帧控制）<br>• 支持运镜 `camera_motion`、`fps`、`duration`、`ratio`、`resolution` |
| **`volcengine-seedream`** | `image` | 火山方舟 ARK | • 1K/1.5K/2K 动态阶梯分辨率适配<br>• 支持 **`layer_decomposition` 图层拆分**模式 |
| **`openai-compat`**<br>(通用兜底) | `image`<br>`video` | 通用渠道 (DALL-E, Sora, ZeroAPI, SiliconFlow, Flux 等) | • 输出标准相对路径 `/images/generations`、`/videos/generations`<br>• **零 Endpoint 配置**，直通 CPA 各凭证上游 BaseURL<br>• 同步/异步自动探测 |

---

## 匹配顺序与短路路由 (Specialized First, Universal Fallback)

1. `qwen-image`（优先匹配 `qwen/*`, `alibaba/*`, `qwen-image-*` 等图像请求）
2. `volcengine-seedance`（优先匹配 `volcengine/doubao-seedance-*` 等视频请求）
3. `volcengine-seedream`（优先匹配 `volcengine/doubao-seedream-*` 等图像与拆层请求）
4. `openai-compat`（通用兜底，匹配 `openai/*`, `dall-e-*`, `sora-*`, `zeroapi/*` 及所有其他 AIGC 模型）

---

## 配置示例 (`config.yaml`)

```yaml
plugins:
  configs:
    aigc-openai-compat:
      qwen-image:
        enabled: true
        models:
          - "qwen/*"
          - "alibaba/*"
          - "qwen-image-*"
        prompt-extend: true
        enable-thinking: true

      volcengine-seedance:
        enabled: true
        models:
          - "volcengine/doubao-seedance-*"
          - "doubao-seedance-*"

      volcengine-seedream:
        enabled: true
        models:
          - "volcengine/doubao-seedream-*"
          - "doubao-seedream-*"

      openai-compat:
        enabled: true
        fallback: true
        image-models:
          - "*"
        video-models:
          - "*"
```

---

## 编译与测试

```bash
# 运行单元测试
go test -v ./...

# 编译 C-Shared 动态库
make build
```
