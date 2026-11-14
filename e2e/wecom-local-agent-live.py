from pathlib import Path
import json,hashlib,time,datetime,os,argparse
os.umask(0o077)
parser=argparse.ArgumentParser()
parser.add_argument('--home',required=True)
parser.add_argument('--room',required=True)
parser.add_argument('--output',required=True)
parser.add_argument('--after-position',type=int)
parser.add_argument('--duration',type=int,default=600)
args=parser.parse_args()
root=Path(args.output).resolve();root.mkdir(parents=True,exist_ok=False)
home=Path(args.home).resolve();room_id=args.room
c=json.loads((home/'credentials/wecom-local.json').read_text())
source='aicat-channel://wecom-local/'+hashlib.sha256((c['accountId']+'\0'+os.path.normpath(c['dataDir'])).encode()).hexdigest()
room=home/'rooms'/hashlib.sha256(('wecom-local\0'+source+'\0'+room_id).encode()).hexdigest()
baseline=int(json.loads((home/'state/wecom-local'/('automation-'+hashlib.sha256(source.encode()).hexdigest()+'.json')).read_text())['position'])
cursor_files=list((home/'state/wecom-local').glob('cursor-*.json'))
since=args.after_position if args.after_position is not None else max((int(json.loads(p.read_text())['position']) for p in cursor_files),default=baseline)
report={'passed':False,'baseline':str(baseline),'validationAfter':str(since),'checks':[],'samples':[],'coverage':'Real incoming group message, installed Codex Agent, aicat HTTP MCP, native client script, DB/WAL and Room echo. No manual outbound MCP calls from the observer.'}
start=time.monotonic();stable=None;last=None
try:
 while time.monotonic()-start<args.duration:
  tasks=[json.loads(p.read_text()) for p in (room/'tasks').glob('*.json')]
  tasks=[t for t in tasks if int(t['trigger'].get('local_id','0'))>since]
  records=[json.loads(x) for x in (room/'messages.jsonl').read_text().splitlines()]
  earliest=min((t['createdAt'] for t in tasks),default='~')
  sent_events={r['event']['id']:r['event'] for r in records if r.get('status')=='sent' and r['recordedAt']>=earliest and r['event']['source']=='aicat-agent://mcp'}
  receipts=[]
  for p in (home/'messages/receipts').glob('*.json'):
   r=json.loads(p.read_text())['receipt']
   if r.get('result',{}).get('chatId')==room_id and r.get('source')=='aicat-agent://mcp' and r['id'] in sent_events:receipts.append(r)
  contexts=[json.loads(x) for x in (room/'context/messages.log').read_text().splitlines()]
  replies=[m for m in contexts if int(m.get('local_id','0'))>since and m.get('outgoing')]
  bad_self=any(t['trigger'].get('outgoing') or t['trigger'].get('from')==c['accountId'] for t in tasks)
  sample={'at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'tasks':[{'id':t['id'],'messageId':t['trigger'].get('local_id'),'status':t['status'],'error':t.get('error','')[:700]} for t in tasks],'mcpSent':sum(r['status']=='sent' for r in receipts),'outgoingRows':len(replies),'selfTrigger':bad_self}
  report['samples'].append(sample)
  key=json.dumps({k:v for k,v in sample.items() if k!='at'},sort_keys=True)
  if key!=last:print(json.dumps(sample,ensure_ascii=False),flush=True);last=key
  if any(t['status'] in ['failed','unknown'] for t in tasks):report['attention']='Agent task did not complete';break
  completed=[t for t in tasks if t['status']=='completed']
  matched=receipts and all(r['status']=='sent' and sum(m.get('text',{}).get('content')==sent_events[r['id']]['data']['text']['content'] for m in replies)==1 for r in receipts)
  if completed and len(completed)==len(tasks) and matched and not bad_self:
   if stable is None:stable=time.monotonic()
   if time.monotonic()-stable>=12:
    report.update({'passed':True,'taskCount':len(tasks),'sentReplies':len(receipts),'incomingIds':[t['trigger']['local_id'] for t in tasks],'outgoingIds':[m['local_id'] for m in replies if any(m.get('text',{}).get('content')==sent_events[r['id']]['data']['text']['content'] for r in receipts)],'checks':['new non-self group message automatically created a persistent Agent task','real Codex task completed','reply obtained a sent receipt from aicat MCP','reply appeared once in the Room context through DB/WAL','own outgoing echo did not create an Agent task','no manual send performed by observer']});break
  else:stable=None
  (root/'automatic-report.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
  time.sleep(2)
finally:
 if not report['passed'] and not report.get('attention') and not any(x['tasks'] for x in report['samples']):report['status']='waiting_for_incoming_message'
 report['observedSeconds']=round(time.monotonic()-start,2)
 (root/'automatic-report.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
 print(json.dumps({k:v for k,v in report.items() if k!='samples'},ensure_ascii=False),flush=True)
