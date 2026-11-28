import { useCallback, useEffect, useReducer, useRef, useState } from 'react';
import { message } from 'antd';
import { applySessionProposal, changeComicSession, createComicSession, getComicAssistant, listComicSessions, readComicSession, sendSessionMessage } from '../../shared/api/comicScreenplayWorkflow.js';
import {sendReferenceMessage} from '../../shared/api/comicScreenplayAssistantReferences.js';

const defaultSession = {id:'default',title:'默认会话',visibility:'project',mode:'confirm',active:true};
function row(cache, project, session) {
  const key = JSON.stringify([project,session]);
  if (!cache.has(key)) cache.set(key,{text:'',busy:'',state:null,session:null,loading:true,error:'',read:0,operation:0});
  return cache.get(key);
}
export function useComicAssistantSessions(project, initialRequest) {
  const cache = useRef(new Map());
  const [,redraw] = useReducer(n => n + 1, 0);
  const [lists,setLists] = useState({});
  const [selections,setSelections] = useState({});
  const listTickets = useRef(new Map());
  const live = useRef(true);
  useEffect(() => {live.current=true;return () => {live.current=false;};},[]);
  const update = () => {if(live.current) redraw();};
  const selected = selections[project] || 'default';
  const list = lists[project]?.sessions || [defaultSession];
  const current = row(cache.current,project,selected);
  const select = id => setSelections(prev => ({...prev,[project]:id}));
  const activeKey = useRef('');
  const key = JSON.stringify([project,selected]);
  activeKey.current = key;
  const loadList = useCallback(async () => {
    const ticket = (listTickets.current.get(project)||0)+1;
    listTickets.current.set(project,ticket);
    try {
      const response = await listComicSessions(project);
      if (!live.current || listTickets.current.get(project)!==ticket) return;
      const sessions = response?.sessions?.length ? response.sessions : [defaultSession];
      setLists(prev => ({...prev,[project]:{sessions}}));
      setSelections(prev => !prev[project] || sessions.some(s=>s.id===prev[project]) ? prev : {...prev,[project]:'default'});
    } catch (error) {
      if(live.current && listTickets.current.get(project)===ticket) setLists(prev => ({...prev,[project]:{...prev[project],error:error.message||'会话列表读取失败'}}));
    }
  },[project]);
  const refresh = useCallback(async () => {
    const entry = row(cache.current,project,selected), ticket = ++entry.read;
    entry.loading=true; entry.error=''; update();
    try {
      const response = selected==='default' ? await getComicAssistant(project) : await readComicSession(project,selected);
      if(entry.read!==ticket) return;
      const next = selected==='default' ? response.state : {...response, revision:response.session.revision};
      if(!entry.state || next.revision >= entry.state.revision) {entry.state=next;entry.session=response.session;}
    } catch(error) {
      if(entry.read===ticket) {entry.error=error.message||'会话读取失败';entry.state=null;entry.session=null;}
    } finally {if(entry.read===ticket){entry.loading=false;update();}}
  },[project,selected]);
  useEffect(()=>{void loadList();},[loadList]);
  useEffect(()=>{void refresh();},[refresh]);
  useEffect(()=>{
    if(initialRequest?.text) {
      row(cache.current,project,'default').text=initialRequest.text;
      setSelections(prev=>({...prev,[project]:'default'}));update();
    }
  },[project,initialRequest?.id,initialRequest?.text]);
  const metadata = list.find(s => s.id===selected) || defaultSession;
  const session = (current.session?.revision ?? -1) >= (metadata.revision ?? 0) ? current.session : metadata;
  async function write(operation, input, afterSaved) {
    if(current.busy || !current.state || current.error) return;
    const ticket=++current.operation, sentText=current.text, sentReferences=current.references;
    current.busy=operation;
    const expected=current.state.revision||0;
    current.stopRevision=expected+1;
    update();
    try {
      const payload={...input,expected_revision:expected,...(operation==='chat'&&sentReferences?.length?{references:sentReferences}:{})};
      const response=operation==='chat' ? await (payload.references?.length?sendReferenceMessage:sendSessionMessage)(project,selected,payload) : await applySessionProposal(project,selected,payload);
      if(ticket!==current.operation) return;
      ++current.read; current.loading=false;
      current.state=selected==='default' ? response.state : response;
      if(response.warning && live.current && activeKey.current===key) message.warning(response.warning);
      if(operation==='chat' && current.text===sentText) current.text='';
      if(operation==='chat' && current.references===sentReferences) current.references=[];
      if(operation!=='chat' && live.current && activeKey.current===key) await afterSaved?.();
    } catch(error) {
      if(ticket===current.operation) {
        if(live.current && activeKey.current===key) message.error(error.message||'会话操作失败');
        await refresh();
      }
    } finally {
      if(ticket===current.operation) {current.busy=''; update();void loadList();}
    }
  }
  async function manage(action,title) {
    try {
      const expected=action==='stop' && current.busy==='chat' ? current.stopRevision : Math.max(current.state?.revision||0,session.revision||0);
      await changeComicSession(project,selected,action,{expected_revision:expected,...(title?{title}:{})});
      if(action==='stop' || action==='archive') {
        ++current.operation;current.busy='';
      }
      await Promise.all([refresh(),loadList()]);
      return true;
    }catch(error){if(live.current && activeKey.current===key)message.error(error.message||'会话操作失败');await refresh();return false;}
  }
  async function create(input) {
    try{
      const result=await createComicSession(project,input);
      await loadList();
      if(live.current && activeKey.current===key) select(result.session.id);
      return true;
    }catch(error){if(live.current && activeKey.current===key)message.error(error.message||'创建会话失败');return false;}
  }
  return {current,session,list,listError:lists[project]?.error,select,refresh,loadList,manage,create,
    setReferences(refs){current.references=refs;update();},
    setText(text){current.text=text;update();},write};
}
