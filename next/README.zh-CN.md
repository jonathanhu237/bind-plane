# Bind Plane · Temvia 迁移预览

释放流程和设备运维已经用模拟的 Telnet 交换机完成端到端验证，但尚未在真实交换机上验证：正式依赖之前请先在实验交换机上确认。Docker 部署方式见 [deploy/README.md](deploy/README.md)。明确切换前保留旧应用；不要配置生产交换机。

安全边界：预查询发现动态记录并不代表静态绑定，不允许执行释放。释放命令的租约过期后，任务变为 `needs_manual_confirmation`；允许另起查询记录，但同一 IP 的后续释放命令被阻止。系统不会自动重发，也暂不提供站内解除阻断功能。日后人工核实时，管理员必须先停止原 worker、确认原交换机会话已关闭，再检查独立任务证据与交换机现状，通过单独留痕的方式处理。切勿直接将状态改回队列。

## 设备运维

只有管理员（不包括操作员）可以使用以下功能：

- **设备**：交换机清单，包括厂商、型号、分组、站点、登录协议（SSH 或 Telnet）和端口；CSV 批量导入，先完整模拟一次再写入，也接受其他系统导出的中文表头；逐台显示连通性（对管理端口做 TCP 连接）和备份状态。
- **配置备份**：worker 登录设备读取运行配置，只有配置变化时才保存新版本（忽略时间戳之类的易变行）。配置用凭据密钥加独立标签以 AES-GCM 加密，只有 API 能解密，每次查看或对比都记入审计日志。
- **计划任务**：按 cron（时区由 `BIND_PLANE_TIMEZONE` 指定，默认 Asia/Shanghai）对全部设备或某个分组执行备份或连通性检测，也可以在设备页手动发起。同一台设备不会同时有两个同类待执行操作。执行记录保留 30 天。
- **合规检查**：基线规则（必须出现 / 不得出现，RE2 正则，可按厂商或分组限定范围），每当备份或规则变化，就用每台设备的最新配置重新评估。RE2 的匹配时间与输入长度成线性关系，任何规则都不会拖垮评估。
- **告警**：设备不可达、备份失败和严重级别的不合规。问题消失后自动解决；确认表示已有人处理。已解决的告警保留 180 天。
- **审计日志**：所有资产变更、导入、计划和规则调整、手动任务、配置查看与告警确认，永久保留。

设备运维只读取交换机，worker 中途退出时该项记为失败，下一次执行会重试。释放任务有独立的 worker 循环，不会被长时间的备份拖慢。

资产修改带版本号。任务会固定其预查询时交换机与命令模板的版本：若二者之一已被修改，或自动解析的地址已改由其他交换机负责，确认释放会被拒绝；排队中的任务若固定版本已失效，会直接失败且不连接交换机，操作员需重新查询。在连接交换机之前发生的失败（资产被停用或修改、凭据或模板不可用）一律记为 `failed`，不会记为 `needs_manual_confirmation`，因此不会阻断后续释放。资产变更记录会列出修改了哪些字段，密码类字段只记录“已更换”或“已删除”。

[English](README.md) | 简体中文

独立的 Go API 与 React 管理后台。开发使用 Vite，生产 Compose 栈使用 Caddy。
这些源码可由你自行修改或删除。

## 前置条件与验证范围

Compose 主路径需要带 Compose v2 的 Docker、Make、Node.js 24 或更新版本
（用于生成器及跨平台密钥生成）。先启动 Docker 引擎。
macOS 缺少 Make 时安装 Xcode Command Line Tools。
发布流程会在 Ubuntu runner 上使用全新 Compose 项目、PostgreSQL 数据卷和 Chromium
执行一次这条路径。这只是一路 CI 验证，不是对所有 Linux／WSL2 或生产环境兼容性的承诺。
本地完整首次使用流程已在 macOS 测试；原生 Windows PowerShell 不属于这条使用路径。
容器外开发 API 需要 Go 1.27 或更新版本，开发前端需要 pnpm 11.24.0。

## 首次启动

在生成项目根目录复制环境变量示例，填写全部五项秘密配置：
`POSTGRES_PASSWORD`、`PASSWORD_RESET_TOKEN_KEY`、`INVITATION_TOKEN_KEY`、
`EMAIL_SETTINGS_ENCRYPTION_KEY`、`BIND_PLANE_CREDENTIAL_ENCRYPTION_KEY`。

