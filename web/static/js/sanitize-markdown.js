/**
 * Unified Markdown → safe HTML rendering (DOMPurify + marked).
 * Timeline/process details use stricter profile, full-page HTML falls back to escaped <pre>.
 */
(function (global) {
    'use strict';

    const CHAT_SANITIZE_CONFIG = {
        ALLOWED_TAGS: ['p', 'br', 'strong', 'em', 'u', 's', 'code', 'pre', 'blockquote',
            'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'ul', 'ol', 'li', 'a', 'img',
            'table', 'thead', 'tbody', 'tr', 'th', 'td', 'hr'],
        ALLOWED_ATTR: ['href', 'title', 'alt', 'src', 'class'],
        ALLOW_DATA_ATTR: false,
    };

    /** Process details timeline: prohibit img, reduce external connections and malicious resources */
    const TIMELINE_SANITIZE_CONFIG = {
        ALLOWED_TAGS: ['p', 'br', 'strong', 'em', 'u', 's', 'code', 'pre', 'blockquote',
            'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'ul', 'ol', 'li', 'a',
            'table', 'thead', 'tbody', 'tr', 'th', 'td', 'hr'],
        ALLOWED_ATTR: ['href', 'title', 'alt', 'class'],
        ALLOW_DATA_ATTR: false,
    };

    const DANGEROUS_URL_PREFIXES = [
        'javascript:',
        'vbscript:',
        'data:text/html',
        'data:text/javascript',
        'data:application/javascript',
    ];

    let domPurifyHooksInstalled = false;

    function escapeHtmlLocal(text) {
        if (text == null || text === '') return '';
        const div = document.createElement('div');
        div.textContent = String(text);
        return div.innerHTML;
    }

    function installDomPurifyHooks() {
        if (domPurifyHooksInstalled || typeof DOMPurify === 'undefined' || !DOMPurify.addHook) {
            return;
        }
        DOMPurify.addHook('uponSanitizeAttribute', function (node, data) {
            const attrName = (data.attrName || '').toLowerCase();
            if ((attrName !== 'src' && attrName !== 'href') || !data.attrValue) {
                return;
            }
            const value = String(data.attrValue).trim().toLowerCase();
            for (let i = 0; i < DANGEROUS_URL_PREFIXES.length; i++) {
                if (value.indexOf(DANGEROUS_URL_PREFIXES[i]) === 0) {
                    data.keepAttr = false;
                    return;
                }
            }
            if (value.indexOf('blob:') === 0) {
                data.keepAttr = false;
                return;
            }
            if (attrName === 'src' && node.tagName && node.tagName.toLowerCase() === 'img') {
                if (value.length <= 2 || /^[a-z]$/i.test(value)) {
                    data.keepAttr = false;
                }
            }
        });
        domPurifyHooksInstalled = true;
    }

    /** When Markdown structure is obvious, it should not be misjudged as full-page HTML due to scattered HTML tags */
    function looksLikeMarkdown(src) {
        const s = String(src);
        return /^#{1,6}\s/m.test(s)
            || /^\s*[-*+]\s/m.test(s)
            || /^\s*\d+\.\s/m.test(s)
            || /\*\*[^*\n]+\*\*/.test(s)
            || /`[^`\n]+`/.test(s)
            || /^```/m.test(s)
            || /^\|.+\|/m.test(s)
            || /^\s*>\s/m.test(s);
    }

    /** The full-page HTML returned by the probing tool should not be rendered as rich text */
    function isHeavyRawHtml(src) {
        const s = String(src);
        if (looksLikeMarkdown(s)) {
            return false;
        }
        if (/<!DOCTYPE\s+html/i.test(s) || /<\s*html\b/i.test(s)) {
            return true;
        }
        if (/<\s*(head|body|iframe|object|embed|form|script|style|meta|link|base)\b/i.test(s)) {
            return true;
        }
        const tags = s.match(/<[a-z][^>]*>/gi);
        return tags != null && tags.length >= 8;
    }

    function escapePlainTextAsHtml(text) {
        return escapeHtmlLocal(text).replace(/\n/g, '<br>');
    }

    function formatHtmlAsEscapedPre(text) {
        return '<pre class="tool-result sanitized-raw-html-fallback">' + escapeHtmlLocal(text) + '</pre>';
    }

    function normalizeSource(text) {
        const raw = text == null ? '' : String(text);
        if (typeof global.normalizeAssistantMarkdownSource === 'function') {
            return global.normalizeAssistantMarkdownSource(raw);
        }
        return raw;
    }

    function parseMarkdownSrc(src) {
        if (typeof marked === 'undefined') {
            return null;
        }
        try {
            marked.setOptions({ breaks: true, gfm: true });
            return marked.parse(src, { async: false });
        } catch (e) {
            console.error('Failed to parse Markdown:', e);
            return null;
        }
    }

    function sanitizeConfigForProfile(profile) {
        return profile === 'timeline' ? TIMELINE_SANITIZE_CONFIG : CHAT_SANITIZE_CONFIG;
    }

    /**
     * @param {string|null|undefined} text
     * @param {{ profile?: 'chat'|'timeline' }} [options]
     * @returns {string} safe HTML
     */
    function buildRichHtmlFromSource(src) {
        const hasHtmlTags = /<[a-z][\s\S]*>/i.test(src);
        const preferMarkdown = typeof marked !== 'undefined'
            && (looksLikeMarkdown(src) || !hasHtmlTags);

        if (preferMarkdown) {
            const parsed = parseMarkdownSrc(src);
            if (parsed != null) {
                return parsed;
            }
        }
        if (hasHtmlTags) {
            return src;
        }
        return escapePlainTextAsHtml(src);
    }

    function formatMarkdownToHtml(text, options) {
        const profile = (options && options.profile === 'timeline') ? 'timeline' : 'chat';
        const src = normalizeSource(text);

        if (isHeavyRawHtml(src)) {
            return formatHtmlAsEscapedPre(src);
        }

        if (typeof DOMPurify === 'undefined') {
            console.warn('DOMPurify not loaded, Markdown has been degraded to plain text rendering (escaped, to prevent XSS)');
            return escapePlainTextAsHtml(src);
        }

        installDomPurifyHooks();
        const config = sanitizeConfigForProfile(profile);
        return DOMPurify.sanitize(buildRichHtmlFromSource(src), config);
    }

    function sanitizeRichHtml(html, profile) {
        if (typeof DOMPurify === 'undefined') {
            return null;
        }
        installDomPurifyHooks();
        return DOMPurify.sanitize(html, sanitizeConfigForProfile(profile || 'chat'));
    }

    function stripSuspiciousImages(root) {
        if (!root || !root.querySelectorAll) {
            return;
        }
        root.querySelectorAll('img').forEach(function (img) {
            const src = (img.getAttribute('src') || '').trim();
            if (!src || src.length <= 2 || /^[a-z]$/i.test(src)) {
                img.remove();
            }
        });
    }

    global.csMarkdownSanitize = {
        CHAT_SANITIZE_CONFIG: CHAT_SANITIZE_CONFIG,
        TIMELINE_SANITIZE_CONFIG: TIMELINE_SANITIZE_CONFIG,
        installDomPurifyHooks: installDomPurifyHooks,
        formatMarkdownToHtml: formatMarkdownToHtml,
        sanitizeRichHtml: sanitizeRichHtml,
        isHeavyRawHtml: isHeavyRawHtml,
        looksLikeMarkdown: looksLikeMarkdown,
        escapeHtmlLocal: escapeHtmlLocal,
        stripSuspiciousImages: stripSuspiciousImages,
    };

    global.formatMarkdown = function formatMarkdown(text, options) {
        return formatMarkdownToHtml(text, options);
    };
})(typeof window !== 'undefined' ? window : globalThis);
