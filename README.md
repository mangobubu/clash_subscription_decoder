# ClashSubAST 订阅定制与安全动态分发平台

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.20%2B-00ADD8?style=flat-square&logo=go" alt="Go Version">
  <img src="https://img.shields.io/badge/Vue-3.x-4FC08D?style=flat-square&logo=vue.js" alt="Vue Version">
  <img src="https://img.shields.io/badge/Vite-5.x-646CFF?style=flat-square&logo=vite" alt="Vite Version">
  <img src="https://img.shields.io/badge/SQLite-3-003B57?style=flat-square&logo=sqlite" alt="SQLite Version">
  <img src="https://img.shields.io/badge/License-MIT-yellow?style=flat-square" alt="License">
</p>

`ClashSubAST` 是一个专为 Clash/Mihomo 客户端量身定制的**订阅解析、自定义合并与安全动态分发平台**。

传统的在线订阅转换服务存在巨大的隐私泄漏隐患，且无法持久化保存个性化配置。本项目旨在提供一个**私有化部署、安全可控、无损动态合成**的终极解决方案。

---

## 🌟 核心特性

- 🔒 **防泄漏 Token 鉴权**  
  订阅地址集成安全 Token 鉴权（包含生成账户、时间戳及强随机盐）。采用**唯一有效性机制**：每次生成新订阅链接时，旧 Token 立即永久作废，保障订阅绝对安全。
- ⚡ **无损动态合成 (AST 解析)**  
  当客户端请求订阅链接时，后端会**实时从上游刷新拉取最新节点**，随后与您保存在本地数据库中的“自定义节点”、“个性化策略组”及“自定义分流规则”进行无损动态融合，输出最终的 YAML 配置文件。
- 🛡️ **上游故障热备 (断连容灾)**  
  如果上游原始订阅服务器因网络波动暂时无法连接，系统将**自动退避使用最近一次成功解析的历史缓存**进行合成直出，确保您的代理软件始终能拉取配置，绝不断网。
- 🎨 **高颜值控制面板**  
  使用 **Vue 3 + Vite + Element Plus** 构建的现代化仪表盘。支持节点展示、代理和分流规则的可视化管理，集成一键生成并复制订阅链接的安全弹窗。

---

## 🏗️ 架构与工作流

本系统由前端管理后台与高并发 Go 后端组成，两部分通过 RESTful API 进行无缝通讯。

```mermaid
sequenceDiagram
    autonumber
    actor Client as Clash 客户端
    participant Server as Go 后端服务
    participant DB as SQLite 数据库
    participant Upper as 订阅上游服务

    Client->>Server: GET /sub?token=xxx (发起订阅更新)
    rect rgb(240, 248, 255)
        note over Server, DB: 安全拦截与校验
        Server->>DB: 验证 Token 并在数据库中匹配有效用户
        alt Token 校验失败
            Server-->>Client: 401 Unauthorized (拒绝访问)
        end
    end
    
    rect rgb(245, 255, 250)
        note over Server, Upper: 上游实时拉取与容灾
        Server->>Upper: 发起 HTTP 请求拉取最新订阅
        alt 拉取成功
            Upper-->>Server: 返回最新 Base64/YAML 节点
            Server->>DB: 缓存最新解析后的原始数据
        else 网络超时 / 节点异常
            Server->>DB: 读取最近一次成功缓存的数据 (热备)
        end
    end

    rect rgb(255, 250, 240)
        note over Server, DB: 配置无损合并 (AST)
        Server->>DB: 读取用户的自定义节点、策略组和过滤规则
        Server->>Server: 将自定义配置动态注入到订阅节点列表中
        Server->>Server: 合成并输出最终的 Clash YAML 字符串
    end
    
    Server-->>Client: 200 OK (直出 YAML 配置，Content-Type: text/yaml)
```

---

## 🛠️ 技术栈

- **前端 (Frontend)**:
  - 核心框架: Vue 3 (Composition API)
  - 构建工具: Vite
  - 语言: TypeScript
  - 状态管理: Pinia
  - UI 库: Element Plus
  - 网络请求: Axios

- **后端 (Backend)**:
  - 开发语言: Go 1.20+
  - Web 框架: Gin
  - 数据库 ORM: GORM (SQLite 3 驱动)
  - 热重载工具: Air

---

## 🚀 快速上手

