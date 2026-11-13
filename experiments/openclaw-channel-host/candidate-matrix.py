#!/usr/bin/env python3
"""Narrow, shared compatibility overlays; never patches official plugin files."""
import argparse,concurrent.futures,json,pathlib,shutil,subprocess
ROOT=pathlib.Path(__file__).resolve().parent

MEDIA='''    media: {saveMediaBuffer: async (buffer, contentType, subdir = 'inbound', maxBytes = 16777216, name = '') => {
      if (!Buffer.isBuffer(buffer) || buffer.length > Math.min(maxBytes, 16777216)) throw Error('MEDIA_LIMIT');
      if (subdir.includes('..') || path.isAbsolute(subdir)) throw Error('INVALID_MEDIA_SUBDIR');
      const dir = path.join(stateDir, 'media', subdir); fs.mkdirSync(dir, {recursive: true, mode: 0o700});
      const file = path.join(dir, crypto.randomUUID() + path.extname(path.basename(name)).slice(0, 16));
      const fd = fs.openSync(file, 'wx', 0o600);
      try {fs.writeFileSync(fd, buffer); fs.fsyncSync(fd);} finally {fs.closeSync(fd);}
      return {path: file, contentType};
    }},
'''
AUTH='''
// Text-only legacy authorization subset; pairing mutation and commands remain unsupported.
export async function resolveSenderCommandAuthorizationWithRuntime(params) {
  if (params.isGroup) throw Error('UNSUPPORTED: legacy group authorization');
  const allowed = [...(params.configuredAllowFrom ?? []), ...(await params.readAllowFromStore())];
  const senderAllowedForCommands = params.dmPolicy !== 'disabled' && allowed.includes(params.senderId);
  return {senderAllowedForCommands, commandAuthorized: false};
}
export function resolveDirectDmAuthorizationOutcome(params) {
  if (params.isGroup) throw Error('UNSUPPORTED: legacy group authorization');
  return params.dmPolicy === 'disabled' ? 'disabled' : params.senderAllowedForCommands ? 'allowed' : 'unauthorized';
}
export function withFileLock() {throw Error('UNSUPPORTED: interactive pairing writes');}
'''

def run_case(case,out):
 kind,version,mode=case
 original=out/'matrix'/(kind+'-'+version+('-root-alias' if kind=='feishu' else '-text'))
 directory=out/'candidates'/(kind+'-'+version+'-'+mode)
 directory.mkdir(parents=True,exist_ok=True)
 for name in ['loader.mjs','feishu-frame.mjs','host.mjs','runtime.mjs','store.mjs','network.mjs','profiles.mjs','fixtures.mjs','e2e.mjs','registration-probe.mjs','package.json','package-lock.json']:
  shutil.copyfile(original/name,directory/name)
 shutil.copytree(original/'shim',directory/'shim',dirs_exist_ok=True)
 # Copy the exact installed dependency graph and byte-identical official packages; local SDK is linked anew.
 shutil.copytree(original/'node_modules',directory/'node_modules',dirs_exist_ok=True,symlinks=True)
 result={'kind':kind,'version':version,'mode':mode,'directory':str(directory),'officialPluginFilesModified':False}
 if kind=='weixin':
  if mode=='legacy-auth':
   p=directory/'shim/sdk.mjs';p.write_text(p.read_text()+AUTH);result['addedSdkNonblankLines']=len([l for l in AUTH.splitlines() if l.strip() and not l.strip().startswith('//')])
  else:
   p=directory/'runtime.mjs';s=p.read_text().replace("import path from 'node:path';","import path from 'node:path';\nimport fs from 'node:fs';\nimport crypto from 'node:crypto';").replace('    channel: {','    channel: {\n'+MEDIA)
   p.write_text(s);result['addedRuntimeNonblankLines']=len(MEDIA.splitlines())+2
  command=['node','e2e.mjs',kind]
 else:
  pkg=json.loads((directory/'package.json').read_text());pkg['dependencies']['jiti']='2.7.0';(directory/'package.json').write_text(json.dumps(pkg,indent=2)+'\n')
  install=subprocess.run(['npm','install','--ignore-scripts','--legacy-peer-deps','--no-audit','--no-fund'],cwd=directory,text=True,capture_output=True,timeout=100)
  (directory/'install.log').write_text(install.stdout+install.stderr);assert install.returncode==0,install.stderr
  p=directory/'registration-probe.mjs';s=p.read_text().replace("import {Store} from './store.mjs';","import {Store} from './store.mjs';\nimport {createJiti} from 'jiti';")
  s=s.replace("const {default: plugin} = await import(pathToFileURL(path.join(root, entry)));", "const jiti = createJiti(import.meta.url, {tryNative: false, fsCache: path.join(directory, 'jiti-cache')});\n  const loaded = await jiti.import(path.join(root, entry));\n  const plugin = loaded.default?.default ?? loaded.default ?? loaded;")
  s=s.replace("registerHttpRoute: () => registrations.push('http'), on:","registerHttpRoute: () => registrations.push('http'),\n    registerCli: () => registrations.push('cli'), registerCommand: () => registrations.push('command'), on:")
  p.write_text(s);result['loader']='jiti@2.7.0, generic transform, SDK root alias, two registration collectors'
  command=['node','registration-probe.mjs','@larksuite/openclaw-lark']
 try:
  completed=subprocess.run(command,cwd=directory,capture_output=True,text=True,timeout=45)
  (directory/'execution.log').write_text(completed.stdout+completed.stderr);result['exitCode']=completed.returncode
  if kind=='weixin':
   report=sorted((directory/'artifacts').glob('*/report.json'))[-1];result['report']=str(report);result['outcome']=json.loads(report.read_text())
  else:result['outcome']=json.loads(completed.stdout.strip().splitlines()[-1])
 except Exception as error:result['error']=str(error)
 # Check the entire official package against the frozen original, not just a before/after hash.
 name='@tencent-weixin/openclaw-weixin' if kind=='weixin' else '@larksuite/openclaw-lark'
 before=original/'node_modules'/name;after=directory/'node_modules'/name
 result['officialPackageByteIdentical']=all((after/f.relative_to(before)).read_bytes()==f.read_bytes() for f in before.rglob('*') if f.is_file() and 'node_modules' not in f.relative_to(before).parts)
 (directory/'case.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
 print(json.dumps({k:v for k,v in result.items() if k!='outcome'}|{'summary':result.get('outcome',{}).get('status',result.get('outcome',{}))},ensure_ascii=False),flush=True)
 return result

def main():
 p=argparse.ArgumentParser();p.add_argument('--out',required=True);args=p.parse_args();out=pathlib.Path(args.out).resolve()
 cases=[('weixin','2.4.8','legacy-auth'),('weixin','3.0.0-beta-0','shared-media'),('feishu','2026.7.16','generic-loader')]
 with concurrent.futures.ThreadPoolExecutor(max_workers=2) as executor:results=list(executor.map(lambda case:run_case(case,out),cases))
 (out/'candidate-report.json').write_text(json.dumps({'scope':'Measured text/registration compatibility changes, not full feature implementations. Main host and official plugin files remain unchanged.','cases':results},ensure_ascii=False,indent=2)+'\n')
if __name__=='__main__':main()
