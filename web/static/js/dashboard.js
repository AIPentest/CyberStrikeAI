// Dashboard page: fetches running conversations, vulnerability statistics, batch tasks, tools and Skills statistics, and renders them.
//
// Engineering infrastructure:
//   - dashboardState centrally stores runtime state (in-flight controller / auto polling timer / last update time /
//     Alarm entries ignored by this session reasons）；
//   Each time refreshDashboard is entered, abort the previous controller, pass the signal to all apiFetch,
//     Avoid rapid consecutive clicks / automatic polling triggering race conditions;
//   - Automatic polling: startDashboardAutoRefresh() pulls once every 60 seconds; automatically pauses when navigating away / the tab is hidden,
//     When switching back again, immediately do one more refresh (based on lastUpdatedAt to avoid invalid requests);
//   - Staleness detection: updateLastUpdatedNow records the timestamp；checkDashboardStale checks every 30 seconds,
//     If not refreshed for more than 5 minutes, add the .is-stale class to the 'Last updated' badge (grayed out + shows ⚠️).

var DASHBOARD_POLL_INTERVAL_MS = 60 * 1000;
var DASHBOARD_STALE_THRESHOLD_MS = 5 * 60 * 1000;
var DASHBOARD_STALE_CHECK_INTERVAL_MS = 30 * 1000;
var DASHBOARD_SEVERITY_STATUS_FILTER_STORAGE_KEY = 'cyberstrike.dashboard.severityStatusFilter';
var DASHBOARD_SEVERITY_STATUS_FILTER_VALUES = ['', 'open', 'confirmed', 'fixed', 'ignored', 'false_positive'];

var dashboardState = {
    currentController: null,    // The AbortController of the currently in-progress fetch
    pollTimer: null,            // setInterval id for automatic polling
    staleTimer: null,           // setInterval id for expiry check
    lastUpdatedAt: 0,           // Timestamp of the last successful refresh (ms)
    dismissedAlertKey: null,    // Fingerprint of alert content dismissed with '×' by the user in the current session (same reasons no longer pop up)
    lastResources: null,        // Previous round key resource snapshot, used to determine whether it's the first time with data / smart CTA
    recentFeedTab: 'vulns',     // Recent Vulnerabilities / Recent Facts Tab
    accessTab: 'c2',            // Access Overview Tab: c2 | webshell
    severityStatusFilter: null,  // Severity distribution current status filter: '' | open | confirmed | fixed | false_positive | ignored
    lastProjectSummary: null,   // Most recent project dashboard summary (for redraw when switching tabs)
};

function dashboardProjectScopedUrl(url) {
    try {
        var pid = typeof getActiveProjectId === 'function' ? (getActiveProjectId() || '') : '';
        if (!pid) return url;
        return url + (url.indexOf('?') === -1 ? '?' : '&') + 'project_id=' + encodeURIComponent(pid);
    } catch (e) {
        return url;
    }
}

async function refreshDashboard() {
    const runningEl = document.getElementById('dashboard-running-tasks');
    const vulnTotalEl = document.getElementById('dashboard-vuln-total');
    const severityIds = ['critical', 'high', 'medium', 'low', 'info'];

    // severityTotalEl is also referenced in the subsequent rendering logic, so it must be declared outside the loading branch
    const severityTotalEl = document.getElementById('dashboard-severity-total');

    // Experience optimization: auto polling / when data already exists, no longer flash the UI into a 「…」 placeholder,
    // Directly pull new data in the background and smoothly replace it; only show the loading state on the first load.
    var isInitialLoad = !dashboardState.lastUpdatedAt;
    if (isInitialLoad) {
        if (runningEl) runningEl.textContent = '…';
        if (vulnTotalEl) vulnTotalEl.textContent = '…';
        severityIds.forEach(s => {
            const el = document.getElementById('dashboard-severity-' + s);
            if (el) el.textContent = '0';
            const pctEl = document.getElementById('dashboard-severity-' + s + '-pct');
            if (pctEl) pctEl.textContent = '0%';
        });
        if (severityTotalEl) severityTotalEl.textContent = '0';
        renderSeverityDonut({}, 0);
        renderVulnStatusPanel(null, 0);
        renderSeverityInsights(null, 0, null);
        setDashboardOverviewPlaceholder('…');
        setEl('dashboard-kpi-tools-calls', '…');
        setEl('dashboard-kpi-success-rate', '…');
        setEl('dashboard-kpi-token-usage', '…');
        setKpiSubText('dashboard-kpi-tasks-sub-text', '…');
        setKpiSubText('dashboard-kpi-vuln-sub-text', '…');
        setKpiSubText('dashboard-kpi-tools-sub-text', '…');
        setKpiSubText('dashboard-kpi-rate-sub-text', '…');
        setKpiSubText('dashboard-kpi-token-sub-text', '…');
        hideEl('dashboard-kpi-vuln-critical-badge');
        hideEl('dashboard-alert-banner');
        setRecentVulnsLoading();
        setRecentFactsLoading();
        ['tools', 'skills', 'knowledge', 'roles', 'agents'].forEach(function (k) {
            setEl('dashboard-resource-' + k, '…');
        });
        setEl('dashboard-webshell-connections', '…');
        setEl('dashboard-c2-listeners-running', '…');
        setEl('dashboard-c2-sessions-online', '…');
        setEl('dashboard-c2-tasks-pending', '…');
        var chartPlaceholder = document.getElementById('dashboard-tools-pie-placeholder');
        if (chartPlaceholder) { chartPlaceholder.style.removeProperty('display'); chartPlaceholder.textContent = (typeof window.t === 'function' ? window.t('common.loading') : 'Loading...'); }
        var barChartEl = document.getElementById('dashboard-tools-bar-chart');
        if (barChartEl) { barChartEl.style.display = 'none'; barChartEl.innerHTML = ''; }
    }

    if (typeof apiFetch === 'undefined') {
        if (runningEl) runningEl.textContent = '-';
        if (vulnTotalEl) vulnTotalEl.textContent = '-';
        setDashboardOverviewPlaceholder('-');
        setRecentVulnsError();
        return;
    }

    // Prevent race: abort the previous request still in progress, then create a new controller
    if (dashboardState.currentController) {
        try { dashboardState.currentController.abort(); } catch (_) { /* ignore */ }
    }
    var controller = (typeof AbortController !== 'undefined') ? new AbortController() : null;
    dashboardState.currentController = controller;
    var signal = controller ? controller.signal : undefined;

    // Unified wrapper: apiFetch + abort signal + both failure/cancellation return null (no errors thrown),
    // Let the upper layer use destructuring assignment to read all results in a flat manner, avoiding a single failure causing the entire Promise.all to reject
    var fetchJson = function (url) {
        return apiFetch(url, { signal: signal })
            .then(function (r) { return r && r.ok ? r.json() : null; })
            .catch(function () { return null; });
    };

    try {
        var selectedSeverityStatus = getDashboardSeverityStatusFilter();
        // /api/vulnerabilities/stats only provides the two independent dimensions by_severity and by_status,
        // Cannot get the cross-count of 「Critical × Pending」. Here we pull once for each of the four levels (limit=1, only getting the total),
        // Use the real "pending × each severity" counts to drive the weighted scores of the alert bar / KPI sub-label / risk overview card,
        // Avoid semantic conflicts like "risk level still shows as extremely high even after all fixes are applied".
        var openVulnQuery = function (sev) {
            return fetchJson('/api/vulnerabilities?severity=' + sev + '&status=open&limit=1');
        };
        const [
            tasksRes, vulnRes, batchRes, monitorRes, knowledgeRes, skillsRes,
            recentVulnsRes, rolesRes, agentsRes,
            openCriticalRes, openHighRes, openMediumRes, openLowRes, toolsConfigRes,
            hitlPendingRes, notificationsRes, externalMcpStatsRes,
            webshellRes,
            c2ListenersRes, c2SessionsRes, c2TasksRes,
            projectSummaryRes, severityFilteredStatsRes, tokenUsageRes
        ] = await Promise.all([
            fetchJson('/api/agent-loop/tasks'),
            fetchJson('/api/vulnerabilities/stats'),
            fetchJson('/api/batch-tasks?limit=500&page=1'),
            fetchJson('/api/monitor/stats?top=30'),
            fetchJson('/api/knowledge/stats'),
            fetchJson('/api/skills/stats'),
            fetchJson('/api/vulnerabilities?limit=10&page=1'),
            fetchJson('/api/roles'),
            fetchJson('/api/multi-agent/markdown-agents'),
            openVulnQuery('critical'),
            openVulnQuery('high'),
            // Medium/low severity "pending" count: used for the weighted risk score of the risk overview card, making it reflect "currently unprocessed risks"
            openVulnQuery('medium'),
            openVulnQuery('low'),
            // Pull the 「total configured count」 of MCP tools for the 「capability overview」 (distinct from monitor/stats' 「has call records»).
            // Only fetch the total field, page_size=1 to reduce data transfer; total already covers internal + external MCP + directly registered tools.
            fetchJson('/api/config/tools?page=1&page_size=1&include_external=false'),
            // HITL pending approval: used for the "requires immediate action" alert banner + recommended actions
            fetchJson('/api/hitl/pending'),
            // Notification summary: since=0 fetches the latest batch, limit controls the size; used for inline display of "recent events"
            fetchJson('/api/notifications/summary?since=0&limit=20&lang=' + encodeURIComponent((window.__locale || 'zh-CN'))),
            // External MCP health
            fetchJson('/api/external-mcp/stats'),
            // WebShell established connections (the foothold after the pentest lands, very critical for operational scenarios)
            fetchJson(dashboardProjectScopedUrl('/api/webshell/connections')),
            // C2 dashboard bar: listener / session / pending tasks (task interface includes pending_queued_count)
            fetchJson(dashboardProjectScopedUrl('/api/c2/listeners')),
            fetchJson(dashboardProjectScopedUrl('/api/c2/sessions?limit=500')),
            fetchJson(dashboardProjectScopedUrl('/api/c2/tasks?page=1&page_size=1')),
            fetchJson('/api/projects/dashboard-summary?fact_limit=10'),
            selectedSeverityStatus ? fetchJson('/api/vulnerabilities/stats?status=' + encodeURIComponent(selectedSeverityStatus)) : Promise.resolve(null),
            fetchJson(dashboardProjectScopedUrl('/api/usage/tokens?days=7&limit=20'))
        ]);

        // If the controller has been aborted during the await, it means a new refresh has been started, so discard this result
        if (signal && signal.aborted) return;

        // Running conversations: only counts Agent loop tasks; for the batch queue, see "Batch Task Queue" on the right
        let agentRunningCount = null;
        if (tasksRes && Array.isArray(tasksRes.tasks)) {
            agentRunningCount = tasksRes.tasks.length;
        }
        let batchRunningCount = 0;
        if (batchRes && Array.isArray(batchRes.queues)) {
            batchRes.queues.forEach(q => {
                const s = (q.status || '').toLowerCase();
                if (s === 'running') batchRunningCount++;
            });
        }
        const runningConversations = agentRunningCount !== null ? agentRunningCount : 0;
        if (runningEl) {
            runningEl.textContent = agentRunningCount !== null ? String(agentRunningCount) : '-';
        }
        // KPI subtitle: all idle / executing
        if (runningConversations === 0) {
            setKpiSubBadge('dashboard-kpi-tasks-sub-text', dt('dashboard.allIdle', null, 'System idle'), 'idle');
        } else {
            setKpiSubBadge('dashboard-kpi-tasks-sub-text', dt('dashboard.executingNow', null, 'Executing'), 'running');
        }

        // Parse the true count of the "pending" scope (a dedicated API for pulling this); if that API fails, fall back to by_severity
        const pickOpenCount = function (res, fallback) {
            if (res && typeof res.total === 'number') return res.total;
            return fallback;
        };

        let criticalCount = 0;
        let highCount = 0;
        let mediumCount = 0;
        let lowCount = 0;
        let openCriticalCount = 0;
        let openHighCount = 0;
        let openMediumCount = 0;
        let openLowCount = 0;
        if (vulnRes && typeof vulnRes.total === 'number') {
            if (vulnTotalEl) vulnTotalEl.textContent = String(vulnRes.total);
            const bySeverity = vulnRes.by_severity || {};
            const total = vulnRes.total || 0;
            const severityDisplayRes = selectedSeverityStatus && severityFilteredStatsRes ? severityFilteredStatsRes : vulnRes;
            const displayBySeverity = severityDisplayRes.by_severity || {};
            const displayTotal = typeof severityDisplayRes.total === 'number' ? severityDisplayRes.total : total;
            criticalCount = bySeverity.critical || 0;
            highCount = bySeverity.high || 0;
            mediumCount = bySeverity.medium || 0;
            lowCount = bySeverity.low || 0;
            // Prefer using the dedicated "pending" count; if the specialized endpoint fails, fall back to by_severity (better a false positive than a false negative)
            openCriticalCount = pickOpenCount(openCriticalRes, criticalCount);
            openHighCount = pickOpenCount(openHighRes, highCount);
            openMediumCount = pickOpenCount(openMediumRes, mediumCount);
            openLowCount = pickOpenCount(openLowRes, lowCount);
            renderDashboardSeveritySummary(displayBySeverity, displayTotal, severityIds);
            renderVulnStatusPanel(vulnRes.by_status || {}, total);
            renderSeverityInsights(
                { critical: openCriticalCount, high: openHighCount, medium: openMediumCount, low: openLowCount },
                openCriticalCount + openHighCount + openMediumCount + openLowCount,
                recentVulnsRes
            );

            // Vulnerability KPI subtitle: both the badge and copy use the "pending" wording
            const critBadge = document.getElementById('dashboard-kpi-vuln-critical-badge');
            const critCountEl = document.getElementById('dashboard-kpi-vuln-critical-count');
            if (critCountEl) critCountEl.textContent = String(openCriticalCount);
            if (critBadge) critBadge.hidden = openCriticalCount === 0;
            const subTextEl = document.getElementById('dashboard-kpi-vuln-sub-text');
            if (subTextEl) {
                if (total === 0) {
                    subTextEl.textContent = dt('dashboard.allClear', null, 'No new risks at present');
                } else if (openCriticalCount === 0 && openHighCount === 0) {
                    // All high severity issues have been resolved → give positive feedback
                    subTextEl.textContent = dt('dashboard.allHandled', null, 'All high-severity issues have been handled');
                } else if (openHighCount > 0) {
                    subTextEl.textContent = dt('dashboard.openHighCountLabel', { count: openHighCount }, 'Pending High Risk' + openHighCount);
                } else {
                    subTextEl.textContent = dt('dashboard.totalCount', { count: total }, 'together' + total + 'measure word');
                }
            }
        } else {
            if (vulnTotalEl) vulnTotalEl.textContent = '-';
            if (severityTotalEl) severityTotalEl.textContent = '-';
            severityIds.forEach(sev => {
                const pctEl = document.getElementById('dashboard-severity-' + sev + '-pct');
                if (pctEl) pctEl.textContent = '-';
            });
            renderSeverityDonut({}, 0);
            renderVulnStatusPanel(null, 0);
            renderSeverityInsights(null, 0, null);
            hideEl('dashboard-kpi-vuln-critical-badge');
            setKpiSubText('dashboard-kpi-vuln-sub-text', '-');
        }

        // Batch task queue: stats by status (optimized version; running matches the batchRunningCount above)
        if (batchRes && Array.isArray(batchRes.queues)) {
            const queues = batchRes.queues;
            let pending = 0, running = batchRunningCount, done = 0;
            queues.forEach(q => {
                const s = (q.status || '').toLowerCase();
                if (s === 'pending' || s === 'paused') pending++;
                else if (s === 'running') { /* already counted into batchRunningCount */ }
                else if (s === 'completed' || s === 'cancelled') done++;
            });
            const total = pending + running + done;
            setEl('dashboard-batch-pending', String(pending));
            setEl('dashboard-batch-running', String(running));
            setEl('dashboard-batch-done', String(done));
            setEl('dashboard-batch-total', total > 0 ? (typeof window.t === 'function' ? window.t('dashboard.totalCount', { count: total }) : `A total of \${total}`) : (typeof window.t === 'function' ? window.t('dashboard.noTasks') : 'No tasks available'));
            
            // Update progress bar
            if (total > 0) {
                const pendingPct = (pending / total * 100).toFixed(1);
                const runningPct = (running / total * 100).toFixed(1);
                const donePct = (done / total * 100).toFixed(1);
                updateProgressBar('dashboard-batch-progress-pending', pendingPct);
                updateProgressBar('dashboard-batch-progress-running', runningPct);
                updateProgressBar('dashboard-batch-progress-done', donePct);
            } else {
                updateProgressBar('dashboard-batch-progress-pending', '0');
                updateProgressBar('dashboard-batch-progress-running', '0');
                updateProgressBar('dashboard-batch-progress-done', '0');
            }
        } else {
            setEl('dashboard-batch-pending', '-');
            setEl('dashboard-batch-running', '-');
            setEl('dashboard-batch-done', '-');
            setEl('dashboard-batch-total', '-');
            updateProgressBar('dashboard-batch-progress-pending', '0');
            updateProgressBar('dashboard-batch-progress-running', '0');
            updateProgressBar('dashboard-batch-progress-done', '0');
        }

        // Tool call: monitor/stats is { summary, topTools }
        let toolsCount = 0, toolsTotalCalls = 0, toolsSuccessRate = -1, toolsFailedCount = 0;
        if (monitorRes && monitorRes.summary) {
            const s = monitorRes.summary;
            toolsCount = s.toolCount || 0;
            toolsTotalCalls = s.totalCalls || 0;
            toolsFailedCount = s.failedCalls || 0;
            const totalSuccess = s.successCalls || 0;
            const effectiveToolCalls = totalSuccess + toolsFailedCount;
            setEl('dashboard-kpi-tools-calls', formatNumber(toolsTotalCalls));
            setKpiSubText('dashboard-kpi-tools-sub-text',
                dt('dashboard.toolsCountLabel', { count: toolsCount }, toolsCount + 'a tool'));
            if (effectiveToolCalls > 0) {
                toolsSuccessRate = (totalSuccess / effectiveToolCalls) * 100;
                const rateStr = toolsSuccessRate.toFixed(1) + '%';
                setEl('dashboard-kpi-success-rate', rateStr);
                setKpiRateBadge('dashboard-kpi-rate-sub-text', toolsSuccessRate, toolsFailedCount);
            } else if (toolsTotalCalls > 0) {
                setEl('dashboard-kpi-success-rate', '-');
                setKpiSubText('dashboard-kpi-rate-sub-text', dt('dashboard.noCompletedYet', null, 'No valid completion yet'));
            } else {
                setEl('dashboard-kpi-success-rate', '-');
                setKpiSubText('dashboard-kpi-rate-sub-text', dt('dashboard.noCallYet', null, 'No calls yet'));
            }
            renderDashboardToolsBar(monitorRes.topTools);
        } else {
            setEl('dashboard-kpi-tools-calls', '-');
            setEl('dashboard-kpi-success-rate', '-');
            setKpiSubText('dashboard-kpi-tools-sub-text', '-');
            setKpiSubText('dashboard-kpi-rate-sub-text', '-');
            renderDashboardToolsBar(null);
        }

        renderDashboardTokenUsage(tokenUsageRes);

        // 「Capability overview → MCP tools」 uses the total configuration count (including tools that have never been called); falls back to monitor's names.length if the dedicated interface fails
        if (toolsConfigRes && typeof toolsConfigRes.total === 'number') {
            setEl('dashboard-resource-tools', formatNumber(toolsConfigRes.total));
        } else if (toolsCount > 0) {
            setEl('dashboard-resource-tools', formatNumber(toolsCount));
        } else {
            setEl('dashboard-resource-tools', '-');
        }

        // Knowledge: fills the "Knowledge" row in the capability overview
        if (knowledgeRes && typeof knowledgeRes === 'object') {
            if (knowledgeRes.enabled === false) {
                setEl('dashboard-resource-knowledge', dt('dashboard.notEnabled', null, 'Not enabled'));
            } else {
                const items = knowledgeRes.total_items ?? 0;
                setEl('dashboard-resource-knowledge', formatNumber(items));
            }
        } else {
            setEl('dashboard-resource-knowledge', '-');
        }

        // Skills：fill in the 「Skills」 row in the capability overview
        if (skillsRes && typeof skillsRes === 'object') {
            const totalSkills = skillsRes.total_skills ?? 0;
            setEl('dashboard-resource-skills', formatNumber(totalSkills));
        } else {
            setEl('dashboard-resource-skills', '-');
        }

        // Role / Agents
        if (rolesRes) {
            // /api/roles returns { roles: [...] } or the array itself
            const roles = Array.isArray(rolesRes) ? rolesRes : (rolesRes.roles || []);
            setEl('dashboard-resource-roles', formatNumber(Array.isArray(roles) ? roles.length : 0));
        } else {
            setEl('dashboard-resource-roles', '-');
        }
        if (agentsRes) {
            // /api/multi-agent/markdown-agents returns { agents: [...] }
            const agents = Array.isArray(agentsRes) ? agentsRes : (agentsRes.agents || []);
            setEl('dashboard-resource-agents', formatNumber(Array.isArray(agents) ? agents.length : 0));
        } else {
            setEl('dashboard-resource-agents', '-');
        }
        // Recent vulnerability list
        renderRecentVulns(recentVulnsRes);
        dashboardState.lastProjectSummary = projectSummaryRes;
        renderRecentFacts(projectSummaryRes);

        // External MCP health (also fetch the down count to feed the alert banner / recommended actions)
        var externalMcpDown = renderExternalMcpHealth(externalMcpStatsRes);

        // HITL pending approval count (fed to alert banner / recommended actions)
        var hitlPending = getHitlPendingCount(hitlPendingRes);

        // 「Recent events」 inline display (from notification summary, filter out types already covered elsewhere in the dashboard)
        renderRecentEvents(notificationsRes);

        // Access overview（C2 + WebShell）
        renderDashboardAccessOverview(c2ListenersRes, c2SessionsRes, c2TasksRes, webshellRes);

        // Key alert bar: merge and display all possible alert sources (vulnerabilities/HITL/failure rate/MCP health)
        renderDashboardAlertBanner({
            criticalCount: openCriticalCount,
            hitlPending: hitlPending,
            failedTools: toolsFailedCount,
            successRate: toolsSuccessRate,
            externalMcpDown: externalMcpDown
        });

        // Smart CTA: hide 「Start your security journey」 when data exists
        var batchTotalCount = (batchRes && Array.isArray(batchRes.queues)) ? batchRes.queues.length : 0;
        var toolsConfiguredCount = (toolsConfigRes && typeof toolsConfigRes.total === 'number')
            ? toolsConfigRes.total : 0;
        updateSmartCTA({
            totalRunning: runningConversations + batchRunningCount,
            totalVulns: (vulnRes && typeof vulnRes.total === 'number') ? vulnRes.total : 0,
            totalCalls: toolsTotalCalls,
            toolsConfigured: toolsConfiguredCount,
            batchTotal: batchTotalCount
        });

        // 「Recommended action」: intelligently generated based on the full current state
        renderRecommendedActions({
            openCriticalCount: openCriticalCount,
            hitlPending: hitlPending,
            externalMcpDown: externalMcpDown,
            successRate: toolsSuccessRate,
            failedTools: toolsFailedCount,
            toolsConfigured: toolsConfiguredCount,
            totalVulns: (vulnRes && typeof vulnRes.total === 'number') ? vulnRes.total : 0,
            totalRunning: runningConversations + batchRunningCount
        });

        // Update "last updated" time
        updateLastUpdatedNow();
    } catch (e) {
        // AbortError is expected (proactively cancelled by a newer refresh), not treated as an error
        if (e && (e.name === 'AbortError' || (signal && signal.aborted))) return;
        console.warn('Failed to fetch dashboard statistics', e);
        if (runningEl) runningEl.textContent = '-';
        if (vulnTotalEl) vulnTotalEl.textContent = '-';
        setDashboardOverviewPlaceholder('-');
        setEl('dashboard-kpi-success-rate', '-');
        setEl('dashboard-kpi-tools-calls', '-');
        setEl('dashboard-kpi-token-usage', '-');
        setKpiSubText('dashboard-kpi-token-sub-text', '-');
        setKpiSubText('dashboard-kpi-tasks-sub-text', '-');
        setKpiSubText('dashboard-kpi-vuln-sub-text', '-');
        setKpiSubText('dashboard-kpi-tools-sub-text', '-');
        setKpiSubText('dashboard-kpi-rate-sub-text', '-');
        setKpiSubText('dashboard-kpi-token-sub-text', '-');
        ['tools', 'skills', 'knowledge', 'roles', 'agents'].forEach(function (k) {
            setEl('dashboard-resource-' + k, '-');
        });
        var accessSecErr = document.getElementById('dashboard-section-access');
        if (accessSecErr) accessSecErr.hidden = true;
        setRecentVulnsError();
        setRecentFactsError();
        renderDashboardToolsBar(null);
        var ph = document.getElementById('dashboard-tools-pie-placeholder');
        if (ph) { ph.style.removeProperty('display'); ph.textContent = (typeof window.t === 'function' ? window.t('dashboard.noCallData') : 'No call data available'); }
    } finally {
        if (dashboardState.currentController === controller) {
            dashboardState.currentController = null;
        }
        // Once the first refreshDashboard completes (whether it succeeds or fails), automatic polling + expiration check starts;
        // Repeated calls are idempotent (internally checks whether the timer already exists).
        startDashboardAutoRefresh();
    }
}

