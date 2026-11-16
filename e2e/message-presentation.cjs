const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
module.exports = async ({page,rpc,emit,room,openRoom,configure,screenshot,check,wait,channel,home,start,stop,preview}) => {
  const roomid = "R:10001", direct = "S:100_200";
  const update = presentation => rpc("ReadExtensionData",channel,{presentation});
  const metadata = {roomid,name:"项目协作群",kind:"group",avatar:"",members:{"100":{name:"金立义",avatar:"https://avatars.fixture.test/self.png"},"200":{name:"小虾",avatar:"https://avatars.fixture.test/peer.png"}}};
  await page.route("https://avatars.fixture.test/**", route => route.fulfill({contentType:"image/png",body:Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=","base64")}));
  await update(metadata);
  assert.equal((await rpc("ListRooms")).length,0);
  await emit(roomid,"old-text","新版已经准备好了",{from:"200",historical:true});
  const first = await room(roomid);
  await page.locator("#nav-messages").click();
  await openRoom(first.id);
  await wait(async()=> (await page.locator(".room-header h1").innerText()) === "项目协作群");
  const item = page.locator(`[data-room-id="${first.id}"]`);
  assert.equal(await item.locator("strong").innerText(),"项目协作群");
  assert.equal(await item.locator(".room-preview").innerText(),"小虾: 新版已经准备好了");
  assert.equal(await item.locator("img").count(),2);
  assert.equal(await page.locator('[data-message-id="old-text"] .room-message-meta span').first().innerText(),"小虾");
  const before=fs.readFileSync(path.join(home,"rooms",first.id,"context/messages.log"));
  await update({...metadata,name:"新群名",members:{...metadata.members,"200":{...metadata.members["200"],name:"小虾的备注"}}});
  await wait(async()=> (await page.locator(".room-header h1").innerText()) === "新群名");
  assert.equal(await item.locator(".room-preview").innerText(),"小虾的备注: 新版已经准备好了");
  assert.deepEqual(fs.readFileSync(path.join(home,"rooms",first.id,"context/messages.log")),before);
  assert.equal((await room(roomid)).updatedAt,first.updatedAt);
  assert.equal((await room(roomid)).tasks.length,0);
  check("Presentation sync names existing history and member avatars; metadata-only renames do not append messages, reorder activity or start Agent");
  for(const [type,label] of [["image","图片"],["file","文件"],["voice","语音"]]) {
    await emit(roomid,type,"unreadable-media-identifier",{from:"200",msgtype:type,historical:true});
    await wait(async()=> (await item.locator(".room-preview").innerText()).includes(`[${label}]`));
    await wait(async()=> (await page.locator(`[data-message-id="${type}"] .room-message-text`).innerText())===`[${label}]`);
    assert(!(await item.innerText()).includes("unreadable-media-identifier"));
  }
  await update({...metadata,roomid:direct,name:"小虾（备注）",kind:"direct",avatar:metadata.members["200"].avatar});
  const yesterday=new Date();yesterday.setDate(yesterday.getDate()-1);
  await emit(direct,"own","收到，谢谢",{from:"100",outgoing:true,historical:true,msgtime:yesterday.getTime()});
  const privateRoom=await room(direct), privateItem=page.locator(`[data-room-id="${privateRoom.id}"]`);
  await wait(async()=> await privateItem.locator(".room-preview").innerText()==="我: 收到，谢谢");
  assert.equal(await privateItem.locator("time").innerText(),"昨天");
  await openRoom(privateRoom.id);
  assert.equal(await page.locator('[data-message-id="own"] .room-agent-label').count(),0);
  assert.equal(await page.locator('[data-message-id="own"] img').getAttribute("src"),metadata.members["100"].avatar);
  await configure(direct,{name:"自己起的会话名"});
  await update({...metadata,roomid:direct,name:"平台改名",kind:"direct"});
  await wait(async()=> (await privateItem.locator("strong").innerText())==="自己起的会话名");
  await page.locator("#room-search").fill("新群名");
  assert.equal(await page.locator(".room-item").count(),1);
  await page.locator("#room-search").fill("");
  check("Readable media labels, self-message attribution, platform timestamps, custom-name priority and name search");
  const valid=(await room(roomid)).presentation;
  await assert.rejects(update({...metadata,avatar:"javascript:alert(1)"}));
  assert.deepEqual((await room(roomid)).presentation,valid);
  check("Host rejects invalid avatar schemes without changing saved presentation");
  for(const theme of ["light","dark"]) {
    await rpc("SetPreferences",{language:"zh-Hans",theme});
    await openRoom(first.id);
    for(const width of [1180,860]) {
      await page.setViewportSize({width,height:820});
      const splitter=page.locator("#sidebar-split");
      await splitter.press("End");
      await screenshot(`presentation-${theme}-${width}`);
    }
  }
  await rpc("SetPreferences",{language:"en",theme:"light"});
  await wait(async()=> (await item.locator(".room-preview").innerText()).includes("[Voice message]"));
  check("English type labels and light/dark screenshots at two window widths");
  await stop(); await start(); await page.goto(preview());
  await page.locator("#nav-messages").click();
  assert.equal((await room(roomid)).presentation.name,"新群名");
  assert.equal((await room(direct)).settings.name,"自己起的会话名");
  check("Presentation and custom names survive host restart without replaying chat history");
};
