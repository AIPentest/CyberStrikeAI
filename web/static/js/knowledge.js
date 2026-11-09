function knowledgeIndexErrorAdvice(error) {
    const text = String(error || '');
    if (/\b401\b|incorrect api key|invalid api key|unauthorized/i.test(text)) return knowledgeT('knowledge.embeddingAuthRejected', 'The embedding service rejected authentication. Please check the API key and service address in the knowledge base embedding configuration, save, and then retry.');
    if (/\b403\b|forbidden/i.test(text)) return knowledgeT('knowledge.embeddingForbidden', 'The embedding service denied access. Please check your account permissions and the access permissions of the selected embedding model.');
    if (/\b429\b|rate limit|quota/i.test(text)) return knowledgeT('knowledge.embeddingRateLimited', 'Embedding service rate-limited request. Please check your request frequency and available account quota, and try again later.');
    if (/timeout|deadline exceeded/i.test(text)) return knowledgeT('knowledge.embeddingTimeout', 'The embedding service request timed out. Please check service availability, network, and proxy configuration, then retry.');
    if (/connection refused|no such host|network is unreachable/i.test(text)) return knowledgeT('knowledge.embeddingUnreachable', 'Unable to connect to the embedding service. Please check the service address, DNS, network, and proxy configuration.');
    return knowledgeT('knowledge.embeddingCheckConfig', 'Please check the embedding model and service configuration based on the error above, fix them, and retry.');
}

// Knowledge base management related functions
function _t(key, opts) {
    return typeof window.t === 'function' ? window.t(key, opts) : key;
}

// Same usage as _t, but keeps the original Chinese text as the fallback value when not initialized
function knowledgeT(key, fallback) {
    if (typeof window === 'object' && window && typeof window.t === 'function') {
        const translated = window.t(key);
        if (translated && translated !== key) return translated;
    }
    return fallback;
}

// Returns the HTML of the "knowledge base not enabled" hint block (uses data-i18n so it updates automatically when the language is switched)
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

// Render the "knowledge base not enabled" state to the container, and apply the current language
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
let isSavingKnowledgeItem = false; // Prevent duplicate submission
let retrievalLogsData = []; // Store retrieval log data, used for viewing details
let knowledgePagination = {
    currentPage: 1,
    pageSize: 10, // Categories per page (changed to paginate by category)
    total: 0,
    currentCategory: ''
};
let knowledgeSearchTimeout = null; // Search debounce timer

// Load knowledge category
async function loadKnowledgeCategories() {
    try {
        // Add timestamp parameter to avoid caching
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
            throw new Error(knowledgeT('knowledge.getCategoriesFailed', 'Failed to get category'));
        }
        const data = await response.json();
        
        // Check whether the knowledge base feature is enabled
        if (data.enabled === false) {
            // Feature not enabled, display a friendly message (using data-i18n, automatically updates when switching language)
            renderKnowledgeNotEnabledState(document.getElementById('knowledge-items-list'));
            return [];
        }
        
        knowledgeCategories = data.categories || [];
        
        // Update category filter dropdown
        const filterDropdown = document.getElementById('knowledge-category-filter-dropdown');
        if (filterDropdown) {
            filterDropdown.innerHTML = '<div class="custom-select-option" data-value="" onclick="selectKnowledgeCategory(\'\')">' + knowledgeT('knowledgePage.all', 'all') + '</div>';
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
        console.error('Failed to load category:', error);
        // Only show the error when the feature is not enabled
        if (!/(知识库功能未启用|knowledge base feature is not enabled)/i.test(error.message)) {
            showNotification(knowledgeT('knowledge.loadCategoriesFailed', 'Failed to load category') + ': ' + error.message, 'error');
        }
        return [];
    }
}

// Load knowledge item list (supports pagination by category, does not load full content by default)
async function loadKnowledgeItems(category = '', page = 1, pageSize = 10) {
    try {
        // Update pagination state
        knowledgePagination.currentCategory = category;
        knowledgePagination.currentPage = page;
        knowledgePagination.pageSize = pageSize;
        
        // Build URL (category pagination mode, does not include full content)
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
            throw new Error(knowledgeT('knowledge.getItemsFailed', 'Failed to get knowledge item'));
        }
        const data = await response.json();

        // Check whether the knowledge base feature is enabled
        if (data.enabled === false) {
            // Feature not enabled, display a friendly message (if not already shown; using data-i18n, it will automatically update when switching languages)
            const container = document.getElementById('knowledge-items-list');
            if (container && !container.querySelector('.empty-state')) {
                renderKnowledgeNotEnabledState(container);
            }
            knowledgeItems = [];
            knowledgePagination.total = 0;
            renderKnowledgePagination();
            return [];
        }
        
        // Handle paginated response data by category
        const categoriesWithItems = data.categories || [];
        knowledgePagination.total = data.total || 0; // Total number of categories
        
        renderKnowledgeItemsByCategories(categoriesWithItems);
        
        // If a single category is selected, don't display pagination (because only one category is displayed)
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
        console.error('Failed to load knowledge item:', error);
        // Only show the error when the feature is not enabled
        if (!/(知识库功能未启用|knowledge base feature is not enabled)/i.test(error.message)) {
            showNotification(knowledgeT('knowledge.loadItemsFailed', 'Failed to load knowledge item') + ': ' + error.message, 'error');
        }
        return [];
    }
}

