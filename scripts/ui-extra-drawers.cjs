// Exercise the production dynamic renderers with offline data only.
const assert = require("node:assert/strict");
module.exports = async ({ page, width, height, theme, out }) => {
  for (const name of ["info-collect", "webshell"])
    await page.addScriptTag({ url: "/static/js/" + name + ".js" });
  const activate = (id) =>
    page.evaluate(
      (id) =>
        document
          .querySelectorAll(".page")
          .forEach((e) => e.classList.toggle("active", e.id === id)),
      id,
    );
  const verify = async (id, name) => {
    await page.locator("#" + id + " .modal-body").waitFor();
    await page.waitForTimeout(220);
    const box = await page.locator("#" + id).evaluate((e) => {
      const p = e.querySelector(".modal-content"),
        b = e.querySelector(".modal-body"),
        h = e.querySelector(".modal-header h2"),
        r = p.getBoundingClientRect(),
        s = getComputedStyle(p);
      return {
        surface: e.dataset.uiSurface,
        x: r.x,
        right: r.right,
        width: r.width,
        height: r.height,
        radius: s.borderRadius,
        bg: s.backgroundColor,
        titleColor: getComputedStyle(h).color,
        bodyWidth: b.clientWidth,
        bodyScroll: b.scrollWidth,
        footer: e.querySelector(".modal-footer").getBoundingClientRect().bottom,
        expandedButton: !!e.querySelector(".app-drawer-expand"),
      };
    });
    assert.equal(box.surface, "drawer");
    assert.equal(box.radius, "0px");
    assert(box.expandedButton);
    assert(
      Math.abs(box.right - width) < 2 && Math.abs(box.height - height) < 2,
      JSON.stringify(box),
    );
    assert(
      box.width <= (width < 768 ? width : width - 48) + 1 && box.width > 0,
      JSON.stringify(box),
    );
    assert(
      box.footer <= height + 1 && box.bodyScroll <= box.bodyWidth + 2,
      JSON.stringify(box),
    );
    assert.equal(
      box.bg,
      theme === "dark" ? "rgb(17, 24, 39)" : "rgb(255, 255, 255)",
    );
    if (theme === "dark")
      assert(Number(box.titleColor.match(/\d+/)[0]) > 150, JSON.stringify(box));
    await page.screenshot({
      animations: "disabled",
      path: `${out}/extra-${name}-${width}-${theme}.png`,
    });
    if (width >= 768) {
      await page.locator("#" + id + " .app-drawer-expand").click();
      assert(
        (await page.locator("#" + id + " .modal-content").boundingBox()).width >
          box.width,
      );
      await page.locator("#" + id + " .app-drawer-expand").click();
    }
  };
  await activate("page-info-collect");
  await page.evaluate(() => {
    window.extraDrawerRequests = [];
    window.apiFetch = async (url, options = {}) => {
      extraDrawerRequests.push({ url, method: options.method || "GET" });
      return { ok: true, json: async () => ({ conversations: [] }) };
    };
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: async (text) => {
          window.extraDrawerCopied = text;
        },
      },
    });
    window.extraDrawerText =
      "Content-Security-Policy: " +
      "example.test ".repeat(120) +
      "\n" +
      "详细字段内容 <script>不应执行</script>\n".repeat(60);
    showCellDetailModal("响应头完整内容", extraDrawerText);
  });
  await verify("info-collect-cell-modal", "cell-fulltext");
  assert.equal(
    await page.locator(".info-collect-cell-modal-pre").textContent(),
    await page.evaluate(() => extraDrawerText),
  );
  await page.locator("#info-collect-cell-modal-copy").click();
  assert.equal(
    await page.evaluate(() => extraDrawerCopied),
    await page.evaluate(() => extraDrawerText),
  );
  await page.keyboard.press("Escape");
  assert.equal(await page.locator("#info-collect-cell-modal").count(), 0);
  await page.evaluate(() => {
    showCellDetailModal("旧字段", "旧值");
    showCellDetailModal("新字段", "新值");
  });
  assert.equal(await page.locator("#info-collect-cell-modal").count(), 1);
  await page.locator("#info-collect-cell-modal-ok").click();
  assert.equal(
    await page.evaluate(() =>
      document.body.classList.contains("app-modal-open"),
    ),
    false,
  );
  await page.evaluate(() =>
    showFofaParseModal("查找演示资产", {
      query: 'domain="example.test"',
      warnings: ["仅展示语法，未发起查询。".repeat(20)],
      explanation: "查询条件解释。\n".repeat(80),
    }),
  );
  await verify("fofa-parse-modal", "parse-result");
  await page.locator("#fofa-parse-query").fill('domain="demo.example.test"');
  await page.locator("#fofa-parse-apply").click();
  assert.equal(
    await page.locator("#fofa-query").inputValue(),
    'domain="demo.example.test"',
  );
  assert.equal(await page.locator("#fofa-parse-modal").count(), 0);
  assert.equal(
    await page.evaluate(() => extraDrawerRequests.length),
    0,
    "Applying syntax must not query a provider",
  );
  await page.evaluate(() => {
    showFofaParseModal("旧请求", { query: "old" });
    showFofaParseModal("新请求", { query: "new" });
  });
  await page.locator("#fofa-parse-query").waitFor();
  assert.equal(await page.locator("#fofa-parse-query").inputValue(), "new");
  await page.keyboard.press("Escape");
  assert.equal(await page.locator("#fofa-parse-modal").count(), 0);
  assert.equal(
    await page.evaluate(() =>
      document.body.classList.contains("app-modal-open"),
    ),
    false,
  );
  await activate("page-webshell");
  await page.evaluate(() => {
    // Keep terminal and persistence transports out of this UI fixture. The actual
    // workspace renderer and DB add/edit/close handlers still execute unchanged.
    initWebshellTerminal = () => {};
    queueWebshellPersistStateSave = () => {};
    webshellConnections = [
      {
        id: "offline-ui",
        name: "离线界面回归",
        url: "https://example.test",
        type: "php",
        project_id: "",
      },
    ];
    selectWebshell("offline-ui", true);
  });
  await page.locator('.webshell-tab[data-tab="db"]').click();
  await page.locator("#webshell-db-add-profile-btn").click();
  await verify("webshell-db-profile-modal", "db-add");
  await page.locator("#webshell-db-profile-cancel-btn").click();
  await page
    .locator('#webshell-db-profiles [data-action="edit"]')
    .first()
    .click();
  await verify("webshell-db-profile-modal", "db-edit");
  const toolbarBackground = await page
    .locator("#webshell-db-profile-modal .webshell-db-toolbar")
    .evaluate((e) => getComputedStyle(e).backgroundColor);
  if (theme === "dark")
    assert(Number(toolbarBackground.match(/\d+/)[0]) < 100, toolbarBackground);

  await page.locator("#webshell-db-type").selectOption("sqlite");
  assert(await page.locator("#webshell-db-sqlite-path").isVisible());
  assert(!(await page.locator("#webshell-db-host").isVisible()));
  await page.keyboard.press("Escape");
  assert(!(await page.locator("#webshell-db-profile-modal").isVisible()));
  assert.equal(
    await page.evaluate(() =>
      document.body.classList.contains("app-modal-open"),
    ),
    false,
  );
  const requests = await page.evaluate(() => extraDrawerRequests);
  assert(
    requests.every(
      (r) =>
        r.method === "GET" &&
        [
          "/api/roles",
          "/api/config",
          "/api/webshell/connections/offline-ui/ai-conversations",
          "/api/webshell/connections/offline-ui/ai-history",
        ].includes(r.url),
    ),
    JSON.stringify(requests),
  );
};
