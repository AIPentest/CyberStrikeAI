/**
 * Audit log datetime picker — cross-browser, locale-aware (SLS-style calendar + time columns).
 */
(function () {
    'use strict';

    var registry = {};
    var popover = null;
    var activeFieldId = null;
    var draft = null;
    var viewYear = 0;
    var viewMonth = 0;

    function pad2(n) {
        return String(n).padStart(2, '0');
    }

    function pickerLocale() {
        if (typeof auditLocale === 'function') return auditLocale();
        if (typeof window.uiLocale === 'function') return window.uiLocale();
        if (typeof window.__locale === 'string' && window.__locale.startsWith('zh')) return 'zh-CN';
        if (typeof window.__locale === 'string' && window.__locale.startsWith('ru')) return 'ru-RU';
        return 'en-US';
    }

    function pickerT(key, fallback) {
        if (typeof auditT === 'function') return auditT(key, null, fallback);
        if (typeof t === 'function') {
            var v = t(key);
            if (v && v !== key) return v;
        }
        return fallback;
    }

    function partsToStorage(p) {
        if (!p) return '';
        return p.y + '-' + pad2(p.m) + '-' + pad2(p.d) + 'T' + pad2(p.h) + ':' + pad2(p.mi);
    }

    function parseStorage(value) {
        if (!value) return null;
        var m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(String(value).trim());
        if (!m) return null;
        return { y: +m[1], m: +m[2], d: +m[3], h: +m[4], mi: +m[5] };
    }

    function formatDisplay(parts) {
        if (!parts) return '';
        var loc = pickerLocale();
        try {
            var d = new Date(parts.y, parts.m - 1, parts.d, parts.h, parts.mi, 0, 0);
            return d.toLocaleString(loc, {
                year: 'numeric',
                month: '2-digit',
                day: '2-digit',
                hour: '2-digit',
                minute: '2-digit',
                hour12: false
            });
        } catch (_) {
            return partsToStorage(parts).replace('T', ' ');
        }
    }

    function nowParts() {
        var n = new Date();
        return { y: n.getFullYear(), m: n.getMonth() + 1, d: n.getDate(), h: n.getHours(), mi: n.getMinutes() };
    }

    function startOfTodayParts() {
        var n = new Date();
        return { y: n.getFullYear(), m: n.getMonth() + 1, d: n.getDate(), h: 0, mi: 0 };
    }

    function monthTitle(year, month) {
        var loc = pickerLocale();
        if (loc.startsWith('zh')) {
            return year + '\u5e74' + pad2(month) + '\u6708';
        }
        try {
            return new Date(year, month - 1, 1).toLocaleString(loc, { month: 'long', year: 'numeric' });
        } catch (_) {
            return year + '-' + pad2(month);
        }
    }

    function weekdayHeaders() {
        var loc = pickerLocale();
        if (loc.startsWith('zh')) {
            return ['\u65e5', '\u4e00', '\u4e8c', '\u4e09', '\u56db', '\u4e94', '\u516d'];
        }
        if (loc.startsWith('ru')) {
            return ['Вс', 'Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб'];
        }
        return ['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa'];
    }

    function buildMonthGrid(year, month) {
        var first = new Date(year, month - 1, 1);
        var start = new Date(first);
        start.setDate(first.getDate() - first.getDay());
        var cells = [];
        var cursor = new Date(start);
        for (var i = 0; i < 42; i++) {
            cells.push({
                y: cursor.getFullYear(),
                m: cursor.getMonth() + 1,
                d: cursor.getDate(),
                inMonth: cursor.getMonth() === month - 1
            });
            cursor.setDate(cursor.getDate() + 1);
        }
        return cells;
    }

    function ensurePopover() {
        if (popover) return popover;
        popover = document.createElement('div');
        popover.className = 'audit-dt-popover';
        popover.hidden = true;
        popover.setAttribute('role', 'dialog');
        popover.innerHTML =
            '<div class="audit-dt-popover-inner">' +
            '<div class="audit-dt-head">' +
            '<button type="button" class="audit-dt-nav" data-nav="prev" aria-label="prev">&lsaquo;</button>' +
            '<span class="audit-dt-month-label"></span>' +
            '<button type="button" class="audit-dt-nav" data-nav="next" aria-label="next">&rsaquo;</button>' +
            '</div>' +
            '<div class="audit-dt-body">' +
            '<div class="audit-dt-calendar"></div>' +
            '<div class="audit-dt-time">' +
            '<div class="audit-dt-time-col" data-part="hour">' +
            '<span class="audit-dt-time-label audit-dt-hour-label"></span>' +
            '<div class="audit-dt-time-list"></div>' +
            '</div>' +
            '<div class="audit-dt-time-col" data-part="minute">' +
            '<span class="audit-dt-time-label audit-dt-minute-label"></span>' +
            '<div class="audit-dt-time-list"></div>' +
            '</div>' +
            '</div>' +
            '</div>' +
            '<div class="audit-dt-footer">' +
            '<button type="button" class="audit-dt-footer-btn" data-action="clear"></button>' +
            '<button type="button" class="audit-dt-footer-btn" data-action="today"></button>' +
            '<button type="button" class="audit-dt-footer-btn audit-dt-footer-btn--primary" data-action="confirm"></button>' +
            '</div>' +
            '</div>';
        document.body.appendChild(popover);

        popover.addEventListener('click', function (ev) {
            ev.stopPropagation();
            var btn = ev.target.closest('[data-nav]');
            if (btn) {
                if (btn.getAttribute('data-nav') === 'prev') {
                    viewMonth -= 1;
                    if (viewMonth < 1) { viewMonth = 12; viewYear -= 1; }
                } else {
                    viewMonth += 1;
                    if (viewMonth > 12) { viewMonth = 1; viewYear += 1; }
                }
                renderPopover();
                return;
            }
            var dayBtn = ev.target.closest('[data-day]');
            if (dayBtn && draft) {
                draft.y = +dayBtn.getAttribute('data-y');
                draft.m = +dayBtn.getAttribute('data-m');
                draft.d = +dayBtn.getAttribute('data-d');
                if (draft.y !== viewYear || draft.m !== viewMonth) {
                    viewYear = draft.y;
                    viewMonth = draft.m;
                    renderCalendar();
                } else {
                    updateDaySelection();
                }
                return;
            }
            var timeBtn = ev.target.closest('[data-time]');
            if (timeBtn && draft) {
                var part = timeBtn.getAttribute('data-part');
                var val = +timeBtn.getAttribute('data-time');
                if (part === 'hour') draft.h = val;
                if (part === 'minute') draft.mi = val;
                updateTimeSelection();
                return;
            }
            var actionBtn = ev.target.closest('[data-action]');
            if (!actionBtn) return;
            var action = actionBtn.getAttribute('data-action');
            if (action === 'clear') {
                applyValue(activeFieldId, '');
                closePopover();
            } else if (action === 'today') {
                if (draft) {
                    var t = nowParts();
                    draft.y = t.y; draft.m = t.m; draft.d = t.d;
                    viewYear = t.y; viewMonth = t.m;
                }
                renderPopover();
            } else if (action === 'confirm') {
                applyValue(activeFieldId, partsToStorage(draft));
                closePopover();
            }
        });

        document.addEventListener('click', onDocumentClick);
        document.addEventListener('keydown', onDocumentKeydown);
        document.addEventListener('languagechange', function () {
            if (!popover.hidden) renderPopover();
            refreshAllDisplays();
        });

        return popover;
    }

    function onDocumentClick(ev) {
        if (!popover || popover.hidden) return;
        if (popover.contains(ev.target)) return;
        if (activeFieldId && registry[activeFieldId] && registry[activeFieldId].wrap.contains(ev.target)) return;
        closePopover();
    }

    function onDocumentKeydown(ev) {
        if (ev.key === 'Escape' && popover && !popover.hidden) {
            closePopover();
        }
    }

    function positionPopover(fieldWrap) {
        var rect = fieldWrap.getBoundingClientRect();
        var width = 320;
        popover.style.width = width + 'px';
        var left = rect.left;
        if (left + width > window.innerWidth - 12) {
            left = Math.max(12, window.innerWidth - width - 12);
        }
        popover.style.left = left + 'px';
        var top = rect.bottom + 6;
        if (top + 340 > window.innerHeight - 12) {
            top = Math.max(12, rect.top - 340 - 6);
        }
        popover.style.top = top + 'px';
    }

    function renderCalendar() {
        if (!popover || !draft) return;
        popover.querySelector('.audit-dt-month-label').textContent = monthTitle(viewYear, viewMonth);
        var cal = popover.querySelector('.audit-dt-calendar');
        var headers = weekdayHeaders();
        var html = '<div class="audit-dt-weekdays">';
        headers.forEach(function (h) { html += '<span>' + h + '</span>'; });
        html += '</div><div class="audit-dt-days">';
        buildMonthGrid(viewYear, viewMonth).forEach(function (cell) {
            var cls = 'audit-dt-day';
            if (!cell.inMonth) cls += ' is-other-month';
            if (draft && cell.y === draft.y && cell.m === draft.m && cell.d === draft.d) cls += ' is-selected';
            html += '<button type="button" class="' + cls + '" data-day="1" data-y="' + cell.y +
                '" data-m="' + cell.m + '" data-d="' + cell.d + '">' + cell.d + '</button>';
        });
        html += '</div>';
        cal.innerHTML = html;
    }

    function renderTimeLists() {
        if (!popover || !draft) return;
        var hourList = popover.querySelector('[data-part="hour"] .audit-dt-time-list');
        var minuteList = popover.querySelector('[data-part="minute"] .audit-dt-time-list');
        var hourHtml = '';
        var minuteHtml = '';
        var h;
        for (h = 0; h < 24; h++) {
            hourHtml += '<button type="button" class="audit-dt-time-item' + (draft && draft.h === h ? ' is-selected' : '') +
                '" data-part="hour" data-time="' + h + '">' + pad2(h) + '</button>';
        }
        for (h = 0; h < 60; h++) {
            minuteHtml += '<button type="button" class="audit-dt-time-item' + (draft && draft.mi === h ? ' is-selected' : '') +
                '" data-part="minute" data-time="' + h + '">' + pad2(h) + '</button>';
        }
        hourList.innerHTML = hourHtml;
        minuteList.innerHTML = minuteHtml;
        scrollTimeSelection(hourList, draft.h);
        scrollTimeSelection(minuteList, draft.mi);
    }

    function updateDaySelection() {
        if (!popover || !draft) return;
        popover.querySelectorAll('.audit-dt-day').forEach(function (btn) {
            var selected = +btn.getAttribute('data-y') === draft.y &&
                +btn.getAttribute('data-m') === draft.m &&
                +btn.getAttribute('data-d') === draft.d;
            btn.classList.toggle('is-selected', selected);
        });
    }

    function updateTimeSelection() {
        if (!popover || !draft) return;
        var hourList = popover.querySelector('[data-part="hour"] .audit-dt-time-list');
        var minuteList = popover.querySelector('[data-part="minute"] .audit-dt-time-list');
        if (!hourList || !minuteList || !hourList.children.length) {
            renderTimeLists();
            return;
        }
        hourList.querySelectorAll('.audit-dt-time-item').forEach(function (btn) {
            btn.classList.toggle('is-selected', +btn.getAttribute('data-time') === draft.h);
        });
        minuteList.querySelectorAll('.audit-dt-time-item').forEach(function (btn) {
            btn.classList.toggle('is-selected', +btn.getAttribute('data-time') === draft.mi);
        });
        scrollTimeSelection(hourList, draft.h);
        scrollTimeSelection(minuteList, draft.mi);
    }

    function renderPopover() {
        if (!popover || !draft) return;
        popover.querySelector('.audit-dt-hour-label').textContent = pickerT('settingsAudit.pickerHour', 'Hour');
        popover.querySelector('.audit-dt-minute-label').textContent = pickerT('settingsAudit.pickerMinute', 'Min');
        popover.querySelector('[data-action="clear"]').textContent = pickerT('settingsAudit.pickerClear', 'Clear');
        popover.querySelector('[data-action="today"]').textContent = pickerT('settingsAudit.pickerToday', 'Today');
        popover.querySelector('[data-action="confirm"]').textContent = pickerT('settingsAudit.pickerConfirm', 'OK');
        renderCalendar();
        renderTimeLists();
    }

    function scrollTimeSelection(listEl, value) {
        var sel = listEl.querySelector('.is-selected');
        if (sel && sel.scrollIntoView) {
            sel.scrollIntoView({ block: 'center' });
        }
    }

    function openPopover(fieldId) {
        ensurePopover();
        var entry = registry[fieldId];
        if (!entry) return;
        activeFieldId = fieldId;
        var stored = entry.wrap.dataset.value || '';
        draft = parseStorage(stored) || nowParts();
        viewYear = draft.y;
        viewMonth = draft.m;
        renderPopover();
        positionPopover(entry.wrap);
        popover.hidden = false;
    }

    function closePopover() {
        if (!popover) return;
        popover.hidden = true;
        activeFieldId = null;
        draft = null;
    }

    function refreshDisplay(fieldId) {
        var entry = registry[fieldId];
        if (!entry) return;
        var parts = parseStorage(entry.wrap.dataset.value || '');
        entry.input.value = parts ? formatDisplay(parts) : '';
        entry.input.placeholder = pickerT('settingsAudit.datetimePlaceholder', 'Select date & time');
        entry.clearBtn.hidden = !parts;
    }

    function refreshAllDisplays() {
        Object.keys(registry).forEach(refreshDisplay);
    }

    function applyValue(fieldId, storageValue) {
        var entry = registry[fieldId];
        if (!entry) return;
        entry.wrap.dataset.value = storageValue || '';
        refreshDisplay(fieldId);
    }

    function bindField(fieldId) {
        var wrap = document.getElementById(fieldId);
        if (!wrap || wrap.dataset.auditDtBound === '1') return;
        var input = wrap.querySelector('.audit-datetime-input');
        var openBtn = wrap.querySelector('.audit-datetime-open-btn');
        var clearBtn = wrap.querySelector('.audit-datetime-clear-btn');
        if (!input || !openBtn || !clearBtn) return;

        wrap.dataset.auditDtBound = '1';
        registry[fieldId] = { wrap: wrap, input: input, clearBtn: clearBtn };

        openBtn.addEventListener('click', function (ev) {
            ev.preventDefault();
            ev.stopPropagation();
            if (!popover || popover.hidden || activeFieldId !== fieldId) {
                openPopover(fieldId);
            } else {
                closePopover();
            }
        });
        input.addEventListener('click', function (ev) {
            ev.stopPropagation();
            openPopover(fieldId);
        });
        clearBtn.addEventListener('click', function (ev) {
            ev.preventDefault();
            ev.stopPropagation();
            applyValue(fieldId, '');
        });
        refreshDisplay(fieldId);
    }

    window.AuditDatetimePicker = {
        init: function () {
            bindField('audit-filter-since-field');
            bindField('audit-filter-until-field');
            refreshAllDisplays();
        },
        getValue: function (inputId) {
            var fieldId = inputId === 'audit-filter-since' ? 'audit-filter-since-field' : 'audit-filter-until-field';
            var entry = registry[fieldId];
            return entry ? (entry.wrap.dataset.value || '') : '';
        },
        setValue: function (inputId, dateObj) {
            if (!dateObj || Number.isNaN(dateObj.getTime())) return;
            var fieldId = inputId === 'audit-filter-since' ? 'audit-filter-since-field' : 'audit-filter-until-field';
            var p = {
                y: dateObj.getFullYear(),
                m: dateObj.getMonth() + 1,
                d: dateObj.getDate(),
                h: dateObj.getHours(),
                mi: dateObj.getMinutes()
            };
            applyValue(fieldId, partsToStorage(p));
        },
        clearAll: function () {
            applyValue('audit-filter-since-field', '');
            applyValue('audit-filter-until-field', '');
            closePopover();
        }
    };
})();
