// 信息收集页面（FOFA）
function _t(key, opts) {
    return typeof window.t === 'function' ? window.t(key, opts) : key;
}

// 翻译带回退中文（i18n 未就绪时仍显示原文，避免暴露内部键名）
function _tf(key, fallback, opts) {
    return typeof window.t === 'function' ? window.t(key, opts) : fallback;
}

const FOFA_FORM_STORAGE_KEY = 'info-collect-fofa-form';
const FOFA_HIDDEN_FIELDS_STORAGE_KEY = 'info-collect-fofa-hidden-fields';

const INFO_COLLECT_PROVIDERS = {
    fofa: {
        label: 'FOFA',
        get placeholder() {
            return _tf('infoCollect.queryPlaceholder', '例如：app="Apache" && country="CN"');
        },
        get nlPlaceholder() {
            return _tf('infoCollectPage.nlPlaceholder', '例如：找美国 Missouri 的 Apache 站点，标题包含 Home');
        },
        get hint() {
            return _tf('infoCollectPage.formHint', '查询语法参考 FOFA 文档，支持 && / || / () 等。');
        },
        get parseHint() {
            return _tf('infoCollectPage.parseHint', '解析后会弹窗展示 FOFA 语法（可编辑），确认无误后再填入查询框并执行查询。');
        },
        maxSize: 10000,
        get sizeHint() {
            return _tf('infoCollectPage.fofaSizeHint', 'FOFA 返回数量上限与账号权限相关，前端最多允许 10000。');
        },
        fullOption: {
            get label() {
                return _tf('infoCollectPage.fofaFullLabel', '完整模式');
            },
            get hint() {
                return _tf('infoCollectPage.fofaFullHint', '向 FOFA 传 full=true，返回更完整/更实时的数据，可能消耗更多额度。');
            }
        },
        fields: 'host,ip,port,domain,title,protocol,country,region,city,server',
        get presets() {
            return [
                [_tf('infoCollectPage.presetApache', 'Apache + 中国'), 'app="Apache" && country="CN"'],
                [_tf('infoCollectPage.presetLogin', '登录页 + 中国'), 'title="登录" && country="CN"'],
                [_tf('infoCollectPage.presetDomain', '指定域名'), 'domain="example.com"'],
                [_tf('infoCollectPage.presetIp', '指定 IP'), 'ip="1.1.1.1"']
            ];
        },
        get fieldPresets() {
            return [
                [_tf('infoCollectPage.minFields', '最小字段'), 'host,ip,port,domain'],
                [_tf('infoCollectPage.webCommon', 'Web 常用'), 'host,title,ip,port,domain,protocol,server,icp,country,region,city'],
                [_tf('infoCollectPage.intelEnhanced', '情报增强'), 'host,ip,port,domain,title,protocol,country,region,city,server,asn,org,icp,header,banner']
            ];
        },
        syntaxGuide: {
            get summary() {
                return _tf('infoCollectPage.fofaSyntaxSummary', 'FOFA 使用 field="value" 精确匹配，支持 &&、||、! 和括号组合；字符串建议用双引号包裹。');
            },
            docsUrl: 'https://en.fofa.info/api',
            get sections() {
                return [
                    [_tf('infoCollectPage.syntaxSectionCommonFields', '常用字段'), ['app="Apache"', 'title="后台管理"', 'body="Powered by"', 'domain="example.com"', 'host="https://example.com"', 'ip="1.1.1.1"', 'port="443"', 'country="CN"', 'city="Hangzhou"', 'server="nginx"']],
                    [_tf('infoCollectPage.syntaxSectionConbinations', '组合写法'), ['app="nginx" && country="CN"', 'title="login" || title="登录"', '(app="Apache" || app="nginx") && port="443"', 'domain="example.com" && !title="404"']],
                    [_tf('infoCollectPage.syntaxSectionExamples', '场景示例'), ['cert="example.com" && port="443"', 'header="JSESSIONID" && country="CN"', 'icon_hash="-247388890"', 'fid="sZyXkR9e" && domain="example.com"']]
                ];
            }
        }
    },
    zoomeye: {
        label: 'ZoomEye',
        get placeholder() {
            return _tf('infoCollect.queryPlaceholder', '例如：app="Apache" && country="CN"');
        },
        get nlPlaceholder() {
            return _tf('infoCollectPage.zoomeyeNlPlaceholder', '例如：找中国的 SSH 服务，排除蜜罐');
        },
        get hint() {
            return _tf('infoCollectPage.zoomeyeHint', 'ZoomEye 支持 app/title/domain/ip/port/country/city 等语法。');
        },
        get parseHint() {
            return _tf('infoCollectPage.zoomeyeParseHint', '解析后会弹窗展示 ZoomEye 语法（可编辑），确认无误后再填入查询框并执行查询。');
        },
        maxSize: 10000,
        get sizeHint() {
            return _tf('infoCollectPage.zoomeyeSizeHint', 'ZoomEye pagesize 最高支持到 10000，实际额度以账号为准。');
        },
        fullOption: null,
        fields: 'ip,port,domain,hostname,title,service,app,country,city',
        get presets() {
            return [
                [_tf('infoCollectPage.presetApache', 'Apache + 中国'), 'app="Apache" && country="CN"'],
                [_tf('infoCollectPage.presetSsh', 'SSH 服务'), 'service="ssh"'],
                [_tf('infoCollectPage.presetDomain', '指定域名'), 'domain="example.com"'],
                [_tf('infoCollectPage.presetIp', '指定 IP'), 'ip="1.1.1.1"']
            ];
        },
        get fieldPresets() {
            return [
                [_tf('infoCollectPage.minFields', '最小字段'), 'ip,port,domain,hostname'],
                [_tf('infoCollectPage.webCommon', 'Web 常用'), 'ip,port,domain,hostname,title,service,app,country,city'],
                [_tf('infoCollectPage.intelEnhanced', '情报增强'), 'ip,port,domain,hostname,title,service,app,country,city,org,isp,ssl']
            ];
        },
        syntaxGuide: {
            get summary() {
                return _tf('infoCollectPage.zoomeyeSyntaxSummary', 'ZoomEye 支持字段检索、引号短语、AND/OR/NOT 与括号组合；字段名以官方控制台实际支持为准。');
            },
            docsUrl: 'https://www.zoomeye.ai/help',
            get sections() {
                return [
                    [_tf('infoCollectPage.syntaxSectionCommonFields', '常用字段'), ['app="Apache"', 'service="ssh"', 'title="登录"', 'domain="example.com"', 'hostname="example.com"', 'ip="1.1.1.1"', 'port=443', 'country="CN"', 'city="Beijing"', 'org="Tencent"']],
                    [_tf('infoCollectPage.syntaxSectionConbinations', '组合写法'), ['app="nginx" AND country="CN"', 'service="http" AND (title="login" OR title="登录")', 'domain="example.com" AND NOT app="cloudflare"', 'port=443 AND country="US"']],
                    [_tf('infoCollectPage.syntaxSectionExamples', '场景示例'), ['ssl.cert.fingerprint="SHA256值"', 'iconhash="-247388890"', 'service="rdp" AND country="CN"', 'app="Elasticsearch" AND port=9200']]
                ];
            }
        }
    },
    quake: {
        label: 'Quake',
        get placeholder() {
            return _tf('infoCollectPage.quakeQueryPlaceholder', '例如：service.name:"http" AND country_cn:"中国"');
        },
        get nlPlaceholder() {
            return _tf('infoCollectPage.quakeNlPlaceholder', '例如：找中国的 HTTP 服务，标题包含登录');
        },
        get hint() {
            return _tf('infoCollectPage.quakeHint', 'Quake 使用 DSL 语法，常见字段如 service.name、domain、ip、port、country_cn。');
        },
        get parseHint() {
            return _tf('infoCollectPage.quakeParseHint', '解析后会弹窗展示 Quake DSL（可编辑），确认无误后再填入查询框并执行查询。');
        },
        maxSize: 10000,
        get sizeHint() {
            return _tf('infoCollectPage.quakeSizeHint', 'Quake size 会消耗积分，建议按需控制返回数量。');
        },
        fullOption: {
            get label() {
                return _tf('infoCollectPage.quakeFullLabel', '最新数据');
            },
            get hint() {
                return _tf('infoCollectPage.quakeFullHint', '向 Quake 传 latest=true，优先查询最新数据。');
            }
        },
        fields: 'ip,port,domain,service.name,service.http.title,location.country_cn,location.province_cn,location.city_cn',
        get presets() {
            return [
                [_tf('infoCollectPage.presetHttpChina', 'HTTP + 中国'), 'service.name:"http" AND country_cn:"中国"'],
                [_tf('infoCollectPage.presetPort443', '443 端口'), 'port:443'],
                [_tf('infoCollectPage.presetDomain', '指定域名'), 'domain:"example.com"'],
                [_tf('infoCollectPage.presetIp', '指定 IP'), 'ip:"1.1.1.1"']
            ];
        },
        get fieldPresets() {
            return [
                [_tf('infoCollectPage.minFields', '最小字段'), 'ip,port,domain'],
                [_tf('infoCollectPage.webCommon', 'Web 常用'), 'ip,port,domain,service.name,service.http.title,location.country_cn,location.city_cn'],
                [_tf('infoCollectPage.intelEnhanced', '情报增强'), 'ip,port,domain,service.name,service.http.title,service.http.server,location.country_cn,location.province_cn,location.city_cn,asn']
            ];
        },
        syntaxGuide: {
            get summary() {
                return _tf('infoCollectPage.quakeSyntaxSummary', 'Quake 使用 Lucene/DSL 风格查询，常见形式是 field:"value"，逻辑运算符通常使用 AND、OR、NOT。');
            },
            docsUrl: 'https://quake.360.net/quake/#/help',
            get sections() {
                return [
                    [_tf('infoCollectPage.syntaxSectionCommonFields', '常用字段'), ['service.name:"http"', 'service.http.title:"登录"', 'service.http.server:"nginx"', 'domain:"example.com"', 'ip:"1.1.1.1"', 'port:443', 'country_cn:"中国"', 'province_cn:"浙江"', 'city_cn:"杭州"']],
                    [_tf('infoCollectPage.syntaxSectionConbinations', '组合写法'), ['service.name:"http" AND country_cn:"中国"', '(service.name:"http" OR service.name:"https") AND port:443', 'domain:"example.com" AND NOT service.http.title:"404"', 'service.http.title:"login" AND port:443']],
                    [_tf('infoCollectPage.syntaxSectionExamples', '场景示例'), ['service.http.favicon.hash:"-247388890"', 'service.http.response.header:"JSESSIONID"', 'service.name:"ssh" AND country_cn:"中国"', 'service.http.title:"Dashboard" AND NOT ip:"127.0.0.1"']]
                ];
            }
        }
    },
    shodan: {
        label: 'Shodan',
        get placeholder() {
            return _tf('infoCollectPage.shodanQueryPlaceholder', '例如：product:nginx country:CN');
        },
        get nlPlaceholder() {
            return _tf('infoCollectPage.shodanNlPlaceholder', '例如：找中国的 nginx 资产，端口 443');
        },
        get hint() {
            return _tf('infoCollectPage.shodanHint', 'Shodan 使用 filter:value 语法，常见字段如 product、port、country、org。');
        },
        get parseHint() {
            return _tf('infoCollectPage.shodanParseHint', '解析后会弹窗展示 Shodan filter 语法（可编辑），确认无误后再填入查询框并执行查询。');
        },
        maxSize: 1000,
        get sizeHint() {
            return _tf('infoCollectPage.shodanSizeHint', 'Shodan 官方每页 100 条；后端会自动翻页聚合，单次最多 1000 条以控制额度消耗。');
        },
        fullOption: null,
        fields: 'ip_str,port,hostnames,domains,org,isp,location.country_name,location.city,product,transport',
        get presets() {
            return [
                [_tf('infoCollectPage.presetNginxChina', 'Nginx + 中国'), 'product:nginx country:CN'],
                [_tf('infoCollectPage.presetSsh', 'SSH 服务'), 'port:22'],
                [_tf('infoCollectPage.presetCertDomain', '证书域名'), 'ssl.cert.subject.cn:example.com'],
                [_tf('infoCollectPage.presetAmazon443', 'Amazon 443'), 'org:"Amazon" port:443']
            ];
        },
        get fieldPresets() {
            return [
                [_tf('infoCollectPage.minFields', '最小字段'), 'ip_str,port,hostnames,domains'],
                [_tf('infoCollectPage.webCommon', 'Web 常用'), 'ip_str,port,hostnames,domains,product,org,location.country_name,location.city'],
                [_tf('infoCollectPage.intelEnhanced', '情报增强'), 'ip_str,port,hostnames,domains,org,isp,asn,location.country_name,location.city,product,transport,ssl.cert.subject.cn']
            ];
        },
        syntaxGuide: {
            get summary() {
                return _tf('infoCollectPage.shodanSyntaxSummary', 'Shodan 默认搜索 banner data；精确条件使用 filter:value，值含空格时用双引号，多个过滤器并列表示收窄结果。');
            },
            docsUrl: 'https://help.shodan.io/the-basics/search-query-fundamentals',
            get sections() {
                return [
                    [_tf('infoCollectPage.syntaxSectionCommonFilters', '常用过滤器'), ['product:nginx', 'port:443', 'country:CN', 'city:Shanghai', 'org:"Amazon"', 'asn:AS15169', 'hostname:example.com', 'ssl.cert.subject.cn:example.com', 'http.title:"Dashboard"']],
                    [_tf('infoCollectPage.syntaxSectionConbinations', '组合写法'), ['product:nginx country:CN', 'apache port:443 country:DE', 'org:"Amazon" port:443', 'ssl.cert.subject.cn:example.com port:443']],
                    [_tf('infoCollectPage.syntaxSectionExamples', '场景示例'), ['http.title:"login" country:CN', 'ssl:true port:443 hostname:example.com', 'vuln:CVE-2021-41773', 'has_screenshot:true product:nginx']]
                ];
            }
        }
    }
};

