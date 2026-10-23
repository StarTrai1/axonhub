# agent 原会话续接失败：#65430–#65433

2026-10-23 只读现场诊断。运行 EXE 内嵌版本为上一轮发布的 `4a693d17`（modified=true）。

四条失败不是同一线程：#65430–#65432为 gpt-6.1-sol，#65433为此前AxonHub线程的 gpt-6-astra。共同的上游错误为旧会话项属于不同资源。AxonHub已有恢复入口仅接受固定Azure错误原文，遗漏这次relay附加的request id和资源脱敏名称，因此未进入相应恢复。

| 请求 | 北京时间 | 上游结果 | 出站历史 |
|---|---|---|---|
| #65430 | 11:33:50 | 首次502；随后400资源不匹配，附request id与trace id | 289项、130 reasoning、1原生checkpoint、67对工具 |
| #65431 | 11:40:27 | 400，相同资源拒绝格式 | 同一会话289项 |
| #65432 | 11:48:43 | 400，相同资源拒绝格式 | 同一会话289项 |
| #65433 | 11:51:18 | 400，资源名称改写成the upstream service | 228项、52 reasoning、无原生checkpoint、72对工具 |
| #65434 | 11:53:13 | 新线程同agent渠道成功，首token约5.29秒 | 入站6项，无原生checkpoint或reasoning |

三条带previous_response_id的入站请求均已在出站前恢复为显式历史；出站没有该字段，工具均完整配对。第一个请求的502重试正文相同；其后400不是相同密文已被网关修复后再次失败。原生checkpoint三层来源均存在且认证读取成功，末层不再有checkpoint。数据仅能证明relay报告资源不匹配，无法确定relay内部Azure资源/账号如何变更。

修复共享的资源不匹配识别：允许已观察到的 `the upstream service` 替代名称，以及严格的 `(request id: 标识)` 和 `[trace_id=标识]` 后缀。继续要求完整错误句式和已有HTTP400、错误码、参数、完整历史检查。有checkpoint进入已有保留源摘要恢复；无checkpoint先移除已物化项的可选ID，若仍明确拒绝则进入已有reasoning恢复。原线程、工具call_id/结果、用户内容与调用预算保持原约束。

[OpenAI conversation state文档](https://developers.openai.com/api/docs/guides/conversation-state#manually-manage-conversation-state)要求普通无状态续接携带完整输出项，包括reasoning。因此本轮保持正常续接原样，只补明确资源拒绝后的错误格式识别，不采用普遍删除历史状态的策略。

新增11条识别正反例与8种管线组合，覆盖HTTP/流内错误、有无checkpoint、原线程续用、正文与工具完整性及凭据保留。现有预算/隔离/缺失源保护继续适用。验证仅在GitHub CI，尚未得到结果；不宣称生产原会话已经恢复或供应商必定接受摘要。

生产数据库、配置和服务未修改，没有真实模型探测。原始正文留在私有临时证据目录，仅脱敏证据进入报告。发布进度待CI更新。
