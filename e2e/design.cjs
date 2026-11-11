// Interactive contract for the current local design, without external services.
const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require(process.env.AICAT_PLAYWRIGHT || 'playwright');
(async()=>{
 const source=process.argv[2],out=path.resolve(process.argv[3]||'build/design-acceptance');fs.mkdirSync(out,{recursive:true});
 const browser=await chromium.launch({headless:true, executablePath:process.env.AICAT_CHROMIUM});const page=await browser.newPage({viewport:{width:1180,height:960},colorScheme:'dark'});const errors=[];page.on('pageerror',e=>errors.push(e.message));
 const report={passed:false,source,checks:[]};
 try{
  await page.evaluate(()=>{Object.defineProperty(navigator,'clipboard',{value:{writeText:async text=>{window.copiedRaw=text;}}});});
  await page.setContent(fs.readFileSync(source,'utf8'));
  const root=page.locator('#aicat-shell-draft');
  if(process.argv.includes('--empty-pins-only')){
    await root.locator('[data-domain-row="api.openai.com"]').first().click({button:'right'});
    await root.locator('.ac-context-menu [data-pin="api.openai.com"]').click();
    if((await root.locator('#ac-pin-children').textContent()).trim())throw Error('unexpected empty-pinned placeholder');
    const gap=await root.evaluate(el=>{const a=el.querySelector('[data-action="toggle-pins"]').closest('.ac-domain-parent').getBoundingClientRect();const b=el.querySelector('[data-action="all-domains"]').closest('.ac-domain-parent').getBoundingClientRect();return b.top-a.bottom;});
    if(Math.abs(gap-17)>.1)throw Error('extra empty-pinned gap '+gap);
    report.checks.push('unpinning the last domain keeps header and zero count, without placeholder text or extra gap');
    await page.screenshot({path:path.join(out,'empty-pins.png'),fullPage:true});report.passed=true;return;
  }
  await root.locator('[data-action="theme"]').click();
  if(await root.getAttribute('data-theme')!=='light')throw Error('system dark theme toggle did not switch to light');
  await root.locator('[data-action="record"]').click();if(!(await root.locator('[data-record-label]').textContent()).includes('Resume'))throw Error('pause label');await root.locator('[data-action="record"]').click();
  await root.locator('[data-action="copy-inspector"]').first().click();
  if(!(await page.evaluate(()=>window.copiedRaw)).startsWith('POST /v1/responses HTTP/1.1\n'))throw Error('Raw copy contains gutter or wrong record');
  report.checks.push('system theme toggle, capture toggle and actual clipboard request without line numbers');
  await root.locator('[data-select="r2"]').click();await root.locator('[data-action="back"]').click();if(await root.locator('[data-request="r1"]').getAttribute('data-selected')!=='true')throw Error('history selection');await root.locator('[data-action="forward"]').click();
  await root.locator('[data-action="profile-menu"]').click();if(await root.locator('.ac-profile-menu button:disabled').count())throw Error('dead account button');await root.locator('[data-action="open-settings"]').click();
  await root.locator('[data-language]').selectOption('zh-CN');if(await root.getAttribute('lang')!=='zh-CN')throw Error('Chinese switch');
  await root.locator('[data-setting="features"]').first().click();await root.locator('[data-feature-traffic]').uncheck();if(await root.locator('[data-module="traffic"]').isVisible())throw Error('disabled feature navigation remains visible');
  await root.locator('[data-feature-traffic]').check();await root.locator('[data-module="traffic"]').click();
  const search=root.locator('[data-query]');await search.click();await search.pressSequentially('RESPONSES');if(await root.locator('[data-request]').count()!==2)throw Error('URL filter '+await root.locator('[data-request]').count()+' '+await search.inputValue());if(!await search.evaluate(e=>document.activeElement===e))throw Error('search focus');
  await root.locator('[data-action="clear"]').click();if(await root.locator('[data-request]').count())throw Error('clear');if(await root.locator('[data-query]').inputValue()!=='')throw Error('clear filter');
  if(!await root.locator('[data-domain-row="api.openai.com"]').count())throw Error('clear removed pin');
  report.checks.push('request history, account menu, language, feature toggle, URL input focus and clear retaining pin');
  await page.screenshot({path:path.join(out,'design-final.png'),fullPage:true});if(errors.length)throw Error(errors.join('\n'));report.passed=true;
 }catch(e){report.error=String(e);await page.screenshot({path:path.join(out,'failure.png'),fullPage:true});process.exitCode=1;}
 finally{fs.writeFileSync(path.join(out,'report.json'),JSON.stringify(report,null,2));console.log(JSON.stringify(report,null,2));await browser.close();}
})();
