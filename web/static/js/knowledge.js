function knowledgeIndexErrorAdvice(error) {
    const text = String(error || '');
    if (/\b401\b|incorrect api key|invalid api key|unauthorized/i.test(text)) return knowledgeT('knowledge.embeddingAuthRejected', '嵌入服务拒绝认证。请检查知识库嵌入配置的 API 密钥及服务地址，保存后重试。');
    if (/\b403\b|forbidden/i.test(text)) return knowledgeT('knowledge.embeddingForbidden', '嵌入服务拒绝访问。请检查账号权限及所选嵌入模型的访问权限。');
    if (/\b429\b|rate limit|quota/i.test(text)) return knowledgeT('knowledge.embeddingRateLimited', '嵌入服务限制请求。请检查请求频率及账号可用额度，稍后重试。');
    if (/timeout|deadline exceeded/i.test(text)) return knowledgeT('knowledge.embeddingTimeout', '嵌入服务请求超时。请检查服务可用性、网络和代理配置后重试。');
    if (/connection refused|no such host|network is unreachable/i.test(text)) return knowledgeT('knowledge.embeddingUnreachable', '无法连接嵌入服务。请检查服务地址、DNS、网络和代理配置。');
    return knowledgeT('knowledge.embeddingCheckConfig', '请根据上方错误检查嵌入模型和服务配置，修正后重试。');
}

// 知识库管理相关功能
function _t(key, opts) {
    return typeof window.t === 'function' ? window.t(key, opts) : key;
}

// 与 _t 同用途，但保留中文原文作为未初始化时的后备值
function knowledgeT(key, fallback) {
    if (typeof window === 'object' && window && typeof window.t === 'function') {
        const translated = window.t(key);
        if (translated && translated !== key) return translated;
    }
    return fallback;
}

// 返回「知识库未启用」提示区块的 HTML（使用 data-i18n 以便语言切换时自动更新）
function getKnowledgeNotEnabledHTML() {
    return `
        <div class="empty-state" style="text-align: center; padding: 40px 20px;">
            <div style="font-size: 48px; margin-bottom: 20px;">📚</div>
            <h3 data-i18n="knowledge.notEnabledTitle" style="margin-bottom: 10px; color: #666;"></h3>
            <p data-i18n="knowledge.notEnabledHint" style="color: #999; margin-bottom: 20px;"></p>
            <button data-i18n="knowledge.goToSettings" onclick="switchToSettings()" style="
                background: #007bff;
                color: white;
                border: none;
                padding: 10px 20px;
                border-radius: 5px;
                cursor: pointer;
                font-size: 14px;
            "></button>
        </div>
    `;
}

// 渲染「知识库未启用」状态到容器，并应用当前语言
function renderKnowledgeNotEnabledState(container) {
    if (!container) return;
    container.innerHTML = getKnowledgeNotEnabledHTML();
    if (typeof window.applyTranslations === 'function') {
        window.applyTranslations(container);
    }
}

let knowledgeCategories = [];
let knowledgeItems = [];
let currentEditingItemId = null;
let isSavingKnowledgeItem = false; // 防止重复提交
let retrievalLogsData = []; // 存储检索日志数据，用于详情查看
let knowledgePagination = {
    currentPage: 1,
    pageSize: 10, // 每页分类数（改为按分类分页）
    total: 0,
    currentCategory: ''
};
let knowledgeSearchTimeout = null; // 搜索防抖定时器

// 加载知识分类
async function loadKnowledgeCategories() {
    try {
        // 添加时间戳参数避免缓存
        const timestamp = Date.now();
        const response = await apiFetch(`/api/knowledge/categories?_t=${timestamp}`, {
            method: 'GET',
            headers: {
                'Cache-Control': 'no-cache, no-store, must-revalidate',
                'Pragma': 'no-cache',
                'Expires': '0'
            }
        });
        if (!response.ok) {
            throw new Error(knowledgeT('knowledge.getCategoriesFailed', '获取分类失败'));
        }
        const data = await response.json();
        
        // 检查知识库功能是否启用
        if (data.enabled === false) {
            // 功能未启用，显示友好提示（使用 data-i18n，切换语言时会自动更新）
            renderKnowledgeNotEnabledState(document.getElementById('knowledge-items-list'));
            return [];
        }
        
        knowledgeCategories = data.categories || [];
        
        // 更新分类筛选下拉框
        const filterDropdown = document.getElementById('knowledge-category-filter-dropdown');
        if (filterDropdown) {
            filterDropdown.innerHTML = '<div class="custom-select-option" data-value="" onclick="selectKnowledgeCategory(\'\')">' + knowledgeT('knowledgePage.all', '全部') + '</div>';
            knowledgeCategories.forEach(category => {
                const option = document.createElement('div');
                option.className = 'custom-select-option';
                option.setAttribute('data-value', category);
                option.textContent = category;
                option.onclick = function() {
                    selectKnowledgeCategory(category);
                };
                filterDropdown.appendChild(option);
            });
        }
        
        return knowledgeCategories;
    } catch (error) {
        console.error('加载分类失败:', error);
        // 只在非功能未启用的情况下显示错误
        if (!/(知识库功能未启用|knowledge base feature is not enabled)/i.test(error.message)) {
            showNotification(knowledgeT('knowledge.loadCategoriesFailed', '加载分类失败') + ': ' + error.message, 'error');
        }
        return [];
    }
}

// 加载知识项列表（支持按分类分页，默认不加载完整内容）
async function loadKnowledgeItems(category = '', page = 1, pageSize = 10) {
    try {
        // 更新分页状态
        knowledgePagination.currentCategory = category;
        knowledgePagination.currentPage = page;
        knowledgePagination.pageSize = pageSize;
        
        // 构建URL（按分类分页模式，不包含完整内容）
        const timestamp = Date.now();
        const offset = (page - 1) * pageSize;
        let url = `/api/knowledge/items?categoryPage=true&limit=${pageSize}&offset=${offset}&_t=${timestamp}`;
        if (category) {
            url += `&category=${encodeURIComponent(category)}`;
        }
        
        const response = await apiFetch(url, {
            method: 'GET',
            headers: {
                'Cache-Control': 'no-cache, no-store, must-revalidate',
                'Pragma': 'no-cache',
                'Expires': '0'
            }
        });
        
        if (!response.ok) {
            throw new Error(knowledgeT('knowledge.getItemsFailed', '获取知识项失败'));
        }
        const data = await response.json();

        // 检查知识库功能是否启用
        if (data.enabled === false) {
            // 功能未启用，显示友好提示（如果还没有显示的话；使用 data-i18n，切换语言时会自动更新）
            const container = document.getElementById('knowledge-items-list');
            if (container && !container.querySelector('.empty-state')) {
                renderKnowledgeNotEnabledState(container);
            }
            knowledgeItems = [];
            knowledgePagination.total = 0;
            renderKnowledgePagination();
            return [];
        }
        
        // 处理按分类分页的响应数据
        const categoriesWithItems = data.categories || [];
        knowledgePagination.total = data.total || 0; // 总分类数
        
        renderKnowledgeItemsByCategories(categoriesWithItems);
        
        // 如果选择了单个分类，不显示分页（因为只显示一个分类）
        if (category) {
            const paginationContainer = document.getElementById('knowledge-pagination');
            if (paginationContainer) {
                paginationContainer.innerHTML = '';
            }
        } else {
            renderKnowledgePagination();
        }
        return categoriesWithItems;
    } catch (error) {
        console.error('加载知识项失败:', error);
        // 只在非功能未启用的情况下显示错误
        if (!/(知识库功能未启用|knowledge base feature is not enabled)/i.test(error.message)) {
            showNotification(knowledgeT('knowledge.loadItemsFailed', '加载知识项失败') + ': ' + error.message, 'error');
        }
        return [];
    }
}

// 渲染知识项列表（按分类分页的数据结构）
function renderKnowledgeItemsByCategories(categoriesWithItems) {
    const container = document.getElementById('knowledge-items-list');
    if (!container) return;
    
    if (categoriesWithItems.length === 0) {
        container.innerHTML = '<div class="empty-state">' + knowledgeT('knowledge.noItems', '暂无知识项') + '</div>';
        return;
    }
    
    // 计算总项数和分类数
    const totalItems = categoriesWithItems.reduce((sum, cat) => sum + (cat.items?.length || 0), 0);
    const categoryCount = categoriesWithItems.length;
    
    // 更新统计信息
    updateKnowledgeStats(categoriesWithItems, categoryCount);
    
    // 渲染分类及知识项
    let html = '<div class="knowledge-categories-container">';
    
    categoriesWithItems.forEach(categoryData => {
        const category = categoryData.category || knowledgeT('knowledge.uncategorized', '未分类');
        const categoryItems = categoryData.items || [];
        const categoryCount = categoryData.itemCount || categoryItems.length;
        
        html += `
            <div class="knowledge-category-section" data-category="${escapeAttr(category)}">
                <div class="knowledge-category-header">
                    <div class="knowledge-category-info">
                        <h3 class="knowledge-category-title">${escapeHtml(category)}</h3>
                        <span class="knowledge-category-count">${knowledgeT('knowledge.itemsCountUnit', '{count} 项').replace('{count}', String(categoryCount))}</span>
                    </div>
                </div>
                <div class="knowledge-items-grid">
                    ${categoryItems.map(item => renderKnowledgeItemCard(item)).join('')}
                </div>
            </div>
        `;
    });
    
    html += '</div>';
    container.innerHTML = html;
    if (typeof rbacAfterDynamicRender === 'function') rbacAfterDynamicRender(container);
}

// 渲染知识项列表（向后兼容，用于按项分页的旧代码）
function renderKnowledgeItems(items) {
    const container = document.getElementById('knowledge-items-list');
    if (!container) return;
    
    if (items.length === 0) {
        container.innerHTML = '<div class="empty-state">' + knowledgeT('knowledge.noItems', '暂无知识项') + '</div>';
        return;
    }
    
    // 按分类分组
    const groupedByCategory = {};
    items.forEach(item => {
        const category = item.category || knowledgeT('knowledge.uncategorized', '未分类');
        if (!groupedByCategory[category]) {
            groupedByCategory[category] = [];
        }
        groupedByCategory[category].push(item);
    });
    
    // 更新统计信息
    updateKnowledgeStats(items, Object.keys(groupedByCategory).length);
    
    // 渲染分组后的内容
    const categories = Object.keys(groupedByCategory).sort();
    let html = '<div class="knowledge-categories-container">';
    
    categories.forEach(category => {
        const categoryItems = groupedByCategory[category];
        const categoryCount = categoryItems.length;
        
        html += `
            <div class="knowledge-category-section" data-category="${escapeAttr(category)}">
                <div class="knowledge-category-header">
                    <div class="knowledge-category-info">
                        <h3 class="knowledge-category-title">${escapeHtml(category)}</h3>
                        <span class="knowledge-category-count">${knowledgeT('knowledge.itemsCountUnit', '{count} 项').replace('{count}', String(categoryCount))}</span>
                    </div>
                </div>
                <div class="knowledge-items-grid">
                    ${categoryItems.map(item => renderKnowledgeItemCard(item)).join('')}
                </div>
            </div>
        `;
    });
    
    html += '</div>';
    container.innerHTML = html;
    if (typeof rbacAfterDynamicRender === 'function') rbacAfterDynamicRender(container);
}

