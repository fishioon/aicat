import argparse,hashlib,importlib.util,json,os,shlex,signal,socket,sqlite3,subprocess,threading,time
from pathlib import Path

spec=importlib.util.spec_from_file_location('send_fixture',Path(__file__).with_name('wecom-local-send.py'));base=importlib.util.module_from_spec(spec);spec.loader.exec_module(base)
def vint(value):
 out=bytearray()
 while value>=128:out.append((value&127)|128);value>>=7
 out.append(value);return bytes(out)
def field(number,data):return vint(number*8+2)+vint(len(data))+data
def content(text,mentions):
 out=b''
 for id,label in mentions:out+=field(1,b'\x08\x05'+field(2,b'\x08'+vint(int(id))+field(2,label.encode())))
 return out+base.native_proto(text)

def main():
 p=argparse.ArgumentParser();p.add_argument('--binary',default='bin/aicat-cli-e2e');p.add_argument('--output');a=p.parse_args();os.umask(0o077)
 root=Path(a.output or 'build/wecom-local-triggers-'+time.strftime('%Y%m%d-%H%M%S')).resolve();root.mkdir(parents=True,exist_ok=False)
 home=root/'home';target=root/'database';home.mkdir();target.mkdir();binary=Path(a.binary).resolve();runtime=binary.parent/'channel-host'
 account='9007199254740995';peer='9007199254740997';direct='S:'+account+'_'+peer
 routes={'R:100':'Group','R:200':'All mentions','R:300':'All text','R:400':'Keyword only','R:500':'Disabled','unknown-room':'Unknown',direct:'Direct','S:'+account+'_9007199254740998':'Disabled direct'}
 rules={room:{'direct':False,'mention':False,'mentionAll':False,'all':False,'keywords':[]} for room in routes}
 rules['R:100'].update(mention=True,keywords=['wake','助手'])
 rules['R:200']['mentionAll']=True;rules['R:300']['all']=True;rules['R:400']['keywords']=['wake'];rules['unknown-room']['keywords']=['wake'];rules[direct]['direct']=True
 with socket.socket() as listener:listener.bind(('127.0.0.1',0));port=listener.getsockname()[1]
 config={'gateway':{'addr':'127.0.0.1:'+str(port)},'systemProxy':{'enabled':False},'extensions':{'traffic':{'enabled':False},'wecom-local':{'enabled':True,'local':{'accountId':account,'dataDir':str(target),'replyRooms':routes,'triggers':rules},'agent':{'enabled':True,'binary':str(root/'codex')}}}}
 (home/'aicat.yaml').write_text(json.dumps(config));(home/'credentials').mkdir();(home/'credentials/wecom-local.json').write_text(json.dumps({'accountId':account,'dataDir':str(target),'key':base.fixture.BASE64_KEY}))
 (root/'codex-local-agent.cjs').write_bytes(Path(__file__).with_name('fixtures').joinpath('codex-local-agent.cjs').read_bytes())
 (root/'codex').write_text('#!/bin/sh\nexec '+shlex.quote(str(runtime/'bun'))+' '+shlex.quote(str(root/'codex-local-agent.cjs'))+' "$@"\n');(root/'codex').chmod(0o700)
 script=root/'send-script';script.write_text('#!/usr/bin/python3\nimport json,sys\nfrom pathlib import Path\nwith Path('+repr(str(root/'submissions.jsonl'))+').open("a") as f:f.write(json.dumps(json.load(sys.stdin))+"\\n")\n');script.chmod(0o700)
 plain=root/'plain.db';db=sqlite3.connect(plain,check_same_thread=False);lock=threading.RLock();seq=0
 db.executescript('PRAGMA page_size=4096;PRAGMA journal_mode=WAL;PRAGMA wal_autocheckpoint=0;CREATE TABLE message_table(message_id INTEGER PRIMARY KEY,server_id INTEGER,sequence INTEGER,sender_id INTEGER,conversation_id TEXT,content_type INTEGER,send_time INTEGER,flag INTEGER,content BLOB);')
 def insert(text,room='R:100',mentions=(),own=False,age=0,typ=0,raw=None,sender=None):
  nonlocal seq
  with lock:
   seq+=1;id=str(seq)
   db.execute('INSERT INTO message_table VALUES (?,?,?,?,?,?,?,?,?)',(seq,seq,seq,int(sender or (account if own else peer)),room,typ,int(time.time())-age,83886080,raw if raw is not None else content(text,mentions)))
   db.commit();base.fixture.publish(plain,target);return id
 for room in routes:insert('initial history',room=room)
 initial=seq;identity=hashlib.sha256((account+'\0'+str(target)).encode()).hexdigest();state=home/'state/wecom-local';state.mkdir(parents=True)
 (state/('cursor-'+identity+'.json')).write_text(json.dumps({'version':1,'identity':identity,'position':'0','anchor':''}))
 process=None;run=0;stop_writer=threading.Event();errors=[]
 def lines(file):
  if not file.exists():return []
  raw=file.read_text();return [json.loads(x) for x in raw[:raw.rfind('\n')+1].splitlines()]
 def submissions():return lines(root/'submissions.jsonl')
 def tasks():return [json.loads(f.read_text()) for f in home.glob('rooms/*/tasks/*.json')]
 def contexts():return [x for f in home.glob('rooms/*/context/messages.log') for x in lines(f)]
 def cursor():
  f=state/('cursor-'+identity+'.json');return int(json.loads(f.read_text())['position'])
 def completed(id):return any(t['trigger'].get('local_id')==id and t['status']=='completed' for t in tasks())
 def wait(fn,label,seconds=25):
  deadline=time.monotonic()+seconds
  while time.monotonic()<deadline:
   if process and process.poll() is not None:raise AssertionError('aicat exited')
   if fn():return
   time.sleep(.05)
  raise AssertionError('timeout: '+label)
 def writer():
  done=0;reverse={name:room for room,name in routes.items()}
  while not stop_writer.wait(.05):
   try:
    for row in submissions()[done:]:insert(row['text'],room=reverse[row['searchName']],own=True);done+=1
   except Exception as e:errors.append(repr(e));return
 thread=threading.Thread(target=writer);thread.start()
 def start():
  nonlocal process,run
  run+=1
  with (root/('serve-'+str(run)+'.log')).open('w') as log:process=subprocess.Popen([str(binary),'serve','--home',str(home)],stdout=log,stderr=subprocess.STDOUT,start_new_session=True,env={**os.environ,'AICAT_E2E_BACKGROUND':'1','AICAT_E2E_WECOM_SEND_SCRIPT':str(script)})
  wait(lambda:'aicat ready at' in (root/('serve-'+str(run)+'.log')).read_text(),'ready');wait(lambda:cursor()==seq,'source import')
 def stop():
  nonlocal process
  if process:os.killpg(process.pid,signal.SIGTERM);assert process.wait(timeout=20)==0;process=None
 report={'passed':False,'checks':[],'binarySHA256':hashlib.sha256(binary.read_bytes()).hexdigest(),'coverage':'Real encrypted DB/WAL, TS message metadata parser, per-conversation triggers, durable Room/tasks, Codex protocol fixture, authenticated HTTP MCP and script boundary fixture. No native UI or real sends.','command':['python3',__file__,'--binary',str(binary),'--output',str(root)]}
 def check(name,ok=True):assert ok,name;report['checks'].append(name);print('PASS',name,flush=True)
 def ignored(name,**kwargs):
  before=len(tasks());id=insert(name,**kwargs);wait(lambda:cursor()>=int(id),'ignored input');time.sleep(.15);assert len(tasks())==before,name
  assert any(m['local_id']==id for m in contexts()),name
  return id
 def triggers(name,reason,**kwargs):
  before=len(tasks());id=insert(name,**kwargs);wait(lambda:completed(id),'trigger '+name)
  incoming=next(m for m in contexts() if m['local_id']==id)
  wait(lambda:any(m.get('outgoing') and m['roomid']==incoming['roomid'] and m['text']['content']=='自动回复 '+incoming['text']['content'] for m in contexts()),'reply echo');wait(lambda:cursor()==seq,'source caught up')
  matched=[t for t in tasks() if t['trigger'].get('local_id')==id];assert len(matched)==1 and len(tasks())==before+1 and matched[0]['triggerReason']==reason,(name,matched)
  return next(m for m in contexts() if m['local_id']==id)
 try:
  start();check('legacy cursor migration preserves original historical fields and imports history without tasks',not tasks() and all(m['room_type']=='unknown' and 'mention_status' not in m for m in contexts()) and json.loads((state/('message-format-'+identity+'.json')).read_text())['metadataAfter']==str(initial))
  ignored('ordinary group text');ignored('@AI CAD literal text');ignored('mention another account',mentions=[(peer,'AI CAD')]);ignored('everyone stays off',mentions=[('0','所有人')]);ignored('wake in unknown type',room='unknown-room');ignored('unconfigured',room='R:999');ignored('direct disabled',room='S:'+account+'_9007199254740998')
  check('unmatched group text, literal names, other accounts, @all-off, unknown types and unconfigured or disabled direct messages enter Room without Agent work',not tasks() and not submissions())
  m=triggers('private request','direct',room=direct);check('configured private chat starts exactly one Agent task',m['room_type']=='direct')
  m=triggers('mention current account','mention',mentions=[(account,'AI CAD')]);check('native mention segment uses numeric account ID and preserves the visible text',m['mention_ids']==[account] and m['mention_status']=='parsed' and m['text']['content']=='@AI CADmention current account')
  triggers('wAkE case insensitive','keyword');check('keyword matching ignores case')
  triggers('wake 助手 two matches','keyword');check('multiple matching keywords create one task')
  triggers('wake plus mention','mention',mentions=[(account,'AI CAD')]);check('mention and keyword OR semantics still create one task')
  m=triggers('all enabled here','mention_all',room='R:200',mentions=[('0','所有人')]);check('@all has an independent per-conversation switch and does not masquerade as a personal mention',m['mention_all'] and not m.get('mention_ids'))
  triggers('test all text','all',room='R:300');check('explicit test mode processes all new text')
  ignored('mention disabled',room='R:400',mentions=[(account,'AI CAD')]);triggers('WAKE keyword-only','keyword',room='R:400');check('conversation rules are independent and keyword-only mode is supported')
  ignored('wake disabled everything',room='R:500',mentions=[(account,'AI CAD')]);check('a rule with every switch off creates no tasks')
  ignored('unrecognized payload',raw=base.fixture.proto('literal @'+account+' is not metadata'));check('unsupported protobuf does not turn literal account digits into a mention')
  triggers('fallback keyword','keyword',raw=base.fixture.proto('WAKE legacy text encoding'));check('keyword matching remains available when mention metadata cannot be parsed')
  ignored('wake own echo',own=True,mentions=[(account,'AI CAD')]);ignored('wake stale',age=700);ignored('wake future',age=-120);ignored('wake image',typ=29);ignored('wake missing sender',sender='0')
  check('self, stale, future, non-text and missing-sender filters still precede trigger matching')
  before=len(tasks());calls=len(submissions());stop();file=state/('cursor-'+identity+'.json');saved=json.loads(file.read_text());saved.update(position='0',anchor='');file.write_text(json.dumps(saved));start();time.sleep(.5)
  check('replay and restart retain trigger decisions, completed tasks and legacy metadata without another reply',len(tasks())==before and len(submissions())==calls and cursor()==seq)
  check('each accepted request follows MCP once and outgoing echoes never trigger Agent',sum(t['trigger'].get('outgoing',False) for t in tasks())==0 and len(tasks())==len(submissions()) and not errors)
  for log in root.glob('serve-*.log'):assert base.fixture.BASE64_KEY not in log.read_text()
  report['tasks']=len(tasks());report['scriptCalls']=len(submissions());report['passed']=True
 finally:
  stop();stop_writer.set();thread.join(timeout=5);db.close();(root/'report.json').write_text(json.dumps(report,indent=2)+'\n');print(root)
if __name__=='__main__':main()
