#!/usr/bin/env python3
"""Static import/version inventory. Never loads official plugins or contacts a platform."""
import argparse,base64,concurrent.futures,hashlib,io,json,pathlib,re,tarfile,urllib.request
ROOT=pathlib.Path(__file__).resolve().parent
SAMPLES={
 'weixin':('@tencent-weixin/openclaw-weixin',['2.4.8','2.4.10','3.0.0-beta-0']),
 'wecom':('@wecom/wecom-openclaw-plugin',['2026.10.19','2026.10.26','2026.10.30']),
 'feishu':('@larksuite/openclaw-lark',['2026.7.9','2026.7.16','2026.8.5-beta.0']),
}

def fetch(url):
 with urllib.request.urlopen(urllib.request.Request(url,headers={'User-Agent':'aicat-compat-audit'}),timeout=40) as response:return response.read()

def inventory(kind,name,version,out):
 target=out/'packages'/kind/version;target.mkdir(parents=True,exist_ok=True)
 meta=json.loads(fetch('https://registry.npmjs.org/'+name.replace('/','%2f')+'/'+version))
 blob=fetch(meta['dist']['tarball'])
 integrity='sha512-'+base64.b64encode(hashlib.sha512(blob).digest()).decode()
 assert integrity==meta['dist']['integrity'],(name,version,'integrity mismatch')
 imports={};dynamic=set();refs=set();evidence={};files=0
 with tarfile.open(fileobj=io.BytesIO(blob),mode='r:gz') as archive:
  for entry in archive:
   if not entry.isfile() or entry.size>8*1024*1024:continue
   rel=pathlib.PurePosixPath(*pathlib.PurePosixPath(entry.name).parts[1:])
   if rel.is_absolute() or '..' in rel.parts:raise ValueError('invalid package path')
   content=archive.extractfile(entry).read();file=target/rel;file.parent.mkdir(parents=True,exist_ok=True);file.write_bytes(content)
   if not str(rel).endswith(('.js','.mjs','.cjs')) or '.test.' in str(rel):continue
   text=content.decode('utf8');files+=1
   # Scope is literal imports and literal runtime paths, not a complete call graph.
   for match in re.finditer(r'(?:^|;)\s*(?:import|export)\s+([^;]*?)\s+from\s*[\'"](openclaw/plugin-sdk[^\'"]*)[\'"]',text,re.M):
    clause,module=match.groups();symbols=[]
    named=re.search(r'\{([^}]+)\}',clause)
    if named:symbols=[x.strip().split(' as ')[0].strip() for x in named.group(1).split(',') if x.strip()]
    else:symbols=['*']
    imports.setdefault(module,set()).update(symbols)
   dynamic.update(re.findall(r'import\([\'"](openclaw/plugin-sdk[^\'"]*)[\'"]\)',text))
   # Published Feishu packages are CommonJS; count referenced namespace properties too.
   for match in re.finditer(r'(?:const|let|var)\s+(\w+)\s*=\s*(?:__importStar\()?require\([\'"](openclaw/plugin-sdk[^\'"]*)[\'"]\)',text):
    alias,module=match.groups()
    symbols=set(re.findall(r'\b'+re.escape(alias)+r'\.(\w+)',text))
    imports.setdefault(module,set()).update(symbols or {'*'})

   for match in re.finditer(r'(?:core|LarkClient\.runtime|api\.runtime|deps\.channelRuntime|ctx\.channelRuntime)\.((?:channel\.)?[A-Za-z_$][\w$]*(?:\??\.[A-Za-z_$][\w$]*)+)\s*\(',text):
    ref=match.group(1).replace('?.','.')
    if ref.startswith(('channel.','reply.','session.','commands.','routing.','media.','text.','pairing.','config.','system.')):
     if ref.startswith(('reply.','session.','commands.','routing.','media.','text.','pairing.')):ref='channel.'+ref
     refs.add(ref);evidence.setdefault(ref,{'file':str(rel),'line':text[:match.start()].count('\n')+1})
 result={'kind':kind,'package':name,'version':version,'integrity':integrity,'tarball':meta['dist']['tarball'],
  'engines':meta.get('engines',{}),'peerDependencies':meta.get('peerDependencies',{}),
  'runtimeEntries':meta.get('openclaw',{}).get('runtimeExtensions',meta.get('openclaw',{}).get('extensions',[])),
  'staticSdkImports':{k:sorted(v) for k,v in sorted(imports.items())},'dynamicSdkImports':sorted(dynamic),
  'literalRuntimeCalls':sorted(refs),'runtimeCallEvidence':evidence,'scannedJavaScriptFiles':files,
  'moduleCount':len(set(imports)|dynamic),'symbolCount':len(set(x for values in imports.values() for x in values if x!='*'))}
 (target/'audit.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
 return result

def main():
 parser=argparse.ArgumentParser();parser.add_argument('--out',required=True);args=parser.parse_args();out=pathlib.Path(args.out).resolve();out.mkdir(parents=True,exist_ok=True)
 jobs=[(kind,name,version) for kind,(name,versions) in SAMPLES.items() for version in versions]
 with concurrent.futures.ThreadPoolExecutor(max_workers=3) as executor:results=list(executor.map(lambda job:inventory(*job,out),jobs))
 shim='\n'.join(file.read_text() for file in sorted((ROOT/'shim').glob('*.mjs')));exports=set(re.findall(r'export\s+(?:async\s+)?(?:function|const)\s+(\w+)',shim))
 report={'scope':'Literal shipped JavaScript imports/calls; excludes computed paths and plugin-internal SDK behavior. Counts are lower bounds, not implementation estimates.','samples':results,'sdkExportsInPrototype':sorted(exports),'comparisons':[]}
 for kind,(_,versions) in SAMPLES.items():
  samples=[next(x for x in results if x['kind']==kind and x['version']==v) for v in versions]
  for previous,current in zip(samples,samples[1:]):
   flatten=lambda value:set(module+'#'+symbol for module,symbols in value['staticSdkImports'].items() for symbol in symbols)
   report['comparisons'].append({'kind':kind,'from':previous['version'],'to':current['version'],
    'addedImports':sorted(flatten(current)-flatten(previous)),'removedImports':sorted(flatten(previous)-flatten(current)),
    'addedLiteralRuntimeCalls':sorted(set(current['literalRuntimeCalls'])-set(previous['literalRuntimeCalls'])),
    'removedLiteralRuntimeCalls':sorted(set(previous['literalRuntimeCalls'])-set(current['literalRuntimeCalls']))})
 for result in results:result['symbolsAbsentFromPrototype']=sorted(set(x for values in result['staticSdkImports'].values() for x in values if x!='*')-exports)
 report['localCode']={file:len([line for line in (ROOT/file).read_text().splitlines() if line.strip() and not line.strip().startswith('//')]) for file in ['runtime.mjs','shim/sdk.mjs','profiles.mjs','host.mjs','store.mjs']}
 (out/'audit-report.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
 for result in results:print(json.dumps({k:result[k] for k in ['kind','version','moduleCount','symbolCount','symbolsAbsentFromPrototype','literalRuntimeCalls']},ensure_ascii=False))
 print(json.dumps({'report':str(out/'audit-report.json'),'comparisons':report['comparisons'],'localCode':report['localCode']},ensure_ascii=False))
if __name__=='__main__':main()
