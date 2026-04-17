# 马赫环

马赫环是一个面向静态资源托管的单体应用，当前版本已经支持图片、JavaScript、CSS、ZIP、EXE、文档、字体、视频等资源的基础上传、管理、外链访问和策略控制。

基础方向见 [DEVELOPMENT_PREP.md](./DEVELOPMENT_PREP.md)，分块开发计划见 [PROJECT_PLAN.md](./PROJECT_PLAN.md)。

## 当前已支持

- Go 后端 API，默认监听 `:8080`
- Svelte + Vite 前端开发界面，默认监听 `:5173`
- SQLite 持久化，默认数据库文件为 `data/machring.db`
- 本机文件系统存储，默认上传目录为 `data/uploads`
- 基于资源类型、扩展名、用户组的默认策略判断
- 资源上传、资源列表、资源详情、软删除、恢复
- `/r/{id}` 直链访问、访问次数与流量累计
- 上传安全增强：文件名清理、扩展名与 MIME 联合校验、高风险资源强制下载、上传与登录失败限流
- 私有资源、签名链接、后台资源详情页可见性切换与签名链接复制
- 安装初始化、管理员登录、当前用户接口、退出登录
- 策略组列表、复制、启用、停用，以及按组规则编辑
- 策略矩阵编辑、扩展名覆盖规则、字段级校验与高级 JSON 入口
- 策略测试接口与命中策略组展示
- 用户组配额、用户管理、站点设置、精选资源广场
- S3 / WebDAV 存储配置、健康检查与资源访问
- 旧 `data/machring.json` 数据首次启动时导入 SQLite

## 当前未完成

- 前端嵌入 Go 单二进制
- 发布前质量门槛与部署文档补齐

当前运行时配置以 `MACHRING_*` 环境变量为准，示例见 [`.env.example`](./.env.example)。仓库里的 [`config.example.yaml`](./config.example.yaml) 目前还没有接入启动流程。

## 开发环境

- Go 1.24.x
- Node.js 20+
- npm 10+

## 启动后端

默认直接启动：

```powershell
go run ./cmd/machring
```

常用环境变量可在启动前设置：

```powershell
$env:MACHRING_HTTP_ADDR=":8080"
$env:MACHRING_PUBLIC_BASE_URL="http://localhost:8080"
$env:MACHRING_SITE_NAME="马赫环"
go run ./cmd/machring
```

默认数据目录结构：

- 数据库：`data/machring.db`
- 上传目录：`data/uploads`
- 临时目录：`data/tmp`

首次启动后，请访问 `/install` 创建管理员账号。管理员密码会以安全哈希形式写入数据库，不再通过环境变量保存默认密码。

## Docker 部署

仓库根目录已经提供：

- `Dockerfile`：同一个多阶段文件生成后端和前端镜像
- `docker-compose.yaml`：启动 `backend` 和 `frontend` 两个服务
- `docker/nginx.conf`：前端静态站点与 `/api`、`/assets`、`/r` 反向代理
- `.env.docker.example`：Docker Compose 环境变量示例

推荐步骤：

```powershell
Copy-Item .env.docker.example .env
docker compose up -d --build
```

默认访问地址：

- 前端入口：`http://localhost:8080`
- 后端健康检查：`http://localhost:8080/api/health`

Compose 已把宿主机 `./data` 挂载到容器内 `/var/lib/machring`，因此以下数据会持久化保留：

- 数据库：`./data/machring.db`
- 上传目录：`./data/uploads`
- 临时目录：`./data/tmp`

停止和重启：

```powershell
docker compose stop
docker compose start
```

清理容器但保留数据：

```powershell
docker compose down
```

## 启动前端

首次安装依赖：

```powershell
cd frontend
npm install
```

开发模式：

```powershell
cd frontend
npm run dev
```

Vite 默认把 `/api` 和 `/assets` 代理到 `http://localhost:8080`。

## 手动验证流程

1. 启动后端：`go run ./cmd/machring`
2. 启动前端：`cd frontend && npm run dev`
3. 打开 `http://localhost:5173`
4. 首次启动先访问 `/install` 创建管理员账号
5. 访问 `/login` 登录后台
6. 访问 `/upload` 上传一个图片或其他静态资源
7. 访问 `/admin/policies` 测试策略或保存策略
8. 访问 `/admin/resources` 查看资源、删除资源、恢复资源

## 质量检查

后续每次开发完成至少执行：

```powershell
go test ./...
```

```powershell
cd frontend
npm run build
```

## 默认策略示例

- 游客可以上传 `jpg/jpeg/png/gif/webp`，单资源月流量默认 1 GB
- 登录用户可以上传图片类资源，单资源月流量默认 10 GB
- 游客不能上传 `zip`
- 普通用户可以上传 `zip`，默认强制下载
- 普通用户默认不能上传 `exe`，管理员可以上传
