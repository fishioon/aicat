import argparse,importlib.util,json,os,signal,socket,sqlite3,subprocess,threading,time,hashlib,shlex
from pathlib import Path
spec=importlib.util.spec_from_file_location('send_fixture',Path(__file__).with_name('wecom-local-send.py'))
base=importlib.util.module_from_spec(spec);spec.loader.exec_module(base)

def main():
 p=argparse.ArgumentParser();p.add_argument('--binary',default='bin/aicat-cli-e2e');p.add_argument('--output');args=p.parse_args()
 os.umask(0o077)
 root=Path(args.output or ('build/wecom-local-agent-'+time.strftime('%Y%m%d-%H%M%S'))).resolve();root.mkdir(parents=True,exist_ok=False)
 home=root/'home';target=root/'database';home.mkdir();target.mkdir()
 binary=Path(args.binary).resolve();runtime=binary.parent/'channel-host'
 with socket.socket() as listener:listener.bind(('127.0.0.1',0));port=listener.getsockname()[1]
 fake=root/'codex-local-agent.cjs';fake.write_bytes(Path(__file__).with_name('fixtures').joinpath('codex-local-agent.cjs').read_bytes())
 codex=root/'codex';codex.write_text('#!/bin/sh\nexec '+shlex.quote(str(runtime/'bun'))+' '+shlex.quote(str(fake))+' "$@"\n');codex.chmod(0o700)
 script=root/'send-script';script.write_text('#!/usr/bin/python3\nimport json,sys\nfrom pathlib import Path\np=Path('+repr(str(root/'submissions.jsonl'))+')\nwith p.open("a") as f:f.write(json.dumps(json.load(sys.stdin))+"\\n")\n');script.chmod(0o700)
 (home/'aicat.yaml').write_text('gateway:\n  addr: 127.0.0.1:'+str(port)+'\nsystemProxy:\n  enabled: false\nextensions:\n  traffic:\n    enabled: false\n  wecom-local:\n    enabled: true\n    local:\n      accountId: "9007199254740995"\n      dataDir: '+json.dumps(str(target))+'\n      replyRooms:\n        auto-room: "Auto test"\n    agent:\n      enabled: true\n      binary: '+json.dumps(str(codex))+'\n')
 (home/'credentials').mkdir();(home/'credentials/wecom-local.json').write_text(json.dumps({'accountId':'9007199254740995','dataDir':str(target),'key':base.fixture.BASE64_KEY}))
 plain=root/'plain.db';db=sqlite3.connect(plain,check_same_thread=False);lock=threading.RLock()
 db.executescript('PRAGMA page_size=4096;PRAGMA journal_mode=WAL;PRAGMA wal_autocheckpoint=0;CREATE TABLE message_table(message_id INTEGER PRIMARY KEY,server_id INTEGER,sequence INTEGER,sender_id INTEGER,conversation_id TEXT,content_type INTEGER,send_time INTEGER,flag INTEGER,content BLOB);')
 sequence=0
 def insert(text,room='auto-room',own=False,age=0,typ=0):
  nonlocal sequence
  with lock:
   sequence+=1;id=sequence
   db.execute('INSERT INTO message_table VALUES (?,?,?,?,?,?,?,?,?)',(id,id,id,9007199254740995 if own else 9007199254740997,room,typ,int(time.time())-age,83886080,base.native_proto(text)))
   db.commit();base.fixture.publish(plain,target);return str(id)
 insert('recent history must not trigger')
 def lines(file):
  if not file.exists():return []
  data=file.read_text();return [json.loads(x) for x in data[:data.rfind('\n')+1].splitlines()]
 def submissions():return lines(root/'submissions.jsonl')
 def trace():return lines(root/'codex-trace.jsonl')
 def tasks():return [json.loads(f.read_text()) for f in home.glob('rooms/*/tasks/*.json')]
 def cursor():
  files=list((home/'state/wecom-local').glob('cursor-*.json'));return json.loads(files[0].read_text()) if files else {}
 def wait(fn,label,seconds=25):
  deadline=time.monotonic()+seconds
  while time.monotonic()<deadline:
   if process is not None and process.poll() is not None:raise AssertionError('aicat exited')
   if fn():return
   time.sleep(.1)
  raise AssertionError('timeout: '+label)
 def completed(id,status='completed'):return any(t['trigger'].get('local_id')==id and t['status']==status for t in tasks())
 process=None;run=0;done=threading.Event();release_echo=threading.Event();errors=[]
 def writer():
  handled=0
  while not done.wait(.05):
   try:
    rows=submissions()
    for row in rows[handled:]:
     assert row['searchName']=='Auto test'
     if 'delayed-echo' in row['text'] and not release_echo.wait(25):raise AssertionError('delayed echo was not released')
     insert(row['text'],own=True);handled+=1
   except Exception as e:errors.append(repr(e));return
 thread=threading.Thread(target=writer);thread.start()
 def start():
  nonlocal process,run
  run+=1
  with (root/('serve-'+str(run)+'.log')).open('w') as log:
   process=subprocess.Popen([str(binary),'serve','--home',str(home)],stdout=log,stderr=subprocess.STDOUT,start_new_session=True,env={**os.environ,'AICAT_E2E_BACKGROUND':'1','AICAT_E2E_WECOM_SEND_SCRIPT':str(script)})
  wait(lambda:'aicat ready at' in (root/('serve-'+str(run)+'.log')).read_text(),'app ready')
  wait(lambda:int(cursor().get('position',-1))==sequence,'import caught up')
 def stop():
  nonlocal process
  if process:
   process.send_signal(signal.SIGTERM);code=process.wait(timeout=20);process=None;assert code==0
 report={'passed':False,'checks':[],'binarySHA256':hashlib.sha256(binary.read_bytes()).hexdigest(),'command':['python3',__file__,'--binary',str(binary),'--output',str(root)],'coverage':'New client automation only: real encrypted DB/WAL, Room/task services, app-server protocol fixture, authenticated aicat HTTP MCP and script boundary fixture. No native UI or real messages.'}
 def check(name,condition=True):assert condition,name;report['checks'].append(name);print('PASS',name,flush=True)
 try:
  start();initial=cursor().copy();time.sleep(.5)
  check('first activation imports recent history without starting Agent',not tasks() and not trace())
  insert('own echo',own=True);insert('old incoming',age=700);insert('other Room',room='unconfigured');insert('media',typ=29)
  wait(lambda:int(cursor()['position'])==sequence,'ignored messages');time.sleep(.3)
  check('own, stale, non-text and unconfigured Room messages never trigger Agent',not tasks() and not submissions())
  first=insert('auto-one');wait(lambda:completed(first),'automatic MCP reply')
  wait(lambda:len(submissions())==1 and int(cursor()['position'])==sequence,'own reply read')
  check('new Room message automatically reaches Agent and explicit tool calls aicat MCP reply',submissions()[0]['text']=='自动回复 auto-one' and any(x.get('result',{}).get('success') is True for x in trace()))
  call=next(x for x in trace() if x.get('kind')=='send_arguments');task=next(t for t in tasks() if t['trigger'].get('local_id')==first)
  journal=[json.loads(x) for f in home.glob('rooms/*/messages.jsonl') for x in f.read_text().splitlines()]
  event=next(r['event'] for r in journal if r.get('status')=='sent')
  check('Agent provides only text; aicat builds the CloudEvent, bound destination and stable tool-call ID',call['arguments']=={'event':{'text':'自动回复 auto-one'}} and event['id']=='agent-'+hashlib.sha256((task['id']+'\0'+str(call['id'])).encode()).hexdigest() and event['source']=='aicat-agent://mcp' and event['specversion']=='1.0' and event['type']=='org.aicat.message.send.requested.v1' and event['datacontenttype']=='application/json' and event['data']['roomid']=='auto-room' and event['data']['search_name']=='Auto test')
  time.sleep(.8);check('ordinary Agent output and own reply never create another send or task',len(tasks())==1 and len(submissions())==1)
  stop();file=next((home/'state/wecom-local').glob('cursor-*.json'));file.write_text(json.dumps(initial));start();time.sleep(.7)
  check('replayed delivery and restart keep completed tasks terminal',len(tasks())==1 and len(submissions())==1)
  held=insert('hold-agent');wait(lambda:completed(held,'running'),'active task')
  follow=insert('release-agent');wait(lambda:completed(held) and completed(follow),'steered messages')
  check('active Room uses existing turn/steer with full trigger messages',any(x.get('method')=='turn/steer' and 'release-agent' in json.dumps(x,ensure_ascii=False) for x in trace()) and len(submissions())==2)
  token=(home/'mcp-token').read_text();(home/'mcp-token').write_text('wrong-token\n')
  failed=insert('auth-error');wait(lambda:completed(failed,'unknown'),'MCP rejection')
  check('automatic reply really traverses MCP authentication; rejected calls do not execute sender',len(submissions())==2)
  (home/'mcp-token').write_text(token)
  recovered=insert('auth-recovered');wait(lambda:completed(recovered),'MCP recovered')
  check('new messages continue after an uncertain task without replaying it',len(submissions())==3 and completed(failed,'unknown'))
  stop();start();time.sleep(.5);check('restart never reruns unknown tasks',len(submissions())==3)
  directory=next(f.parent for f in home.glob('rooms/*/tasks/*.json') if json.loads(f.read_text())['room']=='auto-room')
  kept=directory.with_name('tasks-kept');directory.rename(kept);directory.write_text('blocked')
  before=int(cursor()['position']);count=len(submissions());blocked=insert('after-storage-repair')
  try:
   time.sleep(1)
   check('task storage failure withholds source cursor and never starts Agent',int(cursor()['position'])==before and len(submissions())==count)
  finally:directory.unlink();kept.rename(directory)
  wait(lambda:completed(blocked),'task storage recovery')
  check('unacknowledged delivery recovers into exactly one durable task',sum(t['trigger'].get('local_id')==blocked for t in tasks())==1 and len(submissions())==count+1)
  held=insert('hold-agent-crash');wait(lambda:completed(held,'running'),'held task')
  count=len(submissions());pids={r['pid'] for r in trace()}
  os.killpg(process.pid,signal.SIGKILL);process.wait(timeout=10);process=None
  for pid in pids:
   try:os.killpg(pid,signal.SIGKILL)
   except ProcessLookupError:pass
  start();time.sleep(.3)
  check('a running Agent task recovers as unknown after process crash without another reply',completed(held,'unknown') and len(submissions())==count)
  final=insert('after-crash');wait(lambda:completed(final),'post-crash task')
  check('the resumed Room processes a new message after crash without replaying history',len(submissions())==count+1)
  before=len(tasks());delayed=insert('delayed-echo');began=time.monotonic();wait(lambda:completed(delayed),'script completion before database echo',seconds=10)
  report['taskCompletionBeforeEchoMs']=round((time.monotonic()-began)*1000,2)
  contexts=lambda:[json.loads(x) for f in home.glob('rooms/*/context/messages.log') for x in f.read_text().splitlines()]
  check('Agent task completes from the MCP script receipt while its outgoing DB echo is withheld',not any(m.get('outgoing') and m['text']['content']=='自动回复 delayed-echo' for m in contexts()) and len(tasks())==before+1)
  release_echo.set();wait(lambda:any(m.get('outgoing') and m['text']['content']=='自动回复 delayed-echo' for m in contexts()),'later outgoing echo')
  time.sleep(.5)
  check('later database echo enters Room exactly once and never reruns the completed task',sum(m.get('outgoing',False) and m['text']['content']=='自动回复 delayed-echo' for m in contexts())==1 and len(tasks())==before+1 and completed(delayed))
  count=len(submissions())
  for text in ['forged-target','empty-reply','forged-legacy','conflicting-body']:
   invalid=insert(text);wait(lambda:completed(invalid,'failed'),'invalid reply arguments rejected')
  check('invalid reply and legacy target injection fail the task without reaching the MCP sender',len(submissions())==count and sum(x.get('result',{}).get('success') is False for x in trace())>=4)
  for text in ['legacy-event','hybrid-event']:
   legacy=insert(text);wait(lambda:completed(legacy),'legacy reply normalized')
  check('legacy and hybrid event inputs remain compatible while host-generated IDs replace model IDs',len(submissions())==count+2 and not any(json.loads(p.read_text())['receipt']['id']=='model-generated-id' for p in (home/'messages/receipts').glob('*.json')))
  stop();binding=next(home.glob('rooms/*/agent.json'));state=json.loads(binding.read_text());thread_id=state['threadId'];state.pop('instructionsHash',None);binding.write_text(json.dumps(state))
  policies=json.loads((root/'codex-policies.json').read_text());policies[thread_id]='legacy';(root/'codex-policies.json').write_text(json.dumps(policies))
  before=len(trace());start();upgraded=insert('upgraded-policy');wait(lambda:completed(upgraded),'existing thread instruction update')
  new_trace=trace()[before:]
  check('existing threads receive updated developer instructions once and retain their thread binding',json.loads(binding.read_text())['threadId']==thread_id and bool(json.loads(binding.read_text())['instructionsHash']) and sum(x.get('method')=='thread/inject_items' for x in new_trace)==1 and next(x for x in new_trace if x.get('kind')=='send_arguments')['arguments']=={'event':{'text':'自动回复 upgraded-policy'}})
  stop();before=len(trace());start();unchanged=insert('unchanged-policy');wait(lambda:completed(unchanged),'instruction version unchanged')
  check('restart with unchanged instructions does not append another developer instruction',not any(x.get('method')=='thread/inject_items' for x in trace()[before:]))
  assert not errors,errors
  report['tasks']=len(tasks());report['automaticReplies']=len(submissions());report['passed']=True
 finally:
  stop();done.set();release_echo.set();thread.join();db.close();(root/'report.json').write_text(json.dumps(report,indent=2)+'\n');print(root)
if __name__=='__main__':main()