/** Access overview: C2 / WebShell Tab switching; when C2 is disabled, only keep the WebShell Tab */
function renderDashboardAccessOverview(listenersRes, sessionsRes, tasksRes, webshellRes) {
    var section = document.getElementById('dashboard-section-access');
    if (!section) return;

    var c2ConfigOn = window.__c2Enabled !== false;
    var webshellList = null;
    if (Array.isArray(webshellRes)) webshellList = webshellRes;
    else if (webshellRes && Array.isArray(webshellRes.connections)) webshellList = webshellRes.connections;
    var wsApiOk = webshellRes !== null;
    var c2ApiOk = listenersRes !== null || sessionsRes !== null || tasksRes !== null;
    var showC2 = c2ConfigOn && c2ApiOk;
    var showWs = wsApiOk;

    section.dataset.c2Available = showC2 ? '1' : '0';
    section.dataset.webshellAvailable = showWs ? '1' : '0';

    if (!showC2 && !showWs) {
        section.hidden = true;
        return;
    }

    if (showC2) {
        var running = '-';
        if (listenersRes && Array.isArray(listenersRes.listeners)) {
            running = String(listenersRes.listeners.filter(function (l) {
                return (l && (l.status || '').toLowerCase() === 'running');
            }).length);
        } else if (listenersRes === null) {
            running = '-';
        } else {
            running = '0';
        }
        var online = '-';
        if (sessionsRes && Array.isArray(sessionsRes.sessions)) {
            online = String(sessionsRes.sessions.filter(function (s) {
                if (!s) return false;
                var st = (s.status || '').toLowerCase();
                return st === 'active' || st === 'sleeping';
            }).length);
        } else if (sessionsRes === null) {
            online = '-';
        } else {
            online = '0';
        }
        var pending = '-';
        if (tasksRes && typeof tasksRes.pending_queued_count === 'number') {
            pending = String(tasksRes.pending_queued_count);
        } else if (tasksRes === null) {
            pending = '-';
        } else {
            pending = '0';
        }
        setEl('dashboard-c2-listeners-running', running);
        setEl('dashboard-c2-sessions-online', online);
        setEl('dashboard-c2-tasks-pending', pending);
    }

    if (showWs) {
        var wsCount = webshellList ? webshellList.length : 0;
        setEl('dashboard-webshell-connections', formatNumber(wsCount));
        renderDashboardWebshellRecent(webshellList || []);
    }

    section.hidden = false;
    syncDashboardAccessTabs();
    if (typeof applyTranslations === 'function') {
        try { applyTranslations(section); } catch (_e) { /* ignore */ }
    }
}

/** C2 / WebShell Tab switching (style consistent with 「Recent Vulnerabilities / Recent Facts」) */
function switchDashboardAccessTab(tab) {
    tab = tab === 'webshell' ? 'webshell' : 'c2';
    dashboardState.accessTab = tab;
    applyDashboardAccessTabUI(tab);
}

function applyDashboardAccessTabUI(tab) {
    var tabC2 = document.getElementById('dashboard-access-tab-c2');
    var tabWs = document.getElementById('dashboard-access-tab-webshell');
    var panelC2 = document.getElementById('dashboard-access-panel-c2');
    var panelWs = document.getElementById('dashboard-access-panel-webshell');
    if (tabC2) {
        tabC2.classList.toggle('is-active', tab === 'c2');
        tabC2.setAttribute('aria-selected', tab === 'c2' ? 'true' : 'false');
    }
    if (tabWs) {
        tabWs.classList.toggle('is-active', tab === 'webshell');
        tabWs.setAttribute('aria-selected', tab === 'webshell' ? 'true' : 'false');
    }
    if (panelC2) panelC2.hidden = tab !== 'c2';
    if (panelWs) panelWs.hidden = tab !== 'webshell';
    updateDashboardAccessViewAll(tab);
}

function updateDashboardAccessViewAll(tab) {
    var link = document.getElementById('dashboard-access-view-all');
    if (!link) return;
    if (tab === 'webshell') {
        link.onclick = function () { try { switchPage('webshell'); } catch (_) {} };
        link.setAttribute('data-i18n', 'dashboard.webshellGoManage');
        link.textContent = dt('dashboard.webshellGoManage', null, 'Enter WebShell →');
    } else {
        link.onclick = function () { try { switchPage('c2-listeners'); } catch (_) {} };
        link.setAttribute('data-i18n', 'dashboard.c2GoManage');
        link.textContent = dt('dashboard.c2GoManage', null, 'Enter C2 →');
    }
}

