const assert = require("node:assert/strict");

module.exports = async function ({
  page,
  rpc,
  emit,
  room,
  openRoom,
  configure,
  screenshot,
  check,
  wait,
  channel,
}) {
  async function seed(conversation) {
    const messages = Array.from({ length: 45 }, (_, index) => ({
      msgid: conversation + index,
      roomid: conversation,
      from: "成员",
      msgtype: "text",
      historical: true,
      msgtime: Date.now(),
      text: { content: "历史消息 " + index + "\n第一行\n第二行\n第三行" },
    }));
    for (let offset = 0; offset < messages.length; offset += 10)
      await rpc("ReadExtensionData", channel, {
        messages: messages.slice(offset, offset + 10),
      });
    return (await room(conversation)).id;
  }
  const box = page.locator("#room-messages");
  const follow = page.locator("#room-follow-latest");
  const position = () =>
    box.evaluate((e) => ({
      top: e.scrollTop,
      gap: e.scrollHeight - e.clientHeight - e.scrollTop,
    }));
  const atLatest = async () =>
    await wait(async () => Math.abs((await position()).gap) < 2);
  const first = await seed("follow-a");
  const second = await seed("follow-b");
  await openRoom(first);
  await atLatest();
  assert(await follow.isChecked());
  await emit("follow-a", "fresh", "新入站消息");
  await page.locator('[data-message-id="fresh"]').waitFor();
  await atLatest();
  await configure("follow-a", { automatic: true, model: "room-a" });
  await emit("follow-a", "agent-reply", "请回复以验证自动跟随");
  await wait(async () =>
    (await room("follow-a")).messages.some((m) => m.reply_to === "agent-reply"),
  );
  await wait(
    async () => (await page.locator(".room-message.outgoing").count()) > 0,
  );
  await atLatest();
  check(
    "message entry, new ingress and actual Agent MCP saved replies remain at the latest message",
  );
  await configure("follow-a", { automatic: false });
  await box.hover();
  await page.mouse.wheel(0, -650);
  await wait(async () => !(await follow.isChecked()));
  const paused = (await position()).top;
  await emit("follow-a", "while-reading", "读取历史期间的新消息");
  await page.locator('[data-message-id="while-reading"]').waitFor();
  assert(Math.abs((await position()).top - paused) < 2);
  await follow.check();
  await atLatest();
  await follow.uncheck();
  await emit("follow-a", "explicit-pause", "手动暂停跟随后的新消息\n更多内容");
  await page.locator('[data-message-id="explicit-pause"]').waitFor();
  assert(!(await follow.isChecked()));
  assert((await position()).gap > 20);
  await box.evaluate((e) => {
    e.scrollTop = e.scrollHeight;
  });
  await wait(async () => await follow.isChecked());
  await emit("follow-a", "manual-bottom", "回到最底部后继续跟随");
  await page.locator('[data-message-id="manual-bottom"]').waitFor();
  await atLatest();
  check(
    "reading history or unchecking pauses follow; rechecking or manually returning to the bottom resumes it",
  );
  await page.locator(".room-quote").last().click();
  await wait(async () => !(await follow.isChecked()));
  await openRoom(second);
  await atLatest();
  assert(await follow.isChecked());
  await page.setViewportSize({ width: 860, height: 650 });
  await atLatest();
  await screenshot("messages-follow-latest-light");
  await rpc("SetPreferences", { language: "en", theme: "dark" });
  await wait(
    async () =>
      (await page.locator("#messages-page .follow-latest").innerText()).trim() ===
      "Follow latest",
  );
  await atLatest();
  await screenshot("messages-follow-latest-dark-english");
  check(
    "following resets for another Room, survives viewport resizing and appears in Chinese/light and English/dark layouts",
  );
};
