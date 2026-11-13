import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import time
import urllib.request
import uuid


def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--home',required=True)
    parser.add_argument('--room',required=True)
    parser.add_argument('--search-name',required=True)
    parser.add_argument('--output',required=True)
    args=parser.parse_args()
    os.umask(0o077)
    home,root=Path(args.home).resolve(),Path(args.output).resolve()
    root.mkdir(parents=True,exist_ok=False)
    credential=json.loads((home/'credentials/wecom-local.json').read_text())
    token=(home/'mcp-token').read_text().strip()
    source='aicat-channel://wecom-local/'+hashlib.sha256((credential['accountId']+'\0'+os.path.normpath(credential['dataDir'])).encode()).hexdigest()
    room=home/'rooms'/hashlib.sha256(('wecom-local\0'+source+'\0'+args.room).encode()).hexdigest()
    assert (room/'messages.jsonl').is_file(),'the target must already exist in the client Room store'
    marker='aicat MCP 客户端回复验证 '+datetime.datetime.now().strftime('%Y%m%d-%H%M%S')
    event={'specversion':'1.0','id':'live-reply-'+uuid.uuid4().hex,'source':'aicat-agent://mcp','type':'org.aicat.message.send.requested.v1','datacontenttype':'application/json','data':{'action':'send','roomid':args.room,'search_name':args.search_name,'msgtype':'text','text':{'content':marker}}}
    (root/'request.json').write_text(json.dumps({'channel':'wecom-local','event':event},ensure_ascii=False,indent=2)+'\n')
    report={'passed':False,'coverage':'Production installed app, authenticated HTTP MCP, bundled AppleScript, real WeCom UI and DB/WAL echo, Room event/context storage. Local DB confirmation does not establish recipient delivery or read status.','requestId':event['id'],'checks':[]}
    def check(name,condition):
        assert condition,name
        report['checks'].append(name)
    def call():
        raw=json.dumps({'jsonrpc':'2.0','id':uuid.uuid4().hex,'method':'tools/call','params':{'name':'messages_send','arguments':{'channel':'wecom-local','event':event}}}).encode()
        request=urllib.request.Request('http://127.0.0.1:9981/mcp',raw,{'Content-Type':'application/json','Accept':'application/json, text/event-stream','MCP-Protocol-Version':'2025-06-18','Authorization':'Bearer '+token})
        with urllib.request.urlopen(request,timeout=40) as response:result=json.load(response)
        if 'error' in result:raise AssertionError('MCP protocol error')
        result=result['result']
        if result.get('isError'):raise AssertionError('MCP send was rejected')
        return result.get('structuredContent') or json.loads(result['content'][0]['text'])
    try:
        receipt=call()
        (root/'receipt.json').write_text(json.dumps(receipt,ensure_ascii=False,indent=2)+'\n')
        report['receiptStatus']=receipt['status']
        check('real MCP call reaches client script and confirms a new outgoing local DB message',receipt['status']=='sent' and receipt.get('result',{}).get('confirmation')=='local_database')
        check('identical request returns the original receipt without another native submission',call()==receipt)
        local_id=receipt['result']['messageId']
        deadline=time.monotonic()+15
        while time.monotonic()<deadline:
            context=room/'context/messages.log'
            messages=[json.loads(line) for line in context.read_text().splitlines()] if context.exists() else []
            found=[m for m in messages if m.get('local_id')==local_id]
            if found:break
            time.sleep(.25)
        check('Room context contains the exact outgoing text once with its local ID and outgoing flag',len(found)==1 and found[0]['text']['content']==marker and found[0].get('outgoing') is True)
        records=[json.loads(line) for line in (room/'messages.jsonl').read_text().splitlines()]
        sent=[r for r in records if r['event']['id']==event['id'] and r.get('status')=='sent']
        check('Room journal records one confirmed send for this request',len(sent)==1)
        report['localMessageId']=local_id
        report['passed']=True
    except Exception as error:
        report['error']=str(error)
        raise
    finally:
        (root/'report.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
        print(json.dumps(report,ensure_ascii=False))

if __name__=='__main__':main()
