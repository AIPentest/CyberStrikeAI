// Information gathering page (FOFA)
function _t(key, opts) {
    return typeof window.t === 'function' ? window.t(key, opts) : key;
}

// Translation falls back to Chinese (when i18n is not ready, the original text is still displayed, avoiding exposure of internal key names)
function _tf(key, fallback, opts) {
    return typeof window.t === 'function' ? window.t(key, opts) : fallback;
}

const FOFA_FORM_STORAGE_KEY = 'info-collect-fofa-form';
const FOFA_HIDDEN_FIELDS_STORAGE_KEY = 'info-collect-fofa-hidden-fields';

const INFO_COLLECT_PROVIDERS = {
    fofa: {
        label: 'FOFA',
        get placeholder() {
            return _tf('infoCollect.queryPlaceholder', 'For example: app="Apache" && country="CN"');
        },
        get nlPlaceholder() {
            return _tf('infoCollectPage.nlPlaceholder', 'For example: find Apache sites in Missouri, USA, with titles containing Home');
        },
        get hint() {
            return _tf('infoCollectPage.formHint', 'For query syntax, refer to the FOFA documentation, which supports && / || / (), etc.');
        },
        get parseHint() {
            return _tf('infoCollectPage.parseHint', 'After parsing, a popup will display the FOFA syntax (editable); once confirmed to be correct, fill it into the query box and execute the query.');
        },
        maxSize: 10000,
        get sizeHint() {
            return _tf('infoCollectPage.fofaSizeHint', 'The maximum number of results returned by FOFA depends on account permissions; the frontend allows a maximum of 10000.');
        },
        fullOption: {
            get label() {
                return _tf('infoCollectPage.fofaFullLabel', 'Complete mode');
            },
            get hint() {
                return _tf('infoCollectPage.fofaFullHint', 'Pass full=true to FOFA, which returns more complete/more real-time data, but may consume more quota.');
            }
        },
        fields: 'host,ip,port,domain,title,protocol,country,region,city,server',
        get presets() {
            return [
                [_tf('infoCollectPage.presetApache', 'Apache + China'), 'app="Apache" && country="CN"'],
                [_tf('infoCollectPage.presetLogin', 'Login page + China'), 'title="Login" && country="CN"'],
                [_tf('infoCollectPage.presetDomain', 'specified domain name'), 'domain="example.com"'],
                [_tf('infoCollectPage.presetIp', 'specified IP'), 'ip="1.1.1.1"']
            ];
        },
        get fieldPresets() {
            return [
                [_tf('infoCollectPage.minFields', 'minimum field'), 'host,ip,port,domain'],
                [_tf('infoCollectPage.webCommon', 'Web commonly used'), 'host,title,ip,port,domain,protocol,server,icp,country,region,city'],
                [_tf('infoCollectPage.intelEnhanced', 'intelligence enhancement'), 'host,ip,port,domain,title,protocol,country,region,city,server,asn,org,icp,header,banner']
            ];
        },
        syntaxGuide: {
            get summary() {
                return _tf('infoCollectPage.fofaSyntaxSummary', 'FOFA uses field="value" for exact matching, supports &&, ||, ! and parenthesis combinations; strings are recommended to be wrapped in double quotes.');
            },
            docsUrl: 'https://en.fofa.info/api',
            get sections() {
                return [
                    [_tf('infoCollectPage.syntaxSectionCommonFields', 'Commonly used fields'), ['app="Apache"', 'title="Admin Management"', 'body="Powered by"', 'domain="example.com"', 'host="https://example.com"', 'ip="1.1.1.1"', 'port="443"', 'country="CN"', 'city="Hangzhou"', 'server="nginx"']],
                    [_tf('infoCollectPage.syntaxSectionConbinations', 'combined notation'), ['app="nginx" && country="CN"', 'title="login" || title="login"', '(app="Apache" || app="nginx") && port="443"', 'domain="example.com" && !title="404"']],
                    [_tf('infoCollectPage.syntaxSectionExamples', 'Scenario example'), ['cert="example.com" && port="443"', 'header="JSESSIONID" && country="CN"', 'icon_hash="-247388890"', 'fid="sZyXkR9e" && domain="example.com"']]
                ];
            }
        }
    },
    zoomeye: {
        label: 'ZoomEye',
        get placeholder() {
            return _tf('infoCollect.queryPlaceholder', 'For example: app="Apache" && country="CN"');
        },
        get nlPlaceholder() {
            return _tf('infoCollectPage.zoomeyeNlPlaceholder', 'For example: find SSH services in China, excluding honeypots');
        },
        get hint() {
            return _tf('infoCollectPage.zoomeyeHint', 'ZoomEye supports syntax such as app/title/domain/ip/port/country/city.');
        },
        get parseHint() {
            return _tf('infoCollectPage.zoomeyeParseHint', 'After parsing, a popup will appear displaying the ZoomEye syntax (editable); once confirmed correct, it is then filled into the query box and the query is executed.');
        },
        maxSize: 10000,
        get sizeHint() {
            return _tf('infoCollectPage.zoomeyeSizeHint', 'ZoomEye pagesize supports a maximum of 10000; the actual quota depends on your account.');
        },
        fullOption: null,
        fields: 'ip,port,domain,hostname,title,service,app,country,city',
        get presets() {
            return [
                [_tf('infoCollectPage.presetApache', 'Apache + China'), 'app="Apache" && country="CN"'],
                [_tf('infoCollectPage.presetSsh', 'SSH service'), 'service="ssh"'],
                [_tf('infoCollectPage.presetDomain', 'specified domain name'), 'domain="example.com"'],
                [_tf('infoCollectPage.presetIp', 'specified IP'), 'ip="1.1.1.1"']
            ];
        },
        get fieldPresets() {
            return [
                [_tf('infoCollectPage.minFields', 'minimum field'), 'ip,port,domain,hostname'],
                [_tf('infoCollectPage.webCommon', 'Web commonly used'), 'ip,port,domain,hostname,title,service,app,country,city'],
                [_tf('infoCollectPage.intelEnhanced', 'intelligence enhancement'), 'ip,port,domain,hostname,title,service,app,country,city,org,isp,ssl']
            ];
        },
        syntaxGuide: {
            get summary() {
                return _tf('infoCollectPage.zoomeyeSyntaxSummary', 'ZoomEye supports field search, quoted phrases, and AND/OR/NOT combined with parentheses; field names are based on what the official console actually supports.');
            },
            docsUrl: 'https://www.zoomeye.ai/help',
            get sections() {
                return [
                    [_tf('infoCollectPage.syntaxSectionCommonFields', 'Commonly used fields'), ['app="Apache"', 'service="ssh"', 'title="Login"', 'domain="example.com"', 'hostname="example.com"', 'ip="1.1.1.1"', 'port=443', 'country="CN"', 'city="Beijing"', 'org="Tencent"']],
                    [_tf('infoCollectPage.syntaxSectionConbinations', 'combined notation'), ['app="nginx" AND country="CN"', 'service="http" AND (title="login" OR title="login")', 'domain="example.com" AND NOT app="cloudflare"', 'port=443 AND country="US"']],
                    [_tf('infoCollectPage.syntaxSectionExamples', 'Scenario example'), ['ssl.cert.fingerprint="SHA256 value"', 'iconhash="-247388890"', 'service="rdp" AND country="CN"', 'app="Elasticsearch" AND port=9200']]
                ];
            }
        }
    },
    quake: {
        label: 'Quake',
        get placeholder() {
            return _tf('infoCollectPage.quakeQueryPlaceholder', 'For example: service.name:"http" AND country_cn:"China"');
        },
        get nlPlaceholder() {
            return _tf('infoCollectPage.quakeNlPlaceholder', 'For example: find HTTP services in China, title contains login');
        },
        get hint() {
            return _tf('infoCollectPage.quakeHint', 'Quake uses DSL syntax, with common fields such as service.name, domain, ip, port, country_cn.');
        },
        get parseHint() {
            return _tf('infoCollectPage.quakeParseHint', 'After parsing, a popup will display the Quake DSL (editable); once you confirm it is correct, fill it into the query box and execute the query.');
        },
        maxSize: 10000,
        get sizeHint() {
            return _tf('infoCollectPage.quakeSizeHint', 'Quake size will consume points; it is recommended to control the number of returned results as needed.');
        },
        fullOption: {
            get label() {
                return _tf('infoCollectPage.quakeFullLabel', 'Latest data');
            },
            get hint() {
                return _tf('infoCollectPage.quakeFullHint', 'Pass latest=true to Quake to prioritize querying the latest data.');
            }
        },
        fields: 'ip,port,domain,service.name,service.http.title,location.country_cn,location.province_cn,location.city_cn',
        get presets() {
            return [
                [_tf('infoCollectPage.presetHttpChina', 'HTTP + China'), 'service.name:"http" AND country_cn:"China"'],
                [_tf('infoCollectPage.presetPort443', 'Port 443'), 'port:443'],
                [_tf('infoCollectPage.presetDomain', 'specified domain name'), 'domain:"example.com"'],
                [_tf('infoCollectPage.presetIp', 'specified IP'), 'ip:"1.1.1.1"']
            ];
        },
        get fieldPresets() {
            return [
                [_tf('infoCollectPage.minFields', 'minimum field'), 'ip,port,domain'],
                [_tf('infoCollectPage.webCommon', 'Web commonly used'), 'ip,port,domain,service.name,service.http.title,location.country_cn,location.city_cn'],
                [_tf('infoCollectPage.intelEnhanced', 'intelligence enhancement'), 'ip,port,domain,service.name,service.http.title,service.http.server,location.country_cn,location.province_cn,location.city_cn,asn']
            ];
        },
        syntaxGuide: {
            get summary() {
                return _tf('infoCollectPage.quakeSyntaxSummary', 'Quake uses Lucene/DSL-style queries, with the common form being field:"value", and logical operators typically use AND, OR, NOT.');
            },
            docsUrl: 'https://quake.360.net/quake/#/help',
            get sections() {
                return [
                    [_tf('infoCollectPage.syntaxSectionCommonFields', 'Commonly used fields'), ['service.name:"http"', 'service.http.title:"login"', 'service.http.server:"nginx"', 'domain:"example.com"', 'ip:"1.1.1.1"', 'port:443', 'country_cn:"China"', 'province_cn:"Zhejiang"', 'city_cn:"Hangzhou"']],
                    [_tf('infoCollectPage.syntaxSectionConbinations', 'combined notation'), ['service.name:"http" AND country_cn:"China"', '(service.name:"http" OR service.name:"https") AND port:443', 'domain:"example.com" AND NOT service.http.title:"404"', 'service.http.title:"login" AND port:443']],
                    [_tf('infoCollectPage.syntaxSectionExamples', 'Scenario example'), ['service.http.favicon.hash:"-247388890"', 'service.http.response.header:"JSESSIONID"', 'service.name:"ssh" AND country_cn:"China"', 'service.http.title:"Dashboard" AND NOT ip:"127.0.0.1"']]
                ];
            }
        }
    },
    shodan: {
        label: 'Shodan',
        get placeholder() {
            return _tf('infoCollectPage.shodanQueryPlaceholder', 'For example: product:nginx country:CN');
        },
        get nlPlaceholder() {
            return _tf('infoCollectPage.shodanNlPlaceholder', 'For example: find China nginx assets, port 443');
        },
        get hint() {
            return _tf('infoCollectPage.shodanHint', 'Shodan uses the filter:value syntax, with common fields such as product, port, country, and org.');
        },
        get parseHint() {
            return _tf('infoCollectPage.shodanParseHint', 'After parsing, a popup will display the Shodan filter syntax (editable); once confirmed correct, fill it into the query box and execute the query.');
        },
        maxSize: 1000,
        get sizeHint() {
            return _tf('infoCollectPage.shodanSizeHint', 'Shodan officially returns 100 results per page; the backend automatically paginates and aggregates them, with a maximum of 1000 results per single request to control quota consumption.');
        },
        fullOption: null,
        fields: 'ip_str,port,hostnames,domains,org,isp,location.country_name,location.city,product,transport',
        get presets() {
            return [
                [_tf('infoCollectPage.presetNginxChina', 'Nginx + China'), 'product:nginx country:CN'],
                [_tf('infoCollectPage.presetSsh', 'SSH service'), 'port:22'],
                [_tf('infoCollectPage.presetCertDomain', 'certificate domain name'), 'ssl.cert.subject.cn:example.com'],
                [_tf('infoCollectPage.presetAmazon443', 'Amazon 443'), 'org:"Amazon" port:443']
            ];
        },
        get fieldPresets() {
            return [
                [_tf('infoCollectPage.minFields', 'minimum field'), 'ip_str,port,hostnames,domains'],
                [_tf('infoCollectPage.webCommon', 'Web commonly used'), 'ip_str,port,hostnames,domains,product,org,location.country_name,location.city'],
                [_tf('infoCollectPage.intelEnhanced', 'intelligence enhancement'), 'ip_str,port,hostnames,domains,org,isp,asn,location.country_name,location.city,product,transport,ssl.cert.subject.cn']
            ];
        },
        syntaxGuide: {
            get summary() {
                return _tf('infoCollectPage.shodanSyntaxSummary', 'Shodan searches banner data by default; for exact conditions use filter:value, use double quotes when the value contains spaces, and multiple filters listed together narrow the results.');
            },
            docsUrl: 'https://help.shodan.io/the-basics/search-query-fundamentals',
            get sections() {
                return [
                    [_tf('infoCollectPage.syntaxSectionCommonFilters', 'Commonly used filters'), ['product:nginx', 'port:443', 'country:CN', 'city:Shanghai', 'org:"Amazon"', 'asn:AS15169', 'hostname:example.com', 'ssl.cert.subject.cn:example.com', 'http.title:"Dashboard"']],
                    [_tf('infoCollectPage.syntaxSectionConbinations', 'combined notation'), ['product:nginx country:CN', 'apache port:443 country:DE', 'org:"Amazon" port:443', 'ssl.cert.subject.cn:example.com port:443']],
                    [_tf('infoCollectPage.syntaxSectionExamples', 'Scenario example'), ['http.title:"login" country:CN', 'ssl:true port:443 hostname:example.com', 'vuln:CVE-2021-41773', 'has_screenshot:true product:nginx']]
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

// AI parsing (natural language -> FOFA) interaction state
let fofaParseAbortController = null;
let fofaParseSlowTimer = null;
let fofaParseToastHandle = null;

// HTML escape (if not defined)
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

    // Restore hidden field
    infoCollectState.hiddenFields = new Set(loadHiddenFieldsFromStorage());

    // Restore last input
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

    // Bind Enter shortcut query (use Ctrl/Cmd+Enter in query)
    els.query.addEventListener('keydown', (e) => {
        if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
            e.preventDefault();
            submitFofaSearch();
        }
    });

    // Natural language input: Ctrl/Cmd+Enter triggers parsing
    if (els.nl) {
        els.nl.addEventListener('keydown', (e) => {
            if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
                e.preventDefault();
                parseFofaNaturalLanguage();
            }
        });
    }

    // textarea: automatically grows in height based on content (avoid leaving a blank line by default)
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
    // Also execute once during initialization
    setTimeout(() => {
        autoGrowTextarea(els.query);
        autoGrowTextarea(els.nl);
    }, 0);
    setInfoCollectQueryMode('syntax', { focus: false });
    if (!infoCollectState.queryHeightResizeBound) {
        infoCollectState.queryHeightResizeBound = true;
        window.addEventListener('resize', scheduleInfoCollectQueryCardHeightStabilize);
    }

    // Bind table events（event delegation，bind only once）
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
        queryLabel.textContent = cfg.label + ' ' + _tf('infoCollectPage.syntaxLabel', 'Query syntax (editable, can be directly queried)');
    }
    const nlLabel = document.getElementById('info-collect-nl-label');
    if (nlLabel) {
        nlLabel.textContent = _tf('infoCollectPage.naturalLabel', 'Natural language (optional, AI parses it into query syntax)');
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
        queryLabel.textContent = cfg.label + ' ' + _tf('infoCollectPage.syntaxLabel', 'Query syntax (editable, can be directly queried)');
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
        ? `<a class="info-collect-doc-link" href="${presetDataAttr(guide.docsUrl)}" target="_blank" rel="noopener noreferrer">${escapeHtml(_tf('infoCollectPage.officialDocs', 'Official documentation'))}</a>`
        : '';
    const expanded = !!infoCollectState.syntaxGuideExpanded;
    const sections = (guide.sections || []).map(([title, examples]) => {
        const chips = (examples || []).map(example => {
            return `<button class="syntax-example-chip" type="button" data-info-query-preset="${presetDataAttr(example)}" title="${escapeAttr(_tf('infoCollect.parseModalApply', 'Fill in the query box'))}">${escapeHtml(example)}</button>`;
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
                <button class="syntax-guide-toggle" type="button" data-info-syntax-guide-toggle aria-expanded="${expanded ? 'true' : 'false'}">${escapeHtml(expanded ? _tf('infoCollectPage.collapseExamples', 'Collapse example') : _tf('infoCollectPage.expandExamples', 'Expand example'))}</button>
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
    if (queryLabel) queryLabel.textContent = cfg.label + ' ' + _tf('infoCollectPage.querySyntax', 'Query Syntax');
    if (nlLabel) nlLabel.textContent = _tf('infoCollect.naturalLanguageProviderLabel', 'Natural language (AI parses as {{provider}} grammar)', { provider: cfg.label }).replace('{{provider}}', cfg.label);
    if (queryHint) queryHint.textContent = cfg.hint;
    if (parseHint) parseHint.textContent = cfg.parseHint;
    if (sizeHint) sizeHint.textContent = cfg.sizeHint;
    if (parseBtn && parseBtn.dataset.loading !== '1') parseBtn.title = _tf('infoCollect.parseToProviderQuery', 'Parse natural language into {{provider}} query syntax', { provider: cfg.label }).replace('{{provider}}', cfg.label);
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
            return `<button class="preset-chip" type="button" data-info-query-preset="${presetDataAttr(query)}" title="${escapeAttr(_tf('infoCollectPage.fillExample', 'Fill-in example'))}">${escapeHtml(label)}</button>`;
        }).join('');
    }
    if (fieldPresets) {
        fieldPresets.innerHTML = cfg.fieldPresets.map(([label, fields]) => {
            return `<button class="preset-chip" type="button" data-info-fields-preset="${presetDataAttr(fields)}" title="${escapeAttr(_tf('infoCollectPage.fillFieldsTemplate', 'fill in field template'))}">${escapeHtml(label)}</button>`;
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
        const sizeMsg = _tf('infoCollectPage.maxSizeAdjusted', 'A maximum of {{maxSize}} items can be returned per request; this has been automatically adjusted.', { maxSize });
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
            throw new Error(result.error || _tf('infoCollectPage.requestFailed', 'Request failed') + ': ' + response.status);
        }
        renderFofaResults(result);
    } catch (e) {
        console.error(providerLabel(provider) + ' ' + _tf('infoCollect.queryFailed', 'Query failed') + ':', e);
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

    // Second click: cancel the in-progress parsing (to avoid "thinking it's frozen/failed")
    if (fofaParseAbortController) {
        try { fofaParseAbortController.abort(); } catch (e) { /* ignore */ }
        return;
    }

    // Create the controller first to avoid extremely fast repeated clicks triggering concurrent requests
    fofaParseAbortController = new AbortController();
    setFofaParseLoading(true, _t('infoCollect.parsePending'));

    // Persistent prompt: only disappears when the request completes/is cancelled/fails
    fofaParseToastHandle = showInlineToast(_t('infoCollect.parsePendingClickCancel'), { duration: 0, id: 'fofa-parse-pending' });

    // If it still hasn't returned after a short while, further emphasize "still in progress" to reduce the probability of it being misjudged as a failure
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
            throw new Error(result.error || _tf('infoCollectPage.requestFailed', 'Request failed') + ': ' + resp.status);
        }
        showFofaParseModal(text, result);
        showInlineToast(_t('infoCollect.parseDone'));
    } catch (e) {
        // AbortController cancellation: not treated as a failure
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
            btn.title = _tf('infoCollect.parseToProviderQuery', 'Parse natural language into {{provider}} query syntax', { provider: cfg.label }).replace('{{provider}}', cfg.label);
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
                    <label for="fofa-parse-query">${escapeHtml(cfg.label + ' ' + _tf('infoCollect.providerQueryEditable', 'Query syntax (editable)'))}</label>
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
        // Write to form cache (consistent with existing "direct query")
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

    // Esc to close
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

    // Save current payload to state
    infoCollectState.currentPayload = {
        provider: payload.provider || getInfoCollectProvider(),
        query: payload.query || '',
        total: typeof payload.total === 'number' ? payload.total : 0,
        page: typeof payload.page === 'number' ? payload.page : 1,
        size: typeof payload.size === 'number' ? payload.size : 0,
        fields,
        results
    };

    // Clean up the selection (to avoid misalignment caused by field/result changes)
    infoCollectState.selectedRowIndexes.clear();
    updateSelectedMeta();

    // Trim hidden fields: only keep the ones present in the current fields
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

    // Visible fields
    const visibleFields = fields.filter(f => !infoCollectState.hiddenFields.has(f));

    // Column panel
    renderFofaColumnsPanel(fields, visibleFields);

    // Table header (left: checkbox column; right: action column fixed)
    const headerCells = [
        '<th class="info-collect-col-select"><input type="checkbox" id="fofa-select-all" class="theme-checkbox" title="' + escapeHtml(_t('infoCollect.selectAll')) + '"/></th>',
        ...visibleFields.map(f => `<th>${escapeHtml(String(f))}</th>`),
        '<th class="info-collect-col-actions">' + escapeHtml(_t('infoCollect.actions')) + '</th>'
    ].join('');
    els.thead.innerHTML = `<tr>${headerCells}</tr>`;

    // table body
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
            // host field: render as a clickable link whenever possible
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

    // Update select-all checkbox state
    syncSelectAllCheckbox();
    if (typeof applyRBACToUI === 'function') applyRBACToUI(els.tbody);
}

function inferTargetFromRow(row, fields) {
    // Prefer host (FOFA commonly returns http(s)://...)
    const host = row.host != null ? String(row.host).trim() : '';
    if (host) return host;

    const domain = row.domain != null ? String(row.domain).trim() : '';
    const ip = row.ip != null ? String(row.ip).trim() : '';
    const port = row.port != null ? String(row.port).trim() : '';
    const protocol = row.protocol != null ? String(row.protocol).trim().toLowerCase() : '';

    const base = domain || ip;
    if (!base) return '';

    if (port) {
        // Only make a lightweight inference: 443 -> https, 80 -> http; for the rest, don't forcibly add a scheme
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
    // Some hosts may be a domain or ip:port; we don't force concatenation here, to avoid confusion
    return '';
}

function copyFofaTarget(target) {
    const text = (target || '').trim();
    if (!text) {
        alert(_t('infoCollect.noTargetToCopy'));
        return;
    }
    navigator.clipboard.writeText(text).then(() => {
        // Simple prompt
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

// showInlineToast('xxx'); also supports showInlineToast('xxx', { duration: 0, id: '...' })
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

    // Switch to the conversation page and send a message (each click creates a new session, to avoid sending to historical sessions)
    if (typeof switchPage === 'function') {
        switchPage('chat');
    } else {
        window.location.hash = 'chat';
    }

    const message = buildScanMessage(target, row, { fields });
    const autoSend = !!(clickEvent && (clickEvent.ctrlKey || clickEvent.metaKey));

    setTimeout(async () => {
        // New session: must wait for it to complete, otherwise it will clear the input box later
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
            // Trigger automatic height adjustment (in chat.js if listening to input)
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
    // Interpolated values are not converted to HTML entities (target may be a URL, avoid / becoming &#x2F;); manually replace placeholders when i18n is not ready yet
    const values = {
        target, provider, summary,
        interpolation: { escapeValue: false }
    };
    const template = 'Perform information gathering and basic scanning on the following target:\n{{target}}\n\nRequirements:\n1) Identify services/frameworks and key fingerprints\n2) Enumerate open ports and common admin entry points\n3) Quickly confirm the accessible surface using methods such as httpx/fingerprinting/directory probing\n4) Output reproducible commands and conclusions\n\nKnown information (all fields of this row from {{provider}}):\n{{summary}}';
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

    // Event delegate: select/cell expand
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
            // Clicking the link doesn't pop up a window
            if (e.target && e.target.tagName === 'A') return;
            if (full && full.length > 0) {
                showCellDetailModal(field, full);
            }
        }
    });

    // Select all for thead (since thead re-renders, use event capture to the document)
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
    // Re-render the table (using the payload cached in state)
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

// Click outside the panel to close it (avoiding it from always occupying the top of the table)
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
    // Allow hiding all, but give the user a minimal usable one: at least keep one of host/ip/domain (if present)
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
        // Use SheetJS to generate XLSX (need to include the xlsx library in the page)
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

    // csv: by default exports visible fields, with UTF-8 BOM for Excel Chinese compatibility
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
        // Batch tasks: same as single entry, only include a summary of "all fields of that row" (to avoid duplication and excessive length)
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
        // Don't force switching to the "information collection" role: keep using the currently selected role; if it is the default, pass an empty string to the backend to run the default logic
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
            throw new Error(_tf('infoCollectPage.createQueueNoId', 'Created successfully but no queueId was returned'));
        }

        // Jump to task management and open queue details
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

    // Esc to close
    document.addEventListener('keydown', onKey);
}

// Expose to global (for index.html onclick call)
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
