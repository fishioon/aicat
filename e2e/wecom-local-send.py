import argparse
import concurrent.futures
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import signal
import socket
import sqlite3
import subprocess
import time
import urllib.error
import urllib.request

spec = importlib.util.spec_from_file_location('local_fixture', Path(__file__).with_name('wecom-local.py'))
fixture = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fixture)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', default='bin/aicat-cli-e2e')
    parser.add_argument('--output')
    args = parser.parse_args()
    os.umask(0o077)
    root = Path(args.output or ('build/wecom-local-send-' + time.strftime('%Y%m%d-%H%M%S'))).resolve()
    root.mkdir(parents=True, exist_ok=False)
    home, target = root / 'home', root / 'database'
    home.mkdir()
    target.mkdir()
    binary = Path(args.binary).resolve()
    report = {'passed': False, 'checks': [], 'command': ['python3', __file__, '--binary', str(binary), '--output', str(root)], 'coverage': 'Real MCP authentication and message services, Bun host, stock SQLite3MC, encrypted DB/WAL and Room storage; native UI execution replaced at the script boundary. No native UI or real messages.', 'binarySHA256': hashlib.sha256(binary.read_bytes()).hexdigest()}
    process = None
    run = 0
    executor = concurrent.futures.ThreadPoolExecutor(max_workers=3)
    with socket.socket() as listener:
        listener.bind(('127.0.0.1', 0))
        port = listener.getsockname()[1]
    (home/'aicat.yaml').write_text('gateway:\n  addr: 127.0.0.1:' + str(port) + '\nsystemProxy:\n  enabled: false\nextensions:\n  traffic:\n    enabled: false\n  wecom-local:\n    enabled: true\n    local:\n      accountId: synthetic-account\n      dataDir: ' + json.dumps(str(target)) + '\n')
    (home/'credentials').mkdir()
    (home/'credentials/wecom-local.json').write_text(json.dumps({'accountId':'synthetic-account','dataDir':str(target),'key':fixture.BASE64_KEY}))
    requests = root/'script-requests.jsonl'
    mode = root/'script-mode'
    mode.write_text('normal')
    script = root/'fake-send'
    script.write_text('#!/usr/bin/python3\nimport json,sys,time\nfrom pathlib import Path\nroot=Path('+repr(str(root))+')\nrequest=json.load(sys.stdin)\nwith (root/"script-requests.jsonl").open("a") as f:f.write(json.dumps(request)+"\\n")\nmode=(root/"script-mode").read_text()\nif mode=="hold":time.sleep(60)\nif mode=="fail":sys.exit(23)\nprint("submitted")\n')
    script.chmod(0o700)
    plain = root/'plain-fixture.db'
    db = sqlite3.connect(plain)
    db.executescript('PRAGMA page_size=4096; PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE message_table(message_id INTEGER PRIMARY KEY,server_id INTEGER,sequence INTEGER,sender_id INTEGER,conversation_id TEXT,content_type INTEGER,send_time INTEGER,flag INTEGER,content BLOB);')
    next_id = 1
    def insert(text, room='test-room', flag=0):
        nonlocal next_id
        db.execute('INSERT INTO message_table VALUES (?,?,?,?,?,?,?,?,?)',(next_id,next_id,next_id,1,room,2,int(time.time()),flag,fixture.proto(text)))
        next_id += 1
        db.commit()
        fixture.publish(plain,target)
    insert('initial incoming')

    def records():
        result=[]
        for file in (home/'rooms').glob('*/messages.jsonl'):
            result.extend(json.loads(line) for line in file.read_text().splitlines())
        return result
    def contexts():
        return [json.loads(line) for file in (home/'rooms').glob('*/context/messages.log') for line in file.read_text().splitlines()]
    def submitted():
        return [json.loads(line) for line in requests.read_text().splitlines()] if requests.exists() else []
    def cursor():
        files=list((home/'state/wecom-local').glob('cursor-*.json'))
        return int(json.loads(files[0].read_text())['position']) if files else 0
    def wait(fn, description, timeout=30):
        deadline=time.monotonic()+timeout
        while time.monotonic()<deadline:
            if process is not None and process.poll() is not None:
                raise AssertionError('aicat exited')
            if fn():return
            time.sleep(.1)
        raise AssertionError('timeout: '+description)
    def check(name, condition=True):
        assert condition,name
        report['checks'].append(name)
        print('PASS',name,flush=True)
    def start():
        nonlocal process,run
        run+=1
        with (root/('serve-'+str(run)+'.log')).open('w') as log:
            process=subprocess.Popen([str(binary),'serve','--home',str(home)],stdout=log,stderr=subprocess.STDOUT,start_new_session=True,env={**os.environ,'AICAT_E2E_BACKGROUND':'1','AICAT_E2E_WECOM_SEND_SCRIPT':str(script)})
        wait(lambda:'aicat ready at' in (root/('serve-'+str(run)+'.log')).read_text(),'app ready')
        wait(lambda:cursor()==next_id-1,'DB import')
        time.sleep(.3)
    def stop(crash=False):
        nonlocal process
        if process:
            os.killpg(process.pid,signal.SIGKILL if crash else signal.SIGTERM)
            code=process.wait(timeout=15)
            process=None
            if not crash:assert code==0
    def event(id,text='MCP reply 中文\n第二行',room='test-room',search='Unique test conversation'):
        return {'specversion':'1.0','id':id,'source':'aicat-agent://mcp','type':'org.aicat.message.send.requested.v1','datacontenttype':'application/json','data':{'action':'send','roomid':room,'search_name':search,'msgtype':'text','text':{'content':text}}}
    def mcp(value,auth=True,channel='wecom-local'):
        body=json.dumps({'jsonrpc':'2.0','id':'test','method':'tools/call','params':{'name':'messages_send','arguments':{'channel':channel,'event':value}}}).encode()
        headers={'Content-Type':'application/json','Accept':'application/json, text/event-stream','MCP-Protocol-Version':'2025-06-18'}
        if auth:headers['Authorization']='Bearer '+(home/'mcp-token').read_text().strip()
        req=urllib.request.Request('http://127.0.0.1:'+str(port)+'/mcp',body,headers)
        try:
            with urllib.request.urlopen(req,timeout=40) as response:
                value=json.load(response)
            assert 'error' not in value,value
            return value['result']
        except urllib.error.HTTPError as error:return {'httpStatus':error.code}
    def receipt(result):
        return result.get('structuredContent') or json.loads(result['content'][0]['text'])
    def send_confirm(value, expected=None):
        count=len(submitted())
        future=executor.submit(mcp,value)
        wait(lambda:len(submitted())==count+1,'script submission')
        assert submitted()[-1]=={'searchName':value['data']['search_name'],'text':value['data']['text']['content']}
        insert(expected if expected is not None else value['data']['text']['content'],flag=2)
        result=receipt(future.result(timeout=30))
        wait(lambda:cursor()==next_id-1,'outgoing local echo')
        return result
    try:
        start()
        check('missing MCP authentication cannot execute sender',mcp(event('unauth'),False)['httpStatus']==401 and not submitted())
        for bad in [event('missing-name',search=''),event('unknown-room',room='unknown'),event('oversized',text='x'*4001),event('not-bot')]:
            result=mcp(bad,channel='wecom' if bad['id']=='not-bot' else 'wecom-local')
            assert result.get('isError'),result
        check('missing search name, unknown conversation, oversized text and disabled BOT are rejected before UI work',not submitted())
        value=event('confirmed')
        result=send_confirm(value)
        check('authenticated MCP reaches script with exact target and Unicode/newline text; encrypted WAL local echo confirms sent',result['status']=='sent' and result['result']['confirmation']=='local_database' and result['result']['messageId']=='2')
        assert receipt(mcp(value))==result
        assert mcp(event('confirmed','changed')).get('isError')
        assert mcp(event('confirmed',search='Changed target name')).get('isError')
        check('same request returns original receipt; conflicting content never executes script twice',len(submitted())==1)
        wait(lambda:len(contexts())==2,'authoritative context')
        check('pending and sent journals do not duplicate local database outgoing context',len([r for r in records() if r.get('status')=='pending'])==1 and len([r for r in records() if r.get('status')=='sent'])==1 and len(contexts())==2)
        stop()
        start()
        assert receipt(mcp(value))==result
        insert('incoming after sender restart')
        wait(lambda:cursor()==next_id-1,'reader replay with outgoing journals')
        check('restart preserves send receipt and reader replay tolerates outbound journal entries',len(submitted())==1 and len(contexts())==3)
        mode.write_text('fail')
        failed=event('script-failed','script failure')
        assert receipt(mcp(failed))['status']=='unknown'
        count=len(submitted())
        assert receipt(mcp(failed))['status']=='unknown'
        check('script failure produces durable unknown without retry',len(submitted())==count and len(contexts())==3)
        mode.write_text('normal')
        unconfirmed=event('no-echo','echo in wrong Room')
        count=len(submitted())
        future=executor.submit(mcp,unconfirmed)
        wait(lambda:len(submitted())==count+1,'unconfirmed submission')
        insert('echo in wrong Room',room='other-room',flag=2)
        unknown=receipt(future.result(timeout=30))
        wait(lambda:cursor()==next_id-1,'other Room import')
        check('script completion or identical text in another Room cannot confirm delivery',unknown['status']=='unknown')
        assert receipt(mcp(unconfirmed))['status']=='unknown'
        assert len(submitted())==count+1
        mode.write_text('hold')
        held=event('interrupted','interrupted send')
        count=len(submitted())
        future=executor.submit(mcp,held)
        wait(lambda:len(submitted())==count+1,'held send')
        stop(crash=True)
        try:future.result(timeout=5)
        except Exception:pass
        mode.write_text('normal')
        start()
        check('crash during native submission recovers pending as unknown without another script run',receipt(mcp(held))['status']=='unknown' and len(submitted())==count+1)
        final=send_confirm(event('after-crash','sender recovered'))
        check('sender and incremental reader continue after crash recovery',final['status']=='sent')
        count=len(submitted())
        first=executor.submit(mcp,event('parallel-first','parallel first'))
        wait(lambda:len(submitted())==count+1,'first concurrent submission')
        second=executor.submit(mcp,event('parallel-second','parallel second'))
        time.sleep(.5)
        assert len(submitted())==count+1
        insert('parallel first',flag=2)
        assert receipt(first.result(timeout=30))['status']=='sent'
        wait(lambda:len(submitted())==count+2,'second serialized submission')
        insert('parallel second',flag=2)
        assert receipt(second.result(timeout=30))['status']=='sent'
        wait(lambda:cursor()==next_id-1,'concurrent local echoes')
        check('concurrent MCP requests serialize script execution through confirmation')
        journal=next(p for p in (home/'rooms').glob('*/messages.jsonl') if any(json.loads(line)['event']['data']['roomid']=='test-room' for line in p.read_text().splitlines()))
        journal.chmod(0o400)
        count=len(submitted())
        blocked=event('blocked-journal','must not submit')
        try:
            assert mcp(blocked).get('isError')
            assert len(submitted())==count
        finally:
            journal.chmod(0o600)
        check('Room journal failure prevents UI work and a pending request remains suppressed',receipt(mcp(blocked))['status']=='unknown' and len(submitted())==count)
        check('private receipts omit database credentials and BOT remains uninstalled',all((p.stat().st_mode&0o777)==0o600 and fixture.BASE64_KEY not in p.read_text() for p in (home/'messages/receipts').glob('*.json')) and not (home/'credentials/wecom.json').exists() and not (home/'extensions/wecom').exists())
        check('external MCP send does not create Agent tasks',not list(home.glob('rooms/*/tasks/*')))
        stop()
        for log in root.glob('serve-*.log'):
            assert fixture.BASE64_KEY not in log.read_text()
        report['scriptCalls']=len(submitted())
        report['passed']=True
    except Exception as error:
        report['error']=repr(error)
        raise
    finally:
        stop()
        executor.shutdown(wait=True,cancel_futures=True)
        db.close()
        (root/'report.json').write_text(json.dumps(report,indent=2)+'\n')
        print(root)

if __name__=='__main__':main()