const infoCollectState = {
    currentPayload: null, // { fields, results, query, total, page, size }
    hiddenFields: new Set(),
    selectedRowIndexes: new Set(),
    tableBound: false,
    providerSelectBound: false,
    presetEventsBound: false,
    syntaxGuideExpanded: false,
    queryHeightFrame: null,
    queryHeightResizeBound: false
};

// AI 解析（自然语言 -> FOFA）交互状态
let fofaParseAbortController = null;
let fofaParseSlowTimer = null;
let fofaParseToastHandle = null;

// HTML转义（如果未定义）
if (typeof escapeHtml === 'undefined') {
    function escapeHtml(text) {
        if (text == null) return '';
        const div = document.createElement('div');
        div.textContent = String(text);
        return div.innerHTML;
    }
}

function escapeAttr(text) {
    return escapeHtml(text).replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

function getFofaFormElements() {
    return {
        query: document.getElementById('fofa-query'),
        provider: document.getElementById('fofa-provider'),
        nl: document.getElementById('fofa-nl'),
        size: document.getElementById('fofa-size'),
        page: document.getElementById('fofa-page'),
        fields: document.getElementById('fofa-fields'),
        full: document.getElementById('fofa-full'),
        meta: document.getElementById('fofa-results-meta'),
        selectedMeta: document.getElementById('fofa-selected-meta'),
        thead: document.getElementById('fofa-results-thead'),
        tbody: document.getElementById('fofa-results-tbody'),
        columnsPanel: document.getElementById('fofa-columns-panel'),
        columnsList: document.getElementById('fofa-columns-list')
    };
}

function getInfoCollectProvider() {
    const provider = (document.getElementById('fofa-provider')?.value || 'fofa').trim().toLowerCase();
    return INFO_COLLECT_PROVIDERS[provider] ? provider : 'fofa';
}

function providerLabel(provider) {
    return (INFO_COLLECT_PROVIDERS[provider] || INFO_COLLECT_PROVIDERS.fofa).label;
}

function getInfoCollectFullOption(provider) {
    const cfg = INFO_COLLECT_PROVIDERS[provider] || INFO_COLLECT_PROVIDERS.fofa;
    return cfg.fullOption || null;
}

function isInfoCollectFullEnabled(provider) {
    const els = getFofaFormElements();
    return !!(getInfoCollectFullOption(provider) && els.full && els.full.checked);
}

function loadHiddenFieldsFromStorage() {
    try {
        const raw = localStorage.getItem(FOFA_HIDDEN_FIELDS_STORAGE_KEY);
        if (!raw) return [];
        const arr = JSON.parse(raw);
        if (!Array.isArray(arr)) return [];
        return arr.filter(x => typeof x === 'string');
    } catch (e) {
        return [];
    }
}

function saveHiddenFieldsToStorage() {
    try {
        localStorage.setItem(FOFA_HIDDEN_FIELDS_STORAGE_KEY, JSON.stringify(Array.from(infoCollectState.hiddenFields)));
    } catch (e) {
        // ignore
    }
}

function loadFofaFormFromStorage() {
    try {
        const raw = localStorage.getItem(FOFA_FORM_STORAGE_KEY);
        if (!raw) return null;
        const data = JSON.parse(raw);
        if (!data || typeof data !== 'object') return null;
        return data;
    } catch (e) {
        return null;
    }
}

function saveFofaFormToStorage(payload) {
    try {
        localStorage.setItem(FOFA_FORM_STORAGE_KEY, JSON.stringify(payload));
    } catch (e) {
        // ignore
    }
}

function initInfoCollectPage() {
    const els = getFofaFormElements();
    if (!els.query || !els.size || !els.fields || !els.tbody) return;

    // 恢复隐藏字段
    infoCollectState.hiddenFields = new Set(loadHiddenFieldsFromStorage());

    // 恢复上次输入
    const saved = loadFofaFormFromStorage();
    let shouldResetProviderFields = false;
    if (saved) {
        if (typeof saved.provider === 'string' && els.provider && INFO_COLLECT_PROVIDERS[saved.provider]) els.provider.value = saved.provider;
        if (typeof saved.query === 'string') els.query.value = saved.query;
        if (typeof saved.size === 'number' || typeof saved.size === 'string') els.size.value = saved.size;
        if (typeof saved.page === 'number' || typeof saved.page === 'string') els.page.value = saved.page;
        if (typeof saved.fields === 'string') els.fields.value = saved.fields;
        if (typeof saved.full === 'boolean') els.full.checked = saved.full;
        const provider = getInfoCollectProvider();
        const savedFields = String(saved.fields || '').trim();
        shouldResetProviderFields = provider !== 'fofa' && (
            savedFields === INFO_COLLECT_PROVIDERS.fofa.fields ||
            savedFields === 'host,ip,port,domain,title,protocol,country,province,city,server' ||
            savedFields === 'host,ip,port,domain'
        );
    }
    initInfoCollectProviderSelect();
    bindInfoCollectPresetEvents();
    refreshInfoCollectProviderUI(shouldResetProviderFields);

    // 绑定 Enter 快捷查询（在 query 里用 Ctrl/Cmd+Enter）
    els.query.addEventListener('keydown', (e) => {
        if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
            e.preventDefault();
            submitFofaSearch();
        }
    });

    // 自然语言输入：Ctrl/Cmd+Enter 触发解析
    if (els.nl) {
        els.nl.addEventListener('keydown', (e) => {
            if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
                e.preventDefault();
                parseFofaNaturalLanguage();
            }
        });
    }

    // textarea：按内容自动增高（避免默认留空白行）
    const autoGrowTextarea = (el) => {
        if (!el) return;
        try {
            el.style.height = '36px';
            const max = 96;
            const h = Math.min(max, el.scrollHeight);
            el.style.height = `${h}px`;
        } catch (e) {
            // ignore
        }
    };
    els.query.addEventListener('input', () => autoGrowTextarea(els.query));
    if (els.nl) els.nl.addEventListener('input', () => autoGrowTextarea(els.nl));
    // 初始化时也执行一次
    setTimeout(() => {
        autoGrowTextarea(els.query);
        autoGrowTextarea(els.nl);
    }, 0);
    setInfoCollectQueryMode('syntax', { focus: false });
    if (!infoCollectState.queryHeightResizeBound) {
        infoCollectState.queryHeightResizeBound = true;
        window.addEventListener('resize', scheduleInfoCollectQueryCardHeightStabilize);
    }

    // 绑定表格事件（事件委托，只绑定一次）
    bindFofaTableEvents();
    updateSelectedMeta();
}