// 渲染分页控件（按分类分页）
function renderKnowledgePagination() {
    const container = document.getElementById('knowledge-pagination');
    if (!container) return;
    
    const { currentPage, pageSize, total } = knowledgePagination;
    const totalPages = Math.ceil(total / pageSize); // total是总分类数
    
    if (totalPages <= 1) {
        container.innerHTML = '';
        return;
    }
    
    let html = '<div class="knowledge-pagination" style="display: flex; justify-content: center; align-items: center; gap: 8px; padding: 20px; flex-wrap: wrap;">';
    
    // 上一页按钮
    html += `<button class="pagination-btn" onclick="loadKnowledgePage(${currentPage - 1})" ${currentPage <= 1 ? 'disabled style="opacity: 0.5; cursor: not-allowed;"' : ''}>${knowledgeT('knowledge.prevPage', '上一页')}</button>`;

    // 页码显示（显示分类数）
    html += `<span style="padding: 0 12px;">${knowledgeT('knowledge.paginationInfo', '第 {page} 页，共 {totalPages} 页（共 {total} 个分类）').replace('{page}', String(currentPage)).replace('{totalPages}', String(totalPages)).replace('{total}', String(total))}</span>`;

    // 下一页按钮
    html += `<button class="pagination-btn" onclick="loadKnowledgePage(${currentPage + 1})" ${currentPage >= totalPages ? 'disabled style="opacity: 0.5; cursor: not-allowed;"' : ''}>${knowledgeT('knowledge.nextPage', '下一页')}</button>`;
    
    html += '</div>';
    container.innerHTML = html;
}

// 加载指定页码的知识项
function loadKnowledgePage(page) {
    const { currentCategory, pageSize, total } = knowledgePagination;
    const totalPages = Math.ceil(total / pageSize);
    
    if (page < 1 || page > totalPages) {
        return;
    }
    
    loadKnowledgeItems(currentCategory, page, pageSize);
}