/** Sync Tab visibility and default selected item based on available modules */
function syncDashboardAccessTabs() {
    var section = document.getElementById('dashboard-section-access');
    if (!section || section.hidden) return;

    var showC2 = section.dataset.c2Available === '1';
    var showWs = section.dataset.webshellAvailable === '1';
    var tabNav = document.getElementById('dashboard-access-tabs');
    var tabC2 = document.getElementById('dashboard-access-tab-c2');
    var tabWs = document.getElementById('dashboard-access-tab-webshell');

    if (tabC2) tabC2.hidden = !showC2;
    if (tabWs) tabWs.hidden = !showWs;
    if (tabNav) tabNav.hidden = false;

    var tab = dashboardState.accessTab;
    if (tab === 'c2' && !showC2) tab = 'webshell';
    if (tab === 'webshell' && !showWs) tab = 'c2';
    if (!showC2 && showWs) tab = 'webshell';
    if (showC2 && !showWs) tab = 'c2';
    dashboardState.accessTab = tab;
    applyDashboardAccessTabUI(tab);
}

/** WebShell access overview: latest 3 connection summaries */
function renderDashboardWebshellRecent(list) {
    var container = document.getElementById('dashboard-webshell-recent');
    if (!container) return;
    container.innerHTML = '';
    if (!list || list.length === 0) {
        container.hidden = true;
        return;
    }
    var sorted = list.slice().sort(function (a, b) {
        var ta = (a && a.createdAt) ? Date.parse(a.createdAt) : 0;
        var tb = (b && b.createdAt) ? Date.parse(b.createdAt) : 0;
        return tb - ta;
    });
    var recent = sorted.slice(0, 3);
    recent.forEach(function (conn) {
        if (!conn) return;
        var item = document.createElement('div');
        item.className = 'dashboard-webshell-recent-item';
        item.setAttribute('role', 'button');
        item.setAttribute('tabindex', '0');
        var label = (conn.remark || '').trim() || (conn.url || '').trim() || (conn.id || '');
        var typeTag = (conn.type || 'shell').toUpperCase();
        item.innerHTML =
            '<span class="dashboard-webshell-recent-type">' + esc(typeTag) + '</span>' +
            '<span class="dashboard-webshell-recent-label" title="' + esc(label) + '">' + esc(label) + '</span>';
        var openWs = function () {
            try { switchPage('webshell'); } catch (_) {}
        };
        item.addEventListener('click', openWs);
        item.addEventListener('keydown', function (e) {
            if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                openWs();
            }
        });
        container.appendChild(item);
    });
    container.hidden = false;
}

function setEl(id, text) {
    const el = document.getElementById(id);
    if (el) el.textContent = text;
}

function hideEl(id) {
    const el = document.getElementById(id);
    if (el) el.hidden = true;
}

function showEl(id) {
    const el = document.getElementById(id);
    if (el) el.hidden = false;
}

function setDashboardOverviewPlaceholder(text) {
    ['dashboard-batch-pending', 'dashboard-batch-running', 'dashboard-batch-done', 'dashboard-batch-total'].forEach(id => setEl(id, text));
    updateProgressBar('dashboard-batch-progress-pending', '0');
    updateProgressBar('dashboard-batch-progress-running', '0');
    updateProgressBar('dashboard-batch-progress-done', '0');
}

// Translation helper; falls back to the fallback string when not found.
// Named dt rather than t, to avoid overwriting the window.t exposed by i18n.js (a top-level function declaration with the same name will write to window)
function dt(key, opts, fallback) {
    if (typeof window.t === 'function') {
        const v = window.t(key, opts);
        if (v && v !== key) return v;
    }
    return fallback != null ? fallback : key;
}

// KPI card subtitle: plain text
function setKpiSubText(id, text) {
    const el = document.getElementById(id);
    if (!el) return;
    el.textContent = text;
    el.classList.remove('is-pending', 'is-running', 'is-idle', 'is-warning', 'is-success', 'is-danger');
}

// KPI card subtitle: with status color（pending / running / idle / warning / success / danger）
function setKpiSubBadge(id, text, kind) {
    const el = document.getElementById(id);
    if (!el) return;
    el.textContent = text;
    el.classList.remove('is-pending', 'is-running', 'is-idle', 'is-warning', 'is-success', 'is-danger');
    if (kind) el.classList.add('is-' + kind);
}

// Tool success rate badge coloring
function setKpiRateBadge(id, rate, failedCount) {
    const el = document.getElementById(id);
    if (!el) return;
    el.classList.remove('is-pending', 'is-running', 'is-idle', 'is-warning', 'is-success', 'is-danger');
    if (rate >= 95) {
        el.textContent = dt('dashboard.healthyStatus', null, 'runs smoothly');
        el.classList.add('is-success');
    } else if (rate >= 80) {
        el.textContent = dt('dashboard.normalStatus', null, 'basically normal') + (failedCount > 0 ? ' · ' + dt('dashboard.failedNCalls', { count: failedCount }, failedCount + 'Failure') : '');
        el.classList.add('is-warning');
    } else {
        el.textContent = dt('dashboard.degradedStatus', null, 'needs attention') + (failedCount > 0 ? ' · ' + dt('dashboard.failedNCalls', { count: failedCount }, failedCount + 'Failure') : '');
        el.classList.add('is-danger');
    }
}

function renderDashboardTokenUsage(res) {
    const summary = res && res.summary ? res.summary : null;
    if (!summary) {
        setEl('dashboard-kpi-token-usage', '-');
        setKpiSubText('dashboard-kpi-token-sub-text', '-');
        return;
    }
    const cacheRate = tokenUsageCacheRate(summary);
    const total = Number(summary.totalTokens || 0);
    if (!Number.isFinite(total) || total <= 0) {
        setEl('dashboard-kpi-token-usage', '0');
        setKpiSubText('dashboard-kpi-token-sub-text', dt('dashboard.noTokenUsageYet', null, 'No usage yet'));
        return;
    }
    setEl('dashboard-kpi-token-usage', formatTokenUsageCompact(total));
    setKpiSubText('dashboard-kpi-token-sub-text',
        dt('dashboard.tokenUsageSub', {
            rate: cacheRate
        }, 'cache hit' + cacheRate));
}

function formatTokenUsageCompact(num) {
    const n = Number(num || 0);
    if (!Number.isFinite(n) || n <= 0) return '0';
    if (n >= 1000000) {
        return (n / 1000000).toFixed(n >= 10000000 ? 0 : 1).replace(/\.0$/, '') + 'M';
    }
    if (n >= 1000) {
        return (n / 1000).toFixed(n >= 10000 ? 0 : 1).replace(/\.0$/, '') + 'K';
    }
    return String(Math.trunc(n));
}

// sessionStorage: alert bar 「×」 dismissal record + the most recently **actually displayed** reason fragment (excluding level),
// Used to avoid mistakenly reusing an earlier "only a subset" ignore when the problems go from many to few (e.g., after finishing the HITL review, only severe vulnerabilities remain).
var DASH_SESSION_ALERT_DISMISSED = 'dashboard.dismissedAlert';
var DASH_SESSION_ALERT_LAST_REASONS = 'dashboard.alertLastReasons';

function dashboardAlertReasonKeySetFromJoined(s) {
    if (!s || typeof s !== 'string') return new Set();
    return new Set(s.split(',').map(function (x) { return x.trim(); }).filter(Boolean));
}

/** Whether the current reason fragment is a proper subset of the last displayed fragment (used to clear stale ignores) */
function dashboardAlertCurrentIsStrictSubsetOfLastShown(currentReasonJoined, lastReasonJoined) {
    var cur = dashboardAlertReasonKeySetFromJoined(currentReasonJoined);
    var last = dashboardAlertReasonKeySetFromJoined(lastReasonJoined);
    if (cur.size === 0 || last.size === 0) return false;
    if (cur.size >= last.size) return false;
    var ok = true;
    cur.forEach(function (k) {
        if (!last.has(k)) ok = false;
    });
    return ok;
}

// Critical reminder bar: rendered or hidden based on severity.
//   - level: danger (red) > warning (orange) > info (blue), automatically takes the highest level based on reasons
//   - After the user clicks ×, store the current reasons fingerprint in sessionStorage; if the exact same content appears again within this session, it will be automatically skipped
//   - When the reasons collection changes (e.g., another category of problem is added), the fingerprint becomes invalid and the banner pops up again, avoiding the "dismiss once and never be reminded again" issue
//   - If the "more categories of problems" combination had been shown before and later only some of the problems disappear, then even if the fingerprint is the same as the earlier ignore, the ignore is cleared and the reminder continues (see dashboard.alertLastReasons)
function renderDashboardAlertBanner(stats) {
    const banner = document.getElementById('dashboard-alert-banner');
    const titleEl = document.getElementById('dashboard-alert-title');
    const descEl = document.getElementById('dashboard-alert-desc');
    const actsEl = document.getElementById('dashboard-alert-actions');
    if (!banner || !titleEl || !descEl || !actsEl) return;

    const reasons = [];
    // Compute the fingerprint using reasonKeys (excluding localized strings, so switching the language won't cause the user to see it again)
    const reasonKeys = [];
    let level = 'info'; // info | warning | danger

    if (stats.criticalCount > 0) {
        reasons.push(dt('dashboard.alertCriticalReason', { count: stats.criticalCount },
            'exist' + stats.criticalCount + 'critical vulnerabilities pending processing, immediate remediation recommended'));
        reasonKeys.push('crit:' + stats.criticalCount);
        level = 'danger';
    }
    if (stats.hitlPending > 0) {
        // HITL pending approval blocks the Agent flow, so it stands as a separate entry; does not affect level (unless it's already info, upgrade to warning)
        reasons.push(dt('dashboard.alertHitlReason', { count: stats.hitlPending },
            'have' + stats.hitlPending + 'human-machine collaboration request(s) pending approval, Agent is awaiting your decision'));
        reasonKeys.push('hitl:' + stats.hitlPending);
        if (level === 'info') level = 'warning';
    }
    if (stats.successRate >= 0 && stats.successRate < 80 && stats.failedTools > 0) {
        reasons.push(dt('dashboard.alertFailedReason', { count: stats.failedTools },
            'Tool call success rate is relatively low (' + stats.failedTools + 'failures), please check MCP monitoring'));
        reasonKeys.push('rate:' + Math.round(stats.successRate) + ':' + stats.failedTools);
        if (level === 'info') level = 'warning';
    }
    if (stats.externalMcpDown > 0) {
        // External MCP abnormal server count > 0: affects tool availability
        reasons.push(dt('dashboard.alertMcpDownReason', { count: stats.externalMcpDown },
            'External MCP servers have' + stats.externalMcpDown + 'not running, related tools unavailable'));
        reasonKeys.push('mcp:' + stats.externalMcpDown);
        if (level === 'info') level = 'warning';
    }

    if (reasons.length === 0) {
        banner.hidden = true;
        banner.classList.remove('is-warning', 'is-danger', 'is-info');
        dashboardState.dismissedAlertKey = null;
        try { sessionStorage.removeItem(DASH_SESSION_ALERT_LAST_REASONS); } catch (_) {}
        return;
    }

    var fingerprint = level + '|' + reasonKeys.join(',');
    var reasonPartJoined = reasonKeys.join(',');

    // Check whether the same content has been ignored in this session; if the current is only a proper subset of the "last shown combination", clear the ignore (best practice: still remind of remaining items after partial handling)
    var dismissed = null;
    try { dismissed = sessionStorage.getItem(DASH_SESSION_ALERT_DISMISSED); } catch (_) {}
    var lastShownReasons = '';
    try { lastShownReasons = sessionStorage.getItem(DASH_SESSION_ALERT_LAST_REASONS) || ''; } catch (_) {}

    if (dismissed === fingerprint && dashboardAlertCurrentIsStrictSubsetOfLastShown(reasonPartJoined, lastShownReasons)) {
        try {
            sessionStorage.removeItem(DASH_SESSION_ALERT_DISMISSED);
            dismissed = null;
        } catch (_) { /* ignore */ }
    }

    dashboardState.dismissedAlertKey = fingerprint;

    if (dismissed === fingerprint) {
        banner.hidden = true;
        return;
    }

    banner.hidden = false;
    banner.classList.remove('is-warning', 'is-danger', 'is-info');
    banner.classList.add('is-' + level);

    if (level === 'danger') {
        titleEl.textContent = dt('dashboard.alertDangerTitle', null, 'Requires immediate handling');
    } else if (level === 'warning') {
        titleEl.textContent = dt('dashboard.alertWarningTitle', null, 'needs attention');
    } else {
        titleEl.textContent = dt('dashboard.alertTitle', null, 'remind');
    }

    descEl.textContent = reasons.join(dt('dashboard.alertReasonSeparator', null, '；'));

    actsEl.innerHTML = '';
    if (stats.criticalCount > 0) {
        const btn = document.createElement('button');
        btn.className = 'dashboard-alert-btn';
        btn.textContent = dt('dashboard.viewVulns', null, 'View vulnerabilities');
        btn.onclick = function () { try { switchPage('vulnerabilities'); } catch (e) {} };
        actsEl.appendChild(btn);
    }
    if (stats.hitlPending > 0) {
        const btn = document.createElement('button');
        btn.className = 'dashboard-alert-btn dashboard-alert-btn-secondary';
        btn.textContent = dt('dashboard.viewHitl', null, 'Go to Approval');
        btn.onclick = function () { try { switchPage('hitl'); } catch (e) {} };
        actsEl.appendChild(btn);
    }
    if (stats.successRate >= 0 && stats.successRate < 80) {
        const btn = document.createElement('button');
        btn.className = 'dashboard-alert-btn dashboard-alert-btn-secondary';
        btn.textContent = dt('dashboard.viewMonitor', null, 'View monitoring');
        btn.onclick = function () { try { switchPage('mcp-monitor'); } catch (e) {} };
        actsEl.appendChild(btn);
    }
    if (stats.externalMcpDown > 0) {
        const btn = document.createElement('button');
        btn.className = 'dashboard-alert-btn dashboard-alert-btn-secondary';
        btn.textContent = dt('dashboard.viewMcpManagement', null, 'Manage MCP');
        btn.onclick = function () { try { switchPage('mcp-management'); } catch (e) {} };
        actsEl.appendChild(btn);
    }

    try { sessionStorage.setItem(DASH_SESSION_ALERT_LAST_REASONS, reasonPartJoined); } catch (_) {}
}

// External MCP health: parsed from /api/external-mcp/stats (backend fields are total/enabled/disabled/connected),
// Decide whether to display in line 6 of 「Capability Overview」, and return the count of 「enabled but not connected」 to the alert banner.
function renderExternalMcpHealth(stats) {
    var row = document.getElementById('dashboard-resource-external-mcp-row');
    var textEl = document.getElementById('dashboard-resource-external-mcp-text');
    var healthEl = document.getElementById('dashboard-resource-external-mcp-health');
    if (!row || !textEl) return 0;

    if (!stats || typeof stats !== 'object') {
        row.hidden = true;
        return 0;
    }
    var total = Number(stats.total ?? stats.Total ?? 0) || 0;
    var enabled = Number(stats.enabled ?? stats.Enabled ?? 0) || 0;
    // The backend uses connected to represent the connected count; compatible with the old field running
    var connected = Number(stats.connected ?? stats.Connected ??
        stats.running ?? stats.Running ?? 0) || 0;
    if (total === 0) {
        row.hidden = true;
        return 0;
    }
    // When no "enabled" external MCP is configured, don't display the health row and don't alert either (consistent with the MCP management page)
    if (enabled === 0) {
        row.hidden = true;
        return 0;
    }
    var down = Math.max(0, enabled - connected);
    row.hidden = false;
    textEl.textContent = formatNumber(connected) + ' / ' + formatNumber(enabled);
    if (healthEl) {
        healthEl.classList.remove('is-ok', 'is-warning', 'is-danger');
        if (down === 0) {
            healthEl.classList.add('is-ok');
            healthEl.textContent = dt('dashboard.mcpAllRunning', null, 'Run all');
        } else if (down < enabled) {
            healthEl.classList.add('is-warning');
            healthEl.textContent = dt('dashboard.mcpPartialDown', { count: down },
                down + 'not running');
        } else {
            healthEl.classList.add('is-danger');
            healthEl.textContent = dt('dashboard.mcpAllDown', null, 'All not running');
        }
        healthEl.hidden = false;
    }
    return down;
}