function handleInfoCollectProviderChange() {
    infoCollectState.syntaxGuideExpanded = false;
    refreshInfoCollectProviderUI(true);
}

function setInfoCollectQueryMode(mode, options) {
    const shouldFocus = options?.focus !== false;
    const syntaxPanel = document.getElementById('info-collect-syntax-panel');
    const naturalPanel = document.getElementById('info-collect-natural-panel');

    if (syntaxPanel) {
        syntaxPanel.hidden = false;
        syntaxPanel.classList.add('is-active');
        syntaxPanel.classList.add('is-generated-target');
    }
    if (naturalPanel) {
        naturalPanel.hidden = false;
        naturalPanel.classList.add('is-active');
    }

    const queryLabel = document.getElementById('info-collect-query-label');
    const cfg = INFO_COLLECT_PROVIDERS[getInfoCollectProvider()] || INFO_COLLECT_PROVIDERS.fofa;
    if (queryLabel) {
        queryLabel.textContent = cfg.label + ' ' + _tf('infoCollectPage.syntaxLabel', '查询语法（可编辑，可直接查询）');
    }
    const nlLabel = document.getElementById('info-collect-nl-label');
    if (nlLabel) {
        nlLabel.textContent = _tf('infoCollectPage.naturalLabel', '自然语言（可选，AI 解析为查询语法）');
    }

    if (shouldFocus) {
        const focusTarget = mode === 'natural' ? document.getElementById('fofa-nl') : document.getElementById('fofa-query');
        try { focusTarget?.focus(); } catch (e) { /* ignore */ }
    }
    scheduleInfoCollectQueryCardHeightStabilize();
}

function scheduleInfoCollectQueryCardHeightStabilize() {
    if (infoCollectState.queryHeightFrame) {
        cancelAnimationFrame(infoCollectState.queryHeightFrame);
    }
    infoCollectState.queryHeightFrame = requestAnimationFrame(() => {
        infoCollectState.queryHeightFrame = null;
        stabilizeInfoCollectQueryCardHeight();
    });
}

function stabilizeInfoCollectQueryCardHeight() {
    const card = document.querySelector('.info-collect-query-card');
    if (!card) return;
    const rect = card.getBoundingClientRect();
    if (!rect.width) return;

    const clone = card.cloneNode(true);
    clone.style.position = 'absolute';
    clone.style.visibility = 'hidden';
    clone.style.pointerEvents = 'none';
    clone.style.left = '-10000px';
    clone.style.top = '0';
    clone.style.width = rect.width + 'px';
    clone.style.height = 'auto';
    clone.style.minHeight = '0';
    clone.style.maxHeight = 'none';

    const naturalPanel = clone.querySelector('#info-collect-natural-panel');
    if (naturalPanel) {
        naturalPanel.hidden = false;
        naturalPanel.classList.add('is-active');
    }
    const syntaxPanel = clone.querySelector('#info-collect-syntax-panel');
    if (syntaxPanel) {
        syntaxPanel.hidden = false;
        syntaxPanel.classList.add('is-active', 'is-generated-target');
    }
    const queryLabel = clone.querySelector('#info-collect-query-label');
    if (queryLabel) {
        const cfg = INFO_COLLECT_PROVIDERS[getInfoCollectProvider()] || INFO_COLLECT_PROVIDERS.fofa;
        queryLabel.textContent = cfg.label + ' ' + _tf('infoCollectPage.syntaxLabel', '查询语法（可编辑，可直接查询）');
    }

    document.body.appendChild(clone);
    const stableHeight = Math.ceil(clone.getBoundingClientRect().height);
    clone.remove();
    if (stableHeight > 0) {
        card.style.minHeight = stableHeight + 'px';
    }
}

function presetDataAttr(value) {
    return escapeHtml(String(value == null ? '' : value))
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
}

function bindInfoCollectPresetEvents() {
    if (infoCollectState.presetEventsBound) return;
    infoCollectState.presetEventsBound = true;
    document.addEventListener('click', (event) => {
        const queryBtn = event.target.closest?.('[data-info-query-preset]');
        if (queryBtn) {
            event.preventDefault();
            applyFofaQueryPreset(queryBtn.getAttribute('data-info-query-preset') || '');
            return;
        }
        const fieldsBtn = event.target.closest?.('[data-info-fields-preset]');
        if (fieldsBtn) {
            event.preventDefault();
            applyFofaFieldsPreset(fieldsBtn.getAttribute('data-info-fields-preset') || '');
            return;
        }
        const guideToggle = event.target.closest?.('[data-info-syntax-guide-toggle]');
        if (guideToggle) {
            event.preventDefault();
            toggleInfoCollectSyntaxGuide();
        }
    });
}

function initInfoCollectProviderSelect() {
    const els = getFofaFormElements();
    const select = els.provider;
    if (!select || infoCollectState.providerSelectBound) {
        syncInfoCollectProviderSelect();
        return;
    }
    infoCollectState.providerSelectBound = true;
    select.classList.add('settings-native-select');
    select.tabIndex = -1;
    select.setAttribute('aria-hidden', 'true');

    const wrapper = document.createElement('div');
    wrapper.className = 'settings-custom-select info-collect-provider-select';

    const trigger = document.createElement('button');
    trigger.type = 'button';
    trigger.className = 'settings-custom-select-trigger';
    trigger.setAttribute('aria-haspopup', 'listbox');
    trigger.setAttribute('aria-expanded', 'false');

    const value = document.createElement('span');
    value.className = 'settings-custom-select-value';
    value.id = 'info-collect-provider-select-value';
    const caret = document.createElement('span');
    caret.className = 'settings-custom-select-caret';
    caret.setAttribute('aria-hidden', 'true');
    caret.textContent = '▾';

    const menu = document.createElement('div');
    menu.className = 'settings-custom-select-menu';
    menu.id = 'info-collect-provider-select-menu';
    menu.setAttribute('role', 'listbox');

    trigger.appendChild(value);
    trigger.appendChild(caret);
    select.parentNode.insertBefore(wrapper, select);
    wrapper.appendChild(trigger);
    wrapper.appendChild(menu);
    wrapper.appendChild(select);

    trigger.addEventListener('click', (event) => {
        event.stopPropagation();
        const willOpen = !wrapper.classList.contains('open');
        closeInfoCollectProviderSelect();
        wrapper.classList.toggle('open', willOpen);
        trigger.setAttribute('aria-expanded', willOpen ? 'true' : 'false');
    });

    trigger.addEventListener('keydown', (event) => {
        const options = Array.prototype.filter.call(select.options, (option) => !option.disabled);
        if (!options.length) return;
        const current = Math.max(0, options.indexOf(select.options[select.selectedIndex]));
        let next = current;
        if (event.key === 'ArrowDown') next = Math.min(options.length - 1, current + 1);
        else if (event.key === 'ArrowUp') next = Math.max(0, current - 1);
        else if (event.key === 'Home') next = 0;
        else if (event.key === 'End') next = options.length - 1;
        else if (event.key === 'Escape') {
            closeInfoCollectProviderSelect();
            return;
        } else if (event.key === 'Enter' || event.key === ' ') {
            wrapper.classList.add('open');
            trigger.setAttribute('aria-expanded', 'true');
            event.preventDefault();
            return;
        } else {
            return;
        }
        event.preventDefault();
        const nextOption = options[next];
        if (nextOption && select.value !== nextOption.value) {
            select.value = nextOption.value;
            select.dispatchEvent(new Event('change', { bubbles: true }));
        }
        syncInfoCollectProviderSelect();
    });

    menu.addEventListener('click', (event) => {
        const item = event.target.closest('.settings-custom-select-option');
        if (!item || item.disabled) return;
        event.stopPropagation();
        const option = select.options[Number(item.dataset.index)];
        if (option && !option.disabled && select.value !== option.value) {
            select.value = option.value;
            select.dispatchEvent(new Event('change', { bubbles: true }));
        }
        syncInfoCollectProviderSelect();
        closeInfoCollectProviderSelect();
    });

    select.addEventListener('change', syncInfoCollectProviderSelect);
    document.addEventListener('click', closeInfoCollectProviderSelect);
    document.addEventListener('keydown', (event) => {
        if (event.key === 'Escape') closeInfoCollectProviderSelect();
    });
    syncInfoCollectProviderSelect();
}

function closeInfoCollectProviderSelect() {
    const wrapper = document.querySelector('.info-collect-provider-select');
    const trigger = wrapper?.querySelector('.settings-custom-select-trigger');
    if (!wrapper) return;
    wrapper.classList.remove('open');
    if (trigger) trigger.setAttribute('aria-expanded', 'false');
}