// Render knowledge item list (data structure paginated by category)
function renderKnowledgeItemsByCategories(categoriesWithItems) {
    const container = document.getElementById('knowledge-items-list');
    if (!container) return;
    
    if (categoriesWithItems.length === 0) {
        container.innerHTML = '<div class="empty-state">' + knowledgeT('knowledge.noItems', 'No knowledge items yet') + '</div>';
        return;
    }
    
    // Calculate total item count and category count
    const totalItems = categoriesWithItems.reduce((sum, cat) => sum + (cat.items?.length || 0), 0);
    const categoryCount = categoriesWithItems.length;
    
    // Update statistics information
    updateKnowledgeStats(categoriesWithItems, categoryCount);
    
    // Render categories and knowledge items
    let html = '<div class="knowledge-categories-container">';
    
    categoriesWithItems.forEach(categoryData => {
        const category = categoryData.category || knowledgeT('knowledge.uncategorized', 'Uncategorized');
        const categoryItems = categoryData.items || [];
        const categoryCount = categoryData.itemCount || categoryItems.length;
        
        html += `
            <div class="knowledge-category-section" data-category="${escapeAttr(category)}">
                <div class="knowledge-category-header">
                    <div class="knowledge-category-info">
                        <h3 class="knowledge-category-title">${escapeHtml(category)}</h3>
                        <span class="knowledge-category-count">${knowledgeT('knowledge.itemsCountUnit', '{count} items').replace('{count}', String(categoryCount))}</span>
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

// Render knowledge item list (backward compatible, for old code that paginates by item)
function renderKnowledgeItems(items) {
    const container = document.getElementById('knowledge-items-list');
    if (!container) return;
    
    if (items.length === 0) {
        container.innerHTML = '<div class="empty-state">' + knowledgeT('knowledge.noItems', 'No knowledge items yet') + '</div>';
        return;
    }
    
    // Group by category
    const groupedByCategory = {};
    items.forEach(item => {
        const category = item.category || knowledgeT('knowledge.uncategorized', 'Uncategorized');
        if (!groupedByCategory[category]) {
            groupedByCategory[category] = [];
        }
        groupedByCategory[category].push(item);
    });
    
    // Update statistics information
    updateKnowledgeStats(items, Object.keys(groupedByCategory).length);
    
    // Render the content after grouping
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
                        <span class="knowledge-category-count">${knowledgeT('knowledge.itemsCountUnit', '{count} items').replace('{count}', String(categoryCount))}</span>
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

// Render paging control (paged by category)
function renderKnowledgePagination() {
    const container = document.getElementById('knowledge-pagination');
    if (!container) return;
    
    const { currentPage, pageSize, total } = knowledgePagination;
    const totalPages = Math.ceil(total / pageSize); // total is the total number of categories
    
    if (totalPages <= 1) {
        container.innerHTML = '';
        return;
    }
    
    let html = '<div class="knowledge-pagination" style="display: flex; justify-content: center; align-items: center; gap: 8px; padding: 20px; flex-wrap: wrap;">';
    
    // Previous page button
    html += `<button class="pagination-btn" onclick="loadKnowledgePage(${currentPage - 1})" ${currentPage <= 1 ? 'disabled style="opacity: 0.5; cursor: not-allowed;"' : ''}>${knowledgeT('knowledge.prevPage', 'Previous page')}</button>`;

    // Page number display（display number of categories）
    html += `<span style="padding: 0 12px;">${knowledgeT('knowledge.paginationInfo', 'Page {page} of {totalPages} ({total} categories in total)').replace('{page}', String(currentPage)).replace('{totalPages}', String(totalPages)).replace('{total}', String(total))}</span>`;

    // Next page button
    html += `<button class="pagination-btn" onclick="loadKnowledgePage(${currentPage + 1})" ${currentPage >= totalPages ? 'disabled style="opacity: 0.5; cursor: not-allowed;"' : ''}>${knowledgeT('knowledge.nextPage', 'Next page')}</button>`;
    
    html += '</div>';
    container.innerHTML = html;
}

// Load knowledge items for the specified page number
function loadKnowledgePage(page) {
    const { currentCategory, pageSize, total } = knowledgePagination;
    const totalPages = Math.ceil(total / pageSize);
    
    if (page < 1 || page > totalPages) {
        return;
    }
    
    loadKnowledgeItems(currentCategory, page, pageSize);
}

// Render a single knowledge item card
function renderKnowledgeItemCard(item) {
    // Extract content preview (if item has no content field, it means it's a summary, don't display preview)
    let previewText = '';
    if (item.content) {
        // Remove markdown format, take first 150 characters
        let preview = item.content;
        // Remove markdown heading markers
        preview = preview.replace(/^#+\s+/gm, '');
        // Remove code block
        preview = preview.replace(/```[\s\S]*?```/g, '');
        // Remove inline code
        preview = preview.replace(/`[^`]+`/g, '');
        // Remove link
        preview = preview.replace(/\[([^\]]+)\]\([^\)]+\)/g, '$1');
        // Remove extra whitespace
        preview = preview.replace(/\n+/g, ' ').replace(/\s+/g, ' ').trim();
        
        previewText = preview.length > 150 ? preview.substring(0, 150) + '...' : preview;
    }
    
    // Extract file path for display
    const filePath = item.filePath || '';
    const relativePath = filePath.split(/[/\\]/).slice(-2).join('/'); // Show the last two levels of the path
    
    // Format time
    const createdTime = formatTime(item.createdAt);
    const updatedTime = formatTime(item.updatedAt);
    
    // Prefer to display the update time; if there is no update time, display the creation time
    const displayTime = updatedTime || createdTime;
    const timeLabel = updatedTime ? knowledgeT('knowledge.updatedTimeLabel', 'Update time') : knowledgeT('knowledge.createdTimeLabel', 'Creation time');
    
    // Determine whether it is a recent update (within 7 days)
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
                        <button class="knowledge-item-action-btn" data-require-permission="knowledge:write" onclick="editKnowledgeItem('${item.id}')" title="${knowledgeT('common.edit', 'edit')}">
                            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                                <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                                <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                            </svg>
                        </button>
                        <button class="knowledge-item-action-btn knowledge-item-delete-btn" data-require-permission="knowledge:delete" onclick="deleteKnowledgeItem('${item.id}')" title="${knowledgeT('common.delete', 'Delete')}">
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
                    ${isRecent ? `<span class="knowledge-item-badge-new">${knowledgeT('knowledge.newBadge', 'New')}</span>` : ''}
                </div>
            </div>
        </div>
    `;
}

// Update statistics info (supports paginated data structure by category)
function updateKnowledgeStats(data, categoryCount) {
    const statsContainer = document.getElementById('knowledge-stats');
    if (!statsContainer) return;
    
    // Calculate the number of knowledge items on the current page
    let currentPageItemCount = 0;
    if (Array.isArray(data) && data.length > 0) {
        // Determine whether it is the categoriesWithItems or items array
        if (data[0].category !== undefined && data[0].items !== undefined) {
            // is a data structure paginated by category
            currentPageItemCount = data.reduce((sum, cat) => sum + (cat.items?.length || 0), 0);
        } else {
            // is a paginated-by-item data structure (backward compatible)
            currentPageItemCount = data.length;
        }
    }
    
    // Total category count (from pagination info, only uses the current page category count as a fallback value when undefined)
    const totalCategories = (knowledgePagination.total != null) ? knowledgePagination.total : categoryCount;
    
    statsContainer.innerHTML = `
        <div class="knowledge-stat-item">
            <span class="knowledge-stat-label">${knowledgeT('knowledge.totalCategoriesLabel', 'Total number of categories')}</span>
            <span class="knowledge-stat-value">${totalCategories}</span>
        </div>
        <div class="knowledge-stat-item">
            <span class="knowledge-stat-label">${knowledgeT('knowledge.pageCategoriesLabel', 'Current page category')}</span>
            <span class="knowledge-stat-value">${knowledgeT('knowledge.categoriesCountUnit', '{count} items').replace('{count}', String(categoryCount))}</span>
        </div>
        <div class="knowledge-stat-item">
            <span class="knowledge-stat-label">${knowledgeT('knowledge.pageItemsLabel', 'Current page knowledge items')}</span>
            <span class="knowledge-stat-value">${knowledgeT('knowledge.itemsCountUnit', '{count} items').replace('{count}', String(currentPageItemCount))}</span>
        </div>
    `;

    // Update index progress
    updateIndexProgress();
}

// Update index progress
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
            return; // Fail silently, does not affect the main interface
        }
        
        const status = await response.json();
        const progressContainer = document.getElementById('knowledge-index-progress');
        if (!progressContainer) return;
        
        // Check whether the knowledge base feature is enabled
        if (status.enabled === false) {
            // Feature not enabled, hide progress bar
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
        
        // Check whether the index is currently being rebuilt (prefer using the rebuild status)
        const isRebuilding = status.is_rebuilding || false;
        
        if (totalItems === 0 && !lastError) {
            // No knowledge items, hide progress bar
            progressContainer.style.display = 'none';
            if (indexProgressInterval) {
                clearInterval(indexProgressInterval);
                indexProgressInterval = null;
            }
            return;
        }
        
        // Display progress bar
        progressContainer.style.display = 'block';
        
        // If there is an error message, display the error
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
                        <span style="font-weight: bold; color: #c00;">${knowledgeT('knowledge.indexBuildFailed', 'Index build failed')}</span>
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
                        ">${knowledgeT('knowledge.retryBtn', 'Retry')}</button>
                        <button onclick="rebuildKnowledgeIndexFull()" style="
                            background: #6c757d;
                            color: white;
                            border: none;
                            padding: 6px 12px;
                            border-radius: 4px;
                            cursor: pointer;
                            font-size: 13px;
                        ">${knowledgeT('knowledge.fullRebuildBtn', 'Full rebuild')}</button>
                        <button onclick="stopIndexProgressPolling()" style="
                            background: #6c757d;
                            color: white;
                            border: none;
                            padding: 6px 12px;
                            border-radius: 4px;
                            cursor: pointer;
                            font-size: 13px;
                        ">${knowledgeT('common.close', 'Close')}</button>
                    </div>
                </div>
            `;
            // Stop polling
            if (indexProgressInterval) {
                clearInterval(indexProgressInterval);
                indexProgressInterval = null;
            }
            // Show error notification
            showNotification(knowledgeT('knowledge.indexBuildFailed', 'Index build failed') + ': ' + lastError.substring(0, 100), 'error');
            return;
        }
        

        // Process rebuild state first
        if (isRebuilding) {
            const rebuildTotal = status.rebuild_total || totalItems;
            const rebuildCurrent = status.rebuild_current || 0;
            const rebuildFailed = status.rebuild_failed || 0;
            const rebuildLastItemID = status.rebuild_last_item_id || '';
            const rebuildLastChunks = status.rebuild_last_chunks || 0;
            const rebuildStartTime = status.rebuild_start_time || '';

            // Calculate progress percentage (using rebuild progress)
            let rebuildProgress = progressPercent;
            if (rebuildTotal > 0) {
                rebuildProgress = (rebuildCurrent / rebuildTotal) * 100;
            }

            progressContainer.innerHTML = `
                <div class="knowledge-index-progress">
                    <div class="progress-header">
                        <span class="progress-icon">🔨</span>
                        <span class="progress-text">${knowledgeT('knowledge.buildingIndexProgress', 'Building index: {current}/{total} ({percent}%) - Failed: {failed}').replace('{current}', String(rebuildCurrent)).replace('{total}', String(rebuildTotal)).replace('{percent}', String(rebuildProgress.toFixed(1))).replace('{failed}', String(rebuildFailed))}</span>
                    </div>
                    <div class="progress-bar-container">
                        <div class="progress-bar" style="width: ${rebuildProgress}%"></div>
                    </div>
                    <div class="progress-hint">
                        ${rebuildLastItemID ? knowledgeT('knowledge.processingItem', 'Processing: {id}... ({chunks} chunks)').replace('{id}', escapeHtml(rebuildLastItemID.substring(0, 36))).replace('{chunks}', String(rebuildLastChunks)) : knowledgeT('knowledge.processing', 'Processing...')}
                        ${rebuildStartTime ? `<br>${knowledgeT('knowledge.startTimeLabel', 'Start time：{time}').replace('{time}', String(new Date(rebuildStartTime).toLocaleString()))}` : ''}
                    </div>
                </div>
            `;

            // Continue polling while rebuilding
            if (!indexProgressInterval) {
                indexProgressInterval = setInterval(updateIndexProgress, 2000);
            }
            return;
        }
        
        if (isComplete) {
            progressContainer.innerHTML = `
                <div class="knowledge-index-progress-complete">
                    <span class="progress-icon">✅</span>
                    <span class="progress-text">${knowledgeT('knowledge.indexBuildComplete', 'Index build complete ({indexed}/{total})').replace('{indexed}', String(indexedItems)).replace('{total}', String(totalItems))}</span>
                </div>
            `;
            // Stop polling when done
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
                        <span class="progress-text">${knowledgeT('knowledge.buildingIndexSimple', 'Building index: {current}/{total} ({percent}%)').replace('{current}', String(indexedItems)).replace('{total}', String(totalItems)).replace('{percent}', String(progressPercent.toFixed(1)))}</span>
                    </div>
                    <div class="progress-bar-container">
                        <div class="progress-bar" style="width: ${progressPercent}%"></div>
                    </div>
                    <div class="progress-hint">
                        ${knowledgeT('knowledge.indexDoneHint', 'Once the index has been built, the semantic search feature will be available')}
                        ${showResume ? `<br><button type="button" onclick="buildKnowledgeIndex()" style="margin-top:8px;background:#007bff;color:#fff;border:none;padding:4px 10px;border-radius:4px;cursor:pointer;font-size:12px;">${knowledgeT('knowledge.buildIndex', 'Build index')}</button>` : ''}
                    </div>
                </div>
            `;
            
            // If polling has not started yet, start polling
            if (!indexProgressInterval) {
                indexProgressInterval = setInterval(updateIndexProgress, 3000); // Refresh every 3 seconds
            }
        }
    } catch (error) {
        // Display error message
        console.error('Failed to get index status:', error);
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
                        <span style="font-weight: bold; color: #c00;">${knowledgeT('knowledge.cannotGetIndexStatus', 'Unable to get index status')}</span>
                    </div>
                    <div style="color: #666; font-size: 14px;">
                        ${knowledgeT('knowledge.cannotGetIndexStatusHint', 'Unable to connect to the server to get index status, please check your network connection or refresh the page.')}
                    </div>
                </div>
            `;
        }
        // Stop polling
        if (indexProgressInterval) {
            clearInterval(indexProgressInterval);
            indexProgressInterval = null;
        }
    }
}

