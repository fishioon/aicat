#!/usr/bin/env python3
"""Run fixed plugin versions against frozen copies of the prototype host."""
import argparse,concurrent.futures,hashlib,json,pathlib,shutil,subprocess,time
ROOT=pathlib.Path(__file__).resolve().parent
CASES=[('weixin','2.4.8','text'),('weixin','3.0.0-beta-0','text'),('wecom','2026.10.19','text'),('wecom','2026.10.26','text'),('feishu','2026.7.16','registration'),('feishu','2026.7.16','root-alias'),('feishu','2026.8.5-beta.0','root-alias')]
NAMES={'weixin':'@tencent-weixin/openclaw-weixin','wecom':'@wecom/wecom-openclaw-plugin','feishu':'@larksuite/openclaw-lark'}
BASE={'weixin':'2.4.10','wecom':'2026.10.30'}
FILES=['loader.mjs','feishu-frame.mjs','host.mjs','runtime.mjs','store.mjs','network.mjs','profiles.mjs','fixtures.mjs','e2e.mjs','registration-probe.mjs']

def run_case(case,out):
 kind,version,mode=case;name=NAMES[kind];directory=out/(kind+'-'+version+'-'+mode);directory.mkdir(parents=True,exist_ok=True)
 for file in FILES:shutil.copyfile(ROOT/file,directory/file)
 shutil.copytree(ROOT/'shim',directory/'shim',dirs_exist_ok=True)
 pkg=json.loads((ROOT/'package.json').read_text());pkg['dependencies'][name]=version
 (directory/'package.json').write_text(json.dumps(pkg,indent=2)+'\n')
 shutil.copyfile(ROOT/'package-lock.json',directory/'package-lock.json')
 if kind in BASE:
  p=directory/'profiles.mjs';p.write_text(p.read_text().replace("version: '"+BASE[kind]+"'","version: '"+version+"'"))
 if mode=='root-alias':
  p=directory/'shim/package.json';meta=json.loads(p.read_text());meta['exports']['./plugin-sdk']='./sdk.mjs';p.write_text(json.dumps(meta,indent=2)+'\n')
 result={'kind':kind,'version':version,'mode':mode,'directory':str(directory),'hostFilesUnchanged':all((ROOT/file).read_bytes()==(directory/file).read_bytes() for file in FILES if file!='profiles.mjs'), 'profileChange':'version metadata only' if kind in BASE else 'none','sdkCodeUnchanged':(ROOT/'shim/sdk.mjs').read_bytes()==(directory/'shim/sdk.mjs').read_bytes()}
 started=time.monotonic()
 try:
  install=subprocess.run(['npm','install','--ignore-scripts','--legacy-peer-deps','--no-audit','--no-fund'],cwd=directory,capture_output=True,text=True,timeout=100)
  (directory/'install.log').write_text(install.stdout+install.stderr);result['installExit']=install.returncode
  if install.returncode:raise RuntimeError('dependency installation failed')
  command=['node','e2e.mjs',kind] if mode=='text' else ['node','registration-probe.mjs',name]
  result['command']=command
  completed=subprocess.run(command,cwd=directory,capture_output=True,text=True,timeout=45)
  (directory/'execution.log').write_text(completed.stdout+completed.stderr);result['exitCode']=completed.returncode
  if mode=='text':
   files=sorted((directory/'artifacts').glob('*/report.json'));result['report']=str(files[-1])
   report=json.loads(files[-1].read_text());result['harnessPassed']=report['harnessPassed'];result['textTransportPassed']=report['textTransportPassed'];result['policyCompatible']=report.get('policyCompatible');result['error']=next((s.get('error') for s in report['scenarios'] if s.get('error')),None)
  else:
   lines=completed.stdout.strip().splitlines();result['probe']=json.loads(lines[-1]) if lines else {'error':completed.stderr[-4000:]}
 except Exception as error:result['error']=str(error)
 result['seconds']=round(time.monotonic()-started,2)
 (directory/'case.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
 print(json.dumps(result,ensure_ascii=False),flush=True)
 return result

def main():
 p=argparse.ArgumentParser();p.add_argument('--out',required=True);args=p.parse_args();out=pathlib.Path(args.out).resolve();out.mkdir(parents=True,exist_ok=True)
 with concurrent.futures.ThreadPoolExecutor(max_workers=2) as executor:cases=list(executor.map(lambda case:run_case(case,out),CASES))
 report={'scope':'Frozen host/SDK source. Version metadata changes only. root-alias probe changes only SDK package exports. Exact resolved dependencies preserved in each case package-lock.json. Synthetic platform only.','cases':cases}
 (out/'matrix-report.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
 print('REPORT '+str(out/'matrix-report.json'))
if __name__=='__main__':main()
