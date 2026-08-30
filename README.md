# Clash Subscription Decoder

<p align="center">
  简体中文 | <a href="README_EN.md">English</a>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.26%2B-00ADD8?style=flat-square&logo=go" alt="Go Version">
  <img src="https://img.shields.io/badge/Vue-3.x-4FC08D?style=flat-square&logo=vue.js" alt="Vue Version">
  <img src="https://img.shields.io/badge/Vite-8.x-646CFF?style=flat-square&logo=vite" alt="Vite Version">
  <img src="https://img.shields.io/badge/PostgreSQL-13%2B-4169E1?style=flat-square&logo=postgresql" alt="PostgreSQL Version">
  <img src="https://img.shields.io/badge/License-No--Sale-red?style=flat-square" alt="No-Sale License">
</p>

`Clash Subscription Decoder` 是一个面向 Clash/Mihomo、Surge 与 Shadowrocket 的私有化订阅管理平台。它把远程订阅、本地手动配置、自定义节点、策略组、分流规则和多客户端格式输出集中到一个可视化后台中，适合需要长期维护多套代理配置并降低订阅链接泄漏风险的个人或小团队。

传统在线订阅转换服务往往无法沉淀个性化配置，也会把订阅内容暴露给第三方服务。该项目的核心目标是：在自有环境中保存配置、按配置生成安全订阅地址，并在刷新失败时保留最近一次可用结果。

---

## 🌟 核心特性

- 🔒 **配置级 Token 鉴权**
  每个订阅配置拥有独立 Token。重新生成 Token 后，旧地址立即失效；复制订阅地址只读取当前有效 Token，不会误覆盖旧地址。

- 🛡️ **随机控制台入口**
  控制台仅通过 `/<32 位十六进制哈希>` 入口访问。未配置入口时系统会首次随机生成并持久化，其他未知页面返回真实 HTTP 404。

- 🧩 **结构化 YAML 合并与本地接管**
  后端使用 `yaml.v3` 节点树处理 Clash/Mihomo YAML，将自定义节点、策略组、规则、排序和隐藏状态重新应用到订阅结果中。订阅资源可以在后台一键接管为本地资源，后续按当前配置独立维护。

- 🔄 **远程刷新与缓存兜底**
  远程订阅配置在刷新或客户端拉取订阅时会尝试重新请求上游并更新缓存；如果上游暂时不可用，系统会在已有缓存存在时输出最近一次成功生成的配置。本地手动配置不请求上游，而是直接根据手动维护的节点、策略组和规则生成 YAML。

- 🗂️ **多配置隔离管理**
  支持创建多套远程订阅或本地手动配置。每套配置的节点、策略组、规则、资源排序、隐藏记录和订阅 Token 独立生效，可按场景维护不同客户端或不同线路策略。

- 📲 **多客户端订阅输出**
  同一个配置可输出 Clash/Mihomo YAML、Surge 最新版配置、Surge 5.7.6 兼容配置、Shadowrocket 配置，并提供 Shadowrocket 安装入口。

- 🎛️ **可视化控制台**
  前端基于 Vue 3、Vite、Element Plus 与 CodeMirror 构建，支持订阅解析预览、节点链接解析、资源拖拽排序、批量规则保存/删除、跨配置复制代理组与规则、远程规则本地化、数据备份与导入。

---

## 🏗️ 架构与工作流

```mermaid
sequenceDiagram
    autonumber
    actor Client as 代理客户端
    participant Server as Go 后端
    participant DB as PostgreSQL
    participant Upper as 远程订阅上游

    Client->>Server: GET /sub?token=xxx 或 /surge.conf?token=xxx
    Server->>DB: 校验配置 Token 并读取对应配置
    alt Token 无效或过期
        Server-->>Client: 401 Unauthorized
    end

    alt 远程订阅配置
        Server->>Upper: 请求远程订阅内容
        alt 刷新成功
            Server->>Server: 解码 Base64/YAML/节点 URI
            Server->>DB: 保存原始响应与生成后的 YAML 缓存
        else 刷新失败且存在历史缓存
            Server->>DB: 读取最近一次成功生成的 YAML 缓存
        else 刷新失败且无缓存
            Server-->>Client: 502 Bad Gateway
        end
    else 本地手动配置
        Server->>DB: 读取本地节点、策略组与规则
        Server->>Server: 生成 Clash/Mihomo YAML
        Server->>DB: 保存生成结果
    end

    Server->>DB: 应用自定义节点、策略组、规则、排序与隐藏记录
    alt Shadowrocket / Surge 输出
        Server->>Server: 将最终 Clash YAML 转换为目标 .conf
        Server-->>Client: 200 OK text/plain
    else Clash / Mihomo 输出
        Server-->>Client: 200 OK text/yaml
    end
```

---

## 🛠️ 技术栈

- **前端**
  - Vue 3 + TypeScript
  - Vite
  - Element Plus
  - Axios
  - CodeMirror YAML 编辑器
  - js-yaml

- **后端**
  - Go 1.26+
  - Gin
  - GORM
  - PostgreSQL
  - `gopkg.in/yaml.v3`
  - base64Captcha

