package prompt

// Keep continuation instructions beside the recovered system prompts. The
// recovered body is immutable; this is the server's independent wire contract.
const ScriptBatchContinuation = `
【服务器分批续接合同】
只转换本次提供的章节原文。每次只输出一批，不自行等待或模拟用户继续。
必须输出 batch_info.is_complete（布尔值）、current_batch（本次指定批号），以及 unprocessed_script_rollover（字符串）。即使一次完成也必须提供这些字段。
场景号从本次指定的下一场开始连续递增；element_id 按 场景号-三位序号 从001连续递增。episode_index 必须保持指定剧集。
未完成时，rollover 必须为本次原文逐字不改的非空后缀，长度严格减少，从完整句子的开头开始，不可在句子中间切断。完成时 rollover 必须为空。
只输出一个JSON对象。可附原件规定的转换自检、无损死锁双向核对和暴力脱敏核对报告，不得附加其他内容。
`