```sh
cp .env.example .env
chmod 600 .env
node -e "console.log(require('node:crypto').randomBytes(32).toString('base64url'))"
```

重复执行密钥生成命令，为每个值使用不同的输出，将结果填入 `.env`，不要提交或共享该文件。
后三项密钥必须是 32 个随机字节的不带填充 Base64URL 编码；上述命令也适用于 PostgreSQL 密码。
确认配置完成后：

```sh
make build
make migrate-up
make up
docker compose logs api
```

在较慢主机上，全新的 PostgreSQL 数据卷初始化可能需要一分钟以上。生成的 Compose
为真实的 `pg_isready` 健康检查提供有界的 90 秒启动宽限期，随后仍按 2 秒间隔、
5 秒单次超时和 15 次失败上限检查。`make migrate-up` 会等待就绪；如果 PostgreSQL
始终不健康就会失败，不会关闭持久性或返回假成功。

系统未初始化时，API 日志会输出临时初始化链接。在浏览器打开它并创建管理员，
随后明确登录；初始化成功不会自动创建登录会话。链接中的令牌在页面渲染前从
URL fragment 移除，仅在初始化请求正文中发送。链接过期时重启 API 并查看新日志。
不要分享包含初始化令牌的日志。

默认访问 `http://localhost:5173`。浏览器通过相对 `/api` 路径访问 API，
由 Vite 或 Caddy 代理，因此 `APP_PUBLIC_URL` 必须与地址栏 origin 完全一致。
端口冲突时在 `.env` 调整对应端口；修改 `ADMIN_PORT` 时同步修改 `APP_PUBLIC_URL`。
更改环境变量后用 `docker compose up -d api` 重建 API 容器；单纯 restart 不会载入新值。

PostgreSQL 保存账号、会话和限流状态。API 或数据库重启后，仍有效的会话继续保留；
请求会立即检查过期与撤销状态，后台任务只负责分批回收状态。`make down` 不删除
PostgreSQL 数据卷。除非明确要删除数据库，不要执行 `docker compose down -v`。

API 默认启用不依赖 Redis 的基本防滥用能力。登录分别按来源 IP、规范化邮箱和全局
额度计数；密码找回请求使用独立的来源 IP、邮箱和全局额度。完成密码重置、接受邀请和
首次设置各自使用独立来源 IP 额度。创建／重发邀请共享操作者和收件人额度，测试邮件
使用独立的全局、操作者和收件人额度。已到达的匿名请求会消耗来源额度；对象或全局
额度拒绝不会额外消耗已接受操作的额度。登录成功只清除该邮箱的登录额度。
所有额度自动补充并在有限时间后回收；重复被拒绝不会不断顺延令牌恢复时间。真实额度
不足返回 HTTP 429（`rate_limited`）；限流依赖不可用或超时返回 HTTP 503
（`dependency_unavailable`），并且不会清除现有会话 Cookie。这是单个 API 实例的基本
防护，不是 DDoS 或 SMTP 供应商配额防护。

`.env.example` 列出了每类额度的 `*_RATE_LIMIT_*` 容量和补充间隔。容量必须为正数，
间隔至少为 1ms，计算出的保留时间必须能用数据库表示；非法配置会在启动时拒绝。
`TRUSTED_PROXY_CIDRS` 只能填写直接入口代理的网络；为空时使用直连地址，非法或不可信
的转发头不能改变来源身份。启动 API 前请先执行包含防滥用前向迁移在内的全部迁移。
公网部署还应在 API 前放置维护中的边缘代理或 WAF，处理连接、请求体和全局流量防护；
这属于额外建议，不是应用额度生效的前提。

内置的小型管理后台默认策略如下（容量为突发令牌数，间隔为补充一个令牌的时间）：