---

## 🚀 快速上手

### 1. 环境准备

请先准备：

- [Go 1.26+](https://go.dev/)
- [Node.js 20+](https://nodejs.org/)
- PostgreSQL 数据库
- `pnpm` 或 `npm`

```bash
git clone https://github.com/mangobubu/clash-subscription-decoder.git
cd clash-subscription-decoder
```

### 2. 后端配置与运行

后端读取当前运行目录下的 `config.toml`。开发环境通常在 `backend` 目录内运行，因此先复制配置模板并填写 PostgreSQL 连接信息：

```bash
cd backend
cp config.example.toml config.toml
```

重点配置项：

```toml
[server]
port = 8080

[database]
host = "127.0.0.1"
user = "your_username"
password = "your_password"
dbname = "clash_subscription_decoder"
port = 5432
sslmode = "disable"
timezone = "Asia/Shanghai"

[subscription_fetch]
user_agent = "clash.meta"
proxy_url = ""
insecure_skip_verify = false

[auth]
login_entry_hash = ""
captcha_enabled = true
```

启动开发后端：

```bash
go mod tidy
go run main.go
```

后端默认监听 `http://localhost:8080`。控制台需要通过启动日志打印的随机哈希入口访问，例如 `http://localhost:8080/0123456789abcdef0123456789abcdef`。

#### 控制台登录入口

- 登录入口格式固定为 `/<32 位十六进制哈希>`，哈希配置值本身不包含开头的 `/`。
- 环境变量 `LOGIN_ENTRY_HASH` 的优先级高于 `config.toml` 中的 `[auth].login_entry_hash`。
- 两项均为空时，系统会在首次启动时安全生成入口哈希并持久化到 PostgreSQL，后续重启继续使用同一入口。
- 启动日志会打印当前有效的完整登录入口路径。请将它与实际访问域名组合使用、妥善保存，并避免公开到截图、监控标签或公共日志中。
- `/`、`/login`、错误哈希及其他不存在的页面都会返回真实 HTTP 404，不会回退到控制台页面。

如果通过 Nginx、Caddy 或其他反向代理部署，应将请求原样转发给后端，并保留后端返回的 404。不要使用 `try_files $uri /index.html` 或等价规则把任意路径回退到 `index.html`，否则会绕过不存在页面的 404 行为。

#### 上游订阅返回 403/404

部分订阅服务会识别请求客户端。链接在 Clash/Mihomo 中可用、浏览器直接访问却返回 404，通常是服务商有意隐藏订阅内容，并不表示链接失效。后端可通过以下配置复用客户端标识和代理出口：

- `user_agent` / `SUBSCRIPTION_USER_AGENT`：优先使用的上游请求 User-Agent。建议填写本地客户端实际标识，例如 `clash.meta`、`mihomo` 或 `clash-verge`。
- 登录后的“代理池设置”：可随时开启或关闭代理抓取，并按一行一个维护代理；界面设置保存到数据库后优先于启动配置。
- `proxy_enabled` / `SUBSCRIPTION_PROXY_ENABLED`：是否使用代理池。关闭时订阅抓取明确直连，不读取进程中的 `HTTP_PROXY`。
- `proxy_urls` / `SUBSCRIPTION_PROXY_URLS`：可选代理池。环境变量支持逗号、分号或换行分隔；每次抓取从轮询起点开始，失败时最多尝试 5 个出口。
- `proxy_url` / `SUBSCRIPTION_PROXY_URL`：兼容旧版的单代理配置；未显式设置新开关时，非空的旧配置会自动启用代理。
- `insecure_skip_verify` / `SUBSCRIPTION_INSECURE_SKIP_VERIFY`：仅供自签名上游使用，默认必须保持 `false`。

代理池支持以下写法；无协议前缀统一按 HTTP 代理处理，显式协议支持 `http`、`https`、`socks5` 和 `socks5h`：

```text
hostname:port:username:password
socks5://username:password@host:port
username:password@hostname:port
hostname:port@username:password
```

Docker Desktop 或配置了 `host-gateway` 的 Linux Docker 可使用宿主机 Clash HTTP 代理：

```text
SUBSCRIPTION_PROXY_ENABLED=true
SUBSCRIPTION_PROXY_URLS=http://host.docker.internal:7890
```

请先确认 Clash 已开启“允许局域网连接”，且代理端口与实际配置一致。如果本项目部署在远程服务器，`host.docker.internal` 指向远程服务器宿主机，无法访问个人电脑上的 Clash；此时需要在服务器侧配置可用代理或 VPN 出口。

### 3. 前端开发环境

```bash
cd ../frontend
pnpm install
pnpm dev
```

如果未安装 `pnpm`，也可以使用 `npm install` 与 `npm run dev`。前端开发服务默认运行在 `http://localhost:5173`，并通过 Vite 代理访问后端 `/api`。

---

## 📦 生产构建

项目根目录提供 Go 编写的一键构建脚本：

```bash
go run build.go
```

构建脚本会自动完成：

- 探测并使用 `pnpm`，未找到时回退到 `npm`
- 安装前端依赖
- 执行前端生产构建
- 编译后端二进制
- 将运行产物汇聚到根目录 `release` 目录
- 首次构建时从 `backend/config.example.toml` 生成 `release/config.toml`，再次构建不会覆盖已有配置

产物结构：

```text
release/
├── Clash Subscription Decoder         # Windows 下为 Clash Subscription Decoder.exe
└── config.toml         # 运行配置文件
```

运行时进入 `release` 目录启动二进制即可。请确保 `config.toml` 中的 PostgreSQL 连接信息正确。

---

## 📖 使用指南

1. **初始化管理员**
   从启动日志取得 `/<32 位十六进制哈希>` 控制台入口并访问。首次进入时系统会进入初始化模式；创建管理员账号后，再使用该账号登录。验证码可通过 `config.toml` 的 `[auth]` 配置开启或关闭。

2. **创建配置**
   可以创建两类配置：
   - **远程订阅**：填写上游订阅地址，刷新时后端会请求、解码并生成最终 Clash/Mihomo YAML。
   - **本地手动**：不依赖上游地址，直接在后台维护节点、策略组和规则。

3. **维护节点、策略组与规则**
   支持新增自定义节点、解析代理链接、编辑策略组、维护分流规则、批量保存规则、批量删除规则，以及把订阅中的节点或策略组接管为本地资源。

4. **排序与隐藏订阅资源**
   节点和策略组支持拖拽排序。订阅资源被删除时会记录为隐藏项，刷新订阅后不会自动恢复；接管后的资源则按本地自定义资源管理。

5. **跨配置复用**
   可以从其他配置复制代理组或规则，也可以把远程订阅中的规则本地化，便于在当前配置中继续精细维护。

6. **生成订阅地址**
   每个配置可重新生成独立 Token。弹窗中会提供：
   - Clash / Mihomo YAML：`/sub?token=xxx`
   - Surge 最新版：`/surge.conf?token=xxx`
   - Surge 5.7.6：`/surge-5.7.6.conf?token=xxx`
   - Shadowrocket 配置：`/shadowrocket.conf?token=xxx`
   - Shadowrocket 安装入口：`/shadowrocket/install?token=xxx`

7. **备份与导入**
   控制台支持导出当前数据备份，也支持导入备份 JSON。导入失败时后端会回滚数据库事务。

---

## 🐳 Docker Compose 部署

当前 `docker-compose.yml` 面向已有 PostgreSQL 与已有外部 Docker 网络的生产环境。使用前请按自己的环境修改：

- `DB_HOST`
- `DB_USER`
- `DB_PASSWORD`
- `DB_NAME`
- `DB_PORT`
- `DB_SSLMODE`
- `DB_TIMEZONE`
- `SERVER_PORT`
- `LOGIN_ENTRY_HASH`（可选，优先于 `[auth].login_entry_hash`）
- `BASE_IMAGE_REGISTRY`
- `networks.1panel-network.external`

### 中国大陆构建加速

Dockerfile 已为 APT、npm/pnpm 和 Go Modules 配置国内加速源。由于基础镜像必须在这些配置生效前拉取，项目额外提供 `BASE_IMAGE_REGISTRY` 构建参数，默认仍使用官方 `docker.io`。

可在项目根目录创建 `.env`，为当前项目指定可访问的 Docker Hub 镜像代理：

```dotenv
BASE_IMAGE_REGISTRY=docker.m.daocloud.io
```

然后重新拉取并构建：

```bash
docker compose build --pull app
docker compose up -d --force-recreate app
```

也可以保持 `BASE_IMAGE_REGISTRY=docker.io`，改为在 `/etc/docker/daemon.json` 中配置 Docker daemon 全局镜像加速：

```json
{
  "registry-mirrors": ["https://你的可信镜像加速地址"]
}
```

修改 daemon 配置后需要重启 Docker。公共第三方镜像代理仅作为网络兼容示例，生产环境应优先使用经过信任评估的企业镜像服务或自建镜像仓库。

启动命令：

```bash
docker compose up -d
```

常用管理命令：

```bash
docker compose ps
docker compose logs -f
docker compose down
docker compose up -d --build
```

注意：Compose 文件当前不会自动创建 PostgreSQL 容器，也不会自动创建外部网络。请先确保数据库和外部网络已可用，或按自己的部署环境调整 Compose 配置。`LOGIN_ENTRY_HASH` 留空时可从容器启动日志取得首次生成并持久化的入口；反向代理同样不得把未知路径回退到 `index.html`。

---

## 📄 许可证

本项目采用自定义 **Clash Subscription Decoder 禁止售卖许可 (No-Sale License)**。允许免费使用、复制、修改、分发和内部部署，但禁止直接售卖本项目，禁止售卖任何基于本项目继续开发、修改、重命名、换壳、打包或派生的版本，也禁止将其作为付费源码、付费安装包、付费 SaaS/托管服务的核心商品销售。完整条款参见 [LICENSE](LICENSE)。
