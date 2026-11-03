// API documentation page JavaScript

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

// Build tag -> i18n key mapping from the OpenAPI spec's x-i18n-tags (Option A: backend provides keys)
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

// Initialize
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

// Load token
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
        console.error('Failed to load token:', e);
    }
}

// Load OpenAPI specification
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
        console.error('Failed to load API specification:', error);
        showError(_t('apiDocs.errorLoadFailed') + error.message);
    }
}

// Display error
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

// Render API documentation
function renderAPIDocs() {
    if (!apiSpec || !apiSpec.paths) {
        showError(_t('apiDocs.errorSpecInvalid'));
        return;
    }
    
    // Display authentication description
    renderAuthInfo();
    
    // Render sidebar group
    renderSidebar();
    
    // Render API endpoint
    renderEndpoints();
}

// Render authentication description
function renderAuthInfo() {
    const authSection = document.getElementById('auth-info-section');
    if (!authSection) return;
    
    // Display authentication instructions section
    authSection.style.display = 'block';
    
    // Check if there is a token
    const tokenStatus = document.getElementById('token-status');
    if (currentToken && tokenStatus) {
        tokenStatus.style.display = 'block';
    } else if (tokenStatus) {
        // If there is no token, display a prompt
        tokenStatus.style.display = 'block';
        tokenStatus.style.background = 'rgba(255, 152, 0, 0.1)';
        tokenStatus.style.borderLeftColor = '#ff9800';
        tokenStatus.innerHTML = '<p style="margin: 0; font-size: 0.8125rem; color: #ff9800;">' + escapeHtml(_t('apiDocs.tokenNotDetected')) + '</p>';
    }
}

// Render sidebar
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
    
    // Bind click event
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

// Render API endpoint
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
    
    // Sort by group
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

// Create API endpoint card
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