| 操作 | 额度（容量；补充间隔；持续补充速率） |
| --- | --- |
| 登录 | IP 30；6s；10/分钟 · 邮箱 5；1m；1/分钟 · 全局 60；6s；10/分钟 |
| 密码找回请求 | IP 30；6s；10/分钟 · 邮箱 3；20m；3/小时 · 全局 10；6s；10/分钟 |
| 完成密码重置 | IP 20；1m；1/分钟 |
| 接受邀请 | IP 30；1m；1/分钟 |
| 首次设置 | IP 10；1m；1/分钟 |
| 创建／重发邀请 | 操作者 20；1m；1/分钟 · 收件人 3；20m；3/小时 |
| 测试邮件 | 全局 20；1h；1/小时 · 操作者 5；1h；1/小时 · 收件人 3；20m；3/小时 |

补充是连续按离散间隔进行的，额度不会超过容量；这些数字不是固定时间窗口的请求承诺。
修改容量后，下一次请求会裁剪历史上多出的令牌；修改间隔后，后续请求按新的恢复和有限保留规则计算。

`SHUTDOWN_TIMEOUT` 是唯一的优雅停机预算，默认 30s。API 与 Compose 的
`stop_grace_period` 读取同一个值；预算从首次 SIGINT/SIGTERM 开始，并包含内部退出预留。
API 会停止接受新的 HTTP 请求，并行排空在途请求和已领取邮件，取消后台维护，只有这些任务
结束后才关闭数据库。预算耗尽或第二次终止信号会以非零状态退出；非法或非正值会在启动时拒绝。

## 配置与邮件

`.env.example` 是完整环境变量清单：包括浏览器 origin、服务地址与映射端口、
数据库连接池、会话过期时间、登录及密码找回限流、邀请与重置链接时效、
邮件派送超时和重试设置。Compose 读取 `.env`；Go API 仅读取进程环境变量，
不会自行解析 `.env`。在容器外运行时需要将这些值导入进程，并将数据库
地址改为宿主机可达的地址。

`make up` 启用仅供开发的 Mailpit。界面为 `http://127.0.0.1:8025`，
可以通过 `MAILPIT_UI_PORT` 修改。SMTP 仅在 Compose 网络内监听 `mailpit:1025`。
登录后在系统设置配置 SMTP：主机 `mailpit`、端口 `1025`、安全模式 `none`、
不填凭据并设置测试发件人。启动容器本身不会自动保存这些邮件设置。

生产环境使用真实 SMTP 服务，在系统设置填写 `starttls` 或 `tls`、真实发件地址
和服务要求的配套凭据。系统设置中的系统名称为必填且不区分语言；无论界面语言或邮件语言
如何切换，页面、浏览器标题和系统邮件都使用同一个名称。系统设置也可配置默认邮件语言与图标。

密码找回通过 PostgreSQL transactional outbox 异步发送。请求只提交状态，进程内派送器
随后领取任务并发送 SMTP，没有额外消息队列或 worker 服务。临时故障持久重试，
过期任务和旧记录会自动清理。SMTP 接收后进程中断可能产生重复邮件；系统使用稳定
Message-ID 和一次性重置权限处理至少一次投递。

稳定保存并单独备份 `EMAIL_SETTINGS_ENCRYPTION_KEY`，它加密数据库中的 SMTP 密码。
`PASSWORD_RESET_TOKEN_KEY` 和 `INVITATION_TOKEN_KEY` 也应跨重启保持稳定且互不共用。
主动轮换重置密钥会使尚未投递的旧密钥任务失效；已投递链接仍按数据库中的摘要验证，
受影响用户可以重新申请。邀请链接默认 72 小时，可用 `INVITATION_LINK_TTL` 配置，最长七天。

## 开发

### API

在准备好进程环境变量、可访问的 PostgreSQL 并完成迁移后：

```sh
cd api
go run ./cmd/server
```

`GET http://127.0.0.1:8080/health` 返回 `{"status":"ok"}`。
`HTTP_ADDR` 可修改监听地址；配置缺省时仅监听本机，Compose 示例则显式监听容器所有接口。
不要让宿主机 API 与 Compose API 使用相同宿主机端口。

```sh
go test ./...
go vet ./...
go build -o bin/server ./cmd/server
go test -bench='Benchmark(Hasher|Verifier)$' -benchtime=1x ./internal/auth/adapter/password
```

发布应用前在部署目标评估 Argon2id 哈希成本。环境变量不会因执行这些命令自动加载。

### 管理前端