function syncInfoCollectProviderSelect() {
    const select = document.getElementById('fofa-provider');
    const wrapper = document.querySelector('.info-collect-provider-select');
    if (!select || !wrapper) return;
    const value = wrapper.querySelector('.settings-custom-select-value');
    const menu = wrapper.querySelector('.settings-custom-select-menu');
    const selected = select.options[select.selectedIndex];
    if (value) value.textContent = selected ? selected.textContent : '';
    if (!menu) return;
    menu.innerHTML = '';
    Array.prototype.forEach.call(select.options, (option, index) => {
        const item = document.createElement('button');
        item.type = 'button';
        item.className = 'settings-custom-select-option';
        item.setAttribute('role', 'option');
        item.setAttribute('data-index', String(index));
        item.setAttribute('aria-selected', option.selected ? 'true' : 'false');
        item.classList.toggle('is-selected', option.selected);
        item.disabled = !!option.disabled;
        const check = document.createElement('span');
        check.className = 'settings-custom-select-check';
        check.setAttribute('aria-hidden', 'true');
        check.textContent = '✓';
        const label = document.createElement('span');
        label.className = 'settings-custom-select-label';
        label.textContent = option.textContent;
        item.appendChild(check);
        item.appendChild(label);
        menu.appendChild(item);
    });
}

function renderInfoCollectSyntaxGuide(cfg) {
    const container = document.getElementById('info-collect-syntax-guide');
    if (!container) return;
    const guide = cfg.syntaxGuide;
    if (!guide) {
        container.innerHTML = '';
        container.hidden = true;
        return;
    }
    const docsLink = guide.docsUrl
        ? `<a class="info-collect-doc-link" href="${presetDataAttr(guide.docsUrl)}" target="_blank" rel="noopener noreferrer">${escapeHtml(_tf('infoCollectPage.officialDocs', '官方文档'))}</a>`
        : '';
    const expanded = !!infoCollectState.syntaxGuideExpanded;
    const sections = (guide.sections || []).map(([title, examples]) => {
        const chips = (examples || []).map(example => {
            return `<button class="syntax-example-chip" type="button" data-info-query-preset="${presetDataAttr(example)}" title="${escapeAttr(_tf('infoCollect.parseModalApply', '填入查询框'))}">${escapeHtml(example)}</button>`;
        }).join('');
        return `<div class="syntax-guide-section"><div class="syntax-guide-title">${escapeHtml(title)}</div><div class="syntax-guide-examples">${chips}</div></div>`;
    }).join('');
    container.hidden = false;
    container.classList.toggle('is-expanded', expanded);
    container.innerHTML = `
        <div class="syntax-guide-header">
            <div class="syntax-guide-summary">${escapeHtml(guide.summary || '')}</div>
            <div class="syntax-guide-actions">
                ${docsLink}
                <button class="syntax-guide-toggle" type="button" data-info-syntax-guide-toggle aria-expanded="${expanded ? 'true' : 'false'}">${escapeHtml(expanded ? _tf('infoCollectPage.collapseExamples', '收起示例') : _tf('infoCollectPage.expandExamples', '展开示例'))}</button>
            </div>
        </div>
        <div class="syntax-guide-body"${expanded ? '' : ' hidden'}>${sections}</div>
    `;
}

function toggleInfoCollectSyntaxGuide() {
    infoCollectState.syntaxGuideExpanded = !infoCollectState.syntaxGuideExpanded;
    const provider = getInfoCollectProvider();
    const cfg = INFO_COLLECT_PROVIDERS[provider] || INFO_COLLECT_PROVIDERS.fofa;
    renderInfoCollectSyntaxGuide(cfg);
    scheduleInfoCollectQueryCardHeightStabilize();
}

function refreshInfoCollectProviderUI(resetProviderFields) {
    const els = getFofaFormElements();
    const provider = getInfoCollectProvider();
    const cfg = INFO_COLLECT_PROVIDERS[provider] || INFO_COLLECT_PROVIDERS.fofa;
    const queryLabel = document.getElementById('info-collect-query-label');
    const nlLabel = document.getElementById('info-collect-nl-label');
    const queryHint = document.getElementById('info-collect-query-hint');
    const parseHint = document.getElementById('info-collect-parse-hint');
    const sizeHint = document.getElementById('info-collect-size-hint');
    const parseBtn = document.getElementById('fofa-nl-parse-btn');
    const presets = document.getElementById('info-collect-query-presets');
    const fieldPresets = document.getElementById('info-collect-fields-presets');
    const fullOption = document.getElementById('info-collect-full-option');
    const fullText = fullOption ? fullOption.querySelector('.checkbox-text') : null;
    const fullConfig = getInfoCollectFullOption(provider);
    if (queryLabel) queryLabel.textContent = cfg.label + ' ' + _tf('infoCollectPage.querySyntax', '查询语法');
    if (nlLabel) nlLabel.textContent = _tf('infoCollect.naturalLanguageProviderLabel', '自然语言（AI 解析为 {{provider}} 语法）', { provider: cfg.label }).replace('{{provider}}', cfg.label);
    if (queryHint) queryHint.textContent = cfg.hint;
    if (parseHint) parseHint.textContent = cfg.parseHint;
    if (sizeHint) sizeHint.textContent = cfg.sizeHint;
    if (parseBtn && parseBtn.dataset.loading !== '1') parseBtn.title = _tf('infoCollect.parseToProviderQuery', '将自然语言解析为 {{provider}} 查询语法', { provider: cfg.label }).replace('{{provider}}', cfg.label);
    if (els.query) els.query.placeholder = cfg.placeholder;
    if (els.nl) els.nl.placeholder = cfg.nlPlaceholder;
    if (els.size) {
        els.size.max = String(cfg.maxSize || 10000);
        const currentSize = parseInt(els.size.value, 10) || 100;
        if (cfg.maxSize && currentSize > cfg.maxSize) els.size.value = cfg.maxSize;
    }
    if (fullOption) {
        if (fullConfig) {
            fullOption.hidden = false;
            fullOption.title = fullConfig.hint || '';
            if (fullText) fullText.textContent = fullConfig.label || _t('infoCollectPage.fullLabel');
        } else {
            fullOption.hidden = true;
            fullOption.title = '';
            if (els.full) els.full.checked = false;
        }
    }
    if (els.fields && (resetProviderFields || !els.fields.value.trim())) els.fields.value = cfg.fields;
    if (presets) {
        presets.innerHTML = cfg.presets.map(([label, query]) => {
            return `<button class="preset-chip" type="button" data-info-query-preset="${presetDataAttr(query)}" title="${escapeAttr(_tf('infoCollectPage.fillExample', '填入示例'))}">${escapeHtml(label)}</button>`;
        }).join('');
    }
    if (fieldPresets) {
        fieldPresets.innerHTML = cfg.fieldPresets.map(([label, fields]) => {
            return `<button class="preset-chip" type="button" data-info-fields-preset="${presetDataAttr(fields)}" title="${escapeAttr(_tf('infoCollectPage.fillFieldsTemplate', '填入字段模板'))}">${escapeHtml(label)}</button>`;
        }).join('');
    }
    renderInfoCollectSyntaxGuide(cfg);
    saveFofaFormToStorage({
        provider,
        query: (els.query?.value || '').trim(),
        size: parseInt(els.size?.value, 10) || 100,
        page: parseInt(els.page?.value, 10) || 1,
        fields: els.fields?.value || '',
        full: isInfoCollectFullEnabled(provider)
    });
    setInfoCollectQueryMode('syntax', { focus: false });
    scheduleInfoCollectQueryCardHeightStabilize();
}

function applyFofaQueryPreset(preset) {
    const els = getFofaFormElements();
    if (!els.query) return;
    setInfoCollectQueryMode('syntax');
    els.query.value = (preset || '').trim();
    els.query.focus();
    saveFofaFormToStorage({
        provider: getInfoCollectProvider(),
        query: els.query.value,
        size: parseInt(els.size?.value, 10) || 100,
        page: parseInt(els.page?.value, 10) || 1,
        fields: els.fields?.value || '',
        full: isInfoCollectFullEnabled(getInfoCollectProvider())
    });
}

function applyFofaFieldsPreset(preset) {
    const els = getFofaFormElements();
    if (!els.fields) return;
    els.fields.value = (preset || '').trim();
    els.fields.focus();
    saveFofaFormToStorage({
        provider: getInfoCollectProvider(),
        query: (els.query?.value || '').trim(),
        size: parseInt(els.size?.value, 10) || 100,
        page: parseInt(els.page?.value, 10) || 1,
        fields: els.fields.value,
        full: isInfoCollectFullEnabled(getInfoCollectProvider())
    });
}

function resetFofaForm() {
    const els = getFofaFormElements();
    if (!els.query) return;
    const provider = getInfoCollectProvider();
    const cfg = INFO_COLLECT_PROVIDERS[provider] || INFO_COLLECT_PROVIDERS.fofa;
    els.query.value = '';
    if (els.size) els.size.value = 100;
    if (els.page) els.page.value = 1;
    if (els.fields) els.fields.value = cfg.fields;
    if (els.full) els.full.checked = false;
    if (els.nl) els.nl.value = '';
    setInfoCollectQueryMode('syntax');
    saveFofaFormToStorage({
        provider,
        query: els.query.value,
        size: parseInt(els.size?.value, 10) || 100,
        page: parseInt(els.page?.value, 10) || 1,
        fields: els.fields?.value || '',
        full: isInfoCollectFullEnabled(provider)
    });
    renderFofaResults({ query: '', fields: [], results: [], total: 0, page: 1, size: 0 });
}