// HITL pending approval count: returns the number of pending items; can also be used in the capability overview or KPI subtitle
function getHitlPendingCount(res) {
    if (!res) return 0;
    if (Array.isArray(res.items)) return res.items.length;
    if (typeof res.total === 'number') return res.total;
    if (Array.isArray(res)) return res.length;
    return 0;
}

// 「Recent Events」 inline display: take the top N most important items from the notification summary
// Design principles:
//   - Don't duplicate the "new vulnerability" notification already shown by the alert banner / KPI (vulnerability_created is still filtered)
//   - HITL pending approvals are also prompted in recommended actions and other places, but the timeline is still displayed here, making it convenient to view side by side with task completion and other items
//   - The entire section is hidden when there is no displayable content, to avoid empty modules taking up space
function renderRecentEvents(notifRes) {
    var section = document.getElementById('dashboard-section-events');
    var listEl = document.getElementById('dashboard-events-list');
    if (!section || !listEl) return;

    var items = (notifRes && Array.isArray(notifRes.items)) ? notifRes.items : [];
    // Filter: remove new vulnerability types (avoid duplication with sections like "Recent Vulnerabilities"); HITL no longer filters
    var coveredTypes = { 'vulnerability_created': true };
    var filtered = items.filter(function (it) {
        if (!it || !it.type) return false;
        if (coveredTypes[it.type]) return false;
        return true;
    });

    // Sort by level: p0 > p1 > p2, then by time in descending order
    var levelOrder = { p0: 0, p1: 1, p2: 2 };
    filtered.sort(function (a, b) {
        var la = levelOrder[a.level] != null ? levelOrder[a.level] : 9;
        var lb = levelOrder[b.level] != null ? levelOrder[b.level] : 9;
        if (la !== lb) return la - lb;
        var ta = a.ts || a.createdAt || a.created_at || 0;
        var tb = b.ts || b.createdAt || b.created_at || 0;
        return new Date(tb).getTime() - new Date(ta).getTime();
    });

    var top = filtered.slice(0, 3);
    if (top.length === 0) {
        section.hidden = true;
        listEl.innerHTML = '';
        return;
    }
    section.hidden = false;

    listEl.innerHTML = top.map(function (it) {
        var level = it.level || 'p2';
        var title = esc(it.title || it.message || dt('dashboard.eventUntitled', null, 'event'));
        var msg = esc(it.message || it.summary || it.desc || '');
        var whenRaw = timeAgoStr(it.ts || it.createdAt || it.created_at);
        var when = esc(whenRaw || '—');
        return (
            '<div class="dashboard-event-item lvl-' + esc(level) + '">' +
            '<span class="dashboard-event-dot" aria-hidden="true"></span>' +
            '<div class="dashboard-event-body">' +
            '<div class="dashboard-event-title">' + title + '</div>' +
            (msg && msg !== title ? '<div class="dashboard-event-msg">' + msg + '</div>' : '') +
            '</div>' +
            '<span class="dashboard-event-time">' + when + '</span>' +
            '</div>'
        );
    }).join('');
}

// Recommended actions: Intelligently generate "what to do next" based on the current data state.
// Design principle: each entry must be clickable to go directly to the corresponding page, sorted by priority (urgent > maintenance > configuration),
// Only show the 3-5 most important items at any given time; hide the entire section when there's nothing to recommend.
function renderRecommendedActions(state) {
    var section = document.getElementById('dashboard-section-recommend');
    var listEl = document.getElementById('dashboard-recommend-list');
    if (!section || !listEl) return;

    var actions = [];

    // Urgent class: unhandled critical vulnerabilities
    if (state.openCriticalCount > 0) {
        actions.push({
            level: 'urgent',
            icon: '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><circle cx="12" cy="17" r="1" fill="currentColor" stroke="none"/></svg>',
            title: dt('dashboard.recoFixCritical', { count: state.openCriticalCount },
                'Fix' + state.openCriticalCount + 'pending critical vulnerabilities'),
            desc: dt('dashboard.recoFixCriticalDesc', null, 'Critical-severity vulnerabilities should be addressed as a priority'),
            page: 'vulnerabilities'
        });
    }
    // Urgent class: HITL pending approval
    if (state.hitlPending > 0) {
        actions.push({
            level: 'urgent',
            icon: '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 11l3 3L22 4"/><path d="M21 12v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11"/></svg>',
            title: dt('dashboard.recoApproveHitl', { count: state.hitlPending },
                'approval' + state.hitlPending + 'HITL request'),
            desc: dt('dashboard.recoApproveHitlDesc', null, 'Agent is waiting for your decision to continue'),
            page: 'hitl'
        });
    }
    // Maintenance category: External MCP exception
    if (state.externalMcpDown > 0) {
        actions.push({
            level: 'warning',
            icon: '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg>',
            title: dt('dashboard.recoRestartMcp', { count: state.externalMcpDown },
                'check' + state.externalMcpDown + 'a non-running External MCP'),
            desc: dt('dashboard.recoRestartMcpDesc', null, 'Related tools are unavailable until the MCP service recovers.'),
            page: 'mcp-management'
        });
    }
    // Maintenance class: high failure rate
    if (state.successRate >= 0 && state.successRate < 80 && state.failedTools > 0) {
        actions.push({
            level: 'warning',
            icon: '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>',
            title: dt('dashboard.recoCheckMonitor', { count: state.failedTools },
                'troubleshoot' + state.failedTools + 'tool call failed'),
            desc: dt('dashboard.recoCheckMonitorDesc', null, 'View failed request details in MCP monitoring'),
            page: 'mcp-monitor'
        });
    }
    // Configuration class: first run scenario
    if (state.toolsConfigured === 0) {
        actions.push({
            level: 'setup',
            icon: '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/></svg>',
            title: dt('dashboard.recoSetupMcp', null, 'Configure your first MCP tool'),
            desc: dt('dashboard.recoSetupMcpDesc', null, 'Only after installing the MCP service can the Agent invoke specific capabilities.'),
            page: 'mcp-management'
        });
    }
    if (state.totalVulns === 0 && state.totalRunning === 0 && state.toolsConfigured > 0) {
        actions.push({
            level: 'setup',
            icon: '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>',
            title: dt('dashboard.recoStartScan', null, 'Initiate scan in conversation'),
            desc: dt('dashboard.recoStartScanDesc', null, 'Describe the goal in the conversation, and let AI assist with execution'),
            page: 'chat'
        });
    }

    if (actions.length === 0) {
        section.hidden = true;
        listEl.innerHTML = '';
        return;
    }
    section.hidden = false;
    listEl.innerHTML = actions.slice(0, 5).map(function (a) {
        return (
            '<a class="dashboard-recommend-item lvl-' + a.level + '" data-page="' + esc(a.page) + '" role="button" tabindex="0">' +
            '<span class="dashboard-recommend-icon" aria-hidden="true">' + a.icon + '</span>' +
            '<div class="dashboard-recommend-body">' +
            '<div class="dashboard-recommend-title">' + esc(a.title) + '</div>' +
            '<div class="dashboard-recommend-desc">' + esc(a.desc) + '</div>' +
            '</div>' +
            '<span class="dashboard-recommend-arrow" aria-hidden="true">→</span>' +
            '</a>'
        );
    }).join('');

    // Delegate click/keyboard to recommended item → switchPage
    Array.from(listEl.querySelectorAll('.dashboard-recommend-item')).forEach(function (el) {
        var page = el.getAttribute('data-page');
        el.onclick = function () { try { switchPage(page); } catch (_) {} };
        el.onkeydown = function (e) {
            if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); el.click(); }
        };
    });
}

// Smart CTA: if the user already has any data (task runs / vulnerabilities / tool calls / has configured MCP), then take
// Hide the "Start your security journey" CTA, only keep it as onboarding in a truly blank fresh environment
function updateSmartCTA(state) {
    var cta = document.getElementById('dashboard-cta-block');
    if (!cta) return;
    var hasData = (
        (state.totalRunning || 0) > 0 ||
        (state.totalVulns || 0) > 0 ||
        (state.totalCalls || 0) > 0 ||
        (state.toolsConfigured || 0) > 0 ||
        (state.batchTotal || 0) > 0
    );
    cta.hidden = hasData;
}

// 「Last updated」 time display; also record lastUpdatedAt for use in the stale check, and clear the stale state
function updateLastUpdatedNow() {
    dashboardState.lastUpdatedAt = Date.now();
    const el = document.getElementById('dashboard-last-updated-time');
    if (!el) return;
    const d = new Date();
    const pad = function (n) { return n < 10 ? '0' + n : String(n); };
    el.textContent = pad(d.getHours()) + ':' + pad(d.getMinutes()) + ':' + pad(d.getSeconds());
    const wrap = document.getElementById('dashboard-last-updated');
    if (wrap) {
        wrap.classList.remove('is-stale');
        wrap.classList.remove('is-flash');
        // trigger reflow then add class for the flash animation
        void wrap.offsetWidth;
        wrap.classList.add('is-flash');
    }
    const stale = document.getElementById('dashboard-last-updated-stale');
    if (stale) stale.hidden = true;
}

// Data staleness check: not refreshed for more than DASHBOARD_STALE_THRESHOLD_MS, add the .is-stale class to the badge,
// Display a ⚠️ icon to prompt the user: "This data may be stale, please refresh manually or check the network"
function checkDashboardStale() {
    if (!dashboardState.lastUpdatedAt) return;
    var ageMs = Date.now() - dashboardState.lastUpdatedAt;
    var wrap = document.getElementById('dashboard-last-updated');
    var stale = document.getElementById('dashboard-last-updated-stale');
    if (!wrap) return;
    if (ageMs > DASHBOARD_STALE_THRESHOLD_MS) {
        wrap.classList.add('is-stale');
        if (stale) stale.hidden = false;
    } else {
        wrap.classList.remove('is-stale');
        if (stale) stale.hidden = true;
    }
}

// Automatic polling: silently refresh once every 60 seconds when the dashboard is active + the tab is visible.
// When switched away / the tab is hidden, setInterval is still running, but inside tick it checks and skips the actual refresh;
// When becoming visible again, use lastUpdatedAt to determine whether an immediate extra refresh is needed (refresh if elapsed >= half of the interval).
function startDashboardAutoRefresh() {
    if (dashboardState.pollTimer) return;
    dashboardState.pollTimer = setInterval(function () {
        try {
            var page = document.getElementById('page-dashboard');
            if (!page || !page.classList.contains('active')) return;
            if (typeof document !== 'undefined' && document.hidden) return;
            refreshDashboard();
        } catch (e) {
            console.warn('auto refresh tick failed', e);
        }
    }, DASHBOARD_POLL_INTERVAL_MS);

    if (!dashboardState.staleTimer) {
        dashboardState.staleTimer = setInterval(checkDashboardStale, DASHBOARD_STALE_CHECK_INTERVAL_MS);
    }
}

function stopDashboardAutoRefresh() {
    if (dashboardState.pollTimer) {
        clearInterval(dashboardState.pollTimer);
        dashboardState.pollTimer = null;
    }
    if (dashboardState.staleTimer) {
        clearInterval(dashboardState.staleTimer);
        dashboardState.staleTimer = null;
    }
}

// Severity color scheme and Chinese labels
var SEVERITY_LABELS_FALLBACK = {
    critical: 'serious', high: 'high risk', medium: 'medium risk', low: 'low risk', info: 'information'
};

function severityShortLabel(id) {
    const key = 'dashboard.severity' + id.charAt(0).toUpperCase() + id.slice(1);
    return t(key, null, SEVERITY_LABELS_FALLBACK[id] || id);
}

// friendly relative time: "5 minutes ago" / "2 hours ago" / "yesterday" / "3 days ago"
function timeAgoStr(iso) {
    if (!iso) return '';
    const d = new Date(iso);
    if (isNaN(d.getTime())) return '';
    const diffSec = Math.max(0, Math.floor((Date.now() - d.getTime()) / 1000));
    if (diffSec < 60) return dt('common.justNow', null, 'just now');
    const min = Math.floor(diffSec / 60);
    if (min < 60) return dt('common.minutesAgo', { n: min }, min + 'minutes ago');
    const hr = Math.floor(min / 60);
    if (hr < 24) return dt('common.hoursAgo', { n: hr }, hr + 'hours ago');
    const day = Math.floor(hr / 24);
    if (day < 7) return dt('common.daysAgo', { n: day }, day + 'days ago');
    // If more than a week, display the date
    return d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0');
}

// Recent vulnerability list
function setRecentVulnsLoading() {
    const wrap = document.getElementById('dashboard-recent-vulns');
    const empty = document.getElementById('dashboard-recent-vulns-empty');
    if (!wrap) return;
    Array.from(wrap.querySelectorAll('.dashboard-recent-vuln-item')).forEach(function (n) { n.remove(); });
    if (empty) {
        empty.hidden = false;
        empty.classList.remove('is-rich');
        empty.textContent = dt('common.loading', null, 'Loading...');
    }
}

function setRecentVulnsError() {
    const wrap = document.getElementById('dashboard-recent-vulns');
    const empty = document.getElementById('dashboard-recent-vulns-empty');
    if (!wrap) return;
    Array.from(wrap.querySelectorAll('.dashboard-recent-vuln-item')).forEach(function (n) { n.remove(); });
    if (empty) {
        empty.hidden = false;
        empty.classList.remove('is-rich');
        empty.textContent = dt('common.loadFailed', null, 'Load failed');
    }
}

