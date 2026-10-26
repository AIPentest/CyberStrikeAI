// API文档页面JavaScript

let apiSpec = null;
let currentToken = null;

function _t(key, opts) {
    return typeof window.t === 'function' ? window.t(key, opts) : key;
}

function waitForI18n() {
    return new Promise(function (resolve) {
        if (window.t) return resolve();
        var n = 0;
        var iv = setInterval(function () {
            if (window.t) { clearInterval(iv); resolve(); return; }
            n++;
            if (n >= 100) { clearInterval(iv); resolve(); }
        }, 50);
    });
}

// 从 OpenAPI spec 的 x-i18n-tags 构建 tag -> i18n key 映射（方案 A：后端提供键）
var apiSpecTagToKey = {};
function buildApiSpecTagToKey() {
    apiSpecTagToKey = {};
    if (!apiSpec || !apiSpec.paths) return;
    Object.keys(apiSpec.paths).forEach(function (path) {
        var pathItem = apiSpec.paths[path];
        if (!pathItem || typeof pathItem !== 'object') return;
        ['get', 'post', 'put', 'delete', 'patch'].forEach(function (method) {
            var op = pathItem[method];
            if (!op || !op.tags || !op['x-i18n-tags']) return;
            var tags = op.tags;
            var keys = op['x-i18n-tags'];
            for (var i = 0; i < tags.length && i < keys.length; i++) {
                apiSpecTagToKey[tags[i]] = typeof keys[i] === 'string' ? keys[i] : keys[i];
            }
        });
    });
}
function translateApiDocTag(tag) {
    if (!tag) return tag;
    var key = apiSpecTagToKey[tag];
    if (!key) return tag;
    var i18nKey = 'apiDocs.tags.' + key;
    var translated = _t(i18nKey);
    return translated === i18nKey ? tag : translated;
}
function translateApiDocSummaryFromOp(op) {
    var key = op && op['x-i18n-summary'];
    if (key) return _t('apiDocs.summary.' + key);
    return op && op.summary ? op.summary : '';
}
function translateApiDocResponseDescFromResp(resp) {
    if (!resp) return '';
    var key = resp['x-i18n-description'];
    if (key) return _t('apiDocs.response.' + key);
    return resp.description || '';
}

// 初始化
document.addEventListener('DOMContentLoaded', async () => {
    await waitForI18n();
    await loadToken();
    await loadAPISpec();
    if (apiSpec) {
        renderAPIDocs();
    }
    document.addEventListener('languagechange', function () {
        if (typeof window.applyTranslations === 'function') {
            window.applyTranslations(document);
        }
        if (apiSpec) {
            renderAPIDocs();
        }
    });
});

// 加载token
async function loadToken() {
    try {
        const authData = localStorage.getItem('cyberstrike-auth');
        if (authData) {
            const parsed = JSON.parse(authData);
            if (parsed && parsed.token) {
                const expiry = parsed.expiresAt ? new Date(parsed.expiresAt) : null;
                if (!expiry || expiry.getTime() > Date.now()) {
                    currentToken = parsed.token;
                    return;
                }
            }
        }
        currentToken = localStorage.getItem('swagger_auth_token');
    } catch (e) {
        console.error('加载token失败:', e);
    }
}

// 加载OpenAPI规范
async function loadAPISpec() {
    try {
        const url = '/api/openapi/spec';
        const headers = currentToken ? { 'Authorization': `Bearer ${currentToken}` } : {};
        const response = await fetch(url, { headers });
        if (!response.ok) {
            if (response.status === 401) {
                showError(_t('apiDocs.errorLoginRequired'));
                return;
            }
            throw new Error(_t('apiDocs.errorLoadSpec') + response.status);
        }
        
        apiSpec = await response.json();
        buildApiSpecTagToKey();
    } catch (error) {
        console.error('加载API规范失败:', error);
        showError(_t('apiDocs.errorLoadFailed') + error.message);
    }
}

// 显示错误
function showError(message) {
    const main = document.getElementById('api-docs-main');
    const loadFailed = _t('apiDocs.loadFailed');
    const backToLogin = _t('apiDocs.backToLogin');
    main.innerHTML = `
        <div class="empty-state">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <circle cx="12" cy="12" r="10"/>
                <line x1="15" y1="9" x2="9" y2="15"/>
                <line x1="9" y1="9" x2="15" y2="15"/>
            </svg>
            <h3>${escapeHtml(loadFailed)}</h3>
            <p>${escapeHtml(message)}</p>
            <div style="margin-top: 16px;">
                <a href="/" style="color: var(--accent-color); text-decoration: none;">${escapeHtml(backToLogin)}</a>
            </div>
        </div>
    `;
}

// 渲染API文档
function renderAPIDocs() {
    if (!apiSpec || !apiSpec.paths) {
        showError(_t('apiDocs.errorSpecInvalid'));
        return;
    }
    
    // 显示认证说明
    renderAuthInfo();
    
    // 渲染侧边栏分组
    renderSidebar();
    
    // 渲染API端点
    renderEndpoints();
}