// Stop index progress polling
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

// Select knowledge category
function selectKnowledgeCategory(category) {
    const trigger = document.getElementById('knowledge-category-filter-trigger');
    const wrapper = document.getElementById('knowledge-category-filter-wrapper');
    const dropdown = document.getElementById('knowledge-category-filter-dropdown');
    
    if (trigger && wrapper && dropdown) {
        const displayText = category || knowledgeT('knowledgePage.all', 'all');
        trigger.querySelector('span').textContent = displayText;
        wrapper.classList.remove('open');
        
        // Update selected state
        dropdown.querySelectorAll('.custom-select-option').forEach(opt => {
            opt.classList.remove('selected');
            if (opt.getAttribute('data-value') === category) {
                opt.classList.add('selected');
            }
        });
    }
    // Reset to first page when switching category (if a category is selected, the API will return all items of that category)
    loadKnowledgeItems(category, 1, knowledgePagination.pageSize);
}

// Filter knowledge items
function filterKnowledgeItems() {
    const wrapper = document.getElementById('knowledge-category-filter-wrapper');
    if (wrapper) {
        const selectedOption = wrapper.querySelector('.custom-select-option.selected');
        const category = selectedOption ? selectedOption.getAttribute('data-value') : '';
        // Reset to first page
        loadKnowledgeItems(category, 1, knowledgePagination.pageSize);
    }
}

// Handle search input (with debounce)
function handleKnowledgeSearchInput() {
    const searchInput = document.getElementById('knowledge-search');
    const searchTerm = searchInput?.value.trim() || '';
    
    // Clear the previous timer
    if (knowledgeSearchTimeout) {
        clearTimeout(knowledgeSearchTimeout);
    }
    
    // If the search box is empty, immediately restore the list
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
    
    // When there is a search term, delay 500ms before executing the search (debounce)
    knowledgeSearchTimeout = setTimeout(() => {
        searchKnowledgeItems();
    }, 500);
}

// Search knowledge items (backend keyword matching, search in all data)
async function searchKnowledgeItems() {
    const searchInput = document.getElementById('knowledge-search');
    const searchTerm = searchInput?.value.trim() || '';
    
    if (!searchTerm) {
        // Restore original list (reset to first page)
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
        // Get the currently selected category
        const wrapper = document.getElementById('knowledge-category-filter-wrapper');
        let category = '';
        if (wrapper) {
            const selectedOption = wrapper.querySelector('.custom-select-option.selected');
            category = selectedOption ? selectedOption.getAttribute('data-value') : '';
        }
        
        // Call backend API to perform full search
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
            throw new Error(knowledgeT('knowledge.searchFailed', 'Search failed'));
        }
        
        const data = await response.json();
        
        // Check whether the knowledge base feature is enabled
        if (data.enabled === false) {
            renderKnowledgeNotEnabledState(document.getElementById('knowledge-items-list'));
            return;
        }
        
        // Process search results
        const categoriesWithItems = data.categories || [];
        
        // Render search results
        const container = document.getElementById('knowledge-items-list');
        if (!container) return;
        
        if (categoriesWithItems.length === 0) {
            container.innerHTML = `
                <div class="empty-state" style="text-align: center; padding: 40px 20px;">
                    <div style="font-size: 48px; margin-bottom: 20px;">🔍</div>
                    <h3 style="margin-bottom: 10px;">${knowledgeT('knowledge.searchNoMatch', 'No matching knowledge item found')}</h3>
                    <p style="color: #999;">${knowledgeT('knowledge.searchNoResultsHint', 'Keyword "{term}" has no matching results in all data').replace('{term}', escapeHtml(searchTerm))}</p>
                    <p style="color: #999; margin-top: 10px; font-size: 0.9em;">${knowledgeT('knowledge.searchTryOther', 'Please try other keywords, or use the category filter feature')}</p>
                </div>
            `;
        } else {
            // Calculate total item count and category count
            const totalItems = categoriesWithItems.reduce((sum, cat) => sum + (cat.items?.length || 0), 0);
            const categoryCount = categoriesWithItems.length;
            
            // Update statistics information
            updateKnowledgeStats(categoriesWithItems, categoryCount);
            
            // Render search results
            renderKnowledgeItemsByCategories(categoriesWithItems);
        }
        
        // Hide pagination when searching (because search results display all matching results)
        const paginationContainer = document.getElementById('knowledge-pagination');
        if (paginationContainer) {
            paginationContainer.innerHTML = '';
        }
        
    } catch (error) {
        console.error('Failed to search knowledge items:', error);
        showNotification(knowledgeT('knowledge.searchFailed', 'Search failed') + ': ' + error.message, 'error');
    }
}