function renderRecentVulns(res) {
    const wrap = document.getElementById('dashboard-recent-vulns');
    const empty = document.getElementById('dashboard-recent-vulns-empty');
    if (!wrap) return;

    Array.from(wrap.querySelectorAll('.dashboard-recent-vuln-item')).forEach(function (n) { n.remove(); });

    const list = res && Array.isArray(res.vulnerabilities) ? res.vulnerabilities : [];
    if (list.length === 0) {
        if (empty) {
            empty.hidden = false;
            // Enhanced empty state: title + description + action button, easier to guide users to the next step than plain text
            empty.classList.add('is-rich');
            empty.innerHTML = (
                '<div class="dashboard-empty-title">' + esc(dt('dashboard.noVulnYet', null, 'No recent vulnerabilities')) + '</div>' +
                '<div class="dashboard-empty-desc">' + esc(dt('dashboard.noVulnDesc', null, 'Recent vulnerability records are shown here; new results will appear here after detection is completed in the conversation')) + '</div>' +
                '<button type="button" class="dashboard-empty-action" data-action="scan">' +
                esc(dt('dashboard.startScanBtn', null, 'Go to chat to initiate scan')) + ' →</button>'
            );
            var btn = empty.querySelector('[data-action="scan"]');
            if (btn) btn.onclick = function () { try { switchPage('chat'); } catch (_) {} };
        }
        return;
    }
    if (empty) {
        empty.hidden = true;
        empty.classList.remove('is-rich');
    }

    list.slice(0, 10).forEach(function (v) {
        const sev = (v.severity || 'info').toLowerCase();
        const status = (v.status || 'open').toLowerCase();
        const item = document.createElement('a');
        item.className = 'dashboard-recent-vuln-item';
        item.setAttribute('role', 'button');
        item.tabIndex = 0;
        item.onclick = function () { try { switchPage('vulnerabilities'); } catch (e) {} };
        item.onkeydown = function (e) { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); item.click(); } };

        const severityBadge = '<span class="dashboard-recent-vuln-sev sev-' + sev + '">' + esc(severityShortLabel(sev)) + '</span>';
        const title = '<span class="dashboard-recent-vuln-title" title="' + esc(v.title || '') + '">' + esc(v.title || dt('common.untitled', null, 'Untitled')) + '</span>';
        const target = v.target ? ('<span class="dashboard-recent-vuln-target" title="' + esc(v.target) + '">' + esc(v.target) + '</span>') : '<span class="dashboard-recent-vuln-target"></span>';
        const statusPill = '<span class="dashboard-recent-vuln-status st-' + esc(statusKey(status)) + '"><span class="dashboard-recent-vuln-status-dot"></span>' + esc(statusShortLabel(status)) + '</span>';
        const time = '<span class="dashboard-recent-vuln-time">' + esc(timeAgoStr(v.created_at)) + '</span>';

        item.innerHTML = severityBadge + title + target + statusPill + time;
        wrap.appendChild(item);
    });
}

// Recent Vulnerabilities / Recent Facts tab switching (shared list area, "View All" link changes with the tab)
function switchDashboardFeedTab(tab) {
    tab = tab === 'facts' ? 'facts' : 'vulns';
    dashboardState.recentFeedTab = tab;

    var tabVulns = document.getElementById('dashboard-feed-tab-vulns');
    var tabFacts = document.getElementById('dashboard-feed-tab-facts');
    var panelVulns = document.getElementById('dashboard-feed-panel-vulns');
    var panelFacts = document.getElementById('dashboard-feed-panel-facts');
    if (tabVulns) {
        tabVulns.classList.toggle('is-active', tab === 'vulns');
        tabVulns.setAttribute('aria-selected', tab === 'vulns' ? 'true' : 'false');
    }
    if (tabFacts) {
        tabFacts.classList.toggle('is-active', tab === 'facts');
        tabFacts.setAttribute('aria-selected', tab === 'facts' ? 'true' : 'false');
    }
    if (panelVulns) panelVulns.hidden = tab !== 'vulns';
    if (panelFacts) panelFacts.hidden = tab !== 'facts';
    updateDashboardFeedViewAll(tab);
}

function updateDashboardFeedViewAll(tab) {
    var link = document.getElementById('dashboard-feed-view-all');
    if (!link) return;
    if (tab === 'facts') {
        link.onclick = function () { try { switchPage('projects'); } catch (_) {} };
    } else {
        link.onclick = function () { try { switchPage('vulnerabilities'); } catch (_) {} };
    }
}

function setRecentFactsLoading() {
    var wrap = document.getElementById('dashboard-recent-facts');
    var empty = document.getElementById('dashboard-recent-facts-empty');
    if (!wrap) return;
    clearRecentFactsList(wrap);
    if (empty) {
        empty.hidden = false;
        empty.classList.remove('is-rich');
        empty.textContent = dt('common.loading', null, 'Loading...');
    }
}

function clearRecentFactsList(wrap) {
    if (!wrap) return;
    Array.from(wrap.querySelectorAll('.dashboard-recent-fact-item, .dashboard-recent-facts-meta')).forEach(function (n) { n.remove(); });
}

function setRecentFactsError() {
    var wrap = document.getElementById('dashboard-recent-facts');
    var empty = document.getElementById('dashboard-recent-facts-empty');
    if (!wrap) return;
    clearRecentFactsList(wrap);
    if (empty) {
        empty.hidden = false;
        empty.classList.remove('is-rich');
        empty.textContent = dt('common.loadFailed', null, 'Load failed');
    }
}

function factConfidenceShortLabel(confidence) {
    var c = String(confidence || '').toLowerCase();
    if (c === 'confirmed') return dt('projects.confidenceConfirmed', null, 'Confirmed');
    if (c === 'tentative') return dt('projects.confidenceTentative', null, 'To be confirmed');
    return c || '—';
}

function factCategoryShortLabel(category) {
    var raw = String(category || '').trim();
    return raw || 'note';
}

// Map 8 color schemes deterministically by project_id (falling back to project_name), so the same project keeps consistent colors across refreshes
function projectFactProjectTone(projectId, projectName) {
    var key = String(projectId || projectName || '').trim();
    if (!key) return 0;
    var hash = 0;
    for (var i = 0; i < key.length; i++) {
        hash = ((hash << 5) - hash) + key.charCodeAt(i);
        hash |= 0;
    }
    return Math.abs(hash) % 8;
}

function openProjectFactFromDashboard(projectId, factKey) {
    if (!projectId) return;
    if (typeof switchPage === 'function') {
        switchPage('projects');
    }
    setTimeout(async function () {
        if (typeof window.initProjectsPage === 'function') {
            await window.initProjectsPage();
        }
        if (typeof window.selectProject === 'function') {
            await window.selectProject(projectId);
        }
        if (typeof window.switchProjectTab === 'function') {
            window.switchProjectTab('facts');
        }
        if (factKey && typeof window.viewProjectFactBody === 'function') {
            window.viewProjectFactBody(factKey);
        }
    }, 350);
}

function renderRecentFacts(res) {
    var wrap = document.getElementById('dashboard-recent-facts');
    var empty = document.getElementById('dashboard-recent-facts-empty');
    if (!wrap) return;

    clearRecentFactsList(wrap);

    var list = (res && Array.isArray(res.recent_facts)) ? res.recent_facts : [];
    var totals = (res && res.totals) ? res.totals : {};
    var activeProjects = totals.active_projects || 0;
    var totalFacts = totals.total_facts || 0;

    if (list.length === 0) {
        if (empty) {
            empty.hidden = false;
            empty.classList.add('is-rich');
            var desc = activeProjects > 0
                ? dt('dashboard.noFactsDesc', null, 'In conversations within the bound project, the Agent automatically records facts such as targets, vulnerabilities, and attack chains')
                : dt('projects.selectOrCreateHint', null, 'The project is');
            var ctaLabel = activeProjects > 0
                ? dt('dashboard.goToChat', null, 'Go to conversation')
                : dt('dashboard.createFirstProjectBtn', null, 'Create your first project');
            var ctaAction = activeProjects > 0 ? 'chat' : 'project';
            empty.innerHTML = (
                '<div class="dashboard-empty-title">' + esc(dt('dashboard.noFactsYet', null, 'No recent facts available')) + '</div>' +
                '<div class="dashboard-empty-desc">' + esc(desc) + '</div>' +
                '<button type="button" class="dashboard-empty-action" data-action="' + esc(ctaAction) + '">' +
                esc(ctaLabel) + ' →</button>'
            );
            var btn = empty.querySelector('[data-action]');
            if (btn) {
                btn.onclick = function () {
                    var action = btn.getAttribute('data-action');
                    if (action === 'project') {
                        try { switchPage('projects'); } catch (_) {}
                        setTimeout(function () {
                            if (typeof window.showNewProjectModal === 'function') {
                                window.showNewProjectModal();
                            }
                        }, 350);
                    } else {
                        try { switchPage('chat'); } catch (_) {}
                    }
                };
            }
        }
        return;
    }

    if (empty) {
        empty.hidden = true;
        empty.classList.remove('is-rich');
    }

    list.slice(0, 10).forEach(function (f) {
        if (!f) return;
        var category = factCategoryShortLabel(f.category);
        var confidence = String(f.confidence || 'tentative').toLowerCase();
        var item = document.createElement('a');
        item.className = 'dashboard-recent-fact-item';
        item.setAttribute('role', 'button');
        item.tabIndex = 0;
        var pid = f.project_id || '';
        var fkey = f.fact_key || '';
        item.onclick = function () { openProjectFactFromDashboard(pid, fkey); };
        item.onkeydown = function (e) {
            if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                item.click();
            }
        };

        // Pinned column always occupies a slot, preventing subsequent columns from misaligning with/without pins
        var pinMark = '<span class="dashboard-recent-fact-pin' + (f.pinned ? ' is-pinned' : '') + '"' +
            (f.pinned ? (' title="' + esc(dt('projects.pinned', null, 'Pinned')) + '"') : '') +
            ' aria-hidden="true">' + (f.pinned ? '📌' : '') + '</span>';
        var projectLabel = (f.project_name || '').trim() || dt('projects.defaultProjectName', null, 'project');
        var factKeyLabel = (f.fact_key || '').trim() || '—';
        var projectTone = projectFactProjectTone(pid, projectLabel);
        var projectCol = '<span class="dashboard-recent-fact-project proj-tone-' + projectTone + '" title="' + esc(projectLabel) + '">' + esc(projectLabel) + '</span>';
        var categoryBadge = '<span class="dashboard-recent-fact-cat cat-' + esc(category.toLowerCase().replace(/[^a-z0-9_-]/g, '')) + '">' + esc(category) + '</span>';
        var confBadge = '<span class="dashboard-recent-fact-conf conf-' + esc(confidence) + '">' + esc(factConfidenceShortLabel(confidence)) + '</span>';
        var summary = '<span class="dashboard-recent-fact-summary" title="' + esc(f.summary || '') + '">' + esc(f.summary || dt('common.untitled', null, 'Untitled')) + '</span>';
        var factKeyCol = '<span class="dashboard-recent-fact-key" title="' + esc(factKeyLabel) + '">' + esc(factKeyLabel) + '</span>';
        var time = '<span class="dashboard-recent-fact-time">' + esc(timeAgoStr(f.updated_at)) + '</span>';

        item.innerHTML = pinMark + categoryBadge + confBadge + summary + factKeyCol + projectCol + time;
        wrap.appendChild(item);
    });
}

// Vulnerability status mapping: normalize the status string into 4 categories (avoid dirty data)
function statusKey(s) {
    s = String(s || '').toLowerCase();
    if (s === 'fixed' || s === 'closed' || s === 'resolved') return 'fixed';
    if (s === 'confirmed') return 'confirmed';
    if (s === 'false_positive' || s === 'false-positive' || s === 'fp') return 'fp';
    if (s === 'ignored') return 'ignored';
    return 'open';
}

function statusShortLabel(s) {
    const k = statusKey(s);
    if (k === 'fixed') return dt('dashboard.statusFixed', null, 'Fixed');
    if (k === 'confirmed') return dt('dashboard.statusConfirmed', null, 'Confirmed');
    if (k === 'fp') return dt('dashboard.statusFalsePositive', null, 'false report');
    if (k === 'ignored') return dt('dashboard.statusIgnored', null, 'Ignored');
    return dt('dashboard.statusOpen', null, 'Pending');
}

// Format number, add thousands separator
function formatNumber(num) {
    if (typeof num !== 'number' || isNaN(num)) return '-';
    if (num === 0) return '0';
    return num.toLocaleString('zh-CN');
}

// Update progress bar width
function updateProgressBar(id, percentage) {
    const el = document.getElementById(id);
    if (el) {
        const pct = parseFloat(percentage) || 0;
        el.style.width = Math.max(0, Math.min(100, pct)) + '%';
    }
}

// Top 30 tool execution count bar chart colors (30 non-repeating colors, soft and easy to distinguish)
var DASHBOARD_BAR_COLORS = [
    '#93c5fd', '#a78bfa', '#6ee7b7', '#fde047', '#fda4af',
    '#7dd3fc', '#a5b4fc', '#5eead4', '#fdba74', '#e9d5ff',
    '#67e8f9', '#c4b5fd', '#86efac', '#fcd34d', '#f9a8d4',
    '#bae6fd', '#c7d2fe', '#99f6e4', '#fed7aa', '#ddd6fe',
    '#22d3ee', '#8b5cf6', '#4ade80', '#fbbf24', '#fb7185',
    '#38bdf8', '#818cf8', '#2dd4bf', '#fb923c', '#e0e7ff'
];

function esc(s) {
    if (typeof s !== 'string') return '';
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/"/g, '&quot;');
}

// Vulnerability handling status + fix progress panel
// byStatus: { open, confirmed, fixed, false_positive, ignored } (any missing field is treated as 0)
// total: total number of vulnerabilities (from stats.total)
function renderVulnStatusPanel(byStatus, total) {
    var get = function (k) {
        if (!byStatus || typeof byStatus !== 'object') return 0;
        return Number(byStatus[k] || 0) || 0;
    };
    var open = get('open');
    var confirmed = get('confirmed');
    var fixed = get('fixed');
    var fp = get('false_positive');
    var ignored = get('ignored');

    setEl('dashboard-status-open', formatNumber(open));
    setEl('dashboard-status-confirmed', formatNumber(confirmed));
    setEl('dashboard-status-fixed', formatNumber(fixed));
    setEl('dashboard-status-fp', formatNumber(fp));
    setEl('dashboard-status-ignored', formatNumber(ignored));

    // The fix rate is calculated only for valid vulnerabilities that require remediation; false positives/ignored findings are neutral closures and do not lower the fix rate.
    var actionableTotal = open + confirmed + fixed;
    var rate = actionableTotal > 0 ? (fixed / actionableTotal) * 100 : 0;
    var rateStr = actionableTotal > 0 ? rate.toFixed(rate >= 100 ? 0 : 1) + '%' : '-';
    setEl('dashboard-fix-rate', rateStr);

    var detailEl = document.getElementById('dashboard-fix-detail');
    if (detailEl) {
        detailEl.textContent = '(' + formatNumber(fixed) + ' / ' + formatNumber(actionableTotal) + ')';
    }

    var fixedPct = actionableTotal > 0 ? (fixed / actionableTotal) * 100 : 0;
    var confirmedPct = actionableTotal > 0 ? (confirmed / actionableTotal) * 100 : 0;
    var fixedBar = document.getElementById('dashboard-fix-progress-fixed');
    var confirmedBar = document.getElementById('dashboard-fix-progress-confirmed');
    if (fixedBar) fixedBar.style.width = fixedPct.toFixed(2) + '%';
    if (confirmedBar) confirmedBar.style.width = confirmedPct.toFixed(2) + '%';
}

