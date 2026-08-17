# 微信支付 SCF Serverless 网关后端

基于 **腾讯云 SCF Go 函数** + **微信支付 APIv3 官方 Go SDK** 的极简支付后端。

## 特性

- 官方 SDK `wechatpay-apiv3/wechatpay-go`，签名/验签/加解密全托管
- **微信支付公钥模式**（无需下载平台证书，更安全）
- SCF 静态函数 + 函数 URL
- 全部密钥从环境变量/文件读取，零硬编码
- 5 个核心接口：Native 下单、JSAPI 下单、退款、回调验签、查询订单

## API 接口

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/pay/native` | Native 下单（PC 扫码，返回 `code_url`） |
| POST | `/pay/jsapi` | JSAPI 下单（小程序/公众号，返回调起支付参数） |
| POST | `/pay/refund` | 申请退款 |
| POST | `/pay/notify` | 微信支付回调通知（自动验签解密） |
| GET  | `/pay/query?out_trade_no=xxx` | 查询订单 |

### 请求示例

**Native 下单**
```bash
curl -X POST https://<function-url>/pay/native \
  -H "Content-Type: application/json" \
  -d '{"description":"测试商品","amount":100}'
# amount 单位：分（100 = 1 元）
```

**JSAPI 下单**
```bash
curl -X POST https://<function-url>/pay/jsapi \
  -H "Content-Type: application/json" \
  -d '{"description":"测试商品","amount":100,"openid":"oUpF8uMuAJO_M2pxb1Q9zNjWeS6o"}'
```

**退款**
```bash
curl -X POST https://<function-url>/pay/refund \
  -H "Content-Type: application/json" \
  -d '{"out_trade_no":"SCF20260101120000","refund":50,"total":100,"reason":"商品退货"}'
```

## 环境变量

在 SCF 控制台「函数管理 → 函数配置 → 环境变量」中设置：

| 变量名 | 说明 | 示例 |
|--------|------|------|
| `WXPAY_MCHID` | 商户号 | `xxxxxxxxxx` |
| `WXPAY_APPID` | 应用 ID | `xxxxxxxxxxxxxxxxxx` |
| `WXPAY_APIV3KEY` | APIv3 密钥（32 位） | `xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx` |
| `WXPAY_CERT_SERIAL_NO` | 商户证书序列号 | `xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx` |
| `WXPAY_NOTIFY_URL` | 公网 HTTPS 回调地址 | `https://<function-url>/pay/notify` |
| `WXPAY_PUB_KEY_ID` | 微信支付公钥 ID | `PUB_KEY_ID_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx` |

### 密钥加载（二选一）

**方式一：环境变量（推荐，生产环境）**

将 PEM 内容直接设为环境变量：

| 变量名 | 内容 |
|--------|------|
| `WXPAY_PRIVATE_KEY` | `apiclient_key.pem` 的完整 PEM 文本 |
| `WXPAY_PUBLIC_KEY` | `pub_key.pem` 的完整 PEM 文本 |

**方式二：文件路径（本地测试 / 打包到 zip）**

| 变量名 | 内容 |
|--------|------|
| `WXPAY_PRIVATE_KEY_PATH` | 私钥文件路径（默认 `keys/apiclient_key.pem`） |
| `WXPAY_PUBLIC_KEY_PATH` | 公钥文件路径（默认 `keys/pub_key.pem`） |

## 编译打包

```bash
# 1. 整理依赖
make tidy

# 2. 编译打包（仅二进制）→ 产出 main.zip
make build

# 3. 打包含密钥（可选，本地测试用）
make build-with-keys
```

## SCF 控制台部署（图文步骤）

> 以下步骤对照 SCF 控制台「新建函数」页面的实际表单字段。

### 第一步：进入新建页面