// 渲染认证说明
function renderAuthInfo() {
    const authSection = document.getElementById('auth-info-section');
    if (!authSection) return;
    
    // 显示认证说明部分
    authSection.style.display = 'block';
    
    // 检查是否有token
    const tokenStatus = document.getElementById('token-status');
    if (currentToken && tokenStatus) {
        tokenStatus.style.display = 'block';
    } else if (tokenStatus) {
        // 如果没有token，显示提示
        tokenStatus.style.display = 'block';
        tokenStatus.style.background = 'rgba(255, 152, 0, 0.1)';
        tokenStatus.style.borderLeftColor = '#ff9800';
        tokenStatus.innerHTML = '<p style="margin: 0; font-size: 0.8125rem; color: #ff9800;">' + escapeHtml(_t('apiDocs.tokenNotDetected')) + '</p>';
    }
}

// 渲染侧边栏
function renderSidebar() {
    const groups = new Set();
    Object.keys(apiSpec.paths).forEach(path => {
        Object.keys(apiSpec.paths[path]).forEach(method => {
            const endpoint = apiSpec.paths[path][method];
            if (endpoint.tags && endpoint.tags.length > 0) {
                endpoint.tags.forEach(tag => groups.add(tag));
            }
        });
    });
    
    const groupList = document.getElementById('api-group-list');
    const allGroups = Array.from(groups).sort();
    while (groupList.children.length > 1) {
        groupList.removeChild(groupList.lastChild);
    }
    allGroups.forEach(group => {
        const li = document.createElement('li');
        li.className = 'api-group-item';
        const groupLabel = translateApiDocTag(group);
        li.innerHTML = `<a href="#" class="api-group-link" data-group="${escapeAttr(group)}">${escapeHtml(groupLabel)}</a>`;
        groupList.appendChild(li);
    });
    
    // 绑定点击事件
    groupList.querySelectorAll('.api-group-link').forEach(link => {
        link.addEventListener('click', (e) => {
            e.preventDefault();
            groupList.querySelectorAll('.api-group-link').forEach(l => l.classList.remove('active'));
            link.classList.add('active');
            const group = link.dataset.group;
            renderEndpoints(group === 'all' ? null : group);
        });
    });
}

// 渲染API端点
function renderEndpoints(filterGroup = null) {
    const main = document.getElementById('api-docs-main');
    main.innerHTML = '';
    
    const endpoints = [];
    Object.keys(apiSpec.paths).forEach(path => {
        Object.keys(apiSpec.paths[path]).forEach(method => {
            const endpoint = apiSpec.paths[path][method];
            const tags = endpoint.tags || [];
            if (!filterGroup || filterGroup === 'all' || tags.includes(filterGroup)) {
                endpoints.push({
                    path,
                    method,
                    ...endpoint
                });
            }
        });
    });
    
    // 按分组排序
    endpoints.sort((a, b) => {
        const tagA = a.tags && a.tags.length > 0 ? a.tags[0] : '';
        const tagB = b.tags && b.tags.length > 0 ? b.tags[0] : '';
        if (tagA !== tagB) return tagA.localeCompare(tagB);
        return a.path.localeCompare(b.path);
    });
    
    if (endpoints.length === 0) {
        main.innerHTML = '<div class="empty-state"><h3>' + escapeHtml(_t('apiDocs.noApis')) + '</h3><p>' + escapeHtml(_t('apiDocs.noEndpointsInGroup')) + '</p></div>';
        return;
    }
    
    endpoints.forEach(endpoint => {
        main.appendChild(createEndpointCard(endpoint));
    });
}

// 创建API端点卡片
function createEndpointCard(endpoint) {
    const card = document.createElement('div');
    card.className = 'api-endpoint';
    
    const methodClass = endpoint.method.toLowerCase();
    const tags = endpoint.tags || [];
    const tagHtml = tags.map(tag => `<span class="api-tag">${escapeHtml(translateApiDocTag(tag))}</span>`).join('');
    const summaryText = translateApiDocSummaryFromOp(endpoint);
    card.innerHTML = `
        <div class="api-endpoint-header">
            <div class="api-endpoint-title">
                <span class="api-method ${methodClass}">${endpoint.method.toUpperCase()}</span>
                <span class="api-path">${escapeHtml(endpoint.path)}</span>
                ${tagHtml}
            </div>
        </div>
        <div class="api-endpoint-body">
            <div class="api-section">
                <div class="api-section-title">${escapeHtml(_t('apiDocs.sectionDescription'))}</div>
                ${summaryText ? `<div class="api-description" style="font-weight: 500; margin-bottom: 8px; color: var(--text-primary);">${escapeHtml(summaryText)}</div>` : ''}
                ${endpoint.description ? `
                    <div class="api-description-toggle">
                        <button class="description-toggle-btn" onclick="toggleDescription(this)">
                            <svg class="description-toggle-icon" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                <polyline points="6 9 12 15 18 9"/>
                            </svg>
                            <span>${escapeHtml(_t('apiDocs.viewDetailDesc'))}</span>
                        </button>
                        <div class="api-description-detail" style="display: none;">
                            ${formatDescription(endpoint.description)}
                        </div>
                    </div>
                ` : endpoint.summary ? '' : '<div class="api-description">' + escapeHtml(_t('apiDocs.noDescription')) + '</div>'}
            </div>
            
            ${renderParameters(endpoint)}
            ${renderRequestBody(endpoint)}
            ${renderResponses(endpoint)}
            ${renderTestSection(endpoint)}
        </div>
    `;
    
    return card;
}

