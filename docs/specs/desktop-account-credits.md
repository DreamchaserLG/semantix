# 桌面账户登录与积分（New API）

日期：2026-10-11。状态：用户已批准开工，并确认 5000 个计费 Token = 1 积分；已基于官方 v0.8.0 完成实现、主题适配、测试和 Windows 构建；用户已审核界面并要求提交 PR。真实账户登录由用户在桌面输入凭据确认。验收结果见 [报告](../reports/desktop-account-credits-20261011.md)。

## 版本基准修正

- 使用 [Gnosil/semantix v0.8.0](https://github.com/Gnosil/semantix/releases/tag/v0.8.0) 的发布代码，独立分支 `codex/account-v080`。该 tag 属于 `feat/sidebar-codex-ia` 的发布代码线；当前 main 与它存在 UI 差异，因此账户 PR 以该分支为目标，避免带入无关的版本变更。
- 保留该发布版的薄荷绿品牌色、冷白浅色表面、Logo 水印与 Playfair 首页、单排 Composer、现有侧栏结构。只增加账户登录及积分，不将 main 的界面文件覆盖到 release。
- 新账户组件使用 v0.8.0 的字号、圆角、表面变量，遵循轻量化规范，避免重复描边和重阴影。
- 用户下载的官方程序与开发审核版分开：官方 0.8.0 保留原桌面快捷方式；审核版命名 `semantix-v0.8.0-account-preview.exe`，从本工作区构建。此名称表达开发基准，不冒充官方发布。

## 目标与已确认事实

用户要求先写 SPEC 再实施：给 Semantix agent 增加登录，为账户开放额度，以积分呈现模型调用额度。用户确认已有 New API，地址为 `https://sem.ensureok.ai`。

公开只读探测 `/api/status`：站点名 Semantix，版本 `v0.0.0-semantix.20260924-2`，密码登录开启，Turnstile 关闭，`quota_per_unit=500000`，站点原显示单位 USD。没有读取或修改任何真实账户、密钥或额度。

## 本轮交付

### 启动登录与注册（用户本轮修订）

- 删除面向用户的积分换算比例、Token 余额、模型倍率及额度分配说明卡片；换算规则保留在后端和本 SPEC。账户页只显示身份、积分余额、用量与会话操作。
- 正常启动（不带任何参数）先恢复并验证账户。未登录展示独立品牌登录/注册页；恢复失败或离线时可重试、清除旧会话，不把失败当作已登录。认证成功后挂载 agent 主界面，设置里的账户页保留；退出后返回登录页。
- 注册直接调用本站现有注册接口，不建立本地账户库：用户名、密码、确认密码；按服务器能力增加邮箱验证码及协议同意。服务端关闭注册或要求 Turnstile 时不绕过；引导使用官网完成其要求。
- 注册成功只表示账户创建成功，提示重新登录，不伪造登录状态。密码和确认密码发送后清空，不持久化。网络结果不确定时提示先尝试登录，避免误导重复创建。
- 当前公开状态：允许用户名密码注册，邮箱验证/Turnstile/用户协议/隐私政策开关均关闭。每次注册及验证码请求重新读取服务器开关；不对线上创建测试用户或发送测试邮件。
- 启动页提供原生窗口操作、正确语言/主题和键盘表单操作。浏览器开发预览仍不进行真实认证。

1. 桌面「设置 → 账户与积分」：用户名/密码登录、需要时输入两步验证码、加载与失败状态、登录后显示身份、剩余积分、累计消耗积分、请求次数、上次同步时间。
2. 对接本站现代 New API bearer/refresh 协议（实际前端脚本已确认 `/api/user/auth/refresh`、`X-Auth-Session`、`access_token`、`session.sid`）；Go 后端请求固定 HTTPS 站点，WebView 不直连认证接口。密码只参与本次登录，不保存。访问令牌只保留在 Go 内存；刷新 Cookie 只存在 Go 内存或操作系统凭据库中，不返回前端，不写 localStorage、配置文件或日志。
3. 登录成功后必须再读取 `/api/user/self`，核对用户 ID 与启用状态，才能认定已登录。后端登录响应缺少 ID、返回未知验证挑战或协议不兼容时不能当成成功。
4. 默认尝试用操作系统凭据库记住会话；不可用则仅本次运行登录，明确显示提示，不降级为明文存储。重启后恢复会话时必须再次向服务器验证，离线不能显示成已验证账户。
5. 手动刷新及打开账户页时同步积分；服务故障保留上次成功读数并标明过期；401/403 或账户停用清除身份和本地会话。退出会请求服务器退出并清除本地会话；远端失败或凭据库删除失败必须分别说明，并允许重试清理。
6. 中英繁体文案与现有设置界面样式保持一致。浏览器开发模式明确标记预览，不伪造成功登录或可用额度。

## 积分语义与服务端边界

- 用户确认 `1 积分 = 5000 个计费 Token`：现有 New API quota 单位作为计费 Token，剩余积分 = `quota / 5000`，累计消耗积分 = `used_quota / 5000`。500000 quota 显示 100 积分，1 quota 显示 0.0002 积分。Go 整数运算精确换算，十进制字符串跨桥接和格式化，最多 4 位小数；不得经过 JS 浮点数。支持负余额，累计消耗与请求次数不能为负。
- 计费 Token 受模型、输入/输出、缓存和分组倍率影响，与原始 Token 数不同；这是内部业务口径，不在账户界面展示。`quota_per_unit` 只用于中转站货币显示，不作为积分换算参数。现有实际 Token 用量和费用统计保持原含义。
- 管理员在现有 New API 后台创建/禁用账户、调整额度。桌面用户没有增加余额、修改角色、请求管理员密钥的接口。不建立第二份本地扣费账本。
- 本轮完成账户登录及余额同步；登录会话不会自动替换模型 API Key，也不会创建模型令牌或改写已有模型配置。后续接入模型调用时，使用归属该用户的受限推理令牌访问 `/v1`，由 New API 执行额度检查与结算。控制台会话与推理令牌权限分离；模型令牌的发放、撤销和账户切换需与实际站点接口一起联调。
- 不新增本地密码库、支付/充值系统、独立注册数据库、哈希基线、冻结契约或 CI 门禁；注册复用中转站服务。

## 登录界面与数据库连接

- 沿用桌面现有主题变量、字体、Lucide 图标、按钮和输入框，增加清晰的登录标题、密码显隐、注册与验证码步骤；窄窗单列，深浅主题均可用。
- 连接已部署的 `https://sem.ensureok.ai`。服务器 `controller.Login → User.ValidateAndFill → users` 验证密码与账户状态；`service.CreateLoginSession → user_sessions` 保存/轮换/撤销会话；`GetSelf → GetUserById` 从数据库读取身份和余额。复用服务器现有 SQLite/MySQL/PostgreSQL 连接及安全控制，桌面不直连数据库，不引入第二套账户表。
- 在账户页可打开账户网站、刷新余额、退出；提供 `--account` 启动入口，便于直接打开页面本地审核。此开关仅导航，不跳过登录或改变权限。

## 接口与状态

认证适配器对应用户当前站点的 bearer/refresh 协议（不声称支持所有 New API/One API 分支）：

| 操作 | 请求 | 要求 |
| --- | --- | --- |
| 登录前能力检测 | GET `/api/status` | 密码登录可用，Turnstile/新式密码加密启用则明确不兼容，不尝试绕过 |
| 登录 | POST `/api/user/login`，JSON username/password | success=true；验证 Bearer bundle/user/session；识别 require_verification |
| 两步验证 | POST `/api/user/login/verify`，JSON flow_token/method/code | 仅支持 available 的 2fa；flow 和验证码不保存；遵守服务端 expires_at，最多十分钟 |
| 恢复/续期 | POST `/api/user/auth/refresh` | new_api_refresh Cookie + X-Auth-Session；新 bundle 必须属于同一用户、同一 sid；轮换后及时更新安全存储 |
| 身份/积分 | GET `/api/user/self` | Bearer；ID 一致，status=1，必需数值字段完整 |
| 注册能力 | GET `/api/status` | 每次提交重新检查注册、邮箱验证、协议与挑战开关 |
| 注册 | POST `/api/user/register` | 仅用户名/密码及必要邮箱/验证码，遵守服务端验证，不传角色或额度；成功后回到登录 |
| 注册验证码 | GET `/api/verification?email=...` | 仅邮箱验证启用时；邮箱按查询参数编码；遵守站点限流 |
| 退出 | POST `/api/user/auth/logout` | Cookie + X-Auth-Session；尽力通知远端；本地总是退出 |

Wails 绑定：`AccountStatus()`、`AccountLogin(username,password)`、`AccountVerify(code)`、`AccountLogout()`、`AccountRegistrationOptions()`、`AccountRegister(username,password,email,code,agreed)`、`AccountSendRegistrationCode(email)`。返回脱敏的账户状态，错误仅使用有限错误码；不向 UI 返回服务器原始错误正文（其中可能含凭据）。所有账户操作串行处理，避免刷新/退出/切换账户互相覆盖；每次网络请求限时、限制响应大小、禁跟随重定向。

状态：`signed_out`、`verifying`、`signed_in`。附加 `errorCode`、`warningCodes`、`stale`、`remembered`、`syncedAt`、`hasSession`（尚待 self 验证或撤销重试时仍提供刷新/退出入口）。网络故障与余额为零必须区分；未知余额显示为未知，不显示成 0。

固定站点地址来自产品代码，不接受 workspace、项目配置、模型工具或 WebView 自由传入的登录地址。持久化条目带站点、用户 ID、sid 与刷新 Cookie，恢复只使用预期站点的 new_api_refresh Cookie；不持久化访问令牌。所有认证请求带固定站点 Origin，保持服务端同源校验。已有模型配置保留；登录后进入现有工作区。启动登录是桌面产品入口，模型额度授权仍由中转站服务端执行。

## 验收

- 正常启动先显示登录/注册，已验证会话自动进入；退出后工作区卸载并回到登录页。注册成功不视为登录。
- 注册开关、验证码、协议与 Turnstile 不绕过；密码确认不符不发请求；响应不确定不自动重试。
- 用户界面不出现积分换算、计费 Token 或管理员配额说明。

- 正常登录 → self 校验 → 积分显示；密码错误、禁用账户、过期会话不展示成功。
- 2FA 完成前没有身份/积分、没有持久化会话；错误码可重试，过期需重登录。
- 网络错误/HTML/畸形 JSON/缺失字段/重定向/超大正文均明确失败；不泄露正文、密码或 Cookie。
- 凭据库不可用、恢复失败、删除失败均有确定行为；退出与并发刷新不会恢复旧身份。
- 超过 `2^53` 的 quota 仍精确；零与负余额正确呈现。
- Go HTTP 测试使用本地假服务器与内存凭据库，不创建真实账户或扣费；前端组件测试覆盖主要状态与敏感输入清空；类型检查、前端构建、Go vet/test 和 diff 检查。
- 真实站点只执行公开 status 探测；真实账户的端到端登录待用户在桌面输入凭据验证，报告必须明确这项限制。

## 参考

- [New API v0.10.8 用户登录/退出/self](https://github.com/QuantumNous/new-api/blob/v0.10.8/controller/user.go)
- [New API v0.10.8 两步验证](https://github.com/QuantumNous/new-api/blob/v0.10.8/controller/twofa.go)
- [New API 当前用户接口](https://github.com/QuantumNous/new-api/blob/main/controller/user.go)
- [New API 刷新/退出](https://github.com/QuantumNous/new-api/blob/main/controller/auth_session.go)
- [New API 登录验证](https://github.com/QuantumNous/new-api/blob/main/controller/login_verification.go)
