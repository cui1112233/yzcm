import Taro from "@tarojs/taro";
import { Button, Input, Text, Textarea, View } from "@tarojs/components";
import { useEffect, useRef, useState } from "react";
import { deletePersonalPrompt, listPersonalPrompts, savePersonalPrompt, type PersonalPrompt } from "../../services/scriptProduction";

export default function ScriptPersonalPrompts({ category, onSelect }: { category: string; onSelect: (prompt: PersonalPrompt) => void }) {
  const [list, setList] = useState<PersonalPrompt[]>([]), [editor, setEditor] = useState<PersonalPrompt | null>(null), [error, setError] = useState(""), [busy, setBusy] = useState(false);
  const ticket = useRef(0), locked = useRef(false);
  const load = async () => { const current = ++ticket.current; setBusy(true); setError(""); try { const result = await listPersonalPrompts(category); if (ticket.current === current) setList(result); } catch (cause: any) { if (ticket.current === current) setError(cause.message); } finally { if (ticket.current === current) setBusy(false); } };
  useEffect(() => { void load(); return () => { ticket.current += 1; }; }, [category]);
  const act = async (action: () => Promise<PersonalPrompt[]>) => {
    if (locked.current) return; locked.current = true; const current = ++ticket.current; setBusy(true); setError("");
    try { const next = await action(); if (current === ticket.current) { setList(next); setEditor(null); } }
    catch (cause: any) { if (current === ticket.current) setError(cause.message); }
    finally { locked.current = false; if (current === ticket.current) setBusy(false); }
  };
  return <View className="mp-card">
    <Text className="mp-row-title">我的提示词</Text>
    {error ? <Text className="mp-error">{error}</Text> : null}
    <Button className="mp-secondary" disabled={busy} onClick={() => void load()}>重新读取</Button>
    {list.map(prompt => <View key={prompt.id}><Text className="mp-row-title">{prompt.name}</Text><Text className="mp-row-description">{prompt.body}</Text>
      <Button disabled={busy} onClick={() => onSelect(prompt)}>使用这条提示词</Button><Button disabled={busy} onClick={() => setEditor(prompt)}>编辑</Button>
      <Button disabled={busy} onClick={async () => { if ((await Taro.showModal({ title: "删除提示词", content: `删除“${prompt.name}”？` })).confirm) void act(() => deletePersonalPrompt(prompt)); }}>删除</Button>
    </View>)}
    {!busy && !list.length ? <Text className="mp-row-description">此分类尚无个人提示词。</Text> : null}
    <Button disabled={busy} onClick={() => setEditor({ category, name: "", body: "" })}>新增提示词</Button>
    {editor ? <View><Input className="mp-input" value={editor.name} maxlength={80} placeholder="名称" disabled={busy} onInput={event => setEditor({ ...editor, name: event.detail.value })} /><Textarea className="mp-textarea" value={editor.body} maxlength={-1} placeholder="提示词正文" disabled={busy} onInput={event => setEditor({ ...editor, body: event.detail.value })} /><Button className="mp-primary" disabled={busy} onClick={() => void act(() => savePersonalPrompt(editor))}>保存并核对</Button><Button disabled={busy} onClick={() => setEditor(null)}>取消</Button></View> : null}
  </View>;
}
