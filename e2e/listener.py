#!/usr/bin/env python3
"""Real native listener edits with isolated OS/Home and real proxy requests.
Failure modes: wildcard readiness probes target an unusable IP; a switch drops
records or the CA; occupied ports/config failures retire the old listener;
system proxy endpoints diverge; bad ports apply; restart forgets the selection.
"""
import argparse, fcntl, hashlib, http.client, http.server, json, os, re, signal, socket, ssl, subprocess, tempfile, threading, time
from pathlib import Path
from prototype import Automation, Fixture, wait_for


def main():
    parser=argparse.ArgumentParser();parser.add_argument('--binary',default='bin/aicat-listener-e2e');parser.add_argument('--output');parser.add_argument('--transport-only',action='store_true');opts=parser.parse_args()
    root=Path(opts.output).resolve() if opts.output else Path(tempfile.mkdtemp(prefix='listener-e2e-',dir=Path('build').resolve()))
    root.mkdir(parents=True,exist_ok=True);root.chmod(0o700);home=root/'home';assert not home.exists();home.mkdir(mode=0o700)
    binary=Path(opts.binary).resolve();report={'passed':False,'checks':[],'binary':str(binary),'binarySHA256':hashlib.sha256(binary.read_bytes()).hexdigest(),'replay':f'python3 e2e/listener.py --binary {binary}','buildCommand':'go build -race -tags e2e -o bin/aicat-listener-e2e ./cmd/aicat','artifacts':str(root)}
    report['sources']={str(p):hashlib.sha256(p.read_bytes()).hexdigest() for base in ('internal/app','internal/config','internal/gateway','internal/listenaddr','internal/systemproxy') for p in Path(base).rglob('*.go')}
    report['sources']['e2e/listener.py']=hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
    report['renderMode']='transport-only' if opts.transport_only else 'native-display'
    if opts.transport_only:report['replay']+=' --transport-only'
    (root/'go-version.txt').write_bytes(subprocess.check_output(['go','version','-m',str(binary)]))
    def free_port():
        with socket.socket() as sock:sock.bind(('127.0.0.1',0));return sock.getsockname()[1]
    endpoint={'host':'127.0.0.1','port':free_port(),'bind':'127.0.0.1'}
    cfg=home/'aicat.yaml';cfg.write_text(f'ui:\n  language: zh-Hans\n  theme: light\ngateway:\n  addr: 127.0.0.1:{endpoint["port"]}\n  interceptHosts: [localhost]\n# listener-fixture-preserve\n')
    original=[{'id':'wifi','name':'Wi-Fi','proxies':{'HTTPEnable':0,'HTTPProxy':'','HTTPPort':0,'HTTPSEnable':0,'HTTPSProxy':'','HTTPSPort':0,'ExceptionsList':['localhost'],'CustomValue':'keep'}}]
    os_state=root/'os-state.json';os_state.write_text(json.dumps({'services':original,'trusted':False,'recordProxyWrites':True}));os_state.chmod(0o600)
    def os_fixture(**updates):
        with open(str(os_state)+'.lock','a') as lock:
            fcntl.flock(lock,fcntl.LOCK_EX);state=json.loads(os_state.read_text())
            if updates:
                state.update(updates);temp=os_state.with_suffix('.next');temp.write_text(json.dumps(state));temp.chmod(0o600);temp.replace(os_state)
            return state
    sock_path=Path(tempfile.gettempdir())/f'aicat-listener-{os.getpid()}.sock'
    env=dict(os.environ,MYGO_AUTOMATION=str(sock_path),AICAT_E2E_OS_STATE=str(os_state),AICAT_E2E_ROOT=str(root/'root.pem'),GODEBUG='x509usefallbackroots=1');log=(root/'app.log').open('w');proc=None;auto=None;tls_server=None
    if opts.transport_only:env.pop('MYGO_AUTOMATION',None)
    server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Fixture);server.daemon_threads=True;threading.Thread(target=server.serve_forever,daemon=True).start()
    def api(path='/api/traffic',headers=None,host=None,port=None):
        conn=http.client.HTTPConnection(host or endpoint['host'],port or endpoint['port'],timeout=3);conn.request('GET',path,headers=headers or {});res=conn.getresponse();data=res.read();status=res.status;conn.close();return status,data
    def records():
        code,data=api();assert code==200,(code,data);return json.loads(data)['items']
    def launch():
        nonlocal proc,auto
        log_offset=(root/'app.log').stat().st_size
        proc=subprocess.Popen([str(binary)]+(['serve'] if opts.transport_only else [])+['--home',str(home)],env=env,stdout=log,stderr=log)
        wait_for(lambda:proc.poll() is None and (opts.transport_only or sock_path.exists()) and api()[0]==200,15)
        wait_for(lambda:'aicat ready at ' in (root/'app.log').read_text()[log_offset:],15)
        if opts.transport_only:return
        auto=Automation(sock_path)
        wait_for(lambda:auto.call('windows').get('windows'),15)
        auto.call('menu',path=['E2E','打开窗口']);auto.call('idle',timeout=3000)
        auto.click('account-menu');auto.click('nav-settings');auto.click('settings-proxy')
        wait_for(lambda:node('listener-address') and 'disabled' not in node('listener-address').get('states',[]),15)
    def stop():
        nonlocal proc,auto
        proc.send_signal(signal.SIGTERM);assert proc.wait(timeout=12)==0
        if auto:auto.file.close();auto.sock.close()
        auto=None;proc=None;wait_for(lambda:not sock_path.exists())
    def node(key):return next(n for n in auto.tree() if n.get('key')==key)
    def ready():return 'disabled' not in node('listener-address').get('states',[])
    def active():return node('proxy-address').get('label') or node('proxy-address').get('value')
    def choose(host):
        auto.click('listener-address');option=wait_for(lambda:next((n for n in auto.tree() if n.get('role') in ('option','listItem') and n.get('label')==host),None));auto.call('act',id=option['id'],action='press');auto.call('idle',timeout=3000)
    def edit_port(port):auto.act('listener-port','setValue',text=str(port))
    def apply(bind,port,success=True):
        old=dict(endpoint);choose(bind);edit_port(port)
        client='127.0.0.1' if bind=='0.0.0.0' else '::1' if bind=='::' else bind
        os_fixture(probeGateway=f'[{client}]:{port}' if ':' in client else f'{client}:{port}')
        auto.click('listener-apply');wait_for(ready,20)
        address=f'[{bind}]:{port}' if ':' in bind else f'{bind}:{port}'
        if success:
            wait_for(lambda:active()==address,10);endpoint.update(host=client,port=port,bind=bind)
            wait_for(lambda:api()[0]==200,10);assert address in cfg.read_text()
            assert not any(n.get('key')=='listener-error' for n in auto.tree())
            if old['port']!=port:
                with socket.socket() as probe:probe.settimeout(.3);assert probe.connect_ex((old['host'],old['port']))!=0
        else:
            assert any(n.get('key')=='listener-error' for n in auto.tree());assert api()[0]==200;assert active()==(f'[{old["bind"]}]:{old["port"]}' if ':' in old['bind'] else f'{old["bind"]}:{old["port"]}')
    def send(path):
        conn=http.client.HTTPConnection(endpoint['host'],endpoint['port'],timeout=4);conn.request('POST',f'http://127.0.0.1:{server.server_port}{path}',b'listener-fixture',{'Content-Type':'text/plain'});res=conn.getresponse();data=res.read();assert res.status==200,(res.status,data);conn.close()
        return wait_for(lambda:next((r for r in records() if r['url'].endswith(path) and r['state']=='completed'),None))
    def send_tls(path):
        trust=ssl.create_default_context(cafile=str(home/'ca'/'mitmproxy-ca-cert.pem'))
        conn=http.client.HTTPSConnection(endpoint['host'],endpoint['port'],context=trust,timeout=7);conn.set_tunnel('localhost',tls_server.server_port)
        conn.connect();assert conn.sock.getpeercert(binary_form=True)!=ssl.PEM_cert_to_DER_cert((root/'leaf.pem').read_text())
        conn.request('POST',path,b'tls-listener-fixture');res=conn.getresponse();data=res.read();conn.close();assert res.status==200,(res.status,data)
        record=wait_for(lambda:next((r for r in records() if r['url'].endswith(path) and r['state']=='completed'),None));assert not record.get('tunnel')
    def proxy_target():
        value=os_fixture()['services'][0]['proxies'];assert value['CustomValue']=='keep' and value['ExceptionsList']==['localhost']
        return value
    try:
        if not opts.transport_only and '"CGSSessionScreenIsLocked"=Yes' in subprocess.check_output(['ioreg','-n','Root','-d1'],text=True):raise RuntimeError('macOS is locked; unlock before native E2E')
        def openssl(*args):subprocess.run(['openssl',*args],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        (root/'root.cnf').write_text('[req]\ndistinguished_name=dn\nx509_extensions=v3_ca\nprompt=no\n[dn]\nCN=AICat Listener E2E Root\n[v3_ca]\nbasicConstraints=critical,CA:true\nkeyUsage=critical,keyCertSign,cRLSign\nsubjectKeyIdentifier=hash\n')
        openssl('req','-x509','-newkey','rsa:2048','-nodes','-keyout',str(root/'root.key'),'-out',str(root/'root.pem'),'-days','2','-config',str(root/'root.cnf'))
        openssl('req','-newkey','rsa:2048','-nodes','-keyout',str(root/'leaf.key'),'-out',str(root/'leaf.csr'),'-subj','/CN=localhost')
        (root/'leaf.ext').write_text('subjectAltName=DNS:localhost,IP:127.0.0.1\nextendedKeyUsage=serverAuth\nbasicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\n')
        openssl('x509','-req','-in',str(root/'leaf.csr'),'-CA',str(root/'root.pem'),'-CAkey',str(root/'root.key'),'-CAcreateserial','-out',str(root/'leaf.pem'),'-days','2','-extfile',str(root/'leaf.ext'))
        tls_server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Fixture);tls_server.daemon_threads=True
        tls_context=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER);tls_context.load_cert_chain(root/'leaf.pem',root/'leaf.key');tls_server.socket=tls_context.wrap_socket(tls_server.socket,server_side=True)
        threading.Thread(target=tls_server.serve_forever,daemon=True).start()
        if opts.transport_only:
            ca=None
            for bind in ('127.0.0.1','0.0.0.0','::1'):
                port=free_port();address=f'[{bind}]:{port}' if ':' in bind else f'{bind}:{port}'
                cfg.write_text(re.sub(r'^(\s+addr:).+$',lambda m:m.group(1)+' '+json.dumps(address),cfg.read_text(),flags=re.M))
                endpoint.update(bind=bind,host='127.0.0.1' if bind=='0.0.0.0' else bind,port=port)
                launch();send('/http-transport');send_tls('/tls-transport')
                current=(home/'ca'/'mitmproxy-ca-cert.pem').read_bytes()
                if ca is None:ca=current
                assert ca==current;stop()
            report['checks'].append('CLI startup and real HTTP/TLS MITM on loopback, wildcard and IPv6; CA preserved across process restarts; no native display claim')
            report['passed']=True;return
        launch();assert node('listener-address').get('value')=='127.0.0.1';assert node('listener-port').get('value')==str(endpoint['port'])
        auto.click('listener-address');addresses=[n['label'] for n in auto.tree() if n.get('role') in ('option','listItem')];(root/'address-options.json').write_text(json.dumps(addresses,indent=2));assert addresses[:2]==['127.0.0.1','0.0.0.0']
        auto.call('input',events=[{'type':'key','key':'Escape'}]);auto.call('idle',timeout=3000)
        for value in ('0','65536','abc',''):
            edit_port(value);assert 'disabled' in node('listener-apply').get('states',[]);assert any(n.get('key')=='listener-error' for n in auto.tree())
        edit_port(endpoint['port']);auto.capture(root/'listener-default.png')
        first=send('/before-switch');ca=(home/'ca'/'mitmproxy-ca-cert.pem').read_bytes();send_tls('/tls-before-switch')
        occupied=socket.socket();occupied.bind(('127.0.0.1',0));occupied.listen();occupied_port=occupied.getsockname()[1]
        before=cfg.read_text();apply('127.0.0.1',occupied_port,False);assert cfg.read_text()==before;occupied.close();auto.capture(root/'occupied-port.png')
        report['checks'].append('plain IP options, default loopback, invalid port input and occupied-port failure leave old listener/config usable')
        apply('0.0.0.0',endpoint['port']);second=send('/wildcard-same-port');assert second['id']>first['id'];assert any(r['id']==first['id'] for r in records())
        new_port=free_port();apply('0.0.0.0',new_port);send('/wildcard-new-port');send_tls('/tls-after-switch')
        nic=next((ip for ip in addresses if re.fullmatch(r'\d+\.\d+\.\d+\.\d+',ip) and not ip.startswith(('127.','0.'))),None)
        if nic:apply(nic,new_port);send('/interface-address')
        report['interfaceIP']=nic
        apply('127.0.0.1',new_port)
        if '::1' in addresses:apply('::1',new_port);send('/ipv6-loopback');apply('127.0.0.1',new_port)
        assert (home/'ca'/'mitmproxy-ca-cert.pem').read_bytes()==ca
        send_tls('/tls-after-loopback-restore')
        assert api(headers={'Origin':'https://untrusted.invalid'})[0]==403
        assert api(headers={'Host':'unrelated.invalid'})[0]==403
        report['checks'].append('same-port wildcard switch, live port/IP and IPv6 changes, real forwarding, records/IDs and CA preserved; control routes keep Origin/authority checks')
        saved=home/'saved-config';cfg.rename(saved);cfg.mkdir()
        try:apply('127.0.0.1',free_port(),False)
        finally:cfg.rmdir();saved.rename(cfg)
        send('/after-config-failure');auto.capture(root/'config-save-failure.png')
        fingerprint=hashlib.sha256(ssl.PEM_cert_to_DER_cert(ca.decode())).hexdigest();os_fixture(trusted=True,installedCertificate=fingerprint)
        auto.click('settings-general');auto.click('settings-proxy');wait_for(lambda:any(n.get('key')=='certificate-status' and n.get('label')=='已信任' for n in auto.tree()),15)
        auto.click('system-proxy-toggle');auto.click('system-proxy-enable');wait_for(lambda:proxy_target().get('HTTPEnable')==1)
        previous_count=os_fixture().get('proxyCommandCount',0)
        apply('0.0.0.0',free_port());target=proxy_target();assert target['HTTPProxy']=='127.0.0.1' and target['HTTPPort']==endpoint['port'] and target['HTTPSPort']==endpoint['port']
        writes=os_fixture()['proxyWrites'][previous_count:]
        first_enable=next(i+1 for i,w in enumerate(writes) if w['command'][0]=='-setwebproxy' and w['command'][2]=='127.0.0.1' and w['command'][3]==str(endpoint['port']))
        saved=home/'saved-config';cfg.rename(saved);cfg.mkdir();previous_port=endpoint['port']
        try:
            apply('0.0.0.0',free_port(),False)
            assert proxy_target()['HTTPPort']==previous_port
        finally:cfg.rmdir();saved.rename(cfg)
        current_count=os_fixture()['proxyCommandCount'];os_fixture(failProxyAfterWriteAt=current_count+first_enable)
        before=cfg.read_text();previous_port=endpoint['port'];apply('0.0.0.0',free_port(),False)
        assert cfg.read_text()==before and proxy_target()['HTTPPort']==previous_port
        os_fixture(failProxyAfterWriteAt=0);send('/after-system-proxy-rollback')
        report['checks'].append('config-save failure and injected partial OS write roll back listener and system proxy; wildcard system proxy uses reachable loopback')
        # An OS rollback can itself fail. Keep its new target alive until the
        # actual system settings are repaired, rather than leaving a dead proxy.
        before=cfg.read_text();old_endpoint=dict(endpoint);recovery_port=free_port();count=os_fixture()['proxyCommandCount']
        os_fixture(failProxyAfterWriteAt=count+first_enable,failProxyFrom=count+first_enable+1)
        choose('0.0.0.0');edit_port(recovery_port);auto.click('listener-apply');wait_for(ready,20)
        assert any(n.get('key')=='listener-error' for n in auto.tree()) and cfg.read_text()==before
        assert active()==f'0.0.0.0:{recovery_port}'
        endpoint.update(host='127.0.0.1',bind='0.0.0.0',port=recovery_port);assert api()[0]==200
        auto.capture(root/'system-proxy-recovery-listener.png')
        os_fixture(failProxyAfterWriteAt=0,failProxyFrom=0)
        stop();assert os_fixture()['services']==original
        endpoint.update(old_endpoint);launch();wait_for(lambda:proxy_target().get('HTTPEnable')==1)
        report['checks'].append('unrecoverable OS rollback retains reachable target listener and preserves saved address; clearing the failure permits normal quit/restart recovery')
        apply('0.0.0.0',free_port());auto.capture(root/'listener-applied.png')
        if nic:
            apply(nic,endpoint['port']);assert proxy_target()['HTTPProxy']==nic;send('/interface-system-proxy');apply('0.0.0.0',endpoint['port'])
        assert '# listener-fixture-preserve' in cfg.read_text()
        current=dict(endpoint);stop();assert os_fixture()['services']==original
        launch();assert active()==f'0.0.0.0:{current["port"]}' and node('listener-address').get('value')=='0.0.0.0'
        assert proxy_target()['HTTPPort']==current['port'];send('/after-restart')
        assert (home/'ca'/'mitmproxy-ca-cert.pem').read_bytes()==ca
        send_tls('/tls-after-restart')
        report['checks'].append('real HTTPS decryption and recording before/after live listener edits and after restart')
        auto.click('account-menu');auto.click('nav-settings');auto.click('preference-language')
        english=wait_for(lambda:next((n for n in auto.tree() if n.get('role') in ('option','listItem') and n.get('label')=='English'),None));auto.call('act',id=english['id'],action='press');wait_for(lambda:node('preference-language').get('value')=='English',15)
        auto.click('settings-proxy');auto.click('theme-toggle')
        wait_for(lambda:'theme: dark' in cfg.read_text());auto.call('idle',timeout=3000)
        auto.capture(root/'listener-dark-en.png')
        assert node('listener-apply')['label']=='Apply'
        stop();assert os_fixture()['services']==original
        report['checks'].append('NIC binding updates system proxy endpoint; selection/port/CA persist on restart; OS state restores on quit; English/dark UI')
        assert 'DATA RACE' not in (root/'app.log').read_text();report['passed']=True
    except Exception as err:
        report['error']=repr(err)
        if auto:
            try:auto.capture(root/'failure.png');(root/'failure-tree.json').write_text(json.dumps(auto.tree(),ensure_ascii=False,indent=2))
            except Exception:pass
        raise
    finally:
        if proc and proc.poll() is None:
            proc.send_signal(signal.SIGTERM)
            try:proc.wait(timeout=12)
            except subprocess.TimeoutExpired:proc.kill();proc.wait()
        server.shutdown();server.server_close()
        if tls_server:tls_server.shutdown();tls_server.server_close()
        log.close();(root/'report.json').write_text(json.dumps(report,ensure_ascii=False,indent=2));print(json.dumps({k:v for k,v in report.items() if k!='sources'},ensure_ascii=False,indent=2))


if __name__=='__main__':main()