// 渲染单个知识项卡片
function renderKnowledgeItemCard(item) {
    // 提取内容预览（如果item没有content字段，说明是摘要，不显示预览）
    let previewText = '';
    if (item.content) {
        // 去除markdown格式，取前150字符
        let preview = item.content;
        // 移除markdown标题标记
        preview = preview.replace(/^#+\s+/gm, '');
        // 移除代码块
        preview = preview.replace(/```[\s\S]*?```/g, '');
        // 移除行内代码
        preview = preview.replace(/`[^`]+`/g, '');
        // 移除链接
        preview = preview.replace(/\[([^\]]+)\]\([^\)]+\)/g, '$1');
        // 清理多余空白
        preview = preview.replace(/\n+/g, ' ').replace(/\s+/g, ' ').trim();
        
        previewText = preview.length > 150 ? preview.substring(0, 150) + '...' : preview;
    }
    
    // 提取文件路径显示
    const filePath = item.filePath || '';
    const relativePath = filePath.split(/[/\\]/).slice(-2).join('/'); // 显示最后两级路径
    
    // 格式化时间
    const createdTime = formatTime(item.createdAt);
    const updatedTime = formatTime(item.updatedAt);
    
    // 优先显示更新时间，如果没有更新时间则显示创建时间
    const displayTime = updatedTime || createdTime;
    const timeLabel = updatedTime ? knowledgeT('knowledge.updatedTimeLabel', '更新时间') : knowledgeT('knowledge.createdTimeLabel', '创建时间');
    
    // 判断是否为最近更新（7天内）
    let isRecent = false;
    if (item.updatedAt && updatedTime) {
        const updateDate = new Date(item.updatedAt);
        if (!isNaN(updateDate.getTime())) {
            isRecent = (Date.now() - updateDate.getTime()) < 7 * 24 * 60 * 60 * 1000;
        }
    }
    
    return `
        <div class="knowledge-item-card" data-id="${escapeAttr(item.id)}" data-category="${escapeAttr(item.category)}">
            <div class="knowledge-item-card-header">
                <div class="knowledge-item-card-title-row">
                    <h4 class="knowledge-item-card-title" title="${escapeAttr(item.title)}">${escapeHtml(item.title)}</h4>
                    <div class="knowledge-item-card-actions">
                        <button class="knowledge-item-action-btn" data-require-permission="knowledge:write" onclick="editKnowledgeItem('${item.id}')" title="${knowledgeT('common.edit', '编辑')}">
                            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                                <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                                <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                            </svg>
                        </button>
                        <button class="knowledge-item-action-btn knowledge-item-delete-btn" data-require-permission="knowledge:delete" onclick="deleteKnowledgeItem('${item.id}')" title="${knowledgeT('common.delete', '删除')}">
                            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                                <path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                            </svg>
                        </button>
                    </div>
                </div>
                ${relativePath ? `<div class="knowledge-item-path">📁 ${escapeHtml(relativePath)}</div>` : ''}
            </div>
            ${previewText ? `
            <div class="knowledge-item-card-content">
                <p class="knowledge-item-preview">${escapeHtml(previewText)}</p>
            </div>
            ` : ''}
            <div class="knowledge-item-card-footer">
                <div class="knowledge-item-meta">
                    ${displayTime ? `<span class="knowledge-item-time" title="${timeLabel}">🕒 ${displayTime}</span>` : ''}
                    ${isRecent ? `<span class="knowledge-item-badge-new">${knowledgeT('knowledge.newBadge', '新')}</span>` : ''}
                </div>
            </div>
        </div>
    `;
}

// 更新统计信息（支持按分类分页的数据结构）
function updateKnowledgeStats(data, categoryCount) {
    const statsContainer = document.getElementById('knowledge-stats');
    if (!statsContainer) return;
    
    // 计算当前页的知识项数
    let currentPageItemCount = 0;
    if (Array.isArray(data) && data.length > 0) {
        // 判断是categoriesWithItems还是items数组
        if (data[0].category !== undefined && data[0].items !== undefined) {
            // 是按分类分页的数据结构
            currentPageItemCount = data.reduce((sum, cat) => sum + (cat.items?.length || 0), 0);
        } else {
            // 是按项分页的数据结构（向后兼容）
            currentPageItemCount = data.length;
        }
    }
    
    // 总分类数（来自分页信息，只有在未定义时才使用当前页分类数作为后备值）
    const totalCategories = (knowledgePagination.total != null) ? knowledgePagination.total : categoryCount;
    
    statsContainer.innerHTML = `
        <div class="knowledge-stat-item">
            <span class="knowledge-stat-label">${knowledgeT('knowledge.totalCategoriesLabel', '总分类数')}</span>
            <span class="knowledge-stat-value">${totalCategories}</span>
        </div>
        <div class="knowledge-stat-item">
            <span class="knowledge-stat-label">${knowledgeT('knowledge.pageCategoriesLabel', '当前页分类')}</span>
            <span class="knowledge-stat-value">${knowledgeT('knowledge.categoriesCountUnit', '{count} 个').replace('{count}', String(categoryCount))}</span>
        </div>
        <div class="knowledge-stat-item">
            <span class="knowledge-stat-label">${knowledgeT('knowledge.pageItemsLabel', '当前页知识项')}</span>
            <span class="knowledge-stat-value">${knowledgeT('knowledge.itemsCountUnit', '{count} 项').replace('{count}', String(currentPageItemCount))}</span>
        </div>
    `;

    // 更新索引进度
    updateIndexProgress();
}

// 更新索引进度
let indexProgressInterval = null;

async function updateIndexProgress() {
    try {
        const response = await apiFetch('/api/knowledge/index-status', {
            method: 'GET',
            headers: {
                'Cache-Control': 'no-cache, no-store, must-revalidate',
                'Pragma': 'no-cache',
                'Expires': '0'
            }
        });
        
        if (!response.ok) {
            return; // 静默失败，不影响主界面
        }
        
        const status = await response.json();
        const progressContainer = document.getElementById('knowledge-index-progress');
        if (!progressContainer) return;
        
        // 检查知识库功能是否启用
        if (status.enabled === false) {
            // 功能未启用，隐藏进度条
            progressContainer.style.display = 'none';
            if (indexProgressInterval) {
                clearInterval(indexProgressInterval);
                indexProgressInterval = null;
            }
            return;
        }
        
        const totalItems = status.total_items || 0;
        const indexedItems = status.indexed_items || 0;
        const progressPercent = status.progress_percent || 0;
        const isComplete = status.is_complete || false;
        const lastError = status.last_error || '';
        
        // 检查是否正在重建索引（优先使用重建状态）
        const isRebuilding = status.is_rebuilding || false;
        
        if (totalItems === 0 && !lastError) {
            // 没有知识项，隐藏进度条
            progressContainer.style.display = 'none';
            if (indexProgressInterval) {
                clearInterval(indexProgressInterval);
                indexProgressInterval = null;
            }
            return;
        }
        
        // 显示进度条
        progressContainer.style.display = 'block';
        
        // 如果有错误信息，显示错误
        if (lastError) {
            progressContainer.innerHTML = `
                <div class="knowledge-index-progress-error" style="
                    background: #fee;
                    border: 1px solid #fcc;
                    border-radius: 8px;
                    padding: 16px;
                    margin-bottom: 16px;
                ">
                    <div style="display: flex; align-items: center; margin-bottom: 8px;">
                        <span style="font-size: 20px; margin-right: 8px;">❌</span>
                        <span style="font-weight: bold; color: #c00;">${knowledgeT('knowledge.indexBuildFailed', '索引构建失败')}</span>
                    </div>
                    <div style="color: #666; font-size: 14px; margin-bottom: 12px; line-height: 1.5;">
                        ${escapeHtml(lastError)}
                    </div>
                    <div style="color: #999; font-size: 12px; margin-bottom: 12px;">
                        ${escapeHtml(knowledgeIndexErrorAdvice(lastError))}
                    </div>
                    <div style="display: flex; gap: 8px; flex-wrap: wrap;">
                        <button onclick="buildKnowledgeIndex()" style="
                            background: #007bff;
                            color: white;
                            border: none;
                            padding: 6px 12px;
                            border-radius: 4px;
                            cursor: pointer;
                            font-size: 13px;
                        ">${knowledgeT('knowledge.retryBtn', '重试')}</button>
                        <button onclick="rebuildKnowledgeIndexFull()" style="
                            background: #6c757d;
                            color: white;
                            border: none;
                            padding: 6px 12px;
                            border-radius: 4px;
                            cursor: pointer;
                            font-size: 13px;
                        ">${knowledgeT('knowledge.fullRebuildBtn', '全量重建')}</button>
                        <button onclick="stopIndexProgressPolling()" style="
                            background: #6c757d;
                            color: white;
                            border: none;
                            padding: 6px 12px;
                            border-radius: 4px;
                            cursor: pointer;
                            font-size: 13px;
                        ">${knowledgeT('common.close', '关闭')}</button>
                    </div>
                </div>
            `;
            // 停止轮询
            if (indexProgressInterval) {
                clearInterval(indexProgressInterval);
                indexProgressInterval = null;
            }
            // 显示错误通知
            showNotification(knowledgeT('knowledge.indexBuildFailed', '索引构建失败') + ': ' + lastError.substring(0, 100), 'error');
            return;
        }
        

        // 优先处理重建状态
        if (isRebuilding) {
            const rebuildTotal = status.rebuild_total || totalItems;
            const rebuildCurrent = status.rebuild_current || 0;
            const rebuildFailed = status.rebuild_failed || 0;
            const rebuildLastItemID = status.rebuild_last_item_id || '';
            const rebuildLastChunks = status.rebuild_last_chunks || 0;
            const rebuildStartTime = status.rebuild_start_time || '';

            // 计算进度百分比（使用重建进度）
            let rebuildProgress = progressPercent;
            if (rebuildTotal > 0) {
                rebuildProgress = (rebuildCurrent / rebuildTotal) * 100;
            }

            progressContainer.innerHTML = `
                <div class="knowledge-index-progress">
                    <div class="progress-header">
                        <span class="progress-icon">🔨</span>
                        <span class="progress-text">${knowledgeT('knowledge.buildingIndexProgress', '正在构建索引：{current}/{total} ({percent}%) - 失败：{failed}').replace('{current}', String(rebuildCurrent)).replace('{total}', String(rebuildTotal)).replace('{percent}', String(rebuildProgress.toFixed(1))).replace('{failed}', String(rebuildFailed))}</span>
                    </div>
                    <div class="progress-bar-container">
                        <div class="progress-bar" style="width: ${rebuildProgress}%"></div>
                    </div>
                    <div class="progress-hint">
                        ${rebuildLastItemID ? knowledgeT('knowledge.processingItem', '正在处理：{id}... ({chunks} chunks)').replace('{id}', escapeHtml(rebuildLastItemID.substring(0, 36))).replace('{chunks}', String(rebuildLastChunks)) : knowledgeT('knowledge.processing', '正在处理...')}
                        ${rebuildStartTime ? `<br>${knowledgeT('knowledge.startTimeLabel', '开始时间：{time}').replace('{time}', String(new Date(rebuildStartTime).toLocaleString()))}` : ''}
                    </div>
                </div>
            `;

            // 重建中时继续轮询
            if (!indexProgressInterval) {
                indexProgressInterval = setInterval(updateIndexProgress, 2000);
            }
            return;
        }
        
        if (isComplete) {
            progressContainer.innerHTML = `
                <div class="knowledge-index-progress-complete">
                    <span class="progress-icon">✅</span>
                    <span class="progress-text">${knowledgeT('knowledge.indexBuildComplete', '索引构建完成 ({indexed}/{total})').replace('{indexed}', String(indexedItems)).replace('{total}', String(totalItems))}</span>
                </div>
            `;
            // 完成后停止轮询
            if (indexProgressInterval) {
                clearInterval(indexProgressInterval);
                indexProgressInterval = null;
            }
        } else {
            const showResume = indexedItems < totalItems;
            progressContainer.innerHTML = `
                <div class="knowledge-index-progress">
                    <div class="progress-header">
                        <span class="progress-icon">🔨</span>
                        <span class="progress-text">${knowledgeT('knowledge.buildingIndexSimple', '正在构建索引: {current}/{total} ({percent}%)').replace('{current}', String(indexedItems)).replace('{total}', String(totalItems)).replace('{percent}', String(progressPercent.toFixed(1)))}</span>
                    </div>
                    <div class="progress-bar-container">
                        <div class="progress-bar" style="width: ${progressPercent}%"></div>
                    </div>
                    <div class="progress-hint">
                        ${knowledgeT('knowledge.indexDoneHint', '索引构建完成后，语义搜索功能将可用')}
                        ${showResume ? `<br><button type="button" onclick="buildKnowledgeIndex()" style="margin-top:8px;background:#007bff;color:#fff;border:none;padding:4px 10px;border-radius:4px;cursor:pointer;font-size:12px;">${knowledgeT('knowledge.buildIndex', '构建索引')}</button>` : ''}
                    </div>
                </div>
            `;
            
            // 如果还没有开始轮询，开始轮询
            if (!indexProgressInterval) {
                indexProgressInterval = setInterval(updateIndexProgress, 3000); // 每3秒刷新一次
            }
        }
    } catch (error) {
        // 显示错误信息
        console.error('获取索引状态失败:', error);
        const progressContainer = document.getElementById('knowledge-index-progress');
        if (progressContainer) {
            progressContainer.style.display = 'block';
            progressContainer.innerHTML = `
                <div class="knowledge-index-progress-error" style="
                    background: #fee;
                    border: 1px solid #fcc;
                    border-radius: 8px;
                    padding: 16px;
                    margin-bottom: 16px;
                ">
                    <div style="display: flex; align-items: center; margin-bottom: 8px;">
                        <span style="font-size: 20px; margin-right: 8px;">⚠️</span>
                        <span style="font-weight: bold; color: #c00;">${knowledgeT('knowledge.cannotGetIndexStatus', '无法获取索引状态')}</span>
                    </div>
                    <div style="color: #666; font-size: 14px;">
                        ${knowledgeT('knowledge.cannotGetIndexStatusHint', '无法连接到服务器获取索引状态，请检查网络连接或刷新页面。')}
                    </div>
                </div>
            `;
        }
        // 停止轮询
        if (indexProgressInterval) {
            clearInterval(indexProgressInterval);
            indexProgressInterval = null;
        }
    }
}

// 停止索引进度轮询
function stopIndexProgressPolling() {
    if (indexProgressInterval) {
        clearInterval(indexProgressInterval);
        indexProgressInterval = null;
    }
    const progressContainer = document.getElementById('knowledge-index-progress');
    if (progressContainer) {
        progressContainer.style.display = 'none';
    }
}

// 选择知识分类
function selectKnowledgeCategory(category) {
    const trigger = document.getElementById('knowledge-category-filter-trigger');
    const wrapper = document.getElementById('knowledge-category-filter-wrapper');
    const dropdown = document.getElementById('knowledge-category-filter-dropdown');
    
    if (trigger && wrapper && dropdown) {
        const displayText = category || knowledgeT('knowledgePage.all', '全部');
        trigger.querySelector('span').textContent = displayText;
        wrapper.classList.remove('open');
        
        // 更新选中状态
        dropdown.querySelectorAll('.custom-select-option').forEach(opt => {
            opt.classList.remove('selected');
            if (opt.getAttribute('data-value') === category) {
                opt.classList.add('selected');
            }
        });
    }
    // 切换分类时重置到第一页（如果选择了分类，API会返回该分类的所有项）
    loadKnowledgeItems(category, 1, knowledgePagination.pageSize);
}

// 筛选知识项
function filterKnowledgeItems() {
    const wrapper = document.getElementById('knowledge-category-filter-wrapper');
    if (wrapper) {
        const selectedOption = wrapper.querySelector('.custom-select-option.selected');
        const category = selectedOption ? selectedOption.getAttribute('data-value') : '';
        // 重置到第一页
        loadKnowledgeItems(category, 1, knowledgePagination.pageSize);
    }
}

// 处理搜索输入（带防抖）
function handleKnowledgeSearchInput() {
    const searchInput = document.getElementById('knowledge-search');
    const searchTerm = searchInput?.value.trim() || '';
    
    // 清除之前的定时器
    if (knowledgeSearchTimeout) {
        clearTimeout(knowledgeSearchTimeout);
    }
    
    // 如果搜索框为空，立即恢复列表
    if (!searchTerm) {
        const wrapper = document.getElementById('knowledge-category-filter-wrapper');
        let category = '';
        if (wrapper) {
            const selectedOption = wrapper.querySelector('.custom-select-option.selected');
            category = selectedOption ? selectedOption.getAttribute('data-value') : '';
        }
        loadKnowledgeItems(category, 1, knowledgePagination.pageSize);
        return;
    }
    
    // 有搜索词时，延迟500ms后执行搜索（防抖）
    knowledgeSearchTimeout = setTimeout(() => {
        searchKnowledgeItems();
    }, 500);
}

// 搜索知识项（后端关键字匹配，在所有数据中搜索）
async function searchKnowledgeItems() {
    const searchInput = document.getElementById('knowledge-search');
    const searchTerm = searchInput?.value.trim() || '';
    
    if (!searchTerm) {
        // 恢复原始列表（重置到第一页）
        const wrapper = document.getElementById('knowledge-category-filter-wrapper');
        let category = '';
        if (wrapper) {
            const selectedOption = wrapper.querySelector('.custom-select-option.selected');
            category = selectedOption ? selectedOption.getAttribute('data-value') : '';
        }
        await loadKnowledgeItems(category, 1, knowledgePagination.pageSize);
        return;
    }
    
    try {
        // 获取当前选择的分类
        const wrapper = document.getElementById('knowledge-category-filter-wrapper');
        let category = '';
        if (wrapper) {
            const selectedOption = wrapper.querySelector('.custom-select-option.selected');
            category = selectedOption ? selectedOption.getAttribute('data-value') : '';
        }
        
        // 调用后端API进行全量搜索
        const timestamp = Date.now();
        let url = `/api/knowledge/items?search=${encodeURIComponent(searchTerm)}&_t=${timestamp}`;
        if (category) {
            url += `&category=${encodeURIComponent(category)}`;
        }
        
        const response = await apiFetch(url, {
            method: 'GET',
            headers: {
                'Cache-Control': 'no-cache, no-store, must-revalidate',
                'Pragma': 'no-cache',
                'Expires': '0'
            }
        });
        
        if (!response.ok) {
            throw new Error(knowledgeT('knowledge.searchFailed', '搜索失败'));
        }
        
        const data = await response.json();
        
        // 检查知识库功能是否启用
        if (data.enabled === false) {
            renderKnowledgeNotEnabledState(document.getElementById('knowledge-items-list'));
            return;
        }
        
        // 处理搜索结果
        const categoriesWithItems = data.categories || [];
        
        // 渲染搜索结果
        const container = document.getElementById('knowledge-items-list');
        if (!container) return;
        
        if (categoriesWithItems.length === 0) {
            container.innerHTML = `
                <div class="empty-state" style="text-align: center; padding: 40px 20px;">
                    <div style="font-size: 48px; margin-bottom: 20px;">🔍</div>
                    <h3 style="margin-bottom: 10px;">${knowledgeT('knowledge.searchNoMatch', '未找到匹配的知识项')}</h3>
                    <p style="color: #999;">${knowledgeT('knowledge.searchNoResultsHint', '关键词 "{term}" 在所有数据中没有匹配结果').replace('{term}', escapeHtml(searchTerm))}</p>
                    <p style="color: #999; margin-top: 10px; font-size: 0.9em;">${knowledgeT('knowledge.searchTryOther', '请尝试其他关键词，或使用分类筛选功能')}</p>
                </div>
            `;
        } else {
            // 计算总项数和分类数
            const totalItems = categoriesWithItems.reduce((sum, cat) => sum + (cat.items?.length || 0), 0);
            const categoryCount = categoriesWithItems.length;
            
            // 更新统计信息
            updateKnowledgeStats(categoriesWithItems, categoryCount);
            
            // 渲染搜索结果
            renderKnowledgeItemsByCategories(categoriesWithItems);
        }
        
        // 搜索时隐藏分页（因为搜索结果显示所有匹配结果）
        const paginationContainer = document.getElementById('knowledge-pagination');
        if (paginationContainer) {
            paginationContainer.innerHTML = '';
        }
        
    } catch (error) {
        console.error('搜索知识项失败:', error);
        showNotification(knowledgeT('knowledge.searchFailed', '搜索失败') + ': ' + error.message, 'error');
    }
}

// 刷新知识库
async function refreshKnowledgeBase() {
    try {
        showNotification(knowledgeT('knowledge.scanStarting', '正在扫描知识库...'), 'info');
        const response = await apiFetch('/api/knowledge/scan', {
            method: 'POST'
        });
        if (!response.ok) {
            throw new Error(knowledgeT('knowledge.scanFailed', '扫描知识库失败'));
        }
        const data = await response.json();
        // 根据返回的消息显示不同的提示
        if (data.items_to_index && data.items_to_index > 0) {
            showNotification(knowledgeT('knowledge.scanDoneIndexing', '扫描完成，开始索引 {count} 个新添加或更新的知识项').replace('{count}', String(data.items_to_index)), 'success');
        } else {
            showNotification(data.message || knowledgeT('knowledge.scanDoneNoChange', '扫描完成，没有需要索引的新项或更新项'), 'success');
        }
        // 重新加载知识项（重置到第一页）
        await loadKnowledgeCategories();
        await loadKnowledgeItems(knowledgePagination.currentCategory, 1, knowledgePagination.pageSize);
        
        // 停止现有的轮询
        if (indexProgressInterval) {
            clearInterval(indexProgressInterval);
            indexProgressInterval = null;
        }
        
        // 如果有需要索引的项，等待一小段时间后立即更新进度
        if (data.items_to_index && data.items_to_index > 0) {
            await new Promise(resolve => setTimeout(resolve, 500));
            updateIndexProgress();
            // 开始轮询进度（每2秒刷新一次）
            if (!indexProgressInterval) {
                indexProgressInterval = setInterval(updateIndexProgress, 2000);
            }
        } else {
            // 没有需要索引的项，也更新一次以显示当前状态
            updateIndexProgress();
        }
    } catch (error) {
        console.error('刷新知识库失败:', error);
        showNotification(knowledgeT('knowledge.refreshFailed', '刷新知识库失败') + ': ' + error.message, 'error');
    }
}

// 启动知识库索引任务（mode: missing | full）
async function startKnowledgeIndexJob(mode = 'missing') {
    const fullRebuild = mode === 'full';
    const confirmMessage = fullRebuild
        ? knowledgeT('knowledge.rebuildIndexFullConfirm', '将重新索引全部知识项，这可能需要较长时间，是否继续？')
        : knowledgeT('knowledge.buildIndexConfirm', '将为尚未生成向量的知识项构建索引，是否继续？');
    if (!confirm(confirmMessage)) {
        return;
    }

    const actionLabel = fullRebuild ? knowledgeT('knowledge.fullRebuildIndexLabel', '全量重建索引') : knowledgeT('knowledge.buildIndexLabel', '构建索引');
    showNotification(knowledgeT('knowledge.indexJobStarting', '正在启动{label}...').replace('{label}', actionLabel), 'info');

    if (indexProgressInterval) {
        clearInterval(indexProgressInterval);
        indexProgressInterval = null;
    }

    const progressContainer = document.getElementById('knowledge-index-progress');
    if (progressContainer) {
        progressContainer.style.display = 'block';
        progressContainer.innerHTML = `
            <div class="knowledge-index-progress">
                <div class="progress-header">
                    <span class="progress-icon">🔨</span>
                    <span class="progress-text">${knowledgeT('knowledge.indexJobPreparing', '正在{label}: 准备中...').replace('{label}', actionLabel)}</span>
                </div>
                <div class="progress-bar-container">
                    <div class="progress-bar" style="width: 0%"></div>
                </div>
                <div class="progress-hint">${knowledgeT('knowledge.indexDoneHint', '索引构建完成后，语义搜索功能将可用')}</div>
            </div>
        `;
    }

    const query = fullRebuild ? '?mode=full' : '';
    const response = await apiFetch(`/api/knowledge/index${query}`, {
        method: 'POST'
    });

    let payload = {};
    try {
        payload = await response.json();
    } catch (_) {
        payload = {};
    }

    if (response.status === 409) {
        showNotification(payload.error || knowledgeT('knowledge.indexJobInProgress', '已有索引任务正在进行，请等待完成'), 'warning');
        updateIndexProgress();
        return;
    }
    if (!response.ok) {
        throw new Error(payload.error || knowledgeT('knowledge.indexJobFailed', '{label}失败').replace('{label}', actionLabel));
    }

    showNotification(payload.message || knowledgeT('knowledge.indexJobStarted', '{label}已开始，将在后台进行').replace('{label}', actionLabel), 'success');
    await new Promise(resolve => setTimeout(resolve, 500));
    updateIndexProgress();
    if (!indexProgressInterval) {
        indexProgressInterval = setInterval(updateIndexProgress, 2000);
    }
}

// 构建索引（默认：仅处理尚无向量的知识项）
async function buildKnowledgeIndex() {
    try {
        await startKnowledgeIndexJob('missing');
    } catch (error) {
        console.error('构建索引失败:', error);
        showNotification(knowledgeT('knowledge.buildIndexFailedMsg', '构建索引失败') + ': ' + error.message, 'error');
    }
}

// 全量重建索引（显式 opt-in）
async function rebuildKnowledgeIndexFull() {
    try {
        await startKnowledgeIndexJob('full');
    } catch (error) {
        console.error('全量重建索引失败:', error);
        showNotification(knowledgeT('knowledge.fullRebuildIndexFailedMsg', '全量重建索引失败') + ': ' + error.message, 'error');
    }
}

// 显示添加知识项模态框
function showAddKnowledgeItemModal() {
    if (typeof requirePermission === 'function' && !requirePermission('knowledge:write')) return;
    currentEditingItemId = null;
    document.getElementById('knowledge-item-modal-title').textContent = knowledgeT('knowledgeItemModal.addKnowledge', '添加知识');
    document.getElementById('knowledge-item-category').value = '';
    document.getElementById('knowledge-item-title').value = '';
    document.getElementById('knowledge-item-content').value = '';
    openAppModal('knowledge-item-modal');
}

// 编辑知识项
async function editKnowledgeItem(id) {
    try {
        currentEditingItemId = id;
        document.getElementById('knowledge-item-modal-title').textContent = knowledgeT('knowledgeItemModal.editKnowledge', '编辑知识');
        document.getElementById('knowledge-item-category').value = '';
        document.getElementById('knowledge-item-title').value = '';
        document.getElementById('knowledge-item-content').value = '';
        openAppModal('knowledge-item-modal', { focus: false });
        const response = await apiFetch(`/api/knowledge/items/${id}`);
        if (!response.ok) {
            throw new Error(knowledgeT('knowledge.getItemsFailed', '获取知识项失败'));
        }
        const item = await response.json();
        deferModalContent(() => {
            document.getElementById('knowledge-item-category').value = item.category;
            document.getElementById('knowledge-item-title').value = item.title;
            document.getElementById('knowledge-item-content').value = item.content;
            document.getElementById('knowledge-item-title')?.focus();
        });
    } catch (error) {
        closeAppModal('knowledge-item-modal');
        currentEditingItemId = null;
        console.error('编辑知识项失败:', error);
        showNotification(knowledgeT('knowledge.editItemFailed', '编辑知识项失败') + ': ' + error.message, 'error');
    }
}

// 保存知识项
async function saveKnowledgeItem() {
    if (typeof requirePermission === 'function' && !requirePermission('knowledge:write')) return;
    // 防止重复提交
    if (isSavingKnowledgeItem) {
        showNotification(knowledgeT('knowledge.savingInProgress', '正在保存中，请勿重复点击...'), 'warning');
        return;
    }
    
    const category = document.getElementById('knowledge-item-category').value.trim();
    const title = document.getElementById('knowledge-item-title').value.trim();
    const content = document.getElementById('knowledge-item-content').value.trim();
    
    if (!category || !title || !content) {
        showNotification(knowledgeT('knowledge.fillRequiredFields', '请填写所有必填字段'), 'error');
        return;
    }
    
    // 设置保存中标志
    isSavingKnowledgeItem = true;
    
    // 获取保存按钮和取消按钮
    const saveButton = document.querySelector('#knowledge-item-modal .modal-footer .btn-primary');
    const cancelButton = document.querySelector('#knowledge-item-modal .modal-footer .btn-secondary');
    const modal = document.getElementById('knowledge-item-modal');
    
    const originalButtonText = saveButton ? saveButton.textContent : knowledgeT('common.save', '保存');
    const originalButtonDisabled = saveButton ? saveButton.disabled : false;
    
    // 禁用所有输入字段和按钮
    const categoryInput = document.getElementById('knowledge-item-category');
    const titleInput = document.getElementById('knowledge-item-title');
    const contentInput = document.getElementById('knowledge-item-content');
    
    if (categoryInput) categoryInput.disabled = true;
    if (titleInput) titleInput.disabled = true;
    if (contentInput) contentInput.disabled = true;
    if (cancelButton) cancelButton.disabled = true;
    
    // 设置保存按钮加载状态
    if (saveButton) {
        saveButton.disabled = true;
        saveButton.style.opacity = '0.6';
        saveButton.style.cursor = 'not-allowed';
        saveButton.textContent = knowledgeT('knowledge.savingLabel', '保存中...');
    }
    
    try {
        const url = currentEditingItemId 
            ? `/api/knowledge/items/${currentEditingItemId}`
            : '/api/knowledge/items';
        const method = currentEditingItemId ? 'PUT' : 'POST';
        
        const response = await apiFetch(url, {
            method: method,
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({
                category,
                title,
                content
            })
        });
        
        if (!response.ok) {
            const errorData = await response.json().catch(() => ({}));
            throw new Error(errorData.error || knowledgeT('knowledge.saveItemFailed', '保存知识项失败'));
        }
        
        const item = await response.json();
        const action = currentEditingItemId ? knowledgeT('knowledge.actionUpdated', '更新') : knowledgeT('knowledge.actionCreated', '创建');
        const newItemCategory = item.category || category; // 保存新添加的知识项分类
        
        // 获取当前筛选状态，以便刷新后保持
        const currentCategory = document.getElementById('knowledge-category-filter-wrapper');
        let selectedCategory = '';
        if (currentCategory) {
            const selectedOption = currentCategory.querySelector('.custom-select-option.selected');
            if (selectedOption) {
                selectedCategory = selectedOption.getAttribute('data-value') || '';
            }
        }
        
        // 立即关闭模态框，给用户明确的反馈
        closeKnowledgeItemModal();
        
        // 显示加载状态并刷新数据（等待完成以确保数据同步）
        const itemsListContainer = document.getElementById('knowledge-items-list');
        const originalContent = itemsListContainer ? itemsListContainer.innerHTML : '';
        
        if (itemsListContainer) {
            itemsListContainer.innerHTML = '<div class="loading-spinner">' + knowledgeT('knowledge.refreshing', '刷新中...') + '</div>';
        }
        
        try {
            // 先刷新分类，再刷新知识项
            console.log('开始刷新知识库数据...');
            await loadKnowledgeCategories();
            console.log('分类刷新完成，开始刷新知识项...');
            
            // 如果新添加的知识项不在当前筛选的分类中，切换到该分类显示
            let categoryToShow = selectedCategory;
            if (!currentEditingItemId && selectedCategory && selectedCategory !== '' && newItemCategory !== selectedCategory) {
                // 新添加的知识项，如果当前筛选的不是该分类，切换到新知识项的分类
                categoryToShow = newItemCategory;
                // 更新筛选器显示（不触发加载，因为我们下面会手动加载）
                const trigger = document.getElementById('knowledge-category-filter-trigger');
                const wrapper = document.getElementById('knowledge-category-filter-wrapper');
                const dropdown = document.getElementById('knowledge-category-filter-dropdown');
                if (trigger && wrapper && dropdown) {
                    trigger.querySelector('span').textContent = newItemCategory || knowledgeT('knowledgePage.all', '全部');
                    dropdown.querySelectorAll('.custom-select-option').forEach(opt => {
                        opt.classList.remove('selected');
                        if (opt.getAttribute('data-value') === newItemCategory) {
                            opt.classList.add('selected');
                        }
                    });
                }
                showNotification(knowledgeT('knowledge.saveSuccessSwitchCategory', '✅ {action}成功！已切换到分类"{category}"查看新添加的知识项。').replace('{action}', action).replace('{category}', newItemCategory), 'success');
            }
            
            // 刷新知识项列表（重置到第一页）
            await loadKnowledgeItems(categoryToShow, 1, knowledgePagination.pageSize);
            console.log('知识项刷新完成');
        } catch (err) {
            console.error('刷新数据失败:', err);
            // 如果刷新失败，恢复原内容
            if (itemsListContainer && originalContent) {
                itemsListContainer.innerHTML = originalContent;
            }
            showNotification(knowledgeT('knowledge.savedButRefreshFailed', '⚠️ 知识项已保存，但刷新列表失败，请手动刷新页面查看'), 'warning');
        }
        
    } catch (error) {
        console.error('保存知识项失败:', error);
        showNotification('❌ ' + knowledgeT('knowledge.saveItemFailed', '保存知识项失败') + ': ' + error.message, 'error');

        // 如果通知系统不可用，使用alert
        if (typeof window.showNotification !== 'function') {
            alert('❌ ' + knowledgeT('knowledge.saveItemFailed', '保存知识项失败') + ': ' + error.message);
        }
        
        // 恢复输入字段和按钮状态（错误时不关闭模态框，让用户修改后重试）
        if (categoryInput) categoryInput.disabled = false;
        if (titleInput) titleInput.disabled = false;
        if (contentInput) contentInput.disabled = false;
        if (cancelButton) cancelButton.disabled = false;
        if (saveButton) {
            saveButton.disabled = false;
            saveButton.style.opacity = '';
            saveButton.style.cursor = '';
            saveButton.textContent = originalButtonText;
        }
    } finally {
        // 清除保存中标志
        isSavingKnowledgeItem = false;
    }
}

// 删除知识项
async function deleteKnowledgeItem(id) {
    if (!confirm(knowledgeT('knowledge.deleteItemConfirm', '确定要删除这个知识项吗？'))) {
        return;
    }
    
    // 找到要删除的知识项卡片和删除按钮
    const itemCard = document.querySelector(`.knowledge-item-card[data-id="${id}"]`);
    const deleteButton = itemCard ? itemCard.querySelector('.knowledge-item-delete-btn') : null;
    const categorySection = itemCard ? itemCard.closest('.knowledge-category-section') : null;
    let originalDisplay = '';
    let originalOpacity = '';
    let originalButtonOpacity = '';
    
    // 设置删除按钮的加载状态
    if (deleteButton) {
        originalButtonOpacity = deleteButton.style.opacity;
        deleteButton.style.opacity = '0.5';
        deleteButton.style.cursor = 'not-allowed';
        deleteButton.disabled = true;
        
        // 添加加载动画
        const svg = deleteButton.querySelector('svg');
        if (svg) {
            svg.style.animation = 'spin 1s linear infinite';
        }
    }
    
    // 立即从UI中移除该项（乐观更新）
    if (itemCard) {
        originalDisplay = itemCard.style.display;
        originalOpacity = itemCard.style.opacity;
        itemCard.style.transition = 'opacity 0.3s ease-out, transform 0.3s ease-out';
        itemCard.style.opacity = '0';
        itemCard.style.transform = 'translateX(-20px)';
        
        // 等待动画完成后移除
        setTimeout(() => {
            if (itemCard.parentElement) {
                itemCard.remove();
                
                // 检查分类是否还有项目，如果没有则隐藏分类标题
                if (categorySection) {
                    const remainingItems = categorySection.querySelectorAll('.knowledge-item-card');
                    if (remainingItems.length === 0) {
                        categorySection.style.transition = 'opacity 0.3s ease-out';
                        categorySection.style.opacity = '0';
                        setTimeout(() => {
                            if (categorySection.parentElement) {
                                categorySection.remove();
                            }
                        }, 300);
                    } else {
                        // 更新分类计数
                        const categoryCount = categorySection.querySelector('.knowledge-category-count');
                        if (categoryCount) {
                            const newCount = remainingItems.length;
                            categoryCount.textContent = knowledgeT('knowledge.itemsCountUnit', '{count} 项').replace('{count}', String(newCount));
                        }
                    }
                }
                
                // 不在这里更新统计信息，等待重新加载数据后由正确的逻辑更新
            }
        }, 300);
    }
    
    try {
        const response = await apiFetch(`/api/knowledge/items/${id}`, {
            method: 'DELETE'
        });
        
        if (!response.ok) {
            const errorData = await response.json().catch(() => ({}));
            throw new Error(errorData.error || knowledgeT('knowledge.deleteItemFailed', '删除知识项失败'));
        }

        // 显示成功通知
        showNotification(knowledgeT('knowledge.deleteItemSuccess', '✅ 删除成功！知识项已从系统中移除。'), 'success');
        
        // 重新加载数据以确保数据同步（保持当前页码）
        await loadKnowledgeCategories();
        await loadKnowledgeItems(knowledgePagination.currentCategory, knowledgePagination.currentPage, knowledgePagination.pageSize);
        
    } catch (error) {
        console.error('删除知识项失败:', error);
        
        // 如果删除失败，恢复该项显示
        if (itemCard && originalDisplay !== 'none') {
            itemCard.style.display = originalDisplay || '';
            itemCard.style.opacity = originalOpacity || '1';
            itemCard.style.transform = '';
            itemCard.style.transition = '';
            
            // 如果分类被移除了，需要恢复
            if (categorySection && !categorySection.parentElement) {
                // 需要重新加载来恢复（保持当前分页状态）
                await loadKnowledgeItems(knowledgePagination.currentCategory, knowledgePagination.currentPage, knowledgePagination.pageSize);
            }
        }
        
        // 恢复删除按钮状态
        if (deleteButton) {
            deleteButton.style.opacity = originalButtonOpacity || '';
            deleteButton.style.cursor = '';
            deleteButton.disabled = false;
            const svg = deleteButton.querySelector('svg');
            if (svg) {
                svg.style.animation = '';
            }
        }
        
        showNotification('❌ ' + knowledgeT('knowledge.deleteItemFailed', '删除知识项失败') + ': ' + error.message, 'error');
    }
}

// 临时更新统计信息（删除后）
function updateKnowledgeStatsAfterDelete() {
    const statsContainer = document.getElementById('knowledge-stats');
    if (!statsContainer) return;
    
    const allItems = document.querySelectorAll('.knowledge-item-card');
    const allCategories = document.querySelectorAll('.knowledge-category-section');
    
    const totalItems = allItems.length;
    const categoryCount = allCategories.length;
    
    // 计算总内容大小（这里简化处理，实际应该从服务器获取）
    const statsItems = statsContainer.querySelectorAll('.knowledge-stat-item');
    if (statsItems.length >= 2) {
        const totalItemsSpan = statsItems[0].querySelector('.knowledge-stat-value');
        const categoryCountSpan = statsItems[1].querySelector('.knowledge-stat-value');
        
        if (totalItemsSpan) {
            totalItemsSpan.textContent = totalItems;
        }
        if (categoryCountSpan) {
            categoryCountSpan.textContent = categoryCount;
        }
    }
}

// 关闭知识项模态框
function closeKnowledgeItemModal() {
    closeAppModal('knowledge-item-modal');
    
    // 重置编辑状态
    currentEditingItemId = null;
    isSavingKnowledgeItem = false;
    
    // 恢复所有输入字段和按钮状态
    const categoryInput = document.getElementById('knowledge-item-category');
    const titleInput = document.getElementById('knowledge-item-title');
    const contentInput = document.getElementById('knowledge-item-content');
    const saveButton = document.querySelector('#knowledge-item-modal .modal-footer .btn-primary');
    const cancelButton = document.querySelector('#knowledge-item-modal .modal-footer .btn-secondary');
    
    if (categoryInput) {
        categoryInput.disabled = false;
        categoryInput.value = '';
    }
    if (titleInput) {
        titleInput.disabled = false;
        titleInput.value = '';
    }
    if (contentInput) {
        contentInput.disabled = false;
        contentInput.value = '';
    }
    if (saveButton) {
        saveButton.disabled = false;
        saveButton.style.opacity = '';
        saveButton.style.cursor = '';
        saveButton.textContent = knowledgeT('common.save', '保存');
    }
    if (cancelButton) {
        cancelButton.disabled = false;
    }
}

// 加载检索日志
async function loadRetrievalLogs(conversationId = '', messageId = '') {
    try {
        let url = '/api/knowledge/retrieval-logs?limit=100';
        if (conversationId) {
            url += `&conversationId=${encodeURIComponent(conversationId)}`;
        }
        if (messageId) {
            url += `&messageId=${encodeURIComponent(messageId)}`;
        }
        
        const response = await apiFetch(url);
        if (!response.ok) {
            throw new Error(knowledgeT('retrievalLogs.getLogsFailed', '获取检索日志失败'));
        }
        const data = await response.json();
        renderRetrievalLogs(data.logs || []);
    } catch (error) {
        console.error('加载检索日志失败:', error);
        // 即使加载失败，也显示空状态而不是一直显示"加载中..."
        renderRetrievalLogs([]);
        // 只在非空筛选条件下才显示错误通知（避免在没有数据时显示错误）
        if (conversationId || messageId) {
            showNotification(_t('retrievalLogs.loadError') + ': ' + error.message, 'error');
        }
    }
}

// 渲染检索日志
function renderRetrievalLogs(logs) {
    const container = document.getElementById('retrieval-logs-list');
    if (!container) return;
    
    // 更新统计信息（即使为空数组也要更新）
    updateRetrievalStats(logs);
    
    if (logs.length === 0) {
        container.innerHTML = '<div class="empty-state">' + _t('retrievalLogs.noRecords') + '</div>';
        retrievalLogsData = [];
        return;
    }
    
    // 保存日志数据供详情查看使用
    retrievalLogsData = logs;
    
    container.innerHTML = logs.map((log, index) => {
        // 处理retrievedItems：可能是数组、字符串数组，或者特殊标记
        let itemCount = 0;
        let hasResults = false;
        
        if (log.retrievedItems) {
            if (Array.isArray(log.retrievedItems)) {
                // 过滤掉特殊标记
                const realItems = log.retrievedItems.filter(id => id !== '_has_results');
                itemCount = realItems.length;
                // 如果有特殊标记，表示有结果但ID未知，显示为"有结果"
                if (log.retrievedItems.includes('_has_results')) {
                    hasResults = true;
                    // 如果有真实ID，使用真实数量；否则显示为"有结果"（不显示具体数量）
                    if (itemCount === 0) {
                        itemCount = -1; // -1 表示有结果但数量未知
                    }
                } else {
                    hasResults = itemCount > 0;
                }
            } else if (typeof log.retrievedItems === 'string') {
                // 如果是字符串，尝试解析JSON
                try {
                    const parsed = JSON.parse(log.retrievedItems);
                    if (Array.isArray(parsed)) {
                        const realItems = parsed.filter(id => id !== '_has_results');
                        itemCount = realItems.length;
                        if (parsed.includes('_has_results')) {
                            hasResults = true;
                            if (itemCount === 0) {
                                itemCount = -1;
                            }
                        } else {
                            hasResults = itemCount > 0;
                        }
                    }
                } catch (e) {
                    // 解析失败，忽略
                }
            }
        }
        
        const timeAgo = getTimeAgo(log.createdAt);
        
        return `
            <div class="retrieval-log-card ${hasResults ? 'has-results' : 'no-results'}" data-index="${index}">
                <div class="retrieval-log-card-header">
                    <div class="retrieval-log-icon">
                        ${hasResults ? '🔍' : '⚠️'}
                    </div>
                    <div class="retrieval-log-main-info">
                        <div class="retrieval-log-query">
                            ${escapeHtml(log.query || _t('retrievalLogs.noQuery'))}
                        </div>
                        <div class="retrieval-log-meta">
                            <span class="retrieval-log-time" title="${formatTime(log.createdAt)}">
                                🕒 ${timeAgo}
                            </span>
                            ${log.riskType ? `<span class="retrieval-log-risk-type">📁 ${escapeHtml(log.riskType)}</span>` : ''}
                        </div>
                    </div>
                    <div class="retrieval-log-result-badge ${hasResults ? 'success' : 'empty'}">
                        ${hasResults ? (itemCount > 0 ? itemCount + ' ' + _t('retrievalLogs.itemsUnit') : _t('retrievalLogs.hasResults')) : _t('retrievalLogs.noResults')}
                    </div>
                </div>
                <div class="retrieval-log-card-body">
                    <div class="retrieval-log-details-grid">
                        ${log.conversationId ? `
                            <div class="retrieval-log-detail-item">
                                <span class="detail-label">${_t('retrievalLogs.conversationId')}</span>
                                <code class="detail-value" title="${escapeAttr(_t('retrievalLogs.clickToCopy'))}" data-copy-title-copied="${escapeAttr(_t('common.copied'))}" data-copy-title-click="${escapeAttr(_t('retrievalLogs.clickToCopy'))}" onclick="var t=this; navigator.clipboard.writeText(${escapeJsStringAttr(log.conversationId)}).then(function(){ t.title=t.getAttribute('data-copy-title-copied')||'Copied!'; setTimeout(function(){ t.title=t.getAttribute('data-copy-title-click')||'Click to copy'; }, 2000); });" style="cursor: pointer;">${escapeHtml(log.conversationId)}</code>
                            </div>
                        ` : ''}
                        ${log.messageId ? `
                            <div class="retrieval-log-detail-item">
                                <span class="detail-label">${_t('retrievalLogs.messageId')}</span>
                                <code class="detail-value" title="${escapeAttr(_t('retrievalLogs.clickToCopy'))}" data-copy-title-copied="${escapeAttr(_t('common.copied'))}" data-copy-title-click="${escapeAttr(_t('retrievalLogs.clickToCopy'))}" onclick="var el=this; navigator.clipboard.writeText(${escapeJsStringAttr(log.messageId)}).then(function(){ el.title=el.getAttribute('data-copy-title-copied')||el.title; setTimeout(function(){ el.title=el.getAttribute('data-copy-title-click')||el.title; }, 2000); });" style="cursor: pointer;">${escapeHtml(log.messageId)}</code>
                            </div>
                        ` : ''}
                        <div class="retrieval-log-detail-item">
                            <span class="detail-label">${_t('retrievalLogs.retrievalResult')}</span>
                            <span class="detail-value ${hasResults ? 'text-success' : 'text-muted'}">
                                ${hasResults ? (itemCount > 0 ? _t('retrievalLogs.foundCount', { count: itemCount }) : _t('retrievalLogs.foundUnknown')) : _t('retrievalLogs.noMatch')}
                            </span>
                        </div>
                    </div>
                    ${hasResults && log.retrievedItems && log.retrievedItems.length > 0 ? `
                        <div class="retrieval-log-items-preview">
                            <div class="retrieval-log-items-label">${_t('retrievalLogs.retrievedItemsLabel')}</div>
                            <div class="retrieval-log-items-list">
                                ${log.retrievedItems.slice(0, 3).map((itemId, idx) => `
                                    <span class="retrieval-log-item-tag">${idx + 1}</span>
                                `).join('')}
                                ${log.retrievedItems.length > 3 ? `<span class="retrieval-log-item-tag more">+${log.retrievedItems.length - 3}</span>` : ''}
                            </div>
                        </div>
                    ` : ''}
                    <div class="retrieval-log-actions">
                        <button class="btn-secondary btn-sm" onclick="showRetrievalLogDetails(${index})" style="margin-top: 12px; display: inline-flex; align-items: center; gap: 4px;">
                            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                                <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                                <circle cx="12" cy="12" r="3" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                            </svg>
                            ${_t('retrievalLogs.viewDetails')}
                        </button>
                        <button class="btn-secondary btn-sm retrieval-log-delete-btn" onclick="deleteRetrievalLog(${escapeJsStringAttr(log.id)}, ${index})" style="margin-top: 12px; margin-left: 8px; display: inline-flex; align-items: center; gap: 4px; color: var(--error-color, #dc3545); border-color: var(--error-color, #dc3545);" onmouseover="this.style.backgroundColor='rgba(220, 53, 69, 0.1)'; this.style.color='#dc3545';" onmouseout="this.style.backgroundColor=''; this.style.color='var(--error-color, #dc3545)';" title="${escapeAttr(_t('common.delete'))}">
                            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                                <path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                            </svg>
                            ${_t('common.delete')}
                        </button>
                    </div>
                </div>
            </div>
        `;
    }).join('');
}

// 更新检索统计信息
function updateRetrievalStats(logs) {
    const statsContainer = document.getElementById('retrieval-stats');
    if (!statsContainer) return;
    
    const totalLogs = logs.length;
    // 判断是否有结果：检查retrievedItems数组，过滤掉特殊标记后长度>0，或者包含特殊标记
    const successfulLogs = logs.filter(log => {
        if (!log.retrievedItems) return false;
        if (Array.isArray(log.retrievedItems)) {
            const realItems = log.retrievedItems.filter(id => id !== '_has_results');
            return realItems.length > 0 || log.retrievedItems.includes('_has_results');
        }
        return false;
    }).length;
    // 计算总知识项数（只计算真实ID，不包括特殊标记）
    const totalItems = logs.reduce((sum, log) => {
        if (!log.retrievedItems) return sum;
        if (Array.isArray(log.retrievedItems)) {
            const realItems = log.retrievedItems.filter(id => id !== '_has_results');
            return sum + realItems.length;
        }
        return sum;
    }, 0);
    const successRate = totalLogs > 0 ? ((successfulLogs / totalLogs) * 100).toFixed(1) : 0;
    
    statsContainer.innerHTML = `
        <div class="retrieval-stat-item">
            <span class="retrieval-stat-label" data-i18n="retrievalLogs.totalRetrievals">总检索次数</span>
            <span class="retrieval-stat-value">${totalLogs}</span>
        </div>
        <div class="retrieval-stat-item">
            <span class="retrieval-stat-label" data-i18n="retrievalLogs.successRetrievals">成功检索</span>
            <span class="retrieval-stat-value text-success">${successfulLogs}</span>
        </div>
        <div class="retrieval-stat-item">
            <span class="retrieval-stat-label" data-i18n="retrievalLogs.successRate">成功率</span>
            <span class="retrieval-stat-value">${successRate}%</span>
        </div>
        <div class="retrieval-stat-item">
            <span class="retrieval-stat-label" data-i18n="retrievalLogs.retrievedItems">检索到知识项</span>
            <span class="retrieval-stat-value">${totalItems}</span>
        </div>
    `;
    if (typeof window.applyTranslations === 'function') {
        window.applyTranslations(statsContainer);
    }
}

// 获取相对时间
function getTimeAgo(timeStr) {
    if (!timeStr) return '';
    
    // 处理时间字符串，支持多种格式
    let date;
    if (typeof timeStr === 'string') {
        // 首先尝试直接解析（支持RFC3339/ISO8601格式）
        date = new Date(timeStr);
        
        // 如果解析失败，尝试其他格式
        if (isNaN(date.getTime())) {
            // SQLite格式: "2006-01-02 15:04:05" 或带时区
            const sqliteMatch = timeStr.match(/(\d{4}-\d{2}-\d{2}[\sT]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:[+-]\d{2}:\d{2}|Z)?)/);
            if (sqliteMatch) {
                let timeStr2 = sqliteMatch[1].replace(' ', 'T');
                // 如果没有时区信息，添加Z表示UTC
                if (!timeStr2.includes('Z') && !timeStr2.match(/[+-]\d{2}:\d{2}$/)) {
                    timeStr2 += 'Z';
                }
                date = new Date(timeStr2);
            }
        }
        
        // 如果还是失败，尝试更宽松的格式
        if (isNaN(date.getTime())) {
            // 尝试匹配 "YYYY-MM-DD HH:MM:SS" 格式
            const match = timeStr.match(/(\d{4})-(\d{2})-(\d{2})[\sT](\d{2}):(\d{2}):(\d{2})/);
            if (match) {
                date = new Date(
                    parseInt(match[1]), 
                    parseInt(match[2]) - 1, 
                    parseInt(match[3]),
                    parseInt(match[4]),
                    parseInt(match[5]),
                    parseInt(match[6])
                );
            }
        }
    } else {
        date = new Date(timeStr);
    }
    
    // 检查日期是否有效
    if (isNaN(date.getTime())) {
        return formatTime(timeStr);
    }
    
    // 检查日期是否合理（不在1970年之前，不在未来太远）
    const year = date.getFullYear();
    if (year < 1970 || year > 2100) {
        return formatTime(timeStr);
    }
    
    const now = new Date();
    const diff = now - date;
    
    // 如果时间差为负数或过大（可能是解析错误），返回格式化时间
    if (diff < 0 || diff > 365 * 24 * 60 * 60 * 1000 * 10) { // 超过10年认为是错误
        return formatTime(timeStr);
    }
    
    const seconds = Math.floor(diff / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);
    const days = Math.floor(hours / 24);
    
    if (days > 0) return knowledgeT('common.daysAgo', '{{n}} 天前').replace('{{n}}', String(days));
    if (hours > 0) return knowledgeT('common.hoursAgo', '{{n}} 小时前').replace('{{n}}', String(hours));
    if (minutes > 0) return knowledgeT('common.minutesAgo', '{{n}} 分钟前').replace('{{n}}', String(minutes));
    return knowledgeT('common.justNow', '刚刚');
}

// 截断ID显示
function truncateId(id) {
    if (!id || id.length <= 16) return id;
    return id.substring(0, 8) + '...' + id.substring(id.length - 8);
}

// 筛选检索日志
function filterRetrievalLogs() {
    const conversationId = document.getElementById('retrieval-logs-conversation-id').value.trim();
    const messageId = document.getElementById('retrieval-logs-message-id').value.trim();
    loadRetrievalLogs(conversationId, messageId);
}

// 刷新检索日志
function refreshRetrievalLogs() {
    filterRetrievalLogs();
}

// 删除检索日志
async function deleteRetrievalLog(id, index) {
    if (!confirm(_t('retrievalLogs.deleteConfirm'))) {
        return;
    }
    
    // 找到要删除的日志卡片和删除按钮
    const logCard = document.querySelector(`.retrieval-log-card[data-index="${index}"]`);
    const deleteButton = logCard ? logCard.querySelector('.retrieval-log-delete-btn') : null;
    let originalButtonOpacity = '';
    let originalButtonDisabled = false;
    
    // 设置删除按钮的加载状态
    if (deleteButton) {
        originalButtonOpacity = deleteButton.style.opacity;
        originalButtonDisabled = deleteButton.disabled;
        deleteButton.style.opacity = '0.5';
        deleteButton.style.cursor = 'not-allowed';
        deleteButton.disabled = true;
        
        // 添加加载动画
        const svg = deleteButton.querySelector('svg');
        if (svg) {
            svg.style.animation = 'spin 1s linear infinite';
        }
    }
    
    // 立即从UI中移除该项（乐观更新）
    if (logCard) {
        logCard.style.transition = 'opacity 0.3s ease-out, transform 0.3s ease-out';
        logCard.style.opacity = '0';
        logCard.style.transform = 'translateX(-20px)';
        
        // 等待动画完成后移除
        setTimeout(() => {
            if (logCard.parentElement) {
                logCard.remove();
                
                // 更新统计信息（临时更新，稍后会重新加载）
                updateRetrievalStatsAfterDelete();
            }
        }, 300);
    }
    
    try {
        const response = await apiFetch(`/api/knowledge/retrieval-logs/${id}`, {
            method: 'DELETE'
        });
        
        if (!response.ok) {
            const errorData = await response.json().catch(() => ({}));
            throw new Error(errorData.error || knowledgeT('retrievalLogs.deleteError', '删除检索日志失败'));
        }
        
        // 显示成功通知
        showNotification(knowledgeT('retrievalLogs.deleteSuccess', '✅ 删除成功！检索记录已从系统中移除。'), 'success');
        
        // 从内存中移除该项
        if (retrievalLogsData && index >= 0 && index < retrievalLogsData.length) {
            retrievalLogsData.splice(index, 1);
        }
        
        // 重新加载数据以确保数据同步
        const conversationId = document.getElementById('retrieval-logs-conversation-id')?.value.trim() || '';
        const messageId = document.getElementById('retrieval-logs-message-id')?.value.trim() || '';
        await loadRetrievalLogs(conversationId, messageId);
        
    } catch (error) {
        console.error('删除检索日志失败:', error);
        
        // 如果删除失败，恢复该项显示
        if (logCard) {
            logCard.style.opacity = '1';
            logCard.style.transform = '';
            logCard.style.transition = '';
        }
        
        // 恢复删除按钮状态
        if (deleteButton) {
            deleteButton.style.opacity = originalButtonOpacity || '';
            deleteButton.style.cursor = '';
            deleteButton.disabled = originalButtonDisabled;
            const svg = deleteButton.querySelector('svg');
            if (svg) {
                svg.style.animation = '';
            }
        }
        
        showNotification(_t('retrievalLogs.deleteError') + ': ' + error.message, 'error');
    }
}

// 临时更新统计信息（删除后）
function updateRetrievalStatsAfterDelete() {
    const statsContainer = document.getElementById('retrieval-stats');
    if (!statsContainer) return;
    
    const allLogs = document.querySelectorAll('.retrieval-log-card');
    const totalLogs = allLogs.length;
    
    // 计算成功检索数
    const successfulLogs = Array.from(allLogs).filter(card => {
        return card.classList.contains('has-results');
    }).length;
    
    // 计算总知识项数（简化处理，实际应该从服务器获取）
    const totalItems = Array.from(allLogs).reduce((sum, card) => {
        const badge = card.querySelector('.retrieval-log-result-badge');
        if (badge && badge.classList.contains('success')) {
            const text = badge.textContent.trim();
            const match = text.match(/(\d+)/);
            if (match) {
                return sum + parseInt(match[1], 10);
            }
            return sum + 1; // 有结果但数量未知（如 "Has results" / "有结果"）
        }
        return sum;
    }, 0);
    
    const successRate = totalLogs > 0 ? ((successfulLogs / totalLogs) * 100).toFixed(1) : 0;
    
    statsContainer.innerHTML = `
        <div class="retrieval-stat-item">
            <span class="retrieval-stat-label" data-i18n="retrievalLogs.totalRetrievals">总检索次数</span>
            <span class="retrieval-stat-value">${totalLogs}</span>
        </div>
        <div class="retrieval-stat-item">
            <span class="retrieval-stat-label" data-i18n="retrievalLogs.successRetrievals">成功检索</span>
            <span class="retrieval-stat-value text-success">${successfulLogs}</span>
        </div>
        <div class="retrieval-stat-item">
            <span class="retrieval-stat-label" data-i18n="retrievalLogs.successRate">成功率</span>
            <span class="retrieval-stat-value">${successRate}%</span>
        </div>
        <div class="retrieval-stat-item">
            <span class="retrieval-stat-label" data-i18n="retrievalLogs.retrievedItems">检索到知识项</span>
            <span class="retrieval-stat-value">${totalItems}</span>
        </div>
    `;
    if (typeof window.applyTranslations === 'function') {
        window.applyTranslations(statsContainer);
    }
}

// 显示检索日志详情
async function showRetrievalLogDetails(index) {
    if (!retrievalLogsData || index < 0 || index >= retrievalLogsData.length) {
        showNotification(_t('retrievalLogs.detailError'), 'error');
        return;
    }
    
    const log = retrievalLogsData[index];
    
    // 获取检索到的知识项详情
    let retrievedItemsDetails = [];
    if (log.retrievedItems && Array.isArray(log.retrievedItems)) {
        const realItemIds = log.retrievedItems.filter(id => id !== '_has_results');
        if (realItemIds.length > 0) {
            try {
                // 批量获取知识项详情
                const itemPromises = realItemIds.map(async (itemId) => {
                    try {
                        const response = await apiFetch(`/api/knowledge/items/${itemId}`);
                        if (response.ok) {
                            return await response.json();
                        }
                        return null;
                    } catch (err) {
                        console.error(`获取知识项 ${itemId} 失败:`, err);
                        return null;
                    }
                });
                
                const items = await Promise.all(itemPromises);
                retrievedItemsDetails = items.filter(item => item !== null);
            } catch (err) {
                console.error('批量获取知识项详情失败:', err);
            }
        }
    }
    
    // 显示详情模态框
    showRetrievalLogDetailsModal(log, retrievedItemsDetails);
}

// 显示检索日志详情模态框
function showRetrievalLogDetailsModal(log, retrievedItems) {
    // 创建或获取模态框
    let modal = document.getElementById('retrieval-log-details-modal');
    if (!modal) {
        modal = document.createElement('div');
        modal.id = 'retrieval-log-details-modal';
        modal.className = 'modal';
        modal.innerHTML = `
            <div class="modal-content" style="max-width: 900px; max-height: 90vh; overflow-y: auto;">
                <div class="modal-header">
                    <h2 data-i18n="retrievalLogs.detailsTitle">检索详情</h2>
                    <span class="modal-close" onclick="closeRetrievalLogDetailsModal()">&times;</span>
                </div>
                <div class="modal-body" id="retrieval-log-details-content">
                </div>
                <div class="modal-footer">
                    <button class="btn-secondary" onclick="closeRetrievalLogDetailsModal()" data-i18n="common.close">关闭</button>
                </div>
            </div>
        `;
        if (typeof window.applyTranslations === 'function') {
            window.applyTranslations(modal);
        }
        document.body.appendChild(modal);
    }
    
    const content = document.getElementById('retrieval-log-details-content');
    if (content) content.innerHTML = '<p style="color:#64748b;margin:0;">…</p>';
    openAppModal(modal, { focus: false });

    deferModalContent(() => {
    const timeAgo = getTimeAgo(log.createdAt);
    const fullTime = formatTime(log.createdAt);
    
    let itemsHtml = '';
    if (retrievedItems.length > 0) {
        itemsHtml = retrievedItems.map((item, idx) => {
            // 提取内容预览
            let preview = item.content || '';
            preview = preview.replace(/^#+\s+/gm, '');
            preview = preview.replace(/```[\s\S]*?```/g, '');
            preview = preview.replace(/`[^`]+`/g, '');
            preview = preview.replace(/\[([^\]]+)\]\([^\)]+\)/g, '$1');
            preview = preview.replace(/\n+/g, ' ').replace(/\s+/g, ' ').trim();
            const previewText = preview.length > 200 ? preview.substring(0, 200) + '...' : preview;
            
            return `
                <div class="retrieval-detail-item-card" style="margin-bottom: 16px; padding: 16px; border: 1px solid var(--border-color); border-radius: 8px; background: var(--bg-secondary);">
                    <div style="display: flex; justify-content: space-between; align-items: start; margin-bottom: 8px;">
                        <h4 style="margin: 0; color: var(--text-primary);">${idx + 1}. ${escapeHtml(item.title || _t('retrievalLogs.untitled'))}</h4>
                        <span style="font-size: 0.875rem; color: var(--text-secondary);">${escapeHtml(item.category || _t('retrievalLogs.uncategorized'))}</span>
                    </div>
                    ${item.filePath ? `<div style="font-size: 0.875rem; color: var(--text-muted); margin-bottom: 8px;">📁 ${escapeHtml(item.filePath)}</div>` : ''}
                    <div style="font-size: 0.875rem; color: var(--text-secondary); line-height: 1.6;">
                        ${escapeHtml(previewText || _t('retrievalLogs.noContentPreview'))}
                    </div>
                </div>
            `;
        }).join('');
    } else {
        itemsHtml = '<div style="padding: 16px; text-align: center; color: var(--text-muted);">' + _t('retrievalLogs.noItemDetails') + '</div>';
    }
    
    content.innerHTML = `
        <div style="display: flex; flex-direction: column; gap: 20px;">
            <div class="retrieval-detail-section">
                <h3 style="margin: 0 0 12px 0; font-size: 1.125rem; color: var(--text-primary);">${_t('retrievalLogs.queryInfo')}</h3>
                <div style="padding: 12px; background: var(--bg-secondary); border-radius: 6px; border-left: 3px solid var(--accent-color);">
                    <div style="font-weight: 500; margin-bottom: 8px; color: var(--text-primary);">${_t('retrievalLogs.queryContent')}</div>
                    <div style="color: var(--text-primary); line-height: 1.6; word-break: break-word;">${escapeHtml(log.query || _t('retrievalLogs.noQuery'))}</div>
                </div>
            </div>
            
            <div class="retrieval-detail-section">
                <h3 style="margin: 0 0 12px 0; font-size: 1.125rem; color: var(--text-primary);">${_t('retrievalLogs.retrievalInfo')}</h3>
                <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 12px;">
                    ${log.riskType ? `
                        <div style="padding: 12px; background: var(--bg-secondary); border-radius: 6px;">
                            <div style="font-size: 0.875rem; color: var(--text-secondary); margin-bottom: 4px;">${_t('retrievalLogs.riskType')}</div>
                            <div style="font-weight: 500; color: var(--text-primary);">${escapeHtml(log.riskType)}</div>
                        </div>
                    ` : ''}
                    <div style="padding: 12px; background: var(--bg-secondary); border-radius: 6px;">
                        <div style="font-size: 0.875rem; color: var(--text-secondary); margin-bottom: 4px;">${_t('retrievalLogs.retrievalTime')}</div>
                        <div style="font-weight: 500; color: var(--text-primary);" title="${fullTime}">${timeAgo}</div>
                    </div>
                    <div style="padding: 12px; background: var(--bg-secondary); border-radius: 6px;">
                        <div style="font-size: 0.875rem; color: var(--text-secondary); margin-bottom: 4px;">${_t('retrievalLogs.retrievalResult')}</div>
                        <div style="font-weight: 500; color: var(--text-primary);">${_t('retrievalLogs.itemsCount', { count: retrievedItems.length })}</div>
                    </div>
                </div>
            </div>
            
            ${log.conversationId || log.messageId ? `
                <div class="retrieval-detail-section">
                    <h3 style="margin: 0 0 12px 0; font-size: 1.125rem; color: var(--text-primary);">${_t('retrievalLogs.relatedInfo')}</h3>
                    <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 12px;">
                        ${log.conversationId ? `
                            <div style="padding: 12px; background: var(--bg-secondary); border-radius: 6px;">
                                <div style="font-size: 0.875rem; color: var(--text-secondary); margin-bottom: 4px;">${_t('retrievalLogs.conversationId')}</div>
                                <code style="font-size: 0.8125rem; color: var(--text-primary); word-break: break-all; cursor: pointer;"
                                      onclick="navigator.clipboard.writeText(${escapeJsStringAttr(log.conversationId)}); this.title='${knowledgeT('common.copied', '已复制!')}'; setTimeout(() => this.title='${knowledgeT('retrievalLogs.clickToCopy', '点击复制')}', 2000);"
                                      title="${knowledgeT('retrievalLogs.clickToCopy', '点击复制')}" >${escapeHtml(log.conversationId)}</code>
                            </div>
                        ` : ''}
                        ${log.messageId ? `
                            <div style="padding: 12px; background: var(--bg-secondary); border-radius: 6px;">
                                <div style="font-size: 0.875rem; color: var(--text-secondary); margin-bottom: 4px;">${_t('retrievalLogs.messageId')}</div>
                                <code style="font-size: 0.8125rem; color: var(--text-primary); word-break: break-all; cursor: pointer;" 
                                      onclick="navigator.clipboard.writeText(${escapeJsStringAttr(log.messageId)}); this.title='${knowledgeT('common.copied', '已复制!')}'; setTimeout(() => this.title='${knowledgeT('retrievalLogs.clickToCopy', '点击复制')}', 2000);"
                                      title="${knowledgeT('retrievalLogs.clickToCopy', '点击复制')}" >${escapeHtml(log.messageId)}</code>
                            </div>
                        ` : ''}
                    </div>
                </div>
            ` : ''}
            
            <div class="retrieval-detail-section">
                <h3 style="margin: 0 0 12px 0; font-size: 1.125rem; color: var(--text-primary);">${knowledgeT('retrievalLogs.retrievedItems', '检索到的知识项')} (${retrievedItems.length})</h3>
                ${itemsHtml}
            </div>
        </div>
    `;
    });
}

