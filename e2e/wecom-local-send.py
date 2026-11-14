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


def native_proto(text):
    def field(tag, payload):
        n=len(payload)
        size=bytearray()
        while n>=128:
            size.append((n&127)|128)
            n>>=7
        size.append(n)
        return bytes([tag<<3|2])+size+payload
    return field(1,b'\x08\x00'+field(2,field(1,text.encode())))


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
    (home/'aicat.yaml').write_text('gateway:\n  addr: 127.0.0.1:' + str(port) + '\nsystemProxy:\n  enabled: false\nextensions:\n  traffic:\n    enabled: false\n  wecom-local:\n    enabled: true\n    local:\n      accountId: 9007199254740995\n      dataDir: ' + json.dumps(str(target)) + '\n')
    (home/'credentials').mkdir()
    (home/'credentials/wecom-local.json').write_text(json.dumps({'accountId':'9007199254740995','dataDir':str(target),'key':fixture.BASE64_KEY}))
    requests = root/'script-requests.jsonl'
    mode = root/'script-mode'
    mode.write_text('normal')
    script = root/'fake-send'
    script.write_text('#!/usr/bin/python3\nimport json,sys,time\nfrom pathlib import Path\nroot=Path('+repr(str(root))+')\nrequest=json.load(sys.stdin)\nwith (root/"script-requests.jsonl").open("a") as f:f.write(json.dumps(request)+"\\n")\nmode=(root/"script-mode").read_text()\nif mode=="hold":time.sleep(60)\nif mode=="slow":time.sleep(2)\nif mode=="fail":\n print(request["text"]+" "+request["searchName"],file=sys.stderr)\n sys.exit(23)\nprint("submitted")\n')
    script.chmod(0o700)
    plain = root/'plain-fixture.db'
    db = sqlite3.connect(plain)
    db.executescript('PRAGMA page_size=4096; PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE message_table(message_id INTEGER PRIMARY KEY,server_id INTEGER,sequence INTEGER,sender_id INTEGER,conversation_id TEXT,content_type INTEGER,send_time INTEGER,flag INTEGER,content BLOB);')
    next_id = 1
    def insert(text, room='test-room', flag=83886080, typ=0, outgoing=False):
        nonlocal next_id
        db.execute('INSERT INTO message_table VALUES (?,?,?,?,?,?,?,?,?)',(next_id,next_id,next_id,9007199254740995 if outgoing else 9007199254740997,room,typ,int(time.time()),flag,native_proto(text)))
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
            with urllib.request.urlopen(req,timeout=125) as response:
                value=json.load(response)
            assert 'error' not in value,value
            return value['result']
        except urllib.error.HTTPError as error:return {'httpStatus':error.code}
    def receipt(result):
        return result.get('structuredContent') or json.loads(result['content'][0]['text'])
    def send_submit(value):
        count=len(submitted())
        future=executor.submit(mcp,value)
        wait(lambda:len(submitted())==count+1,'script submission')
        assert submitted()[-1]=={'searchName':value['data']['search_name'],'text':value['data']['text']['content']}
        result=receipt(future.result(timeout=5))
        assert result['status']=='sent' and result['result']['confirmation']=='client_script' and 'messageId' not in result['result']
        insert(value['data']['text']['content'],outgoing=True)
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
        began=time.monotonic()
        result=receipt(mcp(value))
        report['scriptReceiptMs']=round((time.monotonic()-began)*1000,2)
        check('authenticated MCP returns sent at script completion without any DB echo or platform message ID',result['status']=='sent' and result['result']['confirmation']=='client_script' and 'messageId' not in result['result'] and report['scriptReceiptMs']<5000 and len(contexts())==1 and submitted()==[{'searchName':value['data']['search_name'],'text':value['data']['text']['content']}])
        assert receipt(mcp(value))==result
        assert mcp(event('confirmed','changed')).get('isError')
        assert mcp(event('confirmed',search='Changed target name')).get('isError')
        check('same request returns original receipt; conflicting content never executes script twice',len(submitted())==1)
        check('script success records pending and sent without fabricating database context',len([r for r in records() if r.get('status')=='pending'])==1 and len([r for r in records() if r.get('status')=='sent'])==1 and len(contexts())==1)
        insert(value['data']['text']['content'],outgoing=True)
        wait(lambda:len(contexts())==2,'authoritative context')
        check('later database echo independently appends the exact outgoing text once',len([m for m in contexts() if m['text']['content']==value['data']['text']['content'] and m.get('outgoing')])==1 and receipt(mcp(value))==result)
        stop()
        start()
        assert receipt(mcp(value))==result
        insert('incoming after sender restart')
        wait(lambda:cursor()==next_id-1,'reader replay with outgoing journals')
        check('restart preserves send receipt and reader replay tolerates outbound journal entries',len(submitted())==1 and len(contexts())==3)
        mode.write_text('fail')
        failed=event('script-failed','script failure')
        failure=receipt(mcp(failed))
        assert failure['status']=='unknown' and 'script failure' not in failure['error'] and 'Unique test conversation' not in failure['error']
        count=len(submitted())
        assert receipt(mcp(failed))['status']=='unknown'
        check('script failure produces durable unknown without retry',len(submitted())==count and len(contexts())==3)
        mode.write_text('normal')
        no_echo=event('no-echo','echo in wrong Room')
        count=len(submitted())
        future=executor.submit(mcp,no_echo)
        wait(lambda:len(submitted())==count+1,'script submission without echo')
        insert('echo in wrong Room',room='other-room',outgoing=True)
        submitted_receipt=receipt(future.result(timeout=5))
        wait(lambda:cursor()==next_id-1,'other Room import')
        check('script success does not depend on a database match in either Room',submitted_receipt['status']=='sent' and submitted_receipt['result']['confirmation']=='client_script')
        assert receipt(mcp(no_echo))==submitted_receipt
        assert len(submitted())==count+1
        mode.write_text('hold')
        timed_out=event('script-timeout','script timed out')
        timeout_receipt=receipt(mcp(timed_out))
        count=len(submitted())
        check('script timeout remains unknown and never automatically retries',timeout_receipt['status']=='unknown' and 'LOCAL_SEND_SCRIPT_TIMEOUT' in timeout_receipt['error'] and receipt(mcp(timed_out))==timeout_receipt and len(submitted())==count)
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
        final=send_submit(event('after-crash','sender recovered'))
        check('sender and incremental reader continue after crash recovery',final['status']=='sent')
        count=len(submitted())
        mode.write_text('slow')
        first=executor.submit(mcp,event('parallel-first','parallel first'))
        wait(lambda:len(submitted())==count+1,'first concurrent submission')
        second=executor.submit(mcp,event('parallel-second','parallel second'))
        time.sleep(.5)
        assert len(submitted())==count+1
        assert receipt(first.result(timeout=30))['status']=='sent'
        wait(lambda:len(submitted())==count+2,'second serialized submission')
        assert receipt(second.result(timeout=30))['status']=='sent'
        mode.write_text('normal')
        check('concurrent MCP requests serialize native scripts without waiting for DB echoes',len(submitted())==count+2)
        insert('parallel first',outgoing=True)
        insert('parallel second',outgoing=True)
        wait(lambda:cursor()==next_id-1,'concurrent local echoes')
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
        insert('mobile incoming is not outgoing',flag=131090)
        wait(lambda:cursor()==next_id-1,'mobile incoming direction')
        history=contexts()
        initial=next(m for m in history if m.get('local_id')=='1')
        mobile=next(m for m in history if m.get('local_id')==str(next_id-1))
        own=next(m for m in history if m['text']['content']=='parallel second')
        check('real type-zero nested protobuf and sender identity preserve exact text and direction',initial['text']['content']=='initial incoming' and mobile['text']['content']=='mobile incoming is not outgoing' and not mobile.get('outgoing',False) and mobile['from']=='9007199254740997' and own.get('outgoing') is True and own['from']=='9007199254740995')
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
