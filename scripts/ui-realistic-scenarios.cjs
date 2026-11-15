// Anonymized reproductions of the real Kali UI states: 365 tasks, 626 tools,
// 614 enabled tools, multi-file skill, 50 long findings. All writes are rejected.
const assert = require("node:assert/strict");
module.exports = async function ({ page, width, height, theme, out }) {
  for (const script of [
    "tasks",
    "skills",
    "deep-assessments",
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
    const assessment = {
      id: "assessment-fixture",
      name: "授权评估回归任务",
      status: "draft",
      revision: 1,
      steps: [],
      objective: "检查已有数据的布局",
      scope: "离线界面回归",
      updated_at: "2026-11-15T08:00:00Z",
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
            content: "# 审核步骤\n" + "核对已有证据。\n".repeat(40),
          },
        };
      else if (url.pathname === "/api/deep-assessments/options")
        data = {
          roles: [role.name],
          models: ["默认模型"],
          projects: [{ id: "ui-project", name: "回归项目" }],
        };
      else if (url.pathname === "/api/deep-assessments")
        data = { tasks: [assessment], has_more: false };
      else if (url.pathname === "/api/deep-assessments/assessment-fixture")
        data = { task: assessment };
      else if (url.pathname.endsWith("/graph"))
        data = { nodes: [], edges: [], total: 0, folded: 0, has_more: false };
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
  await activate("page-tasks");
  for (const state of ["paused", "running", "completed"]) {
    await page.evaluate(async (state) => {
      fixtureQueue.status = state;
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
    if (state === "paused") await shot("queue");
    await page.evaluate(() => closeBatchQueueDetailModal());
  }
  await activate("page-roles-management");
  await page.evaluate(() => editRole("界面回归角色"));
  await page.locator("#role-tools-list input").first().waitFor();
  await page.evaluate(() => applyTranslations(document));
  assert.equal(await page.locator("#role-modal-title").innerText(), "编辑角色");
  const stats = await page
    .locator("#role-tools-stats")
    .evaluate((e) => ({
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
  await page.evaluate(() => editSkill("review-skill"));
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
  await activate("page-deep-assessments");
  await page.evaluate(() => DeepAssessments.init());
  await page.locator('[data-da-action="create"]').click();
  await page.locator("#da-form-drawer[open]").waitFor();
  await page.waitForTimeout(220);
  const native = await page
    .locator("#da-form-drawer")
    .evaluate((e) => ({
      rect: e.getBoundingClientRect().toJSON(),
      bg: getComputedStyle(e).backgroundColor,
      expand: !!e.querySelector(".app-drawer-expand"),
    }));
  assert(native.expand);
  assert(Math.abs(native.rect.right - width) < 2, JSON.stringify(native));
  assert(Math.abs(native.rect.height - height) < 2);
  assert.equal(
    native.bg,
    theme === "dark" ? "rgb(17, 24, 39)" : "rgb(255, 255, 255)",
  );
  if (width >= 768) {
    const before = native.rect.width;
    await page.locator("#da-form-drawer .app-drawer-expand").click();
    assert(
      (await page.locator("#da-form-drawer").boundingBox()).width > before,
    );
    await page.locator("#da-form-drawer .app-drawer-expand").click();
  }
  await shot("assessment");
  await page.keyboard.press("Escape");
  assert.equal(
    await page.locator("#da-form-drawer").getAttribute("open"),
    null,
  );
  assert.equal(
    await page
      .locator("#da-form-drawer .app-drawer-expand")
      .getAttribute("aria-pressed"),
    "false",
  );
  await page.locator('[data-da-action="select"]').click();
  await page.locator("#da-title h3").waitFor();
  await page.locator('[data-da-action="tab"][data-tab="overview"]').click();
  assert.equal(
    await page.locator("#da-inspector").getAttribute("aria-labelledby"),
    "da-title",
  );
  assert.equal(
    await page.locator("#da-inspector .app-drawer-expand").count(),
    1,
  );
  await shot("assessment-detail");
  await page.keyboard.press("Escape");
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