// Risk overview card: weighted risk score calculated from the severity distribution under the "open" (pending) scope + urgent badge
//
// Why use the open scope instead of the full volume:
//   If using the full set, after all vulnerabilities are fixed, by_severity remains unchanged, and the risk score still stays high,
//   But the urgent badge (pending critical / pending high) has already gone to zero —— visually this creates a 「critical + 0 pending」 semantic conflict.
//   After changing to the open scope, a fix immediately removes the risk, and the risk level and urgent count are fully synchronized.
//
// bySeverityOpen: { critical, high, medium, low } (only counts vulnerabilities with status=open; info is not counted)
// totalOpen:      total number of pending vulnerabilities (= critical + high + medium + low), only used for the "none pending → safe" judgment
// recentVulnsRes: /api/vulnerabilities?limit=10 response (used for the "recently found" time; the scope is the full dataset, independent of remediation status)
function renderSeverityInsights(bySeverityOpen, totalOpen, recentVulnsRes) {
    var riskBox = document.querySelector('.dashboard-severity-insight-risk');
    var levelEl = document.getElementById('dashboard-severity-risk-level');
    var fillEl = document.getElementById('dashboard-severity-risk-fill');
    var scoreEl = document.getElementById('dashboard-severity-risk-score');
    var urgentCriticalEl = document.getElementById('dashboard-severity-urgent-critical');
    var urgentHighEl = document.getElementById('dashboard-severity-urgent-high');
    var urgentCriticalCell = urgentCriticalEl ? urgentCriticalEl.closest('.dashboard-severity-insight-urgent-item') : null;
    var urgentHighCell = urgentHighEl ? urgentHighEl.closest('.dashboard-severity-insight-urgent-item') : null;
    var latestEl = document.getElementById('dashboard-severity-latest-time');

    var sev = bySeverityOpen && typeof bySeverityOpen === 'object' ? bySeverityOpen : {};
    var c = Number(sev.critical || 0) || 0;
    var h = Number(sev.high || 0) || 0;
    var m = Number(sev.medium || 0) || 0;
    var l = Number(sev.low || 0) || 0;

    // Weighted score: Critical ×10, High ×5, Medium ×2, Low ×0.5; Info ignored
    // Threshold design leans "conservative": just 1 pending critical enters "Medium", 2 enter "High", ≥4 enter "Critical"
    var score = c * 10 + h * 5 + m * 2 + l * 0.5;
    var level, levelKey, levelFallback;
    var t = Number(totalOpen || 0) || 0;
    if (t === 0 || score === 0) {
        level = 'safe'; levelKey = 'dashboard.riskSafe'; levelFallback = 'safe';
    } else if (score <= 3) {
        level = 'low'; levelKey = 'dashboard.riskLow'; levelFallback = 'low';
    } else if (score <= 10) {
        level = 'medium'; levelKey = 'dashboard.riskMedium'; levelFallback = 'middle';
    } else if (score <= 30) {
        level = 'high'; levelKey = 'dashboard.riskHigh'; levelFallback = 'high';
    } else {
        level = 'severe'; levelKey = 'dashboard.riskSevere'; levelFallback = 'extremely high';
    }

    if (riskBox) riskBox.setAttribute('data-level', level);
    if (levelEl) levelEl.textContent = dt(levelKey, null, levelFallback);
    // Progress bar uses 0-100 linear mapping: >=100 is directly full
    var pct = Math.max(0, Math.min(100, score));
    if (fillEl) fillEl.style.width = pct.toFixed(1) + '%';
    if (scoreEl) {
        // Score retains one decimal place (low severity 0.5 weight may produce non-integer values); integers are displayed directly
        var displayScore = Math.round(score) === score ? String(score) : score.toFixed(1);
        scoreEl.textContent = score >= 100 ? displayScore + '+' : displayScore;
    }

    // The urgent badge directly reuses the critical / high counts under the open criteria (exactly the same source as the weighted score, so the "extremely high risk + 0 pending" contradiction won't occur)
    if (urgentCriticalEl) urgentCriticalEl.textContent = formatNumber(c);
    if (urgentHighEl) urgentHighEl.textContent = formatNumber(h);
    if (urgentCriticalCell) urgentCriticalCell.classList.toggle('is-zero', c === 0);
    if (urgentHighCell) urgentHighCell.classList.toggle('is-zero', h === 0);

    if (latestEl) {
        var list = recentVulnsRes && Array.isArray(recentVulnsRes.vulnerabilities) ? recentVulnsRes.vulnerabilities : [];
        var latestIso = list.length > 0 ? list[0].created_at : null;
        var timeStr = latestIso ? timeAgoStr(latestIso) : '';
        if (timeStr) {
            latestEl.textContent = timeStr;
            latestEl.classList.remove('is-empty');
        } else {
            latestEl.textContent = dt('dashboard.noneYet', null, 'None yet');
            latestEl.classList.add('is-empty');
        }
    }
}

function renderDashboardToolsBar(topTools) {
    const placeholder = document.getElementById('dashboard-tools-pie-placeholder');
    const barChartEl = document.getElementById('dashboard-tools-bar-chart');
    if (!placeholder || !barChartEl) return;

    if (!Array.isArray(topTools) || topTools.length === 0) {
        placeholder.style.removeProperty('display');
        placeholder.textContent = (typeof window.t === 'function' ? window.t('dashboard.noCallData') : 'No call data available');
        barChartEl.style.display = 'none';
        barChartEl.innerHTML = '';
        return;
    }

    const entries = topTools.map(function (t) {
        return {
            name: t.toolName || '',
            totalCalls: typeof t.totalCalls === 'number' ? t.totalCalls : 0,
        };
    }).filter(function (e) { return e.name && e.totalCalls > 0; })
        .sort(function (a, b) { return b.totalCalls - a.totalCalls; })
        .slice(0, 30);

    if (entries.length === 0) {
        placeholder.style.removeProperty('display');
        placeholder.textContent = (typeof window.t === 'function' ? window.t('dashboard.noCallData') : 'No call data available');
        barChartEl.style.display = 'none';
        barChartEl.innerHTML = '';
        return;
    }

    placeholder.style.display = 'none';
    barChartEl.style.display = 'block';

    const maxCalls = Math.max.apply(null, entries.map(function (e) { return e.totalCalls; }));
    var html = '';
    entries.forEach(function (e, i) {
        var pct = maxCalls > 0 ? (e.totalCalls / maxCalls) * 100 : 0;
        var label = e.name.length > 12 ? e.name.slice(0, 10) + '…' : e.name;
        var color = DASHBOARD_BAR_COLORS[i % DASHBOARD_BAR_COLORS.length];
        var fullName = esc(e.name);
        html += '<div class="dashboard-tools-bar-item" data-tooltip="' + fullName + '">';
        html += '<span class="dashboard-tools-bar-label">' + esc(label) + '</span>';
        html += '<div class="dashboard-tools-bar-track"><div class="dashboard-tools-bar-fill" style="width:' + pct + '%;background:' + color + '"></div></div>';
        html += '<span class="dashboard-tools-bar-value">' + e.totalCalls + '</span>';
        html += '</div>';
    });
    barChartEl.innerHTML = html;
    attachDashboardBarTooltips(barChartEl);
}

var dashboardBarTooltipEl = null;
var dashboardBarTooltipTimer = null;

function attachDashboardBarTooltips(barChartEl) {
    if (!barChartEl) return;
    if (!dashboardBarTooltipEl) {
        dashboardBarTooltipEl = document.createElement('div');
        dashboardBarTooltipEl.className = 'dashboard-tools-bar-tooltip';
        dashboardBarTooltipEl.setAttribute('role', 'tooltip');
        document.body.appendChild(dashboardBarTooltipEl);
    }
    barChartEl.removeEventListener('mouseover', dashboardBarTooltipOnOver);
    barChartEl.removeEventListener('mouseout', dashboardBarTooltipOnOut);
    barChartEl.addEventListener('mouseover', dashboardBarTooltipOnOver);
    barChartEl.addEventListener('mouseout', dashboardBarTooltipOnOut);
}

function dashboardBarTooltipOnOver(ev) {
    var item = ev.target && ev.target.closest && ev.target.closest('.dashboard-tools-bar-item');
    if (!item || !dashboardBarTooltipEl) return;
    var text = item.getAttribute('data-tooltip');
    if (!text) return;
    clearTimeout(dashboardBarTooltipTimer);
    dashboardBarTooltipTimer = setTimeout(function () {
        dashboardBarTooltipEl.textContent = text;
        dashboardBarTooltipEl.style.display = 'block';
        requestAnimationFrame(function () {
            var rect = item.getBoundingClientRect();
            var ttRect = dashboardBarTooltipEl.getBoundingClientRect();
            var x = rect.left + (rect.width / 2) - (ttRect.width / 2);
            var y = rect.top - ttRect.height - 6;
            if (y < 8) y = rect.bottom + 6;
            var pad = 8;
            if (x < pad) x = pad;
            if (x + ttRect.width > window.innerWidth - pad) x = window.innerWidth - ttRect.width - pad;
            dashboardBarTooltipEl.style.left = x + 'px';
            dashboardBarTooltipEl.style.top = y + 'px';
        });
    }, 180);
}

function dashboardBarTooltipOnOut(ev) {
    var item = ev.target && ev.target.closest && ev.target.closest('.dashboard-tools-bar-item');
    var related = ev.relatedTarget && ev.relatedTarget.closest && ev.relatedTarget.closest('.dashboard-tools-bar-item');
    if (item && item === related) return;
    clearTimeout(dashboardBarTooltipTimer);
    dashboardBarTooltipTimer = null;
    if (dashboardBarTooltipEl) dashboardBarTooltipEl.style.display = 'none';
}

// Dashboard → Vulnerability Management: jump with severity/status filters
function navigateToVulnerabilitiesWithFilter(opts) {
    opts = opts || {};
    var params = new URLSearchParams();
    if (opts.severity) params.set('severity', opts.severity);
    if (opts.status) params.set('status', opts.status);
    var qs = params.toString();
    window.location.hash = qs ? 'vulnerabilities?' + qs : 'vulnerabilities';
}
window.navigateToVulnerabilitiesWithFilter = navigateToVulnerabilitiesWithFilter;

function normalizeDashboardSeverityStatusFilter(status) {
    status = String(status || '');
    return DASHBOARD_SEVERITY_STATUS_FILTER_VALUES.indexOf(status) >= 0 ? status : '';
}

function readDashboardSeverityStatusFilterFromStorage() {
    try {
        return normalizeDashboardSeverityStatusFilter(window.localStorage.getItem(DASHBOARD_SEVERITY_STATUS_FILTER_STORAGE_KEY));
    } catch (_) {
        return '';
    }
}

function writeDashboardSeverityStatusFilterToStorage(status) {
    try {
        if (status) {
            window.localStorage.setItem(DASHBOARD_SEVERITY_STATUS_FILTER_STORAGE_KEY, status);
        } else {
            window.localStorage.removeItem(DASHBOARD_SEVERITY_STATUS_FILTER_STORAGE_KEY);
        }
    } catch (_) {
        // localStorage may be disabled by the browser's privacy settings, but the filtering itself can still take effect on the current page.
    }
}

function dashboardSeverityStatusFilterLabel(status) {
    status = normalizeDashboardSeverityStatusFilter(status);
    if (!status) return dt('dashboard.allStatuses', null, 'All states');
    return statusShortLabel(status);
}

function getDashboardSeverityStatusFilter() {
    if (dashboardState.severityStatusFilter === null) {
        dashboardState.severityStatusFilter = readDashboardSeverityStatusFilterFromStorage();
    }
    syncDashboardSeverityStatusFilterUI();
    return dashboardState.severityStatusFilter || '';
}

function updateDashboardSeverityStatusFilter(status) {
    dashboardState.severityStatusFilter = normalizeDashboardSeverityStatusFilter(status);
    writeDashboardSeverityStatusFilterToStorage(dashboardState.severityStatusFilter);
    syncDashboardSeverityStatusFilterUI();
    refreshDashboard();
}
window.updateDashboardSeverityStatusFilter = updateDashboardSeverityStatusFilter;

function selectDashboardSeverityStatusFilter(status, ev) {
    if (ev) {
        ev.preventDefault();
        ev.stopPropagation();
    }
    closeDashboardSeverityStatusFilterMenu();
    updateDashboardSeverityStatusFilter(status);
}
window.selectDashboardSeverityStatusFilter = selectDashboardSeverityStatusFilter;

function syncDashboardSeverityStatusFilterUI() {
    var status = dashboardState.severityStatusFilter;
    if (status === null) status = readDashboardSeverityStatusFilterFromStorage();
    status = normalizeDashboardSeverityStatusFilter(status);

    var root = document.getElementById('dashboard-severity-status-filter');
    var textEl = document.getElementById('dashboard-severity-status-filter-text');
    if (root) root.setAttribute('data-value', status);
    if (textEl) textEl.textContent = dashboardSeverityStatusFilterLabel(status);

    document.querySelectorAll('.dashboard-severity-status-filter-option[data-status]').forEach(function (item) {
        var active = item.getAttribute('data-status') === status;
        item.classList.toggle('is-active', active);
        item.setAttribute('aria-selected', active ? 'true' : 'false');
    });
}

function toggleDashboardSeverityStatusFilterMenu(ev) {
    if (ev) {
        ev.preventDefault();
        ev.stopPropagation();
    }
    syncDashboardSeverityStatusFilterUI();
    var root = document.getElementById('dashboard-severity-status-filter');
    var btn = document.getElementById('dashboard-severity-status-filter-btn');
    var menu = document.getElementById('dashboard-severity-status-filter-menu');
    if (!root || !btn || !menu) return;
    var willOpen = menu.hasAttribute('hidden');
    menu.toggleAttribute('hidden', !willOpen);
    root.classList.toggle('is-open', willOpen);
    btn.setAttribute('aria-expanded', willOpen ? 'true' : 'false');
    ensureDashboardSeverityStatusFilterOutsideListener();
}
window.toggleDashboardSeverityStatusFilterMenu = toggleDashboardSeverityStatusFilterMenu;

function closeDashboardSeverityStatusFilterMenu() {
    var root = document.getElementById('dashboard-severity-status-filter');
    var btn = document.getElementById('dashboard-severity-status-filter-btn');
    var menu = document.getElementById('dashboard-severity-status-filter-menu');
    if (menu) menu.setAttribute('hidden', '');
    if (root) root.classList.remove('is-open');
    if (btn) btn.setAttribute('aria-expanded', 'false');
}

function ensureDashboardSeverityStatusFilterOutsideListener() {
    if (dashboardState.severityStatusFilterOutsideBound) return;
    dashboardState.severityStatusFilterOutsideBound = true;
    document.addEventListener('click', function (ev) {
        var root = document.getElementById('dashboard-severity-status-filter');
        if (root && root.contains(ev.target)) return;
        closeDashboardSeverityStatusFilterMenu();
    });
    document.addEventListener('keydown', function (ev) {
        if (ev.key === 'Escape') closeDashboardSeverityStatusFilterMenu();
    });
}

function openDashboardSeverityVulnerabilities() {
    navigateToVulnerabilitiesWithFilter({ status: getDashboardSeverityStatusFilter() });
}
window.openDashboardSeverityVulnerabilities = openDashboardSeverityVulnerabilities;

function navigateToSeverityWithDashboardStatus(severity) {
    navigateToVulnerabilitiesWithFilter({
        severity: severity,
        status: getDashboardSeverityStatusFilter()
    });
}

