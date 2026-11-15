const fs=require('node:fs'),path=require('node:path'),readline=require('node:readline'),assert=require('node:assert/strict');
const root=path.dirname(process.argv[1]);
const trace=x=>fs.appendFileSync(path.join(root,'codex-trace.jsonl'),JSON.stringify({...x,pid:process.pid})+'\n');
const emit=x=>process.stdout.write(JSON.stringify(x)+'\n');
const threads=new Map(),active=new Map(),calls=new Map();let seq=0,call=100;
function parse(text){return JSON.parse(text.split('trigger_messages:\n')[1].split('\nrequesters:\n')[0]);}
function finish(thread){const t=active.get(thread);if(!t)return;emit({method:'item/completed',params:{threadId:thread,turnId:t.id,item:{type:'agentMessage',id:'ordinary',phase:'final_answer',text:'ordinary output must not be published'}}});emit({method:'turn/completed',params:{threadId:thread,turn:{id:t.id,status:'completed'}}});active.delete(thread);}
function send(thread){
 const t=active.get(thread);if(!t)return;
 const m=t.messages.at(-1),gates=path.join(root,'codex-gates.json');
 if(fs.existsSync(gates)&&JSON.parse(fs.readFileSync(gates)).includes(m.msgid)){t.timer=setTimeout(()=>send(thread),50);return;}
 if(m.text.content==='ordinary-only')return finish(thread);
 if(m.text.content.includes('hold-agent'))return;
 const id=++call;const args={event:{text:'正式回复 '+m.msgid,reply_to:m.msgid}};
 if(m.text.content==='forged-target')args.event.roomid='another-room';
 calls.set(id,{thread,phase:'publish',args});
 emit({id,method:'item/tool/call',params:{threadId:thread,turnId:t.id,tool:'rooms_reply',arguments:args}});
}
readline.createInterface({input:process.stdin}).on('line',line=>{
 const m=JSON.parse(line),p=m.params;trace(m);
 if(!m.method){
  const c=calls.get(m.id);calls.delete(m.id);if(!c)return;const t=active.get(c.thread);if(!t)return;
  const text=t.messages.at(-1).text.content;
  if(c.phase==='publish'&&m.result?.success&&text.startsWith('send:')){
   const receipt=JSON.parse(m.result.contentItems.find(x=>x.type==='inputText')?.text||m.result.contentItems[0].text);
   const id=++call;calls.set(id,{thread:c.thread,phase:'send'});
   return emit({id,method:'item/tool/call',params:{threadId:c.thread,turnId:t.id,tool:'messages_send',arguments:{event:{...c.args.event,reply_id:receipt.id}}}});
  }
  return finish(c.thread);
 }
 if(m.method==='initialize')return emit({id:m.id,result:{userAgent:'fixture'}});
 if(m.method==='initialized')return;
 if(m.method==='model/list')return emit({id:m.id,result:{data:p.cursor?[{model:'room-b',displayName:'Room B',isDefault:false}]:[{model:'room-a',displayName:'Room A',isDefault:true}],nextCursor:p.cursor?null:'page2'}});
 if(['thread/start','thread/resume'].includes(m.method)){
  assert(p.developerInstructions.includes('rooms_reply')&&p.developerInstructions.includes('messages_send'));
  if(p.model==='bad-model')return emit({id:m.id,error:{code:-1,message:'MODEL_UNAVAILABLE'}});
  const id=p.threadId||'thread-'+process.pid+'-'+ ++seq;threads.set(id,p);
  return emit({id:m.id,result:{thread:{id},model:p.model||'room-a'}});
 }
 if(m.method==='thread/inject_items')return emit({id:m.id,result:{}});
 if(m.method==='turn/start'){
  const messages=parse(p.input[0].text),history=fs.readFileSync(path.join(threads.get(p.threadId).cwd,'context/messages.log'),'utf8').trim().split('\n').map(JSON.parse);
  for(const message of messages)assert(history.some(x=>x.msgid===message.msgid));
  const id='turn-'+ ++seq;active.set(p.threadId,{id,messages});emit({id:m.id,result:{turn:{id}}});
  if(p.model==='rerouted')return setTimeout(()=>emit({method:'model/rerouted',params:{threadId:p.threadId,turnId:id,fromModel:p.model,toModel:'room-a',reason:'fixture'}}),30);
  return setTimeout(()=>send(p.threadId),80);
 }
 if(m.method==='turn/steer'){const t=active.get(p.threadId);assert(t&&t.id===p.expectedTurnId);t.messages.push(...parse(p.input[0].text));emit({id:m.id,result:{}});return setTimeout(()=>send(p.threadId),80);}
 if(m.method==='turn/interrupt'){clearTimeout(active.get(p.threadId)?.timer);active.delete(p.threadId);return emit({id:m.id,result:{}});}
 if(m.method==='thread/unsubscribe'){threads.delete(p.threadId);return emit({id:m.id,result:{status:'unsubscribed'}});}
 emit({id:m.id,result:{}});
});