// 渲染参数
function renderParameters(endpoint) {
    const params = endpoint.parameters || [];
    if (params.length === 0) return '';
    
    const requiredLabel = escapeHtml(_t('apiDocs.required'));
    const optionalLabel = escapeHtml(_t('apiDocs.optional'));
    const rows = params.map(param => {
            const required = param.required ? '<span class="api-param-required">' + requiredLabel + '</span>' : '<span class="api-param-optional">' + optionalLabel + '</span>';
        // 处理描述文本，将换行符转换为<br>
        let descriptionHtml = '-';
        if (param.description) {
            const escapedDesc = escapeHtml(param.description);
            descriptionHtml = escapedDesc.replace(/\n/g, '<br>');
        }
        
        return `
            <tr>
                <td><span class="api-param-name">${param.name}</span></td>
                <td><span class="api-param-type">${param.schema?.type || 'string'}</span></td>
                <td>${descriptionHtml}</td>
                <td>${required}</td>
            </tr>
        `;
    }).join('');
    
    const paramName = escapeHtml(_t('apiDocs.paramName'));
    const typeLabel = escapeHtml(_t('apiDocs.type'));
    const descLabel = escapeHtml(_t('apiDocs.description'));
    return `
        <div class="api-section">
            <div class="api-section-title">${escapeHtml(_t('apiDocs.sectionParams'))}</div>
            <div class="api-table-wrapper">
                <table class="api-params-table">
                    <thead>
                        <tr>
                            <th>${paramName}</th>
                            <th>${typeLabel}</th>
                            <th>${descLabel}</th>
                            <th>${requiredLabel}</th>
                        </tr>
                    </thead>
                    <tbody>
                        ${rows}
                    </tbody>
                </table>
            </div>
        </div>
    `;
}

// 渲染请求体
function renderRequestBody(endpoint) {
    if (!endpoint.requestBody) return '';
    
    const content = endpoint.requestBody.content || {};
    let schema = content['application/json']?.schema || {};
    
    // 处理 $ref 引用
    if (schema.$ref) {
        const refPath = schema.$ref.split('/');
        const refName = refPath[refPath.length - 1];
        if (apiSpec.components && apiSpec.components.schemas && apiSpec.components.schemas[refName]) {
            schema = apiSpec.components.schemas[refName];
        }
    }
    
    // 渲染参数表格
    let paramsTable = '';
    if (schema.properties) {
        const requiredFields = schema.required || [];
        const reqLabel = escapeHtml(_t('apiDocs.required'));
        const optLabel = escapeHtml(_t('apiDocs.optional'));
        const rows = Object.keys(schema.properties).map(key => {
            const prop = schema.properties[key];
            const required = requiredFields.includes(key) 
                ? '<span class="api-param-required">' + reqLabel + '</span>' 
                : '<span class="api-param-optional">' + optLabel + '</span>';
            
            // 处理嵌套类型
            let typeDisplay = prop.type || 'object';
            if (prop.type === 'array' && prop.items) {
                typeDisplay = `array[${prop.items.type || 'object'}]`;
            } else if (prop.$ref) {
                const refPath = prop.$ref.split('/');
                typeDisplay = refPath[refPath.length - 1];
            }
            
            // 处理枚举
            if (prop.enum) {
                typeDisplay += ` (${prop.enum.join(', ')})`;
            }
            
            // 处理描述文本，将换行符转换为<br>，但保持其他格式
            let descriptionHtml = '-';
            if (prop.description) {
                // 转义HTML，然后处理换行
                const escapedDesc = escapeHtml(prop.description);
                // 将 \n 转换为 <br>，但不要转换已经转义的换行
                descriptionHtml = escapedDesc.replace(/\n/g, '<br>');
            }
            
            return `
                <tr>
                    <td><span class="api-param-name">${escapeHtml(key)}</span></td>
                    <td><span class="api-param-type">${escapeHtml(typeDisplay)}</span></td>
                    <td>${descriptionHtml}</td>
                    <td>${required}</td>
                    <td>${prop.example !== undefined ? `<code>${escapeHtml(String(prop.example))}</code>` : '-'}</td>
                </tr>
            `;
        }).join('');
        
        if (rows) {
            const pName = escapeHtml(_t('apiDocs.paramName'));
            const tLabel = escapeHtml(_t('apiDocs.type'));
            const dLabel = escapeHtml(_t('apiDocs.description'));
            const exLabel = escapeHtml(_t('apiDocs.example'));
            paramsTable = `
                <div class="api-table-wrapper" style="margin-top: 12px;">
                    <table class="api-params-table">
                        <thead>
                            <tr>
                                <th>${pName}</th>
                                <th>${tLabel}</th>
                                <th>${dLabel}</th>
                                <th>${reqLabel}</th>
                                <th>${exLabel}</th>
                            </tr>
                        </thead>
                        <tbody>
                            ${rows}
                        </tbody>
                    </table>
                </div>
            `;
        }
    }
    
    // 生成示例JSON
    let example = '';
    if (schema.example) {
        example = JSON.stringify(schema.example, null, 2);
    } else if (schema.properties) {
        const exampleObj = {};
        Object.keys(schema.properties).forEach(key => {
            const prop = schema.properties[key];
            if (prop.example !== undefined) {
                exampleObj[key] = prop.example;
            } else {
                // 根据类型生成默认示例
                if (prop.type === 'string') {
                    exampleObj[key] = prop.description || 'string';
                } else if (prop.type === 'number') {
                    exampleObj[key] = 0;
                } else if (prop.type === 'boolean') {
                    exampleObj[key] = false;
                } else if (prop.type === 'array') {
                    exampleObj[key] = [];
                } else {
                    exampleObj[key] = null;
                }
            }
        });
        example = JSON.stringify(exampleObj, null, 2);
    }
    
    return `
        <div class="api-section">
            <div class="api-section-title">${escapeHtml(_t('apiDocs.sectionRequestBody'))}</div>
            ${endpoint.requestBody.description ? `<div class="api-description">${endpoint.requestBody.description}</div>` : ''}
            ${paramsTable}
            ${example ? `
                <div style="margin-top: 16px;">
                    <div style="font-weight: 500; margin-bottom: 8px; color: var(--text-primary);">${escapeHtml(_t('apiDocs.exampleJson'))}</div>
                    <div class="api-response-example">
                        <pre>${escapeHtml(example)}</pre>
                    </div>
                </div>
            ` : ''}
        </div>
    `;
}