function renderDashboardSeveritySummary(bySeverity, total, severityIds) {
    severityIds = Array.isArray(severityIds) && severityIds.length ? severityIds : ['critical', 'high', 'medium', 'low', 'info'];
    total = Number(total || 0);
    bySeverity = bySeverity && typeof bySeverity === 'object' ? bySeverity : {};
    severityIds.forEach(function (sev) {
        var count = Number(bySeverity[sev] || 0) || 0;
        var el = document.getElementById('dashboard-severity-' + sev);
        if (el) el.textContent = String(count);
        var pctEl = document.getElementById('dashboard-severity-' + sev + '-pct');
        if (pctEl) {
            var pct = total > 0 ? Math.round((count / total) * 100) : 0;
            pctEl.textContent = pct + '%';
        }
    });
    renderSeverityDonut(bySeverity, total);
}

// Vulnerability severity distribution: semi-ring (donut) rendering
// Geometric parameters are fixed, making it easy to work with an SVG container with viewBox 0 0 560 320
// Separation between segments is achieved by the gapRad geometric gap, without using an outline stroke, to avoid the white/black edge being too heavy under light/dark colors
var SEVERITY_DONUT_CFG = {
    // viewBox 0 0 480 260：overall stays compact, but the ring thickness returns to near the 「golden ratio」，
    // Let the arc band itself have visual weight, without taking up as much space as the earliest version did.
    // Principle: rInner / rOuter ≈ 0.70, ring thickness ≈ rOuter * 0.30.
    cx: 240,
    cy: 215,
    rOuter: 165,
    rInner: 115,    // ring thickness = 50 (between the original 90 and the previous version's 35, natural and textured)
    labelOffset: 14,
    gapRad: 0.022
};

// Three-stage gradient: [light highlight tone, mid-section saturated color, dark edge] —— create a layered effect like a 3D glazed surface
var SEVERITY_DONUT_GRADIENTS = {
    critical: ['#fecaca', '#f87171', '#dc2626'],
    high: ['#fed7aa', '#fb923c', '#ea580c'],
    medium: ['#fef08a', '#facc15', '#ca8a04'],
    low: ['#99f6e4', '#2dd4bf', '#0f766e'],
    info: ['#bfdbfe', '#60a5fa', '#2563eb']
};

var severityDonutCenterDisplayed = { total: null, hoverCount: null };

var severityDonutState = {
    bySeverity: {},
    total: 0,
    hoverId: null,
    bound: false
};

var severityDonutTooltipEl = null;
var severityDonutTooltipTimer = null;
var severityDonutHoverClearTimer = null;

var SEVERITY_DEFAULT_LABELS = {
    critical: 'serious',
    high: 'high risk',
    medium: 'medium risk',
    low: 'low risk',
    info: 'information'
};

function severityLabel(id) {
    var key = 'dashboard.severity' + id.charAt(0).toUpperCase() + id.slice(1);
    if (typeof window.t === 'function') {
        var v = window.t(key);
        if (v && v !== key) return v;
    }
    return SEVERITY_DEFAULT_LABELS[id] || id;
}

function isDashboardDarkTheme() {
    return document.documentElement.getAttribute('data-theme') === 'dark';
}

function ensureSeverityDonutThemeObserver() {
    if (severityDonutState.themeObserver) return;
    severityDonutState.themeObserver = new MutationObserver(function (mutations) {
        for (var i = 0; i < mutations.length; i++) {
            if (mutations[i].attributeName === 'data-theme') {
                renderSeverityDonut(severityDonutState.bySeverity, severityDonutState.total);
                break;
            }
        }
    });
    severityDonutState.themeObserver.observe(document.documentElement, {
        attributes: true,
        attributeFilter: ['data-theme']
    });
}

function ensureSeverityDonutDefs() {
    var defsEl = document.getElementById('dashboard-severity-donut-defs');
    if (!defsEl) return;
    var dark = isDashboardDarkTheme();
    var html = '';
    html += '<linearGradient id="donut-track-face" x1="0%" y1="0%" x2="0%" y2="100%">';
    if (dark) {
        html += '<stop offset="0%" stop-color="#334155"/>';
        html += '<stop offset="55%" stop-color="#1e293b"/>';
        html += '<stop offset="100%" stop-color="#172033"/>';
    } else {
        html += '<stop offset="0%" stop-color="#f8fafc"/>';
        html += '<stop offset="55%" stop-color="#e8eef5"/>';
        html += '<stop offset="100%" stop-color="#dce5ef"/>';
    }
    html += '</linearGradient>';
    html += '<radialGradient id="donut-track-vignette" cx="50%" cy="85%" r="75%" fx="50%" fy="85%">';
    if (dark) {
        html += '<stop offset="0%" stop-color="#0f172a" stop-opacity="0.55"/>';
        html += '<stop offset="70%" stop-color="#0f172a" stop-opacity="0"/>';
    } else {
        html += '<stop offset="0%" stop-color="#ffffff" stop-opacity="0.35"/>';
        html += '<stop offset="70%" stop-color="#ffffff" stop-opacity="0"/>';
    }
    html += '</radialGradient>';
    html += '<radialGradient id="donut-inner-gloss" cx="35%" cy="75%" r="55%">';
    if (dark) {
        html += '<stop offset="0%" stop-color="#94a3b8" stop-opacity="0.10"/>';
        html += '<stop offset="55%" stop-color="#94a3b8" stop-opacity="0.03"/>';
        html += '<stop offset="100%" stop-color="#94a3b8" stop-opacity="0"/>';
    } else {
        html += '<stop offset="0%" stop-color="#ffffff" stop-opacity="0.45"/>';
        html += '<stop offset="55%" stop-color="#ffffff" stop-opacity="0.08"/>';
        html += '<stop offset="100%" stop-color="#ffffff" stop-opacity="0"/>';
    }
    html += '</radialGradient>';
    html += '<filter id="donut-segment-soften" x="-18%" y="-18%" width="136%" height="136%" color-interpolation-filters="sRGB">';
    html += '<feGaussianBlur in="SourceAlpha" stdDeviation="0.8" result="blur"/>';
    html += '<feOffset dx="0" dy="1.5" in="blur" result="off"/>';
    html += '<feFlood flood-color="' + (dark ? '#000000' : '#0f172a') + '" flood-opacity="' + (dark ? '0.28' : '0.13') + '" result="flood"/>';
    html += '<feComposite in="flood" in2="off" operator="in" result="shadow"/>';
    html += '<feMerge><feMergeNode in="shadow"/><feMergeNode in="SourceGraphic"/></feMerge>';
    html += '</filter>';
    Object.keys(SEVERITY_DONUT_GRADIENTS).forEach(function (id) {
        var stops = SEVERITY_DONUT_GRADIENTS[id];
        html += '<linearGradient id="donut-grad-' + id + '" x1="18%" y1="12%" x2="88%" y2="94%">';
        html += '<stop offset="0%" stop-color="' + stops[0] + '"/>';
        html += '<stop offset="52%" stop-color="' + stops[1] + '"/>';
        html += '<stop offset="100%" stop-color="' + stops[2] + '"/>';
        html += '</linearGradient>';
    });
    defsEl.innerHTML = html;
}

function renderSeverityDonut(bySeverity, total) {
    var svgEl = document.getElementById('dashboard-severity-donut');
    var trackEl = document.getElementById('dashboard-severity-donut-track');
    var leadersEl = document.getElementById('dashboard-severity-donut-leaders');
    var segmentsEl = document.getElementById('dashboard-severity-donut-segments');
    var hitsEl = document.getElementById('dashboard-severity-donut-hits');
    var labelsEl = document.getElementById('dashboard-severity-donut-labels');
    if (!trackEl || !segmentsEl || !labelsEl) return;

    severityDonutState.bySeverity = bySeverity && typeof bySeverity === 'object' ? bySeverity : {};
    severityDonutState.total = total || 0;
    severityDonutState.hoverId = null;

    ensureSeverityDonutThemeObserver();

    var cfg = SEVERITY_DONUT_CFG;
    ensureSeverityDonutDefs();

    // Background trajectory (complete half-ring): double-layer fill creates a groove + highlight
    var trackPath = halfRingPath(cfg.cx, cfg.cy, cfg.rOuter, cfg.rInner);
    trackEl.innerHTML =
        '<path class="donut-track-shadow" d="' + trackPath + '"/>' +
        '<path class="donut-track" fill="url(#donut-track-face)" d="' + trackPath + '"/>' +
        '<path class="donut-track-vignette" fill="url(#donut-track-vignette)" d="' + trackPath + '"/>';

    var ids = ['critical', 'high', 'medium', 'low', 'info'];
    var severities = ids.map(function (id) {
        return { id: id, value: (bySeverity && typeof bySeverity[id] === 'number') ? bySeverity[id] : 0 };
    });
    var visible = severities.filter(function (s) { return s.value > 0; });

    if (svgEl) {
        svgEl.classList.remove('is-highlighting');
        svgEl.removeAttribute('data-hover-severity');
    }
    if (!total || total <= 0 || visible.length === 0) {
        segmentsEl.innerHTML = '';
        if (hitsEl) hitsEl.innerHTML = '';
        labelsEl.innerHTML = '';
        if (leadersEl) leadersEl.innerHTML = '';
        clearSeverityDonutLegendHighlight();
        resetSeverityDonutCenter(false);
        _clearSeverityDonutChartWrapHover();
        if (svgEl) svgEl.classList.remove('donut-ready');
        return;
    }

    resetSeverityDonutCenter(true);

    // Arc length is computed as value/total; if the sum of severities < total (some are ungraded), the right side will retain the blank background track
    var sumVisible = visible.reduce(function (s, seg) { return s + seg.value; }, 0);
    var coverage = sumVisible / total; // The ratio of the half-ring covered by the actual segment
    var visibleCount = visible.length;
    var totalGapRad = cfg.gapRad * Math.max(0, visibleCount - 1);
    // Total available arc of the half-ring = π * coverage (filled proportionally), then subtract the gaps between segments
    var arcsTotalRad = Math.max(0, Math.PI * coverage - totalGapRad);

    var segmentsHtml = '';
    var hitsHtml = '';
    var glossHtml = '';
    var labelsHtml = '';
    var leadersHtml = '';
    var cumRad = 0;

    visible.forEach(function (seg, i) {
        var arcFraction = seg.value / sumVisible;
        var segRad = arcsTotalRad * arcFraction;
        var angleStart = Math.PI - cumRad;
        var angleEnd = angleStart - segRad;

        var path = arcSegmentPath(cfg.cx, cfg.cy, cfg.rOuter, cfg.rInner, angleStart, angleEnd);
        var pctOfTotal = (seg.value / total) * 100;
        var pctRounded = Math.round(pctOfTotal);
        var name = esc(severityLabel(seg.id));
        var ariaLabel = name + ' ' + seg.value + ' (' + pctRounded + '%)';
        segmentsHtml += '<path class="donut-segment seg-' + seg.id + '" data-severity="' + seg.id + '" data-count="' + seg.value + '" data-pct="' + pctRounded + '" fill="url(#donut-grad-' + seg.id + ')" d="' + path + '"/>';
        hitsHtml += '<path class="donut-segment-hit seg-' + seg.id + '" data-severity="' + seg.id + '" fill="transparent" d="' + path + '" tabindex="0" role="button" aria-label="' + ariaLabel + '"/>';
        glossHtml += '<path class="donut-segment-gloss seg-' + seg.id + '" data-severity="' + seg.id + '" fill="url(#donut-inner-gloss)" d="' + arcSegmentPath(cfg.cx, cfg.cy, cfg.rOuter - 2, cfg.rInner + 6, angleStart, angleEnd) + '" pointer-events="none"/>';

        // Only display outer labels when proportion >= 5%, to avoid small segment labels overlapping each other
        if (pctOfTotal >= 5) {
            var midAngle = (angleStart + angleEnd) / 2;
            var labelR = cfg.rOuter + cfg.labelOffset + 6;
            var sinMid = Math.sin(midAngle);
            var cosMid = Math.cos(midAngle);
            var lx = cfg.cx + labelR * cosMid;
            var topLift = sinMid > 0.4 ? Math.round((sinMid - 0.3) * 10) : 0;
            var ly = cfg.cy - labelR * sinMid - topLift;

            var anchor = 'middle';
            if (cosMid < -0.15) anchor = 'end';
            else if (cosMid > 0.15) anchor = 'start';

            var pctText = pctRounded + '%';
            var arcR = cfg.rOuter + 4;
            var lineX1 = cfg.cx + arcR * cosMid;
            var lineY1 = cfg.cy - arcR * sinMid;
            var lineX2 = cfg.cx + (cfg.rOuter + cfg.labelOffset - 2) * cosMid;
            var lineY2 = cfg.cy - (cfg.rOuter + cfg.labelOffset - 2) * sinMid;
            leadersHtml += '<line class="donut-leader label-' + seg.id + '" data-severity="' + seg.id + '" pathLength="100" x1="' + lineX1.toFixed(1) + '" y1="' + lineY1.toFixed(1) + '" x2="' + lineX2.toFixed(1) + '" y2="' + lineY2.toFixed(1) + '"/>';

            labelsHtml += '<text class="donut-label-text label-' + seg.id + '" data-severity="' + seg.id + '" text-anchor="' + anchor + '" x="' + lx.toFixed(1) + '" y="' + ly.toFixed(1) + '">';
            labelsHtml += '<tspan x="' + lx.toFixed(1) + '" dy="0">' + seg.value + ' <tspan class="donut-label-pct">(' + pctText + ')</tspan></tspan>';
            labelsHtml += '<tspan class="donut-label-name" x="' + lx.toFixed(1) + '" dy="14">' + name + '</tspan>';
            labelsHtml += '</text>';
        }

        cumRad += segRad;
        if (i < visibleCount - 1) cumRad += cfg.gapRad;
    });

    if (leadersEl) leadersEl.innerHTML = leadersHtml;
    segmentsEl.innerHTML = segmentsHtml + glossHtml;
    if (hitsEl) hitsEl.innerHTML = hitsHtml;
    labelsEl.innerHTML = labelsHtml;
    if (svgEl) {
        svgEl.classList.remove('donut-ready');
        void svgEl.offsetWidth;
        requestAnimationFrame(function () {
            svgEl.classList.add('donut-ready');
        });
    }
    scheduleSeverityCenterCountUp(total);
    attachSeverityDonutInteractivity();
}

function scheduleSeverityCenterCountUp(targetTotal) {
    if (window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
        var totalEl = document.getElementById('dashboard-severity-total');
        if (totalEl) totalEl.textContent = String(targetTotal);
        severityDonutCenterDisplayed.total = targetTotal;
        return;
    }
    var totalEl = document.getElementById('dashboard-severity-total');
    if (!totalEl || severityDonutState.hoverId) return;
    var from = typeof severityDonutCenterDisplayed.total === 'number' ? severityDonutCenterDisplayed.total : 0;
    var to = targetTotal;
    if (from === to) {
        totalEl.textContent = String(to);
        severityDonutCenterDisplayed.total = to;
        return;
    }
    var start = null;
    var dur = Math.min(520, 180 + Math.abs(to - from) * 28);
    function tick(now) {
        if (!start) start = now;
        var t = Math.min(1, (now - start) / dur);
        var eased = 1 - Math.pow(1 - t, 3);
        var val = Math.round(from + (to - from) * eased);
        totalEl.textContent = String(val);
        if (t < 1) {
            requestAnimationFrame(tick);
        } else {
            totalEl.textContent = String(to);
            severityDonutCenterDisplayed.total = to;
        }
    }
    requestAnimationFrame(tick);
}

