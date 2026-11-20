/** 事实新增 / 编辑弹窗：fact_key 与 summary 必填，body 存攻击链与 POC，links 维护入边。 */
import { useEffect, useMemo, useState } from 'react';
import { Checkbox, Input, Select, TextArea, Typography } from '@douyinfe/semi-ui';
import { api } from '../../lib/http';
import { useT } from '../../lib/i18n';
import { Drawer, toastError, toastSuccess } from '../../components/ui';
import {
    CONFIDENCE_OPTIONS, EDGE_TYPE_HINTS, FACT_CATEGORIES, errText, formatIncomingLinks
} from './helpers';

const { Text } = Typography;

const EMPTY = {
    fact_key: '',
    category: 'note',
    summary: '',
    body: '',
    confidence: 'tentative',
    pinned: false,
    related_vulnerability_id: '',
    links_text: ''
};

export default function FactFormModal({ visible, projectId, factKey, onCancel, onSaved }) {
    const t = useT();
    const [form, setForm] = useState(EMPTY);
    const [categoryOptions, setCategoryOptions] = useState(FACT_CATEGORIES);
    const [loading, setLoading] = useState(false);
    const [saving, setSaving] = useState(false);
    const [factId, setFactId] = useState('');

    const editing = Boolean(factKey);

    useEffect(() => {
        if (!visible) return;
        setForm(EMPTY);
        setCategoryOptions(FACT_CATEGORIES);
        setFactId('');
        setLoading(false);
        setSaving(false);
        if (!factKey) return;
        let cancelled = false;
        setLoading(true);
        api.get(`/projects/${projectId}/facts`, { fact_key: factKey, include_links: 'true' })
            .then(fact => {
                if (cancelled || !fact) return;
                const category = fact.category || 'note';
                if (!FACT_CATEGORIES.some(item => item.value === category)) {
                    setCategoryOptions([...FACT_CATEGORIES, { value: category, label: `${category}（自定义）` }]);
                }
                setFactId(fact.id || '');
                setForm({
                    fact_key: fact.fact_key || factKey,
                    category,
                    summary: fact.summary || '',
                    body: fact.body || '',
                    confidence: (fact.confidence || 'tentative').toLowerCase(),
                    pinned: Boolean(fact.pinned),
                    related_vulnerability_id: fact.related_vulnerability_id || '',
                    links_text: formatIncomingLinks(fact.incoming_links)
                });
            })
            .catch(error => {
                if (!cancelled) toastError(errText(error, t('projects.loadFactFailed', null, '加载事实失败')));
            })
            .finally(() => { if (!cancelled) setLoading(false); });
        return () => { cancelled = true; };
    }, [visible, projectId, factKey]);

    const update = (patch) => setForm(prev => ({ ...prev, ...patch }));

    const linksHint = useMemo(() => EDGE_TYPE_HINTS.join('、'), []);

    const submit = async () => {
        const key = form.fact_key.trim();
        const summary = form.summary.trim();
        if (!key || !summary) {
            toastError(t('projects.factKeySummaryRequired', null, 'fact_key 与 summary 必填'));
            return;
        }
        const payload = {
            fact_key: key,
            category: form.category || 'note',
            summary,
            body: form.body,
            confidence: form.confidence || 'tentative',
            pinned: Boolean(form.pinned),
            related_vulnerability_id: form.related_vulnerability_id.trim(),
            links_text: form.links_text
        };
        if (editing && !factId) {
            toastError(t('projects.loadFactFailed', null, '加载事实失败'));
            return;
        }
        setSaving(true);
        try {
            if (editing && factId) {
                await api.put(`/projects/${projectId}/facts/${factId}`, payload);
            } else {
                await api.post(`/projects/${projectId}/facts`, payload);
            }
            toastSuccess(t('projects.saved', null, '已保存'));
            onSaved?.();
        } catch (error) {
            toastError(errText(error, t('projects.saveFailed', null, '保存失败')));
        } finally {
            setSaving(false);
        }
    };

    return (
        <Drawer
            visible={visible}
            title={editing ? t('projects.editFact', null, '编辑事实') : t('projects.addFact', null, '添加事实')}
            onCancel={onCancel}
            onOk={submit}
            okText={editing ? t('projects.saveChanges', null, '保存修改') : t('projects.saveFact', null, '保存事实')}
            cancelText={t('common.cancel', null, '取消')}
            confirmLoading={saving}
            width={720}
        >
            <Text type="tertiary" size="small">
                {t('projects.factModalSubtitle', null, '摘要注入黑板索引；body 沉淀攻击链与 POC，供审计复现（与漏洞记录分工）')}
            </Text>

            <div className="project-form-grid">
                <div className="project-form-field">
                    <Text strong size="small">fact_key</Text>
                    <Input
                        value={form.fact_key}
                        disabled={loading}
                        placeholder="target/primary_domain"
                        onChange={value => update({ fact_key: value })}
                    />
                </div>
                <div className="project-form-field">
                    <Text strong size="small">{t('projects.category', null, '分类')}</Text>
                    <Select
                        value={form.category}
                        optionList={categoryOptions}
                        onChange={value => update({ category: value })}
                        style={{ width: '100%' }}
                    />
                </div>
            </div>

            <div className="project-form-field">
                <Text strong size="small">{t('projects.summary', null, '摘要')}</Text>
                <Input
                    value={form.summary}
                    placeholder={t('projects.summary', null, '摘要') + '：什么 + 在哪 + 如何验证'}
                    onChange={value => update({ summary: value })}
                />
            </div>

            <div className="project-form-grid">
                <div className="project-form-field">
                    <Text strong size="small">{t('projects.confidence', null, '置信度')}</Text>
                    <Select
                        value={form.confidence}
                        optionList={CONFIDENCE_OPTIONS}
                        onChange={value => update({ confidence: value })}
                        style={{ width: '100%' }}
                    />
                </div>
                <div className="project-form-field">
                    <Text strong size="small">{t('projects.relatedVulnIdLabel', null, '关联漏洞 ID')}</Text>
                    <Input
                        value={form.related_vulnerability_id}
                        placeholder={t('projects.optional', null, '可选')}
                        onChange={value => update({ related_vulnerability_id: value })}
                    />
                </div>
            </div>

            <div className="project-form-field">
                <Text strong size="small">body</Text>
                <TextArea
                    value={form.body}
                    rows={10}
                    placeholder={'## 验证步骤\n1. …\n\n```http\nGET /path HTTP/1.1\n```'}
                    onChange={value => update({ body: value })}
                />
                <Text type="tertiary" size="small">
                    {t('projects.factBodyRelationsHint', null, '正文中的 discovered_on 等关系请填写到下方“关系边”字段，才能生成图谱连线。')}
                </Text>
            </div>

            <div className="project-form-field">
                <Text strong size="small">{t('projects.factLinksLabel', null, '关系边（from → 本事实）')}</Text>
                <TextArea
                    value={form.links_text}
                    rows={3}
                    placeholder={t('projects.factLinksPlaceholder', null, 'discovered_on: target/primary_domain')}
                    onChange={value => update({ links_text: value })}
                />
                <Text type="tertiary" size="small">
                    {t('projects.factLinksHint', null, '每行一条：type: source_fact_key（来源 → 当前事实）。')}
                    {' '}常用 type：{linksHint}。
                </Text>
            </div>

            <Checkbox checked={form.pinned} onChange={event => update({ pinned: event.target.checked })}>
                {t('projects.pinFact', null, '置顶事实（列表与黑板索引优先）')}
            </Checkbox>
        </Drawer>
    );
}