async function submitFofaSearch() {
    const els = getFofaFormElements();
    const provider = getInfoCollectProvider();
    const query = (els.query?.value || '').trim();
    const providerCfg = INFO_COLLECT_PROVIDERS[provider] || INFO_COLLECT_PROVIDERS.fofa;
    const maxSize = providerCfg.maxSize || 10000;
    let size = parseInt(els.size?.value, 10) || 100;
    if (size > maxSize) {
        size = maxSize;
        if (els.size) els.size.value = String(maxSize);
        const sizeMsg = _tf('infoCollectPage.maxSizeAdjusted', '单次最多返回 {{maxSize}} 条，已自动调整。', { maxSize });
        showInlineToast(providerCfg.label + ' ' + sizeMsg.replace('{{maxSize}}', String(maxSize)));
    }
    const page = parseInt(els.page?.value, 10) || 1;
    const fields = (els.fields?.value || '').trim();
    const full = isInfoCollectFullEnabled(provider);

    if (!query) {
        alert(_t('infoCollect.enterFofaQuery'));
        return;
    }

    saveFofaFormToStorage({ provider, query, size, page, fields, full });
    setFofaMeta(providerLabel(provider) + ' ' + _t('infoCollect.querying'));
    setFofaLoading(true);

    try {
        const response = await apiFetch('/api/fofa/search', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ provider, query, size, page, fields, full })
        });

        const result = await response.json().catch(() => ({}));
        if (!response.ok) {
            throw new Error(result.error || _tf('infoCollectPage.requestFailed', '请求失败') + ': ' + response.status);
        }
        renderFofaResults(result);
    } catch (e) {
        console.error(providerLabel(provider) + ' ' + _tf('infoCollect.queryFailed', '查询失败') + ':', e);
        setFofaMeta(_t('infoCollect.queryFailed'));
        renderFofaResults({ provider, query, fields: [], results: [], total: 0, page: 1, size: 0 });
        alert(_t('infoCollect.queryFailed') + ': ' + (e && e.message ? e.message : String(e)));
    } finally {
        setFofaLoading(false);
    }
}

async function parseFofaNaturalLanguage() {
    const els = getFofaFormElements();
    const provider = getInfoCollectProvider();
    const text = (els.nl?.value || '').trim();
    if (!text) {
        alert(_t('infoCollect.enterNaturalLanguage'));
        return;
    }

    // 二次点击：取消进行中的解析（避免“以为卡死/失败”）
    if (fofaParseAbortController) {
        try { fofaParseAbortController.abort(); } catch (e) { /* ignore */ }
        return;
    }

    // 先创建 controller，避免极快的重复点击触发并发请求
    fofaParseAbortController = new AbortController();
    setFofaParseLoading(true, _t('infoCollect.parsePending'));

    // 持续提示：直到请求完成/取消/失败才消失
    fofaParseToastHandle = showInlineToast(_t('infoCollect.parsePendingClickCancel'), { duration: 0, id: 'fofa-parse-pending' });

    // 如果超过一小段时间还没返回，再强调“仍在进行中”，降低误判为失败的概率
    fofaParseSlowTimer = setTimeout(() => {
        const status = document.getElementById('fofa-nl-status');
        if (status) {
            status.textContent = _t('infoCollect.parseSlow');
            status.style.display = 'block';
        }
    }, 1800);

    try {
        const resp = await apiFetch('/api/fofa/parse', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ provider, text }),
            signal: fofaParseAbortController.signal
        });
        const result = await resp.json().catch(() => ({}));
        if (!resp.ok) {
            throw new Error(result.error || _tf('infoCollectPage.requestFailed', '请求失败') + ': ' + resp.status);
        }
        showFofaParseModal(text, result);
        showInlineToast(_t('infoCollect.parseDone'));
    } catch (e) {
        // AbortController 取消：不视为失败
        if (e && (e.name === 'AbortError' || String(e).includes('AbortError'))) {
            showInlineToast(_t('infoCollect.parseCancelled'));
            return;
        }
        console.error(_t('infoCollect.parseFailed') + ':', e);
        showInlineToast(_t('infoCollect.parseFailed') + (e && e.message ? e.message : String(e)), { duration: 2800 });
    }
    finally {
        fofaParseAbortController = null;
        if (fofaParseSlowTimer) {
            clearTimeout(fofaParseSlowTimer);
            fofaParseSlowTimer = null;
        }
        if (fofaParseToastHandle && typeof fofaParseToastHandle.remove === 'function') {
            fofaParseToastHandle.remove();
        }
        fofaParseToastHandle = null;
        setFofaParseLoading(false, '');
    }
}

function setFofaParseLoading(loading, statusText) {
    const btn = document.getElementById('fofa-nl-parse-btn');
    const status = document.getElementById('fofa-nl-status');
    if (btn) {
        if (loading) {
            if (!btn.dataset.originalText) btn.dataset.originalText = btn.textContent || _t('infoCollectPage.parseBtn');
            btn.classList.add('btn-loading');
            btn.textContent = _t('infoCollect.cancelParse');
            btn.title = _t('infoCollect.clickToCancelParse');
            btn.dataset.loading = '1';
            btn.setAttribute('aria-busy', 'true');
            btn.disabled = false;
        } else {
            const provider = getInfoCollectProvider();
            const cfg = INFO_COLLECT_PROVIDERS[provider] || INFO_COLLECT_PROVIDERS.fofa;
            btn.classList.remove('btn-loading');
            btn.textContent = btn.dataset.originalText || _t('infoCollectPage.parseBtn');
            btn.title = _tf('infoCollect.parseToProviderQuery', '将自然语言解析为 {{provider}} 查询语法', { provider: cfg.label }).replace('{{provider}}', cfg.label);
            btn.disabled = false;
            delete btn.dataset.loading;
            btn.removeAttribute('aria-busy');
        }
    }
    if (status) {
        const text = (statusText || '').trim();
        if (loading && text) {
            status.textContent = text;
            status.style.display = 'block';
        } else {
            status.textContent = '';
            status.style.display = 'none';
        }
    }
}

function showFofaParseModal(nlText, parsed) {
    const existing = document.getElementById('fofa-parse-modal');
    if (existing) existing.remove();

    const provider = getInfoCollectProvider();
    const cfg = INFO_COLLECT_PROVIDERS[provider] || INFO_COLLECT_PROVIDERS.fofa;
    const safeNL = escapeHtml((nlText || '').trim());
    const warnings = Array.isArray(parsed?.warnings) ? parsed.warnings.filter(Boolean).map(x => String(x)) : [];
    const explanation = parsed?.explanation != null ? String(parsed.explanation) : '';

    const warningsHtml = warnings.length
        ? `<ul class="info-collect-parse-warnings-list">${warnings.map(w => `<li>${escapeHtml(w)}</li>`).join('')}</ul>`
        : '<div class="muted info-collect-parse-warnings-empty">' + _t('infoCollect.none') + '</div>';

    const modal = document.createElement('div');
    modal.id = 'fofa-parse-modal';
    modal.className = 'modal';
    document.body.appendChild(modal);
    openAppModal(modal, { focus: false });
    deferModalContent(function () {
    modal.innerHTML = `
        <div class="modal-content info-collect-parse-modal-content" style="max-width: 900px;">
            <div class="modal-header">
                <h2>${_t('infoCollect.parseResultTitle')}</h2>
                <span class="modal-close" id="fofa-parse-modal-close" title="${_t('common.close')}">&times;</span>
            </div>
            <div class="info-collect-parse-modal-body">
                <div class="form-group">
                    <label>${_t('infoCollect.naturalLanguageLabel')}</label>
                    <div class="muted info-collect-parse-nl-text">${safeNL || '-'}</div>
                </div>

                <div class="form-group info-collect-parse-form-group">
                    <label for="fofa-parse-query">${escapeHtml(cfg.label + ' ' + _tf('infoCollect.providerQueryEditable', '查询语法（可编辑）'))}</label>
                    <textarea id="fofa-parse-query" class="info-collect-query-input" rows="2" placeholder="${escapeHtml(cfg.placeholder)}"></textarea>
                    <small class="form-hint">${_t('infoCollect.confirmBeforeQuery')}</small>
                </div>

                <div class="form-group info-collect-parse-form-group">
                    <label>${_t('infoCollect.reminder')}</label>
                    <div class="info-collect-parse-warnings">
                        ${warningsHtml}
                    </div>
                </div>

                ${explanation ? `
                <div class="form-group info-collect-parse-form-group">
                    <label>${_t('infoCollect.explanation')}</label>
                    <pre class="info-collect-parse-explanation">${escapeHtml(explanation)}</pre>
                </div>` : ''}
            </div>
            <div class="modal-footer info-collect-parse-modal-footer">
                <button class="btn-secondary" type="button" id="fofa-parse-cancel">${_t('infoCollect.parseModalCancel')}</button>
                <button class="btn-secondary" type="button" id="fofa-parse-apply">${_t('infoCollect.parseModalApply')}</button>
                <button class="btn-primary" type="button" id="fofa-parse-apply-run">${_t('infoCollect.parseModalApplyRun')}</button>
            </div>
        </div>
    `;

    const queryTextarea = document.getElementById('fofa-parse-query');
    if (queryTextarea) {
        queryTextarea.value = (parsed?.query || '').trim();
        queryTextarea.focus();
    }

    const close = function () {
        closeAppModal(modal);
        modal.remove();
        syncAppModalBodyLock();
    };
    modal.addEventListener('click', function (e) {
        if (e.target === modal) close();
    });
    document.getElementById('fofa-parse-modal-close')?.addEventListener('click', close);
    document.getElementById('fofa-parse-cancel')?.addEventListener('click', close);

    const applyToQuery = function (run) {
        const els = getFofaFormElements();
        const q = (queryTextarea?.value || '').trim();
        if (!q) {
            showInlineToast(_t('infoCollect.parseResultEmpty'), { duration: 2600 });
            return;
        }
        if (els.query) {
            els.query.value = q;
            try { els.query.focus(); } catch (e) { /* ignore */ }
        }
        // 写入表单缓存（与现有“直接查询”一致）
        saveFofaFormToStorage({
            query: q,
            size: parseInt(els.size?.value, 10) || 100,
            page: parseInt(els.page?.value, 10) || 1,
            fields: (els.fields?.value || '').trim(),
            full: isInfoCollectFullEnabled(getInfoCollectProvider())
        });
        close();
        if (run) submitFofaSearch();
    };

    document.getElementById('fofa-parse-apply')?.addEventListener('click', () => applyToQuery(false));
    document.getElementById('fofa-parse-apply-run')?.addEventListener('click', () => applyToQuery(true));

    // Esc 关闭
    const onKey = (e) => {
        if (e.key === 'Escape') {
            close();
            document.removeEventListener('keydown', onKey);
        }
    };
    document.addEventListener('keydown', onKey);
    });
}