function resetSeverityDonutCenter(skipTotalSnapshot) {
    var totalEl = document.getElementById('dashboard-severity-total');
    var labelEl = document.getElementById('dashboard-severity-center-label');
    var centerEl = document.getElementById('dashboard-severity-center');
    var n = severityDonutState.total || 0;
    if (!skipTotalSnapshot && totalEl) totalEl.textContent = String(n);
    if (!skipTotalSnapshot) severityDonutCenterDisplayed.total = n;
    severityDonutCenterDisplayed.hoverCount = null;
    if (labelEl) {
        labelEl.textContent = (typeof window.t === 'function' ? window.t('dashboard.totalVulns') : 'Total number of vulnerabilities');
        labelEl.classList.remove('is-severity');
        labelEl.removeAttribute('data-severity');
    }
    if (centerEl) centerEl.classList.remove('is-hovering');
}

function setSeverityDonutHover(severityId) {
    var svgEl = document.getElementById('dashboard-severity-donut');
    var centerEl = document.getElementById('dashboard-severity-center');
    var totalEl = document.getElementById('dashboard-severity-total');
    var labelEl = document.getElementById('dashboard-severity-center-label');
    if (!severityId) {
        severityDonutState.hoverId = null;
        if (svgEl) {
            svgEl.classList.remove('is-highlighting');
            svgEl.removeAttribute('data-hover-severity');
        }
        clearSeverityDonutLegendHighlight();
        resetSeverityDonutCenter(false);
        _clearSeverityDonutChartWrapHover();
        return;
    }
    var count = (severityDonutState.bySeverity && severityDonutState.bySeverity[severityId]) || 0;
    severityDonutState.hoverId = severityId;
    if (svgEl) {
        svgEl.classList.add('is-highlighting');
        svgEl.setAttribute('data-hover-severity', severityId);
    }
    highlightSeverityDonutParts(severityId);
    highlightSeverityLegendItem(severityId);
    if (totalEl) {
        totalEl.textContent = String(count);
        severityDonutCenterDisplayed.hoverCount = count;
    }
    if (labelEl) {
        labelEl.textContent = severityLabel(severityId);
        labelEl.classList.add('is-severity');
        labelEl.setAttribute('data-severity', severityId);
    }
    if (centerEl) centerEl.classList.add('is-hovering');
    var chartWrap = document.querySelector('.dashboard-severity-chart');
    if (chartWrap) chartWrap.setAttribute('data-hover-severity', severityId);
}

function _clearSeverityDonutChartWrapHover() {
    var chartWrap = document.querySelector('.dashboard-severity-chart');
    if (chartWrap) chartWrap.removeAttribute('data-hover-severity');
}

function highlightSeverityDonutParts(severityId) {
    var svgEl = document.getElementById('dashboard-severity-donut');
    if (!svgEl) return;
    svgEl.querySelectorAll('.donut-segment[data-severity], .donut-segment-gloss[data-severity], .donut-leader[data-severity], .donut-label-text[data-severity]').forEach(function (el) {
        var match = el.getAttribute('data-severity') === severityId;
        el.classList.toggle('is-active', match);
        el.classList.toggle('is-dimmed', !match);
    });
}

function highlightSeverityLegendItem(severityId) {
    var legend = document.getElementById('dashboard-vuln-bars');
    if (!legend) return;
    legend.querySelectorAll('.dashboard-severity-legend-item').forEach(function (item) {
        var match = item.getAttribute('data-severity') === severityId;
        item.classList.toggle('is-active', match);
    });
}

function clearSeverityDonutLegendHighlight() {
    var legend = document.getElementById('dashboard-vuln-bars');
    if (legend) {
        legend.querySelectorAll('.dashboard-severity-legend-item.is-active').forEach(function (el) {
            el.classList.remove('is-active');
        });
    }
    var svgEl = document.getElementById('dashboard-severity-donut');
    if (svgEl) {
        svgEl.querySelectorAll('.is-active, .is-dimmed').forEach(function (el) {
            el.classList.remove('is-active', 'is-dimmed');
        });
    }
}

function severityDonutTooltipText(severityId) {
    var count = (severityDonutState.bySeverity && severityDonutState.bySeverity[severityId]) || 0;
    var pct = severityDonutState.total > 0 ? Math.round((count / severityDonutState.total) * 100) : 0;
    var hint = (typeof window.t === 'function' ? window.t('dashboard.severityClickHint') : 'Click to view');
    return severityLabel(severityId) + ' · ' + count + ' (' + pct + '%) — ' + hint;
}

function showSeverityDonutTooltip(ev, severityId) {
    if (!severityDonutTooltipEl) {
        severityDonutTooltipEl = document.createElement('div');
        severityDonutTooltipEl.className = 'dashboard-severity-donut-tooltip';
        severityDonutTooltipEl.setAttribute('role', 'tooltip');
        document.body.appendChild(severityDonutTooltipEl);
    }
    clearTimeout(severityDonutTooltipTimer);
    severityDonutTooltipTimer = setTimeout(function () {
        severityDonutTooltipEl.textContent = severityDonutTooltipText(severityId);
        severityDonutTooltipEl.style.display = 'block';
        requestAnimationFrame(function () {
            var x = ev.clientX;
            var y = ev.clientY;
            var ttRect = severityDonutTooltipEl.getBoundingClientRect();
            var left = x - ttRect.width / 2;
            var top = y - ttRect.height - 12;
            if (top < 8) top = y + 16;
            var pad = 8;
            if (left < pad) left = pad;
            if (left + ttRect.width > window.innerWidth - pad) left = window.innerWidth - ttRect.width - pad;
            severityDonutTooltipEl.style.left = left + 'px';
            severityDonutTooltipEl.style.top = top + 'px';
        });
    }, 120);
}

function hideSeverityDonutTooltip() {
    clearTimeout(severityDonutTooltipTimer);
    severityDonutTooltipTimer = null;
    if (severityDonutTooltipEl) severityDonutTooltipEl.style.display = 'none';
}

function attachSeverityDonutInteractivity() {
    var hitsEl = document.getElementById('dashboard-severity-donut-hits');
    var legend = document.getElementById('dashboard-vuln-bars');
    if (!hitsEl) return;

    if (!severityDonutState.bound) {
        severityDonutState.bound = true;
        hitsEl.addEventListener('mouseover', severityDonutPointerOver);
        hitsEl.addEventListener('mouseout', severityDonutPointerOut);
        hitsEl.addEventListener('click', severityDonutClick);
        hitsEl.addEventListener('keydown', severityDonutKeydown);
        if (legend) {
            legend.addEventListener('mouseover', severityLegendPointerOver);
            legend.addEventListener('mouseout', severityLegendPointerOut);
            legend.addEventListener('click', severityLegendClick);
            legend.addEventListener('keydown', severityLegendKeydown);
        }
    }

    legend && legend.querySelectorAll('.dashboard-severity-legend-item').forEach(function (item) {
        if (!item.getAttribute('data-severity')) return;
        var sev = item.getAttribute('data-severity');
        var count = (severityDonutState.bySeverity && severityDonutState.bySeverity[sev]) || 0;
        item.classList.toggle('is-zero', count === 0);
        item.setAttribute('aria-label', severityDonutTooltipText(sev));
    });
}

function severityDonutHitTarget(el) {
    return el && el.closest && el.closest('.donut-segment-hit');
}

function severityDonutCancelHoverClear() {
    clearTimeout(severityDonutHoverClearTimer);
    severityDonutHoverClearTimer = null;
}

function severityDonutScheduleHoverClear() {
    severityDonutCancelHoverClear();
    severityDonutHoverClearTimer = setTimeout(function () {
        severityDonutHoverClearTimer = null;
        setSeverityDonutHover(null);
        hideSeverityDonutTooltip();
    }, 60);
}

function severityDonutPointerOver(ev) {
    var target = severityDonutHitTarget(ev.target);
    if (!target) return;
    var id = target.getAttribute('data-severity');
    if (!id) return;
    severityDonutCancelHoverClear();
    if (severityDonutState.hoverId === id) return;
    setSeverityDonutHover(id);
    showSeverityDonutTooltip(ev, id);
}

function severityDonutPointerOut(ev) {
    var related = ev.relatedTarget;
    if (related) {
        if (severityDonutHitTarget(related)) return;
        var legendItem = related.closest && related.closest('.dashboard-severity-legend-item[data-severity]');
        if (legendItem) return;
        var hitsRoot = document.getElementById('dashboard-severity-donut-hits');
        if (hitsRoot && hitsRoot.contains(related)) return;
    }
    severityDonutScheduleHoverClear();
}

function severityDonutClick(ev) {
    var target = severityDonutHitTarget(ev.target);
    if (!target) return;
    var id = target.getAttribute('data-severity');
    if (!id) return;
    ev.preventDefault();
    navigateToSeverityWithDashboardStatus(id);
}

function severityDonutKeydown(ev) {
    if (ev.key !== 'Enter' && ev.key !== ' ') return;
    var target = severityDonutHitTarget(ev.target);
    if (!target) return;
    ev.preventDefault();
    var id = target.getAttribute('data-severity');
    if (id) navigateToSeverityWithDashboardStatus(id);
}

function severityLegendPointerOver(ev) {
    var item = ev.target && ev.target.closest && ev.target.closest('.dashboard-severity-legend-item[data-severity]');
    if (!item) return;
    var id = item.getAttribute('data-severity');
    if (!id) return;
    severityDonutCancelHoverClear();
    setSeverityDonutHover(id);
    showSeverityDonutTooltip(ev, id);
}

function severityLegendPointerOut(ev) {
    var item = ev.target && ev.target.closest && ev.target.closest('.dashboard-severity-legend-item[data-severity]');
    var related = ev.relatedTarget && ev.relatedTarget.closest && ev.relatedTarget.closest('.dashboard-severity-legend-item[data-severity]');
    if (item && item === related) return;
    severityDonutScheduleHoverClear();
}

function severityLegendClick(ev) {
    var item = ev.target && ev.target.closest && ev.target.closest('.dashboard-severity-legend-item[data-severity]');
    if (!item) return;
    var id = item.getAttribute('data-severity');
    if (!id) return;
    ev.preventDefault();
    navigateToSeverityWithDashboardStatus(id);
}

function severityLegendKeydown(ev) {
    if (ev.key !== 'Enter' && ev.key !== ' ') return;
    var item = ev.target && ev.target.closest && ev.target.closest('.dashboard-severity-legend-item[data-severity]');
    if (!item) return;
    ev.preventDefault();
    var id = item.getAttribute('data-severity');
    if (id) navigateToSeverityWithDashboardStatus(id);
}

// SVG half-ring (background trajectory) path
function halfRingPath(cx, cy, rOuter, rInner) {
    var x1Outer = cx - rOuter;
    var y1Outer = cy;
    var x2Outer = cx + rOuter;
    var y2Outer = cy;
    var x1Inner = cx - rInner;
    var y1Inner = cy;
    var x2Inner = cx + rInner;
    var y2Inner = cy;
    return 'M ' + x1Outer + ' ' + y1Outer +
        ' A ' + rOuter + ' ' + rOuter + ' 0 0 1 ' + x2Outer + ' ' + y2Outer +
        ' L ' + x2Inner + ' ' + y2Inner +
        ' A ' + rInner + ' ' + rInner + ' 0 0 0 ' + x1Inner + ' ' + y1Inner + ' Z';
}

// Single-segment arc (angleStart > angleEnd, angle decreases counterclockwise, visually advancing clockwise along the top of the half-ring)
function arcSegmentPath(cx, cy, rOuter, rInner, angleStart, angleEnd) {
    var x1Outer = cx + rOuter * Math.cos(angleStart);
    var y1Outer = cy - rOuter * Math.sin(angleStart);
    var x2Outer = cx + rOuter * Math.cos(angleEnd);
    var y2Outer = cy - rOuter * Math.sin(angleEnd);
    var x1Inner = cx + rInner * Math.cos(angleStart);
    var y1Inner = cy - rInner * Math.sin(angleStart);
    var x2Inner = cx + rInner * Math.cos(angleEnd);
    var y2Inner = cy - rInner * Math.sin(angleEnd);

    var largeArc = (angleStart - angleEnd) > Math.PI ? 1 : 0;

    return 'M ' + x1Outer.toFixed(2) + ' ' + y1Outer.toFixed(2) +
        ' A ' + rOuter + ' ' + rOuter + ' 0 ' + largeArc + ' 1 ' + x2Outer.toFixed(2) + ' ' + y2Outer.toFixed(2) +
        ' L ' + x2Inner.toFixed(2) + ' ' + y2Inner.toFixed(2) +
        ' A ' + rInner + ' ' + rInner + ' 0 ' + largeArc + ' 0 ' + x1Inner.toFixed(2) + ' ' + y1Inner.toFixed(2) + ' Z';
}

// After the language switch, the parts of the dashboard that are dynamically rendered by JS (KPI sub-labels, alert bars, semi-ring chart labels,
// status card, recent vulnerability list, capability overview badges, etc.) will not be automatically re-rendered by applyTranslations,
// Need to proactively re-fetch the data and re-render in the new language; consistent with other pages such as tasks/vulnerability.
document.addEventListener('languagechange', function () {
    try {
        var dashboardPage = document.getElementById('page-dashboard');
        if (!dashboardPage || !dashboardPage.classList.contains('active')) {
            return;
        }
        if (typeof refreshDashboard === 'function') {
            refreshDashboard();
        }
    } catch (e) {
        console.warn('languagechange dashboard refresh failed', e);
    }
});

// Page visibility: when switching back from another tab, if half a polling period has already elapsed since the last refresh, immediately perform an extra refresh;
// Avoid the case where a tab left in the background for hours still shows stale data when you come back, without hitting the API every time you switch back to it.
document.addEventListener('visibilitychange', function () {
    if (document.hidden) return;
    var page = document.getElementById('page-dashboard');
    if (!page || !page.classList.contains('active')) return;
    var ageMs = Date.now() - (dashboardState.lastUpdatedAt || 0);
    if (ageMs >= DASHBOARD_POLL_INTERVAL_MS / 2) {
        try { refreshDashboard(); } catch (_) { /* ignore */ }
    } else {
        // No need to re-fetch data, but also run a stale check to update badge status
        checkDashboardStale();
    }
});

// Close alert bar button: store the current reasons fingerprint in sessionStorage so the same content won't pop up again in this session
document.addEventListener('click', function (ev) {
    var btn = ev.target && ev.target.closest && ev.target.closest('#dashboard-alert-close');
    if (!btn) return;
    ev.preventDefault();
    var key = dashboardState.dismissedAlertKey || '';
    try { sessionStorage.setItem(DASH_SESSION_ALERT_DISMISSED, key); } catch (_) {}
    var banner = document.getElementById('dashboard-alert-banner');
    if (banner) banner.hidden = true;
});