// 关闭检索日志详情模态框
function closeRetrievalLogDetailsModal() {
    closeAppModal('retrieval-log-details-modal');
}

// 点击模态框外部关闭
window.addEventListener('click', function(event) {
    const modal = document.getElementById('retrieval-log-details-modal');
    if (event.target === modal) {
        closeRetrievalLogDetailsModal();
    }
});

// 语言切换时重新渲染检索历史列表与统计，使动态内容随语言更新；知识管理页的「未启用」区块已使用 data-i18n，会由 applyTranslations(document) 自动更新
document.addEventListener('languagechange', function () {
    var cur = typeof window.currentPage === 'function' ? window.currentPage() : (window.currentPage || '');
    if (cur === 'knowledge-retrieval-logs') {
        if (retrievalLogsData && retrievalLogsData.length >= 0) {
            renderRetrievalLogs(retrievalLogsData);
        }
    } else if (cur === 'knowledge-management') {
        // 仅对「知识库未启用」状态：已有 data-i18n，applyTranslations 已处理；此处可选地重新应用一次以兼容旧 DOM
        var listEl = document.getElementById('knowledge-items-list');
        if (listEl && typeof window.applyTranslations === 'function') {
            window.applyTranslations(listEl);
        }
    }
});

// 页面切换时加载数据
if (typeof switchPage === 'function') {
    const originalSwitchPage = switchPage;
    window.switchPage = function(page) {
        originalSwitchPage(page);
        
        if (page === 'knowledge-management') {
            loadKnowledgeCategories();
            loadKnowledgeItems(knowledgePagination.currentCategory, 1, knowledgePagination.pageSize);
            updateIndexProgress(); // 更新索引进度
        } else if (page === 'knowledge-retrieval-logs') {
            loadRetrievalLogs();
            // 切换到其他页面时停止轮询
            if (indexProgressInterval) {
                clearInterval(indexProgressInterval);
                indexProgressInterval = null;
            }
        } else {
            // 切换到其他页面时停止轮询
            if (indexProgressInterval) {
                clearInterval(indexProgressInterval);
                indexProgressInterval = null;
            }
        }
    };
}

