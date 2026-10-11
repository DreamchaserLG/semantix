# 桌面账户登录、注册与积分验收记录

日期：2026-10-11。依据 [SPEC](../specs/desktop-account-credits.md)，基于官方 v0.8.0，开发分支 `codex/account-v080`，PR 目标 `feat/sidebar-codex-ia`。用户已完成界面审核并授权提交 PR。

## 交付行为

- 正常启动先显示独立登录/注册界面；已验证的保存会话自动进入工作区，退出账户后返回登录。设置中的「账户与积分」保留余额、用量、刷新和退出。
- 登录/注册页面使用该发布版的品牌、字体和主题变量，支持深浅主题、窄窗口、密码显隐和 2FA。删除面向用户的积分换算、计费 Token、分组及管理员配额说明。
- 通过固定 `https://sem.ensureok.ai` 的现有 New API 接口连接服务端 users、user_sessions。没有新增本地账户表、明文密码库或独立账本，也没有修改中转站源码、数据库结构或线上配置。
- 注册复用 `/api/user/register`，按需调用 `/api/verification`。每次提交重新读取注册开关、邮箱验证和协议要求；不绕过 Turnstile。成功后返回登录，响应不确定时提示先尝试登录，不自动重试创建。
- 密码/验证码发出后清空输入框。访问令牌仅在 Go 内存，刷新凭据进入操作系统凭据库；不可用时仅保存在当前进程并提示。恢复会话必须向服务器验证。
- 内部按 `quota / 5000` 显示积分，以整数与十进制字符串保留精度。500000 quota 对应 100 积分，1 quota 对应 0.0002 积分；支持 int64 负值及超过 JS 安全整数的额度。
- Windows 审核程序为 `desktop/build/bin/semantix-v0.8.0-account-preview.exe`，与下载的官方原版分开。正常启动无需参数；`--account` 仅在认证后导航到账户设置。未发布新的正式安装包或 release。

## 验证结果

| 检查 | 结果 |
| --- | --- |
| `cd desktop && go test -cover ./account` | 通过，语句覆盖率 88.4% |
| `cd desktop && go vet . ./account` | 通过 |
| `go test -run 'Test(ParseDesktopLaunchArgs\|DesktopStartupSettings\|SingleInstance\|WindowsICO\|AppIconPNG)' -count=1 .` | 通过 |
| `cd desktop/frontend && pnpm test:account` | 通过，包含注册、启动恢复和退出返回流程 |
| `pnpm build` | 通过，包含 lint、CSS、类型及现有 bundle 检查；没有放宽检查阈值 |
| startup-settings / settings-navigation / settings-responsive 定向前端检查 | 46 / 4 / 25 项通过 |
| `wails generate module` 与 Windows Wails 构建 | 通过，使用最终前端产物生成审核 EXE |
| Edge headless 页面检查 | 登录/注册在 1280×900 深浅主题、390×780 窄窗全部通过，无页面错误或横向溢出；截图人工检查通过 |
| `git diff --check` | 通过 |
| 现有中转站 service/model/controller 数据库定向测试 | 6 个用例通过：会话创建/刷新/撤销、身份版本失效、轮换竞争、错误 Cookie 拒绝与会话上限；使用测试数据库 |

后端测试覆盖 self 校验、2FA、刷新轮换及身份绑定、失效会话、凭据库故障、并发退出、重定向拒绝、畸形响应、限流、精确积分、注册开关与输入/协议验证、邮箱参数编码和创建结果不确定时禁止自动重试。

前端测试覆盖敏感输入即时清空、密码确认不符不发请求、注册成功回到登录、验证码与重发冷却、协议/挑战展示、真实会话校验前不挂载工作区、登录后挂载、退出后卸载及保存会话恢复。

## 尚未验证的边界

- 没有使用真实凭据进行线上登录、2FA、注册或余额读取，没有创建测试用户或发送测试邮件。公开 status 已确认当前启用密码登录和注册；真实端到端验收需在审核程序中由用户完成。
- 未宣称全仓测试通过。本次运行了上述定向检查；此前主目录全量检查存在无关实验目录与 MCP 测试失败。
- 当前环境 `CGO_ENABLED=0` 且无 gcc/clang，无法运行 Go race detector；普通并发测试已通过。
- 本地构建缺少 ICO 时复用了已有品牌图标生成的忽略目录构建资源，没有修改发布流程或品牌源图。

## 模型调用边界

账户会话目前用于身份和积分查询。模型 API Key 仍按原模型配置使用，尚未自动签发或替换成当前账户的受限推理令牌；现有任意模型调用不能据此认定已绑定该账户扣费。后续需联调服务端令牌发放、撤销、路由和真实请求结算。