function setFofaMeta(text) {
    const els = getFofaFormElements();
    if (els.meta) {
        els.meta.textContent = text || '-';
    }
}

function buildInfoCollectResultsMeta(provider, total, count, page, size, expectedCount, shortfall) {
    let text = providerLabel(provider) + ' · ' + _t('infoCollect.resultsMeta', { total, count, page, size });
    if (provider === 'shodan') {
        let expected = Number(expectedCount || 0);
        if (!Number.isFinite(expected) || expected <= 0) {
            const startOffset = Math.max(0, (Number(page) || 1) - 1) * 100;
            expected = Math.min(Number(size) || 0, Math.max(0, (Number(total) || 0) - startOffset));
        }
        const missing = Number(shortfall || 0);
        if (expected > 0 && (missing > 0 || count < expected)) {
            text += ' · ' + _t('infoCollect.providerReturnedFewer', { expected, count });
        }
    }
    return text;
}

function updateSelectedMeta() {
    const els = getFofaFormElements();
    if (els.selectedMeta) {
        els.selectedMeta.textContent = _t('infoCollectPage.selectedRows', { count: infoCollectState.selectedRowIndexes.size });
    }
}

function setFofaLoading(loading) {
    const els = getFofaFormElements();
    if (!els.tbody) return;
    if (loading) {
        const fieldsCount = (document.getElementById('fofa-fields')?.value || '').split(',').filter(Boolean).length;
        const colspan = Math.max(1, fieldsCount + 1);
        els.tbody.innerHTML = '<tr><td class="muted" style="padding: 16px;" colspan="' + colspan + '">' + escapeHtml(_t('infoCollect.loading')) + '</td></tr>';
    }
}

function renderFofaResults(payload) {
    const els = getFofaFormElements();
    if (!els.thead || !els.tbody) return;

    const fields = Array.isArray(payload.fields) ? payload.fields : [];
    const results = Array.isArray(payload.results) ? payload.results : [];

    // 保存当前 payload 到 state
    infoCollectState.currentPayload = {
        provider: payload.provider || getInfoCollectProvider(),
        query: payload.query || '',
        total: typeof payload.total === 'number' ? payload.total : 0,
        page: typeof payload.page === 'number' ? payload.page : 1,
        size: typeof payload.size === 'number' ? payload.size : 0,
        fields,
        results
    };

    // 清理选择（避免字段/结果变化导致错位）
    infoCollectState.selectedRowIndexes.clear();
    updateSelectedMeta();

    // 修剪隐藏字段：只保留当前 fields 中存在的
    const allowed = new Set(fields);
    infoCollectState.hiddenFields.forEach(f => {
        if (!allowed.has(f)) infoCollectState.hiddenFields.delete(f);
    });
    saveHiddenFieldsToStorage();

    const total = typeof payload.total === 'number' ? payload.total : 0;
    const size = typeof payload.size === 'number' ? payload.size : 0;
    const page = typeof payload.page === 'number' ? payload.page : 1;

    setFofaMeta(buildInfoCollectResultsMeta(
        infoCollectState.currentPayload.provider,
        total,
        results.length,
        page,
        size,
        typeof payload.expected_count === 'number' ? payload.expected_count : 0,
        typeof payload.shortfall === 'number' ? payload.shortfall : 0
    ));

    // 可见字段
    const visibleFields = fields.filter(f => !infoCollectState.hiddenFields.has(f));

    // 列面板
    renderFofaColumnsPanel(fields, visibleFields);

    // 表头（左：勾选列；右：操作列固定）
    const headerCells = [
        '<th class="info-collect-col-select"><input type="checkbox" id="fofa-select-all" class="theme-checkbox" title="' + escapeHtml(_t('infoCollect.selectAll')) + '"/></th>',
        ...visibleFields.map(f => `<th>${escapeHtml(String(f))}</th>`),
        '<th class="info-collect-col-actions">' + escapeHtml(_t('infoCollect.actions')) + '</th>'
    ].join('');
    els.thead.innerHTML = `<tr>${headerCells}</tr>`;

    // 表体
    if (results.length === 0) {
        const colspan = Math.max(1, visibleFields.length + 2);
        els.tbody.innerHTML = '<tr><td class="muted" style="padding: 16px;" colspan="' + colspan + '">' + escapeHtml(_t('common.noData')) + '</td></tr>';
        return;
    }

    const rowsHtml = results.map((row, idx) => {
        const safeRow = row && typeof row === 'object' ? row : {};
        const target = inferTargetFromRow(safeRow, fields);
        const encoded = encodeURIComponent(JSON.stringify(safeRow));
        const encodedTarget = encodeURIComponent(target || '');

        const selectHtml = '<td class="info-collect-col-select"><input class="fofa-row-select theme-checkbox" type="checkbox" data-index="' + idx + '" title="' + escapeHtml(_t('infoCollect.selectRow')) + '"/></td>';

        const cellsHtml = visibleFields.map(f => {
            const val = safeRow[f];
            const text = val == null ? '' : String(val);
            // host 字段：尽量渲染为可点击链接
            if (f === 'host') {
                const href = normalizeHttpLink(text);
                if (href) {
                    const safeHref = escapeAttr(href);
                    return `<td class="info-collect-cell" data-field="${escapeAttr(f)}" data-full="${escapeAttr(text)}" title="${escapeAttr(text)}"><a class="info-collect-link" href="${safeHref}" target="_blank" rel="noopener noreferrer" onclick="event.stopPropagation();">${escapeHtml(text)}</a></td>`;
                }
            }
            return `<td class="info-collect-cell" data-field="${escapeAttr(f)}" data-full="${escapeAttr(text)}" title="${escapeAttr(text)}"><span class="info-collect-cell-text">${escapeHtml(text)}</span></td>`;
        }).join('');

        const actionHtml = `
            <div class="info-collect-actions">
                <button class="btn-icon" onclick="copyFofaTargetEncoded('${encodedTarget}'); event.stopPropagation();" title="${escapeHtml(_t('infoCollect.copyTarget'))}">
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                        <rect x="9" y="9" width="13" height="13" rx="2" stroke="currentColor" stroke-width="2"/>
                        <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
                    </svg>
                </button>
                <button class="btn-icon" onclick="scanFofaRow('${encoded}', event); event.stopPropagation();" title="${escapeHtml(_t('infoCollect.sendToChat'))}">
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                        <path d="M10.5 13.5l3-3" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
                        <path d="M8 8H5a4 4 0 1 0 0 8h3" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
                        <path d="M16 8h3a4 4 0 0 1 0 8h-3" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
                    </svg>
                </button>
                <button class="btn-icon" data-require-permission="asset:write" onclick="importFofaRowAsset(${idx}); event.stopPropagation();" title="${escapeHtml(_t('assets.importOne'))}">
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none"><path d="M12 3v12m0 0 4-4m-4 4-4-4M4 19h16" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>
                </button>
            </div>
        `;

        return `<tr data-index="${idx}">${selectHtml}${cellsHtml}<td class="info-collect-col-actions">${actionHtml}</td></tr>`;
    }).join('');

    els.tbody.innerHTML = rowsHtml;

    // 更新全选框状态
    syncSelectAllCheckbox();
    if (typeof applyRBACToUI === 'function') applyRBACToUI(els.tbody);
}

function inferTargetFromRow(row, fields) {
    // 优先 host（FOFA 常见返回 http(s)://...）
    const host = row.host != null ? String(row.host).trim() : '';
    if (host) return host;

    const domain = row.domain != null ? String(row.domain).trim() : '';
    const ip = row.ip != null ? String(row.ip).trim() : '';
    const port = row.port != null ? String(row.port).trim() : '';
    const protocol = row.protocol != null ? String(row.protocol).trim().toLowerCase() : '';

    const base = domain || ip;
    if (!base) return '';

    if (port) {
        // 仅做一个轻量推断：443 -> https, 80 -> http，其余不强行加 scheme
        const p = parseInt(port, 10);
        if (!isNaN(p) && (p === 80 || p === 443)) {
            const scheme = p === 443 ? 'https' : 'http';
            return `${scheme}://${base}:${p}`;
        }
        if (protocol === 'https' || protocol === 'http') {
            return `${protocol}://${base}:${port}`;
        }
        return `${base}:${port}`;
    }

    return base;
}

function normalizeHttpLink(raw) {
    const v = (raw || '').trim();
    if (!v) return '';
    if (v.startsWith('http://') || v.startsWith('https://')) return v;
    // 某些 host 可能是 domain 或 ip:port；这里不强行拼装，避免误导
    return '';
}

function copyFofaTarget(target) {
    const text = (target || '').trim();
    if (!text) {
        alert(_t('infoCollect.noTargetToCopy'));
        return;
    }
    navigator.clipboard.writeText(text).then(() => {
        // 简单提示
        showInlineToast(_t('infoCollect.targetCopied'));
    }).catch(() => {
        alert(_t('infoCollect.manualCopyHint') + text);
    });
}

function copyFofaTargetEncoded(encodedTarget) {
    try {
        copyFofaTarget(decodeURIComponent(encodedTarget || ''));
    } catch (e) {
        copyFofaTarget(encodedTarget || '');
    }
}