// Refresh knowledge base
async function refreshKnowledgeBase() {
    try {
        showNotification(knowledgeT('knowledge.scanStarting', 'Scanning knowledge base...'), 'info');
        const response = await apiFetch('/api/knowledge/scan', {
            method: 'POST'
        });
        if (!response.ok) {
            throw new Error(knowledgeT('knowledge.scanFailed', 'Failed to scan knowledge base'));
        }
        const data = await response.json();
        // Display different prompts based on the returned message
        if (data.items_to_index && data.items_to_index > 0) {
            showNotification(knowledgeT('knowledge.scanDoneIndexing', 'Scan complete, starting to index {count} newly added or updated knowledge items').replace('{count}', String(data.items_to_index)), 'success');
        } else {
            showNotification(data.message || knowledgeT('knowledge.scanDoneNoChange', 'Scan complete, no new or updated items to index'), 'success');
        }
        // Reload knowledge items (reset to first page)
        await loadKnowledgeCategories();
        await loadKnowledgeItems(knowledgePagination.currentCategory, 1, knowledgePagination.pageSize);
        
        // Stop existing polling
        if (indexProgressInterval) {
            clearInterval(indexProgressInterval);
            indexProgressInterval = null;
        }
        
        // If there are items that need to be indexed, wait a short while and then immediately update the progress
        if (data.items_to_index && data.items_to_index > 0) {
            await new Promise(resolve => setTimeout(resolve, 500));
            updateIndexProgress();
            // Start polling progress (refresh every 2 seconds)
            if (!indexProgressInterval) {
                indexProgressInterval = setInterval(updateIndexProgress, 2000);
            }
        } else {
            // No items to index, but still update once to show the current state
            updateIndexProgress();
        }
    } catch (error) {
        console.error('Failed to refresh knowledge base:', error);
        showNotification(knowledgeT('knowledge.refreshFailed', 'Failed to refresh knowledge base') + ': ' + error.message, 'error');
    }
}

// Start knowledge base indexing task (mode: missing | full)
async function startKnowledgeIndexJob(mode = 'missing') {
    const fullRebuild = mode === 'full';
    const confirmMessage = fullRebuild
        ? knowledgeT('knowledge.rebuildIndexFullConfirm', 'This will re-index all knowledge items, which may take a long time. Continue?')
        : knowledgeT('knowledge.buildIndexConfirm', 'An index will be built for knowledge items whose vectors have not yet been generated. Do you want to continue?');
    if (!confirm(confirmMessage)) {
        return;
    }

    const actionLabel = fullRebuild ? knowledgeT('knowledge.fullRebuildIndexLabel', 'Full index rebuild') : knowledgeT('knowledge.buildIndexLabel', 'Build index');
    showNotification(knowledgeT('knowledge.indexJobStarting', 'Starting {label}...').replace('{label}', actionLabel), 'info');

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
                    <span class="progress-text">${knowledgeT('knowledge.indexJobPreparing', 'Currently {label}: preparing...').replace('{label}', actionLabel)}</span>
                </div>
                <div class="progress-bar-container">
                    <div class="progress-bar" style="width: 0%"></div>
                </div>
                <div class="progress-hint">${knowledgeT('knowledge.indexDoneHint', 'Once the index has been built, the semantic search feature will be available')}</div>
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
        showNotification(payload.error || knowledgeT('knowledge.indexJobInProgress', 'An indexing task is already in progress, please wait for it to complete'), 'warning');
        updateIndexProgress();
        return;
    }
    if (!response.ok) {
        throw new Error(payload.error || knowledgeT('knowledge.indexJobFailed', '{label} failed').replace('{label}', actionLabel));
    }

    showNotification(payload.message || knowledgeT('knowledge.indexJobStarted', '{label} has started and will proceed in the background').replace('{label}', actionLabel), 'success');
    await new Promise(resolve => setTimeout(resolve, 500));
    updateIndexProgress();
    if (!indexProgressInterval) {
        indexProgressInterval = setInterval(updateIndexProgress, 2000);
    }
}

// Build index (default: only process knowledge items that don't yet have vectors)
async function buildKnowledgeIndex() {
    try {
        await startKnowledgeIndexJob('missing');
    } catch (error) {
        console.error('Failed to build index:', error);
        showNotification(knowledgeT('knowledge.buildIndexFailedMsg', 'Failed to build index') + ': ' + error.message, 'error');
    }
}

// Full index rebuild (explicit opt-in)
async function rebuildKnowledgeIndexFull() {
    try {
        await startKnowledgeIndexJob('full');
    } catch (error) {
        console.error('Full index rebuild failed:', error);
        showNotification(knowledgeT('knowledge.fullRebuildIndexFailedMsg', 'Full index rebuild failed') + ': ' + error.message, 'error');
    }
}

// Show the add knowledge item modal
function showAddKnowledgeItemModal() {
    if (typeof requirePermission === 'function' && !requirePermission('knowledge:write')) return;
    currentEditingItemId = null;
    document.getElementById('knowledge-item-modal-title').textContent = knowledgeT('knowledgeItemModal.addKnowledge', 'Add knowledge');
    document.getElementById('knowledge-item-category').value = '';
    document.getElementById('knowledge-item-title').value = '';
    document.getElementById('knowledge-item-content').value = '';
    openAppModal('knowledge-item-modal');
}

