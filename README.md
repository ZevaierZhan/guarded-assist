# Assist v0.2.0 · 远程协助原型 + 真实 Shell 验证

本版把原型的申请页、实时页、完成页与 assist 接入 demo 合并。右下角 `? → 预览与命令` 可模拟本地 Agent 提交命令。它不是静态成功动画：通过服务访问时会执行客户机的真实 PowerShell / Bash，并同步结果。

**仅用于你有权限的测试设备。程序尚未签名，发布者身份未验证，未经过生产安全审计。不要关闭系统防护来运行未知程序。**

## 1. Windows 同机验证

1. 完整解压 Windows 包，关闭上一版 assist / assist-server。双击 `start-demo.cmd`，保留服务窗口。
2. 浏览器自动打开支持方管理入口。填写问题，点击「创建请求并生成链接」。
3. 点击「打开客户请求页」，在另一个标签页模拟客户。**管理页和客户页是两种不同凭据，不是简单切换前端按钮。不要把管理入口发给客户。**
4. 客户打开 assist；首次使用时，在「首次使用？下载并打开一次」中下载新版 EXE，保留下载文件名并打开。打开工具即兑换本次限时配对票据，不再要求网页或本机逐条确认。
5. 等待网页显示设备就绪。**连接成功本身不会执行命令。**
6. 回到支持方管理页，右下角 `? → 预览与命令`，选择 PowerShell、填写命令、工作目录和超时，点击「提交并自动判定」。
7. 服务端与客户执行器使用相同策略：命中诊断白名单自动执行；命中黑名单或未明确收录的命令直接阻断。
8. 双方终端同时显示策略判定、标准输出、错误输出、退出码和耗时。运行中可「停止命令」，也可以「结束协助」。

建议先用以下命令验证：

```powershell
Get-Date
Get-Location
```

分段输出：

```powershell
1..3 | ForEach-Object { Write-Output "step $_"; Start-Sleep -Seconds 1 }
```

错误与退出码：

```powershell
[Console]::Error.WriteLine('demo error')
exit 7
```

程序默认在客户的用户主目录运行。设定的工作目录以客户机器为准，而不是支持方机器。

### 旧版本升级

- 旧 ping 版 v0.1.0 **不能执行本版 Shell 协议**。先结束旧连接并关闭旧进程，再从新请求页下载 v0.2.0。
- Windows 用户级安装位置仍为 `%LOCALAPPDATA%\AssistDemo\assist.exe`，协议仍为 `assist://`；只更新本 Demo 自己注册的处理器，不覆盖无关软件。
- 首次下载文件名携带短期一次性配对数据，EXE 本身的字节不随请求修改。请保留原文件名。
- 第二次新建请求后，不要重新打开携带旧票据的下载文件；应从新网页点击打开。
- `uninstall-assist.cmd` 移除该用户级注册和程序；先结束所有 assist 进程。原下载文件需自己删除。

## 2. 只看原型，不执行命令

直接打开 `index.html` 或独立的 `assist-prototype-v2.html`。`file://` 自动进入纯预览模式。

- 右下角 `?` 切换客户 / 支持方、申请 / 实时 / 完成页，模拟设备回连、掉线和清理待确认。
- 「提交并自动判定」模拟白名单放行或黑名单阻断，不执行 `eval`、Shell 或本机程序。
- 预览输出明确标记「模拟输出」。自定义命令只产生占位结果，不伪装成真实命令成功。
- 将 `ENABLE_DEVTOOLS=true` 改为 `false` 可整体移除入口、抽屉与原型标签。服务认证不依赖该开关。
- 从服务访问时，可通过 `?preview=1` 新开纯预览；不会影响真实会话。

## 3. Linux / Bash 真实验证

使用 Linux 包。先添加执行权限，再启动：

```bash
chmod +x assist-server-linux-amd64 assist-linux-amd64 start-demo.sh
./start-demo.sh
```

页面流程与 Windows 相同，下载时选择 Linux。客户机下载文件后需执行 `chmod +x <原下载文件名>` 并从终端运行。命令由黑/白名单策略自动判定。**Linux 版没有实现桌面协议注册或首次下载自动执行**；本版的一键安装 / 协议唤起体验主要验证 Windows。

```bash
date
pwd
```