// 页面卸载时清理定时器
window.addEventListener('beforeunload', function() {
    if (indexProgressInterval) {
        clearInterval(indexProgressInterval);
        indexProgressInterval = null;
    }
});

// 工具函数
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

function formatTime(timeStr) {
    if (!timeStr) return '';
    
    // 处理时间字符串，支持多种格式
    let date;
    if (typeof timeStr === 'string') {
        // 首先尝试直接解析（支持RFC3339/ISO8601格式）
        date = new Date(timeStr);
        
        // 如果解析失败，尝试其他格式
        if (isNaN(date.getTime())) {
            // SQLite格式: "2006-01-02 15:04:05" 或带时区
            const sqliteMatch = timeStr.match(/(\d{4}-\d{2}-\d{2}[\sT]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:[+-]\d{2}:\d{2}|Z)?)/);
            if (sqliteMatch) {
                let timeStr2 = sqliteMatch[1].replace(' ', 'T');
                // 如果没有时区信息，添加Z表示UTC
                if (!timeStr2.includes('Z') && !timeStr2.match(/[+-]\d{2}:\d{2}$/)) {
                    timeStr2 += 'Z';
                }
                date = new Date(timeStr2);
            }
        }
        
        // 如果还是失败，尝试更宽松的格式
        if (isNaN(date.getTime())) {
            // 尝试匹配 "YYYY-MM-DD HH:MM:SS" 格式
            const match = timeStr.match(/(\d{4})-(\d{2})-(\d{2})[\sT](\d{2}):(\d{2}):(\d{2})/);
            if (match) {
                date = new Date(
                    parseInt(match[1]), 
                    parseInt(match[2]) - 1, 
                    parseInt(match[3]),
                    parseInt(match[4]),
                    parseInt(match[5]),
                    parseInt(match[6])
                );
            }
        }
    } else {
        date = new Date(timeStr);
    }
    
    // 如果日期无效，检查是否是零值时间
    if (isNaN(date.getTime())) {
        // 检查是否是零值时间的字符串形式
        if (typeof timeStr === 'string' && (timeStr.includes('0001-01-01') || timeStr.startsWith('0001'))) {
            return '';
        }
        console.warn('无法解析时间:', timeStr);
        return '';
    }
    
    // 检查日期是否合理（不在1970年之前，不在未来太远）
    const year = date.getFullYear();
    if (year < 1970 || year > 2100) {
        // 如果是零值时间（0001-01-01），返回空字符串，不显示
        if (year === 1) {
            return '';
        }
        console.warn('时间值不合理:', timeStr, '解析为:', date);
        return '';
    }
    
    return date.toLocaleString('zh-CN', {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        hour12: false
    });
}