// Edit knowledge item
async function editKnowledgeItem(id) {
    try {
        currentEditingItemId = id;
        document.getElementById('knowledge-item-modal-title').textContent = knowledgeT('knowledgeItemModal.editKnowledge', 'Edit knowledge');
        document.getElementById('knowledge-item-category').value = '';
        document.getElementById('knowledge-item-title').value = '';
        document.getElementById('knowledge-item-content').value = '';
        openAppModal('knowledge-item-modal', { focus: false });
        const response = await apiFetch(`/api/knowledge/items/${id}`);
        if (!response.ok) {
            throw new Error(knowledgeT('knowledge.getItemsFailed', 'Failed to get knowledge item'));
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
        console.error('Failed to edit knowledge item:', error);
        showNotification(knowledgeT('knowledge.editItemFailed', 'Failed to edit knowledge item') + ': ' + error.message, 'error');
    }
}

// Save knowledge item
async function saveKnowledgeItem() {
    if (typeof requirePermission === 'function' && !requirePermission('knowledge:write')) return;
    // Prevent duplicate submission
    if (isSavingKnowledgeItem) {
        showNotification(knowledgeT('knowledge.savingInProgress', 'Saving in progress, please do not click again...'), 'warning');
        return;
    }
    
    const category = document.getElementById('knowledge-item-category').value.trim();
    const title = document.getElementById('knowledge-item-title').value.trim();
    const content = document.getElementById('knowledge-item-content').value.trim();
    
    if (!category || !title || !content) {
        showNotification(knowledgeT('knowledge.fillRequiredFields', 'Please fill in all required fields'), 'error');
        return;
    }
    
    // Set save-in-progress flag
    isSavingKnowledgeItem = true;
    
    // Get the save button and cancel button
    const saveButton = document.querySelector('#knowledge-item-modal .modal-footer .btn-primary');
    const cancelButton = document.querySelector('#knowledge-item-modal .modal-footer .btn-secondary');
    const modal = document.getElementById('knowledge-item-modal');
    
    const originalButtonText = saveButton ? saveButton.textContent : knowledgeT('common.save', 'Save');
    const originalButtonDisabled = saveButton ? saveButton.disabled : false;
    
    // Disable all input fields and buttons
    const categoryInput = document.getElementById('knowledge-item-category');
    const titleInput = document.getElementById('knowledge-item-title');
    const contentInput = document.getElementById('knowledge-item-content');
    
    if (categoryInput) categoryInput.disabled = true;
    if (titleInput) titleInput.disabled = true;
    if (contentInput) contentInput.disabled = true;
    if (cancelButton) cancelButton.disabled = true;
    
    // Set save button loading state
    if (saveButton) {
        saveButton.disabled = true;
        saveButton.style.opacity = '0.6';
        saveButton.style.cursor = 'not-allowed';
        saveButton.textContent = knowledgeT('knowledge.savingLabel', 'Saving...');
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
            throw new Error(errorData.error || knowledgeT('knowledge.saveItemFailed', 'Failed to save knowledge item'));
        }
        
        const item = await response.json();
        const action = currentEditingItemId ? knowledgeT('knowledge.actionUpdated', 'update') : knowledgeT('knowledge.actionCreated', 'create');
        const newItemCategory = item.category || category; // Save the newly added knowledge item category
        
        // Get the current filter state so it persists after refresh
        const currentCategory = document.getElementById('knowledge-category-filter-wrapper');
        let selectedCategory = '';
        if (currentCategory) {
            const selectedOption = currentCategory.querySelector('.custom-select-option.selected');
            if (selectedOption) {
                selectedCategory = selectedOption.getAttribute('data-value') || '';
            }
        }
        
        // Immediately close the modal, giving the user clear feedback
        closeKnowledgeItemModal();
        
        // Show loading state and refresh data (wait for completion to ensure data synchronization)
        const itemsListContainer = document.getElementById('knowledge-items-list');
        const originalContent = itemsListContainer ? itemsListContainer.innerHTML : '';
        
        if (itemsListContainer) {
            itemsListContainer.innerHTML = '<div class="loading-spinner">' + knowledgeT('knowledge.refreshing', 'Refreshing...') + '</div>';
        }
        
        try {
            // First refresh the categories, then refresh the knowledge items
            console.log('Start refreshing knowledge base data...');
            await loadKnowledgeCategories();
            console.log('Category refresh completed, starting knowledge item refresh...');
            
            // If the newly added knowledge item is not in the currently filtered category, switch to that category to display it
            let categoryToShow = selectedCategory;
            if (!currentEditingItemId && selectedCategory && selectedCategory !== '' && newItemCategory !== selectedCategory) {
                // Newly added knowledge item: if the current filter is not this category, switch to the new knowledge item's category
                categoryToShow = newItemCategory;
                // Update filter display (don't trigger loading, because we'll manually load below)
                const trigger = document.getElementById('knowledge-category-filter-trigger');
                const wrapper = document.getElementById('knowledge-category-filter-wrapper');
                const dropdown = document.getElementById('knowledge-category-filter-dropdown');
                if (trigger && wrapper && dropdown) {
                    trigger.querySelector('span').textContent = newItemCategory || knowledgeT('knowledgePage.all', 'all');
                    dropdown.querySelectorAll('.custom-select-option').forEach(opt => {
                        opt.classList.remove('selected');
                        if (opt.getAttribute('data-value') === newItemCategory) {
                            opt.classList.add('selected');
                        }
                    });
                }
                showNotification(knowledgeT('knowledge.saveSuccessSwitchCategory', '✅ {action} successful! Switched to category "{category}" to view the newly added knowledge item.').replace('{action}', action).replace('{category}', newItemCategory), 'success');
            }
            
            // Refresh knowledge item list (reset to first page)
            await loadKnowledgeItems(categoryToShow, 1, knowledgePagination.pageSize);
            console.log('Knowledge item refresh completed');
        } catch (err) {
            console.error('Failed to refresh data:', err);
            // If refresh fails, restore original content
            if (itemsListContainer && originalContent) {
                itemsListContainer.innerHTML = originalContent;
            }
            showNotification(knowledgeT('knowledge.savedButRefreshFailed', '⚠️ Knowledge item saved, but refreshing the list failed, please manually refresh the page to view'), 'warning');
        }
        
    } catch (error) {
        console.error('Failed to save knowledge item:', error);
        showNotification('❌ ' + knowledgeT('knowledge.saveItemFailed', 'Failed to save knowledge item') + ': ' + error.message, 'error');

        // If the notification system is unavailable, use alert
        if (typeof window.showNotification !== 'function') {
            alert('❌ ' + knowledgeT('knowledge.saveItemFailed', 'Failed to save knowledge item') + ': ' + error.message);
        }
        
        // Restore input field and button state (don't close the modal on error, let the user modify and retry)
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
        // Clear the save-in-progress flag
        isSavingKnowledgeItem = false;
    }
}

// Delete knowledge item
async function deleteKnowledgeItem(id) {
    if (!confirm(knowledgeT('knowledge.deleteItemConfirm', 'Are you sure you want to delete this knowledge item?'))) {
        return;
    }
    
    // Find the knowledge item card to be deleted and the delete button
    const itemCard = document.querySelector(`.knowledge-item-card[data-id="${id}"]`);
    const deleteButton = itemCard ? itemCard.querySelector('.knowledge-item-delete-btn') : null;
    const categorySection = itemCard ? itemCard.closest('.knowledge-category-section') : null;
    let originalDisplay = '';
    let originalOpacity = '';
    let originalButtonOpacity = '';
    
    // Set the loading state of the delete button
    if (deleteButton) {
        originalButtonOpacity = deleteButton.style.opacity;
        deleteButton.style.opacity = '0.5';
        deleteButton.style.cursor = 'not-allowed';
        deleteButton.disabled = true;
        
        // Add loading animation
        const svg = deleteButton.querySelector('svg');
        if (svg) {
            svg.style.animation = 'spin 1s linear infinite';
        }
    }
    
    // Immediately remove the item from the UI (optimistic update)
    if (itemCard) {
        originalDisplay = itemCard.style.display;
        originalOpacity = itemCard.style.opacity;
        itemCard.style.transition = 'opacity 0.3s ease-out, transform 0.3s ease-out';
        itemCard.style.opacity = '0';
        itemCard.style.transform = 'translateX(-20px)';
        
        // Wait for animation to complete before removing
        setTimeout(() => {
            if (itemCard.parentElement) {
                itemCard.remove();
                
                // Check if the category still has items, if not, hide the category title
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
                        // Update category count
                        const categoryCount = categorySection.querySelector('.knowledge-category-count');
                        if (categoryCount) {
                            const newCount = remainingItems.length;
                            categoryCount.textContent = knowledgeT('knowledge.itemsCountUnit', '{count} items').replace('{count}', String(newCount));
                        }
                    }
                }
                
                // Don't update the statistics info here; wait for the data to be reloaded and then be updated by the correct logic
            }
        }, 300);
    }
    
    try {
        const response = await apiFetch(`/api/knowledge/items/${id}`, {
            method: 'DELETE'
        });
        
        if (!response.ok) {
            const errorData = await response.json().catch(() => ({}));
            throw new Error(errorData.error || knowledgeT('knowledge.deleteItemFailed', 'Failed to delete knowledge item'));
        }

        // Show success notification
        showNotification(knowledgeT('knowledge.deleteItemSuccess', '✅ Deletion successful! The knowledge item has been removed from the system.'), 'success');
        
        // Reload data to ensure data synchronization (keep the current page number)
        await loadKnowledgeCategories();
        await loadKnowledgeItems(knowledgePagination.currentCategory, knowledgePagination.currentPage, knowledgePagination.pageSize);
        
    } catch (error) {
        console.error('Failed to delete knowledge item:', error);
        
        // If deletion fails, restore the item display
        if (itemCard && originalDisplay !== 'none') {
            itemCard.style.display = originalDisplay || '';
            itemCard.style.opacity = originalOpacity || '1';
            itemCard.style.transform = '';
            itemCard.style.transition = '';
            
            // If the category was removed, it needs to be restored
            if (categorySection && !categorySection.parentElement) {
                // Need to reload to restore (keep current pagination state)
                await loadKnowledgeItems(knowledgePagination.currentCategory, knowledgePagination.currentPage, knowledgePagination.pageSize);
            }
        }
        
        // Restore delete button state
        if (deleteButton) {
            deleteButton.style.opacity = originalButtonOpacity || '';
            deleteButton.style.cursor = '';
            deleteButton.disabled = false;
            const svg = deleteButton.querySelector('svg');
            if (svg) {
                svg.style.animation = '';
            }
        }
        
        showNotification('❌ ' + knowledgeT('knowledge.deleteItemFailed', 'Failed to delete knowledge item') + ': ' + error.message, 'error');
    }
}

