# 执行锚点

- 分支：sync/upstream-peers-20261022；起点 dev/origin/dev 9a4d06f0797ba915fe505c5cd4eb4d41e65f0e4d。
- 固定上游范围 cbdf9a6d4a3dab1e4bfe5168ce5d80205c805913..48984732291eba882d7d4d964c814e2e783311a7，9 条提交。
- YCE 搜索已完整通过 gate，/tmp/yce-results/yce-search-20261022T094325-1076115-d8fa9e.xml；32172 bytes；a8aaf606cc8b7abfd28a9cc0ed1306ab87e780a58da7207fdb8f74ff34eb6bd7；无任务锚点。
- 只允许 GitHub CI 测试/编译/lint/格式化/生成/tidy；本地只编辑、Git 与 diff 检查。
- 保留原未跟踪 probe/。不重启服务、不写生产 DB/config、不付费推理探测、不装 EXE。
- 保留 9a4d06f0 检查点来源修复；保留 tools 400 修复与容量耗尽不在同渠道重发；已完成旧 PR7 不重做。
- 全部合并冲突已处理；新增设置 patch 字段、流写预算和缓存计费缺口修复。生成代码将在 CI 重生成后回收 artifact。
- 后续：PR → CI 合格 → dev 快进到实际测试 SHA → annotated v1.0.0-unstable.YYYYMMDD.sha8 → 观察 Run GoReleaser in_progress 后停止。重复 dev 测试仅在核对 SHA 一致后取消。

- PR #8: https://github.com/StarTrai1/axonhub/pull/8 。第二轮行为/构建检查通过，格式化产物已回收；下一步提交格式化，验证最终 SHA，dev 快进/tag/release。
