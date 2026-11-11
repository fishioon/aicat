#!/usr/bin/env python3
"""Rules/plugins/MCP integration only. Failure modes covered here: transaction
rollback, cancellation/time/memory budgets, bounded/compressed/stream bodies,
custom replies, restart semantics, persistence conflicts and control access.
No native UI, real Keychain, real system proxy, public server or cloud is used.
"""
import argparse,base64,concurrent.futures,gzip,hashlib,http.client,http.server,json,os,signal,socket,ssl,stat,subprocess,tempfile,threading,time
from pathlib import Path


def wait(fn,seconds=10):
    deadline=time.monotonic()+seconds;last=None
    while time.monotonic()<deadline:
        try:
            value=fn()
            if value:return value
        except (OSError,ValueError,AssertionError) as error:last=repr(error)
        time.sleep(.02)
    raise AssertionError('timeout: '+str(last))

class Upstream(http.server.BaseHTTPRequestHandler):
    protocol_version='HTTP/1.1'
    seen=[];guard=threading.Lock();release=threading.Event()
    def log_message(self,*args):pass
    def do_GET(self):self.do_POST()
    def do_POST(self):
        if self.headers.get('Transfer-Encoding')=='chunked':
            chunks=[]
            while True:
                n=int(self.rfile.readline().split(b';')[0],16)
                if not n:self.rfile.readline();break
                chunks.append(self.rfile.read(n));self.rfile.read(2)
            raw=b''.join(chunks)
        else:raw=self.rfile.read(int(self.headers.get('Content-Length',0)))
        body=gzip.decompress(raw) if self.headers.get('Content-Encoding')=='gzip' else raw
        with self.guard:self.seen.append(self.path)
        if self.path.startswith('/sse'):
            self.send_response(200);self.send_header('Content-Type','text/event-stream');self.send_header('Connection','close');self.end_headers()
            self.wfile.write(b'data: first\n\n');self.wfile.flush();self.release.wait(5)
            try:self.wfile.write(b'data: last\n\n');self.wfile.flush()
            except OSError:pass
            self.close_connection=True;return
        try:echo=json.loads(body) if body else None
        except (ValueError,UnicodeDecodeError):echo=None
        reply={'path':self.path,'method':self.command,'headers':dict(self.headers),'chain':self.headers.get_all('X-Chain',[]),'echo':echo,'length':len(body),'digest':hashlib.sha256(body).hexdigest()}
        data=json.dumps(reply,ensure_ascii=False,separators=(',',':')).encode()
        encoded=gzip.compress(data) if 'gzip' in self.path else data
        self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(encoded)));self.send_header('ETag','fixture-etag')
        if encoded!=data:self.send_header('Content-Encoding','gzip')
        self.end_headers()
        try:self.wfile.write(encoded)
        except OSError:pass

