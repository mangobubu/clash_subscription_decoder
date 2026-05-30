# Clash Proxy - Subscription Customization & Secure Dynamic Distribution Platform

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.20%2B-00ADD8?style=flat-square&logo=go" alt="Go Version">
  <img src="https://img.shields.io/badge/Vue-3.x-4FC08D?style=flat-square&logo=vue.js" alt="Vue Version">
  <img src="https://img.shields.io/badge/Vite-5.x-646CFF?style=flat-square&logo=vite" alt="Vite Version">
  <img src="https://img.shields.io/badge/SQLite-3-003B57?style=flat-square&logo=sqlite" alt="SQLite Version">
  <img src="https://img.shields.io/badge/License-MIT-yellow?style=flat-square" alt="License">
</p>

`clash-proxy` is a specialized **subscription parsing, custom merging, and secure dynamic distribution platform** designed specifically for Clash/Mihomo clients.

Traditional online subscription conversion services pose severe privacy leakage risks and lack the capability to save personalized configurations persistently. This project aims to offer a **privately deployed, secure, and losslessly dynamically synthesized** ultimate solution.

---

## 🌟 Core Features

- 🔒 **Privacy-Preserving Token Authentication**  
  The generated subscription link integrates secure Token authentication (consisting of the generation user, timestamp, and a cryptographically strong random salt). Adheres to the **Sole-Validity Mechanism**: whenever a new subscription link is generated, the older Token is permanently revoked immediately, protecting your subscription from unauthorized access.
- ⚡ **Lossless Dynamic Synthesis (AST Parsing)**  
  When a client requests a subscription link, the backend **refreshes and fetches the upstream subscription in real time**. It then seamlessly merges these nodes with your custom nodes, strategic groups, and personalized rules stored in the local database to compose and output the final Clash YAML config.
- 🛡️ **Upstream Failover & Disaster Recovery**  
  If the upstream subscription server is temporarily unreachable due to network fluctuations, the system **automatically falls back to using the last successfully cached historical subscription** to complete the synthesis, ensuring that your proxy software never loses internet connectivity.
- 🎨 **Beautiful Web Control Panel**  
  A modern, elegant dashboard built using **Vue 3 + Vite + Element Plus**. Supports visual management of nodes, proxies, and custom split-routing rules, integrated with a high-fidelity popup for one-click secure subscription link generation and copying.

---

## 🏗️ Architecture & Workflow

The system is composed of an administration frontend and a highly concurrent Go backend communicating seamlessly via RESTful APIs.

```mermaid
sequenceDiagram
    autonumber
    actor Client as Clash Client
    participant Server as Go Backend Service
    participant DB as SQLite Database
    participant Upper as Upstream Provider

    Client->>Server: GET /sub?token=xxx (Trigger Subscription Update)
    rect rgb(240, 248, 255)
        note over Server, DB: Safety Interception & Verification
        Server->>DB: Validate Token & Find Corresponding User
        alt Token Verification Failed
            Server-->>Client: 401 Unauthorized (Deny Access)
        end
    end
    
    rect rgb(245, 255, 250)
        note over Server, Upper: Real-time Fetch & Failover
        Server->>Upper: HTTP request to pull latest subscription
        alt Pull Successful
            Upper-->>Server: Return latest Base64/YAML nodes
            Server->>DB: Cache decoded upstream data locally
        else Network Timeout / Connection Failed
            Server->>DB: Fetch last successfully cached upstream data (Failover)
        end
    end

    rect rgb(255, 250, 240)
        note over Server, DB: Lossless Config Merge (AST)
        Server->>DB: Read custom nodes, policy groups & routing rules
        Server->>Server: Dynamically inject custom configs into node lists
        Server->>Server: Synthesize and output the final Clash YAML
    end
    
    Server-->>Client: 200 OK (Render YAML Configuration, Content-Type: text/yaml)
```

---

## 🛠️ Technology Stack

- **Frontend**:
  - Core Framework: Vue 3 (Composition API)
  - Build Tool: Vite
  - Programming Language: TypeScript
  - State Management: Pinia
  - UI Library: Element Plus
  - HTTP Client: Axios

- **Backend**:
  - Programming Language: Go 1.20+
  - Web Framework: Gin
  - Database ORM: GORM (SQLite 3 Driver)
  - Live Reload Tool: Air

---

## 🚀 Quick Start

### 1. Clone Project & Prerequisites
Ensure that [Node.js (16+)](https://nodejs.org/) and [Go (1.20+)](https://go.dev/) are installed on your machine.

```bash
git clone https://github.com/mangobubu/clash_proxy.git
cd clash_proxy
```

### 2. Backend Service Setup
The backend utilizes SQLite 3 for storage. No dedicated database installation is required; `gorm.db` will be auto-generated in the `backend` directory upon startup.

```bash
cd backend
# Download and tidy up dependencies
go mod tidy

# Method A: Live Reload with Air in Development
# (Ensure Air is installed: go install github.com/air-verse/air@latest)
air

# Method B: Direct Compilation or Run
go run main.go
```
The backend server listens on `http://localhost:8080` by default.

### 3. Frontend Service Setup
```bash
cd ../frontend

# Install pnpm if not already installed
npm install -g pnpm

# Install dependencies
pnpm install

# Run Frontend Development Server
pnpm dev
```
The frontend application will be served at `http://localhost:5173`. The local development setup is pre-configured with a Vite reverse proxy to forward `/api` requests to backend APIs at `http://localhost:8080/api`.

### 4. Production Build & One-File Deployment

The system supports **single-file full-stack deployment**. When compiling the Go backend, all frontend static assets are statically embedded into the executable binary. In your production environment, you only need to run the single compiled binary.

#### Step 1: Build Frontend
Run the build command in the frontend directory. The compiled static resources will be automatically exported directly to the backend embedding folder `backend/dist`:
```bash
cd frontend
pnpm build
```

#### Step 2: Compile & Run Backend
Compile the Go project in the backend directory. This will merge the compiled frontend assets together with the Go application code into the final executable:
```bash
cd backend
# Compile into an executable binary with embedded frontend (Windows will generate clash-proxy.exe automatically)
go build -o clash-proxy

# Run the full-stack server
./clash-proxy
```
Once started, visit `http://localhost:8080` in your browser to access both the administrative web interface and the background APIs instantly. No need to deploy frontend assets separately via Nginx or other web servers!

---

## 📖 User Guide

1. **Sign Up & Sign In**  
   On your first visit to the admin page, type your desired username and password to automatically register and log in.
2. **Import Upstream Subscriptions**  
   Set up your primary proxy subscription URL in the dashboard. The system will automatically fetch, decode, and visualize all proxy nodes.
3. **Customize Configurations**  
   - Add your own **custom proxy nodes** manually via the interface.
   - Configure your preferred **filtering rules** (e.g., Direct, Global Proxy, China Direct, AdBlock, etc.).
4. **Generate Secure Subscription Links**  
   - Click the prominent **[Generate Subscription]** button.
   - A modal will emerge containing your unique subscription URL with a secure `token` query parameter (e.g., `http://localhost:8080/sub?token=xxx`).
   - Click **[Copy Link]** and paste it directly into your proxy clients (e.g., Clash Verge, Mihomo Party, Clash for Windows, etc.).
   - **Note**: Regenerating the link will immediately invalidate the previous Token, nullifying external access if your link was accidentally leaked.

---

## 📄 License

This project is licensed under the **MIT License**. See the [LICENSE](LICENSE) file for more details.