// 渲染响应
function renderResponses(endpoint) {
    const responses = endpoint.responses || {};
    const responseItems = Object.keys(responses).map(status => {
        const response = responses[status];
        const schema = response.content?.['application/json']?.schema || {};
        let example = '';
        if (schema.example) {
            example = JSON.stringify(schema.example, null, 2);
        }
        const descText = translateApiDocResponseDescFromResp(response);
        return `
            <div style="margin-bottom: 16px;">
                <strong style="color: ${status.startsWith('2') ? 'var(--success-color)' : status.startsWith('4') ? 'var(--error-color)' : 'var(--warning-color)'}">${status}</strong>
                ${descText ? `<span style="color: var(--text-secondary); margin-left: 8px;">${escapeHtml(descText)}</span>` : ''}
                ${example ? `
                    <div class="api-response-example" style="margin-top: 8px;">
                        <pre>${escapeHtml(example)}</pre>
                    </div>
                ` : ''}
            </div>
        `;
    }).join('');
    
    if (!responseItems) return '';
    
    return `
        <div class="api-section">
            <div class="api-section-title">${escapeHtml(_t('apiDocs.sectionResponse'))}</div>
            ${responseItems}
        </div>
    `;
}

// 渲染测试区域
function renderTestSection(endpoint) {
    const method = endpoint.method.toUpperCase();
    const path = endpoint.path;
    const hasBody = endpoint.requestBody && ['POST', 'PUT', 'PATCH'].includes(method);
    
    let bodyInput = '';
    if (hasBody) {
        const schema = endpoint.requestBody.content?.['application/json']?.schema || {};
        let defaultBody = '';
        if (schema.example) {
            defaultBody = JSON.stringify(schema.example, null, 2);
        } else if (schema.properties) {
            const exampleObj = {};
            Object.keys(schema.properties).forEach(key => {
                const prop = schema.properties[key];
                exampleObj[key] = prop.example || (prop.type === 'string' ? '' : prop.type === 'number' ? 0 : prop.type === 'boolean' ? false : null);
            });
            defaultBody = JSON.stringify(exampleObj, null, 2);
        }
        
        const bodyInputId = `test-body-${escapeId(path)}-${method}`;
        bodyInput = `
            <div class="api-test-input-group">
                <label>${escapeHtml(_t('apiDocs.requestBodyJson'))}</label>
                <textarea id="${escapeAttr(bodyInputId)}" class="test-body-input" placeholder='${escapeAttr(_t('apiDocs.requestBodyPlaceholder'))}'>${escapeHtml(defaultBody)}</textarea>
            </div>
        `;
    }
    
    // 处理路径参数
    const pathParams = (endpoint.parameters || []).filter(p => p.in === 'path');
    let pathParamsInput = '';
    if (pathParams.length > 0) {
        pathParamsInput = pathParams.map(param => {
            const inputId = `test-param-${param.name}-${escapeId(path)}-${method}`;
            return `
                <div class="api-test-input-group">
                    <label>${escapeHtml(param.name)} <span style="color: var(--error-color);">*</span></label>
                    <input type="text" id="${escapeAttr(inputId)}" placeholder="${escapeAttr(param.description || param.name)}" required>
                </div>
            `;
        }).join('');
    }
    
    // 处理查询参数
    const queryParams = (endpoint.parameters || []).filter(p => p.in === 'query');
    let queryParamsInput = '';
    if (queryParams.length > 0) {
        queryParamsInput = queryParams.map(param => {
            const inputId = `test-query-${param.name}-${escapeId(path)}-${method}`;
            const defaultValue = param.schema?.default !== undefined ? param.schema.default : '';
            const placeholder = param.description || param.name;
            const required = param.required ? '<span style="color: var(--error-color);">*</span>' : '<span style="color: var(--text-muted);">' + escapeHtml(_t('apiDocs.optional')) + '</span>';
            return `
                <div class="api-test-input-group">
                    <label>${escapeHtml(param.name)} ${required}</label>
                    <input type="${param.schema?.type === 'number' || param.schema?.type === 'integer' ? 'number' : 'text'}" 
                           id="${escapeAttr(inputId)}" 
                           placeholder="${escapeAttr(placeholder)}" 
                           value="${escapeAttr(defaultValue)}"
                           ${param.required ? 'required' : ''}>
                </div>
            `;
        }).join('');
    }
    
    const testSectionTitle = escapeHtml(_t('apiDocs.testSection'));
    const queryParamsTitle = escapeHtml(_t('apiDocs.queryParams'));
    const sendRequestLabel = escapeHtml(_t('apiDocs.sendRequest'));
    const copyCurlLabel = escapeHtml(_t('apiDocs.copyCurl'));
    const clearResultLabel = escapeHtml(_t('apiDocs.clearResult'));
    const copyCurlTitle = escapeHtml(_t('apiDocs.copyCurlTitle'));
    const clearResultTitle = escapeHtml(_t('apiDocs.clearResultTitle'));
    return `
        <div class="api-test-section">
            <div class="api-section-title">${testSectionTitle}</div>
            <div class="api-test-form">
                ${pathParamsInput}
                ${queryParamsInput ? `<div style="margin-top: 16px;"><div style="font-weight: 500; margin-bottom: 8px; color: var(--text-primary);">${queryParamsTitle}</div>${queryParamsInput}</div>` : ''}
                ${bodyInput}
                <div class="api-test-buttons">
                    <button class="api-test-btn primary" onclick="testAPI(${escapeJsStringAttr(method)}, ${escapeJsStringAttr(path)}, ${escapeJsStringAttr(endpoint.operationId || '')})">
                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                            <polygon points="5 3 19 12 5 21 5 3"/>
                        </svg>
                        ${sendRequestLabel}
                    </button>
                    <button class="api-test-btn copy-curl" onclick="copyCurlCommand(event, ${escapeJsStringAttr(method)}, ${escapeJsStringAttr(path)})" title="${copyCurlTitle}">
                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                            <rect x="9" y="9" width="13" height="13" rx="2" ry="2" stroke="currentColor" stroke-width="2"/>
                            <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" stroke="currentColor" stroke-width="2"/>
                        </svg>
                        ${copyCurlLabel}
                    </button>
                    <button class="api-test-btn clear-result" onclick="clearTestResult('${escapeId(path)}-${method}')" title="${clearResultTitle}">
                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                            <polyline points="3 6 5 6 21 6"/>
                            <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>
                        </svg>
                        ${clearResultLabel}
                    </button>
                </div>
                <div id="test-result-${escapeId(path)}-${method}" class="api-test-result" style="display: none;"></div>
            </div>
        </div>
    `;
}

