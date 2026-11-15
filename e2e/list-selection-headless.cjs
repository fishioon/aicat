require("./traffic-default.cjs").run({
  scenario: "list-selection",
  verify: async (context) => {
    await context.page.locator("#nav-settings").click();
    await context.page.locator("#preference-language-zh-Hans").click();
    await context.page.waitForFunction(() => document.documentElement.lang === "zh-Hans");
    await context.page.locator("#nav-traffic").click();
    await context.page.locator("#recording").click();
    await context.wait(async () => (await context.current()).recording);
    return require("./list-selection.cjs")(context);
  },
}).catch((error) => { console.error(error); process.exitCode = 1; });
