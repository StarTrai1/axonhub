# 执行锚点

- 分支fix/agent-cache-recovery-65643，基线f87de72d67f3dfb657bdfc2eddd6230489d2fc9d，原probe/不动。
- 仅CI构建/测试/lint/格式化/生成；本地只读、编辑、Git、数据分析。无付费生产探测/配置/DB写入/重启/EXE安装。
- YCE gate=true，/tmp/yce-results/yce-search-20261023T071933-1558189-1398cd.xml已分段读到EOF，35353bytes，e40c8d67016afdd4b1b0d3c47b8762b2942dc91827a7faa0836f198bf3279106，无任务锚点。
- Windows只读snapshot：C:\Users\DDD\AppData\Local\Temp\axonhub-65643-cache\snapshot.db，855207936bytes，quick_check=ok，max_request65650。完成审计后按精确路径+大小删除。
- 私有证据/tmp/axonhub-65643-cache，mode700；audit.py/initial-evidence.json，正文不进Git/CI。复用vendor/tmp/axonhub-64535-reform/vendor。
- 生产已安装上轮f87de72d。#65643不是未升级，错误格式已识别，增量checkpoint绑定漏过。
- Jev一次0.99选择narrow_native_first，收据/tmp/axonhub-65643-cache/jev-response.json。规则明确，无rules Markdown候选。没有子代理。
- OpenAI官方缓存文档已读取；agentrouter官网/API公告被WAF挡住，第三方转载公告未当技术结论证据。
- 改动含delta来源、泛化资源ID修正、策略范围、ID变更不学成reasoning删除。新API六场景涵盖HTTP/WS及后续显式/泛化checkpoint拒绝。
- 交付：PR CI全部过→精确测试SHA快进dev→annotated unstable日期sha标签→GoReleaser in_progress停止。gh必须-R StarTrai1/axonhub。
