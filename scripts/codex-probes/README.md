# Linux Codex 探测脚本

两个 Python 入口直接调度**本机安装的官方 Codex CLI**。每次均为新 `codex exec --json --ephemeral`，不用 resume，不拼装/伪造 Codex 的 User-Agent、身份、会话头或 TLS 指纹。协议由所安装的 CLI 生成；这保证使用真实客户端，**不代表与交互式 Codex 的工具列表、指令或配置逐字节相同，也不保证不被渠道封禁**。

运行要求：Linux（使用 `/proc`、进程组和 `flock`），Python 3.11+，Codex CLI 0.155.1+。本实现按官方 `rust-v0.155.1` 源码核对，建议固定该版本，升级后先运行 `--check` 并检查官方变更。Python 仅使用标准库。

## 准备

为 any 与 key **分别建立专用 AxonHub API key，并在其 active profile 中限制为单一目标渠道**。本脚本只知道 URL/key/model；如果 key 可访问多个渠道，成功结果可能来自另一个渠道，脚本无法证明 any 或 key 已恢复。不要用有管理权限的 key。本脚本不修改渠道、配额或网关重试策略。

URL 是 API 基地址，如 `https://gateway.example/v1`，不要传完整 `/responses` 地址。key 通过环境变量传入，不进命令行、配置文件或日志：

```bash
read -rsp 'AxonHub key: ' AXONHUB_PROBE_API_KEY
export AXONHUB_PROBE_API_KEY
python3 scripts/codex-probes/keepalive.py \
  --url https://gateway.example/v1 --model YOUR_MODEL --check
```

`--check` 检查本地配置、题库与 CLI 版本；不发模型请求。`--codex /absolute/path/to/codex` 可指定二进制。先使用你渠道实际支持的 model/effort；脚本不会猜测模型别名、修改 service tier 或伪造能力。

## 1. any 两阶段循环

```bash
python3 scripts/codex-probes/keepalive.py \
  --url https://gateway.example/v1 --model YOUR_MODEL \
  --concurrency 2 --stagger 5 --effort low --reasoning-effort medium \
  --retry-min 30 --retry-max 180 \
  --interval-min 60 --interval-max 120 --timeout 600 \
  --state-dir /home/probe/.local/state/axonhub-probes/any
```

- 第一阶段：从简单题库随机无放回抽题，以配置的并发数运行独立新会话。启动错开，收到**最终结构化错误** `We're currently experiencing high demand, which may cause temporary errors` 或已确认的 `当前模型 <模型名> 负载已经达到上限，请稍后重试` 后删除本次本地会话，再按指数退避与随机抖动重试。默认并发 2，可配置 1–32。不会仅凭 HTTP 500/429 就归类为容量不足。
- CLI 的 `request_max_retries`、`stream_max_retries` 设为 0，不叠加客户端重试；AxonHub 内部重试完成后 CLI 才会返回最终结果。脚本不修改网关重试。不能从错误文本证明上游物理执行次数。
- 任一会话以 `turn.completed` 且进程退出码 0 完成，停止补充第一阶段任务，取消并等待其他私有子进程退出、清理全部本地 attempt 后进入第二阶段。已发送的上游请求可能仍在网关/提供方结束中；本地取消不是上游零用量保证。
- 第二阶段：仅一个会话，从独立逻辑题库抽题，完成并清理后随机等待再创建下一会话。在这次第二阶段内**累计** 10 次目标 high-demand 错误回到第一阶段；中途成功不清零。phase/counter 落盘，重启后恢复。
- 401/403、封禁提示、其他错误、超时、缺少完成事件均停止并清理，退出码 1，不被当作 high-demand 无限重试。`turn.failed` 优先于中间或迟到的 `error`，模型正文及 stderr 都不参与容量错误识别。
- `attempt_result` 日志提供 `error` 和 `error_source`。优先记录结构化错误；没有结构化错误原因时，才记录 stderr 最后一条明确以 `Error:` 开头的错误行，或进程退出/超时说明。诊断先脱敏当前 key、常见凭据字段及认证头，再限制为 2048 字符；不保存原始 stderr、请求头或完整日志。成功时这两个字段为 `null`。
- `--max-attempts N` / `--max-tokens N` 提供每次脚本启动的停止条件，0 为不限。token 预算按 CLI 已报告的 input+output 累计，reasoning 是 output 的子集，不再相加。并发在途请求和失败未报告用量可能超出预算；它不是硬计费上限。若要成本硬限制，请同时配置网关/API key 配额。

题库：`questions-simple.json` 为 1,600 道重新编写的知识和日常问题；`questions-reasoning.json` 为 172 道原创有限数学/逻辑题。第二阶段要求认真核验并简短作答，不要求展示思维链或无意义空转。**短答案不代表低推理 token；无法保证思考时间或保持渠道容量。** 可通过 `--simple-bank` / `--reasoning-bank` 替换同结构 JSON，不会物理移动第一阶段题目。

