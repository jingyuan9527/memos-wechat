# memos-wechat

微信公众号测试号回调服务：只接收**文本消息**，校验发送者白名单后异步写入自建 [Memos](https://usememos.com/)，并立刻返回固定回复。

- 单一路由 `GET/POST /callback`，其他路径一律 404
- 只处理文本，图片 / 语音 / 视频 / 位置等消息直接丢弃
- Memos 调用异步执行，不阻塞微信回调链路
- 消息原文原样透传为 memo 内容，可见性固定 `PRIVATE`

## 环境要求

- Go 1.22+（或 Docker）
- Memos v0.22+，并已生成 Access Token
- 公网可访问的 HTTPS 回调地址（隧道工具属于外部依赖，不在本项目范围内）

## 环境变量

服务自身配置项集中声明在 `docker-compose.yml` 的 `environment` 中；直接跑二进制时通过 shell 环境变量注入。

| 变量 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `WECHAT_TOKEN` | 是 | — | 微信测试号后台自定义的 Token，用于签名校验 |
| `WECHAT_ALLOW_OPENID` | 是 | — | 唯一放行的发送者 OpenID，其余来源一律忽略 |
| `MEMOS_API_URL` | 是 | — | Memos 实例基地址，不含 `/api/v1` 前缀 |
| `MEMOS_ACCESS_TOKEN` | 是 | — | Memos 访问令牌，即 `Bearer` 之后的字符串 |
| `LISTEN_PORT` | 否 | `8080` | 容器内服务监听端口 |
| `HOST_PORT` | 否 | `8080` | 宿主机映射端口，仅 compose 使用，写在 `.env` 里 |

配置全部来自环境变量，缺少必填项时进程启动即失败，不存在硬编码兜底。

`MEMOS_API_URL` 会被归一化：漏写 `https://` 会自动补全，结尾多余的 `/` 会被去掉。

> **敏感值不要提交**：`WECHAT_TOKEN` 与 `MEMOS_ACCESS_TOKEN` 直接写在 `docker-compose.yml` 中，填入真实值后提交会泄露。提交前务必用 `git diff --cached` 自查；若已泄露，需立即到微信后台与 Memos 重置令牌。可用 `git update-index --skip-worktree docker-compose.yml` 让本地改动不再进入暂存区。

## 运行

### 本地

```bash
export WECHAT_TOKEN=your_token
export WECHAT_ALLOW_OPENID=your_openid
export MEMOS_API_URL=https://memos.example.com
export MEMOS_ACCESS_TOKEN=your_memos_token

go run .
```

### Docker Compose

```bash
# 1. 编辑 docker-compose.yml，填入 WECHAT_TOKEN / WECHAT_ALLOW_OPENID / MEMOS_API_URL / MEMOS_ACCESS_TOKEN
# 2. 可选：cp .env.example .env 并修改 HOST_PORT 覆盖宿主机端口
docker compose up -d
```

上面的命令拉取 CI 产物；本地改代码后想自行构建，加 `--build`：

```bash
docker compose up -d --build
```

### 纯镜像部署（docker-compose-ghcr.yml）

只拉取 GHCR 上的 CI 产物，不含 `build` 指令，适合部署机无源码或不想本地构建的场景。配置项全部从 `.env` 读取，令牌不会进入被追踪的 compose 文件：

```bash
# 1. 准备 .env 并填入 4 个必填项
cp .env.example .env
# 2. 拉取并启动
docker compose -f docker-compose-ghcr.yml up -d
```

镜像 tag 直接写在 `image` 行，默认 `latest`；要锁版本就把 `:latest` 换成 `:1.0.0`。容器端口固定映射到宿主机 `8080`，不受 `.env` 中 `HOST_PORT` 影响（该项只服务于 `docker-compose.yml`）。

必填项缺失时 compose 会直接报错退出，不会带着空令牌把服务拉起来。

### 直接使用镜像

```bash
docker run -d --name memos-wechat --restart unless-stopped -p 8080:8080 \
  -e WECHAT_TOKEN=your_token \
  -e WECHAT_ALLOW_OPENID=your_openid \
  -e MEMOS_API_URL=https://memos.example.com \
  -e MEMOS_ACCESS_TOKEN=your_memos_token \
  ghcr.io/jingyuan/memos-wechat:latest
```

## 测试

```bash
go test ./...
```

测试与需求中的最小验收用例一一对应：

| 验收用例 | 对应测试 |
| --- | --- |
| 配置页 GET 校验通过 / 失败 | `TestVerifyURLEchoesEchostr`、`TestCheckSignatureRejectsInvalidInput` |
| 白名单用户发文本消息，写入 1 条私有 memo | `TestWhitelistedTextCreatesOnePrivateMemo` |
| 非白名单 OpenID 不生成 memo | `TestNonWhitelistedOpenIDIsIgnored` |
| 图片等非文本消息不生成 memo | `TestNonTextMessageIsIgnored` |
| 同 `MsgId + CreateTime` 重试只创建 1 条 memo | `TestDuplicatePushCreatesOnlyOneMemo` |
| Memos 服务不可用时仍正常回复 | `TestMemosFailureStillReplies`、`TestMemosUnreachableStillReplies` |
| 仅 `/callback` 对外开放 | `TestOnlyCallbackRouteIsExposed` |

## 镜像构建

`.github/workflows/docker-publish.yml` 负责构建并推送镜像到 GHCR，使用内置 `GITHUB_TOKEN`，无需在仓库里配置任何 secrets。

| 触发条件 | 行为 |
| --- | --- |
| 推送到 `main` | 构建 `linux/amd64` + `linux/arm64` 并推送，标签含 `main`、`latest` |
| 推送 `v*` 标签 | 同上，标签含 `1.2.3`、`1.2` |
| 向 `main` 提 PR | 仅构建 `linux/amd64` 验证可构建，不登录、不推送 |
| 手动触发 | 同推送行为 |

构建前先跑 `gofmt` / `go vet` / `go test`，任一失败即不产出镜像。

镜像地址为 `ghcr.io/<你的 GitHub 用户名>/<仓库名>`（用户名带大写时会自动转小写）。GHCR 包默认私有，首次推送后如要免登录拉取，需在仓库的 Packages 设置中把该包改为 Public；否则先执行 `docker login ghcr.io`。

## 微信测试号配置

在测试号后台「接口配置信息」中填写：

- URL：`https://<你的域名>/callback`
- Token：与 `WECHAT_TOKEN` 完全一致

点击提交时，微信会发起 `GET /callback` 签名校验；校验通过后服务原样返回 `echostr`，配置即生效。

## 行为说明

| 场景 | 返回给微信 | 是否写 Memos |
| --- | --- | --- |
| `GET` 签名校验通过 | `echostr` | 否 |
| `GET` 签名校验失败 | `fail` | 否 |
| `POST` 签名校验失败 | `fail`（HTTP 403） | 否 |
| 白名单用户 + 文本消息 | 固定文案 `✅已存入Memos` 的 XML | 是（异步） |
| 同一 `MsgId + CreateTime` 重复推送 | 固定文案 `✅已存入Memos` 的 XML | 否（去重命中） |
| 非白名单 OpenID | 纯文本 `success` | 否 |
| 非文本消息类型 | 纯文本 `success` | 否 |
| XML 解析失败 / 请求体读取失败 | 纯文本 `success` | 否 |
| Memos 调用失败 | 不受影响，照常返回固定文案 | 是（失败仅记日志） |

补充说明：

- **幂等**：以 `MsgId + CreateTime` 为键做内存去重，`TTL = 60s`。去重登记在返回响应前同步完成，以覆盖微信重试与首次请求并发到达的情况。
- **异步**：Memos 写入在独立 goroutine 中执行，HTTP 客户端超时 `10s`。下游结果不回传微信。
- **签名**：`POST` 同样校验签名，避免公网端点被任意伪造请求驱动写入 Memos。
- **可见性**：固定 `PRIVATE`，不提供任何覆盖入口。

## 接口

### `GET /callback`

| 参数 | 说明 |
| --- | --- |
| `signature` | 微信签名 |
| `timestamp` | 时间戳 |
| `nonce` | 随机数 |
| `echostr` | 校验通过后原样返回 |

### `POST /callback`

请求体为微信消息 XML，解析 `FromUserName`、`MsgType`、`Content`、`MsgId`、`CreateTime`。

### `POST {MEMOS_API_URL}/api/v1/memos`

```json
{
  "content": "消息原文",
  "visibility": "PRIVATE"
}
```

请求头：`Authorization: Bearer {MEMOS_ACCESS_TOKEN}`、`Content-Type: application/json`。

## 日志

以 JSON 结构化输出到 stdout，覆盖：收到推送（含 openid、msgType、msgId、createTime、content）、是否放行、去重命中、Memos 调用结果、异常堆栈。启动与请求日志均不会打印令牌。

> 注意：为便于排障，日志会完整打印消息正文。若正文属于敏感信息，请在采集侧做脱敏或调整日志级别。

## 明确不做

- 不支持图片、语音、视频、位置、卡片消息
- 不实现微信内查询 / 修改 / 删除 Memo
- 不做标签解析、内容裁剪、模板替换
- 不自带公网隧道（cloudflared / ngrok 等属外部工具）
- 不支持多用户 / 多 OpenID 管理
- 不提供 Web 管理界面与数据库
- 不支持企业微信与个人微信协议机器人