// 显示通知
function showNotification(message, type = 'info') {
    // 如果存在全局通知系统（且不是当前函数），使用它
    if (typeof window.showNotification === 'function' && window.showNotification !== showNotification) {
        window.showNotification(message, type);
        return;
    }
    
    // 否则使用自定义的toast通知
    showToastNotification(message, type);
}

// 显示Toast通知
function showToastNotification(message, type = 'info') {
    // 创建通知容器（如果不存在）
    let container = document.getElementById('toast-notification-container');
    if (!container) {
        container = document.createElement('div');
        container.id = 'toast-notification-container';
        container.style.cssText = `
            position: fixed;
            top: 20px;
            right: 20px;
            z-index: 10100;
            display: flex;
            flex-direction: column;
            gap: 12px;
            pointer-events: none;
        `;
        document.body.appendChild(container);
    }
    
    // 创建通知元素
    const toast = document.createElement('div');
    toast.className = `toast-notification toast-${type}`;
    
    const typeStyles = {
        success: {
            background: '#f4fbf6',
            border: '1px solid #cce8d4',
            color: '#3d6654',
            iconColor: '#52a06a',
            icon: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true"><circle cx="8" cy="8" r="7" stroke="currentColor" stroke-width="1.2"/><path d="M5 8l2 2 4-4" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"/></svg>'
        },
        error: {
            background: '#fef7f7',
            border: '1px solid #f3d0d0',
            color: '#8b4444',
            iconColor: '#c96a6a',
            icon: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true"><circle cx="8" cy="8" r="7" stroke="currentColor" stroke-width="1.2"/><path d="M6 6l4 4M10 6l-4 4" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/></svg>'
        },
        info: {
            background: '#f5f9ff',
            border: '1px solid #cfe0f5',
            color: '#4a6078',
            iconColor: '#6b8fbf',
            icon: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true"><circle cx="8" cy="8" r="7" stroke="currentColor" stroke-width="1.2"/><path d="M8 7v4M8 5.5v.01" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/></svg>'
        },
        warning: {
            background: '#fffbf3',
            border: '1px solid #f0dfc0',
            color: '#7a6535',
            iconColor: '#c4a04a',
            icon: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true"><path d="M8 2.5l6 10.5H2L8 2.5z" stroke="currentColor" stroke-width="1.2" stroke-linejoin="round"/><path d="M8 7v2.5M8 11v.01" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/></svg>'
        }
    };

    const style = typeStyles[type] || typeStyles.info;

    toast.style.cssText = `
        background: ${style.background};
        border: ${style.border};
        color: ${style.color};
        padding: 10px 14px;
        border-radius: 10px;
        box-shadow: 0 2px 8px rgba(15, 23, 42, 0.06);
        min-width: 220px;
        max-width: 420px;
        pointer-events: auto;
        animation: slideInRight 0.25s ease-out;
        display: flex;
        align-items: center;
        gap: 10px;
        font-size: 0.875rem;
        line-height: 1.45;
        word-wrap: break-word;
        backdrop-filter: none;
        -webkit-backdrop-filter: none;
    `;

    toast.innerHTML = `
        <span style="color: ${style.iconColor}; flex-shrink: 0; display: flex; align-items: center;">${style.icon}</span>
        <span style="flex: 1;">${escapeHtml(message)}</span>
        <button onclick="this.parentElement.remove()" style="
            background: transparent;
            border: none;
            color: ${style.color};
            cursor: pointer;
            font-size: 1rem;
            padding: 0;
            margin-left: 4px;
            opacity: 0.45;
            flex-shrink: 0;
            width: 20px;
            height: 20px;
            display: flex;
            align-items: center;
            justify-content: center;
        " onmouseover="this.style.opacity='0.75'" onmouseout="this.style.opacity='0.45'">×</button>
    `;
    
    container.appendChild(toast);
    
    // 自动移除（成功消息显示5秒，错误消息显示7秒，其他显示4秒）
    const duration = type === 'success' ? 5000 : type === 'error' ? 7000 : 4000;
    setTimeout(() => {
        if (toast.parentElement) {
            toast.style.animation = 'slideOutRight 0.3s ease-out';
            setTimeout(() => {
                if (toast.parentElement) {
                    toast.remove();
                }
            }, 300);
        }
    }, duration);
}