从项目根目录停止 Compose 前端，让开发服务器使用该端口，然后在另一终端运行：

```sh
docker compose stop admin
cd admin
pnpm install --ignore-scripts
pnpm dev
```

打开 Vite 实际打印的 URL，默认 `http://127.0.0.1:5173`；端口占用时 Vite 会尝试下一端口。
首次安装生成前端自己的锁文件，应纳入版本控制。API 的 `APP_PUBLIC_URL` 必须等于
Vite 打印的 origin（包括 localhost 与 127.0.0.1 的差异和端口）。修改根 `.env` 后
在项目根目录执行 `docker compose up -d api`，如需初始化则从日志读取新链接。
Vite 代理从根 `.env` 读取 `API_PORT`。

```sh
pnpm lint
pnpm check
pnpm test
pnpm build
pnpm preview --host 127.0.0.1
```

lint 使用 Oxlint，check 使用 TypeScript，test 运行单元及组件测试。
preview 只检查本地构建，生产由 Caddy 提供 `/api` 代理与 SPA 路由回退。
各应用独立管理依赖和配置，没有根 workspace；生成器不安装依赖。

## 生产部署与升级

使用 `APP_ENV=production`，`APP_PUBLIC_URL` 设置为公开 HTTPS origin。
没有 TLS 的网络中，只有设置 `APP_ALLOW_PLAIN_HTTP=true` 才接受 `http://` origin，
API 启动时会警告：密码和会话 Cookie 将以明文在网络中传输。
通过外部 ingress 或自定义 Caddy 提供 TLS；默认 Compose 网关只绑定本机 HTTP，
不自动配置公网 DNS 或 TLS。后端和数据库端口保持私有。
Nginx 可替代 Caddy，但必须保留相同的 API 代理及 SPA 回退行为。

```sh
make build
make migrate-up
docker compose up -d api admin
```

生产不要使用 `make up`，它会启用开发 Mailpit。使用真实 SMTP。
升级应用时请按停机和备份顺序操作。API 停止后保持 PostgreSQL 运行，
每一步命令成功完成后再执行下一步：

```sh
(
set -eu
umask 077

# 1. 在更换镜像或数据库结构前停止 API。
docker compose stop api

# 2. 在项目之外创建私有持久备份目录，不使用 /tmp。
backup_dir="$(mktemp -d "${HOME:?}/temvia-backup.XXXXXX")"
chmod 700 "$backup_dir"
backup_file="$backup_dir/temvia-$(date -u +%Y%m%dT%H%M%SZ).sql"
docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB"' > "$backup_file.partial"
chmod 600 "$backup_file.partial"
mv "$backup_file.partial" "$backup_file"
printf '备份已写入 %s\n' "$backup_file"

# 3. 从更新后的源码重新构建 API、后台和迁移镜像。
make build

# 4. 显式执行全部正向迁移；失败时子 shell 立即停止。
make migrate-up

# 5. 只有迁移成功后才启动已重新构建的服务。
docker compose up -d api admin
)
```

子 shell 在任一步失败后停止，不会关闭当前交互终端。
带 `.partial` 后缀的文件表示备份失败，不能用于恢复。
成功的备份应保留在持久存储中，并复制到受保护的异机备份位置；示例不自动管理备份保留期。

升级前请将 `.env`、`EMAIL_SETTINGS_ENCRYPTION_KEY`、
`PASSWORD_RESET_TOKEN_KEY` 和 `INVITATION_TOKEN_KEY` 单独备份到受保护的密钥存储，
不要提交这些值，也不要把它们放入公开备份。构建或迁移失败时必须停止流程，
先检查错误，并根据回滚计划恢复数据库及匹配的镜像，不要启动与数据库结构不兼容的 API。
不要执行 `docker compose down -v`，PostgreSQL 数据卷中保存着应用数据。

回滚前检查受影响的每项迁移并匹配 API 版本，不要把执行一次 down 当成通用回滚方案。
数据库与加密／签名密钥分别备份。

## 账号、权限和邀请

首位管理员获得不可修改的 Super Admin 内置角色。
“用户与权限”包含用户、邀请和角色；权限按资源分别授予读取和写入，写入不隐含读取。
角色编辑器和 API 验证跨功能所需权限组合。委派管理员只能管理其有效权限范围内的邀请角色。
自定义角色至少一个权限，用户与邀请至少一个角色；已分配角色不可删除，
系统拒绝使可用 Super Admin 数量降为零的修改。