随机抖动只用于平滑流量/退避，不模拟人类身份、不尝试绕过封禁、速率限制或容量控制。

上面的参数是一组较温和的起点：第一阶段每个 worker 失败后等待约 30–45、60–90、120–180 秒，后续最多 180 秒；第二阶段完成后等待 60–120 秒再开始下一次，使用 `medium` 推理强度。脚本的默认值未改变，请显式传入示例参数。任何请求本身的执行时间另计；AxonHub 内部多次重试的用量也需要单独考虑。

### 手动暂停与恢复

前台终端按 **P** 暂停、**R** 恢复、**空格** 切换、**Q** 退出，不需要回车。收到暂停时停止排新请求，取消当前私有 Codex 进程并完成全部清理后才打印 `paused`。删除正在进行时会先删完；清理失败则报错停止，不伪装为暂停成功。恢复创建新的会话，保持原阶段与累计 high-demand 次数；人为取消不计入高需求错误。

后台/systemd 无终端时，用日志里的脚本 PID：`kill -USR1 PID` 暂停，`kill -USR2 PID` 恢复。systemd 可用 `systemctl kill --kill-whom=main --signal=SIGUSR1 YOUR_SERVICE`，恢复换 SIGUSR2。不要用 SIGSTOP，它会把清理一起冻结。暂停期间按 Q/Ctrl+C 仍会正常退出。暂停状态不跨脚本重启保存，阶段和计数会保存。

## 2. key 定时单次请求

```bash
# 一次性准确日期，含时区；指定时间已过去则补一次
python3 scripts/codex-probes/scheduled_probe.py \
  --url https://gateway.example/v1 --model YOUR_MODEL \
  --at '2026-09-21T08:00:00+08:00' \
  --state-dir /home/probe/.local/state/axonhub-probes/key-weekly

# 下一次指定本地时间（默认 Asia/Shanghai），执行一次后退出
python3 scripts/codex-probes/scheduled_probe.py \
  --url https://gateway.example/v1 --model YOUR_MODEL --daily 08:00:00

# 下一次周一 08:00，执行一次后退出
python3 scripts/codex-probes/scheduled_probe.py \
  --url https://gateway.example/v1 --model YOUR_MODEL --weekly Mon@08:00:00
```

每次只创建一个新会话、尝试一次；成功或失败都清理后退出，失败不自动重复尝试。成功退出码 0，错误/超时为 1，配置错误为 2，SIGINT/SIGTERM 为 130。时间计划写入 `last-slot.json`，等待中的任务重启后继续原时间并可补执行；联系上游前写 durable claim，已尝试或结果不确定的相同 slot 不自动重放。`--now` 每次显式调用都代表一个新 slot，适合外部 systemd timer；不要给该单次服务配置 `Restart=always`。

带 DST 的时区遇到不存在的墙钟时间会拒绝；有歧义的时间优先使用含明确 UTC offset 的 `--at`。同一个 state-dir 绑定 URL/key/model，不允许不同目标共享状态。

**窗口边界由上游决定。** 脚本不会重置周配额或现有 5 小时窗口，也不能保证第一次请求一定启动某种窗口。若该渠道确实使用“重置后的首次有效请求开启 5 小时窗口”，可以按已知 reset 时间安排第一条请求；单次模式不会查询私有 quota API；常驻窗口模式只读取 AxonHub 的受限快照接口，也不会用四舍五入的百分比推断窗口。定时请求不增加随机延迟，避免偏离你选择的时间。

## 周重置监听 + 每日两次（常驻模式）

```bash
python3 scripts/codex-probes/scheduled_probe.py \
  --url https://gateway.example/v1 --model YOUR_MODEL \
  --watch-windows --first-time 08:00:00 --timezone Asia/Shanghai \
  --quota-poll 30 --quota-max-age 3600 --catch-up-grace 900 \
  --effort low --timeout 600 \
  --state-dir /home/probe/.local/state/axonhub-probes/key-windows
```

每日在自定义首发时间及其 **5 个实际小时后** 各执行一次，例如 08:00 与 13:00；周重置另触发一次，不占每天这两个名额。首发时间超过 19:00 时，第二次落在次日，仍属于前一个首发日期的计划。周重置和日内计划相同时间到期会合并为一次；任一任务失败不会循环尝试。

需要部署本次 AxonHub 新增的 `GET /v1/axonhub/quota-windows`。它接受同一个推理 API key，但要求其 active profile **明确绑定且仅绑定一个渠道 ID**，并满足 project profile 的渠道和 tag 约束；无绑定、多渠道或匿名回退 key 会返回 403。只读接口不会创建会话、刷新上游或返回渠道凭据/account key/raw quota。必须在 AxonHub 开启该 provider 的配额采集，并能实际采到 `7d`/`weekly` 的重置时间；仅凭渠道名叫 key 并不足够。