打开 [SCF 控制台](https://console.cloud.tencent.com/scf/list) → 选择地域（如**成都**）→ 点击「新建」。

### 第二步：选择创建方式

选择 **「从头开始」**（从一个 Hello World 示例开始）。

### 第三步：基础配置

| 字段 | 填写值 | 说明 |
|------|--------|------|
| **函数类型** | `事件函数` | 接收 JSON 格式事件触发执行 |
| **函数名称** | `pay` | 字母开头，2~60 字符 |
| **地域** | `成都`（或就近选择） | 与密钥申请地域无关 |
| **运行环境** | `Go 1` | ⚠️ 必须选 Go 1，不是其他语言 |
| **时区** | `UTC` | 默认即可（代码内用 `time.Now()` 自行处理） |

### 第四步：函数代码

| 字段 | 填写值 | 说明 |
|------|--------|------|
| **提交方法** | `本地上传zip包` | Go 运行时不支持在线编辑 |
| **执行方法** | `main` | 即二进制文件名，无需 `main.handler` 格式 |

点击上传 `make build` 产出的 `main.zip`。

### 第五步：高级配置（关键）

展开「高级配置」面板：

#### 环境配置

| 字段 | 建议值 | 说明 |
|------|--------|------|
| **内存** | `256MB`（最低）或 `512MB` | 支付场景涉及 RSA 签名，128MB 偏小 |
| **临时存储** | `0.5 GB` | 默认免费额度 |
| **初始化超时时间** | `65 秒` | 默认值，SDK 首次初始化需下载证书 |
| **执行超时时间** | `60 秒` | ⚠️ 默认 3 秒太短！支付接口需调大（范围 1-900 秒） |

#### 环境变量

在「环境变量」表格中逐行添加（key/value）：

```
WXPAY_MCHID          = xxxxxxxxxx
WXPAY_APPID          = xxxxxxxxxxxxxxxxxx
WXPAY_APIV3KEY       = xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
WXPAY_CERT_SERIAL_NO = xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
WXPAY_PUB_KEY_ID     = PUB_KEY_ID_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
WXPAY_NOTIFY_URL     = https://<你的函数URL地址>/pay/notify
WXPAY_PRIVATE_KEY    = -----BEGIN PRIVATE KEY-----\n...(apiclient_key.pem 完整内容)
WXPAY_PUBLIC_KEY     = -----BEGIN PUBLIC KEY-----\n...(pub_key.pem 完整内容)
```

> **⚠️ PEM 密钥换行问题**：SCF 控制台环境变量输入框粘贴多行 PEM 文本时，换行符可能被转义为字面 `\n`。
> 代码已自动处理 `\n` 转义，直接粘贴 PEM 全文即可。如仍报错，可在 `/pay/health` 接口查看具体错误信息。
>
> 也可用单行格式：`-----BEGIN PRIVATE KEY-----\nMIIEvQ...\n-----END PRIVATE KEY-----`（用 `\n` 表示换行）。

#### 网络配置

| 字段 | 值 | 说明 |
|------|----|------|
| **公网访问** | ✅ 启用 | 默认已启用，支付必须访问微信支付 API |

### 第六步：触发器配置

> ⚠️ **重要变更**：API 网关触发器已于 **2025-06-30 停止服务**，不再支持新建。
> 替代方案：使用**函数 URL**（推荐）或 **TSE 云原生网关**。

触发器选择 **「暂不创建」**，先完成函数创建。

### 第七步：完成创建并配置函数 URL

1. 勾选「我已阅读并同意《腾讯云云函数网络服务协议》」→ 点击「完成」
2. 进入函数详情页 →「触发管理」→ 点击「创建触发器」
3. 触发方式选择 **「函数 URL」**：
   - 鉴权类型：`免鉴权`（支付回调需公网可访问，验签由代码 SDK 处理）
   - 或选 `云API鉴权`（如需限制调用方）
4. 创建后获得 URL：`https://<function-url>.ap-chengdu.tencentscfapp.com/`

### 第八步：回填回调地址

将上一步获得的函数 URL 填入环境变量：

```
WXPAY_NOTIFY_URL = https://<function-url>.ap-chengdu.tencentscfapp.com/pay/notify
```

在控制台「函数配置 → 环境变量」中修改，保存后生效。

### 验证

```bash
# 健康检查
curl https://<function-url>.ap-chengdu.tencentscf.com/pay/health
# 期望返回: {"code":0,"message":"ok","data":"ok"}

# Native 下单测试
curl -X POST https://<function-url>.ap-chengdu.tencentscf.com/pay/native \
  -H "Content-Type: application/json" \
  -d '{"description":"测试商品","amount":1}'
```

## 安全说明

- 私钥 `apiclient_key.pem` 绝不硬编码，全部从环境变量读取
- 回调通知使用 SDK 自动验签（RSA-SHA256），拒绝伪造请求
- APIv3Key 仅用于回调解密，不参与请求签名
- 公钥模式避免平台证书下载的中间人风险