- Windows 版报告系统 PowerShell；不自动安装 Bash / Git / WSL。
- Linux 版检测现有 Bash；没有 Bash 时拒绝该能力，不自动安装。
- macOS 默认提供原生 `Assist.app` 体验包：从本次邀请页下载 ZIP，解压后打开 `Assist.app`。包内同时带 Apple 芯片 arm64 与 Intel x64 程序，运行时自动选择；无需安装 Python、Node 或系统服务。App 会打开终端显示连接、命令输出与停止状态。关闭终端或按 Ctrl+C 可停止本机接入。
- `Assist.app` 按每次请求动态打包，配对信息位于应用包内，不依赖 ZIP 名称；新请求需重新下载。它不安装服务、不注册协议、不设置开机启动；ZIP 和解压后的 App 留在客户机上，需手动删除。
- **此 App 尚未签名、公证，也未在真实 macOS 机器验收。** macOS 可能阻止打开；只在确认来源可信时使用系统设置「隐私与安全性 → 仍要打开」作单次例外，不要关闭 Gatekeeper。被管理的 Mac 可能禁止例外；正式分发仍需签名、公证。参见 [Apple 安全说明](https://support.apple.com/en-lamr/102445)。
- 邀请页保留单文件 Python 3.8+ 脚本作为备选：已有 Python 的客户可下载脚本、保留原文件名，在终端执行 `cd ~/Downloads` 和 `python3 './<原文件名>.py'`。两条 Mac 路径都使用 Bash 执行受策略限制的命令，不注册 `assist://` 协议。

## 4. 这是什么样的「终端」

这是**逐条非交互式命令窗口**，并非 PTY / ConPTY 或持续交互 Shell。

- 支持多行脚本、独立 stdout / stderr、持续输出、工作目录、退出码、超时与取消。
- 每条命令启动一个新进程；变量、`cd` 和环境修改不会自动继承到下一条。需要连贯操作时放在一条多行命令中，或显式指定目录。
- 不支持 vim、top 等全屏程序，不提供键盘流、交互密码输入或提权代理。
- PowerShell 使用 `-NoProfile -NonInteractive -Command`，不设置 ExecutionPolicy bypass。UTF-8 输出设置在命令前完成。
- Bash 使用 `--noprofile --norc -c`。这是降低环境干扰，不是安全隔离。
- 单条命令最大 4096 UTF-8 字节；超时 1–60 秒；输出约 60 KB 后截断并提示。每次会话最多 30 条命令且只能有一条活动命令。
- 会话最长 10 分钟，由 daemon 本地截止时间与后台共同约束。

## 5. 组件与通信

```text
支持方开发面板
    └─ HTTP POST /api/requests/{id}/commands
服务记录不可变命令并执行策略判定
    ├─ 黑名单 / 未收录 → blocked → SSE 双方页面
    └─ 白名单 → WebSocket exec → 客户 assist
assist
    ├─ 再次执行同一策略（防御纵深）
    ├─ 通过后创建当前用户的 Shell 进程
    └─ WebSocket output / result → 服务 → SSE → 双方页面
结束
    └─ HTTP stop → WSS stop → 取消并等待执行器 → stopped 回执
```

原型组件：`Header`、`Stepper`、`Scopes`、`RequestSide`、`ConnectionSteps`、`PolicyCard`、`Terminal`、`SessionCard`、`EventFeed`、`CompletePage`、`renderModal`、`renderDev`。

本版开发面板暂时代替 MCP / Agent。**尚未实现 MCP 工具注册或 Agent 宿主自动唤醒**，不把一次 HTTP 提交称为真实 Agent 编排。

认证对象分离：管理凭据、客户查看凭据、一次性配对票据和设备凭据。`?` 只是控件入口，不是安全边界。策略在服务端和执行器各执行一次，默认拒绝未收录命令；这仍不是完整的生产零信任或系统沙箱。

## 6. 跨机器接入

默认 `127.0.0.1:18777` 仅适合同机验证，不能原样发给别的电脑。

支持方创建请求时，页面会列出所有已启用网卡的本机 IP，并默认优先选择系统默认路由使用的地址。支持方只需在「我的 IP」中选择客户所在网络能够访问的地址；本地 HTTP 模式下，该地址会直接写入本次邀请链接和接入工具的回连信息，不再需要填写对方 IP。

同一私有局域网内可直接使用本机 IP 启动服务，例如：

```powershell
.\assist-server.exe --listen 0.0.0.0:18777 --public http://192.168.1.10:18777
```

把示例 IP 换成页面「我的 IP」列表中适合客户网络的地址，并确认防火墙允许该端口；服务必须监听 `0.0.0.0` 或所选网卡地址。对方应先在浏览器打开邀请链接验证连通。局域网 HTTP 不加密，管理链接和配对信息会经过明文网络，请只在可信网络使用。

如果通过公网接入，则需要准备一个真实可达、带有效证书的 HTTPS 原点，再将 `/`、`/api/*`、SSE 和 WSS 转发到本地服务：

```powershell
.\assist-server.exe --public https://your-support.example --listen 127.0.0.1:18777
```

这里的域名仅为占位符。公网地址仍须使用 HTTPS。此源码内的 WS 客户端是受控 demo 的子集实现，**没有完整企业代理 / PAC 适配、自动重连或生产一致性测试**。第三方入口必须同时支持 SSE 与 WebSocket。

管理凭据只保留在支持方入口，勿发送给客户。页面需要由服务器提供；浏览器直接打开 HTML 不会连接真实 daemon。

## 7. 停止与安全边界

- 网络断开时不自动重放命令；未确认结果保持「未知」。
- 网页停止先撤销新操作，再通知原生端；只有收到 `stopped` 才显示停止回执。
- 在 Windows 中用 Job Object 将普通子进程关联到 assist 生命周期；运行中停止通过进程树终止。Linux 通过进程组取消活动命令。
- 这不是系统沙箱。白名单中的命令仍以当前用户权限运行；结束连接不回滚已完成的修改。
- 黑名单优先，未命中白名单默认阻断；生产环境应改用结构化工具能力，而不是依赖正则解析任意 Shell。
- 原始 Shell 输出没有完整的自动脱敏保证，策略放行不代表输出一定不含敏感信息。
- 程序目前未签名、请求签发方未通过官方业务身份校验。不得作为无需审核的生产远控平台发布。
- 没有管理员自动提权，不应以管理员 / root 启动客户工具做常规演示。

## 8. 开发与测试

构建需要 Go 1.23+，仅构建机需要；分发后的程序不需要 Go、Python、Node 或 npx。源码没有 Go 第三方依赖，`GOPROXY=off` 可构建。

```bash
bash build.sh
go test -race ./...
go vet ./...
python tests/integration.py
python tests/ui_test.py
```

Python / Playwright 仅是开发测试工具，不是客户运行依赖。浏览器 UI 测试使用离线 DOM；真实 HTTP / WS / Bash 另行测试，不以离线测试冒充浏览器操作系统协议唤起成功。详见 `TEST-REPORT.md`。

### 实现参考

- Microsoft PowerShell 命令行参数：https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_powershell_exe?view=powershell-5.1
- Windows Job Objects：https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects

上述文档描述底层接口，不代表微软认证本 demo。