// 添加CSS动画（如果不存在）
if (!document.getElementById('toast-notification-styles')) {
    const style = document.createElement('style');
    style.id = 'toast-notification-styles';
    style.textContent = `
        @keyframes slideInRight {
            from {
                transform: translateX(16px);
                opacity: 0;
            }
            to {
                transform: translateX(0);
                opacity: 1;
            }
        }
        @keyframes slideOutRight {
            from {
                transform: translateX(0);
                opacity: 1;
            }
            to {
                transform: translateX(16px);
                opacity: 0;
            }
        }
    `;
    document.head.appendChild(style);
}

// 点击模态框外部关闭
window.addEventListener('click', function(event) {
    const modal = document.getElementById('knowledge-item-modal');
    if (event.target === modal) {
        closeKnowledgeItemModal();
    }
});

// 切换到设置页面（用于功能未启用时的提示）
function switchToSettings() {
    if (typeof switchPage === 'function') {
        switchPage('settings');
        // 等待设置页面加载后，切换到知识库配置部分
        setTimeout(() => {
            if (typeof switchSettingsSection === 'function') {
                // 查找知识库配置部分（通常在基本设置中）
                const knowledgeSection = document.querySelector('[data-section="knowledge"]');
                if (knowledgeSection) {
                    switchSettingsSection('knowledge');
                } else {
                    // 如果没有独立的知识库部分，切换到基本设置
                    switchSettingsSection('basic');
                    // 滚动到知识库配置区域
                    setTimeout(() => {
                        const knowledgeEnabledCheckbox = document.getElementById('knowledge-enabled');
                        if (knowledgeEnabledCheckbox) {
                            knowledgeEnabledCheckbox.scrollIntoView({ behavior: 'smooth', block: 'center' });
                            // 高亮显示
                            knowledgeEnabledCheckbox.parentElement.style.transition = 'background-color 0.3s';
                            knowledgeEnabledCheckbox.parentElement.style.backgroundColor = '#e3f2fd';
                            setTimeout(() => {
                                knowledgeEnabledCheckbox.parentElement.style.backgroundColor = '';
                            }, 2000);
                        }
                    }, 300);
                }
            }
        }, 100);
    }
}