NORMAL='''
type Msg = any;
export function onRequestHeaders(r: Msg) {
 if (globalThis.oldRequest) throw new Error('global leak');
 globalThis.oldRequest=true;
 r.url=r.url.replace('/normal','/normal/echo');
 r.headers['x-chain']=['first'];r.context={step:1};return r;
}
export async function onRequest(r: Msg) {
 await new Promise(resolve=>setTimeout(resolve,2));
 if(r.context.step!==1)throw new Error('request context missing');
 const data=JSON.parse(host.bodyText(r.body));data.requestRewritten=host.settings.label;
 r.body=host.bodyFromText(JSON.stringify(data));r.context.step=2;return r;
}
export function onResponseHeaders(r: Msg) {
 if(r.context.step!==2)throw new Error('response context missing');
 r.statusCode=201;r.headers['set-cookie']=['one=1; Path=/','two=2; Path=/'];r.context.step=3;return r;
}
export function onResponse(r: Msg) {
 if(r.context.step!==3)throw new Error('body context missing');
 const data=JSON.parse(host.bodyText(r.body));data.responseRewritten=true;
 r.body=host.bodyFromText(JSON.stringify(data));console.info('done',r.context.step);return r;
}
'''
POST='''
export function onRequestHeaders(r){r.headers['x-chain'].push('second');return r;}
export function onRequest(r){const j=JSON.parse(host.bodyText(r.body));j.post=true;r.body=host.bodyFromText(JSON.stringify(j));return r;}
export function onResponse(r){const j=JSON.parse(host.bodyText(r.body));j.responsePost=true;r.body=host.bodyFromText(JSON.stringify(j));return r;}
'''
PRE='''
export function onRequestHeaders(r){r.headers['x-kept']=['yes'];return r;}
export function onRequest(r){const j=JSON.parse(host.bodyText(r.body));j.kept=true;r.body=host.bodyFromText(JSON.stringify(j));return r;}
'''
FAIL='''
export function onRequest(r){r.headers['x-leak']=['bad'];r.context.leak=true;r.body=host.bodyFromText('broken');throw new Error('intentional rollback');}
export function onResponse(r){if(r.context.leak)throw new Error('failed context leaked');return r;}
'''
AFTER="export function onRequest(r){const j=JSON.parse(host.bodyText(r.body));j.after=true;r.body=host.bodyFromText(JSON.stringify(j));return r;}"
SPIN="export function onRequest(r){for(;;){}return r;}"
MEMORY="export function onRequest(r){const keep=[];for(;;){keep.push(new Uint8Array(4<<20));}return r;}"
BAD="export function onRequest(r){r.headers['content-length']=['999999'];return r;}"
STREAM="""
export function onRequestHeaders(r){r.headers['x-stream']=['yes'];return r;}
export function onRequest(r){throw new Error('stream request body hook ran');}
export function onResponseHeaders(r){r.headers['x-stream-response']=['yes'];return r;}
export function onResponse(r){throw new Error('stream response body hook ran');}
"""
MOCK="""
export function onRequest(r){return {context:{mock:true},response:{statusCode:202,headers:{'content-type':['application/json']},body:host.bodyFromText('{"mock":true}')}};}
export function onResponseHeaders(r){if(!r.context.mock)throw new Error('mock context');r.headers['x-mock']=['yes'];return r;}
export function onResponse(r){const j=JSON.parse(host.bodyText(r.body));j.responseHook=true;r.body=host.bodyFromText(JSON.stringify(j));return r;}
"""
LOGS="export function onRequestHeaders(r){for(let i=0;i<100;i++)console.log('x'.repeat(3000));r.headers['x-environment']=[typeof fetch+':'+typeof process+':'+typeof require];return r;}"
BOMB="export function onRequest(r){r.body=host.bodyFromText('should-not-replace-bomb');return r;}"