// Temporarily update statistics info (after deletion)
function updateKnowledgeStatsAfterDelete() {
    const statsContainer = document.getElementById('knowledge-stats');
    if (!statsContainer) return;
    
    const allItems = document.querySelectorAll('.knowledge-item-card');
    const allCategories = document.querySelectorAll('.knowledge-category-section');
    
    const totalItems = allItems.length;
    const categoryCount = allCategories.length;
    
    // Calculate total content size (simplified here, should actually be fetched from the server)
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

// Close knowledge item modal box
function closeKnowledgeItemModal() {
    closeAppModal('knowledge-item-modal');
    
    // Reset edit state
    currentEditingItemId = null;
    isSavingKnowledgeItem = false;
    
    // Restore all input fields and button states
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
        saveButton.textContent = knowledgeT('common.save', 'Save');
    }
    if (cancelButton) {
        cancelButton.disabled = false;
    }
}

// Load retrieval log
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
            throw new Error(knowledgeT('retrievalLogs.getLogsFailed', 'Failed to retrieve search logs'));
        }
        const data = await response.json();
        renderRetrievalLogs(data.logs || []);
    } catch (error) {
        console.error('Failed to load retrieval log:', error);
        // Even if loading fails, display the empty state instead of always showing "Loading..."
        renderRetrievalLogs([]);
        // Only display error notification when filter condition is non-empty (avoid displaying errors when there is no data)
        if (conversationId || messageId) {
            showNotification(_t('retrievalLogs.loadError') + ': ' + error.message, 'error');
        }
    }
}