// Rendering parameters
function renderParameters(endpoint) {
    const params = endpoint.parameters || [];
    if (params.length === 0) return '';
    
    const requiredLabel = escapeHtml(_t('apiDocs.required'));
    const optionalLabel = escapeHtml(_t('apiDocs.optional'));
    const rows = params.map(param => {
            const required = param.required ? '<span class="api-param-required">' + requiredLabel + '</span>' : '<span class="api-param-optional">' + optionalLabel + '</span>';
        // Process the description text, converting newline characters to <br>
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

// Render request body
function renderRequestBody(endpoint) {
    if (!endpoint.requestBody) return '';
    
    const content = endpoint.requestBody.content || {};
    let schema = content['application/json']?.schema || {};
    
    // Handle $ref references
    if (schema.$ref) {
        const refPath = schema.$ref.split('/');
        const refName = refPath[refPath.length - 1];
        if (apiSpec.components && apiSpec.components.schemas && apiSpec.components.schemas[refName]) {
            schema = apiSpec.components.schemas[refName];
        }
    }
    
    // Render parameter table
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
            
            // Handle nested types
            let typeDisplay = prop.type || 'object';
            if (prop.type === 'array' && prop.items) {
                typeDisplay = `array[${prop.items.type || 'object'}]`;
            } else if (prop.$ref) {
                const refPath = prop.$ref.split('/');
                typeDisplay = refPath[refPath.length - 1];
            }
            
            // Process enumeration
            if (prop.enum) {
                typeDisplay += ` (${prop.enum.join(', ')})`;
            }
            
            // Process description text, converting newline characters to <br>, but preserve other formatting
            let descriptionHtml = '-';
            if (prop.description) {
                // Escape HTML, then process line breaks
                const escapedDesc = escapeHtml(prop.description);
                // Convert \n to <br>, but do not convert already escaped newlines
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
    
    // Generate example JSON
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
                // Generate default example based on type
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

// render response
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

// Render test area
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
    
    // Handle path parameters
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
    
    // Process query parameters
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

// Test API
async function testAPI(method, path, operationId) {
    const resultId = `test-result-${escapeId(path)}-${method}`;
    const resultDiv = document.getElementById(resultId);
    if (!resultDiv) return;
    
    resultDiv.style.display = 'block';
    resultDiv.className = 'api-test-result loading';
    resultDiv.textContent = _t('apiDocs.sendingRequest');
    
    try {
        // Replace path parameter
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
        
        // Ensure the path starts with /api (if the path in the OpenAPI spec doesn't include /api)
        if (!actualPath.startsWith('/api') && !actualPath.startsWith('http')) {
            actualPath = '/api' + actualPath;
        }
        
        // Build query parameters
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
        
        // Add query string
        if (queryParams.length > 0) {
            actualPath += (actualPath.includes('?') ? '&' : '?') + queryParams.join('&');
        }
        
        // Build request options
        const options = {
            method: method,
            headers: {
                'Content-Type': 'application/json',
            }
        };
        
        // Add token
        if (currentToken) {
            options.headers['Authorization'] = 'Bearer ' + currentToken;
        } else {
            throw new Error(_t('apiDocs.errorTokenRequired'));
        }
        
        // Add request body
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
        
        // Send request
        const response = await fetch(actualPath, options);
        const responseText = await response.text();
        
        let responseData;
        try {
            responseData = JSON.parse(responseText);
        } catch {
            responseData = responseText;
        }
        
        // Display result
        resultDiv.className = response.ok ? 'api-test-result success' : 'api-test-result error';
        resultDiv.textContent = `${_t('apiDocs.statusCode')}${response.status} ${response.statusText}\n\n${typeof responseData === 'string' ? responseData : JSON.stringify(responseData, null, 2)}`;
        
    } catch (error) {
        resultDiv.className = 'api-test-result error';
        resultDiv.textContent = _t('apiDocs.requestFailed') + error.message;
    }
}

// Clear test results
function clearTestResult(id) {
    const resultDiv = document.getElementById(`test-result-${id}`);
    if (resultDiv) {
        resultDiv.style.display = 'none';
        resultDiv.textContent = '';
    }
}

// Copy curl command
function copyCurlCommand(event, method, path) {
    try {
        // Replace path parameter
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
        
        // Ensure the path starts with /api
        if (!actualPath.startsWith('/api') && !actualPath.startsWith('http')) {
            actualPath = '/api' + actualPath;
        }
        
        // Build query parameters
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
        
        // Add query string
        if (queryParams.length > 0) {
            actualPath += (actualPath.includes('?') ? '&' : '?') + queryParams.join('&');
        }
        
        // Build the full URL
        const baseUrl = window.location.origin;
        const fullUrl = baseUrl + actualPath;
        
        // Build curl command
        let curlCommand = `curl -X ${method.toUpperCase()} "${fullUrl}"`;
        
        // Add request header
        curlCommand += ` \\\n  -H "Content-Type: application/json"`;
        
        // Add Authorization header
        if (currentToken) {
            curlCommand += ` \\\n  -H "Authorization: Bearer ${currentToken}"`;
        } else {
            curlCommand += ` \\\n  -H "Authorization: Bearer YOUR_TOKEN_HERE"`;
        }
        
        // Add request body (if any)
        if (['POST', 'PUT', 'PATCH'].includes(method.toUpperCase())) {
            const bodyInputId = `test-body-${escapeId(path)}-${method}`;
            const bodyInput = document.getElementById(bodyInputId);
            if (bodyInput && bodyInput.value.trim()) {
                try {
                    // Validate JSON format and format it
                    const jsonBody = JSON.parse(bodyInput.value.trim());
                    const jsonString = JSON.stringify(jsonBody);
                    // Inside single quotes, you only need to escape the single quote itself
                    const escapedJson = jsonString.replace(/'/g, "'\\''");
                    curlCommand += ` \\\n  -d '${escapedJson}'`;
                } catch (e) {
                    // If not valid JSON, use the original value directly
                    const escapedBody = bodyInput.value.trim().replace(/'/g, "'\\''");
                    curlCommand += ` \\\n  -d '${escapedBody}'`;
                }
            }
        }
        
        // Copy to clipboard
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
            console.error('Copy failed:', err);
            // If the clipboard API fails, use the fallback method
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
        console.error('Failed to generate curl command:', error);
        alert(_t('apiDocs.curlGenFailed') + error.message);
    }
}

// Format description text (handle markdown format)
function formatDescription(text) {
    if (!text) return '';
    
    // First extract code blocks (to prevent markdown inside code blocks from being processed)
    let formatted = text;
    const codeBlocks = [];
    let codeBlockIndex = 0;
    
    // Extract code blocks (supports language identifiers, such as ```json or ```javascript)
    formatted = formatted.replace(/```(\w+)?\s*\n?([\s\S]*?)```/g, (match, lang, code) => {
        const placeholder = `__CODE_BLOCK_${codeBlockIndex}__`;
        codeBlocks[codeBlockIndex] = {
            lang: (lang && lang.trim()) || '',
            code: code.trim()
        };
        codeBlockIndex++;
        return placeholder;
    });
    
    // Extract inline code (to avoid markdown inside inline code being processed)
    const inlineCodes = [];
    let inlineCodeIndex = 0;
    formatted = formatted.replace(/`([^`\n]+)`/g, (match, code) => {
        const placeholder = `__INLINE_CODE_${inlineCodeIndex}__`;
        inlineCodes[inlineCodeIndex] = code;
        inlineCodeIndex++;
        return placeholder;
    });
    
    // Escape HTML (but preserve placeholders)
    formatted = escapeHtml(formatted);
    
    // Restore inline code (needs escaping, because the placeholder has already been escaped)
    inlineCodes.forEach((code, index) => {
        formatted = formatted.replace(
            `__INLINE_CODE_${index}__`,
            `<code class="inline-code">${escapeHtml(code)}</code>`
        );
    });
    
    // Restore code block (code block content has already been escaped, use it directly)
    codeBlocks.forEach((block, index) => {
        const langLabel = block.lang ? `<span class="code-lang">${escapeHtml(block.lang)}</span>` : '';
        // Code block content has already been saved during extraction, no need to escape again
        formatted = formatted.replace(
            `__CODE_BLOCK_${index}__`,
            `<pre class="code-block">${langLabel}<code>${escapeHtml(block.code)}</code></pre>`
        );
    });
    
    // Process title (### title)
    formatted = formatted.replace(/^###\s+(.+)$/gm, '<h3 class="md-h3">$1</h3>');
    formatted = formatted.replace(/^##\s+(.+)$/gm, '<h2 class="md-h2">$1</h2>');
    formatted = formatted.replace(/^#\s+(.+)$/gm, '<h1 class="md-h1">$1</h1>');
    
    // Handle bold text (**text** or __text__)
    formatted = formatted.replace(/\*\*([^*]+?)\*\*/g, '<strong>$1</strong>');
    formatted = formatted.replace(/__([^_]+?)__/g, '<strong>$1</strong>');
    
    // Handle italic (*text* or _text_, but not conflicting with bold)
    formatted = formatted.replace(/(?<!\*)\*([^*\n]+?)\*(?!\*)/g, '<em>$1</em>');
    formatted = formatted.replace(/(?<!_)_([^_\n]+?)_(?!_)/g, '<em>$1</em>');
    
    // Process link [text](url)
    formatted = formatted.replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" target="_blank" rel="noopener noreferrer" class="md-link">$1</a>');
    
    // Handle list items (ordered and unordered)
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
                // Only add a newline if it is not the last line
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
    
    // Process paragraphs (consecutive blank lines separate paragraphs)
    formatted = formatted.replace(/(<br>\s*){2,}/g, '</p><p class="md-paragraph">');
    formatted = '<p class="md-paragraph">' + formatted + '</p>';
    
    // Clean up redundant <br> tags (around block-level elements)
    formatted = formatted.replace(/(<\/?(h[1-6]|ul|ol|li|pre|p)[^>]*>)\s*<br>/gi, '$1');
    formatted = formatted.replace(/<br>\s*(<\/?(h[1-6]|ul|ol|li|pre|p)[^>]*>)/gi, '$1');
    
    // Convert remaining single newlines to <br> (but avoid inside block-level elements)
    formatted = formatted.replace(/\n(?!<\/?(h[1-6]|ul|ol|li|pre|p|code))/g, '<br>');
    
    return formatted;
}

// HTML escaping
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

// ID escaping (for HTML ID attribute)
function escapeId(text) {
    return text.replace(/[{}]/g, '').replace(/\//g, '-');
}

// Toggle description show/hide
function toggleDescription(button) {
    const icon = button.querySelector('.description-toggle-icon');
    const detail = button.parentElement.querySelector('.api-description-detail');
    const span = button.querySelector('span');
    
    if (detail.style.display === 'none') {
        detail.style.display = 'block';
        icon.style.transform = 'rotate(180deg)';
        span.textContent = typeof window.t === 'function' ? window.t('apiDocs.hideDetailDesc') : 'Hide detailed description';
    } else {
        detail.style.display = 'none';
        icon.style.transform = 'rotate(0deg)';
        span.textContent = typeof window.t === 'function' ? window.t('apiDocs.viewDetailDesc') : 'View details';
    }
}
