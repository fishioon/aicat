const fs=require('node:fs'),path=require('node:path'),readline=require('node:readline');
const root=path.dirname(process.argv[1]);
const trace=x=>fs.appendFileSync(path.join(root,'codex-trace.jsonl'),JSON.stringify({...x,pid:process.pid})+'\n');
const emit=x=>process.stdout.write(JSON.stringify({...x,pid:process.pid})+'\n');
const active=new Map(),calls=new Map();let seq=0,call=100;
const policyFile=path.join(root,'codex-policies.json');
const policies=fs.existsSync(policyFile)?JSON.parse(fs.readFileSync(policyFile,'utf8')):{};
const savePolicies=()=>fs.writeFileSync(policyFile,JSON.stringify(policies));
function parse(text){return JSON.parse(text.slice('trigger_messages:\n'.length).split('\nrequesters:\n')[0]);}
function send(thread){
 const t=active.get(thread);if(!t)return;
 const m=t.messages[t.messages.length-1];
 if(m.text.content.includes('hold-agent'))return;
 const id=++call;calls.set(id,thread);
 const args={event:{text:'自动回复 '+m.text.content}};
 if(policies[thread]==='legacy'||['legacy-event','hybrid-event','forged-legacy','conflicting-body'].includes(m.text.content)){
  const text=args.event.text;
  args.event={specversion:'1.0',id:'model-generated-id',source:'aicat-agent://mcp',type:'org.aicat.message.send.requested.v1',datacontenttype:'application/json',data:{action:'send',roomid:m.roomid,msgtype:'text',text:{content:text}}};
  if(m.text.content==='hybrid-event')args.event.text=text;
  if(m.text.content==='forged-legacy')args.event.data.roomid='another-room';
  if(m.text.content==='conflicting-body')args.event.text='different text';
 }
 if(m.text.content==='forged-target')args.event.roomid='another-room';
 if(m.text.content==='empty-reply')args.event.text='';
 trace({kind:'send_arguments',id,arguments:args});
 emit({id,method:'item/tool/call',params:{threadId:thread,turnId:t.id,tool:'messages_send',arguments:args}});
}
readline.createInterface({input:process.stdin}).on('line',line=>{
 const m=JSON.parse(line),p=m.params;trace(m);
 if(!m.method){
  const thread=calls.get(m.id),t=active.get(thread);calls.delete(m.id);
  emit({method:'item/completed',params:{threadId:thread,turnId:t.id,item:{type:'agentMessage',id:'ordinary',phase:'final_answer',text:'ordinary output must not be forwarded'}}});
  emit({method:'turn/completed',params:{threadId:thread,turn:{id:t.id,status:'completed'}}});active.delete(thread);return;
 }
 if(m.method==='initialize')return emit({id:m.id,result:{userAgent:'fixture'}});
 if(m.method==='initialized')return;
 if(['thread/start','thread/resume'].includes(m.method)){
  if(!p.developerInstructions.includes('messages_send({event:{text:'))throw Error('missing compact reply policy');
  const id=p.threadId||'thread-'+path.basename(p.cwd);
  if(m.method==='thread/start'){policies[id]='compact';savePolicies();}
  return emit({id:m.id,result:{thread:{id}}});
 }
 if(m.method==='thread/inject_items'){
  if(!p.items.some(item=>item.role==='developer'&&item.content.some(c=>c.text.includes('messages_send({event:{text:'))))throw Error('missing instruction update');
  policies[p.threadId]='compact';savePolicies();return emit({id:m.id,result:{}});
 }
 if(m.method==='turn/start'){
  const id='turn-'+ ++seq;active.set(p.threadId,{id,messages:parse(p.input[0].text)});emit({id:m.id,result:{turn:{id}}});return setTimeout(()=>send(p.threadId),100);
 }
 if(m.method==='turn/steer'){
  const t=active.get(p.threadId);if(!t||t.id!==p.expectedTurnId)return emit({id:m.id,error:{code:-1,message:'no active turn'}});
  t.messages.push(...parse(p.input[0].text));emit({id:m.id,result:{}});return setTimeout(()=>send(p.threadId),100);
 }
 if(m.method==='turn/interrupt'){active.delete(p.threadId);return emit({id:m.id,result:{}});}
 emit({id:m.id,result:{}});
});