// Render retrieval log
function renderRetrievalLogs(logs) {
    const container = document.getElementById('retrieval-logs-list');
    if (!container) return;
    
    // Update statistics info (update even if it's an empty array)
    updateRetrievalStats(logs);
    
    if (logs.length === 0) {
        container.innerHTML = '<div class="empty-state">' + _t('retrievalLogs.noRecords') + '</div>';
        retrievalLogsData = [];
        return;
    }
    
    // Save log data for use in the detail view
    retrievalLogsData = logs;
    
    container.innerHTML = logs.map((log, index) => {
        // Handle retrievedItems: may be an array, string array, or special marker
        let itemCount = 0;
        let hasResults = false;
        
        if (log.retrievedItems) {
            if (Array.isArray(log.retrievedItems)) {
                // Filter out special markers
                const realItems = log.retrievedItems.filter(id => id !== '_has_results');
                itemCount = realItems.length;
                // If there is a special marker, it indicates there is a result but the ID is unknown, display as "has result"
                if (log.retrievedItems.includes('_has_results')) {
                    hasResults = true;
                    // If there is a real ID, use the real count; otherwise display as "has results" (do not display the specific count)
                    if (itemCount === 0) {
                        itemCount = -1; // -1 indicates there are results but the count is unknown
                    }
                } else {
                    hasResults = itemCount > 0;
                }
            } else if (typeof log.retrievedItems === 'string') {
                // If it's a string, try to parse JSON
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
                    // Parse failed, ignore
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

// Update retrieval statistics information
function updateRetrievalStats(logs) {
    const statsContainer = document.getElementById('retrieval-stats');
    if (!statsContainer) return;
    
    const totalLogs = logs.length;
    // Determine whether there is a result: check the retrievedItems array, after filtering out special markers the length > 0, or it contains special markers
    const successfulLogs = logs.filter(log => {
        if (!log.retrievedItems) return false;
        if (Array.isArray(log.retrievedItems)) {
            const realItems = log.retrievedItems.filter(id => id !== '_has_results');
            return realItems.length > 0 || log.retrievedItems.includes('_has_results');
        }
        return false;
    }).length;
    // Calculate the total number of knowledge items (only count real IDs, excluding special tokens)
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
            <span class="retrieval-stat-label" data-i18n="retrievalLogs.totalRetrievals">Total retrievals</span>
            <span class="retrieval-stat-value">${totalLogs}</span>
        </div>
        <div class="retrieval-stat-item">
            <span class="retrieval-stat-label" data-i18n="retrievalLogs.successRetrievals">Success</span>
            <span class="retrieval-stat-value text-success">${successfulLogs}</span>
        </div>
        <div class="retrieval-stat-item">
            <span class="retrieval-stat-label" data-i18n="retrievalLogs.successRate">Success rate</span>
            <span class="retrieval-stat-value">${successRate}%</span>
        </div>
        <div class="retrieval-stat-item">
            <span class="retrieval-stat-label" data-i18n="retrievalLogs.retrievedItems">Items retrieved</span>
            <span class="retrieval-stat-value">${totalItems}</span>
        </div>
    `;
    if (typeof window.applyTranslations === 'function') {
        window.applyTranslations(statsContainer);
    }
}

// Get relative time
function getTimeAgo(timeStr) {
    if (!timeStr) return '';
    
    // Process time string, supports multiple formats
    let date;
    if (typeof timeStr === 'string') {
        // First try direct parsing (supports RFC3339/ISO8601 format)
        date = new Date(timeStr);
        
        // If parsing fails, try other formats
        if (isNaN(date.getTime())) {
            // SQLite format: "2006-01-02 15:04:05" or with timezone
            const sqliteMatch = timeStr.match(/(\d{4}-\d{2}-\d{2}[\sT]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:[+-]\d{2}:\d{2}|Z)?)/);
            if (sqliteMatch) {
                let timeStr2 = sqliteMatch[1].replace(' ', 'T');
                // If there is no timezone information, add Z to indicate UTC
                if (!timeStr2.includes('Z') && !timeStr2.match(/[+-]\d{2}:\d{2}$/)) {
                    timeStr2 += 'Z';
                }
                date = new Date(timeStr2);
            }
        }
        
        // If it still fails, try a more lenient format
        if (isNaN(date.getTime())) {
            // Try to match "YYYY-MM-DD HH:MM:SS" format
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
    
    // Check if the date is valid
    if (isNaN(date.getTime())) {
        return formatTime(timeStr);
    }
    
    // Check whether the date is reasonable (not before 1970, not too far in the future)
    const year = date.getFullYear();
    if (year < 1970 || year > 2100) {
        return formatTime(timeStr);
    }
    
    const now = new Date();
    const diff = now - date;
    
    // If the time difference is negative or too large (possibly a parsing error), return the formatted time
    if (diff < 0 || diff > 365 * 24 * 60 * 60 * 1000 * 10) { // More than 10 years is considered an error
        return formatTime(timeStr);
    }
    
    const seconds = Math.floor(diff / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);
    const days = Math.floor(hours / 24);
    
    if (days > 0) return knowledgeT('common.daysAgo', '{{n}} days ago').replace('{{n}}', String(days));
    if (hours > 0) return knowledgeT('common.hoursAgo', '{{n}} hours ago').replace('{{n}}', String(hours));
    if (minutes > 0) return knowledgeT('common.minutesAgo', '{{n}} minutes ago').replace('{{n}}', String(minutes));
    return knowledgeT('common.justNow', 'just now');
}

// Truncate ID display
function truncateId(id) {
    if (!id || id.length <= 16) return id;
    return id.substring(0, 8) + '...' + id.substring(id.length - 8);
}

// Filter search logs
function filterRetrievalLogs() {
    const conversationId = document.getElementById('retrieval-logs-conversation-id').value.trim();
    const messageId = document.getElementById('retrieval-logs-message-id').value.trim();
    loadRetrievalLogs(conversationId, messageId);
}

// Refresh retrieval log
function refreshRetrievalLogs() {
    filterRetrievalLogs();
}

// Delete retrieval log
async function deleteRetrievalLog(id, index) {
    if (!confirm(_t('retrievalLogs.deleteConfirm'))) {
        return;
    }
    
    // Find the log card to be deleted and the delete button
    const logCard = document.querySelector(`.retrieval-log-card[data-index="${index}"]`);
    const deleteButton = logCard ? logCard.querySelector('.retrieval-log-delete-btn') : null;
    let originalButtonOpacity = '';
    let originalButtonDisabled = false;
    
    // Set the loading state of the delete button
    if (deleteButton) {
        originalButtonOpacity = deleteButton.style.opacity;
        originalButtonDisabled = deleteButton.disabled;
        deleteButton.style.opacity = '0.5';
        deleteButton.style.cursor = 'not-allowed';
        deleteButton.disabled = true;
        
        // Add loading animation
        const svg = deleteButton.querySelector('svg');
        if (svg) {
            svg.style.animation = 'spin 1s linear infinite';
        }
    }
    
    // Immediately remove the item from the UI (optimistic update)
    if (logCard) {
        logCard.style.transition = 'opacity 0.3s ease-out, transform 0.3s ease-out';
        logCard.style.opacity = '0';
        logCard.style.transform = 'translateX(-20px)';
        
        // Wait for animation to complete before removing
        setTimeout(() => {
            if (logCard.parentElement) {
                logCard.remove();
                
                // Update statistics info (temporary update, will be reloaded later)
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
            throw new Error(errorData.error || knowledgeT('retrievalLogs.deleteError', 'Failed to delete retrieval log'));
        }
        
        // Show success notification
        showNotification(knowledgeT('retrievalLogs.deleteSuccess', '✅ Deletion successful! The retrieval record has been removed from the system.'), 'success');
        
        // Remove this item from memory
        if (retrievalLogsData && index >= 0 && index < retrievalLogsData.length) {
            retrievalLogsData.splice(index, 1);
        }
        
        // Reload data to ensure data synchronization
        const conversationId = document.getElementById('retrieval-logs-conversation-id')?.value.trim() || '';
        const messageId = document.getElementById('retrieval-logs-message-id')?.value.trim() || '';
        await loadRetrievalLogs(conversationId, messageId);
        
    } catch (error) {
        console.error('Failed to delete retrieval log:', error);
        
        // If deletion fails, restore the item display
        if (logCard) {
            logCard.style.opacity = '1';
            logCard.style.transform = '';
            logCard.style.transition = '';
        }
        
        // Restore delete button state
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

// Temporarily update statistics info (after deletion)
function updateRetrievalStatsAfterDelete() {
    const statsContainer = document.getElementById('retrieval-stats');
    if (!statsContainer) return;
    
    const allLogs = document.querySelectorAll('.retrieval-log-card');
    const totalLogs = allLogs.length;
    
    // Calculate successful retrieval count
    const successfulLogs = Array.from(allLogs).filter(card => {
        return card.classList.contains('has-results');
    }).length;
    
    // Calculate total knowledge item count (simplified handling, should actually be fetched from the server)
    const totalItems = Array.from(allLogs).reduce((sum, card) => {
        const badge = card.querySelector('.retrieval-log-result-badge');
        if (badge && badge.classList.contains('success')) {
            const text = badge.textContent.trim();
            const match = text.match(/(\d+)/);
            if (match) {
                return sum + parseInt(match[1], 10);
            }
            return sum + 1; // Has results but the count is unknown (e.g. "Has results" / "Has results")
        }
        return sum;
    }, 0);
    
    const successRate = totalLogs > 0 ? ((successfulLogs / totalLogs) * 100).toFixed(1) : 0;
    
    statsContainer.innerHTML = `
        <div class="retrieval-stat-item">
            <span class="retrieval-stat-label" data-i18n="retrievalLogs.totalRetrievals">Total retrievals</span>
            <span class="retrieval-stat-value">${totalLogs}</span>
        </div>
        <div class="retrieval-stat-item">
            <span class="retrieval-stat-label" data-i18n="retrievalLogs.successRetrievals">Success</span>
            <span class="retrieval-stat-value text-success">${successfulLogs}</span>
        </div>
        <div class="retrieval-stat-item">
            <span class="retrieval-stat-label" data-i18n="retrievalLogs.successRate">Success rate</span>
            <span class="retrieval-stat-value">${successRate}%</span>
        </div>
        <div class="retrieval-stat-item">
            <span class="retrieval-stat-label" data-i18n="retrievalLogs.retrievedItems">Items retrieved</span>
            <span class="retrieval-stat-value">${totalItems}</span>
        </div>
    `;
    if (typeof window.applyTranslations === 'function') {
        window.applyTranslations(statsContainer);
    }
}

// Show retrieval log details
async function showRetrievalLogDetails(index) {
    if (!retrievalLogsData || index < 0 || index >= retrievalLogsData.length) {
        showNotification(_t('retrievalLogs.detailError'), 'error');
        return;
    }
    
    const log = retrievalLogsData[index];
    
    // Get details of retrieved knowledge items
    let retrievedItemsDetails = [];
    if (log.retrievedItems && Array.isArray(log.retrievedItems)) {
        const realItemIds = log.retrievedItems.filter(id => id !== '_has_results');
        if (realItemIds.length > 0) {
            try {
                // Batch get knowledge item details
                const itemPromises = realItemIds.map(async (itemId) => {
                    try {
                        const response = await apiFetch(`/api/knowledge/items/${itemId}`);
                        if (response.ok) {
                            return await response.json();
                        }
                        return null;
                    } catch (err) {
                        console.error(`Failed to get knowledge item ${itemId}:`, err);
                        return null;
                    }
                });
                
                const items = await Promise.all(itemPromises);
                retrievedItemsDetails = items.filter(item => item !== null);
            } catch (err) {
                console.error('Failed to batch retrieve knowledge item details:', err);
            }
        }
    }
    
    // Show details modal
    showRetrievalLogDetailsModal(log, retrievedItemsDetails);
}

// Show retrieval log details modal
function showRetrievalLogDetailsModal(log, retrievedItems) {
    // Create or get modal box
    let modal = document.getElementById('retrieval-log-details-modal');
    if (!modal) {
        modal = document.createElement('div');
        modal.id = 'retrieval-log-details-modal';
        modal.className = 'modal';
        modal.innerHTML = `
            <div class="modal-content" style="max-width: 900px; max-height: 90vh; overflow-y: auto;">
                <div class="modal-header">
                    <h2 data-i18n="retrievalLogs.detailsTitle">Retrieval details</h2>
                    <span class="modal-close" onclick="closeRetrievalLogDetailsModal()">&times;</span>
                </div>
                <div class="modal-body" id="retrieval-log-details-content">
                </div>
                <div class="modal-footer">
                    <button class="btn-secondary" onclick="closeRetrievalLogDetailsModal()" data-i18n="common.close">Close</button>
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
            // Extract content preview
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
                                      onclick="navigator.clipboard.writeText(${escapeJsStringAttr(log.conversationId)}); this.title='${knowledgeT('common.copied', 'Copied!')}'; setTimeout(() => this.title='${knowledgeT('retrievalLogs.clickToCopy', 'Click to copy')}', 2000);"
                                      title="${knowledgeT('retrievalLogs.clickToCopy', 'Click to copy')}" >${escapeHtml(log.conversationId)}</code>
                            </div>
                        ` : ''}
                        ${log.messageId ? `
                            <div style="padding: 12px; background: var(--bg-secondary); border-radius: 6px;">
                                <div style="font-size: 0.875rem; color: var(--text-secondary); margin-bottom: 4px;">${_t('retrievalLogs.messageId')}</div>
                                <code style="font-size: 0.8125rem; color: var(--text-primary); word-break: break-all; cursor: pointer;" 
                                      onclick="navigator.clipboard.writeText(${escapeJsStringAttr(log.messageId)}); this.title='${knowledgeT('common.copied', 'Copied!')}'; setTimeout(() => this.title='${knowledgeT('retrievalLogs.clickToCopy', 'Click to copy')}', 2000);"
                                      title="${knowledgeT('retrievalLogs.clickToCopy', 'Click to copy')}" >${escapeHtml(log.messageId)}</code>
                            </div>
                        ` : ''}
                    </div>
                </div>
            ` : ''}
            
            <div class="retrieval-detail-section">
                <h3 style="margin: 0 0 12px 0; font-size: 1.125rem; color: var(--text-primary);">${knowledgeT('retrievalLogs.retrievedItems', 'Retrieved knowledge items')} (${retrievedItems.length})</h3>
                ${itemsHtml}
            </div>
        </div>
    `;
    });
}

// Close the retrieval log details modal
function closeRetrievalLogDetailsModal() {
    closeAppModal('retrieval-log-details-modal');
}

// Click outside the modal to close it
window.addEventListener('click', function(event) {
    const modal = document.getElementById('retrieval-log-details-modal');
    if (event.target === modal) {
        closeRetrievalLogDetailsModal();
    }
});

// On a language switch, re-render the retrieval history list and statistics so dynamic content follows the language update; the knowledge management page's "not enabled" block already uses data-i18n and is updated automatically by applyTranslations(document)
document.addEventListener('languagechange', function () {
    var cur = typeof window.currentPage === 'function' ? window.currentPage() : (window.currentPage || '');
    if (cur === 'knowledge-retrieval-logs') {
        if (retrievalLogsData && retrievalLogsData.length >= 0) {
            renderRetrievalLogs(retrievalLogsData);
        }
    } else if (cur === 'knowledge-management') {
        // Only for the "knowledge base not enabled" state: data-i18n already exists and has been handled by applyTranslations; optionally re-apply once here for compatibility with legacy DOM
        var listEl = document.getElementById('knowledge-items-list');
        if (listEl && typeof window.applyTranslations === 'function') {
            window.applyTranslations(listEl);
        }
    }
});

// Load data when switching pages
if (typeof switchPage === 'function') {
    const originalSwitchPage = switchPage;
    window.switchPage = function(page) {
        originalSwitchPage(page);
        
        if (page === 'knowledge-management') {
            loadKnowledgeCategories();
            loadKnowledgeItems(knowledgePagination.currentCategory, 1, knowledgePagination.pageSize);
            updateIndexProgress(); // Update index progress
        } else if (page === 'knowledge-retrieval-logs') {
            loadRetrievalLogs();
            // Stop polling when switching to another page
            if (indexProgressInterval) {
                clearInterval(indexProgressInterval);
                indexProgressInterval = null;
            }
        } else {
            // Stop polling when switching to another page
            if (indexProgressInterval) {
                clearInterval(indexProgressInterval);
                indexProgressInterval = null;
            }
        }
    };
}

// Clean up timers when the page unloads
window.addEventListener('beforeunload', function() {
    if (indexProgressInterval) {
        clearInterval(indexProgressInterval);
        indexProgressInterval = null;
    }
});

// utility function
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
    
    // Process time string, supports multiple formats
    let date;
    if (typeof timeStr === 'string') {
        // First try direct parsing (supports RFC3339/ISO8601 format)
        date = new Date(timeStr);
        
        // If parsing fails, try other formats
        if (isNaN(date.getTime())) {
            // SQLite format: "2006-01-02 15:04:05" or with timezone
            const sqliteMatch = timeStr.match(/(\d{4}-\d{2}-\d{2}[\sT]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:[+-]\d{2}:\d{2}|Z)?)/);
            if (sqliteMatch) {
                let timeStr2 = sqliteMatch[1].replace(' ', 'T');
                // If there is no timezone information, add Z to indicate UTC
                if (!timeStr2.includes('Z') && !timeStr2.match(/[+-]\d{2}:\d{2}$/)) {
                    timeStr2 += 'Z';
                }
                date = new Date(timeStr2);
            }
        }
        
        // If it still fails, try a more lenient format
        if (isNaN(date.getTime())) {
            // Try to match "YYYY-MM-DD HH:MM:SS" format
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
    
    // If the date is invalid, check whether it is a zero-value time
    if (isNaN(date.getTime())) {
        // Check whether it is the string form of a zero value time
        if (typeof timeStr === 'string' && (timeStr.includes('0001-01-01') || timeStr.startsWith('0001'))) {
            return '';
        }
        console.warn('Unable to parse time:', timeStr);
        return '';
    }
    
    // Check whether the date is reasonable (not before 1970, not too far in the future)
    const year = date.getFullYear();
    if (year < 1970 || year > 2100) {
        // If it is a zero value time (0001-01-01), return an empty string, do not display
        if (year === 1) {
            return '';
        }
        console.warn('The time value is invalid:', timeStr, 'Parse as:', date);
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

// Display notification
function showNotification(message, type = 'info') {
    // If a global notification system exists (and it's not the current function), use it
    if (typeof window.showNotification === 'function' && window.showNotification !== showNotification) {
        window.showNotification(message, type);
        return;
    }
    
    // Otherwise use a custom toast notification
    showToastNotification(message, type);
}

// Display Toast notification
function showToastNotification(message, type = 'info') {
    // Create notification container (if not exists)
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
    
    // Create notification element
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
    
    // Auto remove (success messages display for 5 seconds, error messages display for 7 seconds, others display for 4 seconds)
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

// Add CSS animation (if it doesn't exist)
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

// Click outside the modal to close it
window.addEventListener('click', function(event) {
    const modal = document.getElementById('knowledge-item-modal');
    if (event.target === modal) {
        closeKnowledgeItemModal();
    }
});

// Switch to the settings page (used as a prompt when the feature is not enabled)
function switchToSettings() {
    if (typeof switchPage === 'function') {
        switchPage('settings');
        // Wait for the settings page to load, then switch to the knowledge base configuration section
        setTimeout(() => {
            if (typeof switchSettingsSection === 'function') {
                // Find the knowledge base configuration section (usually in basic settings)
                const knowledgeSection = document.querySelector('[data-section="knowledge"]');
                if (knowledgeSection) {
                    switchSettingsSection('knowledge');
                } else {
                    // If there is no separate knowledge base section, switch to basic settings
                    switchSettingsSection('basic');
                    // Scroll to the knowledge base configuration area
                    setTimeout(() => {
                        const knowledgeEnabledCheckbox = document.getElementById('knowledge-enabled');
                        if (knowledgeEnabledCheckbox) {
                            knowledgeEnabledCheckbox.scrollIntoView({ behavior: 'smooth', block: 'center' });
                            // highlight display
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

// Custom dropdown component interaction
document.addEventListener('DOMContentLoaded', function() {
    const wrapper = document.getElementById('knowledge-category-filter-wrapper');
    const trigger = document.getElementById('knowledge-category-filter-trigger');
    
    if (wrapper && trigger) {
        // Click trigger to open/close dropdown menu
        trigger.addEventListener('click', function(e) {
            e.stopPropagation();
            wrapper.classList.toggle('open');
        });
        
        // Click outside to close the dropdown menu
        document.addEventListener('click', function(e) {
            if (!wrapper.contains(e.target)) {
                wrapper.classList.remove('open');
            }
        });
        
        // Update selected state when selecting an option
        const dropdown = document.getElementById('knowledge-category-filter-dropdown');
        if (dropdown) {
            // Select the "All" option by default
            const defaultOption = dropdown.querySelector('.custom-select-option[data-value=""]');
            if (defaultOption) {
                defaultOption.classList.add('selected');
            }
            
            dropdown.addEventListener('click', function(e) {
                const option = e.target.closest('.custom-select-option');
                if (option) {
                    // Remove the previous selection state
                    dropdown.querySelectorAll('.custom-select-option').forEach(opt => {
                        opt.classList.remove('selected');
                    });
                    // Add selected state
                    option.classList.add('selected');
                }
            });
        }
    }
});
