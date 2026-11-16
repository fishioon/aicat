const fs=require('node:fs'),path=require('node:path'),net=require('node:net'),http=require('node:http'),crypto=require('node:crypto');
const {spawn,execFileSync}=require('node:child_process'),assert=require('node:assert/strict');
const {chromium}=require(process.env.AICAT_PLAYWRIGHT||'playwright');
const realAgent=process.argv[4]==='real-agent';
const binary=path.resolve(process.argv[2]||'bin/aicat-e2e'),root=path.resolve(process.argv[3]||'build/schedules-'+Date.now());
if(fs.existsSync(root))throw Error('Use a fresh output directory');
fs.mkdirSync(root,{recursive:true,mode:0o700});const home=path.join(root,'home');fs.mkdirSync(home,{mode:0o700});
const report={passed:false,checks:[],command:process.argv,binarySHA256:crypto.createHash('sha256').update(fs.readFileSync(binary)).digest('hex'),coverage:'Real Bun NPM installation, stdio channel protocol, Room storage/dispatch, authenticated HTTP MCP and headless UI. Actual minute boundaries verify cron triggering and offline skips; no clock or cursor fixtures. Platform and Codex app-server are protocol fixtures; no native UI or live delivery.'};
const pause=ms=>new Promise(r=>setTimeout(r,ms));
async function wait(fn,timeout=25000){let last;for(const end=Date.now()+timeout;Date.now()<end;){try{const value=await fn();if(value)return value;}catch(e){last=e;}await pause(70);}throw Error('timeout '+(last?.stack||fn));}
async function port(){const s=net.createServer();await new Promise(r=>s.listen(0,'127.0.0.1',r));const p=s.address().port;await new Promise(r=>s.close(r));return p;}
const check=(name,condition=true)=>{assert(condition,name);report.checks.push(name);console.log('PASS',name);};
const rows=file=>fs.existsSync(file)?fs.readFileSync(file,'utf8').split('\n').filter(Boolean).map(JSON.parse):[];
let proc,browser,server,preview,token,base,log,registry,page;
const transcript=[];
async function rpc(method,...args){const r=await fetch(new URL('/rpc',preview),{method:'POST',headers:{'Content-Type':'application/json','X-Aicat-Preview':token},body:JSON.stringify({method:'Desktop.'+method,args})});const v=await r.json();if(!r.ok)throw Error(JSON.stringify(v));return v;}
async function start(){const f=path.join(root,'preview-url');fs.rmSync(f,{force:true});proc=spawn(binary,['--home',home],{env:{...process.env,AICAT_E2E_BACKGROUND:'1',AICAT_E2E_OS_STATE:path.join(root,'os-state.json'),AICAT_E2E_PREVIEW:f},stdio:['ignore',log,log]});await wait(()=>fs.existsSync(f));preview=fs.readFileSync(f,'utf8').trim();token=new URL(preview).searchParams.get('token');await wait(async()=> (await rpc('State')).gateway.state==='ready');}
async function stop(){if(proc?.exitCode===null){proc.kill('SIGTERM');await wait(()=>proc.exitCode!==null);assert.equal(proc.exitCode,0);}}
const channel='fixture.room-channel';
async function emit(room,id,text=id,extra={}){return rpc('ReadExtensionData',channel,{message:{msgid:id,roomid:room,from:'peer',msgtype:'text',msgtime:Date.now(),text:{content:text},...extra}});}
async function list(){return rpc('ListRooms');}
async function room(conversation){const r=(await list()).find(x=>x.channel===channel&&x.conversation===conversation);assert(r,'missing '+conversation);return rpc('ReadRoomByID',r.id);}
async function configure(conversation,patch){const r=await room(conversation);await rpc('UpdateRoomSettings',r.id,{...r.settings,...patch});return r.id;}
async function completed(conversation,id,status='completed'){return (await room(conversation)).tasks.some(t=>t.trigger.msgid===id&&t.status===status);}
async function tool(name,args={},auth=true,method='tools/call'){
 const request={jsonrpc:'2.0',id:crypto.randomUUID(),method,params:method==='tools/call'?{name,arguments:args}:{}};
 const response=await fetch(base+'/mcp',{method:'POST',headers:{'Content-Type':'application/json','Accept':'application/json, text/event-stream',...(auth?{Authorization:'Bearer '+fs.readFileSync(path.join(home,'mcp-token'),'utf8').trim()}: {})},body:JSON.stringify(request)});
 const result=response.status===200?await response.json():{httpStatus:response.status};
 transcript.push({request,response:result});return result;
}
const receipt=r=>{assert(!r.error&&!r.result?.isError,JSON.stringify(r));return r.result?.structuredContent||JSON.parse(r.result.content[0].text);};
const at=delay=>new Date(Date.now()+delay).toISOString();
const spec=(name,delay=4000)=>({name,prompt:'执行 '+name,at:at(delay),enabled:true});
async function set(room_id,id,schedule){return receipt(await tool('schedules_set',{room_id,id,schedule}));}
async function schedules(room_id){return receipt(await tool('schedules_list',{room_id}));}
async function remove(room_id,id){return receipt(await tool('schedules_delete',{room_id,id}));}
const trace=()=>rows(path.join(root,'codex-trace.jsonl'));
async function occurrences(conversation,id){return (await room(conversation)).messages.filter(m=>m.schedule?.schedule_id===id);}
async function delivered(conversation,id){const messages=await occurrences(conversation,id);return messages.length&&await completed(conversation,messages.at(-1).msgid);}
async function agentActions(conversation,id,actions){await emit(conversation,id,'schedule-actions:'+JSON.stringify(actions));await wait(()=>completed(conversation,id));}
(async()=>{try{
 const p=await port();base='http://127.0.0.1:'+p;
 const fake=path.join(root,'codex-room-agent.cjs');fs.copyFileSync('e2e/fixtures/codex-room-agent.cjs',fake);
 const codex=path.join(root,'codex');const q=s=>"'"+s.replaceAll("'","'\\''")+"'";fs.writeFileSync(codex,'#!/bin/sh\nexec '+q(process.execPath)+' '+q(fake)+' "$@"\n',{mode:0o700});
 if(realAgent){
  const realBinary=process.env.AICAT_CODEX_BINARY||execFileSync('which',['codex'],{encoding:'utf8'}).trim();
  const authHome=process.env.CODEX_HOME||path.join(require('node:os').homedir(),'.codex'),isolatedCodex=path.join(root,'codex-home');fs.mkdirSync(isolatedCodex,{mode:0o700});
  const auth=path.join(authHome,'auth.json');if(fs.existsSync(auth))fs.symlinkSync(auth,path.join(isolatedCodex,'auth.json'));
  fs.writeFileSync(path.join(isolatedCodex,'config.toml'),'model = "gpt-6.1-sol"\nmodel_reasoning_effort = "low"\n');
  const bridge=path.join(root,'codex-bridge.cjs');
  fs.writeFileSync(bridge,`const fs=require('node:fs'),readline=require('node:readline'),{spawn}=require('node:child_process');const child=spawn(${JSON.stringify(realBinary)},process.argv.slice(2),{env:{...process.env,CODEX_HOME:${JSON.stringify(isolatedCodex)}},stdio:['pipe','pipe','inherit']});const log=(direction,line)=>{try{const frame=JSON.parse(line);fs.appendFileSync(${JSON.stringify(path.join(root,'real-codex-trace.jsonl'))},JSON.stringify({direction,...frame})+'\\n');}catch{}};readline.createInterface({input:process.stdin}).on('line',line=>{log('request',line);child.stdin.write(line+'\\n')});readline.createInterface({input:child.stdout}).on('line',line=>{log('response',line);process.stdout.write(line+'\\n')});child.on('exit',code=>process.exit(code||0));`);
  fs.writeFileSync(codex,'#!/bin/sh\nexec '+q(process.execPath)+' '+q(bridge)+' "$@"\n',{mode:0o700});
  report.coverage='Real Codex app-server and authenticated model choose dynamic schedule tools from natural-language requests and call real HTTP MCP. Channel is a local NPM protocol fixture; no real platform sends or native UI.';
  report.codexVersion=execFileSync(realBinary,['--version'],{encoding:'utf8'}).trim();report.realCodex=true;report.realModel=true;
 }
 fs.writeFileSync(path.join(home,'aicat.yaml'),JSON.stringify({ui:{language:'zh-Hans',theme:'light'},gateway:{addr:'127.0.0.1:'+p},systemProxy:{enabled:false},extensions:{traffic:{enabled:false}},messageExecution:{codexBinary:codex}}));
 fs.writeFileSync(path.join(root,'os-state.json'),JSON.stringify({services:[],trusted:true,recordProxyWrites:true}));
 const dir=path.join(root,'package');fs.mkdirSync(dir);
 fs.writeFileSync(path.join(dir,'package.json'),JSON.stringify({name:'@fixture/room-channel',version:'1.0.0',type:'module',aicat:{api:1,id:channel,entry:'index.mjs',identityFields:['account'],fields:[{key:'account',type:'string',required:true,label:'账号'}],capabilities:{send:true},room:{mode:'conversation',trigger:{all:true}}}}));
 fs.writeFileSync(path.join(dir,'index.mjs'),`import fs from 'node:fs';import path from 'node:path';export function createChannel(api){return {open(){return {opened:true}},close(){},async observe(event){if(event.query?.message)await api.publish(event.query.message);return {}},send(event){fs.appendFileSync(path.join(api.stateDirectory,'sent.jsonl'),JSON.stringify(event)+'\\n');return {confirmation:'fixture_platform',chatId:event.data.roomid}}}}`);
 const archive=path.join(root,'fixture.tgz');execFileSync('tar',['-czf',archive,'-C',root,'package']);const tgz=fs.readFileSync(archive);
 server=http.createServer((req,res)=>{if(req.url.endsWith('.tgz'))return res.end(tgz);res.setHeader('Content-Type','application/json');res.end(JSON.stringify({name:'@fixture/room-channel','dist-tags':{latest:'1.0.0'},versions:{'1.0.0':{name:'@fixture/room-channel',version:'1.0.0',dist:{tarball:registry+'/fixture.tgz',shasum:crypto.createHash('sha1').update(tgz).digest('hex')}}}}));});
 await new Promise(r=>server.listen(0,'127.0.0.1',r));registry='http://127.0.0.1:'+server.address().port;
 log=fs.openSync(path.join(root,'app.log'),'a');await start();
 await rpc('InstallChannelPackage',{id:channel,name:'定时渠道夹具',package:'@fixture/room-channel',version:'1.0.0',format:'aicat',registry,publisher:'fixture',summary:'定时任务协议验收'});
 await rpc('ConfigureMessageChannel',channel,{accountId:'',secret:'',values:{account:'account-a'}});await rpc('SetExtensionEnabled',channel,true);await wait(async()=> (await rpc('State')).messageChannels[channel].phase==='connected');
 await emit('chat-a','seed-a');await emit('chat-b','seed-b');const a=await room('chat-a'),b=await room('chat-b');
 if(realAgent){
  await configure('chat-a',{automatic:true,prompt:'这是隔离的定时工具验收。只使用 aicat MCP 的 schedules_set、schedules_list、schedules_delete 和 messages_send，不调用 shell 或其他工具，不访问外部消息渠道。发送时使用 source_room.id 作为 target_room_id。按用户要求实际操作，不能只描述步骤。',model:''});
  const realCompleted=id=>wait(async()=>{const task=(await room('chat-a')).tasks.find(t=>t.trigger.msgid===id);if(task&&['failed','unknown'].includes(task.status))throw Error(JSON.stringify(task));return task?.status==='completed';},180000);
  await emit('chat-a','real-create','请为当前 Room 创建一条每日总结规则，id 固定为 real-daily，name 为每日总结，prompt 为总结本 Room 当天的讨论并通过 messages_send 发到 source_room.id，cron 为 0 18 * * *，timezone 为 Asia/Taipei，enabled 为 true。创建后调用 schedules_list 核对，保留规则，然后通过 messages_send 正式回复 REAL_SCHEDULE_CREATED。');
  await realCompleted('real-create');const created=(await schedules(a.id)).find(x=>x.id==='real-daily');assert(created&&created.schedule.enabled&&created.schedule.cron==='0 18 * * *'&&created.schedule.timezone==='Asia/Taipei');assert((await room('chat-a')).messages.some(m=>m.outgoing&&m.text.content.includes('REAL_SCHEDULE_CREATED')));
  check('real Codex model chooses schedules_set and schedules_list from a natural-language Room request and publishes its confirmation through HTTP MCP');
  await emit('chat-a','real-remove','请先用 schedules_set 将 real-daily 这条规则停用（保留它的名称、prompt、cron 和 timezone），再用 schedules_list 核对 enabled 为 false，然后用 schedules_delete 删除它，再调用 schedules_list 确认列表为空。最后通过 messages_send 正式回复 REAL_SCHEDULE_DELETED。');
  await realCompleted('real-remove');assert.equal((await schedules(a.id)).length,0);assert((await room('chat-a')).messages.some(m=>m.outgoing&&m.text.content.includes('REAL_SCHEDULE_DELETED')));
  check('real Codex model pauses, lists and deletes the persisted rule through aicat MCP');report.passed=true;return;
 }
 await configure('chat-a',{automatic:true,model:'room-a'});await configure('chat-b',{automatic:true,model:'room-b'});
 const listed=await tool('',{},true,'tools/list');for(const name of ['schedules_set','schedules_list','schedules_delete'])assert(listed.result.tools.some(t=>t.name===name));
 assert.equal((await tool('schedules_list',{room_id:a.id},false)).httpStatus,401);
 check('public MCP advertises schedule tools and enforces bearer authentication');
 const invalid=[{...spec('invalid'),at:'2026-11-15T12:00:00'}, {...spec('invalid'),at:'2000-01-01T00:00:00Z'}, {...spec('invalid'),cron:'* * * * *',timezone:'Asia/Taipei'}, {name:'invalid',prompt:'x',cron:'* * * * *',enabled:true}, {name:'invalid',prompt:'x',cron:'* * * * * *',timezone:'UTC',enabled:true}, {name:'invalid',prompt:'x',cron:'0 0 31 2 *',timezone:'UTC',enabled:true}, {name:'invalid',prompt:'x',cron:'* * * * *',timezone:'Invalid/Zone',enabled:true}];
 for(const schedule of invalid)assert((await tool('schedules_set',{room_id:a.id,id:'invalid',schedule})).result.isError);
 assert.equal((await schedules(a.id)).length,0);check('invalid dates, missing timezone, impossible cron and conflicting modes never persist');
 const once=spec('单次总结',5000);
 await agentActions('chat-a','create-once',[{name:'schedules_set',arguments:{id:'once',schedule:once}},{name:'schedules_list',arguments:{}}]);
 assert.equal((await schedules(a.id))[0].schedule.name,once.name);assert.equal((await schedules(b.id)).length,0);
 const started=trace().find(x=>x.method==='thread/start');assert(!('dynamicTools' in started.params));const agentCalls=trace().filter(x=>x.method==='mcp_tool_call');for(const name of ['schedules_set','schedules_list'])assert(agentCalls.some(x=>x.name===name));
 check('Room Agent creates and lists schedules through the aicat MCP connection with explicit Room identity');
 await agentActions('chat-a','forged-room',[{name:'schedules_set',arguments:{room_id:b.id,id:'forged',schedule:spec('forged')}}]);assert.equal((await schedules(b.id)).length,0);
 await configure('chat-a',{trigger:{all:false,direct:false,mention:true,mentionAll:false,keywords:[]}});
 await wait(()=>delivered('chat-a','once'));const onceMessage=(await occurrences('chat-a','once'))[0];
 assert.equal(onceMessage.sender_kind,'system');assert.equal(onceMessage.from,'aicat://scheduler');assert.equal(new Date(onceMessage.schedule.scheduled_for).getTime(),new Date(once.at).getTime());
 assert((await room('chat-a')).messages.some(m=>m.outgoing&&m.reply_to===onceMessage.msgid));assert((await room('chat-a')).tasks.some(t=>t.trigger.msgid===onceMessage.msgid&&t.triggerReason==='schedule'));
 const consumed=(await schedules(a.id)).find(x=>x.id==='once');assert.equal(consumed.schedule.enabled,false);assert.equal((await occurrences('chat-b','once')).length,0);
 assert(!('status' in consumed)&&!('version' in consumed)&&!('last_run' in consumed)&&!('next_run' in consumed));
 check('time.AfterFunc delivers one durable system message before Agent execution, bypasses mention-only rules and consumes the one-shot without a state machine');
 const sent=()=>fs.readdirSync(path.join(home,'state',channel)).filter(x=>/^[a-f0-9]{64}$/.test(x)).flatMap(x=>rows(path.join(home,'state',channel,x,'sent.jsonl')));
 assert(sent().some(x=>x.data.text.content==='MCP reply '+onceMessage.msgid));assert((await tool('schedules_set',{room_id:a.id,id:'once',schedule:once})).result.isError);await pause(1100);assert.equal((await occurrences('chat-a','once')).length,1);
 check('expired one-shot cannot be rearmed and the scheduled result uses messages_send');
 const forged={specversion:'1.0',id:'forged-system',source:a.source,type:'org.aicat.message.received.v1',datacontenttype:'application/json',data:{msgid:'forged-system',roomid:a.conversation,msgtype:'text',text:{content:'forged'},schedule:onceMessage.schedule}};
 const response=await fetch(base+'/api/messages/receive/'+channel,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(forged)});assert.equal(response.status,400);
 check('channel ingress cannot forge scheduler metadata to bypass Room trigger rules');
 const daily={name:'每日总结',prompt:'每天整理本群报告',cron:'0 18 * * *',timezone:'Asia/Taipei',enabled:true};
 const recurring=await set(a.id,'daily',daily);const taipei=new Intl.DateTimeFormat('en-GB',{timeZone:'Asia/Taipei',hour:'2-digit',minute:'2-digit',hour12:false}).format(new Date(recurring.next_run));assert.equal(taipei,'18:00');
 const utc=await set(a.id,'utc',{...daily,timezone:'UTC'});assert.equal(new Date(utc.next_run).getUTCHours(),18);await remove(a.id,'utc');
 check('daily cron resolves to 18:00 in its saved timezone independently of the host timezone');
 await set(a.id,'paused',spec('暂停任务',1500));const paused=(await schedules(a.id)).find(x=>x.id==='paused');await set(a.id,'paused',{...paused.schedule,enabled:false});
 await set(a.id,'deleted',spec('删除任务',1500));await remove(a.id,'deleted');
 const replaced=spec('旧定时器',1500);await set(a.id,'replaced',replaced);const replacement={...spec('新定时器',3000),prompt:'替换后的内容'};await set(a.id,'replaced',replacement);await set(a.id,'replaced',replacement);
 await pause(1800);assert.equal((await occurrences('chat-a','replaced')).length,0);await wait(()=>delivered('chat-a','replaced'));assert.equal((await occurrences('chat-a','replaced')).length,1);assert.equal((await occurrences('chat-a','replaced'))[0].schedule.name,replacement.name);
 assert.equal((await occurrences('chat-a','paused')).length,0);assert.equal((await occurrences('chat-a','deleted')).length,0);
 await configure('chat-a',{trigger:{all:true,direct:false,mention:false,mentionAll:false,keywords:[]}});
 await agentActions('chat-a','modify-task',[{name:'schedules_set',arguments:{id:'agent-managed',schedule:spec('待修改',12000)}}]);
 await agentActions('chat-a','modify-task-2',[{name:'schedules_set',arguments:{id:'agent-managed',schedule:{...spec('已修改',12000),enabled:false}}},{name:'schedules_delete',arguments:{id:'agent-managed'}}]);
 assert(!(await schedules(a.id)).some(x=>x.id==='agent-managed'));
 check('MCP and Room Agent replace, pause and delete rules, cancel stale timers and preserve idempotent registration');
 const sentBeforeSchedule= sent().length;await configure('chat-a',{mode:'snapshot'});await set(a.id,'send', {...spec('报告外发',1500),prompt:'send:总结本群并发送报告'});await wait(()=>delivered('chat-a','send'));
 const sendMessage=(await occurrences('chat-a','send'))[0],reply=(await room('chat-a')).messages.find(m=>m.outgoing&&m.reply_to===sendMessage.msgid);assert.equal(reply.delivery.status,'sent');assert.equal(sent().length,sentBeforeSchedule+1);
 const scheduledTask=(await room('chat-a')).tasks.find(t=>t.trigger.msgid===sendMessage.msgid);assert.equal(scheduledTask.settings.mode,'conversation');
 check('scheduled work avoids snapshot supersession and messages_send records one external delivery');
 await configure('chat-a',{mode:'conversation'});await set(a.id,'automatic-off',spec('关闭自动处理',1500));await configure('chat-a',{automatic:false});await pause(1800);
 assert.equal((await schedules(a.id)).find(x=>x.id==='automatic-off').schedule.enabled,false);await configure('chat-a',{automatic:true});await pause(1100);assert.equal((await occurrences('chat-a','automatic-off')).length,0);
 check('automatic processing off skips the one-shot permanently; restoring it does not retry');
 await set(a.id,'channel-off',spec('关闭渠道',1500));await rpc('SetExtensionEnabled',channel,false);await pause(1800);await rpc('SetExtensionEnabled',channel,true);await wait(async()=> (await rpc('State')).messageChannels[channel].phase==='connected');await pause(1100);assert.equal((await occurrences('chat-a','channel-off')).length,0);
 check('disabled channel skips the due one-shot and never replays it when enabled');
 const roomLog=path.join(home,'rooms',a.id,'context/messages.log'),logBackup=roomLog+'.saved';
 await set(a.id,'room-failure',spec('Room 落盘失败',1500));fs.renameSync(roomLog,logBackup);fs.mkdirSync(roomLog);await pause(1800);fs.rmdirSync(roomLog);fs.renameSync(logBackup,roomLog);await pause(1100);assert.equal((await occurrences('chat-a','room-failure')).length,0);assert.equal((await schedules(a.id)).find(x=>x.id==='room-failure').schedule.enabled,false);
 check('Room storage failure starts no Agent work and creates no automatic retry');
 const minute={name:'周期验收',prompt:'周期总结',cron:'* * * * *',timezone:'UTC',enabled:true};
 const minuteSchedule=await set(a.id,'minute',minute),minuteDue=new Date(minuteSchedule.next_run).getTime();
 await set(b.id,'skip-minute',minute);await configure('chat-b',{automatic:false});
 await set(a.id,'paused-minute',{...minute,enabled:false});await set(a.id,'deleted-minute',minute);await remove(a.id,'deleted-minute');
 console.log('Waiting for real cron boundary',new Date(minuteDue).toISOString());await wait(()=>delivered('chat-a','minute'),70000);
 const firstMinute=(await occurrences('chat-a','minute'))[0];assert.equal(new Date(firstMinute.schedule.scheduled_for).getTime(),minuteDue);assert(new Date((await schedules(a.id)).find(x=>x.id==='minute').next_run).getTime()>minuteDue);assert.equal((await occurrences('chat-a','minute')).length,1);
 assert.equal((await occurrences('chat-a','paused-minute')).length,0);assert.equal((await occurrences('chat-a','deleted-minute')).length,0);await configure('chat-b',{automatic:true});await pause(1100);assert.equal((await occurrences('chat-b','skip-minute')).length,0);
 await remove(a.id,'minute');await remove(b.id,'skip-minute');
 check('real cron fires at its calendar boundary, advances itself, respects pause/delete and skips disabled Room work without catch-up');
 await set(a.id,'restart-future',spec('重启后未来触发',6000));await set(a.id,'restart-missed',spec('离线错过',1500));await stop();await pause(1800);await start();await wait(()=>delivered('chat-a','restart-future'));assert.equal((await occurrences('chat-a','restart-missed')).length,0);assert.equal((await occurrences('chat-a','once')).length,1);assert((await schedules(a.id)).some(x=>x.id==='daily'));
 check('restart restores future timers and cron rules, skips expired one-shots and never replays consumed rules');
 const nextMinute=await set(a.id,'offline-minute',minute),missedDue=new Date(nextMinute.next_run).getTime();await stop();console.log('Waiting past offline cron boundary',new Date(missedDue).toISOString());await pause(Math.max(0,missedDue-Date.now())+300);await start();await wait(async()=> (await rpc('State')).messageChannels[channel].phase==='connected');await pause(1100);
 assert.equal((await occurrences('chat-a','offline-minute')).length,0);assert(new Date((await schedules(a.id)).find(x=>x.id==='offline-minute').next_run).getTime()>missedDue);await remove(a.id,'offline-minute');
 check('cron restart schedules the next future boundary without replaying a missed offline occurrence');
 await set(a.id,'account-bound',spec('旧账号任务',2000));await rpc('SetExtensionEnabled',channel,false);await rpc('ConfigureMessageChannel',channel,{accountId:'',secret:'',values:{account:'account-b'}});await rpc('SetExtensionEnabled',channel,true);await wait(async()=> (await rpc('State')).messageChannels[channel].phase==='connected');await emit('chat-a','new-account-seed');await pause(2300);
 const newRoom=(await list()).find(r=>r.channel===channel&&r.conversation==='chat-a'&&r.id!==a.id);assert(newRoom);assert.equal((await rpc('ReadRoomByID',a.id)).messages.filter(m=>m.schedule?.schedule_id==='account-bound').length,0);assert.equal((await schedules(newRoom.id)).length,0);assert.equal((await schedules(a.id)).find(x=>x.id==='account-bound').schedule.enabled,false);await remove(a.id,'account-bound');
 check('account change never delivers old Room work into the new account');
 browser=await chromium.launch({headless:true,executablePath:process.env.AICAT_CHROMIUM||'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'});page=await browser.newPage({viewport:{width:1280,height:1000}});const errors=[];page.on('pageerror',e=>errors.push(e.message));await page.goto(preview);await page.waitForFunction(()=>!!window.desktop?.state());await page.locator('#nav-messages').click();await page.locator('[data-room-id="'+a.id+'"]').click();await page.locator('[data-message-id="'+onceMessage.msgid+'"]').waitFor();assert((await page.locator('#room-messages').innerText()).includes('定时系统消息'));await page.screenshot({path:path.join(root,'scheduled-room.png'),fullPage:true});assert.equal(errors.length,0);
 check('headless message page displays scheduled system messages and linked Agent replies');
 check('only rules are persisted in aicat.yaml; no scheduler state file exists',!fs.existsSync(path.join(home,'state/schedules.json'))&&fs.readFileSync(path.join(home,'aicat.yaml'),'utf8').includes('schedules:'));
 report.passed=true;
 }catch(e){report.error=e.stack;if(page)await page.screenshot({path:path.join(root,'failure.png'),fullPage:true}).catch(()=>{});throw e;}finally{await stop();if(realAgent)fs.rmSync(path.join(root,'codex-home/auth.json'),{force:true});if(browser)await browser.close();if(server)await new Promise(r=>server.close(r));if(log!==undefined)fs.closeSync(log);fs.writeFileSync(path.join(root,'mcp.json'),JSON.stringify(transcript,null,2),{mode:0o600});fs.writeFileSync(path.join(root,'report.json'),JSON.stringify(report,null,2),{mode:0o600});console.log(JSON.stringify({passed:report.passed,checks:report.checks.length,root}));}})().catch(e=>{console.error(e);process.exitCode=1;});