def main():
    parser=argparse.ArgumentParser();parser.add_argument('--binary',default='bin/aicat-rules-e2e');parser.add_argument('--port',type=int,default=19993);parser.add_argument('--output');args=parser.parse_args()
    root=Path(args.output).resolve() if args.output else Path(tempfile.mkdtemp(prefix='rules-e2e-',dir=Path('build').resolve()))
    root.mkdir(parents=True,exist_ok=True);root.chmod(0o700);home=root/'.aicat';home.mkdir(mode=0o700)
    cfg=home/'aicat.yaml';cfg.write_text(f'''# preserve this comment\nfutureSetting: keep\nfeatures:\n  traffic: true\ngateway:\n  addr: 127.0.0.1:{args.port}\n  interceptHosts: [localhost]\n  bufferedBodyLimit: 2048\ntraffic:\n  maxRecords: 200\n  maxBodyBytes: 4096\n  maxTotalBodyBytes: 1048576\n''')
    def openssl(*argv):subprocess.run(['openssl',*argv],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    (root/'root.cnf').write_text('[req]\ndistinguished_name=dn\nx509_extensions=v3_ca\nprompt=no\n[dn]\nCN=Rules E2E Root\n[v3_ca]\nbasicConstraints=critical,CA:true\nkeyUsage=critical,keyCertSign,cRLSign\n')
    openssl('req','-x509','-newkey','rsa:2048','-nodes','-keyout',str(root/'root.key'),'-out',str(root/'root.pem'),'-days','2','-config',str(root/'root.cnf'))
    openssl('req','-newkey','rsa:2048','-nodes','-keyout',str(root/'leaf.key'),'-out',str(root/'leaf.csr'),'-subj','/CN=localhost')
    (root/'leaf.ext').write_text('subjectAltName=DNS:localhost,IP:127.0.0.1\nextendedKeyUsage=serverAuth\nbasicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\n')
    openssl('x509','-req','-in',str(root/'leaf.csr'),'-CA',str(root/'root.pem'),'-CAkey',str(root/'root.key'),'-CAcreateserial','-out',str(root/'leaf.pem'),'-days','2','-extfile',str(root/'leaf.ext'))
    servers=[]
    for tls in (False,True):
        server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Upstream);server.daemon_threads=True
        if tls:
            context=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER);context.load_cert_chain(root/'leaf.pem',root/'leaf.key');server.socket=context.wrap_socket(server.socket,server_side=True)
        threading.Thread(target=server.serve_forever,daemon=True).start();servers.append(server)
    hp,tp=(s.server_address[1] for s in servers)
    state=root/'os.json';state.write_text('{"services":[],"trusted":false}');state.chmod(0o600)
    env=dict(os.environ,AICAT_E2E_ROOT=str(root/'root.pem'),AICAT_E2E_OS_STATE=str(state),GODEBUG='x509usefallbackroots=1')
    binary=str(Path(args.binary).resolve());log=(root/'app.log').open('w');process=None;token='';number=0
    report={'passed':False,'scope':'rules/plugins/MCP only','checks':[],'binary':binary,'port':args.port,
            'binarySHA256':hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
            'goVersion':subprocess.check_output(['go','version'],text=True).strip(),
            'modules':json.loads(subprocess.check_output(['go','list','-m','-json','github.com/MoeForge/moejs'],text=True)),
            'command':['python3','e2e/rules.py','--binary',args.binary,'--port',str(args.port)],
            'sourceHashes':{str(p):hashlib.sha256(p.read_bytes()).hexdigest() for directory in ['internal/plugins','internal/rewrite','internal/mcpserver','internal/gateway','internal/traffic','internal/config','internal/app','cmd/aicat'] for p in sorted(Path(directory).rglob('*.go'))}}
    checks=report['checks']
    def control_http(method,path,body=None,headers=None):
        c=http.client.HTTPConnection('127.0.0.1',args.port,timeout=8);c.request(method,path,body=body,headers=headers or {});r=c.getresponse();reply=(r.status,r.read(),dict(r.getheaders()));c.close();return reply
    def api(path='/api/traffic'):
        status,data,_=control_http('GET',path);assert status==200,(status,data);return json.loads(data)
    def ready():return process.poll() is None and (home/'mcp-token').exists() and api() is not None
    def launch():
        nonlocal process,token
        process=subprocess.Popen([binary,'serve','--home',str(home)],env=env,stdout=log,stderr=log)
        wait(ready);token=(home/'mcp-token').read_text().strip()
    def stop():
        process.send_signal(signal.SIGTERM);assert process.wait(timeout=8)==0
    def rpc(method,params=None,auth=True,extra=None):
        nonlocal number
        number+=1;headers={'Content-Type':'application/json','Accept':'application/json, text/event-stream','MCP-Protocol-Version':'2025-06-18'}
        if auth:headers['Authorization']='Bearer '+token
        if extra:headers.update(extra)
        status,data,h=control_http('POST','/mcp',json.dumps({'jsonrpc':'2.0','id':number,'method':method,'params':params or {}}),headers)
        if status!=200:return status,data,h
        return status,json.loads(data),h
    def tool(name,arguments=None,error=False):
        status,data,_=rpc('tools/call',{'name':name,'arguments':arguments or {}});assert status==200,(name,status,data)
        if error:
            assert 'error' in data or data['result'].get('isError'),data;return data
        assert 'error' not in data and not data['result'].get('isError'),(name,data)
        return data['result'].get('structuredContent') or json.loads(data['result']['content'][0]['text'])
    def put(id,source):return tool('plugins_put',{'manifest':{'apiVersion':1,'id':id,'name':id,'version':'1.0.0','main':'index.ts'},'source':source})
    def rule(id,path,plugin=None,**kw):return dict(id=id,name=id,enabled=True,match={'hosts':['127.0.0.1','localhost'],'pathPrefix':path},plugin=plugin or id,**kw)
    def fetch(path,body=b'',tls=False,encoding=None):
        if tls:
            context=ssl.create_default_context(cafile=str(root/'client-roots.pem'));c=http.client.HTTPSConnection('127.0.0.1',args.port,context=context,timeout=8);c.set_tunnel('localhost',tp);target=path
        else:c=http.client.HTTPConnection('127.0.0.1',args.port,timeout=8);target=f'http://127.0.0.1:{hp}{path}'
        headers={'Content-Type':'application/json'}
        if encoding:headers['Content-Encoding']=encoding
        c.request('POST' if body else 'GET',target,body=body,headers=headers);r=c.getresponse();data=r.read();status=r.status;response_headers=r.getheaders();c.close();return status,data,response_headers
    def latest(path):
        rows=api()['items'];return next(r['id'] for r in rows if path in r['url'])
    def record(id):
        return wait(lambda:(r if (r:=api('/api/traffic/'+str(id)))['state'] in ('completed','failed','canceled') else None))
    def body(info):
        status,data,_=control_http('GET','/api/bodies/'+info['id']);assert status==200;return data
    try:
        launch();initial_token=token
        (root/'client-roots.pem').write_bytes((root/'root.pem').read_bytes()+(home/'ca/mitmproxy-ca-cert.pem').read_bytes())
        assert stat.S_IMODE((home/'mcp-token').stat().st_mode)==0o600
        assert rpc('tools/list',auth=False)[0]==401
        assert rpc('tools/list',extra={'Authorization':'Bearer invalid'})[0]==401
        assert rpc('tools/list',extra={'Origin':'https://untrusted.invalid'})[0]==403
        assert rpc('tools/list',extra={'Host':'wrong.invalid'})[0]==403
        status,init,_=rpc('initialize',{'protocolVersion':'2025-06-18','capabilities':{},'clientInfo':{'name':'rules-e2e','version':'1'}})
        assert status==200 and init['result']['instructions']
        names={t['name'] for t in rpc('tools/list')[1]['result']['tools']};assert {'rules_put','plugins_put','plugins_guide','rules_test','traffic_get'}<=names
        assert 'onRequestHeaders' in tool('plugins_guide')['guide']
        resource=rpc('resources/read',{'uri':'aicat://plugins/sdk/v1'})[1];assert resource['result']['contents'][0]['text']
        assert api()['items']==[]
        checks.append('authenticated official MCP transport shares the proxy port; origin/authority/token checks and SDK discovery work without capturing control calls')

        put('normal',NORMAL);put('post',POST);put('pre',PRE);put('fail',FAIL);put('after',AFTER);put('spin',SPIN);put('memory',MEMORY);put('invalid',BAD);put('stream',STREAM);put('mock',MOCK);put('logs',LOGS);put('bomb',BOMB)
        rules=[rule('normal','/normal',settings={'label':'改写'}),rule('post','/normal'),rule('pre','/fail'),rule('fail','/fail'),rule('after','/fail'),rule('spin','/timeout'),rule('memory','/memory'),rule('invalid','/invalid'),rule('stream','/large'),rule('sse','/sse',plugin='stream'),rule('mock','/mock'),rule('logs','/logs'),rule('bomb','/bomb')]
        for r in rules:tool('rules_put',{'rule':r})
        saved=tool('rewrite_set_enabled',{'enabled':True})
        assert saved['restartRequired'] and saved['config']['enabled'] and not saved['activeEnabled']
        assert '# preserve this comment' in cfg.read_text() and 'futureSetting: keep' in cfg.read_text()
        snapshot=tool('rules_list');old=snapshot['revision']
        tool('rules_set_enabled',{'id':'normal','enabled':False,'expectedRevision':old})
        tool('rules_reorder',{'ids':[r['id'] for r in rules],'expectedRevision':old},error=True)
        tool('rules_set_enabled',{'id':'normal','enabled':True})
        tool('rules_reorder',{'ids':[r['id'] for r in rules]})
        tool('rules_reorder',{'ids':['normal']*len(rules)},error=True)
        tool('plugins_delete',{'id':'normal'},error=True)
        bad={'apiVersion':1,'id':'../outside','name':'bad','version':'1','main':'index.ts'}
        tool('plugins_put',{'manifest':bad,'source':NORMAL},error=True)
        bad['id']='outside';bad['main']='../evil.ts';tool('plugins_put',{'manifest':bad,'source':NORMAL},error=True)
        bad['main']='index.ts';tool('plugins_validate',{'manifest':bad,'source':'import fs from "node:fs"; export function onRequest(r){fs.readFileSync("secret");return r}'},error=True)
        tool('plugins_validate',{'manifest':bad,'source':'export function onRequest( {'},error=True)
        outside=root/'outside';outside.mkdir();(home/'plugins/escape').symlink_to(outside,target_is_directory=True)
        bad['id']='escape';tool('plugins_put',{'manifest':bad,'source':'export function onRequest(r){return r}'},error=True)
        assert list(outside.iterdir())==[]
        sample={'request':{'url':f'http://127.0.0.1:{hp}/normal','method':'POST','headers':{'content-type':['application/json']},'body':base64.b64encode(b'{"v":1}').decode()}}
        dry=tool('rules_test',{'rule':rules[0],'stage':'onRequestHeaders','exchange':sample})
        assert '/normal/echo' in dry['exchange']['request']['url'] and dry['executions'][0]['status']=='success'
        disabled=dict(rules[0],enabled=False)
        assert tool('rules_test',{'rule':disabled,'stage':'onRequestHeaders','exchange':sample})['executions'][0]['status']=='success'
        put('repair','export function onRequest(r){return r;}')
        broken='export function onRequest( {'
        (home/'plugins/repair/index.ts').write_text(broken)
        editable=tool('plugins_get',{'id':'repair'})
        assert editable['source']==broken and editable['plugin']['error']
        assert any(p['id']=='repair' and p.get('error') for p in tool('plugins_list')['plugins'])
        repair_manifest={k:editable['plugin'][k] for k in ('apiVersion','id','name','version','main')}
        tool('plugins_put',{'manifest':repair_manifest,'source':'export function onRequest(r){return r;}','expectedRevision':editable['revision']})
        tool('plugins_put',{'manifest':repair_manifest,'source':NORMAL,'expectedRevision':editable['revision']},error=True)
        tool('plugins_delete',{'id':'repair'})
        assert fetch('/normal',b'{"v":1}')[0]==200
        checks.append('MCP plugin/rule CRUD, validation, ordered updates, revision conflicts and offline tests preserve unrelated config; saved changes are inactive until restart')

        stop();launch();assert token==initial_token
        active=tool('rewrite_status');assert active['activeEnabled'] and not active['restartRequired'] and all(not p.get('error') for p in active['activePlugins'])
        payload='{"input":"原始请求"}'.encode()
        for tls in (False,True):
            status,data,headers=fetch('/normal',payload,tls=tls);reply=json.loads(data)
            assert status==201 and reply['echo']['requestRewritten']=='改写' and reply['echo']['post'] and reply['responseRewritten'] and reply['responsePost'],(status,reply)
            assert reply['chain']==['first','second'] and len([v for k,v in headers if k.lower()=='set-cookie'])==2
            rec=record(latest('/normal'))
            assert rec['requestReceived']['url'].endswith('/normal') and rec['requestSent']['url'].endswith('/normal/echo')
            assert body(rec['requestReceived']['body'])==payload
            assert json.loads(body(rec['requestSent']['body']))['requestRewritten']=='改写'
            assert rec['responseReceived']['statusCode']==200 and rec['responseSent']['statusCode']==201
            assert rec['ruleRevision']==active['activeRevision'] and len(rec['executions'])==7 and all(e['status']=='success' for e in rec['executions']),rec['executions']
            assert tool('traffic_get',{'id':rec['id']})['id']==rec['id']
            retained=tool('traffic_body',{'id':rec['requestReceived']['body']['id']})
            assert base64.b64decode(retained['data'])==payload and not retained['returnedTruncated']
            (root/('tls-record.json' if tls else 'http-record.json')).write_text(json.dumps(rec,ensure_ascii=False,indent=2))
        checks.append('real HTTP and selected HTTPS execute all four JS/TS stages in order, retain per-rule context, preserve multi-value headers and record distinct original/sent snapshots')

        status,data,headers=fetch('/normal-gzip',gzip.compress(payload),encoding='gzip');reply=json.loads(data)
        assert status==201 and reply['echo']['requestRewritten']=='改写' and not reply['headers'].get('Content-Encoding')
        assert not any(k.lower() in ('content-encoding','etag') for k,v in headers)
        rec=record(latest('/normal-gzip'));assert gzip.decompress(body(rec['requestReceived']['body']))==payload
        assert gzip.decompress(body(rec['responseReceived']['body']))!=body(rec['responseSent']['body'])
        checks.append('bounded gzip decoding rewrites actual bytes, fixes content framing and preserves encoded originals for inspection')

        status,data,_=fetch('/fail',b'{"v":1}');reply=json.loads(data)
        assert status==200 and reply['echo']['kept'] and reply['echo']['after'] and not reply['headers'].get('X-Leak')
        rec=record(latest('/fail'));errors=[e for e in rec['executions'] if e['status']=='error']
        assert len(errors)==1 and 'intentional rollback' in errors[0]['error'] and rec['state']=='completed',rec
        checks.append('a failed plugin rolls back its own body, headers and context while preserving successful earlier changes and continuing later plugins')

        for route,label in [('/timeout','deadline'),('/memory','memory'),('/invalid','maintained')]:
            start=time.monotonic();status,data,_=fetch(route,b'{"v":1}');assert status==200 and json.loads(data)['echo']=={'v':1} and time.monotonic()-start<3
            rec=record(latest(route));assert any(e['status']=='error' and label in e.get('error','').lower() for e in rec['executions']),rec['executions']
        checks.append('dead loops, memory exhaustion and invalid framing edits are bounded, fail open and leave the proxy usable')

        large=b'x'*10000;status,data,headers=fetch('/large',large);reply=json.loads(data);assert status==200 and reply['digest']==hashlib.sha256(large).hexdigest() and reply['headers']['X-Stream']=='yes'
        rec=record(latest('/large'));assert rec['requestReceived']['body']['state']=='truncated' and rec['requestReceived']['body']['observed']==len(large)
        skipped={e['stage'] for e in rec['executions'] if e['status']=='skipped'};assert {'onRequest','onResponse'}<=skipped and not any(e['status']=='error' for e in rec['executions'])
        bomb_size=(1<<20)+65536;bomb=gzip.compress(b'x'*bomb_size);status,data,_=fetch('/bomb',bomb,encoding='gzip');assert status==200 and json.loads(data)['length']==bomb_size
        rec=record(latest('/bomb'));assert any('decoded body exceeds' in e.get('error','') for e in rec['executions']),rec
        checks.append('large bodies stream without truncating wire data, body hooks explicitly skip, and decompression bombs preserve the original payload')

        c=http.client.HTTPConnection('127.0.0.1',args.port,timeout=4);c.request('GET',f'http://127.0.0.1:{hp}/sse');response=c.getresponse();assert response.getheader('X-Stream-Response')=='yes';assert response.readline()==b'data: first\n';assert not Upstream.release.is_set();Upstream.release.set();response.read();c.close()
        rec=record(latest('/sse'));assert any(e['stage']=='onResponse' and e['status']=='skipped' for e in rec['executions'])
        checks.append('SSE headers can change while events reach the client before completion and full response-body hooks are skipped')

        with Upstream.guard:before=Upstream.seen.count('/mock')
        status,data,headers=fetch('/mock');assert status==202 and json.loads(data)=={'mock':True,'responseHook':True} and ('X-Mock','yes') in headers
        with Upstream.guard:assert Upstream.seen.count('/mock')==before
        rec=record(latest('/mock'));assert rec['synthetic'] and rec['state']=='completed',rec and len(rec['executions'])==3
        checks.append('custom responses avoid the upstream and still run response hooks with complete synthetic traffic records')

        status,data,_=fetch('/logs');assert json.loads(data)['headers']['X-Environment']=='undefined:undefined:undefined'
        rec=record(latest('/logs'));assert len(rec['executions'][0]['logs'])<=8 and len(json.dumps(rec['executions']))<16384
        assert len(tool('rewrite_logs')['executions'])<=64
        def concurrent_request(i):
            status,data,_=fetch('/normal',json.dumps({'n':i}).encode());j=json.loads(data);assert status==201 and j['echo']['n']==i and j['echo']['requestRewritten']=='改写'
        with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:list(pool.map(concurrent_request,range(20)))
        checks.append('plugin host capabilities are restricted, diagnostics are bounded, and concurrent requests do not share mutable globals or contexts')

        # Cancel clients while JavaScript is running, then ensure workers recover.
        sockets=[]
        for _ in range(8):
            c=socket.create_connection(('127.0.0.1',args.port));c.sendall(f'POST http://127.0.0.1:{hp}/timeout HTTP/1.1\r\nHost: 127.0.0.1:{hp}\r\nContent-Length: 2\r\n\r\n{{}}'.encode());sockets.append(c)
        time.sleep(.02)
        for c in sockets:c.close()
        time.sleep(.3);concurrent_request(99)
        checks.append('request cancellation interrupts scripts and releases bounded execution slots')

        tool('rewrite_set_enabled',{'enabled':False});assert fetch('/normal',payload)[0]==201
        stop();launch();assert fetch('/normal',payload)[0]==200 and not tool('rewrite_status')['activeEnabled']
        rec=record(latest('/normal'));assert not rec.get('executions') and rec['requestReceived']['body']['id']==rec['requestSent']['body']['id']
        tool('rewrite_set_enabled',{'enabled':True});stop();cfg.write_text(cfg.read_text().replace('traffic: true','traffic: false'));launch()
        assert api()['items']==[];assert fetch('/normal',payload)[0]==201 and api()['items']==[] and tool('rewrite_logs')['executions']
        checks.append('global disable takes effect only after restart; disabling traffic collection preserves rewrite behavior and bounded diagnostics')

        put('unused','export function onRequest(r){return r;}');tool('plugins_delete',{'id':'unused'});assert list((home/'trash/plugins').glob('unused-*'))
        assert '# preserve this comment' in cfg.read_text() and 'futureSetting: keep' in cfg.read_text()
        checks.append('plugin deletion refuses referenced modules and moves unused source to private recoverable trash')
        stop();report['passed']=True
    except BaseException as error:
        report['error']=repr(error)
        raise
    finally:
        Upstream.release.set()
        if process and process.poll() is None:
            process.terminate()
            try:process.wait(timeout=8)
            except subprocess.TimeoutExpired:process.kill();process.wait()
        for server in servers:server.shutdown();server.server_close()
        log.close();report['raceWarning']='WARNING: DATA RACE' in (root/'app.log').read_text();report['passed']=report['passed'] and not report['raceWarning']
        (root/'report.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n');print(json.dumps({'passed':report['passed'],'checks':len(checks),'error':report.get('error'),'report':str(root/'report.json')},ensure_ascii=False))
    if not report['passed']:raise SystemExit(1)

if __name__=='__main__':main()