// 测试API
async function testAPI(method, path, operationId) {
    const resultId = `test-result-${escapeId(path)}-${method}`;
    const resultDiv = document.getElementById(resultId);
    if (!resultDiv) return;
    
    resultDiv.style.display = 'block';
    resultDiv.className = 'api-test-result loading';
    resultDiv.textContent = _t('apiDocs.sendingRequest');
    
    try {
        // 替换路径参数
        let actualPath = path;
        const pathParams = path.match(/\{([^}]+)\}/g) || [];
        pathParams.forEach(param => {
            const paramName = param.slice(1, -1);
            const inputId = `test-param-${paramName}-${escapeId(path)}-${method}`;
            const input = document.getElementById(inputId);
            if (input && input.value) {
                actualPath = actualPath.replace(param, encodeURIComponent(input.value));
            } else {
                throw new Error(_t('apiDocs.errorPathParamRequired', { name: paramName }));
            }
        });
        
        // 确保路径以/api开头（如果OpenAPI规范中的路径不包含/api）
        if (!actualPath.startsWith('/api') && !actualPath.startsWith('http')) {
            actualPath = '/api' + actualPath;
        }
        
        // 构建查询参数
        const queryParams = [];
        const endpointSpec = apiSpec.paths[path]?.[method.toLowerCase()];
        if (endpointSpec && endpointSpec.parameters) {
            endpointSpec.parameters.filter(p => p.in === 'query').forEach(param => {
                const inputId = `test-query-${param.name}-${escapeId(path)}-${method}`;
                const input = document.getElementById(inputId);
                if (input && input.value !== '' && input.value !== null && input.value !== undefined) {
                    queryParams.push(`${encodeURIComponent(param.name)}=${encodeURIComponent(input.value)}`);
                } else if (param.required) {
                    throw new Error(_t('apiDocs.errorQueryParamRequired', { name: param.name }));
                }
            });
        }
        
        // 添加查询字符串
        if (queryParams.length > 0) {
            actualPath += (actualPath.includes('?') ? '&' : '?') + queryParams.join('&');
        }
        
        // 构建请求选项
        const options = {
            method: method,
            headers: {
                'Content-Type': 'application/json',
            }
        };
        
        // 添加token
        if (currentToken) {
            options.headers['Authorization'] = 'Bearer ' + currentToken;
        } else {
            throw new Error(_t('apiDocs.errorTokenRequired'));
        }
        
        // 添加请求体
        if (['POST', 'PUT', 'PATCH'].includes(method)) {
            const bodyInputId = `test-body-${escapeId(path)}-${method}`;
            const bodyInput = document.getElementById(bodyInputId);
            if (bodyInput && bodyInput.value.trim()) {
                try {
                    options.body = JSON.stringify(JSON.parse(bodyInput.value.trim()));
                } catch (e) {
                    throw new Error(_t('apiDocs.errorJsonInvalid') + e.message);
                }
            }
        }
        
        // 发送请求
        const response = await fetch(actualPath, options);
        const responseText = await response.text();
        
        let responseData;
        try {
            responseData = JSON.parse(responseText);
        } catch {
            responseData = responseText;
        }
        
        // 显示结果
        resultDiv.className = response.ok ? 'api-test-result success' : 'api-test-result error';
        resultDiv.textContent = `${_t('apiDocs.statusCode')}${response.status} ${response.statusText}\n\n${typeof responseData === 'string' ? responseData : JSON.stringify(responseData, null, 2)}`;
        
    } catch (error) {
        resultDiv.className = 'api-test-result error';
        resultDiv.textContent = _t('apiDocs.requestFailed') + error.message;
    }
}