// showInlineToast('xxx')；也支持 showInlineToast('xxx', { duration: 0, id: '...' })
function showInlineToast(text, options) {
    const opts = options && typeof options === 'object' ? options : {};
    const duration = typeof opts.duration === 'number' ? opts.duration : 1200;
    const id = typeof opts.id === 'string' && opts.id.trim() ? opts.id.trim() : '';
    const replace = opts.replace !== false;

    if (id && replace) {
        document.getElementById(id)?.remove();
    }

    const toast = document.createElement('div');
    if (id) toast.id = id;
    toast.textContent = String(text == null ? '' : text);
    toast.style.cssText = 'position: fixed; top: 24px; right: 24px; background: rgba(0,0,0,0.85); color: #fff; padding: 10px 12px; border-radius: 8px; z-index: 10000; font-size: 13px; max-width: 420px; line-height: 1.4; box-shadow: 0 6px 18px rgba(0,0,0,0.22);';
    document.body.appendChild(toast);

    let timer = null;
    const remove = () => {
        try { if (timer) clearTimeout(timer); } catch (e) { /* ignore */ }
        timer = null;
        try { toast.remove(); } catch (e) { /* ignore */ }
    };

    if (duration > 0) {
        timer = setTimeout(remove, duration);
    }

    return { el: toast, remove };
}

function truncateForPreview(value, maxLen) {
    const s = value == null ? '' : String(value);
    if (maxLen <= 0 || s.length <= maxLen) return s;
    return s.slice(0, maxLen) + '...(' + _t('infoCollect.truncated') + ')';
}

function formatFofaRowSummary(row, fields) {
    const r = row && typeof row === 'object' ? row : {};
    const order = [];
    const seen = new Set();

    const preferred = Array.isArray(fields) ? fields : [];
    preferred.forEach(k => {
        const key = String(k || '').trim();
        if (!key || seen.has(key)) return;
        seen.add(key);
        order.push(key);
    });

    Object.keys(r).sort().forEach(k => {
        if (seen.has(k)) return;
        seen.add(k);
        order.push(k);
    });

    if (order.length === 0) return '-';

    const lines = order.map((k) => {
        const v = r[k];
        let text = '';
        if (v === null) text = 'null';
        else if (v === undefined) text = '';
        else if (typeof v === 'string') text = v === '' ? '""' : v;
        else if (typeof v === 'number' || typeof v === 'boolean') text = String(v);
        else {
            try { text = JSON.stringify(v); } catch (e) { text = String(v); }
        }
        text = truncateForPreview(text, 800);
        return `- ${k}: ${text}`;
    });

    return lines.join('\n');
}

function scanFofaRow(encodedRowJson, clickEvent) {
    let row = {};
    try {
        row = JSON.parse(decodeURIComponent(encodedRowJson));
    } catch (e) {
        console.warn(_t('infoCollectPage.parseRowFailed') + ':', e);
    }

    const fields = (document.getElementById('fofa-fields')?.value || '').split(',').map(s => s.trim()).filter(Boolean);
    const target = inferTargetFromRow(row, fields);
    if (!target) {
        alert(_t('infoCollect.cannotInferTarget'));
        return;
    }

    // 切换到对话页并发送消息（每次点击都新建会话，避免发到历史会话）
    if (typeof switchPage === 'function') {
        switchPage('chat');
    } else {
        window.location.hash = 'chat';
    }

    const message = buildScanMessage(target, row, { fields });
    const autoSend = !!(clickEvent && (clickEvent.ctrlKey || clickEvent.metaKey));

    setTimeout(async () => {
        // 新建会话：必须等待其完成，否则它会在后续把输入框清空
        try {
            if (typeof startNewConversation === 'function') {
                const maybePromise = startNewConversation();
                if (maybePromise && typeof maybePromise.then === 'function') {
                    await maybePromise;
                }
            }
        } catch (e) {
            // ignore
        }

        const input = document.getElementById('chat-input');
        if (input) {
            input.value = message;
            // 触发自动高度调整（chat.js 里如果监听 input）
            input.dispatchEvent(new Event('input', { bubbles: true }));
            input.focus();
        }
        if (autoSend) {
            if (typeof sendMessage === 'function') {
                window.__csNextChatFinalizationPolicy = { requireExecutionEvidence: true };
                sendMessage();
            } else {
                alert(_t('infoCollect.noSendMessage'));
            }
        } else {
            showInlineToast(_t('infoCollect.filledToInput'));
        }
    }, 250);
}

function buildScanMessage(target, row, options) {
    const opts = options && typeof options === 'object' ? options : {};
    const fields = Array.isArray(opts.fields) ? opts.fields : [];

    const summary = formatFofaRowSummary(row || {}, fields);
    const provider = providerLabel(infoCollectState.currentPayload?.provider || getInfoCollectProvider());
    // 插值不转 HTML 实体（target 可能是 URL，避免 / 变 &#x2F;）；i18n 未就绪时手动替换占位符
    const values = {
        target, provider, summary,
        interpolation: { escapeValue: false }
    };
    const template = '对以下目标做信息收集与基础扫描：\n{{target}}\n\n要求：\n1) 识别服务/框架与关键指纹\n2) 枚举开放端口与常见管理入口\n3) 用 httpx/指纹/目录探测等方式快速确认可访问面\n4) 输出可复现的命令与结论\n\n已知信息（来自 {{provider}} 该行全部字段）：\n{{summary}}';
    let text = _tf('infoCollect.scanMessageTemplate', template, values);
    if (text === template) {
        Object.entries({ target, provider, summary }).forEach(([name, value]) => {
            text = text.replace('{{' + name + '}}', String(value));
        });
    }
    return text.trim();
}

function bindFofaTableEvents() {
    if (infoCollectState.tableBound) return;
    infoCollectState.tableBound = true;

    const els = getFofaFormElements();
    if (!els.tbody) return;

    // 事件委托：选择/单元格展开
    els.tbody.addEventListener('click', (e) => {
        const checkbox = e.target && e.target.classList && e.target.classList.contains('fofa-row-select') ? e.target : null;
        if (checkbox) {
            const idx = parseInt(checkbox.getAttribute('data-index'), 10);
            if (!isNaN(idx)) {
                if (checkbox.checked) infoCollectState.selectedRowIndexes.add(idx);
                else infoCollectState.selectedRowIndexes.delete(idx);
                updateSelectedMeta();
                syncSelectAllCheckbox();
            }
            return;
        }

        const cell = e.target && e.target.closest ? e.target.closest('.info-collect-cell') : null;
        if (cell) {
            const full = cell.getAttribute('data-full') || '';
            const field = cell.getAttribute('data-field') || '';
            // 点击链接不弹窗
            if (e.target && e.target.tagName === 'A') return;
            if (full && full.length > 0) {
                showCellDetailModal(field, full);
            }
        }
    });

    // thead 的全选（因为 thead 会重渲染，用事件捕获到 document）
    document.addEventListener('change', (e) => {
        const t = e.target;
        if (!t || t.id !== 'fofa-select-all') return;
        const checked = !!t.checked;
        toggleSelectAllRows(checked);
    });
}

function toggleSelectAllRows(checked) {
    const els = getFofaFormElements();
    if (!els.tbody) return;
    const boxes = els.tbody.querySelectorAll('input.fofa-row-select');
    infoCollectState.selectedRowIndexes.clear();
    boxes.forEach(b => {
        b.checked = checked;
        const idx = parseInt(b.getAttribute('data-index'), 10);
        if (checked && !isNaN(idx)) infoCollectState.selectedRowIndexes.add(idx);
    });
    updateSelectedMeta();
    syncSelectAllCheckbox();
}

function syncSelectAllCheckbox() {
    const selectAll = document.getElementById('fofa-select-all');
    const els = getFofaFormElements();
    if (!selectAll || !els.tbody) return;
    const boxes = els.tbody.querySelectorAll('input.fofa-row-select');
    const total = boxes.length;
    const selected = infoCollectState.selectedRowIndexes.size;
    if (total === 0) {
        selectAll.checked = false;
        selectAll.indeterminate = false;
        return;
    }
    if (selected === 0) {
        selectAll.checked = false;
        selectAll.indeterminate = false;
    } else if (selected === total) {
        selectAll.checked = true;
        selectAll.indeterminate = false;
    } else {
        selectAll.checked = false;
        selectAll.indeterminate = true;
    }
}

function renderFofaColumnsPanel(allFields, visibleFields) {
    const els = getFofaFormElements();
    if (!els.columnsList) return;
    const currentVisible = new Set(visibleFields);
    els.columnsList.innerHTML = allFields.map(f => {
        const checked = currentVisible.has(f);
        const safe = escapeHtml(f);
        return `
            <label class="info-collect-col-item" title="${safe}">
                <input type="checkbox" ${checked ? 'checked' : ''} onchange="toggleFofaColumn('${safe}', this.checked)" />
                <span>${safe}</span>
            </label>
        `;
    }).join('');
}

function toggleFofaColumn(field, visible) {
    const f = String(field || '').trim();
    if (!f) return;
    if (visible) infoCollectState.hiddenFields.delete(f);
    else infoCollectState.hiddenFields.add(f);
    saveHiddenFieldsToStorage();
    // 重新渲染表格（用 state 中缓存的 payload）
    if (infoCollectState.currentPayload) {
        renderFofaResults(infoCollectState.currentPayload);
    }
}

function toggleFofaColumnsPanel() {
    const els = getFofaFormElements();
    if (!els.columnsPanel) return;
    const show = els.columnsPanel.style.display === 'none' || !els.columnsPanel.style.display;
    els.columnsPanel.style.display = show ? 'block' : 'none';
}

function closeFofaColumnsPanel() {
    const els = getFofaFormElements();
    if (els.columnsPanel) els.columnsPanel.style.display = 'none';
}

// 点击面板外部关闭（避免一直占着表格顶部）
document.addEventListener('click', (e) => {
    const panel = document.getElementById('fofa-columns-panel');
    const btn = e.target && e.target.closest ? e.target.closest('button') : null;
    const isColumnsBtn = btn && btn.getAttribute && btn.getAttribute('onclick') && String(btn.getAttribute('onclick')).includes('toggleFofaColumnsPanel');
    if (!panel || panel.style.display === 'none') return;
    if (panel.contains(e.target) || isColumnsBtn) return;
    panel.style.display = 'none';
});

