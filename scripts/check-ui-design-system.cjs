/** Offline Chromium layout/keyboard regression checks using production HTML/CSS/renderers.
 * Install Playwright outside the source tree; run with NODE_PATH pointing to that node_modules.
 * Uses synthetic records; never starts the backend or sends requests to assessment targets.
 */
const { chromium } = require("playwright");
const fs = require("fs"),
  http = require("http"),
  path = require("path");
const root = path.resolve(__dirname, "..");
const out =
  process.env.CSAI_UI_ARTIFACTS ||
  fs.mkdtempSync(path.join(require("os").tmpdir(), "csai-ui-"));
fs.mkdirSync(out, { recursive: true });
const html = fs
  .readFileSync(root + "/web/templates/index.html", "utf8")
  .replace(/<script\b[^>]*>[\s\S]*?<\/script>/g, "")
  .replace(/\{\{\.Version\}\}/g, "UI preview")
  .replace(
    "</body>",
    '<script>window.switchPage=function(){};</script><script src="/static/vendor/i18next.min.js"></script><script src="/static/js/i18n.js"></script><script src="/static/js/modal.js"></script><script src="/static/js/mobile-ui.js"></script></body>',
  );
const server = http.createServer((req, res) => {
  const p = req.url.split("?")[0];
  if (p.startsWith("/api/")) {
    res.setHeader("Content-Type", "application/json");
    return res.end("{}");
  }
  if (p === "/") {
    res.setHeader("Content-Type", "text/html");
    return res.end(html);
  }
  const f = path.join(root, "web", p);
  if (!f.startsWith(root + "/web/") || !fs.existsSync(f)) {
    res.statusCode = 404;
    return res.end();
  }
  res.setHeader(
    "Content-Type",
    p.endsWith(".css")
      ? "text/css"
      : p.endsWith(".js")
        ? "application/javascript"
        : "application/octet-stream",
  );
  res.end(fs.readFileSync(f));
});
(async () => {
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  const browser = await chromium.launch({ headless: true });
  const findings = [];
  let count = 0;
  for (const width of process.env.CSAI_UI_WIDTHS
    ? process.env.CSAI_UI_WIDTHS.split(",").map(Number)
    : [1440, 1024, 390, 360])
    for (const theme of ["light", "dark"]) {
      const height =
        width === 360 ? 640 : width === 390 ? 844 : width === 1024 ? 768 : 900;
      const page = await browser.newPage({
        viewport: { width, height },
        colorScheme: theme,
        locale: "zh-CN",
      });
      page.on("pageerror", (e) =>
        findings.push({ width, theme, issue: e.message }),
      );
      await page.goto("http://127.0.0.1:" + server.address().port);
      await page.evaluate(() => window.i18nReady);
      await page.evaluate((t) => {
        document.documentElement.dataset.theme = t;
        document
          .querySelectorAll(".page")
          .forEach((e) => e.classList.remove("active"));
        document
          .getElementById("page-roles-management")
          .classList.add("active");
      }, theme);
      const ids = await page
        .locator('[data-ui-surface="drawer"]')
        .evaluateAll((els) => els.map((e) => e.id));
      for (const id of ids) {
        // A drawer nested in a page is visible only while that page is active.
        await page.evaluate((id) => {
          const e = document.getElementById(id);
          e.closest(".settings-section-content")?.classList.add("active");
          const parent = e.closest(".page");
          if (parent) {
            document
              .querySelectorAll(".page")
              .forEach((p) => p.classList.remove("active"));
            parent.classList.add("active");
          }
          openAppModal(id);
        }, id);
        await page.waitForTimeout(220);
        const info = await page.evaluate((id) => {
          const e = document.getElementById(id),
            p = e.firstElementChild,
            r = p.getBoundingClientRect(),
            s = getComputedStyle(p),
            b = p.querySelector(
              ".modal-body,.projects-modal-body,.c2-modal-body,#vulnerability-inspector-body",
            );
          return {
            id,
            x: r.x,
            y: r.y,
            w: r.width,
            h: r.height,
            bg: s.backgroundColor,
            corners: [
              s.borderTopLeftRadius,
              s.borderTopRightRadius,
              s.borderBottomRightRadius,
              s.borderBottomLeftRadius,
            ],
            overflow: b ? b.scrollWidth > b.clientWidth + 2 : false,
            footer: p
              .querySelector(
                ".modal-footer,.projects-modal-footer,.c2-modal-footer",
              )
              ?.getBoundingClientRect().bottom,
          };
        }, id);
        count++;
        const channel = Number(info.bg.match(/\d+/)?.[0]);
        if (
          (theme === "dark" && channel > 100) ||
          (theme === "light" && channel < 200)
        )
          findings.push({
            width,
            theme,
            id,
            issue: "surface theme mismatch",
            bg: info.bg,
          });
        if (
          info.x < -0.5 ||
          info.y < -0.5 ||
          Math.abs(info.x + info.w - width) > 2 ||
          Math.abs(info.h - height) > 2 ||
          info.footer > height + 1 ||
          info.overflow ||
          info.corners.some((radius) => radius !== "0px")
        )
          findings.push({ width, theme, ...info });
        if (
          [
            "role-modal",
            "asset-editor-modal",
            "vulnerability-modal",
            "skill-modal",
          ].includes(id)
        )
          await page.screenshot({
            animations: "disabled",
            path: out + "/" + id + "-" + width + "-" + theme + ".png",
          });
        await page.evaluate((id) => closeAppModal(id), id);
      }
      // Compact actions should remain short on mobile; test nested layer and restoration.
      await page.evaluate(() => {
        openAppModal("fact-detail-modal");
        openAppModal("fact-modal");
      });
      const nested = await page.evaluate(
        () =>
          Number(
            getComputedStyle(document.getElementById("fact-modal")).zIndex,
          ) >
          Number(
            getComputedStyle(document.getElementById("fact-detail-modal"))
              .zIndex,
          ),
      );
      if (!nested) throw new Error("nested stacking");
      await page.evaluate(() => {
        closeAppModal("fact-modal");
        closeAppModal("fact-detail-modal");
        openAppModal("project-modal");
      });
      const small = await page.locator("#project-modal > div").boundingBox();
      if (small.height >= height - 10)
        throw new Error("short dialog is full screen");
      await page.evaluate(() => closeAppModal("project-modal"));
      if (width === 1440) {
        await page.evaluate(() => openAppModal("role-modal"));
        await page.locator("#role-modal .app-drawer-expand").click();
        const expanded = await page.locator("#role-modal > div").boundingBox();
        if (expanded.width < 1200) throw new Error("expand failed");
        await page.evaluate(() => closeAppModal("role-modal"));
      }
      // Load production renderers with synthetic data; no backend or target execution.
      await page.evaluate(() => {
        window.apiFetch = async () => ({
          ok: true,
          json: async () => ({
            log: {
              createdAt: "2026-11-15T08:00:00Z",
              category: "config",
              action: "apply",
              result: "success",
              message: "配置已保存",
              detail: { source: "UI fixture" },
            },
          }),
        });
        window.showToast = (m) => console.log("TOAST", m);
      });
      for (const script of ["knowledge", "audit", "roles", "c2"])
        await page.addScriptTag({ url: "/static/js/" + script + ".js" });
      await page.evaluate(() => showAuditLogDetail("fixture"));
      await page.waitForTimeout(100);
      if (
        (await page
          .locator("#audit-detail-modal .app-drawer-expand")
          .count()) !== 1
      ) {
        console.log(
          await page
            .locator("#audit-detail-modal")
            .evaluateAll((es) => es.map((e) => e.outerHTML)),
        );
        throw new Error("deferred audit header missing");
      }
      await page.screenshot({
        animations: "disabled",
        path: out + "/audit-" + width + "-" + theme + ".png",
      });
      await page.keyboard.press("Escape");
      if (await page.locator("#audit-detail-modal").count())
        throw new Error("Escape did not run audit cleanup");
      await page.evaluate(() =>
        showRetrievalLogDetailsModal(
          {
            createdAt: "2026-11-15T08:00:00Z",
            query: "查找接口鉴权检查方法",
            riskType: "鉴权",
          },
          [
            {
              title: "接口鉴权检查",
              category: "Web",
              content: "验证不同角色之间的数据访问边界。",
            },
          ],
        ),
      );
      await page.waitForTimeout(100);
      await page.screenshot({
        animations: "disabled",
        path: out + "/retrieval-" + width + "-" + theme + ".png",
      });
      await page.evaluate(() => closeRetrievalLogDetailsModal());
      await page.route("**/api/**", (route) =>
        route.fulfill({ json: { profiles: [], projects: [], listeners: [] } }),
      );
      await page.evaluate(() => C2.showCreateListenerModal());
      await page.waitForTimeout(200);
      if ((await page.locator("#c2-modal .app-drawer-expand").count()) !== 1)
        throw new Error("C2 header missing");
      await page.screenshot({
        animations: "disabled",
        path: out + "/c2-" + width + "-" + theme + ".png",
      });
      await page.evaluate(() => C2.closeModal());
      // Test focus wrap, native close cleanup and restoration through real role handlers.
      await page.evaluate(() => {
        const b = document.createElement("button");
        b.textContent = "Open editor";
        document.body.appendChild(b);
        b.focus();
        window.fixtureFocus = b;
        openAppModal("role-modal");
      });
      await page.waitForTimeout(30);
      await page.locator("#role-modal .modal-footer .btn-primary").focus();
      await page.keyboard.press("Tab");
      if (
        !(await page.evaluate(() =>
          document
            .getElementById("role-modal")
            .contains(document.activeElement),
        ))
      )
        throw new Error("focus escaped");
      await page.keyboard.press("Escape");
      if (
        !(await page.evaluate(
          () =>
            !isAppModalOpen("role-modal") &&
            document.activeElement === window.fixtureFocus,
        ))
      )
        throw new Error("close/focus restore failed");
      await page.evaluate(() => {
        window.fixtureFocus.remove();
        roles = [
          {
            name: "安全审计",
            description: "检查授权范围内的代码与配置，整理可复核的证据。",
            tools: ["read_file", "search_code"],
            enabled: true,
          },
          {
            name: "报告整理",
            description: "汇总已验证发现，生成清晰的报告。",
            tools: [],
            enabled: true,
          },
        ];
        renderRolesList();
      });
      const pageIds = await page
        .locator(".page[id]")
        .evaluateAll((es) => es.map((e) => e.id));
      for (const id of pageIds) {
        await page.evaluate((id) => {
          document
            .querySelectorAll(".page")
            .forEach((e) => e.classList.toggle("active", e.id === id));
          if (window.CSAMobile)
            CSAMobile.wrapTables(document.getElementById(id));
        }, id);
        const overflow = await page.evaluate(
          () => document.documentElement.scrollWidth > innerWidth + 2,
        );
        if (overflow)
          findings.push({ width, theme, id, issue: "page overflow" });
        if (
          [
            "page-dashboard",
            "page-roles-management",
            "page-asset-library",
            "page-workflows",
            "page-settings",
          ].includes(id)
        )
          await page.screenshot({
            animations: "disabled",
            path: out + "/" + id + "-" + width + "-" + theme + ".png",
          });
      }
      await require("./ui-realistic-scenarios.cjs")({
        page,
        width,
        height,
        theme,
        out,
      });
      await require("./ui-extra-drawers.cjs")({
        page,
        width,
        height,
        theme,
        out,
      });
      await page.close();
    }
  fs.writeFileSync(
    out + "/results.json",
    JSON.stringify({ count, findings }, null, 2),
  );
  console.log(JSON.stringify({ count, findings, artifacts: out }, null, 2));
  if (findings.length) process.exitCode = 1;
  await browser.close();
  server.close();
})().catch((e) => {
  console.error(e);
  server.close();
  process.exit(1);
});
