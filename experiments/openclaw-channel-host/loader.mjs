import {createJiti} from 'jiti';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

// Native ESM first; generic CJS/import.meta interoperability fallback, without vendor source edits.
export async function loadPlugin(entry, stateDir) {
  try {
    const loaded = await import(entry);
    return {plugin: loaded.default?.default ?? loaded.default ?? loaded, mode: 'native'};
  } catch (error) {
    if (!/exports is not defined|require is not defined|ERR_REQUIRE_ESM|Cannot use ['"]import.meta['"] outside a module/.test(String(error))) throw error;
    const jiti = createJiti(import.meta.url, {tryNative: false, fsCache: path.join(stateDir, 'module-cache')});
    const loaded = await jiti.import(fileURLToPath(entry));
    return {plugin: loaded.default?.default ?? loaded.default ?? loaded, mode: 'jiti'};
  }
}