### 1. 克隆项目与环境准备
确保您的本地已安装 [Node.js (16+)](https://nodejs.org/) 和 [Go (1.20+)](https://go.dev/) 环境。

```bash
git clone https://github.com/mangobubu/clash_proxy.git
cd clash_proxy
```

### 2. 后端服务部署
后端数据库使用 SQLite 3，无需安装独立的数据库软件，启动时会自动在 `backend` 目录下创建 `gorm.db`。

```bash
cd backend
# 下载依赖
go mod tidy

# 方式 A：开发环境下使用 Air 实时重载运行
# （需先全局安装 air: go install github.com/air-verse/air@latest）
air

# 方式 B：直接用 Go 编译或运行
go run main.go
```
后端服务默认监听端口：`http://localhost:8080`

### 3. 前端服务部署
```bash
cd ../frontend

# 安装 pnpm (如已安装可跳过)
npm install -g pnpm

# 安装依赖
pnpm install

# 运行前端开发服务器
pnpm dev
```
前端服务默认运行于：`http://localhost:5173`。前端已配置了 Vite 反向代理，会自动将 `/api` 的请求转发至后端的 `http://localhost:8080/api`。

### 4. 生产环境构建与一键部署

本系统支持**一站式全栈一键打包部署**。为了给您提供最极致、极简的运维体验，项目内置了一个纯 Go 编写的跨平台自动化构建系统。构建完成后，所有的运行资产（含完全嵌入前端的独立二进制文件与初始配置文件）都会被自动汇聚到根目录下的 **`release`** 统一产物目录中。

#### 步骤一：一键全栈构建
您无需单独管理前端和后端的编译，也无需手动拷贝任何配置文件。只需在项目**根目录**下运行以下一行命令：
```bash
go run build.go
```
构建系统会全自动检测您的本地包管理器（优先使用 `pnpm`，若无则自动降级回退至 `npm`），自动拉取前端依赖、进行前端生产打包，并将后端编译与所有资产汇聚至 `release` 中。

#### 步骤二：运行全栈服务
构建成功后，您仅需将项目根目录下的 **`release`** 目录整体拷走即可部署。它的结构如下：
```text
release/
├── ClashSubAST         # 内置了全量前端的独立全栈二进制程序 (Windows 下为 ClashSubAST.exe)
├── config.toml         # 自动生成的初始运行配置文件 (自动保护，二次构建不会覆盖您的修改)
└── config.example.toml # 配置模板备份
```
进入该目录并直接启动程序即可拉起完整服务：
```bash
cd release
# 运行全栈服务 (Windows 环境下文件名为 ./ClashSubAST.exe)
./ClashSubAST
```
运行后，访问 `http://localhost:8080` 即可直接开始使用完整的前端管理界面与后端 API 服务，真正实现“拷走即用”！

---

## 📖 使用指南

1. **注册与登录**  
   首次使用时，在登录页面输入用户名和密码即可自动完成注册并登录。
2. **导入原始订阅**  
   在后台中配置您的上游订阅地址，系统将自动对其进行拉取和解码，并展示其中的节点列表。
3. **定制个性化配置**  
   - 可在界面中添加专属的**自定义节点**。
   - 配置您喜好的**分流规则**（如：直连、全局代理、中国IP直连、广告拦截等）。
4. **生成安全订阅**  
   - 在主界面点击亮眼的 **[生成最终订阅]** 按钮。
   - 系统将弹窗显示一个附带专属 `token` 参数的安全链接（例如 `http://localhost:8080/sub?token=xxx`）。
   - 点击 **[一键复制]** 并粘贴到您的代理客户端（如 Clash Verge, Mihomo Party, Clash for Windows 等）中即可。
   - **注意**：如果未来点击了“重新生成”，旧的链接会当即失效，防止订阅链接泄露后的不可控风险。

---

## 🐳 Docker & Docker Compose 极速部署指南

如果您希望在生产环境中利用 Docker 进行轻量化一键部署与管理，项目已内置了高水准的容器化支持：
- **多阶段构建 Dockerfile**：编译环境与生产运行环境物理隔离，最终生成的生产镜像仅约为 50MB。
- **云原生一键容器编排**：内置了隔离的自定义 Docker 网络组，且全量支持通过环境变量配置外部数据库与连接参数，零挂载实现秒速启动。

### 1. 快速一键拉起
在安装了 Docker 及 Docker Compose 的服务器上，直接配置好 `docker-compose.yml` 中的环境变量后，仅需运行以下一行命令即可拉起服务：

```bash
# 一键在后台启动 ClashSubAST 独立服务 (自动与隔离的网络组及外部 PostgreSQL 数据库建立连接)
docker compose up -d
```

### 2. 容器服务生命周期管理
```bash
# 查看所有容器服务的运行状态、端口以及健康度 (自动进行数据库就绪检测)
docker compose ps

# 实时追踪并查看容器的运行日志输出
docker compose logs -f

# 优雅停止并销毁容器集群 (数据已通过数据卷持久化保存，数据绝对安全)
docker compose down
```

### 3. 数据备份与配置文件维护
* **配置文件更新**：您可以直接在宿主机修改根目录下的 `config.toml` 文件，修改后运行 `docker compose restart app` 重启应用容器即可使最新配置生效。
* **数据库持久化**：数据库的数据会被安全地保存在 Docker Named Volume `clash-sub-ast-pgdata` 中，即使容器群被完全删除重建，您的数据依然完好无损。

---

## 📄 开源协议

本项目基于 **MIT License** 开源。详情参见 [LICENSE](LICENSE) 文件。