// 自定义下拉组件交互
document.addEventListener('DOMContentLoaded', function() {
    const wrapper = document.getElementById('knowledge-category-filter-wrapper');
    const trigger = document.getElementById('knowledge-category-filter-trigger');
    
    if (wrapper && trigger) {
        // 点击触发器打开/关闭下拉菜单
        trigger.addEventListener('click', function(e) {
            e.stopPropagation();
            wrapper.classList.toggle('open');
        });
        
        // 点击外部关闭下拉菜单
        document.addEventListener('click', function(e) {
            if (!wrapper.contains(e.target)) {
                wrapper.classList.remove('open');
            }
        });
        
        // 选择选项时更新选中状态
        const dropdown = document.getElementById('knowledge-category-filter-dropdown');
        if (dropdown) {
            // 默认选中"全部"选项
            const defaultOption = dropdown.querySelector('.custom-select-option[data-value=""]');
            if (defaultOption) {
                defaultOption.classList.add('selected');
            }
            
            dropdown.addEventListener('click', function(e) {
                const option = e.target.closest('.custom-select-option');
                if (option) {
                    // 移除之前的选中状态
                    dropdown.querySelectorAll('.custom-select-option').forEach(opt => {
                        opt.classList.remove('selected');
                    });
                    // 添加选中状态
                    option.classList.add('selected');
                }
            });
        }
    }
});