// 清除测试结果
function clearTestResult(id) {
    const resultDiv = document.getElementById(`test-result-${id}`);
    if (resultDiv) {
        resultDiv.style.display = 'none';
        resultDiv.textContent = '';
    }
}

// 复制curl命令
function copyCurlCommand(event, method, path) {
    try {
        // 替换路径参数
        let actualPath = path;
        const pathParams = path.match(/\{([^}]+)\}/g) || [];
        pathParams.forEach(param => {
            const paramName = param.slice(1, -1);
            const inputId = `test-param-${paramName}-${escapeId(path)}-${method}`;
            const input = document.getElementById(inputId);
            if (input && input.value) {
                actualPath = actualPath.replace(param, encodeURIComponent(input.value));
            }
        });
        
        // 确保路径以/api开头
        if (!actualPath.startsWith('/api') && !actualPath.startsWith('http')) {
            actualPath = '/api' + actualPath;
        }
        
        // 构建查询参数
        const queryParams = [];
        const endpointSpec = apiSpec.paths[path]?.[method.toLowerCase()];
        if (endpointSpec && endpointSpec.parameters) {
            endpointSpec.parameters.filter(p => p.in === 'query').forEach(param => {
                const inputId = `test-query-${param.name}-${escapeId(path)}-${method}`;
                const input = document.getElementById(inputId);
                if (input && input.value !== '' && input.value !== null && input.value !== undefined) {
                    queryParams.push(`${encodeURIComponent(param.name)}=${encodeURIComponent(input.value)}`);
                }
            });
        }
        
        // 添加查询字符串
        if (queryParams.length > 0) {
            actualPath += (actualPath.includes('?') ? '&' : '?') + queryParams.join('&');
        }
        
        // 构建完整的URL
        const baseUrl = window.location.origin;
        const fullUrl = baseUrl + actualPath;
        
        // 构建curl命令
        let curlCommand = `curl -X ${method.toUpperCase()} "${fullUrl}"`;
        
        // 添加请求头
        curlCommand += ` \\\n  -H "Content-Type: application/json"`;
        
        // 添加Authorization头
        if (currentToken) {
            curlCommand += ` \\\n  -H "Authorization: Bearer ${currentToken}"`;
        } else {
            curlCommand += ` \\\n  -H "Authorization: Bearer YOUR_TOKEN_HERE"`;
        }
        
        // 添加请求体（如果有）
        if (['POST', 'PUT', 'PATCH'].includes(method.toUpperCase())) {
            const bodyInputId = `test-body-${escapeId(path)}-${method}`;
            const bodyInput = document.getElementById(bodyInputId);
            if (bodyInput && bodyInput.value.trim()) {
                try {
                    // 验证JSON格式并格式化
                    const jsonBody = JSON.parse(bodyInput.value.trim());
                    const jsonString = JSON.stringify(jsonBody);
                    // 在单引号内，只需要转义单引号本身
                    const escapedJson = jsonString.replace(/'/g, "'\\''");
                    curlCommand += ` \\\n  -d '${escapedJson}'`;
                } catch (e) {
                    // 如果不是有效JSON，直接使用原始值
                    const escapedBody = bodyInput.value.trim().replace(/'/g, "'\\''");
                    curlCommand += ` \\\n  -d '${escapedBody}'`;
                }
            }
        }
        
        // 复制到剪贴板
        const button = event ? event.target.closest('button') : null;
        navigator.clipboard.writeText(curlCommand).then(() => {
            if (button) {
                const originalText = button.innerHTML;
                const copiedLabel = escapeHtml(_t('apiDocs.copied'));
                button.innerHTML = '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="20 6 9 17 4 12"/></svg>' + copiedLabel;
                button.style.color = 'var(--success-color)';
                setTimeout(() => {
                    button.innerHTML = originalText;
                    button.style.color = '';
                }, 2000);
            } else {
                alert(_t('apiDocs.curlCopied'));
            }
        }).catch(err => {
            console.error('复制失败:', err);
            // 如果clipboard API失败，使用fallback方法
            const textarea = document.createElement('textarea');
            textarea.value = curlCommand;
            textarea.style.position = 'fixed';
            textarea.style.opacity = '0';
            document.body.appendChild(textarea);
            textarea.select();
            try {
                document.execCommand('copy');
                if (button) {
                    const originalText = button.innerHTML;
                    const copiedLabel = escapeHtml(_t('apiDocs.copied'));
                    button.innerHTML = '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="20 6 9 17 4 12"/></svg>' + copiedLabel;
                    button.style.color = 'var(--success-color)';
                    setTimeout(() => {
                        button.innerHTML = originalText;
                        button.style.color = '';
                    }, 2000);
                } else {
                    alert(_t('apiDocs.curlCopied'));
                }
            } catch (e) {
                alert(_t('apiDocs.copyFailedManual') + curlCommand);
            }
            document.body.removeChild(textarea);
        });
        
    } catch (error) {
        console.error('生成curl命令失败:', error);
        alert(_t('apiDocs.curlGenFailed') + error.message);
    }
}

// 格式化描述文本（处理markdown格式）
function formatDescription(text) {
    if (!text) return '';
    
    // 先提取代码块（避免代码块内的markdown被处理）
    let formatted = text;
    const codeBlocks = [];
    let codeBlockIndex = 0;
    
    // 提取代码块（支持语言标识符，如 ```json 或 ```javascript）
    formatted = formatted.replace(/```(\w+)?\s*\n?([\s\S]*?)```/g, (match, lang, code) => {
        const placeholder = `__CODE_BLOCK_${codeBlockIndex}__`;
        codeBlocks[codeBlockIndex] = {
            lang: (lang && lang.trim()) || '',
            code: code.trim()
        };
        codeBlockIndex++;
        return placeholder;
    });
    
    // 提取行内代码（避免行内代码内的markdown被处理）
    const inlineCodes = [];
    let inlineCodeIndex = 0;
    formatted = formatted.replace(/`([^`\n]+)`/g, (match, code) => {
        const placeholder = `__INLINE_CODE_${inlineCodeIndex}__`;
        inlineCodes[inlineCodeIndex] = code;
        inlineCodeIndex++;
        return placeholder;
    });
    
    // 转义HTML（但保留占位符）
    formatted = escapeHtml(formatted);
    
    // 恢复行内代码（需要转义，因为占位符已经被转义了）
    inlineCodes.forEach((code, index) => {
        formatted = formatted.replace(
            `__INLINE_CODE_${index}__`,
            `<code class="inline-code">${escapeHtml(code)}</code>`
        );
    });
    
    // 恢复代码块（代码块内容已经转义过，直接使用）
    codeBlocks.forEach((block, index) => {
        const langLabel = block.lang ? `<span class="code-lang">${escapeHtml(block.lang)}</span>` : '';
        // 代码块内容已经在提取时保存，不需要再次转义
        formatted = formatted.replace(
            `__CODE_BLOCK_${index}__`,
            `<pre class="code-block">${langLabel}<code>${escapeHtml(block.code)}</code></pre>`
        );
    });
    
    // 处理标题（### 标题）
    formatted = formatted.replace(/^###\s+(.+)$/gm, '<h3 class="md-h3">$1</h3>');
    formatted = formatted.replace(/^##\s+(.+)$/gm, '<h2 class="md-h2">$1</h2>');
    formatted = formatted.replace(/^#\s+(.+)$/gm, '<h1 class="md-h1">$1</h1>');
    
    // 处理加粗文本（**text** 或 __text__）
    formatted = formatted.replace(/\*\*([^*]+?)\*\*/g, '<strong>$1</strong>');
    formatted = formatted.replace(/__([^_]+?)__/g, '<strong>$1</strong>');
    
    // 处理斜体（*text* 或 _text_，但不与加粗冲突）
    formatted = formatted.replace(/(?<!\*)\*([^*\n]+?)\*(?!\*)/g, '<em>$1</em>');
    formatted = formatted.replace(/(?<!_)_([^_\n]+?)_(?!_)/g, '<em>$1</em>');
    
    // 处理链接 [text](url)
    formatted = formatted.replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" target="_blank" rel="noopener noreferrer" class="md-link">$1</a>');
    
    // 处理列表项（有序和无序）
    const lines = formatted.split('\n');
    const result = [];
    let inUnorderedList = false;
    let inOrderedList = false;
    let orderedListStart = 1;
    
    for (let i = 0; i < lines.length; i++) {
        const line = lines[i];
        const unorderedMatch = line.match(/^[-*]\s+(.+)$/);
        const orderedMatch = line.match(/^\d+\.\s+(.+)$/);
        
        if (unorderedMatch) {
            if (inOrderedList) {
                result.push('</ol>');
                inOrderedList = false;
            }
            if (!inUnorderedList) {
                result.push('<ul class="md-list">');
                inUnorderedList = true;
            }
            result.push(`<li class="md-list-item">${unorderedMatch[1]}</li>`);
        } else if (orderedMatch) {
            if (inUnorderedList) {
                result.push('</ul>');
                inUnorderedList = false;
            }
            if (!inOrderedList) {
                result.push('<ol class="md-list">');
                inOrderedList = true;
                orderedListStart = parseInt(line.match(/^(\d+)\./)[1]) || 1;
            }
            result.push(`<li class="md-list-item">${orderedMatch[1]}</li>`);
        } else {
            if (inUnorderedList) {
                result.push('</ul>');
                inUnorderedList = false;
            }
            if (inOrderedList) {
                result.push('</ol>');
                inOrderedList = false;
            }
            if (line.trim()) {
                result.push(line);
            } else if (i < lines.length - 1) {
                // 只在非最后一行时添加换行
                result.push('<br>');
            }
        }
    }
    
    if (inUnorderedList) {
        result.push('</ul>');
    }
    if (inOrderedList) {
        result.push('</ol>');
    }
    
    formatted = result.join('\n');
    
    // 处理段落（连续的空行分隔段落）
    formatted = formatted.replace(/(<br>\s*){2,}/g, '</p><p class="md-paragraph">');
    formatted = '<p class="md-paragraph">' + formatted + '</p>';
    
    // 清理多余的<br>标签（在块级元素前后）
    formatted = formatted.replace(/(<\/?(h[1-6]|ul|ol|li|pre|p)[^>]*>)\s*<br>/gi, '$1');
    formatted = formatted.replace(/<br>\s*(<\/?(h[1-6]|ul|ol|li|pre|p)[^>]*>)/gi, '$1');
    
    // 将剩余的单个换行符转换为<br>（但避免在块级元素内）
    formatted = formatted.replace(/\n(?!<\/?(h[1-6]|ul|ol|li|pre|p|code))/g, '<br>');
    
    return formatted;
}

// HTML转义
function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

function escapeJsString(text) {
    return JSON.stringify(String(text == null ? '' : text));
}

function escapeAttr(text) {
    return escapeHtml(text).replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

function escapeJsStringAttr(text) {
    return escapeAttr(escapeJsString(text));
}

// ID转义（用于HTML ID属性）
function escapeId(text) {
    return text.replace(/[{}]/g, '').replace(/\//g, '-');
}

// 切换描述显示/隐藏
function toggleDescription(button) {
    const icon = button.querySelector('.description-toggle-icon');
    const detail = button.parentElement.querySelector('.api-description-detail');
    const span = button.querySelector('span');
    
    if (detail.style.display === 'none') {
        detail.style.display = 'block';
        icon.style.transform = 'rotate(180deg)';
        span.textContent = typeof window.t === 'function' ? window.t('apiDocs.hideDetailDesc') : '隐藏详细说明';
    } else {
        detail.style.display = 'none';
        icon.style.transform = 'rotate(0deg)';
        span.textContent = typeof window.t === 'function' ? window.t('apiDocs.viewDetailDesc') : '查看详细说明';
    }
}
