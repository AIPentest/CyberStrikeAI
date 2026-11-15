// Anonymized reproductions of populated UI states: 365 tasks, 626 tools,
// 614 enabled tools, multi-file skill, 50 long findings. All writes are rejected.
const assert = require("node:assert/strict");
module.exports = async function ({ page, width, height, theme, out }) {
  for (const script of [
    "tasks",
    "skills",
    "vulnerability",
    "vulnerability-workspace",
  ])
    await page.addScriptTag({ url: "/static/js/" + script + ".js" });
  await page.evaluate(() => {
    const tools = Array.from({ length: 626 }, (_, i) => ({
      name: "review_tool_" + i,
      description: "检查授权范围内的接口、权限和配置，保留可复核证据。".repeat(
        4,
      ),
      enabled: i < 614,
    }));
    const role = {
      name: "界面回归角色",
      description: "真实规模的长描述与工具清单",
      enabled: true,
      tools: [],
      user_prompt: "根据已有证据整理报告。".repeat(12),
    };
    const queue = {
      id: "ui-fixture",
      title: "多目标授权评估回归队列",
      status: "paused",
      role: role.name,
      agentMode: "single",
      scheduleMode: "manual",
      concurrency: 2,
      totalTasks: 365,
      completedTasks: 34,
      createdAt: "2026-11-10T13:01:13Z",
      tasks: Array.from({ length: 365 }, (_, i) => ({
        id: "task-" + i,
        status: i < 34 ? "completed" : "pending",
        message:
          "https://long-service-name-" + i + ".example.test/verification",
      })),
    };
    window.fixtureQueue = queue;
    window.apiFetch = async (input, opts = {}) => {
      if (opts.method && opts.method !== "GET")
        throw new Error("UI regression must not write: " + input);
      const url = new URL(input, location.origin);
      let data = {};
      if (url.pathname === "/api/roles") data = { roles: [role] };
      else if (url.pathname === "/api/config/tools") {
        const size = Number(url.searchParams.get("page_size") || 20),
          index = Number(url.searchParams.get("page") || 1);
        data = {
          tools: tools.slice((index - 1) * size, index * size),
          total: 626,
          total_enabled: 614,
          total_pages: Math.ceil(626 / size),
          page: index,
          page_size: size,
        };
      } else if (url.pathname === "/api/batch-tasks/ui-fixture")
        data = { queue };
      else if (url.pathname.endsWith("/files"))
        data = {
          files: [
            { path: "SKILL.md" },
            { path: "references/long-reference-name.md" },
            { path: "assets/README.txt" },
          ],
        };
      else if (url.pathname.startsWith("/api/skills/"))
        data = {
          skill: {
            id: "review-skill",
            description: "多文件技能包编辑测试",
            name: "review-skill",
            path: "skills/review-skill",
            scripts: [
              { rel_path: "scripts/check.py", description: "离线证据校核" },
            ],
            content:
              url.searchParams.get("depth") === "summary"
                ? "# 技能摘要\n按需加载正文。"
                : "# 审核步骤\n" + "核对已有证据。\n".repeat(40),
          },
        };
      return { ok: true, json: async () => data };
    };
    window.showNotification = (message, type) => {
      if (type === "error") throw new Error(message);
    };
    roles = [role];
  });
  const activate = async (id) =>
    page.evaluate(
      (id) =>
        document
          .querySelectorAll(".page")
          .forEach((e) => e.classList.toggle("active", e.id === id)),
      id,
    );
  const shot = async (name) =>
    page.screenshot({
      animations: "disabled",
      path: `${out}/real-${name}-${width}-${theme}.png`,
    });
  const contrastResults = [];
  await activate("page-tasks");
  const states = [
    { status: "paused", scheduleMode: "manual", label: "继续执行" },
    { status: "pending", scheduleMode: "manual", label: "开始执行" },
    {
      status: "pending",
      scheduleMode: "cron",
      scheduleEnabled: true,
      label: "立即执行一轮",
    },
    {
      status: "pending",
      scheduleMode: "cron",
      scheduleEnabled: false,
      label: "开始执行",
    },
    { status: "running", scheduleMode: "manual" },
    { status: "completed", scheduleMode: "manual" },
    { status: "cancelled", scheduleMode: "manual" },
  ];
  for (const state of states) {
    await page.evaluate(async (state) => {
      Object.assign(fixtureQueue, state);
      await showBatchQueueDetail("ui-fixture");
    }, state);
    await page
      .locator("#batch-queue-detail-content .batch-task-item")
      .first()
      .waitFor();
    const layout = await page
      .locator("#batch-queue-detail-modal .modal-header")
      .evaluate((e) => {
        const title = e.querySelector("h2").getBoundingClientRect(),
          header = e.getBoundingClientRect();
        const controls = Array.from(e.querySelectorAll("button"))
          .filter((b) => b.getClientRects().length)
          .map((b) => b.getBoundingClientRect());
        return {
          titleWidth: title.width,
          headerHeight: header.height,
          allInside: controls.every(
            (r) => r.left >= header.left && r.right <= header.right + 1,
          ),
          titleOverlaps: controls.some(
            (r) =>
              r.left < title.right &&
              r.right > title.left &&
              r.top < title.bottom &&
              r.bottom > title.top,
          ),
        };
      });
    assert(layout.titleWidth > 200, JSON.stringify(layout));
    assert(layout.headerHeight < 190, JSON.stringify(layout));
    assert(layout.allInside && !layout.titleOverlaps, JSON.stringify(layout));
    await page.evaluate(() => applyTranslations(document.body));
    const assertLabels = async () => {
      const text = await page.locator("#batch-queue-detail-title").innerText();
      assert.equal(text, "批量任务队列 - 多目标授权评估回归队列");
      if (state.label)
        assert.equal(
          await page.locator("#batch-queue-start-btn").innerText(),
          state.label,
        );
      else
        assert.equal(
          await page.locator("#batch-queue-start-btn").isVisible(),
          false,
        );
    };
    await assertLabels();
    // Real breakpoint transitions call mobile boot, which reapplies all translations.
    if (width === 1440 && state.label) {
      await page.setViewportSize({ width: 390, height: 844 });
      await page.waitForTimeout(150);
      await assertLabels();
      await page.setViewportSize({ width, height });
      await page.waitForTimeout(150);
      await assertLabels();
    }
    for (const button of await page
      .locator(
        "#batch-queue-detail-modal .modal-header-actions .btn-primary:visible",
      )
      .all()) {
      for (const appearance of ["normal", "hover", "focus"]) {
        if (appearance === "normal")
          await page.mouse.move(width - 4, height - 4);
        if (appearance === "hover") await button.hover();
        if (appearance === "focus") {
          await page.mouse.move(width - 4, height - 4);
          await button.focus();
          await page.keyboard.press("Tab");
          await page.keyboard.press("Shift+Tab");
          assert(
            await button.evaluate((e) => e.matches(":focus-visible")),
            "Keyboard focus indicator missing",
          );
        }
        await page.waitForTimeout(220);
        const colors = await button.evaluate((e) => ({
          foreground: getComputedStyle(e).color,
          background: getComputedStyle(e).backgroundColor,
          disabled: e.disabled,
          text: e.textContent,
        }));
        assert(!colors.disabled);
        const rgb = (value) => {
          assert(/^rgb/.test(value), JSON.stringify(colors));
          const values = value.match(/[\d.]+/g).map(Number);
          assert(
            values.length === 3 || values[3] === 1,
            "Primary background must be opaque",
          );
          return values.slice(0, 3).map((v) => {
            const c = v / 255;
            return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
          });
        };
        const lum = (value) => {
          const [r, g, b] = rgb(value);
          return 0.2126 * r + 0.7152 * g + 0.0722 * b;
        };
        const a = lum(colors.foreground),
          b = lum(colors.background);
        const ratio = (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
        contrastResults.push({
          state: state.status,
          label: state.label,
          appearance,
          ratio,
          ...colors,
        });
        assert(
          ratio >= 4.5,
          JSON.stringify({ width, theme, appearance, ratio, ...colors }),
        );
      }
    }
    if (state.status === "paused") await shot("queue");
    await page.evaluate(() => closeBatchQueueDetailModal());
  }
  // Queue names are literal content, including characters that look like markup.
  await page.evaluate(async () => {
    fixtureQueue.title = "复核 & <说明>";
    fixtureQueue.status = "paused";
    await showBatchQueueDetail("ui-fixture");
    applyTranslations(document.body);
  });
  assert.equal(
    await page.locator("#batch-queue-detail-title").innerText(),
    "批量任务队列 - 复核 & <说明>",
  );
  assert.equal(await page.locator("#batch-queue-detail-title > *").count(), 1);
  await page.evaluate(async () => {
    fixtureQueue.title = "";
    await showBatchQueueDetail("ui-fixture");
    applyTranslations(document.body);
  });
  assert.equal(
    await page.locator("#batch-queue-detail-title").innerText(),
    "批量任务队列",
  );
  await page.evaluate(() => {
    fixtureQueue.title = "多目标授权评估回归队列";
  });
  require("node:fs").writeFileSync(
    `${out}/queue-contrast-${width}-${theme}.json`,
    JSON.stringify(contrastResults, null, 2),
  );
  // A real language change translates the prefix and action, but never the queue name.
  await page.evaluate(async () => {
    fixtureQueue.status = "paused";
    await showBatchQueueDetail("ui-fixture");
    onLanguageSelect("en-US");
  });
  await page.waitForFunction(
    () =>
      document.documentElement.lang === "en-US" &&
      document.getElementById("batch-queue-start-btn").textContent === "Resume",
  );
  assert(
    (await page.locator("#batch-queue-detail-title").innerText()).endsWith(
      " - 多目标授权评估回归队列",
    ),
  );
  await page.evaluate(() => onLanguageSelect("zh-CN"));
  await page.waitForFunction(
    () =>
      document.documentElement.lang === "zh-CN" &&
      document.getElementById("batch-queue-start-btn").textContent ===
        "继续执行",
  );
  assert.equal(
    await page.locator("#batch-queue-detail-title").innerText(),
    "批量任务队列 - 多目标授权评估回归队列",
  );
  await page.evaluate(() => closeBatchQueueDetailModal());
  await activate("page-roles-management");
  await page.evaluate(() => editRole("界面回归角色"));
  await page.locator("#role-tools-list input").first().waitFor();
  await page.evaluate(() => applyTranslations(document));
  assert.equal(await page.locator("#role-modal-title").innerText(), "编辑角色");
  const stats = await page.locator("#role-tools-stats").evaluate((e) => ({
    width: e.clientWidth,
    scroll: e.scrollWidth,
    text: e.textContent,
  }));
  assert(stats.text.includes("614") && stats.text.includes("626"), stats.text);
  assert(stats.scroll <= stats.width + 2, JSON.stringify(stats));
  await page.locator("#role-tools-stats").scrollIntoViewIfNeeded();
  await shot("role-tools");
  await page.evaluate(() => closeRoleModal());
  await page.evaluate(() => showAddRoleModal());
  await page.evaluate(() => applyTranslations(document));
  assert.equal(await page.locator("#role-modal-title").innerText(), "添加角色");
  await page.evaluate(() => closeRoleModal());
  await activate("page-skills-management");
  // View is dynamically created and must use the same square drawer as Edit.
  await page.evaluate(() => viewSkill("review-skill"));
  await page.locator("#skill-view-modal").waitFor();
  await page.waitForTimeout(220);
  const skillView = await page.locator("#skill-view-modal").evaluate((e) => {
    const p = e.querySelector(".modal-content"),
      r = p.getBoundingClientRect(),
      s = getComputedStyle(p);
    return {
      surface: e.dataset.uiSurface,
      width: r.width,
      right: r.right,
      height: r.height,
      radius: s.borderRadius,
      bg: s.backgroundColor,
      footer: e.querySelector(".modal-footer").getBoundingClientRect().bottom,
    };
  });
  assert.equal(skillView.surface, "drawer");
  assert.equal(skillView.radius, "0px");
  assert(
    Math.abs(skillView.right - width) < 2 &&
      Math.abs(skillView.height - height) < 2,
    JSON.stringify(skillView),
  );
  assert(
    Math.abs(
      skillView.width - (width < 768 ? width : Math.min(960, width - 48)),
    ) < 2,
    JSON.stringify(skillView),
  );
  assert(skillView.footer <= height + 1, JSON.stringify(skillView));
  assert.equal(
    skillView.bg,
    theme === "dark" ? "rgb(17, 24, 39)" : "rgb(255, 255, 255)",
  );
  assert.equal(
    await page.locator("#skill-view-body").innerText(),
    "# 技能摘要\n按需加载正文。",
  );
  await shot("skill-view-summary");
  await page.locator("#skill-view-modal [data-skill-load-full]").click();
  await page.waitForFunction(() =>
    document
      .getElementById("skill-view-body")
      .textContent.startsWith("# 审核步骤"),
  );
  if (width >= 768) {
    await page.locator("#skill-view-modal .app-drawer-expand").click();
    assert(
      (await page.locator("#skill-view-modal .modal-content").boundingBox())
        .width >= skillView.width,
    );
  }
  await page.keyboard.press("Escape");
  assert.equal(await page.locator("#skill-view-modal").count(), 0);
  assert.equal(
    await page.evaluate(() =>
      document.body.classList.contains("app-modal-open"),
    ),
    false,
  );
  await page.evaluate(() => viewSkill("review-skill"));
  await page.locator("#skill-view-modal [data-skill-view-edit]").click();
  assert.equal(await page.locator("#skill-view-modal").count(), 0);
  await page.locator("#skill-modal").waitFor();
  assert.equal(
    await page.locator("#skill-modal").getAttribute("data-ui-surface"),
    "drawer",
  );
  await page.locator("#skill-content").waitFor();
  await page.evaluate(() => applyTranslations(document));
  assert.equal(
    await page.locator("#skill-modal-title").innerText(),
    "编辑Skill",
  );
  await shot("skill");
  await page.evaluate(() => closeSkillModal());
  await page.evaluate(() => showAddSkillModal());
  await page.evaluate(() => applyTranslations(document));
  assert.equal(
    await page.locator("#skill-modal-title").innerText(),
    "添加Skill",
  );
  await page.evaluate(() => closeSkillModal());
  // Real breakpoint transition: mobile boot reapplies the translations.
  if (width === 1440) {
    await activate("page-roles-management");
    await page.evaluate(() => editRole("界面回归角色"));
    await page.setViewportSize({ width: 390, height: 844 });
    await page.waitForTimeout(100);
    assert.equal(
      await page.locator("#role-modal-title").innerText(),
      "编辑角色",
    );
    await page.evaluate(() => closeRoleModal());
    await page.setViewportSize({ width, height });
    await activate("page-skills-management");
    await page.evaluate(() => editSkill("review-skill"));
    await page.setViewportSize({ width: 390, height: 844 });
    await page.waitForTimeout(100);
    assert.equal(
      await page.locator("#skill-modal-title").innerText(),
      "编辑Skill",
    );
    await page.evaluate(() => closeSkillModal());
    await page.setViewportSize({ width, height });
  }
  await activate("page-vulnerabilities");
  await page.evaluate(() => {
    const rows = Array.from({ length: 50 }, (_, i) => ({
      id: "finding-" + i,
      title: "接口响应与权限检查发现：包含较长的组件版本和复现说明 " + i,
      description: "已验证的证据摘要。".repeat(10),
      severity: i % 3 ? "low" : "high",
      status: "open",
      target: "https://service-" + i + ".example.test",
      target_host: "service-" + i + ".example.test",
      target_domain: "example.test",
      createdAt: "2026-11-10T12:00:00Z",
    }));
    vulnerabilityPagination = {
      currentPage: 1,
      totalPages: 3,
      total: 110,
      pageSize: 50,
    };
    renderVulnerabilities(rows, { view: "host" });
    renderVulnerabilityPagination();
  });
  await page.locator(".vulnerability-record-row").first().waitFor();
  if (width < 768) {
    await page
      .locator(".vulnerability-record-row")
      .first()
      .scrollIntoViewIfNeeded();
    await page.locator("#page-vulnerabilities").evaluate((e) => {
      e.scrollTop +=
        e.querySelector(".vulnerability-record-row").getBoundingClientRect()
          .top - e.getBoundingClientRect().top;
    });
    const scroll = await page
      .locator("#page-vulnerabilities")
      .evaluate((e) => ({
        height: e.clientHeight,
        scrollTop: e.scrollTop,
        contentOverflow: getComputedStyle(e.querySelector(".page-content"))
          .overflowY,
        headerBottom: e.querySelector(".page-header").getBoundingClientRect()
          .bottom,
        top: e.getBoundingClientRect().top,
      }));
    assert(
      scroll.scrollTop > 0 && scroll.headerBottom <= scroll.top + 1,
      JSON.stringify(scroll),
    );
    assert(scroll.contentOverflow === "visible", JSON.stringify(scroll));
    assert(scroll.height >= height - 150, JSON.stringify(scroll));
  }
  await shot("findings");
};