这不是上游事件推送：默认每 30 秒读取 AxonHub 快照，得到周 reset_at 后提前登记本地定时器，执行时间不必等下一次轮询。上游更改时间可由后续快照修正；快照超过默认 1 小时视为过期，不用它建立新计划。已经登记的 reset 时间可在短暂网络故障时执行，实际准确性仍取决于上游时间、服务器采集和时钟同步。`--quota-poll`、`--quota-max-age` 可调，客户端与服务器时间差超过 60 秒停止。

本地持久化每个周 reset 和两个每日 slot；请求前记录 unconfirmed，SIGKILL/重启后不重复发已经开始的请求。默认漏过超过 15 分钟的 slot 记 missed；短暂中断仅补最新一个时刻，不爆发回放积压。`--catch-up-grace` 调整补执行时间。--watch-windows 常驻时的 systemd service 使用 `Type=simple`，不另外设置 timer，也不要与每日/每周 timer 同时运行。

## 常开 Linux 的 systemd 示例

以专用低权限 `probe` 用户运行，安装固定版本 Codex，代码放在 `/opt/axonhub/scripts/codex-probes`。创建 `/etc/axonhub-probe-key.env`，权限 `0600`，内容为 `AXONHUB_PROBE_API_KEY=...`。不要把真实 key 提交到 Git。

`/etc/systemd/system/axonhub-key-probe.service`：

```ini
[Unit]
Description=One disposable Codex request through AxonHub
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
User=probe
EnvironmentFile=/etc/axonhub-probe-key.env
ExecStart=/usr/bin/python3 /opt/axonhub/scripts/codex-probes/scheduled_probe.py --url https://gateway.example/v1 --model YOUR_MODEL --codex /usr/local/bin/codex --now --state-dir /home/probe/.local/state/axonhub-probes/key-timer
TimeoutStartSec=12min
TimeoutStopSec=30s
KillMode=mixed
```

`/etc/systemd/system/axonhub-key-probe.timer`（示例每日 08:00；按真实配额和工作时间调整）：

```ini
[Unit]
Description=Start the configured key probe time
[Timer]
OnCalendar=*-*-* 08:00:00 Asia/Shanghai
Persistent=true
AccuracySec=1s
RandomizedDelaySec=0
[Install]
WantedBy=timers.target
```

每周示例为 `OnCalendar=Mon *-*-* 08:00:00 Asia/Shanghai`。每日与每周计划重叠时不要重复定义同一个触发。安装这些配置后由你执行 `systemctl daemon-reload` / `systemctl enable --now axonhub-key-probe.timer`；本次实现未安装或启动任何服务。

常驻 keepalive 可将 service 改为 `Type=simple` 并使用 `keepalive.py` 的命令；保留 `KillMode=mixed` 和停止宽限期。建议 `Restart=no`，避免鉴权错误/封禁后被 systemd 反复拉起。

## 本地会话与清理边界

每个 attempt 使用独立、带随机 ID 的 `codex-home` 和空工作目录，真实 key 仅存在子进程环境。不会读取/删除日常 `~/.codex/sessions`，不会调用 AxonHub archive/delete，也不会删除上游会话。进程退出后删除 attempt 的全部文件，phase、slot 和所有权记录保留。

Ctrl+C、SIGTERM、超时先结束该私有进程组并等待退出，再清理目录。SIGKILL/断电不可能保证即时清理；下一次启动依据 owner、随机目录名、Linux boot ID/PID starttime 核对遗留子进程，避免杀死复用 PID 的正常程序。存在不明活跃进程或所有权不匹配时停止，绝不扩大删除范围。

```bash
python3 scripts/codex-probes/keepalive.py \
  --url https://gateway.example/v1 --model YOUR_MODEL \
  --state-dir /home/probe/.local/state/axonhub-probes/any --cleanup-only
```

脚本所有权目录不得放置你的正常会话，勿用 `~/.codex` 作为 state-dir。相同 state-dir 有文件锁，不允许第二个实例同时操作。

## 依据与验证

- 官方 [non-interactive 模式](https://developers.openai.com/codex/noninteractive)：`exec`、JSONL 完成事件与 `--ephemeral`。
- [Codex 配置](https://developers.openai.com/codex/config-reference) 与 [0.155.1 exec CLI](https://github.com/openai/codex/blob/rust-v0.155.1/codex-rs/exec/src/cli.rs)、[JSONL 事件](https://github.com/openai/codex/blob/rust-v0.155.1/codex-rs/exec/src/exec_events.rs)、[配置 schema](https://github.com/openai/codex/blob/rust-v0.155.1/codex-rs/core/config.schema.json)。检索日期 2026-09-19。
- 简单题主题参考 NASA [天空为什么是蓝色](https://spaceplace.nasa.gov/blue-sky/) 与 USGS [水循环](https://www.usgs.gov/water-science-school/water-cycle)，问题为重新编写，没有复制“十万个为什么”书籍内容。
- GitHub Actions 用 fake CLI 验证新会话、清理、超时/中断、错误分类、阶段切换、计数、定时去重和不触碰外部文件；不安装/执行真实 Codex，不调用真实上游。离线测试不能证明提供方额度窗口行为或封禁策略。