邀请与已激活用户分开。一次性链接打开 `/accept-invitation#token=...`，
页面渲染前移除 fragment，接受邀请原子创建账号和角色分配后需要明确登录。
密码找回请求对已知和未知邮箱都返回相同 202；重置成功使旧会话失效并要求重新登录。

## 用户停用、恢复与删除

用户列表默认包含正常和停用用户，可按状态筛选。沿用用户读写权限：读取查看用户，写入可
停用、恢复或删除任意其他用户，包括 Super Admin；角色授予仍遵守原有范围限制。
禁止停用或删除本人，停用、删除及角色修改都必须保留至少一位未停用的 Super Admin。
这些约束在服务端事务中执行，并检查用户版本；并发冲突后应刷新用户再确认。

停用立即使所有设备的会话失效，禁止登录及密码找回，但保留邮箱、密码和角色。管理员仍可
调整角色，停用用户的角色分配仍算使用中。恢复后需重新登录；原会话和旧密码重置链接不会
恢复有效。已登录页面检测到停用后提示联系管理员，匿名登录和找回入口不会泄露停用状态。

删除需要逐个确认并输入目标邮箱，不可恢复，不必先停用。账号、凭据、会话、重置权限和
角色分配被清理；操作历史及关联记录保留原姓名、邮箱和用户标识，并标记“已删除用户”。
历史继续按保留期限清理，最小历史身份记录不含登录凭据。删除账号不等于彻底清除个人信息。
同一邮箱可重新邀请，但新账号使用新身份，不继承原密码、角色或历史归属。

创建者停用或删除不撤销其发出的邀请，邀请仍按原时效和权限规则接受或管理。
新增业务模块应明确自己的关联保留规则，不要对业务记录默认级联删除。

执行全部迁移后再启动新版 API。生命周期迁移的回退在有停用用户或已删除身份时会拒绝，
避免恢复访问或丢失历史；请按实际数据制定回滚方案，不要清空数据绕过限制。

## 操作历史

具有 `operation-logs.read` 权限的管理员从“系统监控 → 操作历史”进入。
支持时间、操作者 UUID、动作、结果、对象类型及 ID 筛选、游标分页和本地化详情。
范围内操作成功或失败各生成一条尽力记录，存储故障可能产生缺口。
默认保留 180 天，具有设置写入权限者可配置
1–3650 天，清理每小时运行且限制单次工作量。

来源 IP 默认记录直接连接地址。反向代理部署通过 `TRUSTED_PROXY_CIDRS` 配置
可信的直接代理网络，才读取其转发 IP 头。不可信来源的头会忽略，不要填入公共客户端网段。

## 在线用户与强制退出

“系统监控 → 在线用户”按账号展示至少持有一个有效登录会话的用户。
关闭页面或暂时无活动不代表离线。列表显示姓名、邮箱、有效会话数和最近活动时间，
每 15 秒刷新；已登录页面每 30 秒检查会话，浏览器休眠时可能延迟。

`online-users.read` 允许查看；`online-users.write` 允许强制退出，两者独立。
可强制退出任意用户，包括 Super Admin 和本人。所有旧会话被撤销，用户仍可重新登录。
服务端下次请求立即拒绝旧会话，联网页面在会话检查后显示失效提示并跳转登录页。
成功和失败的强制退出操作均进入操作历史。

## 许可与后续修复

Temvia 提供的代码使用 MIT；保留 LICENSE 和 `admin/UPSTREAM.md` 中的第三方声明。
你拥有自己新增的代码。生成项目没有 Temvia 运行依赖，不自动获取模板升级。
参阅 [Temvia 版本记录](https://github.com/jonathanhu237/temvia/releases)并自行合并适用修复。
缺陷和建议通过 [Issues](https://github.com/jonathanhu237/temvia/issues)，
安全问题使用 [SECURITY.md](https://github.com/jonathanhu237/temvia/blob/main/SECURITY.md) 中的私密入口。
项目尽力维护，不承诺固定响应时间或旧版本支持。