function showAllFofaColumns() {
    infoCollectState.hiddenFields.clear();
    saveHiddenFieldsToStorage();
    if (infoCollectState.currentPayload) renderFofaResults(infoCollectState.currentPayload);
}

function hideAllFofaColumns() {
    const p = infoCollectState.currentPayload;
    if (!p || !Array.isArray(p.fields)) return;
    // 允许隐藏全部，但给用户一个最小可用：至少保留 host/ip/domain 中之一（如果存在）
    const keep = ['host', 'ip', 'domain'].find(x => p.fields.includes(x));
    infoCollectState.hiddenFields = new Set(p.fields.filter(f => f !== keep));
    saveHiddenFieldsToStorage();
    renderFofaResults(p);
}

function exportFofaResults(format) {
    const p = infoCollectState.currentPayload;
    if (!p || !Array.isArray(p.results) || p.results.length === 0) {
        alert(_t('infoCollect.noExportResult'));
        return;
    }

    const fields = p.fields || [];
    const visibleFields = fields.filter(f => !infoCollectState.hiddenFields.has(f));
    const provider = p.provider || 'fofa';

    const now = new Date();
    const ts = `${now.getFullYear()}${String(now.getMonth() + 1).padStart(2, '0')}${String(now.getDate()).padStart(2, '0')}_${String(now.getHours()).padStart(2, '0')}${String(now.getMinutes()).padStart(2, '0')}${String(now.getSeconds()).padStart(2, '0')}`;

    if (format === 'json') {
        const payload = {
            provider,
            query: p.query || '',
            total: p.total || 0,
            page: p.page || 1,
            size: p.size || 0,
            fields: fields,
            results: p.results
        };
        downloadBlob(JSON.stringify(payload, null, 2), `${provider}_results_${ts}.json`, 'application/json;charset=utf-8');
        return;
    }

    if (format === 'xlsx') {
        // 使用 SheetJS 生成 XLSX（需在页面中引入 xlsx 库）
        if (typeof XLSX === 'undefined') {
            alert(_t('infoCollect.xlsxNotLoaded'));
            return;
        }
        const aoa = [visibleFields].concat(p.results.map(row => {
            const r = row && typeof row === 'object' ? row : {};
            return visibleFields.map(f => r[f] != null ? r[f] : '');
        }));
        const ws = XLSX.utils.aoa_to_sheet(aoa);
        const wb = XLSX.utils.book_new();
        XLSX.utils.book_append_sheet(wb, ws, _t('infoCollect.batchScanTitle'));
        XLSX.writeFile(wb, `${provider}_results_${ts}.xlsx`);
        return;
    }

    // csv：默认导出可见字段，带 UTF-8 BOM 以兼容 Excel 中文
    const header = visibleFields;
    const rows = p.results.map(row => {
        const r = row && typeof row === 'object' ? row : {};
        return header.map(f => csvEscape(r[f]));
    });
    const csv = [header.map(csvEscape).join(','), ...rows.map(cols => cols.join(','))].join('\n');
    const csvWithBom = '\uFEFF' + csv;
    downloadBlob(csvWithBom, `${provider}_results_${ts}.csv`, 'text/csv;charset=utf-8');
}

function csvEscape(value) {
    if (value == null) return '""';
    const s = String(value).replace(/"/g, '""');
    return `"${s}"`;
}

function downloadBlob(content, filename, mime) {
    const blob = new Blob([content], { type: mime });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
}

async function batchScanSelectedFofaRows() {
    const p = infoCollectState.currentPayload;
    if (!p || !Array.isArray(p.results) || p.results.length === 0) {
        alert(_t('infoCollect.noResults'));
        return;
    }
    const selected = Array.from(infoCollectState.selectedRowIndexes).sort((a, b) => a - b);
    if (selected.length === 0) {
        alert(_t('infoCollect.selectRowsFirst'));
        return;
    }

    const fields = p.fields || [];
    const tasks = [];
    const skipped = [];

    selected.forEach(idx => {
        const row = p.results[idx];
        const target = inferTargetFromRow(row || {}, fields);
        if (!target) {
            skipped.push(idx + 1);
            return;
        }
        // 批量任务：与单条一致，只带“该行全部字段”的摘要（避免重复与超长）
        tasks.push(buildScanMessage(target, row || {}, {
            fields
        }));
    });

    if (tasks.length === 0) {
        alert(_t('infoCollect.noScanTarget'));
        return;
    }

    const title = (p.query ? _t('infoCollect.batchScanTitle') + '：' + p.query : _t('infoCollect.batchScanTitle')).slice(0, 80);
    try {
        // 不强制切换到“信息收集”角色：沿用当前已选角色；若为默认则传空字符串交给后端走默认逻辑
        let role = '';
        if (typeof getCurrentRole === 'function') {
            try { role = getCurrentRole() || ''; } catch (e) { /* ignore */ }
        }
        if (role === 'Default') role = '';

        const resp = await apiFetch('/api/batch-tasks', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                title,
                tasks,
                role,
                projectId: typeof getActiveProjectId === 'function' ? getActiveProjectId() || '' : '',
            })
        });
        const result = await resp.json().catch(() => ({}));
        if (!resp.ok) {
            throw new Error(result.error || _t('infoCollect.createQueueFailed') + ': ' + resp.status);
        }
        const queueId = result.queueId;
        if (!queueId) {
            throw new Error(_tf('infoCollectPage.createQueueNoId', '创建成功但未返回 queueId'));
        }

        // 跳到任务管理并打开队列详情
        if (typeof switchPage === 'function') switchPage('tasks');
        setTimeout(() => {
            if (typeof showBatchQueueDetail === 'function') {
                showBatchQueueDetail(queueId);
            }
        }, 250);

        if (skipped.length > 0) {
            showInlineToast(_t('infoCollect.queueCreatedSkipped', { n: skipped.length }));
        } else {
            showInlineToast(_t('infoCollect.batchQueueCreated'));
        }
    } catch (e) {
        console.error(_t('infoCollect.batchScanFailed') + ':', e);
        alert(_t('infoCollect.batchScanFailed') + ': ' + (e && e.message ? e.message : String(e)));
    }
}

function showCellDetailModal(field, fullText) {
    const existing = document.getElementById('info-collect-cell-modal');
    if (existing) existing.remove();

    const text = fullText == null ? '' : String(fullText);
    const fieldName = field || _t('infoCollect.field');
    const charCountLabel = _t('infoCollect.cellValueLength', { count: Array.from(text).length });
    const modal = document.createElement('div');
    modal.id = 'info-collect-cell-modal';
    modal.className = 'info-collect-cell-modal';
    modal.innerHTML = `
        <div class="info-collect-cell-modal-content" role="dialog" aria-modal="true">
            <div class="info-collect-cell-modal-header">
                <div class="info-collect-cell-modal-heading">
                    <div class="info-collect-cell-modal-title">${escapeHtml(fieldName)}</div>
                    <div class="info-collect-cell-modal-subtitle">${escapeHtml(charCountLabel)}</div>
                </div>
                <button class="btn-icon" type="button" id="info-collect-cell-modal-close" title="${_t('common.close')}">
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                        <path d="M18 6L6 18M6 6l12 12" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
                    </svg>
                </button>
            </div>
            <div class="info-collect-cell-modal-body">
                <pre class="info-collect-cell-modal-pre">${escapeHtml(text)}</pre>
            </div>
            <div class="info-collect-cell-modal-footer">
                <button class="btn-secondary" type="button" id="info-collect-cell-modal-copy">${_t('common.copy')}</button>
                <button class="btn-primary" type="button" id="info-collect-cell-modal-ok">${_t('common.close')}</button>
            </div>
        </div>
    `;

    document.body.appendChild(modal);
    openAppModal(modal);

    const onKey = (e) => {
        if (e.key === 'Escape') {
            close();
        }
    };
    const close = function () {
        document.removeEventListener('keydown', onKey);
        closeAppModal(modal);
        modal.remove();
        syncAppModalBodyLock();
    };
    modal.addEventListener('click', (e) => {
        if (e.target === modal) close();
    });
    document.getElementById('info-collect-cell-modal-close')?.addEventListener('click', close);
    document.getElementById('info-collect-cell-modal-ok')?.addEventListener('click', close);
    document.getElementById('info-collect-cell-modal-copy')?.addEventListener('click', () => {
        navigator.clipboard.writeText(text).then(() => showInlineToast(_t('common.copied'))).catch(() => alert(_t('common.copyFailed')));
    });

    // Esc 关闭
    document.addEventListener('keydown', onKey);
}

// 暴露到全局（供 index.html onclick 调用）
window.initInfoCollectPage = initInfoCollectPage;
window.resetFofaForm = resetFofaForm;
window.submitFofaSearch = submitFofaSearch;
window.parseFofaNaturalLanguage = parseFofaNaturalLanguage;
window.setInfoCollectQueryMode = setInfoCollectQueryMode;
window.scanFofaRow = scanFofaRow;
window.copyFofaTarget = copyFofaTarget;
window.copyFofaTargetEncoded = copyFofaTargetEncoded;
window.applyFofaQueryPreset = applyFofaQueryPreset;
window.applyFofaFieldsPreset = applyFofaFieldsPreset;
window.toggleFofaColumnsPanel = toggleFofaColumnsPanel;
window.closeFofaColumnsPanel = closeFofaColumnsPanel;
window.showAllFofaColumns = showAllFofaColumns;
window.hideAllFofaColumns = hideAllFofaColumns;
window.toggleFofaColumn = toggleFofaColumn;
window.exportFofaResults = exportFofaResults;
window.batchScanSelectedFofaRows = batchScanSelectedFofaRows;

document.addEventListener('languagechange', function () {
    updateSelectedMeta();
});

if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', function () { updateSelectedMeta(); });
} else {
    updateSelectedMeta();
}
