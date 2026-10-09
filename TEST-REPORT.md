# Assist v0.2.0 验证报告

## 本次实际完成

| 层次 | 结果 | 证据 |
|---|---|---|
| Go 单元测试（含 race） | 16 项通过 | tests/go-tests.jsonl |
| Go 静态检查 | go vet 通过 | 构建时实际执行 |
| Linux 真实 HTTP / WS / SSE / Bash | 37 项通过 | tests/integration-results.json |
| Chromium 离线交互与布局 | 19 项通过 | tests/ui-results.json |
| Windows x64 可执行文件 | 交叉编译成功 | dist/assist.exe、assist-server.exe |
| Windows PowerShell / Job Object / 新版注册与 URI 分发 | 未实机测试 | 不能由 Linux 结果推断成功 |
| 浏览器网络端到端 | 未完成 | 当前 Chromium 受管 URLBlocklist 阻止导航；未修改或绕过策略 |
| macOS、公网 HTTPS / WSS、企业代理 | 未实测 | 不作可用性保证 |

## 真实命令输出证据

执行器实际运行：

```bash
printf 'stdout: hello 中文\n'; printf 'stderr: diagnostic\n' >&2; sleep 1; printf 'done\n'; exit 7
```

返回 stdout 含 `stdout: hello 中文` 与 `done`；stderr 含 `stderr: diagnostic`；退出码为 **7**，标记 **failed**，没有把错误伪装成成功。

另已实际验证：分段输出早于进程完成、网页审批前不下发、本机未确认不执行、客户拒绝、支持方不能代批、审批摘要不匹配被拒绝、工作目录、超时、运行中取消、本机确认中取消、取消后新命令、配对票据重放失败、运行中撤销、原生 cancelled 与 stopped 回执、撤销后禁止操作。

## 浏览器测试边界

离线 Chromium 操作使用 HTML 的预览模式，明确标记模拟输出。测试了发送命令、审批、拒绝、停止、终端渲染、报告页、开发说明移除及手机布局。另加载了真实后端测试回执验证终端渲染忠实性和 HTML 转义。

这不是 Windows 浏览器 → assist:// → EXE → PowerShell 的完整实机证明。v0.1.0 的用户实测经验不能自动外推到本版；本版增加了 Shell 执行、本机逐次确认、进程生命周期管理等路径。

## Windows 建议验收

1. 关闭旧程序，从新页面下载 v0.2.0 并更新，观察 assist:// 能否启动新版。
2. 新建第二条申请，只通过网页唤起，不重新下载。
3. 提交 `Get-Date` / `Get-Location`，分别在网页和本机批准。
4. 测试错误退出 `exit 7`、分段输出、超时与停止。
5. 在客户网页拒绝、在本机拒绝、等待本机确认时取消；均应不执行。
6. 运行中结束协助，应显示取消与停止回执，不能再发送命令。
7. 验证关闭 assist 控制台时普通子进程是否按预期终止。

**未签名的测试版本，不是可直接对真实客户公开分发的生产产品。**
