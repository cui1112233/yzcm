package prompt

// This accompanies, and never changes, the frozen 138 body.
const Storyboard138Continuation = `【按已保存来源编号续接】本次只处理当前场景剩余的script_content。source_elements必须使用这些真实element_id，从第一个尚未覆盖的元素开始按顺序连续覆盖。动作拆镜可复用同一编号，但不能跳号、虚构编号或只重复旧结果。衔接上下文仅供参考，不能再次处理。每次只返回一批；剩余内容由服务器根据编号核算，自检报告里的概述不作为剩余原文。`
